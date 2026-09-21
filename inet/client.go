package inet

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"time"

	"github.com/jancona/m17"
)

// EventFunc is called on link state changes: event is "Connect" or
// "Disconnect", with the reflector name and module as arguments.
const maxRetries = 10

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
	r.sendDISC()
	r.event("Disconnect")
	return r.conn.Close()
}

func (r *Client) handle() {
	r.running = true
	for r.connected || r.connecting {
		r.conn.SetDeadline(time.Now().Add(10 * time.Second))
		// Receiving a message
		buffer := make([]byte, 1024)
		l, _, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				log.Printf("[DEBUG] Reflector read timed out")
				continue
			}
			log.Printf("[DEBUG] Client.Handle(): error reading from UDP: %v", err)
			r.running = false
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
			r.event("Connect")
			r.pingTimer.Reset(30 * time.Second)
			log.Printf("[DEBUG] Received ACKN")
		case m17.MagicNACK:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received NACK, disconnecting")
			r.event("Disconnect")
			// r.done = true
		case m17.MagicDISC:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received DISC, disconnecting")
			r.event("Disconnect")
			// r.done = true
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
			if r.packetHandler != nil {
				p := m17.NewPacketFromBytes(buffer[4:])
				// log.Printf("[DEBUG] Received packet from reflector. buffer: % 02x, buffer len: %d, p: %v", buffer[4:], len(buffer[4:]), p)
				r.packetHandler(p)
			}
		}
	}
	r.running = false
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
