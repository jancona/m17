package server

import (
	"fmt"
	"log"
	"strings"

	bridge "github.com/StalkR/discordgo-bridge"
	"github.com/bwmarrin/discordgo"
	"github.com/jancona/m17"
)

type Module interface {
	Name() byte
	HandlePacket(m17.Packet) error
	HandleStreamDatagram(m17.StreamDatagram) error
}

type DiscordModule struct {
	name    byte
	server  *InetServer
	channel *bridge.Channel
	bot     *bridge.Bot
	session *discordgo.Session
}

func NewDiscordModule(name byte, server *InetServer, channelName string, webhookURL string, botToken string) (*DiscordModule, error) {
	log.Printf("[DEBUG] NewDiscordModule(%s, %s, %s, %s)", string(name), channelName, webhookURL, botToken)
	m := DiscordModule{
		name:   name,
		server: server,
	}
	c := bridge.NewChannel("#"+channelName, webhookURL, m.recvMessage)
	m.channel = c
	m.bot = bridge.NewBot(botToken, c)
	err := m.bot.Start()
	if err != nil {
		log.Printf("[ERROR] Error starting bot: %v", err)
		return nil, err
	}
	m.session, err = discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("error creating session: %v", err)
	}
	err = m.session.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening session: %v", err)
	}
	log.Printf("[DEBUG] NewDiscordModule: %#v", m)
	return &m, nil
}

func (m *DiscordModule) Name() byte {
	return m.name
}

func (m *DiscordModule) HandlePacket(p m17.Packet) error {
	log.Printf("[DEBUG] Received packet: %s", p.String())
	err := m.channel.Send(p.LSF.Src.Callsign(), string(p.Payload))
	if err != nil {
		log.Printf("[ERROR] Unable to send message to Discord: %v", err)
	}
	return err
}
func (m *DiscordModule) HandleStreamDatagram(sd m17.StreamDatagram) error {
	log.Printf("[DEBUG] Ignoring StreamDatagram: %v", sd)
	return nil
}

func (m *DiscordModule) recvMessage(nick string, text string) {
	log.Printf("[DEBUG] Received nick: %s, message: %s", nick, text)
	// member, err := m.session.GuildMembersSearch(m.session.State.Guilds[0].ID, nick, 1)
	// if err != nil {
	// 	log.Printf("[ERROR] GuildMember: %v", err)
	// }
	// log.Printf("[DEBUG] member: %v", member)
	// log.Printf("[DEBUG] member: %v", member[0].DisplayName())
	// nick = strings.ToUpper(member[0].DisplayName())
	nick = strings.ToUpper(nick)
	loc := m17.CallsignRegex.FindStringIndex(nick)
	if loc == nil || loc[1] == 0 {
		log.Printf("[INFO] No callsign found in nick: %s", nick)
		return
	}
	callsign := nick[loc[0]:loc[1]]
	log.Printf("[DEBUG] loc: %v, callsign: %s", loc, callsign)
	msg := append([]byte(text), 0)
	p, err := m17.NewPacket("@ALL", callsign, m17.PacketTypeSMS, msg)
	if err != nil {
		log.Printf("[INFO] Error building packet: %v", err)
		return
	}
	clients := m.server.lookupClientsByModule(m.Name())
	for _, c := range clients {
		m.server.SendPacket(p, c.addr)
	}
}
