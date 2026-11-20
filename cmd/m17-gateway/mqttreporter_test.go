package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jancona/m17"
	"github.com/jancona/m17/dashboard"
	"github.com/jancona/m17/mqttstatus"
)

var updateFixtures = flag.Bool("update", false, "rewrite testdata/mqtt from the reporter's output")

func testPublisher(t *testing.T) *mqttstatus.Publisher {
	t.Helper()
	p, err := mqttstatus.New(mqttstatus.Config{Host: "127.0.0.1", Port: 1883, Keepalive: time.Minute, Name: "m17-gateway", LogLevel: "INFO"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// testReporter is an MQTT reporter on an unstarted publisher (so messages
// stay queued) with a fixed clock.
func testReporter(t *testing.T) (*mqttReporter, *mqttstatus.Publisher) {
	p := testPublisher(t)
	r := newMQTTReporter(p)
	r.now = func() time.Time { return time.Date(2026, 11, 20, 1, 2, 3, 4000000, time.UTC) }
	r.rfHang, r.netHang = time.Hour, time.Hour // no mode changes behind the scenario's back
	return r, p
}

func testLSF(t *testing.T, dst, src string, typ m17.LSFType, dt m17.LSFDataType, can byte) *m17.LSF {
	t.Helper()
	lsf, err := m17.NewLSF(dst, src, typ, dt, can)
	if err != nil {
		t.Fatal(err)
	}
	return &lsf
}

// gnssLSF carries a position in META: 43.88 N, 69.99 W, 39 m, 62 km/h
// bearing 35, from a handheld's GNSS
func gnssLSF(t *testing.T) *m17.LSF {
	lsf := testLSF(t, "@ALL", "N1ADJ", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	lsf.Type[1] |= 0x1 << 5 // encryption subtype 1: GNSS
	lsf.Meta = [14]byte{
		0x12,             // data source 1 (OpenRTX), station type 2 (handheld)
		0xE0,             // valid: lat/lon, altitude, bearing/speed; radius bits 0, bearing high bit 0
		35,               // bearing low byte
		0x3E, 0x67, 0xA4, // latitude
		0xCE, 0x3A, 0x6E, // longitude (negative: west)
		0x04, 0x36, // altitude: 0x0436/2 - 500 = 39 m
		0x07, 0xC0, // speed: 0x07C/2 = 62 km/h
		0,
	}
	return lsf
}

// scenario drives a reporter through a gateway session.
func scenario(t *testing.T, r reporter) {
	r.starting("mmdvm", modemInfo{
		ident:    "MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646",
		protocol: 1, connection: "uart", port: "/dev/ttyAMA0", speed: 115200,
	})
	r.link(linkLinking, reasonStartup, "M17-M17", 'C')
	r.link(linkLinking, reasonEstablished, "M17-M17", 'C')

	voice := testLSF(t, "@ALL", "N1ADJ", m17.LSFTypeStream, m17.LSFDataTypeVoice, 2)
	r.voiceStart("rf", voice, false, 4.25)
	r.voiceEnd("rf", voice, 150, 1.5, false)

	net := testLSF(t, "M17-M17 C", "AA5RL", m17.LSFTypeStream, m17.LSFDataTypeVoiceData, 0)
	r.voiceStart("network", net, true, 0)
	r.voiceEnd("network", net, 40, 0, true)

	sms, err := m17.NewPacket("PARROT", "N1ADJ", m17.PacketTypeSMS, []byte("hi\x00"))
	if err != nil {
		t.Fatal(err)
	}
	r.packet("rf", sms.LSF, sms, 7.25)
	other, err := m17.NewPacket("@ALL", "DK1MI", m17.PacketType(0x81), []byte{0xde, 0xad})
	if err != nil {
		t.Fatal(err)
	}
	r.packet("network", other.LSF, other, 0)

	r.gnss("rf", gnssLSF(t))

	r.link(linkRelinking, "", "M17-M17", 'C')
	r.link(linkFailed, reasonTimer, "M17-M17", 'C')
	r.link(linkFailed, reasonRejected, "M17-M17", 'C')
	r.stopping()
	r.link(linkUnlinked, reasonShutdown, "M17-M17", 'C')
	r.stopped()
}

// TestMQTTMessages checks the reporter's messages against
// testdata/mqtt/session.jsonl, one message per line (go test -update to
// rewrite it). The file doubles as sample messages for consumers.
func TestMQTTMessages(t *testing.T) {
	r, p := testReporter(t)
	scenario(t, r)
	var lines []string
	for _, m := range p.Drain() {
		if m.Topic != "m17-gateway/json" {
			t.Errorf("topic %s", m.Topic)
		}
		checkSchema(t, m.Payload)
		retained := strings.HasPrefix(m.Payload, `{"link":`)
		if m.Retain != retained {
			t.Errorf("retain %v for %s", m.Retain, m.Payload)
		}
		lines = append(lines, m.Payload)
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "mqtt", "session.jsonl")
	if *updateFixtures {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("messages differ from %s:\n%s", path, got)
	}
}

// required fields and allowed values, from M17-Project/MMDVMHost's
// schema.json, G4KLX's gateway schemas, and docs/mqtt-design.md's additions
var schema = map[string]struct {
	required []string
	enums    map[string][]string
}{
	"MMDVM":     {[]string{"timestamp"}, map[string][]string{"mode": {"lockout", "idle", "error", "M17"}}},
	"status":    {[]string{"timestamp", "message"}, nil},
	"Modem":     {[]string{"timestamp", "open"}, map[string][]string{"connection": {"uart", "udp", "i2c", "null", "spi"}}},
	"M17":       {[]string{"timestamp", "action"}, map[string][]string{"action": {"invalid", "rejected", "start", "late_entry", "end", "lost"}, "source": {"rf", "network"}, "traffic_type": {"audio", "audio_data", "data", "packet"}}},
	"M17Packet": {[]string{"timestamp", "source", "src_callsign", "dst_callsign", "protocol", "length"}, map[string][]string{"source": {"rf", "network"}}},
	"M17GNSS":   {[]string{"timestamp", "source", "src_callsign", "latitude", "longitude"}, map[string][]string{"source": {"rf", "network"}}},
	"link":      {[]string{"timestamp", "action"}, map[string][]string{"action": {"linking", "unlinked", "failed", "relinking"}, "reason": {"user", "timer", "remote", "startup", "established", "rejected", "shutdown"}, "protocol": {"m17"}}},
}

func checkSchema(t *testing.T, payload string) {
	t.Helper()
	var top map[string]map[string]any
	if err := json.Unmarshal([]byte(payload), &top); err != nil || len(top) != 1 {
		t.Errorf("not a single-key object: %s", payload)
		return
	}
	for typ, obj := range top {
		s, ok := schema[typ]
		if !ok {
			t.Errorf("unknown type %s", typ)
			continue
		}
		for _, f := range s.required {
			if _, ok := obj[f]; !ok {
				t.Errorf("%s without %s: %s", typ, f, payload)
			}
		}
		for f, allowed := range s.enums {
			if v, ok := obj[f]; ok && !contains(allowed, v.(string)) {
				t.Errorf("%s %s %q not allowed: %s", typ, f, v, payload)
			}
		}
		for _, f := range []string{"src_callsign", "dst_callsign"} {
			if v, ok := obj[f].(string); ok && (len(v) < 1 || len(v) > 9) {
				t.Errorf("%s %s %q is not 1-9 characters", typ, f, v)
			}
		}
		if ts, _ := obj["timestamp"].(string); len(ts) != len("2026-11-20T01:02:03.004Z") || !strings.HasSuffix(ts, "Z") {
			t.Errorf("%s timestamp %q", typ, ts)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestModeHang: M17 while there's activity, idle again a hang time after
// it ends; the RF hang applies once there's been RF activity.
func TestModeHang(t *testing.T) {
	r, p := testReporter(t)
	r.rfHang, r.netHang = 80*time.Millisecond, 20*time.Millisecond
	modes := func() []string {
		var out []string
		for _, m := range p.Drain() {
			var top map[string]mqttstatus.MMDVM
			if json.Unmarshal([]byte(m.Payload), &top) == nil && top["MMDVM"].Mode != "" {
				out = append(out, top["MMDVM"].Mode)
			}
		}
		return out
	}
	net := testLSF(t, "@ALL", "AA5RL", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	r.voiceStart("network", net, false, 0)
	r.voiceEnd("network", net, 25, 0, false)
	time.Sleep(60 * time.Millisecond)
	if got := strings.Join(modes(), " "); got != "M17 idle" {
		t.Errorf("network only: modes %q, want \"M17 idle\" after the 20 ms hang", got)
	}

	rf := testLSF(t, "@ALL", "N1ADJ", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	r.voiceStart("rf", rf, false, 0)
	r.voiceEnd("rf", rf, 25, 0, false)
	time.Sleep(40 * time.Millisecond)
	if got := strings.Join(modes(), " "); got != "M17" {
		t.Errorf("RF, 40 ms after: modes %q, want still M17", got)
	}
	// Activity during the hang keeps it M17
	r.voiceStart("network", net, false, 0)
	r.voiceEnd("network", net, 25, 0, false)
	time.Sleep(150 * time.Millisecond)
	if got := strings.Join(modes(), " "); got != "idle" {
		t.Errorf("after the RF hang: modes %q, want idle once", got)
	}
}

// TestRepublishOnConnect: a (re)connection republishes the modem, the mode
// and the retained link.
func TestRepublishOnConnect(t *testing.T) {
	r, p := testReporter(t)
	r.starting("cc1200", modemInfo{ident: "CC1200-HAT 420-450 MHz\nFW v2.3 by Wojciech SP5WWP", connection: "uart", port: "/dev/ttyAMA0", speed: 460800})
	r.link(linkLinking, reasonEstablished, "M17-M17", 'C')
	p.Drain()
	r.republish()
	var types []string
	for _, m := range p.Drain() {
		var top map[string]json.RawMessage
		json.Unmarshal([]byte(m.Payload), &top)
		for k := range top {
			types = append(types, k)
		}
		if strings.Contains(m.Payload, "CC1200") && !strings.Contains(m.Payload, `"description":"CC1200-HAT 420-450 MHz / FW v2.3 by Wojciech SP5WWP"`) {
			t.Errorf("multi-line ident not joined: %s", m.Payload)
		}
	}
	if got := strings.Join(types, " "); got != "Modem MMDVM link" {
		t.Errorf("republished %q", got)
	}
}

// TestLinkEvents maps the reflector client's events.
func TestLinkEvents(t *testing.T) {
	r, p := testReporter(t)
	g := &Gateway{rep: r}
	for _, ev := range []string{"Connect", "Disconnect", "NoAnswer", "Rejected", "Closed", "Something"} {
		g.linkEvent(ev, "M17-M17", 'C')
	}
	var got []string
	for _, m := range p.Drain() {
		var top map[string]mqttstatus.Link
		json.Unmarshal([]byte(m.Payload), &top)
		got = append(got, top["link"].Action+"/"+top["link"].Reason)
	}
	want := "linking/established relinking/ failed/timer failed/rejected unlinked/shutdown"
	if strings.Join(got, " ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, " "), want)
	}
}

// TestDashboardLogUnchanged: the dashboard.log adapter writes the same
// events and fields as before MQTT (see DashboardLog.md). There's no GNSS
// line: dashboard.Logger skips positions for 15 s after it's created, as
// it always has.
func TestDashboardLogUnchanged(t *testing.T) {
	var buf strings.Builder
	l := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey || a.Key == slog.MessageKey || a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	scenario(t, dashReporter{dashboard.New(l)})
	want := `{"type":"Gateway","subtype":"Start","modem":"mmdvm","modemIdent":"MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646"}
{"type":"Reflector","subtype":"Connect","name":"M17-M17","module":"C"}
{"type":"RF","subtype":"Voice Start","src":"N1ADJ","dst":"@ALL","can":2,"mer":4.250000}
{"type":"RF","subtype":"Voice End","src":"N1ADJ","dst":"@ALL","can":2,"mer":1.500000}
{"type":"Internet","subtype":"Voice Start","src":"AA5RL","dst":"M17-M17 C","can":0}
{"type":"Internet","subtype":"Voice End","src":"AA5RL","dst":"M17-M17 C","can":0}
{"type":"RF","subtype":"Packet","src":"N1ADJ","dst":"PARROT","can":0,"mer":7.250000,"packetType":5,"smsMessage":"hi"}
{"type":"Internet","subtype":"Packet","src":"DK1MI","dst":"@ALL","can":0,"packetType":129}
{"type":"Reflector","subtype":"Disconnect","name":"M17-M17","module":"C"}
{"type":"Reflector","subtype":"Disconnect","name":"M17-M17","module":"C"}
{"type":"Reflector","subtype":"Disconnect","name":"M17-M17","module":"C"}
`
	if buf.String() != want {
		t.Errorf("dashboard.log:\n%s\nwant\n%s", buf.String(), want)
	}
}
