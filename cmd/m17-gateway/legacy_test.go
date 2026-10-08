package main

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jancona/m17"
	"github.com/jancona/m17/inet"
)

// fakeReflector links any client, records the streams and packets it is
// sent, and, if parrot is set, echoes probes to PARROT readdressed to
// broadcast as mrefd does. Probes are not recorded.
type fakeReflector struct {
	conn    *net.UDPConn
	mu      sync.Mutex
	streams []m17.EncodedCallsign // DST of each stream frame
	packets []m17.Packet
	probes  int
}

func newFakeReflector(t *testing.T, parrot bool) *fakeReflector {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	r := &fakeReflector{conn: conn}
	go func() {
		for {
			buf := make([]byte, 1024)
			l, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			buf = buf[:l]
			if l < 4 {
				continue
			}
			switch string(buf[:4]) {
			case m17.MagicCONN:
				conn.WriteToUDP([]byte(m17.MagicACKN), from)
			case m17.MagicM17Stream:
				sd, err := m17.NewStreamDatagramFromBytes(buf)
				if err != nil {
					t.Errorf("bad stream datagram: %v", err)
					continue
				}
				r.mu.Lock()
				r.streams = append(r.streams, sd.LSF.Dst)
				r.mu.Unlock()
			case m17.MagicM17Packet:
				p, err := m17.NewPacketFromBytes(buf[4:])
				if err != nil {
					t.Errorf("bad packet: %v", err)
					continue
				}
				if p.LSF.Dst.Callsign() == "PARROT" && p.Type == m17.PacketTypeRAW {
					r.mu.Lock()
					r.probes++
					r.mu.Unlock()
					if parrot {
						p.LSF.Dst = m17.EncodedDestinationAllBytes
						p.LSF.CalcCRC()
						conn.WriteToUDP(append([]byte(m17.MagicM17Packet), p.ToBytes()...), from)
					}
					continue
				}
				r.mu.Lock()
				r.packets = append(r.packets, p)
				r.mu.Unlock()
			}
		}
	}()
	return r
}

func (r *fakeReflector) received() ([]m17.EncodedCallsign, []m17.Packet, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]m17.EncodedCallsign(nil), r.streams...), append([]m17.Packet(nil), r.packets...), r.probes
}

// lsfModem records the DST of each voice frame it transmits.
type lsfModem struct {
	capturingModem
	dsts []m17.EncodedCallsign
}

func (m *lsfModem) TransmitVoiceStream(sd m17.StreamDatagram) error {
	m.mu.Lock()
	m.dsts = append(m.dsts, sd.LSF.Dst)
	m.mu.Unlock()
	return m.capturingModem.TransmitVoiceStream(sd)
}

// linkedGateway is a duplex gateway linked to a fake reflector, returned
// once the reflector's probe is settled: answered if parrot, else legacy.
func linkedGateway(t *testing.T, parrot bool) (*Gateway, *lsfModem, *fakeReflector) {
	t.Helper()
	old := inet.ProbeInterval
	inet.ProbeInterval = 50 * time.Millisecond
	t.Cleanup(func() { inet.ProbeInterval = old })

	refl := newFakeReflector(t, parrot)
	m := &lsfModem{}
	g := &Gateway{modem: m, state: Idle, lastStreamID: 0xFFFF, duplex: true,
		callsign: "N1ADJ G", packetQueue: make(chan func() error, packetQueueLen)}
	cs, err := m17.EncodeCallsign("N1ADJ G")
	if err != nil {
		t.Fatal(err)
	}
	g.encodedCallsign = *cs
	go g.sendPackets()
	t.Cleanup(func() { close(g.packetQueue) })

	g.inetClient, err = inet.NewClient("M17-QTC", "127.0.0.1", uint(refl.conn.LocalAddr().(*net.UDPAddr).Port), "A",
		g.callsign, nil, g.queuePacket, g.TransmitVoiceStream)
	if err != nil {
		t.Fatal(err)
	}
	g.inetClient.ProbeReflector = true
	if err := g.inetClient.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.inetClient.Close() })

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, probes := refl.received()
		if parrot && probes > 0 {
			time.Sleep(50 * time.Millisecond) // for the reply
			break
		}
		if !parrot && g.inetClient.Legacy() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reflector probe not settled")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if g.inetClient.Legacy() == parrot {
		t.Fatalf("Legacy() = %v for a reflector that answers PARROT: %v", !parrot, parrot)
	}
	return g, m, refl
}

// rfStream has the gateway receive a three-frame RF stream to dst.
func rfStream(t *testing.T, g *Gateway, dst string) {
	t.Helper()
	lsf, err := m17.NewLSF("@ALL", "W1AW", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	lsf.Dst = encoded(t, dst)
	lsf.CalcCRC()
	g.receivedRFLSF(lsf, 0)
	for fn := range uint16(3) {
		if fn == 2 {
			fn |= 0x8000
		}
		g.receivedRFStreamFrame(lsf, make([]byte, 16), 0x4321, fn, 0)
	}
	g.receivedRFStreamEOT(lsf, 0x4321, 0x8002, 0)
}

// waitStreams waits until the reflector has received n stream frames.
func waitStreams(t *testing.T, refl *fakeReflector, n int) []m17.EncodedCallsign {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		streams, _, _ := refl.received()
		if len(streams) >= n {
			return streams
		}
		if time.Now().After(deadline) {
			t.Fatalf("reflector received %d stream frames, want %d", len(streams), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// encoded encodes cs. EncodeCallsign rejects ALL, which a radio may still
// send, so that one is encoded here.
func encoded(t *testing.T, cs string) m17.EncodedCallsign {
	t.Helper()
	if cs == "ALL" {
		const chars = " ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-/."
		var a uint64
		for i := len(cs) - 1; i >= 0; i-- {
			a = a*40 + uint64(strings.IndexByte(chars, cs[i]))
		}
		var e m17.EncodedCallsign
		for i := 5; i >= 0; i-- {
			e[i] = byte(a)
			a >>= 8
		}
		return e
	}
	e, err := m17.EncodeCallsign(cs)
	if err != nil {
		t.Fatal(err)
	}
	return *e
}

// TestLegacyBroadcastReaddressed: a broadcast stream to a legacy reflector
// is addressed to the reflector and module, and repeated on RF as received.
func TestLegacyBroadcastReaddressed(t *testing.T) {
	for _, dst := range []string{"@ALL", "ALL", "#ALL"} {
		t.Run(dst, func(t *testing.T) {
			g, m, refl := linkedGateway(t, false)
			rfStream(t, g, dst)
			want := *g.inetClient.EncodedName
			for i, got := range waitStreams(t, refl, 3) {
				if got != want {
					t.Errorf("frame %d to reflector: DST %s, want %s", i, got.Callsign(), want.Callsign())
				}
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(m.dsts) != 3 {
				t.Fatalf("repeated %d frames, want 3", len(m.dsts))
			}
			for i, got := range m.dsts {
				if got != encoded(t, dst) {
					t.Errorf("frame %d repeated on RF: DST %s, want %s", i, got.Callsign(), dst)
				}
			}
		})
	}
}

// TestLegacyDirectedUnchanged: a directed stream to a legacy reflector is
// sent unchanged.
func TestLegacyDirectedUnchanged(t *testing.T) {
	g, _, refl := linkedGateway(t, false)
	rfStream(t, g, "PARROT")
	for i, got := range waitStreams(t, refl, 3) {
		if got.Callsign() != "PARROT" {
			t.Errorf("frame %d to reflector: DST %s, want PARROT", i, got.Callsign())
		}
	}
}

// TestCurrentStreamsUnchanged: streams to a current reflector keep their DST.
func TestCurrentStreamsUnchanged(t *testing.T) {
	g, _, refl := linkedGateway(t, true)
	rfStream(t, g, "@ALL")
	rfStream(t, g, "PARROT")
	streams := waitStreams(t, refl, 6)
	for i, want := range []string{"@ALL", "@ALL", "@ALL", "PARROT", "PARROT", "PARROT"} {
		if got := streams[i].Callsign(); got != want {
			t.Errorf("frame %d to reflector: DST %s, want %s", i, got, want)
		}
	}
}

// TestLegacyPacketsNotSent: packets go to a current reflector but not a
// legacy one, and a duplex gateway repeats them either way.
func TestLegacyPacketsNotSent(t *testing.T) {
	for _, parrot := range []bool{true, false} {
		g, m, refl := linkedGateway(t, parrot)
		receive(t, g, "W1AW", "hello")
		waitSent(t, &m.recordingModem, 1)
		time.Sleep(100 * time.Millisecond)
		_, packets, _ := refl.received()
		want := 0
		if parrot {
			want = 1
		}
		if len(packets) != want {
			t.Errorf("reflector answering PARROT %v: received %d packets, want %d", parrot, len(packets), want)
		}
	}
}
