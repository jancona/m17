package inet

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jancona/m17"
)

// packetModule records the packets the server passes it.
type packetModule struct {
	mu   sync.Mutex
	pkts []m17.Packet
}

func (m *packetModule) Name() byte { return 'A' }
func (m *packetModule) HandlePacket(p m17.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pkts = append(m.pkts, p)
	return nil
}
func (m *packetModule) HandleStreamDatagram(m17.StreamDatagram) error { return nil }
func (m *packetModule) received() []m17.Packet {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]m17.Packet(nil), m.pkts...)
}

// TestServerAnswersProbe: a probing client finds an inet.Server current,
// so it keeps sending packets, and the probe never reaches the module.
func TestServerAnswersProbe(t *testing.T) {
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.LocalAddr().(*net.UDPAddr)
	l.Close()
	mod := &packetModule{}
	s := NewServer("M17-BRG", addr.String(), map[byte]Module{'A': mod})
	go s.Start()
	t.Cleanup(func() { s.Close() })
	time.Sleep(100 * time.Millisecond)

	old := ProbeInterval
	ProbeInterval = 50 * time.Millisecond
	t.Cleanup(func() { ProbeInterval = old })
	events := make(chan string, 16)
	c, err := NewClient("M17-BRG", "127.0.0.1", uint(addr.Port), "A", "N1ADJ G",
		func(event, _ string, _ byte) { events <- event }, func(m17.Packet) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.ProbeReflector = true
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	waitEvent(t, events, "Connect")
	time.Sleep(5 * ProbeInterval) // past the point an unanswered probe makes it legacy
	if c.Legacy() {
		t.Fatal("client found inet.Server legacy")
	}

	sms, err := m17.NewPacket("W1AW", "N1ADJ G", m17.PacketTypeSMS, []byte("hi\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendPacket(*sms); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(mod.received()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := mod.received()
	if len(got) != 1 || got[0].Type != m17.PacketTypeSMS {
		t.Errorf("module received %v, want just the SMS", got)
	}
}
