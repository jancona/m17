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
	handler         func([]byte) error
	done            bool
}

func NewM17Relay(server string, port uint, module string, callsign string, handler func([]byte) error) (*Relay, error) {
	cs, err := EncodeCallsign(callsign)
	if err != nil {
		return nil, fmt.Errorf("bad callsign %s: %w", callsign, err)
	}
	if len(module) != 1 || module[0] < 'A' || module[0] > 'Z' {
		return nil, fmt.Errorf("module should be A-Z, got '%s'", module)
	}
	c := Relay{
		Server:          server,
		Port:            port,
		Module:          []byte(module)[0],
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
				log.Printf("[DEBUG] discoverReceive: timeout: %v", err)
				break
			} else {
				log.Printf("[DEBUG] discoverReceive: error reading from UDP: %v", err)
				break
			}
		}
		buffer = buffer[:l]
		// fmt.Printf("packet received from: %s, buffer:\n%#v\n%s\n", rmAddr, buffer, string(buffer[:4]))
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
			c.handler(buffer)
		}
	}
}

func (c *Relay) SendMessage(dst []byte, src []byte, message string) error {
	msg := []byte(message)
	if len(msg) > 821 {
		msg = msg[:821]
	}
	cmd := make([]byte, 17+len(msg))
	copy(cmd, []byte(magicM17))
	copy(cmd[4:10], dst)
	copy(cmd[10:16], src)
	cmd[16] = 0x05
	copy(cmd[17:], msg)
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
