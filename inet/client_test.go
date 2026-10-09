package inet

import (
	"bytes"
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

// parrotReflector answers CONN with ACKN and reports each packet a client
// sends it. If echo returns true for a packet (numbered from 1), the
// packet is sent back readdressed to broadcast, as mrefd's PARROT does.
func parrotReflector(t *testing.T, echo func(n int) bool) (*net.UDPConn, chan m17.Packet) {
	t.Helper()
	refl, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { refl.Close() })
	pkts := make(chan m17.Packet, 16)
	go func() {
		n := 0
		for {
			buf := make([]byte, 1024)
			l, from, err := refl.ReadFromUDP(buf)
			if err != nil {
				return
			}
			buf = buf[:l]
			switch {
			case l >= 4 && string(buf[:4]) == m17.MagicCONN:
				refl.WriteToUDP([]byte(m17.MagicACKN), from)
			case l > 4 && string(buf[:4]) == m17.MagicM17Packet:
				p, err := m17.NewPacketFromBytes(buf[4:])
				if err != nil {
					t.Errorf("bad packet from client: %v", err)
					continue
				}
				n++
				pkts <- p
				if echo(n) {
					reply := p
					lsf := *p.LSF
					reply.LSF = &lsf
					reply.LSF.Dst = m17.EncodedDestinationAllBytes
					reply.LSF.CalcCRC()
					refl.WriteToUDP(append([]byte(m17.MagicM17Packet), reply.ToBytes()...), from)
				}
			}
		}
	}()
	return refl, pkts
}

func probingClient(t *testing.T, refl *net.UDPConn, handler func(m17.Packet) error) (*Client, chan string) {
	t.Helper()
	old := ProbeInterval
	ProbeInterval = 100 * time.Millisecond
	t.Cleanup(func() { ProbeInterval = old })
	events := make(chan string, 16)
	c, err := NewClient("M17-QTC", "127.0.0.1", uint(refl.LocalAddr().(*net.UDPAddr).Port), "A", "N1ADJ G",
		func(event, _ string, _ byte) { events <- event }, handler, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.ProbeReflector = true
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, events
}

func nextPacket(t *testing.T, pkts chan m17.Packet) m17.Packet {
	t.Helper()
	select {
	case p := <-pkts:
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("no packet from client")
	}
	return m17.Packet{}
}

// TestProbeCurrentReflector: on linking, the client sends PARROT a raw
// packet with valid CRCs; a reflector that echoes it is current, and the
// echo is not passed on.
func TestProbeCurrentReflector(t *testing.T) {
	refl, pkts := parrotReflector(t, func(int) bool { return true })
	passed := make(chan m17.Packet, 4)
	c, _ := probingClient(t, refl, func(p m17.Packet) error { passed <- p; return nil })

	p := nextPacket(t, pkts)
	if got := p.LSF.Dst.Callsign(); got != "PARROT" {
		t.Errorf("probe DST %q, want PARROT", got)
	}
	if got := p.LSF.Src.Callsign(); got != "N1ADJ G" {
		t.Errorf("probe SRC %q, want the linked callsign", got)
	}
	if p.Type != m17.PacketTypeRAW || len(p.Payload) != 8 {
		t.Errorf("probe type %v, payload % x; want a raw 8-byte tag", p.Type, p.Payload)
	}
	if !p.LSF.CheckCRC() || !p.CheckCRC() {
		t.Error("probe has a bad LSF or payload CRC")
	}
	// Wait past the point where an unanswered probe would make it legacy.
	time.Sleep(5 * ProbeInterval)
	if c.Legacy() {
		t.Error("reflector that echoed the probe is legacy")
	}
	select {
	case p := <-pkts:
		t.Errorf("probe resent after the reply: %v", p)
	default:
	}
	select {
	case p := <-passed:
		t.Errorf("probe reply passed to the packet handler: %v", p)
	default:
	}
}

// TestProbeLegacyReflector: a reflector that doesn't answer three probes
// is legacy (and current until then); a late answer makes it current.
func TestProbeLegacyReflector(t *testing.T) {
	refl, pkts := parrotReflector(t, func(n int) bool { return n == 4 })
	c, _ := probingClient(t, refl, nil)

	p1 := nextPacket(t, pkts)
	if c.Legacy() {
		t.Error("legacy before the probe went unanswered")
	}
	nextPacket(t, pkts)
	nextPacket(t, pkts)
	time.Sleep(3 * ProbeInterval / 2)
	if !c.Legacy() {
		t.Fatal("not legacy after three unanswered probes")
	}
	select {
	case p := <-pkts:
		t.Fatalf("fourth probe sent: %v", p)
	default:
	}

	// The reply to the first probe arrives late.
	p1.LSF.Dst = m17.EncodedDestinationAllBytes
	p1.LSF.CalcCRC()
	from := c.conn.LocalAddr().(*net.UDPAddr)
	refl.WriteToUDP(append([]byte(m17.MagicM17Packet), p1.ToBytes()...), from)
	deadline := time.Now().Add(2 * time.Second)
	for c.Legacy() {
		if time.Now().After(deadline) {
			t.Fatal("still legacy after a late reply")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestProbeOnRelink: each link is probed afresh, with a new tag.
func TestProbeOnRelink(t *testing.T) {
	old := connRetryInterval
	connRetryInterval = 100 * time.Millisecond
	t.Cleanup(func() { connRetryInterval = old })
	refl, pkts := parrotReflector(t, func(int) bool { return true })
	c, events := probingClient(t, refl, nil)
	waitEvent(t, events, "Connect")
	p1 := nextPacket(t, pkts)

	refl.WriteToUDP([]byte(m17.MagicDISC), c.conn.LocalAddr().(*net.UDPAddr))
	waitEvent(t, events, "Disconnect")
	waitEvent(t, events, "Connect")
	p2 := nextPacket(t, pkts)
	if bytes.Equal(p1.Payload, p2.Payload) {
		t.Error("relink probe reused the tag")
	}
}

// TestNoProbeByDefault: a client without ProbeReflector sends no probe.
func TestNoProbeByDefault(t *testing.T) {
	refl, pkts := parrotReflector(t, func(int) bool { return true })
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
	select {
	case p := <-pkts:
		t.Errorf("probe sent without ProbeReflector: %v", p)
	case <-time.After(300 * time.Millisecond):
	}
}
