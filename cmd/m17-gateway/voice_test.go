package main

import (
	"slices"
	"testing"
	"time"

	"github.com/jancona/m17"
	"github.com/jancona/m17/inet"
)

// TestNetVoiceWaitsForRF: reflector voice that arrives while a local radio
// is transmitting is held back, and the gateway joins the stream once no
// RF has been received for rxHoldoff.
func TestNetVoiceWaitsForRF(t *testing.T) {
	holdoff := 100 * time.Millisecond
	g, m := testGateway(t, holdoff, 10*time.Millisecond)
	g.inetClient = &inet.Client{}
	lsf, err := m17.NewLSF("@ALL", "W2JJT C", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	send := func(fn uint16) {
		sd := m17.NewStreamDatagram(0x1234, fn, &lsf, make([]byte, 16))
		if err := g.TransmitVoiceStream(sd); err != nil {
			t.Fatal(err)
		}
	}

	// Frames 0-4 arrive while a local radio is transmitting.
	for fn := range uint16(5) {
		g.rxHeard()
		send(fn)
		time.Sleep(40 * time.Millisecond)
	}
	if got := m.voiceFrames(); len(got) > 0 {
		t.Fatalf("frames %v transmitted while RF was being received", got)
	}
	// The radio has stopped, but the holdoff has not passed.
	send(5)
	if got := m.voiceFrames(); len(got) > 0 {
		t.Fatalf("frames %v transmitted within the holdoff", got)
	}
	time.Sleep(holdoff)
	for fn := uint16(6); fn < 9; fn++ {
		send(fn)
	}
	send(9 | 0x8000)
	if got, want := m.voiceFrames(), []uint16{6, 7, 8, 9 | 0x8000}; !slices.Equal(got, want) {
		t.Errorf("transmitted frames %v, want %v", got, want)
	}
}

// TestNetVoiceWaitsForSpeaking: reflector voice is held back while the
// gateway speaks, through overlapping holds, and joined, with a fresh LSF,
// once the last hold is released.
func TestNetVoiceWaitsForSpeaking(t *testing.T) {
	g, m := testGateway(t, 10*time.Millisecond, 10*time.Millisecond)
	g.inetClient = &inet.Client{}
	lsf, err := m17.NewLSF("@ALL", "W7GKB D", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	send := func(fn uint16) {
		sd := m17.NewStreamDatagram(0x6425, fn, &lsf, make([]byte, 16))
		if err := g.TransmitVoiceStream(sd); err != nil {
			t.Fatal(err)
		}
	}
	release := g.holdVoice()
	inner := g.holdVoice() // overlaps the first, and ends before it
	for fn := range uint16(3) {
		send(fn)
	}
	inner()
	for fn := uint16(3); fn < 5; fn++ {
		send(fn)
	}
	if got := m.voiceFrames(); len(got) > 0 {
		t.Fatalf("frames %v transmitted while the gateway spoke", got)
	}
	release()
	send(5)
	send(6 | 0x8000)
	if got, want := m.voiceFrames(), []uint16{5, 6 | 0x8000}; !slices.Equal(got, want) {
		t.Errorf("transmitted frames %v, want %v", got, want)
	}
}

// TestOwnVoiceNotInterrupted: reflector voice that arrives during an
// announcement or an echo waits for it to finish, instead of taking the
// transmitter from it frame by frame.
func TestOwnVoiceNotInterrupted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		speak func(g *Gateway) error
		want  []uint16 // the gateway's own frames
	}{
		{"announcement", func(g *Gateway) error {
			g.audioClips = map[string][]byte{"welcome": make([]byte, 2*16)}
			return g.playMessage("welcome")
		}, []uint16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10 | 0x8000}},
		{"echo", func(g *Gateway) error {
			lsf, err := m17.NewLSF("#ECHO", "N1ADJ", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
			if err != nil {
				return err
			}
			g.setState(Echo)
			g.echoStreamStart()
			for _, fn := range []uint16{0, 1, 2 | 0x8000} {
				l := lsf
				g.echoStreamRecord(m17.NewStreamDatagram(0x2222, fn, &l, make([]byte, 16)))
			}
			return g.echoStreamEnd()
		}, []uint16{0, 1, 2 | 0x8000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, m := testGateway(t, 10*time.Millisecond, 10*time.Millisecond)
			g.inetClient = &inet.Client{}
			cs, err := m17.EncodeCallsign("N1ADJ G")
			if err != nil {
				t.Fatal(err)
			}
			g.encodedCallsign = *cs
			lsf, err := m17.NewLSF("@ALL", "W7GKB D", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
			if err != nil {
				t.Fatal(err)
			}
			netFN := uint16(0x100)
			send := func(fn uint16) {
				sd := m17.NewStreamDatagram(0x6425, fn, &lsf, make([]byte, 16))
				if err := g.TransmitVoiceStream(sd); err != nil {
					t.Fatal(err)
				}
			}
			// A reflector frame arrives with each frame the gateway sends.
			arriving := true
			m.onVoice = func() {
				if arriving {
					arriving = false
					send(netFN)
					netFN++
					arriving = true
				}
			}
			if err := tc.speak(g); err != nil {
				t.Fatal(err)
			}
			arriving = false
			send(netFN | 0x8000)
			want := append(tc.want, netFN|0x8000)
			if got := m.voiceFrames(); !slices.Equal(got, want) {
				t.Errorf("transmitted frames %v, want %v", got, want)
			}
		})
	}
}

// TestNetVoiceAfterRFStreamEnd: once a local stream has ended, reflector
// voice waits only rfTurnaround, not the full rxHoldoff.
func TestNetVoiceAfterRFStreamEnd(t *testing.T) {
	g, m := testGateway(t, 5*rfTurnaround, 10*time.Millisecond)
	g.inetClient = &inet.Client{}
	lsf, err := m17.NewLSF("@ALL", "PARROT", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	send := func(fn uint16) {
		sd := m17.NewStreamDatagram(0x5678, fn, &lsf, make([]byte, 16))
		if err := g.TransmitVoiceStream(sd); err != nil {
			t.Fatal(err)
		}
	}

	g.rxHeard()
	g.rxStreamEnded()
	send(0)
	if got := m.voiceFrames(); len(got) > 0 {
		t.Fatalf("frames %v transmitted within rfTurnaround of the stream end", got)
	}
	time.Sleep(rfTurnaround + 20*time.Millisecond)
	send(1)
	if got, want := m.voiceFrames(), []uint16{1}; !slices.Equal(got, want) {
		t.Errorf("transmitted frames %v, want %v", got, want)
	}

	// RF heard again without a stream end: the full holdoff applies.
	g.rxHeard()
	time.Sleep(rfTurnaround + 20*time.Millisecond)
	send(2)
	if got, want := m.voiceFrames(), []uint16{1}; !slices.Equal(got, want) {
		t.Errorf("transmitted frames %v, want %v", got, want)
	}
}

// TestNetVoiceAfterRFLastFrame: a received last frame ends the local stream
// at once, without waiting for the decoder's end-of-stream report, which
// in duplex comes only after the repeat has gone out.
func TestNetVoiceAfterRFLastFrame(t *testing.T) {
	g, _ := testGateway(t, 5*rfTurnaround, 10*time.Millisecond)
	lsf, err := m17.NewLSF("PARROT", "N1ADJ", m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	g.receivedRFStreamFrame(lsf, make([]byte, 16), 0x1111, 20, 0)
	g.receivedRFStreamFrame(lsf, make([]byte, 16), 0x1111, 21|0x8000, 0)
	if !g.localRFActive() {
		t.Fatal("local RF not active right after its last frame")
	}
	// The decoder reports the end once the repeat has gone out. That must
	// not restart the turnaround.
	time.Sleep(rfTurnaround / 2)
	g.receivedRFStreamEOT(lsf, 0x1111, 21|0x8000, 0)
	time.Sleep(rfTurnaround/2 + 20*time.Millisecond)
	if g.localRFActive() {
		t.Error("local RF still active rfTurnaround after its last frame")
	}
}
