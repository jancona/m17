package main

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jancona/m17"
	"github.com/jancona/m17/inet"
)

// eventReporter records voice and packet reports as short strings.
type eventReporter struct {
	nopReporter
	mu     sync.Mutex
	events []string
}

func (r *eventReporter) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *eventReporter) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (r *eventReporter) voiceStart(source string, lsf *m17.LSF, late bool, _ float64) {
	r.add(fmt.Sprintf("start %s %s>%s", source, strings.TrimSpace(lsf.Src.Callsign()), strings.TrimSpace(lsf.Dst.Callsign())))
}

func (r *eventReporter) voiceEnd(source string, _ *m17.LSF, frames int, _ float64, lost bool) {
	r.add(fmt.Sprintf("end %s %d", source, frames))
}

func (r *eventReporter) packet(source string, lsf *m17.LSF, _ *m17.Packet, _ float64) {
	r.add(fmt.Sprintf("packet %s %s>%s", source, strings.TrimSpace(lsf.Src.Callsign()), strings.TrimSpace(lsf.Dst.Callsign())))
}

func ownTXGateway(t *testing.T) (*Gateway, *recordingModem, *eventReporter) {
	t.Helper()
	g, m := testGateway(t, 10*time.Millisecond, 10*time.Millisecond)
	g.inetClient = &inet.Client{}
	cs, err := m17.EncodeCallsign("N1ADJ G")
	if err != nil {
		t.Fatal(err)
	}
	g.encodedCallsign = *cs
	r := &eventReporter{}
	g.rep = r
	return g, m, r
}

func recordEcho(t *testing.T, g *Gateway, frames ...uint16) {
	t.Helper()
	g.setState(Echo)
	g.echoStreamStart()
	for _, fn := range frames {
		lsf, err := m17.NewLSF("#ECHO", "N1ADJ", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
		if err != nil {
			t.Fatal(err)
		}
		g.echoStreamRecord(m17.NewStreamDatagram(0x2222, fn, &lsf, make([]byte, 16)))
	}
}

// TestOwnVoiceReported: the gateway's echoes and spoken messages are
// reported as network voice, from the gateway, as MMDVMHost reports what
// M17Gateway sends it.
func TestOwnVoiceReported(t *testing.T) {
	t.Run("echo", func(t *testing.T) {
		g, _, r := ownTXGateway(t)
		recordEcho(t, g, 0, 1, 2|0x8000)
		if err := g.echoStreamEnd(); err != nil {
			t.Fatal(err)
		}
		if got, want := r.got(), []string{"start network N1ADJ G>N1ADJ", "end network 3"}; !slices.Equal(got, want) {
			t.Errorf("reports %q, want %q", got, want)
		}
	})
	t.Run("message", func(t *testing.T) {
		g, m, r := ownTXGateway(t)
		g.audioClips = map[string][]byte{"welcome": make([]byte, 2*16)}
		if err := g.playMessage("welcome"); err != nil {
			t.Fatal(err)
		}
		frames := len(m.voiceFrames())
		if got, want := r.got(), []string{"start network N1ADJ G>@ALL", fmt.Sprintf("end network %d", frames)}; !slices.Equal(got, want) {
			t.Errorf("reports %q, want %q", got, want)
		}
	})
}

// TestOwnPacketsReported: echo and info replies are reported as network
// packets when they go out.
func TestOwnPacketsReported(t *testing.T) {
	g, m, r := ownTXGateway(t)
	if err := g.echoPacket(smsPacket(t, "hi")); err != nil {
		t.Fatal(err)
	}
	if err := g.infoPacket(smsPacket(t, "info")); err != nil {
		t.Fatal(err)
	}
	waitSent(t, m, 2)
	if got, want := r.got(), []string{"packet network N1ADJ G>W1AW", "packet network N1ADJ G>W1AW"}; !slices.Equal(got, want) {
		t.Errorf("reports %q, want %q", got, want)
	}
}

// TestOwnVoiceReportsDontOverlap: reported network voice never overlaps.
// A reflector stream arriving during an echo ends the echo's report first;
// an echo while a reflector stream is reported isn't reported.
func TestOwnVoiceReportsDontOverlap(t *testing.T) {
	refl, err := m17.NewLSF("@ALL", "W7GKB D", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("reflector during echo", func(t *testing.T) {
		g, m, r := ownTXGateway(t)
		recordEcho(t, g, 0, 1, 2, 3|0x8000)
		// A reflector frame arrives with the echo's second frame
		sent := 0
		m.onVoice = func() {
			sent++
			if sent == 2 {
				l := refl
				if err := g.TransmitVoiceStream(m17.NewStreamDatagram(0x6425, 0, &l, make([]byte, 16))); err != nil {
					t.Error(err)
				}
			}
		}
		if err := g.echoStreamEnd(); err != nil {
			t.Fatal(err)
		}
		got := r.got()
		if len(got) != 3 || got[0] != "start network N1ADJ G>N1ADJ" || !strings.HasPrefix(got[1], "end network ") || got[2] != "start network W7GKB D>@ALL" {
			t.Errorf("reports %q", got)
		}
	})
	t.Run("echo during reflector", func(t *testing.T) {
		g, _, r := ownTXGateway(t)
		l := refl
		if err := g.TransmitVoiceStream(m17.NewStreamDatagram(0x6425, 0, &l, make([]byte, 16))); err != nil {
			t.Fatal(err)
		}
		recordEcho(t, g, 0, 1|0x8000)
		if err := g.echoStreamEnd(); err != nil {
			t.Fatal(err)
		}
		if got, want := r.got(), []string{"start network W7GKB D>@ALL"}; !slices.Equal(got, want) {
			t.Errorf("reports %q, want %q", got, want)
		}
	})
}
