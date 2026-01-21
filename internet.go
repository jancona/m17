package m17

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

const (
	MagicLen = 4

	MagicACKN      = "ACKN"
	MagicCONN      = "CONN"
	MagicDISC      = "DISC"
	MagicLSTN      = "LSTN"
	MagicNACK      = "NACK"
	MagicPING      = "PING"
	MagicPONG      = "PONG"
	MagicM17Stream = "M17 "
	MagicM17Packet = "M17P"

	maxRetries = 10
)

type InetClient struct {
	Name            string
	Server          string
	Port            uint
	Module          byte
	EncodedName     *EncodedCallsign
	encodedCallsign *EncodedCallsign
	callsign        string
	conn            *net.UDPConn
	connected       bool
	connecting      bool
	pingTimer       *time.Timer
	retryCount      int
	packetHandler   func(Packet) error
	streamHandler   func(StreamDatagram) error
	running         bool
	dashLog         *DashboardLogger
}

func NewInetClient(name string, server string, port uint, module string, callsign string, dashLog *DashboardLogger, packetHandler func(Packet) error, streamHandler func(StreamDatagram) error) (*InetClient, error) {
	cs, err := EncodeCallsign(callsign)
	if err != nil {
		return nil, fmt.Errorf("bad callsign %s: %w", callsign, err)
	}
	// n := NormalizeCallsignModule(name + " " + module)
	// encodedName, err := EncodeCallsign(n)
	// if err != nil {
	// 	return nil, fmt.Errorf("bad name/module %s: %w", n, err)
	// }
	var m byte
	switch {
	case len(module) == 0:
		m = 0
	case len(module) > 1 || module[0] < 'A' || module[0] > 'Z':
		return nil, fmt.Errorf("module must be A-Z or empty, got '%s'", module)
	case len(module) == 1:
		m = []byte(module)[0]
	}
	var r *InetClient
	r = &InetClient{
		Name:   name,
		Server: server,
		Port:   port,
		Module: m,
		// EncodedName:     encodedName,
		callsign:        callsign,
		encodedCallsign: cs,
		packetHandler:   packetHandler,
		streamHandler:   streamHandler,
		dashLog:         dashLog,
		pingTimer: time.AfterFunc(30*time.Second, func() {
			log.Printf("[DEBUG] No PINGs received in > 30 seconds. Disconnected.")
			r.pingTimer.Stop()
			r.connected = false
			r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
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

func (r *InetClient) Connect() error {
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
func (r *InetClient) Close() error {
	log.Print("[DEBUG] InetClient.Close()")
	r.running = false
	r.pingTimer.Stop()
	r.sendDISC()
	r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
	return r.conn.Close()
}

func (r *InetClient) handle() {
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
			log.Printf("[DEBUG] InetClient.Handle(): error reading from UDP: %v", err)
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
		case MagicACKN:
			r.connected = true
			r.connecting = false
			r.dashLog.Log("Reflector", "Connect", "name", r.Name, "module", string(r.Module))
			r.pingTimer.Reset(30 * time.Second)
			log.Printf("[DEBUG] Received ACKN")
		case MagicNACK:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received NACK, disconnecting")
			r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
			// r.done = true
		case MagicDISC:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received DISC, disconnecting")
			r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
			// r.done = true
		case MagicPING:
			r.sendPONG()
			r.pingTimer.Reset(30 * time.Second)
			// case magicINFO:
		case MagicM17Stream: // M17 voice stream
			// log.Printf("[DEBUG] stream buffer: % 2x", buffer)
			if r.streamHandler != nil {
				sd, err := NewStreamDatagramFromBytes(buffer)
				if err != nil {
					log.Printf("[INFO] Dropping bad stream datagram: %v", err)
				} else {
					// log.Printf("[DEBUG] Receive StreamDatagram: %s", sd)
					r.streamHandler(sd)
				}
			}
		case MagicM17Packet: // M17 packet
			if r.packetHandler != nil {
				p := NewPacketFromBytes(buffer[4:])
				// log.Printf("[DEBUG] Received packet from reflector. buffer: % 02x, buffer len: %d, p: %v", buffer[4:], len(buffer[4:]), p)
				r.packetHandler(p)
			}
		}
	}
	r.running = false
}

func (r *InetClient) SendPacket(p Packet) error {
	b := p.ToBytes()
	cmd := make([]byte, 0, MagicLen+len(b))
	cmd = append(cmd, []byte(MagicM17Packet)...)
	cmd = append(cmd, b...)
	// log.Printf("[DEBUG] p: %#v, cmd: %#v", p, cmd)

	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending packet message: %w", err)
	}
	return nil
}

func (r *InetClient) SendStream(sd StreamDatagram) error {
	// log.Printf("[DEBUG] Send StreamDatagram: %s", sd)
	_, err := r.conn.Write(sd.ToBytes())
	if err != nil {
		return fmt.Errorf("error sending stream message: %w", err)
	}
	return nil
}

func (r *InetClient) sendCONN() error {
	cmd := make([]byte, 11)
	copy(cmd, []byte(MagicCONN))
	copy(cmd[4:10], r.encodedCallsign[:])
	cmd[10] = r.Module
	log.Printf("[DEBUG] Sending CONN callsign: %s, module %s, cmd: %#v", r.callsign, string(r.Module), cmd)
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	return nil
}
func (r *InetClient) sendPONG() error {
	// log.Print("[DEBUG] Sending PONG")
	cmd := make([]byte, 10)
	copy(cmd, []byte(MagicPONG))
	copy(cmd[4:10], r.encodedCallsign[:])
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}
func (r *InetClient) sendDISC() error {
	cmd := make([]byte, 10)
	copy(cmd, []byte(MagicDISC))
	copy(cmd[4:10], r.encodedCallsign[:])
	log.Printf("[DEBUG] Sending DISC cmd: %#v", cmd)
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending DISC: %w", err)
	}
	return nil
}

type StreamDatagram struct {
	StreamID    uint16
	FrameNumber uint16
	LastFrame   bool
	LSF         *LSF
	Payload     [16]byte
}

func NewStreamDatagramFromBytes(buffer []byte) (StreamDatagram, error) {
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

func NewStreamDatagram(streamID uint16, frameNumber uint16, lsf *LSF, payload []byte) StreamDatagram {
	sd := StreamDatagram{
		StreamID:    streamID,
		FrameNumber: frameNumber,
		LastFrame:   frameNumber&0x8000 == 0x8000,
		LSF:         lsf,
	}
	copy(sd.Payload[:], payload)
	return sd
}

func (sd StreamDatagram) ToBytes() []byte {
	buf := make([]byte, 0, 54)
	buf = append(buf, []byte(MagicM17Stream)...)
	buf, _ = binary.Append(buf, binary.BigEndian, sd.StreamID)
	buf = append(buf, sd.LSF.ToLSDBytes()...)
	buf, _ = binary.Append(buf, binary.BigEndian, sd.FrameNumber)
	buf = append(buf, sd.Payload[:]...)
	crc := CRC(buf[:52])
	buf, _ = binary.Append(buf, binary.BigEndian, crc)
	return buf
}

func (sd StreamDatagram) String() string {
	return fmt.Sprintf(`{
	StreamID: %04x,
	FrameNumber: %04x,
	LastFrame: %v,
	LSF: %s,
	Payload: [% 2x],
}`, sd.StreamID, sd.FrameNumber, sd.LastFrame, sd.LSF, sd.Payload)
}
