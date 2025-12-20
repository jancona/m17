package m17

import (
	"fmt"
)

const (
	samplesPerSecond = 24000
)

type Modem interface {
	StartDecoding(sink func(typ uint16, softBits []SoftBit))
	Start() error
	Reset() error
	Close() error
	TransmitPacket(Packet) error
	TransmitVoiceStream(StreamDatagram) error
}

func extractPayload(dist float32, typ uint16, symbols []Symbol) ([]Symbol, []SoftBit, float32) {
	offset := 0
	for i := range 2 {
		d, t := syncDistance(symbols, i+1)
		if t == typ && d < dist {
			dist = d
			offset = i + 1
		}
	}
	// skip offset
	symbols = symbols[offset:]
	// skip past sync
	symbols = symbols[16*5:]
	pld := make([]Symbol, SymbolsPerPayload)
	for i := range pld {
		pld[i] = symbols[i*5]
	}
	softBits := calcSoftbits(pld)
	// skip by most, but not all of the payload
	// if we skip everything we miss the next packet for some reason.
	symbols = symbols[(SymbolsPerPayload-offset-16)*5:]
	return symbols, softBits, dist
}

func generateLSFBits(l LSF) ([]Bit, error) {
	bits := unpackBits(LSFSyncBytes)

	b, err := ConvolutionalEncode(l.ToBytes(), LSFPuncturePattern, LSFFinalBit)
	if err != nil {
		return nil, fmt.Errorf("unable to encode LSF: %w", err)
	}
	encodedBits := NewPayloadBits(b)
	// encodedBits[0:len(b)] = b[:]
	rfBits := InterleaveBits(encodedBits)
	rfBits = RandomizeBits(rfBits)
	// Append LSF to the output
	bits = append(bits, rfBits[:]...)
	return bits, nil
}

func generateLSFSymbols(l *LSF) ([]Symbol, error) {
	// log.Printf("[DEBUG] generateLSFSymbols(%v)", *l)
	// bits, err := generateLSFBits(l)
	// if err != nil {
	// 	return nil, fmt.Errorf("unable to encode LSF: %w", err)
	// }
	// return AppendBits(nil, NewPayloadBits(bits)), nil
	syms := AppendSyncwordSymbols(nil, LSFSync)
	b, err := ConvolutionalEncode(l.ToBytes(), LSFPuncturePattern, LSFFinalBit)
	if err != nil {
		return nil, fmt.Errorf("unable to encode LSF: %w", err)
	}
	encodedBits := NewPayloadBits(b)
	// encodedBits[0:len(b)] = b[:]
	rfBits := InterleaveBits(encodedBits)
	rfBits = RandomizeBits(rfBits)
	// Append LSF to the output
	syms = AppendBits(syms, rfBits)
	return syms, err
}

func generateStreamBits(sd StreamDatagram) ([]Bit, error) {
	bits := unpackBits(StreamSyncBytes)
	lich := extractLICH(int((sd.FrameNumber&0x7fff)%6), sd.LSF)
	encodedLICH := EncodeLICH(lich)
	lichBits := unpackBits(encodedLICH)
	b, err := ConvolutionalEncodeStream(lichBits, sd)
	if err != nil {
		return nil, fmt.Errorf("encode stream: %w", err)
	}
	encodedBits := NewPayloadBits(b)
	rfBits := InterleaveBits(encodedBits)
	rfBits = RandomizeBits(rfBits)
	bits = append(bits, rfBits[:]...)
	return bits, nil
}

func generateStreamSymbols(sd StreamDatagram) ([]Symbol, error) {
	syms := AppendSyncwordSymbols(nil, StreamSync)
	lich := extractLICH(int((sd.FrameNumber&0x7fff)%6), sd.LSF)
	encodedLICH := EncodeLICH(lich)
	lichBits := unpackBits(encodedLICH)
	b, err := ConvolutionalEncodeStream(lichBits, sd)
	if err != nil {
		return syms, fmt.Errorf("encode stream: %w", err)
	}
	encodedBits := NewPayloadBits(b)
	rfBits := InterleaveBits(encodedBits)
	rfBits = RandomizeBits(rfBits)
	syms = AppendBits(syms, rfBits)
	// log.Printf("[DEBUG] len(syms): %d, syms: [% v]", len(syms), syms)
	return syms, nil
}

func extractLICH(lichCnt int, lsf *LSF) []byte {
	lich := lsf.ToBytes()[lichCnt*5 : lichCnt*5+5]
	return append(lich, byte(lichCnt)<<5)
}

func unpackBits(in []byte) []Bit {
	bits := make([]Bit, 8*len(in))
	for i := range in {
		for j := range 8 {
			bits[i*8+j].Set((in[i] >> (7 - j)) & 1)
		}
	}
	return bits
}
func packBits(in []Bit) []byte {
	// log.Printf("[DEBUG] packBits in: % v", in)
	bytes := make([]byte, len(in)/8)
	for i := range bytes {
		for j := range 8 {
			if in[8*i+j] {
				bytes[i] |= 1 << (7 - j)
			}
		}
	}
	// log.Printf("[DEBUG] packBits out: % 02x", bytes)
	return bytes
}
