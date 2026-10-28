package modem

import (
	"errors"
	"io"
	"slices"
	"testing"
	"time"
)

func TestReconnectDelay(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, d := range want {
		if got := reconnectDelay(i + 1); got != d {
			t.Errorf("reconnectDelay(%d) = %v, want %v", i+1, got, d)
		}
	}
}

// A driver on a fake port that records the delays it sleeps through
// instead of waiting them out.
func newReconnectTestModem(p *fakeModemPort) (*MMDVM, *[]time.Duration) {
	var slept []time.Duration
	m := &MMDVM{port: p, sendCmds: make(chan []byte, 10), portName: "fake"}
	m.sleep = func(d time.Duration) { slept = append(slept, d) }
	return m, &slept
}

// Wait for modemReceive to return, failing the test if it does not.
func waitReceive(t *testing.T, done <-chan struct{}, limit time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("modemReceive still running after %v", limit)
	}
}

// TestModemReceiveReconnects: when the port fails the driver closes it and
// reopens it, waiting longer each time, sets the modem up again and goes on
// with what the modem sends afterwards. Commands queued meanwhile are
// dropped, as they are stale by then.
func TestModemReceiveReconnects(t *testing.T) {
	gone := errors.New("read /dev/ttyACM0: device not configured")
	first := &fakeModemPort{readErr: gone}
	back := &fakeModemPort{}
	back.answerHandshake()
	m, slept := newReconnectTestModem(first)

	opens := 0
	m.openPort = func() (io.ReadWriteCloser, error) {
		opens++
		if opens < 3 {
			return nil, errors.New("open /dev/ttyACM0: no such file or directory")
		}
		return back, nil
	}
	m.sendCmds <- modemMsg(mmdvmM17Stream, make([]byte, 49))
	m.setSpace(4)

	done := make(chan struct{})
	go func() { m.modemReceive(); close(done) }()

	// The modem is back once the driver reads from the new port.
	deadline := time.Now().Add(5 * time.Second)
	for m.getPort() != back || m.isGone() {
		if time.Now().After(deadline) {
			t.Fatal("the driver did not move to the reopened port")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !first.closed {
		t.Error("the failed port was not closed")
	}
	if first.reads != 1 {
		t.Errorf("the failed port was read %d times, want 1", first.reads)
	}
	if opens != 3 {
		t.Errorf("port opened %d times, want 3", opens)
	}
	// 1 s, then 2 s, then 4 s, slept a second at a time.
	want := []time.Duration{time.Second, time.Second, time.Second,
		time.Second, time.Second, time.Second, time.Second}
	if !slices.Equal(*slept, want) {
		t.Errorf("slept %v, want %v", *slept, want)
	}
	if len(m.sendCmds) != 0 {
		t.Errorf("%d commands kept across the outage, want 0", len(m.sendCmds))
	}
	if m.getSpace() != 0 {
		t.Errorf("space %d after the outage, want 0 until the modem reports it", m.getSpace())
	}

	// Shutting down works as usual on the new port.
	m.setExit(true)
	back.fail(errors.New("port closed"))
	waitReceive(t, done, time.Second)
}

// TestSendToModemDropsWhileGone: nothing is queued for a modem that is
// being reopened, so that no stale frames go out when it is back.
func TestSendToModemDropsWhileGone(t *testing.T) {
	m := &MMDVM{sendCmds: make(chan []byte, 10)}
	m.setGone(true)
	m.sendToModem(modemMsg(mmdvmM17Stream, make([]byte, 49)))
	if len(m.sendCmds) != 0 {
		t.Errorf("%d commands queued while the modem is gone, want 0", len(m.sendCmds))
	}
	m.setGone(false)
	m.sendToModem(modemMsg(mmdvmM17Stream, make([]byte, 49)))
	if len(m.sendCmds) != 1 {
		t.Errorf("%d commands queued with the modem back, want 1", len(m.sendCmds))
	}
}

// TestCloseDuringReconnect: Close() while the driver waits to try the port
// again ends the wait within a second.
func TestCloseDuringReconnect(t *testing.T) {
	first := &fakeModemPort{readErr: errors.New("device not configured")}
	m, slept := newReconnectTestModem(first)
	m.openPort = func() (io.ReadWriteCloser, error) {
		return nil, errors.New("no such file or directory")
	}
	m.sleep = func(d time.Duration) {
		*slept = append(*slept, d)
		if len(*slept) == 5 { // in the 8 s wait before the fourth try
			m.setExit(true)
		}
	}

	done := make(chan struct{})
	go func() { m.modemReceive(); close(done) }()
	waitReceive(t, done, time.Second)
	if len(*slept) != 5 {
		t.Errorf("slept %d times after Close(), want 5", len(*slept))
	}
}

// TestCloseWhileReopening: Close() while the port is being opened closes
// the old port, so the driver closes the new one once it is open.
func TestCloseWhileReopening(t *testing.T) {
	first := &fakeModemPort{readErr: errors.New("device not configured")}
	back := &fakeModemPort{}
	back.answerHandshake()
	m, _ := newReconnectTestModem(first)
	m.openPort = func() (io.ReadWriteCloser, error) {
		m.Close()
		return back, nil
	}

	done := make(chan struct{})
	go func() { m.modemReceive(); close(done) }()
	waitReceive(t, done, 5*time.Second)
	if !back.closed {
		t.Error("the port opened during Close() was left open")
	}
}

// TestModemReceiveQuietOnClose: when Close() closes the port, the loop's
// read error is the expected one: no reopening, no complaint.
func TestModemReceiveQuietOnClose(t *testing.T) {
	p := &fakeModemPort{}
	m, _ := newReconnectTestModem(p)
	opened := false
	m.openPort = func() (io.ReadWriteCloser, error) { opened = true; return p, nil }

	done := make(chan struct{})
	go func() { m.modemReceive(); close(done) }()
	time.Sleep(20 * time.Millisecond)

	// Close() marks the exit before the port fails, as the real one does.
	m.setExit(true)
	p.fail(errors.New("port closed"))
	waitReceive(t, done, time.Second)
	if opened {
		t.Error("the port was reopened after Close()")
	}
}
