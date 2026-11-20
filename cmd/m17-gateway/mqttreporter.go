package main

import (
	"encoding/hex"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jancona/m17"
	"github.com/jancona/m17/mqttstatus"
)

// Mode hang times: after this long with no activity the mode goes back to
// idle. MMDVMHost's defaults (RFModeHang, NetModeHang).
const (
	rfModeHang  = 10 * time.Second
	netModeHang = 3 * time.Second
)

// gnssInterval limits positions to one per this long, as dashboard.log
const gnssInterval = 15 * time.Second

// mqttReporter publishes the gateway's events over MQTT (see
// docs/mqtt-design.md).
type mqttReporter struct {
	p       *mqttstatus.Publisher
	now     func() time.Time
	rfHang  time.Duration
	netHang time.Duration

	mu          sync.Mutex
	modem       *mqttstatus.Modem // current, republished on reconnect
	mode        string
	curLink     *mqttstatus.Link
	activeRF    int // streams in progress
	activeNet   int
	rfSinceIdle bool
	hang        *time.Timer
	lastGNSS    time.Time
}

func newMQTTReporter(p *mqttstatus.Publisher) *mqttReporter {
	r := &mqttReporter{p: p, now: time.Now, rfHang: rfModeHang, netHang: netModeHang}
	p.OnConnect(r.republish)
	return r
}

func (r *mqttReporter) ts() string { return mqttstatus.Timestamp(r.now()) }

// republish sends the current state after a (re)connection, so consumers
// that started after the gateway, or a restarted broker, learn it.
func (r *mqttReporter) republish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.modem != nil {
		m := *r.modem
		m.Timestamp = r.ts()
		r.p.JSON("Modem", m, false)
	}
	if r.mode != "" {
		r.p.JSON("MMDVM", mqttstatus.MMDVM{Timestamp: r.ts(), Mode: r.mode}, false)
	}
	if r.curLink != nil {
		r.p.JSON("link", *r.curLink, true)
	}
}

func (r *mqttReporter) lifecycle(msg string) {
	r.p.JSON("MMDVM", mqttstatus.MMDVM{Timestamp: r.ts(), Message: msg}, false)
	r.p.JSON("status", mqttstatus.Status{Timestamp: r.ts(), Message: msg}, false)
}

func (r *mqttReporter) starting(_ string, mi modemInfo) {
	r.lifecycle("m17-gateway is starting")
	m := mqttstatus.Modem{
		Timestamp:   r.ts(),
		Open:        true,
		Protocol:    mi.protocol,
		Description: strings.Join(strings.Fields(strings.ReplaceAll(mi.ident, "\n", " / ")), " "),
		Modes:       []string{"M17"},
		Connection:  mi.connection,
		Port:        mi.port,
		Speed:       mi.speed,
	}
	r.mu.Lock()
	r.modem = &m
	r.p.JSON("Modem", m, false)
	r.setMode("idle")
	r.mu.Unlock()
}

func (r *mqttReporter) stopping() { r.lifecycle("m17-gateway is stopping") }

func (r *mqttReporter) stopped() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hang != nil {
		r.hang.Stop()
	}
	m := mqttstatus.Modem{Timestamp: r.ts(), Open: false}
	r.modem = &m
	r.p.JSON("Modem", m, false)
}

func (r *mqttReporter) link(action, reason, reflector string, module byte) {
	l := mqttstatus.Link{Timestamp: r.ts(), Action: action, Reason: reason, Reflector: reflector, Protocol: "m17"}
	if module >= 'A' && module <= 'Z' {
		l.Module = string(module)
	}
	r.mu.Lock()
	r.curLink = &l
	r.mu.Unlock()
	r.p.JSON("link", l, true)
}

// setMode publishes a mode change. r.mu is held.
func (r *mqttReporter) setMode(mode string) {
	if r.mode == mode {
		return
	}
	r.mode = mode
	r.p.JSON("MMDVM", mqttstatus.MMDVM{Timestamp: r.ts(), Mode: mode}, false)
}

// activityStart: a stream or packet began, so the mode is M17 until a hang
// time after the last activity ends.
func (r *mqttReporter) activityStart(rf bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rf {
		r.activeRF++
		r.rfSinceIdle = true
	} else {
		r.activeNet++
	}
	if r.hang != nil {
		r.hang.Stop()
	}
	r.setMode("M17")
}

func (r *mqttReporter) activityEnd(rf bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rf {
		r.activeRF = max(r.activeRF-1, 0)
	} else {
		r.activeNet = max(r.activeNet-1, 0)
	}
	if r.activeRF > 0 || r.activeNet > 0 {
		return
	}
	hang := r.netHang
	if r.rfSinceIdle {
		hang = r.rfHang
	}
	if r.hang != nil {
		r.hang.Stop()
	}
	r.hang = time.AfterFunc(hang, r.idle)
}

func (r *mqttReporter) idle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeRF > 0 || r.activeNet > 0 {
		return
	}
	r.rfSinceIdle = false
	r.setMode("idle")
}

// mmdvmCallsign is a callsign as MMDVMHost sends it: broadcast is "ALL".
func mmdvmCallsign(c m17.EncodedCallsign) string {
	s := c.Callsign()
	if s == m17.DestinationAll {
		return "ALL"
	}
	return s
}

func trafficType(lsf *m17.LSF) string {
	switch lsf.DataType() {
	case m17.LSFDataTypeVoice:
		return "audio"
	case m17.LSFDataTypeVoiceData:
		return "audio_data"
	case m17.LSFDataTypeData:
		return "data"
	}
	return ""
}

func (r *mqttReporter) voiceStart(source string, lsf *m17.LSF, lateEntry bool, _ float64) {
	r.activityStart(source == "rf")
	action := "start"
	if lateEntry {
		action = "late_entry"
	}
	can := int(lsf.CAN())
	r.p.JSON("M17", mqttstatus.M17{
		Timestamp:   r.ts(),
		SrcCallsign: mmdvmCallsign(lsf.Src),
		DstCallsign: mmdvmCallsign(lsf.Dst),
		Source:      source,
		Action:      action,
		TrafficType: trafficType(lsf),
		CAN:         &can,
	}, false)
}

func (r *mqttReporter) voiceEnd(source string, _ *m17.LSF, frames int, ber float64, lost bool) {
	action := "end"
	if lost {
		action = "lost"
	}
	duration := float64(frames) / 25
	msg := mqttstatus.M17{Timestamp: r.ts(), Source: source, Action: action, Duration: &duration}
	if source == "rf" {
		msg.BER = &ber
	}
	r.p.JSON("M17", msg, false)
	r.activityEnd(source == "rf")
}

func (r *mqttReporter) packet(source string, lsf *m17.LSF, p *m17.Packet, _ float64) {
	r.activityStart(source == "rf")
	can := int(lsf.CAN())
	msg := mqttstatus.M17Packet{
		Timestamp:   r.ts(),
		Source:      source,
		SrcCallsign: mmdvmCallsign(lsf.Src),
		DstCallsign: mmdvmCallsign(lsf.Dst),
		Protocol:    int(p.Type),
		Length:      1 + len(p.Payload),
		CAN:         &can,
	}
	if p.Type == m17.PacketTypeSMS {
		text := p.SMSText()
		msg.Text = &text
	} else {
		msg.Data = strings.ToUpper(hex.EncodeToString(append([]byte{byte(p.Type)}, p.Payload...)))
	}
	r.p.JSON("M17Packet", msg, false)
	r.activityEnd(source == "rf")
}

func (r *mqttReporter) gnss(source string, lsf *m17.LSF) {
	g := lsf.GNSS()
	if g == nil || !g.ValidLatLon {
		return
	}
	r.mu.Lock()
	if r.now().Sub(r.lastGNSS) < gnssInterval {
		r.mu.Unlock()
		return
	}
	r.lastGNSS = r.now()
	r.mu.Unlock()
	msg := mqttstatus.M17GNSS{
		Timestamp:   r.ts(),
		Source:      source,
		SrcCallsign: mmdvmCallsign(lsf.Src),
		DataSource:  int(g.DataSource),
		StationType: int(g.StationType),
		Latitude:    round6(float64(g.Latitude)),
		Longitude:   round6(float64(g.Longitude)),
	}
	if g.ValidAltitude {
		a := float64(g.Altitude)
		msg.Altitude = &a
	}
	if g.ValidBearingSpeed {
		s, b := float64(g.Speed), int(g.Bearing)
		msg.Speed, msg.Bearing = &s, &b
	}
	if g.ValidRadius {
		rad := int(g.Radius)
		msg.Radius = &rad
	}
	r.p.JSON("M17GNSS", msg, false)
}

// round6 rounds a coordinate to 6 decimal places (about 10 cm), as
// dashboard.log writes them: more is float32 noise.
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
