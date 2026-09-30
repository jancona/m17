package inet

import (
	"net"
	"testing"
	"time"

	"github.com/jancona/m17"
)

// TestClientLinksToLateReflector: a client that starts before its
// reflector is listening (qtcd starting after m17-gateway at boot, say)
// keeps resending CONN and links once the reflector answers.
func TestClientLinksToLateReflector(t *testing.T) {
	old := connRetryInterval
	connRetryInterval = 100 * time.Millisecond
	t.Cleanup(func() { connRetryInterval = old })

	// Find a free port, and leave nothing listening on it for now.
	probe, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	events := make(chan string, 10)
	c, err := NewClient("M17-QTC", "127.0.0.1", uint(port), "A", "N1ADJ G",
		func(event, _ string, _ byte) { events <- event }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	// The reflector comes up late, and answers CONN with ACKN.
	time.Sleep(500 * time.Millisecond)
	refl, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { refl.Close() })
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, err := refl.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n >= 4 && string(buf[:4]) == m17.MagicCONN {
				refl.WriteToUDP([]byte(m17.MagicACKN), from)
			}
		}
	}()

	select {
	case ev := <-events:
		if ev != "Connect" {
			t.Errorf("first event %q, want Connect", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client never linked to the late reflector")
	}
}

// TestClientBacksOffCONN: a reflector that never answers gets CONNs at a
// growing interval, capped at maxConnRetryInterval.
func TestClientBacksOffCONN(t *testing.T) {
	oldRetry, oldMax := connRetryInterval, maxConnRetryInterval
	connRetryInterval, maxConnRetryInterval = 50*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { connRetryInterval, maxConnRetryInterval = oldRetry, oldMax })

	refl, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { refl.Close() })
	conns := make(chan time.Time, 32)
	go func() {
		buf := make([]byte, 64)
		for {
			n, _, err := refl.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n >= 4 && string(buf[:4]) == m17.MagicCONN {
				conns <- time.Now()
			}
		}
	}()

	c, err := NewClient("M17-M17", "127.0.0.1", uint(refl.LocalAddr().(*net.UDPAddr).Port), "C", "N1ADJ G", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	var at []time.Time
	for len(at) < 6 {
		select {
		case ts := <-conns:
			at = append(at, ts)
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d CONNs", len(at))
		}
	}
	// Waits after the first CONN: 100, 200, 200, 200 ms (the first resend
	// doubles 50 to 100). Allow for scheduling slack.
	for i, want := range []time.Duration{100, 200, 200, 200} {
		got := at[i+2].Sub(at[i+1])
		if got < want*time.Millisecond*8/10 || got > want*time.Millisecond*2 {
			t.Errorf("gap %d: %v, want about %vms", i+1, got, want)
		}
	}
}

// fakeReflector answers each CONN with the next reply in replies (ACKN once
// they run out), and reports what it sent on the returned channel.
func fakeReflector(t *testing.T, replies ...string) (*net.UDPConn, chan *net.UDPAddr) {
	t.Helper()
	refl, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { refl.Close() })
	clients := make(chan *net.UDPAddr, 16)
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, err := refl.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n >= 4 && string(buf[:4]) == m17.MagicCONN {
				reply := m17.MagicACKN
				if len(replies) > 0 {
					reply, replies = replies[0], replies[1:]
				}
				refl.WriteToUDP([]byte(reply), from)
				clients <- from
			}
		}
	}()
	return refl, clients
}

func waitEvent(t *testing.T, events chan string, want string) {
	t.Helper()
	for {
		select {
		case ev := <-events:
			if ev == want {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event", want)
		}
	}
}

// TestClientGivesUpAfterNACK: a reflector that refuses the link (NACK) is
// not asked again; the refusals the spec lists are not transient.
func TestClientGivesUpAfterNACK(t *testing.T) {
	old := connRetryInterval
	connRetryInterval = 50 * time.Millisecond
	t.Cleanup(func() { connRetryInterval = old })
	refl, clients := fakeReflector(t, m17.MagicNACK, m17.MagicNACK)

	events := make(chan string, 16)
	c, err := NewClient("M17-M17", "127.0.0.1", uint(refl.LocalAddr().(*net.UDPAddr).Port), "Z", "N1ADJ G",
		func(event, _ string, _ byte) { events <- event }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	waitEvent(t, events, "Disconnect")
	<-clients // the first CONN
	select {
	case <-clients:
		t.Error("client sent CONN again after a NACK")
	case <-time.After(time.Second):
	}
}

// TestClientRelinksAfterDISC: a reflector that drops a linked client (DISC)
// is asked to link again.
func TestClientRelinksAfterDISC(t *testing.T) {
	old := connRetryInterval
	connRetryInterval = 100 * time.Millisecond
	t.Cleanup(func() { connRetryInterval = old })
	refl, clients := fakeReflector(t)

	events := make(chan string, 16)
	c, err := NewClient("M17-QTC", "127.0.0.1", uint(refl.LocalAddr().(*net.UDPAddr).Port), "A", "N1ADJ G",
		func(event, _ string, _ byte) { events <- event }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	waitEvent(t, events, "Connect")
	from := <-clients
	refl.WriteToUDP([]byte(m17.MagicDISC), from)
	waitEvent(t, events, "Disconnect")
	waitEvent(t, events, "Connect")
}
