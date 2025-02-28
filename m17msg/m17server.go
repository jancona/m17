package main

import (
	"embed"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/data/validation"
	"fyne.io/fyne/v2/widget"
	"github.com/jancona/m17text/m17"
)

const (
	prefM17CallsignKey = "callsign"
	prefM17NameKey     = "name"
	prefM17ServerKey   = "server"
	prefM17PortKey     = "port"
	prefM17ModuleKey   = "module"
)

//go:embed Icon.png
var f embed.FS
var m17IconResource fyne.Resource

func init() {
	m17Icon, _ := f.ReadFile("Icon.png")
	m17IconResource = fyne.NewStaticResource("Icon.png", m17Icon)
}

type m17Server struct {
	ID       string
	app      fyne.App
	callsign string
	name     string
	host     string
	port     uint
	module   string
	relay    *m17.Relay
}

func initM17Server(a fyne.App) service {
	return &m17Server{app: a}
}

func (s *m17Server) configure(u *ui) (fyne.CanvasObject, func(prefix string, a fyne.App)) {
	callsign := widget.NewEntry()
	if s.callsign != "" {
		callsign.Text = s.callsign
	}
	name := widget.NewEntry()
	if s.name != "" {
		name.Text = s.name
	}
	server := widget.NewEntry()
	server.Validator = func(s string) error {
		if net.ParseIP(s) != nil {
			return nil
		}
		matches, _ := regexp.MatchString(`^(?:[_a-z0-9](?:[_a-z0-9-]{0,61}[a-z0-9])?\.)*(?:[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?)?$`, s)
		if matches {
			return nil
		}
		return errors.New("server must be an IP address or domain name")
	}
	port := widget.NewEntry()
	port.PlaceHolder = "17000"
	port.Text = "17000"
	port.Validator = validation.NewRegexp("^[0-9]{1,6}$", "port must be a number")
	module := widget.NewEntry()
	module.Validator = validation.NewRegexp("^[A-Z]{0,1}$", "module must be a capital letter A-Z or empty")
	f := widget.NewForm()
	f.AppendItem(&widget.FormItem{Text: "Callsign", Widget: callsign})
	f.AppendItem(&widget.FormItem{Text: "Name", Widget: name})
	f.AppendItem(&widget.FormItem{Text: "Server", Widget: server})
	f.AppendItem(&widget.FormItem{Text: "Port", Widget: port})
	f.AppendItem(&widget.FormItem{Text: "Module", Widget: module})
	return f,
		func(prefix string, a fyne.App) {
			s.callsign = strings.ToUpper(callsign.Text)
			s.app.Preferences().SetString(prefix+prefM17CallsignKey, s.callsign)
			s.app.Preferences().SetString(prefix+prefM17NameKey, name.Text)
			s.app.Preferences().SetString(prefix+prefM17ServerKey, server.Text)
			p, err := strconv.Atoi(port.Text)
			if err != nil {
				log.Printf("bad port: %v", err)

			}
			s.app.Preferences().SetInt(prefix+prefM17PortKey, p)
			s.app.Preferences().SetString(prefix+prefM17ModuleKey, module.Text)
			err = f.Validate()
			if err != nil {
				log.Printf("validation failed: %v", err)
			} else {
				s.doConnect(name.Text, server.Text, uint(p), module.Text, u)
			}
		}
}

func (s *m17Server) disconnect() {
	if s.relay != nil {
		s.relay.Close()
	}
}

func (s *m17Server) loadChats(u *ui) {
	// for _, s := range u.data.servers {
	// 	id, _ := strconv.Atoi(s.id)
	// 	cs, _ := d.conn.Client.Channels(discapi.GuildID(id))
	// 	for _, c := range cs {
	// 		if c.Type == discapi.GuildCategory || c.Type == discapi.GuildVoice {
	// 			continue // ignore voice and groupings for now
	// 		}

	// 		chn := &channel{id: strconv.Itoa(int(c.ID)), name: "#" + c.Name, server: s}
	// 		if len(s.channels) == 0 {
	// 			chn.messages = d.loadRecentMessages(c.ID)
	// 			if s == u.currentServer {
	// 				u.setChannel(chn)
	// 			}
	// 		}
	// 		s.channels = append(s.channels, chn)
	// 	}
	// }
	u.channels.Refresh()

	// for _, s := range u.data.servers {
	// 	for i, c := range s.channels {
	// 		if i == 0 {
	// 			continue // we did this one above
	// 		}

	// 		id, _ := strconv.Atoi(c.id)
	// 		c.messages = d.loadRecentMessages(discapi.ChannelID(id))
	// 	}
	// }
}

// func (d *m17) loadRecentMessages(id discapi.ChannelID) []*message {
// 	ms, err := d.conn.Client.Messages(id, 15)
// 	if err != nil {
// 		return nil
// 	}

// 	var list []*message
// 	for i := len(ms) - 1; i >= 0; i-- { // newest message is first in response
// 		m := ms[i]
// 		msg := &message{content: m.Content, user: &user{
// 			name:      m.Author.Username,
// 			avatarURL: m.Author.AvatarURL()},
// 		}
// 		list = append(list, msg)
// 	}

// 	return list
// }

func (s *m17Server) send(ch *channel, text string) {
	// s.relay.SendSMS(ch.name, s.callsign, text)
	// Add a trailing NUL
	msg := append([]byte(text), 0)
	p, err := m17.NewPacket(ch.name, s.callsign, m17.PacketTypeSMS, []byte(msg))
	if err != nil {
		fmt.Printf("Error creating Packet: %v\n", err)
		return
	}
	err = s.relay.SendPacket(*p)
	if err != nil {
		fmt.Printf("Error sending message: %v\n", err)
		return
	}

}

type messageEvent struct {
	serverID       string
	channelName    string
	content        string
	sourceCallsign string
}

var m17Handlers []func(ev *messageEvent)

func addHandler(h func(ev *messageEvent)) {
	m17Handlers = append(m17Handlers, h)
}
func (s *m17Server) handleM17(p m17.Packet) error {
	// // A packet is an LSF + type code 0x05 for SMS + data up to 823 bytes
	// log.Printf("[DEBUG] p: %#v", p)
	dst, err := m17.DecodeCallsign(p.LSF.Dst[:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad dst callsign: %v", err)
	}
	src, err := m17.DecodeCallsign(p.LSF.Src[:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad src callsign: %v", err)
	}
	var msg string
	if len(p.Payload) > 0 {
		msg = string(p.Payload[0 : len(p.Payload)-1])
	}
	if p.Type == m17.PacketTypeSMS && (dst == s.callsign || dst == m17.DestinationAll || dst[0:1] == "#") {
		fmt.Printf("%s %s>%s: %s\n", time.Now().Format(time.DateTime), src, dst, msg)
		chName := src
		if strings.HasPrefix(dst, "@") || strings.HasPrefix(dst, "#") {
			chName = dst
		}
		ev := &messageEvent{
			serverID:       s.ID,
			channelName:    chName,
			content:        msg,
			sourceCallsign: src,
		}
		for _, h := range m17Handlers {
			h(ev)
		}
	}
	return nil
}

func (s *m17Server) login(prefix string, u *ui) {
	s.callsign = s.app.Preferences().String(prefix + prefM17CallsignKey)
	name := s.app.Preferences().String(prefix + prefM17NameKey)
	server := s.app.Preferences().String(prefix + prefM17ServerKey)
	port := s.app.Preferences().Int(prefix + prefM17PortKey)
	module := s.app.Preferences().String(prefix + prefM17ModuleKey)
	// migrate to new preferences
	if server == "" {
		server = name
		s.app.Preferences().SetString(prefix+prefM17ServerKey, server)
	}
	s.doConnect(name, server, uint(port), module, u)
}

func (s *m17Server) doConnect(name string, server string, port uint, module string, u *ui) {
	var err error
	log.Printf("Connecting to %s:%d %s, callsign %s", server, port, module, s.callsign)
	s.relay, err = m17.NewRelay(server, port, module, s.callsign, s.handleM17)
	if err != nil {
		log.Printf("fail to connect create client: %v", err)
	}
	err = s.relay.Connect()
	if err != nil {
		fmt.Printf("Error connecting to %s:%d %s: %v", server, port, module, err)
	}
	go func() {
		s.relay.Handle()
		// When Handle exits, we're done
		// os.Exit(0)
	}()
	s.name = name
	s.host = server
	s.port = port
	s.module = module
	s.ID = s.name + " " + s.module
	// s.name = fmt.Sprintf("%s:%d %s", s.host, s.port, s.module)
	s.loadServers(u)

}

func (s *m17Server) loadServers(u *ui) {
	server := &server{service: s, name: s.name, id: s.ID, iconResource: m17IconResource}

	if u.data == nil {
		u.data = &appData{}
	}
	u.data.servers = append(u.data.servers, server)
	if len(u.data.servers) > 0 {
		u.currentServer = u.data.servers[0]
		u.servers.Select(0)
	}
	u.servers.Refresh()

	addHandler(func(ev *messageEvent) {
		if ev.serverID != server.id {
			return
		}
		// log.Printf("displaying ev: %#v for server: %#v", *ev, server)
		ch := findChan(u.data, ev.serverID, ev.channelName)
		if ch == nil {
			// log.Println("Could not find channel for incoming message")
			// return
			ch = &channel{id: ev.channelName, name: ev.channelName, server: server}
			s.addChannel(server, ch, u)
		}

		msg := &message{
			content: ev.content,
			user: &user{
				name: ev.sourceCallsign,
			},
		}
		ch.messages = append(ch.messages, msg)
		if u.currentChannel == nil {
			u.currentChannel = ch
		}
		if ch == u.currentChannel {
			u.appendMessages([]*message{msg})
		}
	})

	s.loadChannels(u)
}
func (s *m17Server) loadChannels(u *ui) {
	// for _, s := range u.data.servers {
	// 	id, _ := strconv.Atoi(s.id)
	// 	cs, _ := d.conn.Client.Channels(discapi.GuildID(id))
	// 	for _, c := range cs {
	// 		if c.Type == discapi.GuildCategory || c.Type == discapi.GuildVoice {
	// 			continue // ignore voice and groupings for now
	// 		}

	// 		chn := &channel{id: strconv.Itoa(int(c.ID)), name: "#" + c.Name, server: s}
	// 		if len(s.channels) == 0 {
	// 			chn.messages = d.loadRecentMessages(c.ID)
	// 			if s == u.currentServer {
	// 				u.setChannel(chn)
	// 			}
	// 		}
	// 		s.channels = append(s.channels, chn)
	// 	}
	// }
	u.channels.Refresh()

	// for _, s := range u.data.servers {
	// 	for i, c := range s.channels {
	// 		if i == 0 {
	// 			continue // we did this one above
	// 		}

	// 		id, _ := strconv.Atoi(c.id)
	// 		c.messages = d.loadRecentMessages(discapi.ChannelID(id))
	// 	}
	// }
}

func (s *m17Server) addChannel(sv *server, c *channel, u *ui) {
	sv.channels = append(sv.channels, c)
	u.channels.Refresh()
}
