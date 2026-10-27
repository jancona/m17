package server

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jancona/m17/inet"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"

	"github.com/jancona/m17"
)

const (
	// Each M17 source callsign gets its own IRC connection. These bound
	// how many a stream of packets with made-up callsigns can open.
	maxIRCUsers    = 50
	ircIdleTimeout = 30 * time.Minute
	// Packets wait here while a connection to the ircd is made, so the
	// reflector's receive loop never does.
	ircQueueLen = 64
)

type IRCModule struct {
	name           byte
	server         *inet.Server
	serverName     string
	port           uint
	useTLS         bool
	serverPassword string
	queue          chan m17.Packet

	mu    sync.Mutex // protects users
	users map[string]*ircUser
}
type ircUser struct {
	conn    ircevent.Connection
	lastGet time.Time
}

func NewIRCModule(name byte, server *inet.Server, serverName string, port uint, useTLS bool, serverPassword string) (*IRCModule, error) {
	log.Printf("[DEBUG] NewIRCModule(%s, %s, %d, %v)", string(name), serverName, port, useTLS)
	m := IRCModule{
		name:           name,
		server:         server,
		serverName:     serverName,
		port:           port,
		useTLS:         useTLS,
		serverPassword: serverPassword,
		queue:          make(chan m17.Packet, ircQueueLen),
		users:          map[string]*ircUser{},
	}
	go m.sendLoop()
	go m.idleUserLoop()
	return &m, nil
}

func (m *IRCModule) Name() byte {
	return m.name
}

// HandlePacket queues p to be sent to IRC. It runs on the reflector's
// receive loop, so it never waits: when the queue is full, p is dropped.
func (m *IRCModule) HandlePacket(p m17.Packet) error {
	log.Printf("[DEBUG] Received packet: %s", p.String())
	select {
	case m.queue <- p:
	default:
		log.Printf("[INFO] IRC queue full, dropping packet from %s", p.LSF.Src.Callsign())
	}
	return nil
}

func (m *IRCModule) sendLoop() {
	for p := range m.queue {
		m.sendPacket(p)
	}
}

func (m *IRCModule) sendPacket(p m17.Packet) {
	dst := p.LSF.Dst.Callsign()
	if !ircTargetAllowed(dst) {
		log.Printf("[INFO] Not sending message from %s to IRC services %s", p.LSF.Src.Callsign(), dst)
		return
	}
	u := m.getIRCUser(p.LSF.Src.Callsign())
	if u != nil {
		msg := strings.ReplaceAll(string(p.Payload), "\x00", "")
		err := u.conn.Privmsg(dst, msg)
		if err != nil {
			log.Printf("[ERROR] Unable to send message: %v", err)
		}
	}
}

// ircTargetAllowed reports whether an M17 destination may be messaged on
// IRC. The message is sent from the source callsign's nick, so messaging
// NickServ and the like would let anyone on M17 run service commands
// (REGISTER, SET, ...) as that callsign.
func ircTargetAllowed(dst string) bool {
	return !strings.HasSuffix(strings.ToUpper(dst), "SERV")
}

func (m *IRCModule) HandleStreamDatagram(sd m17.StreamDatagram) error {
	log.Printf("[DEBUG] Ignoring StreamDatagram: %v", sd)
	return nil
}

func (m *IRCModule) getIRCUser(callsign string) *ircUser {
	m.mu.Lock()
	u, ok := m.users[callsign]
	n := len(m.users)
	if ok {
		u.lastGet = time.Now()
	}
	m.mu.Unlock()
	if ok {
		return u
	}
	if n >= maxIRCUsers {
		log.Printf("[INFO] %d IRC connections open, dropping message from %s", n, callsign)
		return nil
	}
	u = &ircUser{
		conn: ircevent.Connection{
			Server: m.serverName + ":" + strconv.Itoa(int(m.port)),
			UseTLS: m.useTLS,
			Nick:   callsign,
			// Debug logs every line sent, the server password included
			Debug:    false,
			Password: m.serverPassword,
			// RequestCaps: []string{"server-time", "message-tags"},
		},
	}
	// u.conn.AddConnectCallback(func(e ircmsg.Message) {})
	// u.conn.AddCallback("JOIN", func(e ircmsg.Message) {}) // TODO try to rejoin if we *don't* get this
	u.conn.AddCallback("PRIVMSG", func(e ircmsg.Message) {
		log.Printf("[DEBUG] PRIVMSG callback: %#v", e)
		if len(e.Params) < 2 {
			return
		}
		text := e.Params[1]
		srcNick := strings.ToUpper(e.Source)
		loc := m17.CallsignRegex.FindStringIndex(srcNick)
		if loc == nil || loc[1] == 0 {
			log.Printf("[INFO] No callsign found in nick: %s", srcNick)
			return
		}
		srcCallsign := srcNick[loc[0]:loc[1]]
		log.Printf("[DEBUG] loc: %v, callsign: %s", loc, srcCallsign)
		msg := append([]byte(text), 0)
		p, err := m17.NewPacket(callsign, srcCallsign, m17.PacketTypeSMS, msg)
		if err != nil {
			log.Printf("[INFO] Error building packet: %v", err)
			return
		}
		m.server.SendPacketToModule(m.Name(), p)
	})
	err := u.conn.Connect()
	if err != nil {
		log.Printf("[INFO] Failed to connect to ircd, dropping message: %v", err)
		return nil
	}
	go u.conn.Loop()
	u.lastGet = time.Now()
	m.mu.Lock()
	m.users[callsign] = u
	m.mu.Unlock()
	return u
}

// idleUserLoop closes the IRC connections of callsigns that have sent
// nothing for ircIdleTimeout. It runs as a background goroutine.
func (m *IRCModule) idleUserLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.removeIdleUsers(time.Now().Add(-ircIdleTimeout))
	}
}

// removeIdleUsers forgets the users last used before cutoff and quits
// their connections. It returns how many it removed.
func (m *IRCModule) removeIdleUsers(cutoff time.Time) int {
	var idle []*ircUser
	m.mu.Lock()
	for cs, u := range m.users {
		if u.lastGet.Before(cutoff) {
			log.Printf("[INFO] Closing idle IRC connection for %s", cs)
			idle = append(idle, u)
			delete(m.users, cs)
		}
	}
	m.mu.Unlock()
	for _, u := range idle {
		u.conn.Quit()
	}
	return len(idle)
}
