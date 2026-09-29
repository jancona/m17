package m17

import (
	"bytes"
	"strings"
	"testing"
)

// encodeSoftBits runs data through the transmit chain (convolutional code,
// interleaver, randomiser, symbols) and back to soft bits, as a receiver
// with a perfect channel would see them.
func encodeSoftBits(t *testing.T, data []byte, puncture PuncturePattern, finalBit byte) []SoftBit {
	t.Helper()
	b, err := ConvolutionalEncode(data, puncture, finalBit)
	if err != nil {
		t.Fatalf("ConvolutionalEncode: %v", err)
	}
	rfBits := RandomizeBits(InterleaveBits(NewPayloadBits(b)))
	return CalcSoftbits(AppendBits(nil, rfBits))
}

// decodePacket feeds a packet to a Decoder frame by frame, split into
// 25-byte chunks the way the modems transmit it, and returns what the
// decoder delivered.
func decodePacket(t *testing.T, p *Packet) [][]byte {
	t.Helper()
	var got [][]byte
	d := NewDecoder(
		func(LSF, float64) error { return nil },
		nil, nil, nil,
		func(_ LSF, payload []byte, _ float64) error {
			got = append(got, bytes.Clone(payload))
			return nil
		},
	)
	d.DecodeFrame(LSFSync, encodeSoftBits(t, p.LSF.ToBytes(), LSFPuncturePattern, LSFFinalBit))
	data := p.PayloadBytes()
	for n := 0; n*25 < len(data); n++ {
		chunk := make([]byte, 26)
		left := copy(chunk, data[n*25:])
		if left > 25 {
			left = 25
		}
		if (n+1)*25 < len(data) {
			chunk[25] = byte(n << 2)
		} else {
			chunk[25] = byte(1<<7 | left<<2)
		}
		d.DecodeFrame(PacketSync, encodeSoftBits(t, chunk, PacketPuncturePattern, PacketModeFinalBit))
	}
	return got
}

func TestDecoderPacketLengths(t *testing.T) {
	// Up to the 33-frame (825-byte) maximum; frame offsets past 255 bytes
	// once overflowed a byte and panicked.
	for _, n := range []int{10, 21, 22, 150, 250, 251, 256, 300, 500, 700, 821} {
		text := strings.Repeat("x", n) + "\x00"
		p, err := NewPacket("N0CALL", "W1AW", PacketTypeSMS, []byte(text))
		if err != nil {
			t.Fatalf("NewPacket: %v", err)
		}
		got := decodePacket(t, p)
		if len(got) != 1 {
			t.Errorf("%d characters: decoded %d packets, want 1", n, len(got))
			continue
		}
		if want := p.PayloadBytes(); !bytes.Equal(got[0], want) {
			t.Errorf("%d characters: decoded %d bytes, want %d", n, len(got[0]), len(want))
		}
	}
}
