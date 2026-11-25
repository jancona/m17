package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jancona/m17"
	"github.com/jancona/m17/dashboard"
	"github.com/jancona/m17/inet"
	"github.com/jancona/m17/modem"
)

// reporter is told about what the gateway does: dashboard.log and MQTT
// both hear about it through here. source is "rf" or "network".
type reporter interface {
	starting(modemType string, mi modemInfo)
	stopping()
	stopped()
	link(action, reason, reflector string, module byte)
	voiceStart(source string, lsf *m17.LSF, lateEntry bool, ber float64)
	voiceEnd(source string, lsf *m17.LSF, frames int, ber float64, lost bool)
	packet(source string, lsf *m17.LSF, p *m17.Packet, ber float64)
	gnss(source string, lsf *m17.LSF)
}

// modemInfo describes the open modem, for MQTT's Modem message.
type modemInfo struct {
	ident      string
	protocol   int    // MMDVM protocol version; 0 for other modems
	connection string // uart or spi; "" for the dummy modem
	port       string
	speed      int
}

// Link actions and reasons, as G4KLX's gateway programs publish them
const (
	linkLinking   = "linking"
	linkUnlinked  = "unlinked"
	linkRelinking = "relinking"
	linkFailed    = "failed"

	reasonStartup     = "startup"
	reasonEstablished = "established"
	reasonTimer       = "timer"
	reasonRejected    = "rejected"
	reasonShutdown    = "shutdown"
)

// report returns the gateway's reporter, or one that does nothing (tests
// build a Gateway without one).
func (g *Gateway) report() reporter {
	if g.rep == nil {
		return nopReporter{}
	}
	return g.rep
}

type nopReporter struct{}

func (nopReporter) starting(string, modemInfo)                    {}
func (nopReporter) stopping()                                     {}
func (nopReporter) stopped()                                      {}
func (nopReporter) link(string, string, string, byte)             {}
func (nopReporter) voiceStart(string, *m17.LSF, bool, float64)    {}
func (nopReporter) voiceEnd(string, *m17.LSF, int, float64, bool) {}
func (nopReporter) packet(string, *m17.LSF, *m17.Packet, float64) {}
func (nopReporter) gnss(string, *m17.LSF)                         {}

// reporters tells each of its reporters.
type reporters []reporter

func (rs reporters) starting(t string, mi modemInfo) {
	for _, r := range rs {
		r.starting(t, mi)
	}
}
func (rs reporters) stopping() {
	for _, r := range rs {
		r.stopping()
	}
}
func (rs reporters) stopped() {
	for _, r := range rs {
		r.stopped()
	}
}
func (rs reporters) link(a, reason, refl string, m byte) {
	for _, r := range rs {
		r.link(a, reason, refl, m)
	}
}
func (rs reporters) voiceStart(s string, lsf *m17.LSF, late bool, ber float64) {
	for _, r := range rs {
		r.voiceStart(s, lsf, late, ber)
	}
}
func (rs reporters) voiceEnd(s string, lsf *m17.LSF, frames int, ber float64, lost bool) {
	for _, r := range rs {
		r.voiceEnd(s, lsf, frames, ber, lost)
	}
}
func (rs reporters) packet(s string, lsf *m17.LSF, p *m17.Packet, ber float64) {
	for _, r := range rs {
		r.packet(s, lsf, p, ber)
	}
}
func (rs reporters) gnss(s string, lsf *m17.LSF) {
	for _, r := range rs {
		r.gnss(s, lsf)
	}
}

// ── dashboard.log ───────────────────────────────────────────────────────

// dashReporter writes dashboard.log exactly as before MQTT: the same
// events, types and fields.
type dashReporter struct{ l *dashboard.Logger }

func dashType(source string) string {
	if source == "rf" {
		return "RF"
	}
	return "Internet"
}

func mer(ber float64) json.Number { return json.Number(fmt.Sprintf("%f", ber)) }

func (d dashReporter) starting(modemType string, mi modemInfo) {
	d.l.Log("Gateway", "Start", "modem", modemType, "modemIdent", mi.ident)
}
func (d dashReporter) stopping() {}
func (d dashReporter) stopped()  {}

// link: dashboard.log has only Connect (the link was made) and Disconnect
// (it went away)
func (d dashReporter) link(action, reason, reflector string, module byte) {
	switch {
	case action == linkLinking && reason == reasonEstablished:
		d.l.Log("Reflector", "Connect", "name", reflector, "module", string(module))
	case action == linkRelinking, action == linkFailed && reason == reasonRejected,
		action == linkUnlinked && reason == reasonShutdown:
		d.l.Log("Reflector", "Disconnect", "name", reflector, "module", string(module))
	}
}
func (d dashReporter) voiceStart(source string, lsf *m17.LSF, _ bool, ber float64) {
	if source == "rf" {
		d.l.LogFrame(lsf, "RF", "Voice Start", "mer", mer(ber))
	} else {
		d.l.LogFrame(lsf, "Internet", "Voice Start")
	}
}
func (d dashReporter) voiceEnd(source string, lsf *m17.LSF, _ int, ber float64, _ bool) {
	if source == "rf" {
		d.l.LogFrame(lsf, "RF", "Voice End", "mer", mer(ber))
	} else {
		d.l.LogFrame(lsf, "Internet", "Voice End")
	}
}
func (d dashReporter) packet(source string, lsf *m17.LSF, p *m17.Packet, ber float64) {
	args := []any{}
	if source == "rf" {
		args = append(args, "mer", mer(ber))
	}
	args = append(args, "packetType", p.Type)
	if p.Type == m17.PacketTypeSMS && len(p.Payload) > 0 {
		args = append(args, "smsMessage", p.SMSText())
	}
	d.l.LogFrame(lsf, dashType(source), "Packet", args...)
}
func (d dashReporter) gnss(source string, lsf *m17.LSF) { d.l.LogGNSS(lsf, dashType(source)) }

// linkEvent turns the reflector client's events into link reports.
func (g *Gateway) linkEvent(event, name string, module byte) {
	switch event {
	case inet.EventConnect:
		g.report().link(linkLinking, reasonEstablished, name, module)
	case inet.EventDisconnect:
		g.report().link(linkRelinking, "", name, module)
	case inet.EventNoAnswer:
		g.report().link(linkFailed, reasonTimer, name, module)
	case inet.EventRejected:
		g.report().link(linkFailed, reasonRejected, name, module)
	case inet.EventClosed:
		g.report().link(linkUnlinked, reasonShutdown, name, module)
	}
}

// moduleByte is a [Reflector] Module as the client stores it: 0 for none.
func moduleByte(m string) byte {
	if len(m) == 1 {
		return m[0]
	}
	return 0
}

// reporter builds the configured reporters: dashboard.log (a no-op when
// it's off) and MQTT when it's on.
func (c config) reporter() reporter {
	rs := reporters{dashReporter{dashboard.New(c.dashboardLogger)}}
	if c.mqtt != nil {
		rs = append(rs, newMQTTReporter(c.mqtt))
	}
	return rs
}

// modemInfo describes the open modem from the configuration and what the
// modem reported.
func (c config) modemInfo(mdm modem.Modem) modemInfo {
	mi := modemInfo{ident: mdm.Ident()}
	switch c.modemType {
	case "cc1200", "cc1200v2", "mmdvm":
		mi.connection = "uart"
		mi.port = c.modemCfg.Key("Port").String()
		mi.speed = c.modemCfg.Key("Speed").MustInt(0)
	case "sx1255":
		mi.connection = "spi"
		mi.port = c.modemCfg.Key("SPIDevice").MustString("/dev/spidev0.0")
	}
	if v, ok := mdm.(interface{ ProtocolVersion() int }); ok {
		mi.protocol = v.ProtocolVersion()
	}
	return mi
}

// The gateway's own transmissions (echoes, info replies and spoken
// messages) are reported as network traffic: in an MMDVMHost hotspot the
// gateway program sends them to MMDVMHost like reflector traffic, and
// MMDVMHost reports them so. Dashboards then show the radio transmitting.

// ownReport is the gateway's own voice transmission being reported.
type ownReport struct {
	lsf   m17.LSF
	start time.Time
}

// reportOwnVoice reports the start of the gateway's own voice
// transmission, unless a reflector stream is being reported (reported
// network voice never overlaps). Call the returned function with the
// number of frames sent when it ends.
func (g *Gateway) reportOwnVoice(lsf m17.LSF) func(frames int) {
	g.netStreamMutex.Lock()
	defer g.netStreamMutex.Unlock()
	if g.netStreamActive {
		return func(int) {}
	}
	g.endOwnReport(g.ownReport, -1)
	r := &ownReport{lsf: lsf, start: time.Now()}
	g.ownReport = r
	g.report().voiceStart("network", &r.lsf, false, 0)
	return func(frames int) {
		g.netStreamMutex.Lock()
		defer g.netStreamMutex.Unlock()
		g.endOwnReport(r, frames)
	}
}

// endOwnReport reports the end of r, if it's still the one being reported.
// frames < 0: ended early, so count the frames from the time it started.
// Must be called with netStreamMutex held.
func (g *Gateway) endOwnReport(r *ownReport, frames int) {
	if r == nil || g.ownReport != r {
		return
	}
	if frames < 0 {
		frames = int(time.Since(r.start) / m17.FrameTime)
	}
	g.report().voiceEnd("network", &r.lsf, frames, 0, false)
	g.ownReport = nil
}

// reportOwnPacket reports a packet the gateway sends of its own.
func (g *Gateway) reportOwnPacket(p *m17.Packet) {
	g.report().packet("network", p.LSF, p, 0)
}
