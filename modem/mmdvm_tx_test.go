package modem

import (
	"sync"
	"testing"
	"time"

	"gopkg.in/ini.v1"
)

func TestDCOffset(t *testing.T) {
	cfg, err := ini.Load([]byte("[Modem]\nTXDCOffset=-5\nRXDCOffset=127\n[Bad]\nTXDCOffset=200\n"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := dcOffset(cfg.Section("Modem"), "TXDCOffset")
	if err != nil || tx != -5 {
		t.Errorf("TXDCOffset=-5: got %d, %v", tx, err)
	}
	rx, err := dcOffset(cfg.Section("Modem"), "RXDCOffset")
	if err != nil || rx != 127 {
		t.Errorf("RXDCOffset=127: got %d, %v", rx, err)
	}
	if v, err := dcOffset(cfg.Section("Modem"), "Unset"); err != nil || v != 0 {
		t.Errorf("unset: got %d, %v, want 0", v, err)
	}
	if _, err := dcOffset(cfg.Section("Bad"), "TXDCOffset"); err == nil {
		t.Error("TXDCOffset=200: no error")
	}
	for v, want := range map[int8]byte{-128: 0, -5: 123, 0: 128, 127: 255} {
		if got := dcOffsetByte(v); got != want {
			t.Errorf("dcOffsetByte(%d) = %d, want %d", v, got, want)
		}
	}
}

// TestWaitTXDone: TransmitPacket must not return until the modem reports
// its buffer empty again, and must not wait for ever if it never does.
func TestWaitTXDone(t *testing.T) {
	m := &MMDVM{sendCmds: make(chan []byte, 10)}
	m.setSpace(31) // idle: buffer empty
	m.setSpace(28) // a packet written
	go func() {
		time.Sleep(300 * time.Millisecond)
		m.setSpace(31) // the modem has sent it
	}()
	start := time.Now()
	m.waitTXDone(2 * time.Second)
	if d := time.Since(start); d < 300*time.Millisecond+packetEndMarginMMDVM || d > time.Second {
		t.Errorf("returned after %v, want about %v", d, 300*time.Millisecond+packetEndMarginMMDVM)
	}

	m.setSpace(28) // and one that never drains
	start = time.Now()
	m.waitTXDone(200 * time.Millisecond)
	if d := time.Since(start); d < 200*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("stuck buffer: returned after %v, want the 200 ms limit", d)
	}

	old := &MMDVM{sendCmds: make(chan []byte, 10)} // never reports space
	start = time.Now()
	old.waitTXDone(2 * time.Second)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("no space reports: waited %v, want an immediate return", d)
	}
}

// fakeModemPort is a non-blocking serial port: Read returns queued bytes, or
// 0 when there are none. onWrite can queue a reply when a command is written.
type fakeModemPort struct {
	mu      sync.Mutex
	in      []byte
	onWrite func(cmd []byte)
}

func (p *fakeModemPort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := copy(b, p.in)
	p.in = p.in[n:]
	return n, nil
}

func (p *fakeModemPort) Write(b []byte) (int, error) {
	if p.onWrite != nil {
		p.onWrite(b)
	}
	return len(b), nil
}

func (p *fakeModemPort) Close() error { return nil }

func (p *fakeModemPort) queue(msg []byte) {
	p.mu.Lock()
	p.in = append(p.in, msg...)
	p.mu.Unlock()
}

func modemMsg(typ byte, payload []byte) []byte {
	return append([]byte{mmdvmFrameStart, byte(3 + len(payload)), typ}, payload...)
}

// streamFrames queues n M17 voice frames, as the modem sends while its
// receiver hears a transmission.
func streamFrames(p *fakeModemPort, n int) {
	for range n {
		p.queue(modemMsg(mmdvmM17Stream, append([]byte{0xFF, 0x5D}, make([]byte, 49)...)))
	}
}

// TestStartupWhileReceiving: a hotspot started while its receiver hears M17
// traffic must still find the version reply and the ACK behind the frames.
func TestStartupWhileReceiving(t *testing.T) {
	p := &fakeModemPort{}
	streamFrames(p, 60) // 2.4 s of a voice stream already queued
	p.onWrite = func(cmd []byte) {
		streamFrames(p, 5)
		switch cmd[2] {
		case mmdvmGetVersion:
			p.queue(modemMsg(mmdvmGetVersion, append([]byte{1}, "MMDVM_HS_Dual_Hat-v1.6.1 20251011 14.7456MHz dual ADF7021"...)))
		default:
			p.queue(modemMsg(mmdvmACK, []byte{cmd[2]}))
		}
		streamFrames(p, 5)
	}
	m := &MMDVM{port: p, sendCmds: make(chan []byte, 10)}
	if err := m.readVersion(); err != nil {
		t.Fatalf("readVersion: %v", err)
	}
	if m.protocolVersion != 1 || m.hwType != mmdvmHWTypeMMDVM_HS_DUAL_HAT {
		t.Errorf("protocol %d, hardware %d; want 1, MMDVM_HS_Dual_Hat", m.protocolVersion, m.hwType)
	}
	streamFrames(p, 60)
	if err := m.setFrequency(444175000, 434175000, 0); err != nil {
		t.Errorf("setFrequency behind received frames: %v", err)
	}
}
