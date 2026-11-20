package mqttstatus

import (
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{Host: "127.0.0.1", Port: 1883, Keepalive: time.Minute, Name: "m17-gateway", LogLevel: "INFO"}
}

func TestCheck(t *testing.T) {
	if err := testConfig().Check(); err != nil {
		t.Errorf("good config: %v", err)
	}
	for name, change := range map[string]func(*Config){
		"no host":         func(c *Config) { c.Host = "" },
		"port 0":          func(c *Config) { c.Port = 0 },
		"port too big":    func(c *Config) { c.Port = 70000 },
		"short keepalive": func(c *Config) { c.Keepalive = time.Second },
		"slash in name":   func(c *Config) { c.Name = "a/b" },
		"wildcard name":   func(c *Config) { c.Name = "a#" },
		"empty name":      func(c *Config) { c.Name = "" },
		"bad log level":   func(c *Config) { c.LogLevel = "LOUD" },
	} {
		c := testConfig()
		change(&c)
		if c.Check() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c := testConfig()
	c.LogLevel = "none"
	if err := c.Check(); err != nil {
		t.Errorf("LogLevel none: %v", err)
	}
}

func TestJSONWrapsTheType(t *testing.T) {
	p, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	p.JSON("MMDVM", MMDVM{Timestamp: "2026-11-20T01:02:03.004Z", Mode: "idle"}, false)
	p.JSON("link", Link{Timestamp: "2026-11-20T01:02:03.004Z", Action: "unlinked", Reason: "startup", Protocol: "m17"}, true)
	got := p.Drain()
	want := []Message{
		{"m17-gateway/json", `{"MMDVM":{"timestamp":"2026-11-20T01:02:03.004Z","mode":"idle"}}`, false},
		{"m17-gateway/json", `{"link":{"timestamp":"2026-11-20T01:02:03.004Z","action":"unlinked","reason":"startup","protocol":"m17"}}`, true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestQueueDropsWhenFull(t *testing.T) {
	p, _ := New(testConfig())
	for i := 0; i < queueLen+10; i++ {
		p.JSON("MMDVM", MMDVM{Mode: "idle"}, false)
	}
	if n := len(p.Drain()); n != queueLen {
		t.Errorf("queued %d, want %d", n, queueLen)
	}
	if d := p.dropped.Load(); d != 10 {
		t.Errorf("dropped %d, want 10", d)
	}
}

func TestTimestamp(t *testing.T) {
	ts := time.Date(2026, 11, 19, 20, 11, 55, 785402000, time.FixedZone("EST", -5*3600))
	if got := Timestamp(ts); got != "2026-11-20T01:11:55.785Z" {
		t.Errorf("Timestamp = %s", got)
	}
}

func TestLogWriter(t *testing.T) {
	c := testConfig()
	c.LogLevel = "INFO"
	p, _ := New(c)
	w := p.LogWriter()
	w.now = func() time.Time { return time.Date(2026, 11, 20, 1, 2, 3, 4000000, time.UTC) }
	w.Write([]byte("2026/11/19 20:02:03.004000 [DEBUG] noisy\n"))
	w.Write([]byte("2026/11/19 20:02:03.004000 [INFO] Connected to MMDVM modem on /dev/ttyAMA0\n"))
	w.Write([]byte("2026/11/19 20:02:03.004000 [WARN] getIdent: timeout\n"))
	w.Write([]byte("2026/11/19 20:02:03.004000 [ERROR] broken\n"))
	// A multi-line DEBUG dump is one message, and below INFO
	w.Write([]byte("2026/11/19 20:02:03.004000 [DEBUG] LSF: {\n\tDst: PARROT\n\tSrc: N1ADJ\n}\n"))
	w.Write([]byte("2026/11/19 20:02:03.004000 Bad configuration: no callsign\n"))
	var got []string
	for _, m := range p.Drain() {
		if m.Topic != "m17-gateway/log" || m.Retain {
			t.Errorf("message %+v", m)
		}
		got = append(got, m.Payload)
	}
	want := []string{
		"I: 2026-11-20 01:02:03.004 Connected to MMDVM modem on /dev/ttyAMA0",
		"W: 2026-11-20 01:02:03.004 getIdent: timeout",
		"E: 2026-11-20 01:02:03.004 broken",
		"I: 2026-11-20 01:02:03.004 Bad configuration: no callsign",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("log lines:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	c.LogLevel = "NONE"
	p, _ = New(c)
	p.LogWriter().Write([]byte("[ERROR] nothing\n"))
	if n := len(p.Drain()); n != 0 {
		t.Errorf("LogLevel NONE sent %d lines", n)
	}
}

func TestClientID(t *testing.T) {
	if got := clientID("m17-gateway", "m17-hotspot"); got != "m17-gateway-m17-hotspot" {
		t.Errorf("clientID = %q", got)
	}
	if got := clientID("m17-gateway", "a-very-long-hostname-indeed"); len(got) != 23 {
		t.Errorf("clientID %q not cut to 23", got)
	}
}
