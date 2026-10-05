package m17

import (
	"slices"
	"testing"
)

// encodeStreamFrame returns the soft bits a perfect receiver would see for
// stream frame fn of a stream described by lsf, with its LICH chunk.
func encodeStreamFrame(t *testing.T, lsf *LSF, fn uint16) []SoftBit {
	t.Helper()
	cnt := int(fn&0x7fff) % 6
	lich := make([]byte, 6)
	copy(lich, lsf.ToBytes()[cnt*5:cnt*5+5])
	lich[5] = byte(cnt) << 5
	var lichBits []Bit
	for _, by := range EncodeLICH(lich) {
		for j := 7; j >= 0; j-- {
			var b Bit
			b.Set((by >> j) & 1)
			lichBits = append(lichBits, b)
		}
	}
	payload := make([]byte, 16)
	payload[0] = byte(fn)
	b, err := ConvolutionalEncodeStream(lichBits, NewStreamDatagram(0, fn, lsf, payload))
	if err != nil {
		t.Fatal(err)
	}
	return CalcSoftbits(AppendBits(nil, RandomizeBits(InterleaveBits(NewPayloadBits(b)))))
}

func streamLSF(t *testing.T, src string) *LSF {
	t.Helper()
	l, err := NewLSF("@ALL", src, LSFTypeStream, LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatal(err)
	}
	return &l
}

// streamEvent is one decoder callback: "frame", "lich" or "end".
type streamEvent struct {
	kind string
	src  string
	sid  uint16
	fn   uint16
}

func recordingDecoder(events *[]streamEvent) *Decoder {
	return NewDecoder(
		func(LSF, float64) error { return nil },
		func(l LSF, _ []byte, sid, fn uint16, _ float64) error {
			*events = append(*events, streamEvent{"frame", l.Src.Callsign(), sid, fn})
			return nil
		},
		func(l LSF, _ float64) error {
			*events = append(*events, streamEvent{"lich", l.Src.Callsign(), 0, 0})
			return nil
		},
		func(l LSF, sid, fn uint16, _ float64) error {
			*events = append(*events, streamEvent{"end", l.Src.Callsign(), sid, fn})
			return nil
		},
		nil,
	)
}

// TestDecoderStreamSwitchWithoutLSF: a transmitter that switches to another
// caller's stream mid-transmission, with no new LSF and the frame number
// starting over, gets every frame through. Once the LICH has rebuilt the
// new LSF, the old stream ends and a new one starts.
//
// Here the first LSF rebuilt after the switch mixes chunks of both (slots 4
// and 5 come from the old stream's frames 106 and 107), fails its CRC and is
// discarded. Frames 4-9 then rebuild the new one, so it starts at frame 9.
func TestDecoderStreamSwitchWithoutLSF(t *testing.T) {
	a, b := streamLSF(t, "W2JJT"), streamLSF(t, "AA5RL")
	var ev []streamEvent
	d := recordingDecoder(&ev)
	d.DecodeFrame(LSFSync, encodeSoftBits(t, a.ToBytes(), LSFPuncturePattern, LSFFinalBit))
	for fn := uint16(100); fn < 110; fn++ {
		d.DecodeFrame(StreamSync, encodeStreamFrame(t, a, fn))
	}
	for fn := range uint16(12) {
		d.DecodeFrame(StreamSync, encodeStreamFrame(t, b, fn))
	}

	var frames []uint16
	var aSID, bSID uint16
	var ended, started bool
	for _, e := range ev {
		switch e.kind {
		case "frame":
			frames = append(frames, e.fn)
			if e.src == "W2JJT" {
				aSID = e.sid
			} else {
				bSID = e.sid
			}
		case "end":
			// The old stream's last frame is the new caller's frame 8,
			// the last one delivered under it.
			if e.src != "W2JJT" || e.sid != aSID || e.fn != 9|0x8000 {
				t.Errorf("end event %+v, want W2JJT stream %04x ending at frame %04x", e, aSID, 9|0x8000)
			}
			ended = true
		case "lich":
			if e.src == "AA5RL" {
				if !ended {
					t.Error("new stream started before the old one ended")
				}
				started = true
			}
		}
	}
	want := []uint16{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if !slices.Equal(frames, want) {
		t.Errorf("delivered frames %v, want %v", frames, want)
	}
	if !ended || !started {
		t.Fatalf("old stream ended %v, new stream started %v; want both", ended, started)
	}
	if bSID == aSID {
		t.Errorf("new stream kept stream ID %04x", aSID)
	}
	// Frames 0-8 arrive before the new LSF is complete and go out under the
	// old one; from frame 9, whose LICH chunk completes it, under the new.
	for _, e := range ev {
		if e.kind == "frame" && e.fn < 100 {
			if wantSrc := map[bool]string{true: "W2JJT", false: "AA5RL"}[e.fn < 9]; e.src != wantSrc {
				t.Errorf("frame %d delivered as %s, want %s", e.fn, e.src, wantSrc)
			}
		}
	}
}

// TestDecoderDropsRepeatedFrame: the same frame number twice in a row is
// delivered once.
func TestDecoderDropsRepeatedFrame(t *testing.T) {
	a := streamLSF(t, "W2JJT")
	var ev []streamEvent
	d := recordingDecoder(&ev)
	d.DecodeFrame(LSFSync, encodeSoftBits(t, a.ToBytes(), LSFPuncturePattern, LSFFinalBit))
	for _, fn := range []uint16{0, 1, 1, 2, 4} {
		d.DecodeFrame(StreamSync, encodeStreamFrame(t, a, fn))
	}
	var frames []uint16
	for _, e := range ev {
		if e.kind == "frame" {
			frames = append(frames, e.fn)
		}
	}
	if want := []uint16{0, 1, 2, 4}; !slices.Equal(frames, want) {
		t.Errorf("delivered frames %v, want %v", frames, want)
	}
}

// TestDecoderLICHStartedStreamEnds: a stream whose LSF was missed, picked up
// from the LICH, still ends on an EOT marker.
func TestDecoderLICHStartedStreamEnds(t *testing.T) {
	a := streamLSF(t, "W2JJT")
	var ev []streamEvent
	d := recordingDecoder(&ev)
	for fn := uint16(20); fn < 28; fn++ {
		d.DecodeFrame(StreamSync, encodeStreamFrame(t, a, fn))
	}
	d.DecodeFrame(EOTMarker, nil)
	if n := len(ev); n == 0 || ev[n-1].kind != "end" || ev[n-1].fn != 28|0x8000 {
		t.Errorf("events %+v, want an end at frame %04x last", ev, 28|0x8000)
	}
}
