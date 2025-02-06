package m17

import (
	"fmt"
	"log"
	"net"
)

const (
	magicACKN = "ACKN"
	magicCONN = "CONN"
	magicDISC = "DISC"
	magicLSTN = "LSTN"
	magicNACK = "NACK"
	magicPING = "PING"
	magicPONG = "PONG"
	magicM17  = "M17 "
)

type Relay struct {
	Server          string
	Port            uint
	Module          byte
	EncodedCallsign []byte
	Callsign        string
	conn            *net.UDPConn
	connected       bool
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
		EncodedCallsign: cs,
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
	return c.conn.Close()
}
func (c *Relay) Handle() {
	// c.conn.SetReadDeadline(time.Now().Add(2000 * time.Millisecond))
	for !c.done {
		// Receiving a message
		buffer := make([]byte, 1024)
		l, _, err := c.conn.ReadFromUDP(buffer)
		if err != nil {
			if err, ok := err.(net.Error); ok && err.Timeout() {
				log.Printf("[DEBUG] Relay.Handle(): timeout: %v", err)
				break
			} else {
				log.Printf("[DEBUG] Relay.Handle(): error reading from UDP: %v", err)
				break
			}
		}
		buffer = buffer[:l]
		// log.Printf("[DEBUG] Packet received, len: %d:\n%#v\n%s\n", l, buffer, string(buffer[:4]))
		if l < 4 {
			// too short
			continue
		}
		magic := string(buffer[0:4])
		switch magic {
		case magicACKN:
			c.connected = true
		case magicNACK:
		case magicDISC:
			c.connected = false
		case magicPING:
			c.sendPONG()
		// case magicINFO:
		case magicM17:
			p := NewPacketFromBytes(buffer[4:])
			// log.Printf("[DEBUG] packet from relay: %#v", p)
			c.handler(p)
		}
	}
}

func (c *Relay) SendMessage(dst []byte, src []byte, message string) error {
	lsf := LSF{
		Dst:  dst,
		Src:  src,
		Type: []byte{0, 2},
	}
	lsf.CalcCRC()

	msg := []byte(message)
	if len(msg) > 820 {
		msg = msg[:820]
	}
	// add a trailing NUL, if needed
	if len(msg) == 0 || msg[len(msg)-1] != 0 {
		msg = append(msg, 0)
	}
	p := NewPacket(lsf, PacketTypeSMS, msg)

	cmd := make([]byte, 0, LSFSize+1+len(msg)+2)
	cmd = append(cmd, []byte(magicM17)...)
	cmd = append(cmd, p.ToBytes()...)
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
	copy(cmd[4:10], c.EncodedCallsign)
	cmd[10] = c.Module
	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	return nil
}
func (c *Relay) sendPONG() error {
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicPONG))
	copy(cmd[4:10], c.EncodedCallsign)
	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	return nil
}
