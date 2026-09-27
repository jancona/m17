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
