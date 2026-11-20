// Package mqttstatus publishes a gateway's status over MQTT in the format
// MMDVMHost and G4KLX's gateway programs use: JSON events on <Name>/json,
// each an object with one key naming its type ({"M17": {...}}), and log
// lines on <Name>/log. See docs/mqtt-design.md.
//
// Publishing never blocks the caller: messages go into a bounded queue
// that one goroutine sends from. While the broker is unreachable the queue
// holds what it can, and when it's full new messages are dropped and
// counted.
package mqttstatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Config is the [MQTT] section.
type Config struct {
	Host      string
	Port      int
	Auth      bool
	Username  string
	Password  string
	Keepalive time.Duration
	// Name is the topic prefix: events go to <Name>/json, log lines to
	// <Name>/log.
	Name string
	// LogLevel is the lowest level sent to <Name>/log: NONE, ERROR, WARN,
	// INFO or DEBUG.
	LogLevel string
}

// QoS 2 and no retain, as MMDVMHost, except for the messages the gateway
// programs retain (link).
const qos = 2

// queueLen is how many messages wait for the broker before new ones are
// dropped: a few minutes of busy traffic.
const queueLen = 256

// Check reports what's wrong with a configuration, or nil.
func (c Config) Check() error {
	var errs []error
	if c.Host == "" {
		errs = append(errs, errors.New("MQTT Host must be set"))
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("MQTT Port %d out of range (1 to 65535)", c.Port))
	}
	if c.Keepalive < 5*time.Second {
		errs = append(errs, fmt.Errorf("MQTT Keepalive %v too short (at least 5 seconds)", c.Keepalive))
	}
	if c.Name == "" || strings.ContainsAny(c.Name, "/#+") {
		errs = append(errs, fmt.Errorf("MQTT Name %q must be set, without / # or +", c.Name))
	}
	if _, ok := levelRank[strings.ToUpper(c.LogLevel)]; !ok && strings.ToUpper(c.LogLevel) != "NONE" {
		errs = append(errs, fmt.Errorf("MQTT LogLevel %q must be NONE, ERROR, WARN, INFO or DEBUG", c.LogLevel))
	}
	return errors.Join(errs...)
}

type message struct {
	topic   string
	payload []byte
	retain  bool
}

// Publisher sends messages to an MQTT broker.
type Publisher struct {
	cfg       Config
	client    mqtt.Client
	queue     chan message
	dropped   atomic.Int64
	done      chan struct{}
	finished  chan struct{}
	closeOnce sync.Once

	mu        sync.Mutex
	onConnect func()
	connects  int
}

// New creates a Publisher. Nothing is sent until Start.
func New(cfg Config) (*Publisher, error) {
	if err := cfg.Check(); err != nil {
		return nil, err
	}
	p := &Publisher{
		cfg:      cfg,
		queue:    make(chan message, queueLen),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	host, _ := os.Hostname()
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port)).
		SetClientID(clientID(cfg.Name, host)).
		SetKeepAlive(cfg.Keepalive).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetMaxReconnectInterval(time.Minute).
		SetCleanSession(true).
		SetOnConnectHandler(func(mqtt.Client) {
			log.Printf("[INFO] MQTT connected to %s:%d", cfg.Host, cfg.Port)
			p.mu.Lock()
			p.connects++
			f := p.onConnect
			reconnect := p.connects > 1
			p.mu.Unlock()
			// The first time, the queue still holds everything published
			// so far. After a reconnection the broker may have restarted
			// and consumers may be new, so the state goes out again.
			if f != nil && reconnect {
				go f()
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Printf("[INFO] MQTT connection lost: %v; reconnecting", err)
		})
	if cfg.Auth {
		opts.SetUsername(cfg.Username).SetPassword(cfg.Password)
	}
	p.client = mqtt.NewClient(opts)
	return p, nil
}

// clientID names the connection: the topic name and host, so two gateways
// sharing a broker don't knock each other off.
func clientID(name, host string) string {
	id := name
	if host != "" {
		id += "-" + host
	}
	if len(id) > 23 { // MQTT 3.1's limit, which some brokers still enforce
		id = id[:23]
	}
	return id
}

// OnConnect sets a function called (in its own goroutine) each time the
// broker connection is made again after a loss, to republish the current
// state.
func (p *Publisher) OnConnect(f func()) {
	p.mu.Lock()
	p.onConnect = f
	p.mu.Unlock()
}

// Start connects in the background and starts sending. It doesn't wait
// for the broker.
func (p *Publisher) Start() {
	log.Printf("[INFO] MQTT publishing to %s:%d as %s", p.cfg.Host, p.cfg.Port, p.cfg.Name)
	p.client.Connect() // retries in the background (SetConnectRetry)
	go p.run()
	go p.reportDrops()
}

func (p *Publisher) run() {
	defer close(p.finished)
	for {
		var m message
		select {
		case m = <-p.queue:
		case <-p.done:
			// Send what's left, briefly: the last messages are the
			// shutdown ones.
			for {
				select {
				case m = <-p.queue:
					p.send(m, 500*time.Millisecond)
				default:
					return
				}
			}
		}
		// Hold the message until the broker is there, or give up when
		// closing
		for !p.client.IsConnectionOpen() {
			select {
			case <-p.done:
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		p.send(m, 5*time.Second)
	}
}

func (p *Publisher) send(m message, wait time.Duration) {
	t := p.client.Publish(m.topic, qos, m.retain, m.payload)
	if !t.WaitTimeout(wait) {
		p.dropped.Add(1)
		return
	}
	if t.Error() != nil {
		p.dropped.Add(1)
	}
}

func (p *Publisher) reportDrops() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
			if n := p.dropped.Swap(0); n > 0 {
				log.Printf("[WARN] MQTT: %d messages dropped in the last minute (broker unreachable or slow)", n)
			}
		}
	}
}

// Close sends what's queued (for up to about a second) and disconnects.
func (p *Publisher) Close() {
	p.closeOnce.Do(func() {
		close(p.done)
		select {
		case <-p.finished:
		case <-time.After(2 * time.Second):
		}
		p.client.Disconnect(250)
	})
}

func (p *Publisher) enqueue(m message) {
	select {
	case p.queue <- m:
	default:
		p.dropped.Add(1)
	}
}

// JSON publishes v as {"typ": v} on <Name>/json.
func (p *Publisher) JSON(typ string, v any, retain bool) {
	if p == nil {
		return
	}
	b, err := json.Marshal(map[string]any{typ: v})
	if err != nil {
		log.Printf("[ERROR] MQTT: encoding %s: %v", typ, err)
		return
	}
	p.enqueue(message{topic: p.cfg.Name + "/json", payload: b, retain: retain})
}

// logLine publishes a formatted log line on <Name>/log.
func (p *Publisher) logLine(line string) {
	p.enqueue(message{topic: p.cfg.Name + "/log", payload: []byte(line)})
}

// Timestamp formats t as MMDVMHost does: UTC, with milliseconds.
func Timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Message is a queued message, as Drain returns it.
type Message struct {
	Topic   string
	Payload string
	Retain  bool
}

// Drain removes and returns the queued messages. It's for tests: a
// Publisher that hasn't been started keeps everything in its queue.
func (p *Publisher) Drain() []Message {
	var out []Message
	for {
		select {
		case m := <-p.queue:
			out = append(out, Message{Topic: m.topic, Payload: string(m.payload), Retain: m.retain})
		default:
			return out
		}
	}
}
