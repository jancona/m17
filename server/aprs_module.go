package server

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

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
	name       byte
	server     *InetServer
	serverName string
	aprsSymbol string
	users      map[string]*aprsUser
}

type aprsUser struct {
	module             *APRSModule
	aprsCallsign       string
	passcode           int16
	mu                 sync.Mutex
	conn               *fap.Conn
	lastGet            time.Time
	lastPositionReport time.Time // limit how often we send position reports
}

func (u *aprsUser) sendGNSSFrame(s *m17.GNSS) error {
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

	posStr, err := fap.MakePosition(
		float64(s.Latitude),
		float64(s.Longitude),
		speed, course, altitude,
		u.module.aprsSymbol,
		nil,
	)
	if err != nil {
		return fmt.Errorf("making position report: %w", err)
	}

	frame := fmt.Sprintf("%s>%s,WIDE1-1,WIDE2-1:%s", u.aprsCallsign, deviceID, posStr)
	log.Printf("[DEBUG] Sending GNSS position report: %v, frame: %s", s, frame)
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.conn == nil {
		return fmt.Errorf("APRS-IS connection not available for %s", u.aprsCallsign)
	}
	return u.conn.SendLine(frame)
}

func NewAPRSModule(name byte, server *InetServer, serverName string, aprsSymbol string) (*APRSModule, error) {
	log.Printf("[DEBUG] NewAPRSModule(%s, %s)", string(name), serverName)
	if len(aprsSymbol) != 2 {
		return nil, fmt.Errorf("Bad APRS symbol '%s'", aprsSymbol)
	}
	m := APRSModule{
		name:       name,
		server:     server,
		serverName: serverName,
		aprsSymbol: aprsSymbol,
		users:      map[string]*aprsUser{},
	}
	return &m, nil
}

func (m *APRSModule) Name() byte {
	return m.name
}

func (m *APRSModule) HandlePacket(p m17.Packet) error {
	log.Printf("[DEBUG] Received packet: %s", p.String())
	u := m.getAPRSUser(p.LSF.Src.Callsign())
	if u != nil {
		dst := aprsCallsign(p.LSF.Dst.Callsign())
		msgText := strings.ReplaceAll(string(p.Payload), "\x00", "")
		frame := fmt.Sprintf("%s>%s::%-9s:%s", u.aprsCallsign, deviceID, dst, msgText)
		log.Printf("[DEBUG] Sending frame: '%s', passcode: %d", frame, u.passcode)
		u.mu.Lock()
		err := u.conn.SendLine(frame)
		u.mu.Unlock()
		if err != nil {
			log.Printf("[INFO] Unable to send message: %v", err)
		}
		if p.LSF.GNSS() != nil && p.LSF.GNSS().ValidLatLon {
			if time.Since(u.lastPositionReport) > 5*time.Minute {
				err := u.sendGNSSFrame(p.LSF.GNSS())
				if err != nil {
					log.Printf("[INFO] Unable to send location report: %v", err)
				}
				u.lastPositionReport = time.Now()
			}
		}
	}
	return nil
}
func (m *APRSModule) HandleStreamDatagram(sd m17.StreamDatagram) error {
	if sd.LSF.GNSS() != nil && sd.LSF.GNSS().ValidLatLon {
		u := m.getAPRSUser(sd.LSF.Src.Callsign())
		if u != nil {
			if time.Since(u.lastPositionReport) > 5*time.Minute {
				err := u.sendGNSSFrame(sd.LSF.GNSS())
				if err != nil {
					log.Printf("[INFO] Unable to send location report: %v", err)
				}
				u.lastPositionReport = time.Now()
			}
		}
	} else {
		log.Printf("[DEBUG] Ignoring StreamDatagram: %v", sd)
	}
	return nil
}

func (m *APRSModule) getAPRSUser(callsign string) *aprsUser {
	u, ok := m.users[callsign]

	if !ok {
		u = &aprsUser{
			module: m,
		}

		ac := aprsCallsign(callsign)
		filter := "g/" + ac
		if !strings.Contains(ac, "-") {
			filter += "*"
		}
		u.aprsCallsign = ac
		u.passcode = fap.AprsPasscode(ac)
		passcodeStr := fmt.Sprintf("%d", u.passcode)
		log.Printf("[DEBUG] Calling Dial(%s, %s, %s, m17-bridge, 0.1, %s)", m.serverName+clientDefinedFilterPort, u.aprsCallsign, passcodeStr, filter)
		conn, err := fap.Dial(m.serverName+clientDefinedFilterPort, u.aprsCallsign, passcodeStr, "m17-bridge", "0.1", filter)
		if err != nil {
			log.Printf("[INFO] Unable to connect to APRS-IS: %v", err)
			return nil
		}
		u.conn = conn
		log.Printf("[DEBUG] u.conn: %#v", u.conn)
		go func() {
			for {
				for {
					raw, err := conn.ReadPacket(readTimeout)
					if err != nil {
						log.Printf("[DEBUG] ReadPacket error: %v", err)
						u.mu.Lock()
						conn.Close()
						u.conn = nil
						u.mu.Unlock()
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
					msg := pkt.Message
					log.Printf("[DEBUG] dst: %s, msgText: %s, msgID: %s, aprsCallsign: %s", msg.Destination, msg.Text, msg.ID, ac)
					if msg.ID != "" {
						// Send ack
						ackFrame := fmt.Sprintf("%s>%s::%-9s:ack%s", u.aprsCallsign, deviceID, pkt.SrcCallsign, msg.ID)
						log.Printf("[DEBUG] Sending ack frame: '%s'", ackFrame)
						u.mu.Lock()
						err := conn.SendLine(ackFrame)
						u.mu.Unlock()
						if err != nil {
							log.Printf("[INFO] Unable to send ack: %v", err)
						}
					}
					if msg.Destination == ac {
						msgBytes := append([]byte(msg.Text), 0)
						p, err := m17.NewPacket(callsign, strings.ReplaceAll(pkt.SrcCallsign, "-", " "), m17.PacketTypeSMS, msgBytes)
						if err != nil {
							log.Printf("[INFO] Error building packet: %v", err)
							return
						}
						clients := m.server.lookupClientsByModule(m.Name())
						for _, c := range clients {
							m.server.SendPacket(p, c.addr)
						}
					}
				}
				log.Printf("[DEBUG] Reconnecting APRS-IS for %s", u.aprsCallsign)
				time.Sleep(reconnectDelay)
				passcodeStr := fmt.Sprintf("%d", u.passcode)
				log.Printf("[DEBUG] Calling Dial(%s, %s, %s, m17-bridge, 0.1, %s)", m.serverName+clientDefinedFilterPort, u.aprsCallsign, passcodeStr, filter)
				newConn, err := fap.Dial(m.serverName+clientDefinedFilterPort, u.aprsCallsign, passcodeStr, "m17-bridge", "0.1", filter)
				if err != nil {
					log.Printf("[INFO] Unable to reconnect to APRS-IS: %v", err)
					continue
				}
				u.mu.Lock()
				conn = newConn
				u.conn = conn
				u.mu.Unlock()
			}
		}()
		m.users[callsign] = u
	}

	u.lastGet = time.Now()
	return u
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
