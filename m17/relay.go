package m17

import (
	"fmt"
	"log"
	"net"
	"time"
)

const (
	magicLen = 4

	magicACKN           = "ACKN"
	magicCONN           = "CONN"
	magicDISC           = "DISC"
	magicLSTN           = "LSTN"
	magicNACK           = "NACK"
	magicPING           = "PING"
	magicPONG           = "PONG"
	magicM17Voice       = "M17 "
	magicM17VoiceHeader = "M17H"
	magicM17VoiceData   = "M17D"
	magicM17Packet      = "M17P"
)

type Relay struct {
	Server          string
	Port            uint
	Module          byte
	EncodedCallsign [6]byte
	Callsign        string
	conn            *net.UDPConn
	connected       bool
	lastPing        time.Time
	handler         func(Packet) error
	done            bool
}

func NewRelay(server string, port uint, module string, callsign string, handler func(p Packet) error) (*Relay, error) {
	cs, err := EncodeCallsign(callsign)
	if err != nil {
		return nil, fmt.Errorf("bad callsign %s: %w", callsign, err)
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
	c := Relay{
		Server:          server,
		Port:            port,
		Module:          m,
		Callsign:        callsign,
		EncodedCallsign: *cs,
		handler:         handler,
	}
	return &c, nil
}

func (c *Relay) Connect() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", c.Server, c.Port))
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	// Dial UDP connection to relay/reflector
	c.conn, err = net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	err = c.sendCONN()
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}

	return nil
}
func (c *Relay) Close() error {
	log.Print("[DEBUG] Relay.Close()")
	c.sendDISC()
	return c.conn.Close()
}
func (c *Relay) Handle() {
	for !c.done {
		// Receiving a message
		buffer := make([]byte, 1024)
		l, _, err := c.conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("[DEBUG] Relay.Handle(): error reading from UDP: %v", err)
			break
		}
		buffer = buffer[:l]
		// log.Printf("[DEBUG] Packet received, len: %d:\n%#v\n%s\n", l, buffer, string(buffer[:4]))
		if l < 4 {
			// too short
			continue
		}
		magic := string(buffer[0:4])
		if magic != "PING" {
			// log.Printf("[DEBUG] Packet received, len: %d:\n%#v\n%s\n", l, buffer, string(buffer[:4]))
		}
		switch magic {
		case magicACKN:
			c.connected = true
		case magicNACK:
			c.connected = false
			log.Print("[INFO] Received NACK, disconnecting")
			c.done = true
		case magicDISC:
			c.connected = false
			log.Print("[INFO] Received DISC, disconnecting")
			c.done = true
		case magicPING:
			c.sendPONG()
			c.lastPing = time.Now()
			// case magicINFO:
		case magicM17Voice: // M17 voice stream
			log.Print("[DEBUG] Ignoring M17 voice data")
			// TODO: Implement M17 voice streams
		case magicM17Packet: // M17 packet
			p := NewPacketFromBytes(buffer[4:])
			c.handler(p)
		case magicM17VoiceHeader: // M17 voice two-packet header
		case magicM17VoiceData: // M17 voice two-packet data
		}
	}
}

func (c *Relay) SendPacket(p Packet) error {
	if time.Since(c.lastPing) > 30*time.Second {
		log.Printf("[DEBUG] Last ping was at %s", c.lastPing)
	}
	b := p.ToBytes()
	cmd := make([]byte, 0, magicLen+len(b))
	cmd = append(cmd, []byte(magicM17Packet)...)
	cmd = append(cmd, b...)
	// log.Printf("[DEBUG] p: %#v, cmd: %#v", p, cmd)

	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending text message: %w", err)
	}
	return nil
}

func (c *Relay) sendCONN() error {
	cmd := make([]byte, 11)
	copy(cmd, []byte(magicCONN))
	copy(cmd[4:10], c.EncodedCallsign[:])
	cmd[10] = c.Module
	log.Printf("[DEBUG] Sending CONN callsign: %s, module %s, cmd: %#v", c.Callsign, string(c.Module), cmd)
	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	return nil
}
func (c *Relay) sendPONG() error {
	// log.Print("[DEBUG] Sending PONG")
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicPONG))
	copy(cmd[4:10], c.EncodedCallsign[:])
	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}
func (c *Relay) sendDISC() error {
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicDISC))
	copy(cmd[4:10], c.EncodedCallsign[:])
	log.Printf("[DEBUG] Sending DISC cmd: %#v", cmd)
	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending DISC: %w", err)
	}
	return nil
}
