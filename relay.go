package m17

import (
	"encoding/binary"
	"fmt"
	"log"
	"log/slog"
	"net"
	"time"
)

const (
	magicLen = 4

	magicACKN      = "ACKN"
	magicCONN      = "CONN"
	magicDISC      = "DISC"
	magicLSTN      = "LSTN"
	magicNACK      = "NACK"
	magicPING      = "PING"
	magicPONG      = "PONG"
	magicM17Voice  = "M17 "
	magicM17Packet = "M17P"
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
	packetHandler   func(Packet) error
	streamHandler   func(StreamDatagram) error
	done            bool
	dashLog         *slog.Logger
	lastStreamID    uint16
}

func NewRelay(server string, port uint, module string, callsign string, dashLog *slog.Logger, packetHandler func(Packet) error, streamHandler func(StreamDatagram) error) (*Relay, error) {
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
		packetHandler:   packetHandler,
		streamHandler:   streamHandler,
		dashLog:         dashLog,
		lastStreamID:    0xFFFF,
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
	log.Printf("[DEBUG] Connected to %s:%d", c.Server, c.Port)
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
			// log.Printf("[DEBUG] stream buffer: % 2x", buffer)
			if c.streamHandler != nil {
				sd, err := NewStreamDatagram(c.EncodedCallsign, buffer)
				if err != nil {
					log.Printf("[INFO] Dropping bad stream datagram: %v", err)
				} else {
					// log.Printf("[DEBUG] sd: %#v", sd)
					c.streamHandler(sd)
					if c.dashLog != nil && c.lastStreamID != sd.StreamID {
						c.dashLog.Info("", "type", "Internet", "subtype", "Voice Start", "src", sd.LSF.Src.Callsign(), "dst", sd.LSF.Dst.Callsign(), "can", sd.LSF.CAN())
						c.lastStreamID = sd.StreamID
					}
					if c.dashLog != nil && sd.LastFrame {
						c.dashLog.Info("", "type", "Internet", "subtype", "Voice End", "src", sd.LSF.Src.Callsign(), "dst", sd.LSF.Dst.Callsign(), "can", sd.LSF.CAN())
						c.lastStreamID = 0xFFFF
					}
				}
			}
		case magicM17Packet: // M17 packet
			if c.packetHandler != nil {
				p := NewPacketFromBytes(buffer[4:])
				c.packetHandler(p)
				if c.dashLog != nil {
					c.dashLog.Info("", "type", "Internet", "subtype", "Packet", "src", p.LSF.Src.Callsign(), "dst", p.LSF.Dst.Callsign(), "can", p.LSF.CAN())
				}
			}
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
		return fmt.Errorf("error sending packet message: %w", err)
	}
	return nil
}

func (c *Relay) SendStream(lsf LSF, sid uint16, fn uint16, payload []byte) error {
	if time.Since(c.lastPing) > 30*time.Second {
		log.Printf("[DEBUG] Last ping was at %s", c.lastPing)
	}
	// log.Printf("[DEBUG] SendStream: LSF: %v, sid: %x, fn: %d", lsf, sid, fn)
	cmd := make([]byte, 0, 54)
	cmd = append(cmd, []byte(magicM17Voice)...)
	cmd, _ = binary.Append(cmd, binary.BigEndian, sid)
	cmd = append(cmd, lsf.ToLSDBytes()...)
	cmd, _ = binary.Append(cmd, binary.BigEndian, fn)
	cmd = append(cmd, payload...)
	crc := CRC(cmd[:52])
	cmd, _ = binary.Append(cmd, binary.BigEndian, crc)

	_, err := c.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending stream message: %w", err)
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

type StreamDatagram struct {
	StreamID    uint16
	FrameNumber uint16
	LastFrame   bool
	LSF         LSF
	Payload     [16]byte
}

func NewStreamDatagram(encodedCallsign [6]byte, buffer []byte) (StreamDatagram, error) {
	sd := StreamDatagram{}
	if len(buffer) != 54 {
		return sd, fmt.Errorf("stream datagram buffer length %d != 50", len(buffer))
	}
	if CRC(buffer) != 0 {
		return sd, fmt.Errorf("bad CRC for stream datagram buffer")
	}
	buffer = buffer[4:]
	_, err := binary.Decode(buffer, binary.BigEndian, &sd.StreamID)
	if err != nil {
		log.Printf("[INFO] Unable to decode streamID from stream datagram: %v", err)
		return sd, fmt.Errorf("bad streamID from stream datagram: %v", err)
	}
	sd.LSF = NewLSFFromLSD(buffer[2:30])
	dst, _ := EncodeCallsign("@ALL")
	sd.LSF.Dst = *dst
	sd.LSF.Type[1] |= 0x2 << 5
	copy(sd.LSF.Meta[:], sd.LSF.Src[:])
	copy(sd.LSF.Meta[6:], encodedCallsign[:])
	sd.LSF.CalcCRC()

	_, err = binary.Decode(buffer[30:], binary.BigEndian, &sd.FrameNumber)
	if err != nil {
		log.Printf("[INFO] Unable to decode frameNumber from stream datagram: %v", err)
		return sd, fmt.Errorf("bad frameNumber from stream datagram: %v", err)
	}
	sd.LastFrame = sd.FrameNumber&0x8000 == 0x8000
	// sd.FrameNumber &= 0x7fff
	copy(sd.Payload[:], buffer[32:48])
	return sd, nil
}
