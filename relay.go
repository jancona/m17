package m17

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"time"
)

const (
	magicLen = 4

	magicACKN = "ACKN"
	magicCONN = "CONN"
	magicDISC = "DISC"
	// magicLSTN      = "LSTN"
	magicNACK      = "NACK"
	magicPING      = "PING"
	magicPONG      = "PONG"
	magicM17Voice  = "M17 "
	magicM17Packet = "M17P"

	maxRetries = 10
)

var callsignAll, _ = EncodeCallsign("@ALL")

type Relay struct {
	Name            string
	Server          string
	Port            uint
	Module          byte
	EncodedCallsign [6]byte
	Callsign        string
	conn            *net.UDPConn
	connected       bool
	connecting      bool
	pingTimer       *time.Timer
	retryCount      int
	packetHandler   func(Packet) error
	streamHandler   func(StreamDatagram) error
	running         bool
	dashLog         *slog.Logger
	lastStreamID    uint16
	lastLogTime     time.Time
	lastFrameTimer  *time.Timer
	lastLSF         *LSF // Workaround for reflectors that change the SRC during the stream
}

func NewRelay(name string, server string, port uint, module string, callsign string, dashLog *slog.Logger, packetHandler func(Packet) error, streamHandler func(StreamDatagram) error) (*Relay, error) {
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
	var r *Relay
	r = &Relay{
		Name:            name,
		Server:          server,
		Port:            port,
		Module:          m,
		Callsign:        callsign,
		EncodedCallsign: *cs,
		packetHandler:   packetHandler,
		streamHandler:   streamHandler,
		dashLog:         dashLog,
		lastStreamID:    0xFFFF,
		pingTimer: time.AfterFunc(30*time.Second, func() {
			log.Printf("[DEBUG] No PINGs received in > 30 seconds. Disconnected.")
			r.pingTimer.Stop()
			r.connected = false
			if r.dashLog != nil {
				r.dashLog.Info("", "type", "Reflector", "subtype", "Disconnect", "name", r.Name, "module", string(r.Module))
			}
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

func (r *Relay) Connect() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", r.Server, r.Port))
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	// Dial UDP connection to relay/reflector
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
func (r *Relay) Close() error {
	log.Print("[DEBUG] Relay.Close()")
	r.running = false
	r.pingTimer.Stop()
	r.sendDISC()
	if r.dashLog != nil {
		r.dashLog.Info("", "type", "Reflector", "subtype", "Disconnect", "name", r.Name, "module", string(r.Module))
	}
	return r.conn.Close()
}

func (r *Relay) handle() {
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
			log.Printf("[DEBUG] Relay.Handle(): error reading from UDP: %v", err)
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
		case magicACKN:
			r.connected = true
			r.connecting = false
			if r.dashLog != nil {
				r.dashLog.Info("", "type", "Reflector", "subtype", "Connect", "name", r.Name, "module", string(r.Module))
			}
			r.pingTimer.Reset(30 * time.Second)
			log.Printf("[DEBUG] Received ACKN")
		case magicNACK:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received NACK, disconnecting")
			if r.dashLog != nil {
				r.dashLog.Info("", "type", "Reflector", "subtype", "Disconnect", "name", r.Name, "module", string(r.Module))
			}
			// r.done = true
		case magicDISC:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received DISC, disconnecting")
			if r.dashLog != nil {
				r.dashLog.Info("", "type", "Reflector", "subtype", "Disconnect", "name", r.Name, "module", string(r.Module))
			}
			// r.done = true
		case magicPING:
			r.sendPONG()
			r.pingTimer.Reset(30 * time.Second)
			// case magicINFO:
		case magicM17Voice: // M17 voice stream
			// log.Printf("[DEBUG] stream buffer: % 2x", buffer)
			if r.streamHandler != nil {
				sd, err := NewStreamDatagramFromBytes(buffer)
				if err != nil {
					log.Printf("[INFO] Dropping bad stream datagram: %v", err)
				} else {
					// log.Printf("[DEBUG] Receive StreamDatagram: %s", sd)
					gnss := sd.LSF.GNSS()
					sd.LSF.Dst = *callsignAll
					// Add Extended Callsign Data
					sd.LSF.Type[1] &= 0x9F     // zero out Encryption Subtype
					sd.LSF.Type[1] |= 0x2 << 5 // Set it to ECS
					copy(sd.LSF.Meta[:], sd.LSF.Src[:])
					copy(sd.LSF.Meta[6:], r.EncodedCallsign[:])
					sd.LSF.Meta[12] = 0
					sd.LSF.Meta[13] = 0
					sd.LSF.CalcCRC()
					// log.Printf("[DEBUG] Handle StreamDatagram id: %04x, fn: %04x, last: %v", sd.StreamID, sd.FrameNumber, sd.LastFrame)
					r.streamHandler(sd)
					if r.lastFrameTimer != nil {
						r.lastFrameTimer.Reset(time.Second)
					}
					if r.dashLog != nil && r.lastStreamID != sd.StreamID {
						if r.lastFrameTimer != nil {
							// Should never happen
							r.lastFrameTimer.Stop()
						}
						log.Printf("[DEBUG] Start Internet voice stream: %s", sd)
						r.dashLog.Info("", "type", "Internet", "subtype", "Voice Start", "src", sd.LSF.Src.Callsign(), "dst", sd.LSF.Dst.Callsign(), "can", sd.LSF.CAN())
						r.lastStreamID = sd.StreamID
						r.lastLSF = sd.LSF
						// Provide a backstop if we don't receive a last frame packet
						r.lastFrameTimer = time.AfterFunc(time.Second, func() {
							log.Printf("[DEBUG] Timed out Internet voice stream %04x", sd.StreamID)
							r.dashLog.Info("", "type", "Internet", "subtype", "Voice End", "src", sd.LSF.Src.Callsign(), "dst", sd.LSF.Dst.Callsign(), "can", sd.LSF.CAN())
							r.lastStreamID = 0xFFFF
							r.lastFrameTimer = nil
							r.lastLSF = nil
						})
					}
					if r.dashLog != nil && gnss != nil && gnss.ValidAltitude && time.Since(r.lastLogTime) > 15*time.Second {
						r.lastLogTime = time.Now()
						args := []any{
							"type", "Internet",
							"subtype", "GNSS",
							"dataSource", gnss.DataSource,
							"stationType", gnss.StationType,
							"src", sd.LSF.Src.Callsign(),
							"latitude", json.Number(fmt.Sprintf("%f", gnss.Latitude)),
							"longitude", json.Number(fmt.Sprintf("%f", gnss.Longitude)),
						}
						if gnss.ValidAltitude {
							args = append(args,
								"altitude", json.Number(fmt.Sprintf("%.1f", gnss.Altitude)),
							)
						}
						if gnss.ValidBearingSpeed {
							args = append(args,
								"speed", json.Number(fmt.Sprintf("%.1f", gnss.Speed)),
								"bearing", gnss.Bearing,
							)
						}
						if gnss.ValidRadius {
							args = append(args,
								"radius", gnss.Radius,
							)
						}
						r.dashLog.Info("", args...)
					}
					if r.dashLog != nil && sd.LastFrame {
						log.Printf("[DEBUG] End Internet voice stream: %s", sd)
						r.dashLog.Info("", "type", "Internet", "subtype", "Voice End", "src", r.lastLSF.Src.Callsign(), "dst", r.lastLSF.Dst.Callsign(), "can", r.lastLSF.CAN())
						r.lastStreamID = 0xFFFF
						r.lastLSF = nil
						r.lastFrameTimer.Stop()
						r.lastFrameTimer = nil
					}
				}
			}
		case magicM17Packet: // M17 packet
			if r.packetHandler != nil {
				p := NewPacketFromBytes(buffer[4:])
				// log.Printf("[DEBUG] Received packet from reflector. buffer: % 02x, buffer len: %d, p: %v", buffer[4:], len(buffer[4:]), p)
				r.packetHandler(p)
				if r.dashLog != nil {
					if p.Type == PacketTypeSMS && len(p.Payload) > 0 {
						msg := string(p.Payload[0 : len(p.Payload)-1])
						r.dashLog.Info("", "type", "Internet", "subtype", "Packet", "src", p.LSF.Src.Callsign(), "dst", p.LSF.Dst.Callsign(), "can", p.LSF.CAN(), "packetType", p.Type, "smsMessage", msg)
					} else {
						r.dashLog.Info("", "type", "Internet", "subtype", "Packet", "src", p.LSF.Src.Callsign(), "dst", p.LSF.Dst.Callsign(), "can", p.LSF.CAN(), "packetType", p.Type)
					}
				}
			}
		}
	}
	r.running = false
}

func (r *Relay) SendPacket(p Packet) error {
	b := p.ToBytes()
	cmd := make([]byte, 0, magicLen+len(b))
	cmd = append(cmd, []byte(magicM17Packet)...)
	cmd = append(cmd, b...)
	// log.Printf("[DEBUG] p: %#v, cmd: %#v", p, cmd)

	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending packet message: %w", err)
	}
	return nil
}

func (r *Relay) SendStream(lsf *LSF, sid uint16, fn uint16, payload []byte) error {
	sd := NewStreamDatagram(sid, fn, lsf, payload)
	// log.Printf("[DEBUG] Send StreamDatagram: %s", sd)
	_, err := r.conn.Write(sd.ToBytes())
	if err != nil {
		return fmt.Errorf("error sending stream message: %w", err)
	}
	return nil
}

func (r *Relay) sendCONN() error {
	cmd := make([]byte, 11)
	copy(cmd, []byte(magicCONN))
	copy(cmd[4:10], r.EncodedCallsign[:])
	cmd[10] = r.Module
	log.Printf("[DEBUG] Sending CONN callsign: %s, module %s, cmd: %#v", r.Callsign, string(r.Module), cmd)
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending CONN: %w", err)
	}
	return nil
}
func (r *Relay) sendPONG() error {
	// log.Print("[DEBUG] Sending PONG")
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicPONG))
	copy(cmd[4:10], r.EncodedCallsign[:])
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}
func (r *Relay) sendDISC() error {
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicDISC))
	copy(cmd[4:10], r.EncodedCallsign[:])
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
	buf = append(buf, []byte(magicM17Voice)...)
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
