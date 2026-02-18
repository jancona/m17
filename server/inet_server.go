package server

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/jancona/m17"
)

type InetServer struct {
	Name          string
	InterfaceAddr string
	conn          *net.UDPConn
	modules       map[byte]Module
	running       bool
	mutex         sync.Mutex
	clients       map[string]*client
}

func NewInetServer(name string, addr string, modules map[byte]Module) *InetServer {
	s := InetServer{
		Name:          name,
		InterfaceAddr: addr,
		modules:       modules,
		clients:       map[string]*client{},
	}
	return &s
}
func (s *InetServer) Start() error {
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
func (s *InetServer) handle() {
	log.Print("[INFO] InetServer is ready")
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
		if n < m17.MagicLen {
			log.Printf("[DEBUG] Ignoring short packet from %s: [% x]", addr, buf)
			continue
		}
		magic := string(buf[:m17.MagicLen])
		if magic != m17.MagicPING && magic != m17.MagicPONG {
			log.Printf("[DEBUG] Received packet from %s: magic: %s, [% x]", addr, magic, buf)
		}
		switch magic {
		case m17.MagicACKN:
			// s.recvACKN(buf)
		case m17.MagicCONN:
			s.recvConnect(buf, addr, false)
		case m17.MagicDISC:
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
		case m17.MagicLSTN:
			s.recvConnect(buf, addr, true)
		case m17.MagicNACK:
			// s.recvNACK(buf)
		case m17.MagicPING:
			fallthrough
		case m17.MagicPONG:
			if n != 10 {
				log.Printf("[INFO] Bad PING/PONG packet length %d, should be 10", n)
			} else {
				c := s.lookupClient(addr)
				if c != nil {
					// log.Printf("[DEBUG] Received PING/PONG from client %v", *c)
					c.pongTimer.Reset(30 * time.Second)
				}
			}
		case m17.MagicM17Stream:
			log.Printf("[DEBUG] InetServer received stream message: % 2x", buf)
			sd, err := m17.NewStreamDatagramFromBytes(buf)
			if err != nil {
				log.Printf("[INFO] Dropping bad stream datagram: %v", err)
				s.sendNACK(addr)
			} else {
				log.Printf("[DEBUG] InetServer received StreamDatagram: %s", sd)
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
		case m17.MagicM17Packet:
			p := m17.NewPacketFromBytes(buf[4:])
			log.Printf("[DEBUG] InetServer received packet: %s", p.String())
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

func (s *InetServer) Close() {
	s.conn.Close()
}

func (s *InetServer) recvConnect(buf []byte, addr *net.UDPAddr, listenOnly bool) {
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

func (s *InetServer) sendACKN(addr *net.UDPAddr) error {
	// log.Print("[DEBUG] Sending ACKN")
	cmd := make([]byte, 10)
	copy(cmd, []byte(m17.MagicACKN))
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending ACKN: %w", err)
	}
	return nil
}

func (s *InetServer) sendNACK(addr *net.UDPAddr) error {
	// log.Print("[DEBUG] Sending NACK")
	cmd := make([]byte, 10)
	copy(cmd, []byte(m17.MagicNACK))
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending NACK: %w", err)
	}
	return nil
}

func (s *InetServer) sendPING(encodedCallsign []byte, addr *net.UDPAddr) error {
	// log.Print("[DEBUG] Sending PING")
	cmd := make([]byte, 10)
	copy(cmd, []byte(m17.MagicPING))
	copy(cmd[4:10], encodedCallsign)
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending PONG: %w", err)
	}
	return nil
}

// func (s *InetServer) sendDISC(encodedCallsign []byte, addr *net.UDPAddr) error {
// 	cmd := make([]byte, 10)
// 	copy(cmd, []byte(m17.MagicDISC))
// 	copy(cmd[4:10], encodedCallsign[:])
// 	log.Printf("[DEBUG] Sending DISC cmd: %#v", cmd)
// 	_, err := s.conn.WriteToUDP(cmd, addr)
// 	if err != nil {
// 		return fmt.Errorf("error sending DISC: %w", err)
// 	}
// 	return nil
// }

func (s *InetServer) SendPacket(p *m17.Packet, addr *net.UDPAddr) error {
	cmd := []byte("M17P")
	cmd = append(cmd, p.ToBytes()...)
	log.Printf("[DEBUG] Sending Packet: %#v", cmd)
	_, err := s.conn.WriteToUDP(cmd, addr)
	if err != nil {
		return fmt.Errorf("error sending DISC: %w", err)
	}
	return nil
}

func (s *InetServer) SendDatagram(sd *m17.StreamDatagram, addr *net.UDPAddr) error {
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

func (s *InetServer) newClient(callsign []byte, module Module, addr *net.UDPAddr, listenOnly bool) *client {
	var c client
	cs, _ := m17.DecodeCallsign(callsign)
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

func (s *InetServer) addClient(c *client) {
	s.mutex.Lock()
	s.clients[c.addr.String()] = c
	s.mutex.Unlock()
}

func (s *InetServer) removeClient(c *client) {
	s.mutex.Lock()
	delete(s.clients, c.addr.String())
	s.mutex.Unlock()
}

func (s *InetServer) lookupClient(addr *net.UDPAddr) *client {
	key := addr.String()
	s.mutex.Lock()
	c := s.clients[key]
	s.mutex.Unlock()
	// log.Printf("[DEBUG] lookupClient(%s): %v", key, c)
	return c
}

func (s *InetServer) lookupClientsByModule(m byte) []*client {
	ret := []*client{}
	for _, c := range s.clients {
		if c.module.Name() == m {
			ret = append(ret, c)
		}
	}
	return ret
}
