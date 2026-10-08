package inet

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jancona/m17"
)

// EventFunc is called on link state changes: event is "Connect" or
// "Disconnect", with the reflector name and module as arguments.
const maxRetries = 10

// An unanswered CONN is resent after connRetryInterval, the wait doubling
// each time up to maxConnRetryInterval. Without resends a reflector that is
// not up yet (a local one still starting, say) or that drops the first CONN
// would leave the client unlinked for good; without the backoff a
// reflector that stays unreachable would get a CONN every few seconds
// indefinitely.
var (
	connRetryInterval    = 5 * time.Second
	maxConnRetryInterval = time.Minute
)

// ProbeInterval is the wait between PARROT probes, and after the last
// one before an unanswered reflector is taken to be legacy.
var ProbeInterval = 1500 * time.Millisecond

const (
	probeTries  = 3
	probeTagLen = 8
)

type reflectorKind int

const (
	reflectorUnknown reflectorKind = iota // not yet known; treated as current
	reflectorCurrent
	reflectorLegacy
)

type EventFunc func(event string, name string, module byte)

func (r *Client) event(event string) {
	if r.events != nil {
		r.events(event, r.Name, r.Module)
	}
}

type Client struct {
	Name            string
	Server          string
	Port            uint
	Module          byte
	EncodedName     *m17.EncodedCallsign
	encodedCallsign *m17.EncodedCallsign
	callsign        string
	conn            *net.UDPConn
	connected       bool
	connecting      bool
	pingTimer       *time.Timer
	retryCount      int
	packetHandler   func(m17.Packet) error
	streamHandler   func(m17.StreamDatagram) error
	running         bool
	events          EventFunc

	// ProbeReflector, set before Connect, makes the client find out on
	// each link whether the reflector is current or legacy (see Legacy).
	ProbeReflector bool
	probeMu        sync.Mutex
	probeTag       []byte // payload of the probes for this link; nil if none
	probeSent      int
	probeTimer     *time.Timer
	kind           reflectorKind
}

func NewClient(name string, server string, port uint, module string, callsign string, events EventFunc, packetHandler func(m17.Packet) error, streamHandler func(m17.StreamDatagram) error) (*Client, error) {
	cs, err := m17.EncodeCallsign(callsign)
	if err != nil {
		return nil, fmt.Errorf("bad callsign %s: %w", callsign, err)
	}
	n := m17.NormalizeCallsignModule(name + " " + module)
	encodedName, err := m17.EncodeCallsign(n)
	if err != nil {
		return nil, fmt.Errorf("bad name/module %s: %w", n, err)
	}
	var m byte
	switch {
	case len(module) == 0:
		m = 0
	case len(module) > 1 || module[0] < 'A' || module[0] > 'Z':
		return nil, fmt.Errorf("module must be A-Z or empty, got '%s'", module)
	case len(module) == 1:
		m = []byte(module)[0]
	}
	var r *Client
	r = &Client{
		Name:            name,
		Server:          server,
		Port:            port,
		Module:          m,
		EncodedName:     encodedName,
		callsign:        callsign,
		encodedCallsign: cs,
		packetHandler:   packetHandler,
		streamHandler:   streamHandler,
		events:          events,
		pingTimer: time.AfterFunc(30*time.Second, func() {
			log.Printf("[DEBUG] No PINGs received in > 30 seconds. Disconnected.")
			r.pingTimer.Stop()
			r.connected = false
			r.stopProbe()
			r.event("Disconnect")
			r.retryCount = 0
			for !r.connected && r.retryCount < maxRetries {
				// Close connection before retrying
				r.conn.Close()
				for r.running {
					log.Printf("[DEBUG] Waiting for handler to stop...")
					time.Sleep(10 * time.Second)
				}
				time.Sleep(time.Duration(r.retryCount*5) * time.Second)
				err := r.Connect()
				if err != nil {
					log.Printf("[ERROR] Connection retry error: %v", err)
				}
				r.retryCount++
				// Wait for connection ACKN
				time.Sleep(5 * time.Second)
				log.Printf("[DEBUG] Retry %d, connected: %v", r.retryCount, r.connected)
			}
			if !r.connected {
				log.Printf("[DEBUG] Max retries exceeded, giving up")
			}
		}),
	}
	r.pingTimer.Stop()
	return r, nil
}

func (r *Client) Connect() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", r.Server, r.Port))
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	// Dial UDP connection to server/reflector
	r.conn, err = net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	r.connecting = true
	err = r.sendCONN()
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	log.Printf("[DEBUG] Sent connect to %s %s:%d", r.Name, r.Server, r.Port)
	go r.handle()
	return nil
}
func (r *Client) Close() error {
	log.Print("[DEBUG] Client.Close()")
	r.running = false
	r.pingTimer.Stop()
	r.stopProbe()
	r.sendDISC()
	r.event("Disconnect")
	return r.conn.Close()
}

func (r *Client) handle() {
	r.running = true
	connGap := connRetryInterval // wait before resending an unanswered CONN
	for r.connected || r.connecting {
		wait := 10 * time.Second
		if r.connecting {
			wait = connGap
		}
		// A read deadline only: SetDeadline would also expire writes, so a
		// resent CONN, or any packet sent after a quiet spell, would fail.
		r.conn.SetReadDeadline(time.Now().Add(wait))
		// Receiving a message
		buffer := make([]byte, 1024)
		l, _, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			switch {
			case errors.Is(err, os.ErrDeadlineExceeded):
				if r.connecting {
					connGap = min(2*connGap, maxConnRetryInterval)
					log.Printf("[DEBUG] No answer to CONN; resending (next in %v)", connGap)
					if err := r.sendCONN(); err != nil {
						log.Printf("[DEBUG] Resending CONN: %v", err)
					}
				} else {
					log.Printf("[DEBUG] Reflector read timed out")
				}
				continue
			case errors.Is(err, net.ErrClosed):
				log.Printf("[DEBUG] Client.Handle(): connection closed")
				r.running = false
			default:
				// A UDP socket reports "connection refused" when nothing
				// was listening for an earlier datagram. Keep going: the
				// reflector may be starting up.
				log.Printf("[DEBUG] Client.Handle(): error reading from UDP: %v; retrying", err)
				time.Sleep(time.Second)
				continue
			}
			break
		}
		buffer = buffer[:l]
		// log.Printf("[DEBUG] Packet received, len: %d:\n%#v\n%s\n", l, buffer, string(buffer[:4]))
		if l < 4 {
			// too short
			log.Printf("[DEBUG] Short message received from reflector: [% 02x]", buffer)
			continue
		}
		magic := string(buffer[0:4])
		// if magic != "PING" {
		// 	log.Printf("[DEBUG] Packet received, len: %d:\n%#v\n%s\n", l, buffer, string(buffer[:4]))
		// }
		switch magic {
		case m17.MagicACKN:
			r.connected = true
			r.connecting = false
			connGap = connRetryInterval
			r.event("Connect")
			r.pingTimer.Reset(30 * time.Second)
			log.Printf("[DEBUG] Received ACKN")
			if r.ProbeReflector {
				r.startProbe()
			}
		case m17.MagicNACK:
			// Refused: the M17 inet spec's reasons (no such module, blocked
			// by the GateKeeper, a mismatched interlink) are not transient,
			// so asking again would only annoy the reflector. Give up.
			r.pingTimer.Stop()
			r.connected = false
			r.connecting = false
			r.stopProbe()
			log.Print("[INFO] Reflector refused the link (NACK); not retrying")
			r.event("Disconnect")
		case m17.MagicDISC:
			// Dropped by the reflector. The spec says the reflector can be
			// assumed to be going down for maintenance, and the client may
			// reconnect after an appropriate time: ask again with the
			// unanswered-CONN backoff. The ping timer's reconnect is stopped
			// so the two cannot run at once.
			r.pingTimer.Stop()
			r.connected = false
			r.connecting = true
			r.stopProbe()
			log.Printf("[INFO] Reflector disconnected us (DISC); relinking in %v", connGap)
			r.event("Disconnect")
		case m17.MagicPING:
			r.sendPONG()
			r.pingTimer.Reset(30 * time.Second)
			// case magicINFO:
		case m17.MagicM17Stream: // M17 voice stream
			// log.Printf("[DEBUG] stream buffer: % 2x", buffer)
			if r.streamHandler != nil {
				sd, err := m17.NewStreamDatagramFromBytes(buffer)
				if err != nil {
					log.Printf("[INFO] Dropping bad stream datagram: %v", err)
				} else {
					// log.Printf("[DEBUG] Receive StreamDatagram: %s", sd)
					r.streamHandler(sd)
				}
			}
		case m17.MagicM17Packet: // M17 packet
			if r.packetHandler != nil || r.ProbeReflector {
				p, err := m17.NewPacketFromBytes(buffer[4:])
				if err != nil {
					log.Printf("[INFO] Dropping bad packet from reflector: %v", err)
					continue
				}
				if r.probeReply(p) || r.packetHandler == nil {
					continue
				}
				// log.Printf("[DEBUG] Received packet from reflector. buffer: % 02x, buffer len: %d, p: %v", buffer[4:], len(buffer[4:]), p)
				r.packetHandler(p)
			}
		}
	}
	r.running = false
}

// Legacy reports whether the reflector is a legacy one (all urfd, and mrefd
// before 1.0.0). Those forward only streams addressed to their reflector
// and module, and support neither PARROT nor packet mode; current ones
// forward traffic to its real destination. A reflector is legacy if it
// did not answer the PARROT probe sent when the client linked; until then
// it is treated as current. Always false unless ProbeReflector is set.
func (r *Client) Legacy() bool {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	return r.kind == reflectorLegacy
}

// startProbe sends the reflector a raw packet to PARROT with a random tag.
// A current reflector sends it back; a legacy one does not.
func (r *Client) startProbe() {
	tag := make([]byte, probeTagLen)
	rand.Read(tag)
	r.probeMu.Lock()
	if r.probeTimer != nil {
		r.probeTimer.Stop()
	}
	r.kind = reflectorUnknown
	r.probeTag = tag
	r.probeSent = 0
	r.probeMu.Unlock()
	r.sendProbe(tag)
}

// sendProbe sends the probe tagged tag, unless it has been answered or
// superseded, and resends it after ProbeInterval. Once probeTries have
// gone unanswered, the reflector is legacy.
func (r *Client) sendProbe(tag []byte) {
	r.probeMu.Lock()
	if !bytes.Equal(tag, r.probeTag) || r.kind != reflectorUnknown {
		r.probeMu.Unlock()
		return
	}
	if r.probeSent == probeTries {
		r.kind = reflectorLegacy
		r.probeMu.Unlock()
		log.Printf("[INFO] No reply from %s to PARROT; treating it as a legacy reflector", r.Name)
		return
	}
	r.probeSent++
	r.probeTimer = time.AfterFunc(ProbeInterval, func() { r.sendProbe(tag) })
	r.probeMu.Unlock()
	p, err := m17.NewPacket("PARROT", r.callsign, m17.PacketTypeRAW, tag)
	if err == nil {
		err = r.SendPacket(*p)
	}
	if err != nil {
		log.Printf("[ERROR] Sending PARROT probe: %v", err)
	}
}

// stopProbe stops probing the reflector, as the link has gone.
func (r *Client) stopProbe() {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	if r.probeTimer != nil {
		r.probeTimer.Stop()
	}
	r.probeTag = nil
}

// probeReply reports whether p is the reflector's reply to the probe, and
// if so, notes the reflector is current. The reply's LSF is not checked:
// mrefd readdresses it to broadcast.
func (r *Client) probeReply(p m17.Packet) bool {
	if p.Type != m17.PacketTypeRAW {
		return false
	}
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	if r.probeTag == nil || !bytes.Equal(p.Payload, r.probeTag) {
		return false
	}
	r.probeTimer.Stop()
	if r.kind == reflectorLegacy {
		log.Printf("[INFO] Late reply from %s to PARROT; treating it as a current reflector", r.Name)
	} else {
		log.Printf("[DEBUG] %s replied to PARROT: current reflector", r.Name)
	}
	r.kind = reflectorCurrent
	return true
}

func (r *Client) SendPacket(p m17.Packet) error {
	b := p.ToBytes()
	cmd := make([]byte, 0, m17.MagicLen+len(b))
	cmd = append(cmd, []byte(m17.MagicM17Packet)...)
	cmd = append(cmd, b...)
	// log.Printf("[DEBUG] p: %#v, cmd: %#v", p, cmd)

	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending packet message: %w", err)
	}
	return nil
}

func (r *Client) SendStream(sd m17.StreamDatagram) error {
	// log.Printf("[DEBUG] Send StreamDatagram: %s", sd)
	_, err := r.conn.Write(sd.ToBytes())
	if err != nil {
		return fmt.Errorf("error sending stream message: %w", err)
	}
	return nil
}

func (r *Client) sendCONN() error {
	cmd := make([]byte, 11)
	copy(cmd, []byte(m17.MagicCONN))
	copy(cmd[4:10], r.encodedCallsign[:])
	cmd[10] = r.Module
	log.Printf("[DEBUG] Sending CONN callsign: %s, module %s, cmd: %#v", r.callsign, string(r.Module), cmd)
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	return nil
}
func (r *Client) sendPONG() error {
	// log.Print("[DEBUG] Sending PONG")
	cmd := make([]byte, 10)
	copy(cmd, []byte(m17.MagicPONG))
	copy(cmd[4:10], r.encodedCallsign[:])
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}
func (r *Client) sendDISC() error {
	cmd := make([]byte, 10)
	copy(cmd, []byte(m17.MagicDISC))
	copy(cmd[4:10], r.encodedCallsign[:])
	log.Printf("[DEBUG] Sending DISC cmd: %#v", cmd)
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending DISC: %w", err)
	}
	return nil
}
