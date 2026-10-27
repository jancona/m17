package server

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jancona/m17/inet"

	fap "github.com/hessu/go-aprs-fap"

	"github.com/jancona/m17"
)

const (
	clientDefinedFilterPort = ":14580"
	deviceID                = "APZ001" // Experimental
	mpsToKmh                = 3.6
	readTimeout             = 5 * time.Minute
	reconnectDelay          = 10 * time.Second
)

type APRSModule struct {
	name         byte
	server       *inet.Server
	serverName   string
	callsign     string
	passcode     string
	aprsSymbol   string
	staleTimeout time.Duration

	mu    sync.Mutex // protects conn and users
	conn  *fap.Conn
	users map[string]*aprsUser
}

type aprsUser struct {
	aprsCallsign       string
	lastPositionReport time.Time
	lastHeard          time.Time
}

func NewAPRSModule(name byte, server *inet.Server, serverName string, callsign string, aprsSymbol string, staleTimeout time.Duration) (*APRSModule, error) {
	log.Printf("[DEBUG] NewAPRSModule(%s, %s, %s)", string(name), serverName, callsign)
	if len(aprsSymbol) != 2 {
		return nil, fmt.Errorf("Bad APRS symbol '%s'", aprsSymbol)
	}
	if callsign == "" {
		return nil, fmt.Errorf("APRS module requires a Callsign")
	}
	passcode := fmt.Sprintf("%d", fap.AprsPasscode(callsign))
	m := &APRSModule{
		name:         name,
		server:       server,
		serverName:   serverName,
		callsign:     callsign,
		passcode:     passcode,
		aprsSymbol:   aprsSymbol,
		staleTimeout: staleTimeout,
		users:        map[string]*aprsUser{},
	}
	if err := m.connect(); err != nil {
		return nil, fmt.Errorf("connecting to APRS-IS: %w", err)
	}
	go m.readLoop()
	if staleTimeout > 0 {
		go m.staleUserLoop()
	}
	return m, nil
}

func (m *APRSModule) Name() byte {
	return m.name
}

// connect dials the APRS-IS server with a filter for all known users.
// Must be called with m.mu held.
func (m *APRSModule) connect() error {
	filter := m.buildFilter()
	log.Printf("[DEBUG] Calling Dial(%s, %s, m17-bridge, 0.1, %s)", m.serverName+clientDefinedFilterPort, m.callsign, filter)
	conn, err := fap.Dial(m.serverName+clientDefinedFilterPort, m.callsign, m.passcode, "m17-bridge", "0.1", filter)
	if err != nil {
		return err
	}
	m.conn = conn
	return nil
}

// buildFilter constructs a g/ filter string covering all known users.
func (m *APRSModule) buildFilter() string {
	if len(m.users) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.users))
	for _, u := range m.users {
		ac := u.aprsCallsign
		if !strings.Contains(ac, "-") {
			ac += "*"
		}
		parts = append(parts, ac)
	}
	return "g/" + strings.Join(parts, "/")
}

// updateFilter sends a #filter command to update the server-side filter
// for the current connection. Must be called with m.mu held.
func (m *APRSModule) updateFilter() {
	if m.conn == nil {
		return
	}
	filter := m.buildFilter()
	if filter == "" {
		return
	}
	line := "#filter " + filter
	log.Printf("[DEBUG] Updating APRS-IS filter: %s", line)
	if err := m.conn.SendLine(line); err != nil {
		log.Printf("[INFO] Unable to update APRS-IS filter: %v", err)
	}
}

// sendLine sends a line on the APRS-IS connection.
// Acquires m.mu internally.
func (m *APRSModule) sendLine(line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return fmt.Errorf("APRS-IS connection not available")
	}
	return m.conn.SendLine(line)
}

func (m *APRSModule) readLoop() {
	for {
		m.mu.Lock()
		conn := m.conn
		m.mu.Unlock()
		if conn == nil {
			time.Sleep(reconnectDelay)
			m.mu.Lock()
			err := m.connect()
			m.mu.Unlock()
			if err != nil {
				log.Printf("[INFO] Unable to reconnect to APRS-IS: %v", err)
				continue
			}
			m.mu.Lock()
			conn = m.conn
			m.mu.Unlock()
		}
		for {
			raw, err := conn.ReadPacket(readTimeout)
			if err != nil {
				log.Printf("[DEBUG] ReadPacket error: %v", err)
				m.mu.Lock()
				conn.Close()
				m.conn = nil
				m.mu.Unlock()
				break
			}
			log.Printf("[DEBUG] received packet: %s", raw)
			pkt, err := fap.Parse(raw)
			if err != nil {
				log.Printf("[DEBUG] Parse error: %v", err)
				continue
			}
			if pkt.Type != fap.PacketTypeMessage || pkt.Message == nil {
				continue
			}
			m.handleAPRSMessage(pkt)
		}
		log.Printf("[DEBUG] Reconnecting APRS-IS")
		time.Sleep(reconnectDelay)
		m.mu.Lock()
		err := m.connect()
		m.mu.Unlock()
		if err != nil {
			log.Printf("[INFO] Unable to reconnect to APRS-IS: %v", err)
		}
	}
}

func (m *APRSModule) handleAPRSMessage(pkt *fap.Packet) {
	msg := pkt.Message
	dst := msg.Destination
	log.Printf("[DEBUG] dst: %s, msgText: %s, msgID: %s", dst, msg.Text, msg.ID)
	// Find which M17 callsign this APRS destination maps to
	m.mu.Lock()
	var m17Callsign string
	for cs, u := range m.users {
		if u.aprsCallsign == dst {
			m17Callsign = cs
			break
		}
	}
	m.mu.Unlock()
	if m17Callsign == "" {
		// Not ours, so not ours to ack: an ack tells the sender to stop
		// retrying, and the message would be lost.
		log.Printf("[DEBUG] No M17 user found for APRS destination %s", dst)
		return
	}
	if msg.ID != "" {
		// Send ack using the destination callsign as the source
		ackFrame := fmt.Sprintf("%s>%s::%-9s:ack%s", dst, deviceID, pkt.SrcCallsign, msg.ID)
		log.Printf("[DEBUG] Sending ack frame: '%s'", ackFrame)
		if err := m.sendLine(ackFrame); err != nil {
			log.Printf("[INFO] Unable to send ack: %v", err)
		}
	}
	msgBytes := append([]byte(msg.Text), 0)
	p, err := m17.NewPacket(m17Callsign, strings.ReplaceAll(pkt.SrcCallsign, "-", " "), m17.PacketTypeSMS, msgBytes)
	if err != nil {
		log.Printf("[INFO] Error building packet: %v", err)
		return
	}
	m.server.SendPacketToModule(m.Name(), p)
}

// getOrAddUser returns the aprsUser for the given M17 callsign,
// creating one and updating the APRS-IS filter if needed.
// It always updates lastHeard to the current time. It returns nil for a
// callsign that is not a valid APRS-IS source.
func (m *APRSModule) getOrAddUser(callsign string) *aprsUser {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[callsign]
	if !ok {
		ac := aprsCallsign(callsign)
		if !validAPRSCallsign(ac) {
			log.Printf("[INFO] Ignoring M17 callsign %q: %q is not a valid APRS callsign", callsign, ac)
			return nil
		}
		u = &aprsUser{
			aprsCallsign: ac,
		}
		m.users[callsign] = u
		m.updateFilter()
	}
	u.lastHeard = time.Now()
	return u
}

// staleUserLoop periodically removes users that haven't been heard from
// within m.staleTimeout. It runs as a background goroutine.
func (m *APRSModule) staleUserLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.removeStaleUsers()
	}
}

// removeStaleUsers deletes any users whose lastHeard time exceeds staleTimeout
// and updates the APRS-IS filter if any were removed.
func (m *APRSModule) removeStaleUsers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for cs, u := range m.users {
		if time.Since(u.lastHeard) > m.staleTimeout {
			log.Printf("[INFO] Removing stale APRS user %s (last heard %v ago)", cs, time.Since(u.lastHeard).Round(time.Second))
			delete(m.users, cs)
			removed++
		}
	}
	if removed > 0 {
		m.updateFilter()
	}
}

func (m *APRSModule) HandlePacket(p m17.Packet) error {
	log.Printf("[DEBUG] Received packet: %s", p.String())
	u := m.getOrAddUser(p.LSF.Src.Callsign())
	if u == nil {
		return fmt.Errorf("invalid APRS source callsign %q", p.LSF.Src.Callsign())
	}
	dst := aprsCallsign(p.LSF.Dst.Callsign())
	msgText := strings.ReplaceAll(string(p.Payload), "\x00", "")
	body, err := fap.EncodeMessage(&fap.Message{
		Destination: dst,
		Text:        msgText,
	})
	if err != nil {
		log.Printf("[INFO] Unable to encode APRS message: %v", err)
		return err
	}
	packet := fmt.Sprintf("%s>%s:%s", u.aprsCallsign, deviceID, body)
	log.Printf("[DEBUG] Sending packet: '%s'", packet)
	if err := m.sendLine(packet); err != nil {
		log.Printf("[INFO] Unable to send message: %v", err)
	}
	if p.LSF.GNSS() != nil && p.LSF.GNSS().ValidLatLon {
		if time.Since(u.lastPositionReport) > 5*time.Minute {
			if err := m.sendGNSSFrame(u, p.LSF.GNSS()); err != nil {
				log.Printf("[INFO] Unable to send location report: %v", err)
			}
			u.lastPositionReport = time.Now()
		}
	}
	return nil
}

func (m *APRSModule) HandleStreamDatagram(sd m17.StreamDatagram) error {
	if sd.LSF.GNSS() != nil && sd.LSF.GNSS().ValidLatLon {
		u := m.getOrAddUser(sd.LSF.Src.Callsign())
		if u == nil {
			return nil
		}
		if time.Since(u.lastPositionReport) > 5*time.Minute {
			if err := m.sendGNSSFrame(u, sd.LSF.GNSS()); err != nil {
				log.Printf("[INFO] Unable to send location report: %v", err)
			}
			u.lastPositionReport = time.Now()
		}
	} else {
		log.Printf("[DEBUG] Ignoring StreamDatagram: %v", sd)
	}
	return nil
}

func (m *APRSModule) sendGNSSFrame(u *aprsUser, s *m17.GNSS) error {
	var speed, course, altitude *float64
	if s.ValidBearingSpeed {
		sp := float64(s.Speed) * mpsToKmh
		speed = &sp
		c := float64(s.Bearing)
		course = &c
	}
	if s.ValidAltitude {
		alt := float64(s.Altitude)
		altitude = &alt
	}

	posStr, err := fap.EncodePosition(
		float64(s.Latitude),
		float64(s.Longitude),
		speed, course, altitude,
		m.aprsSymbol,
		nil,
	)
	if err != nil {
		return fmt.Errorf("making position report: %w", err)
	}

	frame := fmt.Sprintf("%s>%s,WIDE1-1,WIDE2-1:%s", u.aprsCallsign, deviceID, posStr)
	log.Printf("[DEBUG] Sending GNSS position report: %v, frame: %s", s, frame)
	return m.sendLine(frame)
}

func aprsCallsign(callsign string) string {
	// build APRS callsign by removing non-numeric suffix
	parts := strings.Split(callsign, " ")
	aprsCallsign := parts[0]
	suffix := parts[len(parts)-1]
	if len(suffix) != 1 || suffix[0] < '1' || suffix[0] > '9' {
		suffix = ""
	}
	if suffix != "" {
		aprsCallsign = aprsCallsign + "-" + suffix
	}
	log.Printf("[DEBUG] APRS callsign: %s", aprsCallsign)
	return aprsCallsign
}

// aprsCallsignRE matches an APRS-IS source callsign: letters and digits
// with an optional numeric SSID. M17 callsigns may also contain '/' and
// '.', and a '/' would split a g/ filter entry, so they are rejected.
var aprsCallsignRE = regexp.MustCompile(`^[A-Z0-9]{1,9}(-[0-9]{1,2})?$`)

func validAPRSCallsign(cs string) bool {
	return len(cs) <= 9 && aprsCallsignRE.MatchString(cs)
}
