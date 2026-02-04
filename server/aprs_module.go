package server

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/ebarkie/aprs"

	"github.com/jancona/m17"
)

const (
	clientDefinedFilterPort = ":14580"
	deviceID                = "APZ001" // Experimental
	metersToFeet            = 3.280839895
	mpsToKnots              = 1.94384
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
	aprsCallsign       aprs.Addr
	passcode           uint16
	ctx                context.Context
	frames             <-chan aprs.Frame
	lastGet            time.Time
	lastPositionReport time.Time // limit how often we send position reports
}

func (u *aprsUser) sendGNSSFrame(s *m17.GNSS) error {
	p := aprs.PositionReport{ // create a position report
		Lat:            float64(s.Latitude),
		Lon:            float64(s.Longitude),
		Symbol:         u.module.aprsSymbol,
		MessageCapable: true, // all our clients can receive messages
	}
	if s.ValidAltitude {
		p.Altitude = int(math.Round(float64(s.Altitude) * metersToFeet))
	}
	if s.ValidBearingSpeed {
		p.CSExtension(int(s.Bearing), int(math.Round(float64(s.Speed)*mpsToKnots)), 0, 0)
	}

	f := aprs.Frame{}
	f.Src = u.aprsCallsign
	f.Dst.FromString(deviceID)
	f.Path.FromString("WIDE1-1,WIDE2-1")
	f.Text = p.String()
	log.Printf("[DEBUG] Sending GNSS position report: %v, f: %#v", s, f)
	return f.SendTCP(u.module.serverName+clientDefinedFilterPort, int(u.passcode))
}

func NewAPRSModule(name byte, server *InetServer, serverName string, aprsCallsign string, aprsSymbol string) (*APRSModule, error) {
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
		f := aprs.Frame{}
		f.Src = u.aprsCallsign
		f.Dst.FromString(deviceID)
		f.Text = fmt.Sprintf(":%-9s:", aprsCallsign(p.LSF.Dst.Callsign())) + strings.ReplaceAll(string(p.Payload), "\x00", "")
		log.Printf("[DEBUG] Sending frame: '%s' to %s, passcode: %d", f.String(), m.serverName+clientDefinedFilterPort, u.passcode)
		err := f.SendTCP(m.serverName+clientDefinedFilterPort, int(u.passcode))
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

		aprsCallsign := aprsCallsign(callsign)
		filter := "g/" + aprsCallsign
		if !strings.Contains(aprsCallsign, "-") {
			filter += "*"
		}
		u.aprsCallsign.FromString(aprsCallsign)
		u.passcode = aprs.GenPass(aprsCallsign)
		u.ctx = context.Background()
		go func() {
			for {
				log.Printf("[DEBUG] Calling RecvIS(ctx, %s, %v, %d, %s)", m.serverName+clientDefinedFilterPort, u.aprsCallsign, int(u.passcode), filter)
				u.frames = aprs.RecvIS(u.ctx, m.serverName+clientDefinedFilterPort, u.aprsCallsign, int(u.passcode), filter)
				for f := range u.frames {
					log.Printf("[DEBUG] received frame: %#v", f)
					dst, msgText, ack := decodeMsg(f.Text)
					log.Printf("[DEBUG] dst: %s, msgText: %s, ack: %s, aprsCallsign: %s", dst, msgText, ack, aprsCallsign)
					if ack != "" {
						// Send ack
						a := aprs.Frame{}
						a.Src = u.aprsCallsign
						a.Dst.FromString(deviceID)
						c := f.Src.Call
						if f.Src.SSID > 0 {
							c += fmt.Sprintf("-%d", f.Src.SSID)
						}
						a.Text = fmt.Sprintf(":%-9s:", c) + "ack" + ack[1:]
						log.Printf("[DEBUG] Sending frame: '%s' to %s, passcode: %d", a.String(), m.serverName+clientDefinedFilterPort, u.passcode)
						err := a.SendTCP(m.serverName+clientDefinedFilterPort, int(u.passcode))
						if err != nil {
							log.Printf("[INFO] Unable to send message: %v", err)
						}
					}
					if dst == aprsCallsign {
						msg := append([]byte(msgText), 0)
						p, err := m17.NewPacket(callsign, strings.ReplaceAll(f.Src.String(), "-", " "), m17.PacketTypeSMS, msg)
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
				log.Printf("[DEBUG] Exiting RecvIS loop for %s", u.aprsCallsign)
			}
		}()
		m.users[callsign] = u
	}

	u.lastGet = time.Now()
	return u
}

func decodeMsg(text string) (dst string, msg string, ack string) {
	if len(text) == 0 || text[0] != ':' {
		// Not an APRS message
		return
	}
	text = text[1:]
	parts := strings.SplitN(text, ":", 2)
	switch len(parts) {
	case 1: // No ":"'s
		dst = strings.TrimSpace(parts[0])
	case 2:
		dst = strings.TrimSpace(parts[0])
		i := strings.LastIndex(parts[1], "{")
		if i == -1 {
			msg = parts[1]
		} else {
			msg = parts[1][:i]
			ack = parts[1][i:]
		}
	}
	return
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
