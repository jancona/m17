package server

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"

	"github.com/jancona/m17"
)

type IRCModule struct {
	name           byte
	server         *InetServer
	serverName     string
	port           uint
	useTLS         bool
	serverPassword string
	users          map[string]*ircUser
}
type ircUser struct {
	conn    ircevent.Connection
	lastGet time.Time
}

func NewIRCModule(name byte, server *InetServer, serverName string, port uint, useTLS bool, serverPassword string) (*IRCModule, error) {
	log.Printf("[DEBUG] NewIRCModule(%s, %s, %d, %v)", string(name), serverName, port, useTLS)
	m := IRCModule{
		name:           name,
		server:         server,
		serverName:     serverName,
		port:           port,
		useTLS:         useTLS,
		serverPassword: serverPassword,
		users:          map[string]*ircUser{},
	}
	return &m, nil
}

func (m *IRCModule) Name() byte {
	return m.name
}

func (m *IRCModule) HandlePacket(p m17.Packet) error {
	log.Printf("[DEBUG] Received packet: %s", p.String())
	u := m.getIRCUser(p.LSF.Src.Callsign())
	if u != nil {
		msg := strings.ReplaceAll(string(p.Payload), "\x00", "")
		err := u.conn.Privmsg(p.LSF.Dst.Callsign(), msg)
		if err != nil {
			log.Printf("[ERROR] Unable to send message: %v", err)
		}
	}
	return nil
}
func (m *IRCModule) HandleStreamDatagram(sd m17.StreamDatagram) error {
	log.Printf("[DEBUG] Ignoring StreamDatagram: %v", sd)
	return nil
}

func (m *IRCModule) getIRCUser(callsign string) *ircUser {
	u, ok := m.users[callsign]
	if !ok {
		u = &ircUser{
			conn: ircevent.Connection{
				Server:   m.serverName + ":" + strconv.Itoa(int(m.port)),
				UseTLS:   m.useTLS,
				Nick:     callsign,
				Debug:    true,
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
			clients := m.server.lookupClientsByModule(m.Name())
			for _, c := range clients {
				m.server.SendPacket(p, c.addr)
			}
		})
		err := u.conn.Connect()
		if err != nil {
			log.Printf("[INFO] Failed to connect to ircd, dropping message: %v", err)
			return nil
		}
		go u.conn.Loop()
		m.users[callsign] = u
	}
	u.lastGet = time.Now()
	return u
}
