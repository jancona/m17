package main

import (
	"sync"
	"testing"
	"time"

	"github.com/jancona/m17"
)

// recordingModem records when each packet is transmitted.
type recordingModem struct {
	mu   sync.Mutex
	sent []time.Time
}

func (m *recordingModem) StartDecoding(func(uint16, []m17.SoftBit))    {}
func (m *recordingModem) Start() error                                 { return nil }
func (m *recordingModem) Reset() error                                 { return nil }
func (m *recordingModem) Close() error                                 { return nil }
func (m *recordingModem) TransmitVoiceStream(m17.StreamDatagram) error { return nil }
func (m *recordingModem) TransmitPacket(m17.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, time.Now())
	return nil
}

func (m *recordingModem) times() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Time(nil), m.sent...)
}

func testGateway(t *testing.T, gap time.Duration) (*Gateway, *recordingModem) {
	t.Helper()
	m := &recordingModem{}
	g := &Gateway{modem: m, state: Idle, lastStreamID: 0xFFFF, packetGap: gap, packetQueue: make(chan func() error, packetQueueLen)}
	go g.sendPackets()
	t.Cleanup(func() { close(g.packetQueue) })
	return g, m
}

func smsPacket(t *testing.T, text string) m17.Packet {
	t.Helper()
	p, err := m17.NewPacket("N1ADJ 8", "W1AW", m17.PacketTypeSMS, append([]byte(text), 0))
	if err != nil {
		t.Fatal(err)
	}
	return *p
}

func waitSent(t *testing.T, m *recordingModem, n int) []time.Time {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(m.times()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d packets sent, want %d", len(m.times()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return m.times()
}

// TestPacketsSpaced: queued packets go out one at a time, at least
// packetGap apart.
func TestPacketsSpaced(t *testing.T) {
	gap := 150 * time.Millisecond
	g, m := testGateway(t, gap)
	for _, s := range []string{"one", "two", "three", "four"} {
		g.queueLocalPacket(smsPacket(t, s))
	}
	sent := waitSent(t, m, 4)
	for i := 1; i < len(sent); i++ {
		if d := sent[i].Sub(sent[i-1]); d < gap {
			t.Errorf("packets %d and %d only %v apart; gap is %v", i-1, i, d, gap)
		}
	}
}

// TestPacketWaitsForRF: a packet queued while a radio is transmitting goes
// out only after the channel has been quiet for packetGap.
func TestPacketWaitsForRF(t *testing.T) {
	gap := 150 * time.Millisecond
	g, m := testGateway(t, gap)
	stop := time.Now().Add(400 * time.Millisecond)
	g.rfHeard()
	g.queueLocalPacket(smsPacket(t, "while you talk"))
	var lastRF time.Time
	for time.Now().Before(stop) { // frames every 40 ms, as a voice stream
		g.rfHeard()
		lastRF = time.Now()
		if len(m.times()) > 0 {
			t.Fatal("packet transmitted while RF was being received")
		}
		time.Sleep(40 * time.Millisecond)
	}
	sent := waitSent(t, m, 1)
	if d := sent[0].Sub(lastRF); d < gap {
		t.Errorf("packet went %v after the last RF frame; gap is %v", d, gap)
	}
}
