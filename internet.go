package m17

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
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
	magicM17Stream = "M17 "
	magicM17Packet = "M17P"

	maxRetries = 10
)

type Relay struct {
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

func NewRelay(name string, server string, port uint, module string, callsign string, dashLog *DashboardLogger, packetHandler func(Packet) error, streamHandler func(StreamDatagram) error) (*Relay, error) {
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
	var r *Relay
	r = &Relay{
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
	r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
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
			r.dashLog.Log("Reflector", "Connect", "name", r.Name, "module", string(r.Module))
			r.pingTimer.Reset(30 * time.Second)
			log.Printf("[DEBUG] Received ACKN")
		case magicNACK:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received NACK, disconnecting")
			r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
			// r.done = true
		case magicDISC:
			r.connected = false
			r.connecting = false
			log.Print("[INFO] Received DISC, disconnecting")
			r.dashLog.Log("Reflector", "Disconnect", "name", r.Name, "module", string(r.Module))
			// r.done = true
		case magicPING:
			r.sendPONG()
			r.pingTimer.Reset(30 * time.Second)
			// case magicINFO:
		case magicM17Stream: // M17 voice stream
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
		case magicM17Packet: // M17 packet
			if r.packetHandler != nil {
				p := NewPacketFromBytes(buffer[4:])
				// log.Printf("[DEBUG] Received packet from reflector. buffer: % 02x, buffer len: %d, p: %v", buffer[4:], len(buffer[4:]), p)
				r.packetHandler(p)
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

func (r *Relay) SendStream(sd StreamDatagram) error {
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
	copy(cmd[4:10], r.encodedCallsign[:])
	cmd[10] = r.Module
	log.Printf("[DEBUG] Sending CONN callsign: %s, module %s, cmd: %#v", r.callsign, string(r.Module), cmd)
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
	copy(cmd[4:10], r.encodedCallsign[:])
	_, err := r.conn.Write(cmd)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}
func (r *Relay) sendDISC() error {
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicDISC))
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
	buf = append(buf, []byte(magicM17Stream)...)
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

type Server struct {
	Name          string
	InterfaceAddr string
	conn          *net.UDPConn
	modules       map[byte]Module
	running       bool
	mutex         sync.Mutex
	clients       map[string]*client
}

func NewServer(name string, addr string, modules map[byte]Module) *Server {
	s := Server{
		Name:          name,
		InterfaceAddr: addr,
		modules:       modules,
		clients:       map[string]*client{},
	}
	return &s
}
func (s *Server) Start() error {
	udpAddr, err := net.ResolveUDPAddr("udp", s.InterfaceAddr)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve address %s", s.InterfaceAddr)
		return err
	}

	s.conn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Printf("[ERROR] Failed to listen on %v", udpAddr)
		return err
	}
	log.Printf("[INFO] Listening on: %s", s.InterfaceAddr)

	s.handle()

	return nil
}
func (s *Server) handle() {
	log.Print("[INFO] Server is ready")
	for {
		buf := make([]byte, 1024)
		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(*net.OpError); ok && ne.Timeout() {
				continue
			}
			if ne, ok := err.(*net.OpError); ok && ne.Op == "read" && ne.Err.Error() == "use of closed network connection" {
				log.Print("[DEBUG] Socket closed, exiting listen loop.", nil)
				return
			}
			log.Printf("[ERROR] Error reading packet: %v", err)
			continue
		}
		buf = buf[:n]
		if n < magicLen {
			log.Printf("[DEBUG] Ignoring short packet from %s: [% x]", addr, buf)
			continue
		}
		magic := string(buf[:magicLen])
		if magic != magicPING && magic != magicPONG {
			log.Printf("[DEBUG] Received packet from %s: magic: %s, [% x]", addr, magic, buf)
		}
		switch magic {
		case magicACKN:
			// s.recvACKN(buf)
		case magicCONN:
			s.recvConnect(buf, addr, false)
		case magicDISC:
			if n != 10 {
				log.Printf("[INFO] Bad DISC packet length %d, should be 19", n)
			} else {
				c := s.lookupClient(addr)
				if c != nil {
					log.Printf("[INFO] Disconnecting client %s", addr.String())
					c.pongTimer.Stop()
					c.pingTimer.Stop()
					s.removeClient(c)
				}
			}
		case magicLSTN:
			s.recvConnect(buf, addr, true)
		case magicNACK:
			// s.recvNACK(buf)
		case magicPING:
			fallthrough
		case magicPONG:
			if n != 10 {
				log.Printf("[INFO] Bad PING/PONG packet length %d, should be 10", n)
			} else {
				c := s.lookupClient(addr)
				if c != nil {
					// log.Printf("[DEBUG] Received PING/PONG from client %v", *c)
					c.pongTimer.Reset(30 * time.Second)
				}
			}
		case magicM17Stream:
			log.Printf("[DEBUG] Server received stream message: % 2x", buf)
			sd, err := NewStreamDatagramFromBytes(buf)
			if err != nil {
				log.Printf("[INFO] Dropping bad stream datagram: %v", err)
				s.sendNACK(addr)
			} else {
				log.Printf("[DEBUG] Server received StreamDatagram: %s", sd)
				c := s.lookupClient(addr)
				if c != nil {
					err := c.module.HandleStreamDatagram(sd)
					if err != nil {
						log.Printf("[ERROR] Error calling streamHandler: %v", err)
						s.sendNACK(addr)
					}
					// Send the datagram to other clients of the module
					for _, cl := range s.lookupClientsByModule(c.module.Name()) {
						if c != cl {
							s.SendDatagram(&sd, cl.addr)
						}
					}
				}
			}
		case magicM17Packet:
			p := NewPacketFromBytes(buf[4:])
			log.Printf("[DEBUG] Server received packet: %s", p.String())
			c := s.lookupClient(addr)
			if c != nil {
				c.module.HandlePacket(p)
				// Send the packet to other clients of the module, becuase the module won't send it back
				// Should this be here or in the module?
				for _, cl := range s.lookupClientsByModule(c.module.Name()) {
					if c != cl {
						s.SendPacket(&p, cl.addr)
					}
				}
			}
		}
	}
}

func (s *Server) Close() {
	s.conn.Close()
}

func (s *Server) recvConnect(buf []byte, addr *net.UDPAddr, listenOnly bool) {
	if len(buf) != 11 {
		s.sendNACK(addr)
		log.Printf("[INFO] Bad CONN packet length %d, should be 11", len(buf))
		return
	}
	module := s.modules[buf[10]]
	if module == nil {
		s.sendNACK(addr)
		log.Printf("[INFO] Invalid module '%s'", string(buf[10]))
		return
	}
	c := s.newClient(buf[4:10], module, addr, listenOnly)
	log.Printf("[INFO] Connecting client %s", addr.String())
	s.addClient(c)
	s.sendACKN(addr)
}

func (s *Server) sendACKN(addr *net.UDPAddr) error {
	// log.Print("[DEBUG] Sending ACKN")
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicACKN))
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending ACKN: %w", err)
	}
	return nil
}

func (s *Server) sendNACK(addr *net.UDPAddr) error {
	// log.Print("[DEBUG] Sending NACK")
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicNACK))
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending NACK: %w", err)
	}
	return nil
}

func (s *Server) sendPING(encodedCallsign []byte, addr *net.UDPAddr) error {
	// log.Print("[DEBUG] Sending PING")
	cmd := make([]byte, 10)
	copy(cmd, []byte(magicPING))
	copy(cmd[4:10], encodedCallsign)
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}

// func (s *Server) sendDISC(encodedCallsign []byte, addr *net.UDPAddr) error {
// 	cmd := make([]byte, 10)
// 	copy(cmd, []byte(magicDISC))
// 	copy(cmd[4:10], encodedCallsign[:])
// 	log.Printf("[DEBUG] Sending DISC cmd: %#v", cmd)
// 	_, err := s.conn.WriteToUDP(cmd, addr)
// 	if err != nil {
// 		return fmt.Errorf("error sending DISC: %w", err)
// 	}
// 	return nil
// }

func (s *Server) SendPacket(p *Packet, addr *net.UDPAddr) error {
	cmd := []byte("M17P")
	cmd = append(cmd, p.ToBytes()...)
	log.Printf("[DEBUG] Sending Packet: %#v", cmd)
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending DISC: %w", err)
	}
	return nil
}

func (s *Server) SendDatagram(sd *StreamDatagram, addr *net.UDPAddr) error {
	cmd := []byte("M17 ")
	cmd = append(cmd, sd.ToBytes()...)
	log.Printf("[DEBUG] Sending StreamDatagram: %#v to %s", cmd, addr.String())
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending DISC: %w", err)
	}
	return nil
}

type client struct {
	encodedCallsign []byte
	callsign        string
	module          Module
	addr            *net.UDPAddr
	pongTimer       *time.Timer
	pingTimer       *time.Timer
	listenOnly      bool
}

func (s *Server) newClient(callsign []byte, module Module, addr *net.UDPAddr, listenOnly bool) *client {
	var c client
	cs, _ := DecodeCallsign(callsign)
	c = client{
		encodedCallsign: callsign,
		callsign:        cs,
		module:          module,
		addr:            addr,
		listenOnly:      listenOnly,
		pongTimer: time.AfterFunc(30*time.Second, func() {
			log.Printf("[DEBUG] No PONGs received in > 30 seconds. Disconnecting.")
			c.pongTimer.Stop()
			c.pingTimer.Stop()
			s.removeClient(&c)
		}),
		pingTimer: time.AfterFunc(3*time.Second, func() {
			// log.Printf("[DEBUG] Sending PING to %s at %s", c.callsign, c.addr.String())
			s.sendPING(c.encodedCallsign, c.addr)
			c.pingTimer.Reset(3 * time.Second)
		}),
	}
	return &c
}

func (s *Server) addClient(c *client) {
	s.mutex.Lock()
	s.clients[c.addr.String()] = c
	s.mutex.Unlock()
}

func (s *Server) removeClient(c *client) {
	s.mutex.Lock()
	delete(s.clients, c.addr.String())
	s.mutex.Unlock()
}

func (s *Server) lookupClient(addr *net.UDPAddr) *client {
	key := addr.String()
	s.mutex.Lock()
	c := s.clients[key]
	s.mutex.Unlock()
	// log.Printf("[DEBUG] lookupClient(%s): %v", key, c)
	return c
}

func (s *Server) lookupClientsByModule(m byte) []*client {
	ret := []*client{}
	for _, c := range s.clients {
		if c.module.Name() == m {
			ret = append(ret, c)
		}
	}
	return ret
}
