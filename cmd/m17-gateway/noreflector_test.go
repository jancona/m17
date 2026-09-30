package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jancona/m17"
)

// capturingModem records the packets it is asked to transmit.
type capturingModem struct {
	recordingModem
	mu   sync.Mutex
	pkts []m17.Packet
}

func (m *capturingModem) TransmitPacket(p m17.Packet) error {
	m.mu.Lock()
	m.pkts = append(m.pkts, p)
	m.mu.Unlock()
	return m.recordingModem.TransmitPacket(p)
}

func (m *capturingModem) sent() []m17.Packet {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]m17.Packet(nil), m.pkts...)
}

// noReflectorGateway is a gateway with no reflector configured (Name empty),
// so it has no inet client.
func noReflectorGateway(t *testing.T, duplex bool) (*Gateway, *capturingModem) {
	t.Helper()
	m := &capturingModem{}
	g := &Gateway{modem: m, state: Idle, lastStreamID: 0xFFFF, duplex: duplex,
		callsign: "N1ADJ G", packetQueue: make(chan func() error, packetQueueLen)}
	cs, err := m17.EncodeCallsign("N1ADJ G")
	if err != nil {
		t.Fatal(err)
	}
	g.encodedCallsign = *cs
	go g.sendPackets()
	t.Cleanup(func() { close(g.packetQueue) })
	return g, m
}

func receive(t *testing.T, g *Gateway, dst, text string) {
	t.Helper()
	p, err := m17.NewPacket(dst, "N1ADJ 8", m17.PacketTypeSMS, append([]byte(text), 0))
	if err != nil {
		t.Fatal(err)
	}
	payload := p.PayloadBytes()
	if err := g.receivedRFPacket(*p.LSF, payload, 0); err != nil {
		t.Fatalf("receivedRFPacket: %v", err)
	}
}

// TestNoReflectorRepeats: with no reflector, a duplex gateway still repeats
// a packet it receives, and a simplex one does not transmit it.
func TestNoReflectorRepeats(t *testing.T) {
	g, m := noReflectorGateway(t, true)
	receive(t, g, "W1AW", "hello")
	waitSent(t, &m.recordingModem, 1)

	g, m = noReflectorGateway(t, false)
	receive(t, g, "W1AW", "hello")
	time.Sleep(300 * time.Millisecond)
	if n := len(m.sent()); n != 0 {
		t.Errorf("simplex gateway transmitted %d packets, want 0", n)
	}
}

// TestNoReflectorInfo: the #INFO (and /INFO) reply says the gateway is not linked.
func TestNoReflectorInfo(t *testing.T) {
	g, m := noReflectorGateway(t, false)
	receive(t, g, "#INFO", "")
	waitSent(t, &m.recordingModem, 1)
	got := string(m.sent()[0].Payload)
	if !strings.Contains(got, "not linked") {
		t.Errorf("/INFO reply %q, want it to say not linked", got)
	}
}
