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

// processSymbolStream reads RRC-filtered symbols from rxSymbols, detects sync bursts,
// extracts payloads, and delivers soft bits to frameSink. Used by both CC1200 and SX1255 modems.
// sps is the number of samples per symbol (5 for CC1200, 1 for SX1255 after max-abs decimation).
func processSymbolStream(rxSymbols <-chan float32, frameSink func(typ uint16, softBits []SoftBit), sps int) {
	var symbols []Symbol

	// Symbol buffer size: 8 preamble symbols, 8 for the syncword, and SymbolsPerFrame for the payload,
	// times two for lookahead, floor(sps/2) extra for timing error correction, plus padding.
	bufSize := 8*sps + 2*(8*sps+SymbolsPerFrame*sps) + sps/2 + 256

	// Diagnostic: track minimum sync distance seen per interval
	// var minDist float32 = 999
	// var minDistType uint16
	// diagTicker := time.NewTicker(5 * time.Second)
	// defer diagTicker.Stop()

	for {
		// Refill symbol buffer
		for range bufSize - len(symbols) {
			symbols = append(symbols, Symbol(<-rxSymbols))
		}

		// Looking for a sync burst
		// calculate euclidean norm
		dist, typ := syncDistance(symbols, 0, sps)

		// // Track minimum distance for diagnostics
		// if dist < minDist {
		// 	minDist = dist
		// 	minDistType = typ
		// }
		// select {
		// case <-diagTicker.C:
		// 	typName := "?"
		// 	switch minDistType {
		// 	case LSFSync:
		// 		typName = "LSF"
		// 	case StreamSync:
		// 		typName = "Stream"
		// 	case PacketSync:
		// 		typName = "Packet"
		// 	case EOTMarker:
		// 		typName = "EOT"
		// 	}
		// 	log.Printf("[DEBUG] sync: minDist=%.2f type=%s (thresholds: LSF/EOT<4.5, Stream/Pkt<5.0)", minDist, typName)
		// 	minDist = 999
		// default:
		// }

		switch {
		case typ == LSFSync && dist < 4.5:
			var pld []SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols, sps)
			frameSink(typ, pld)

		case typ == PacketSync && dist < 5.0:
			var pld []SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols, sps)
			frameSink(typ, pld)

		case typ == StreamSync && dist < 5.0:
			var pld []SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols, sps)
			frameSink(typ, pld)

		case typ == EOTMarker && dist < 4.5:
			symbols = symbols[16*sps:]
			frameSink(typ, nil)

		default:
			// No sync found, advance one symbol
			symbols = symbols[1:]
		}
	}
}

func extractPayload(dist float32, typ uint16, symbols []Symbol, sps int) ([]Symbol, []SoftBit, float32) {
	offset := 0
	for i := range sps / 2 {
		d, t := syncDistance(symbols, i+1, sps)
		if t == typ && d < dist {
			dist = d
			offset = i + 1
		}
	}
	// skip offset
	symbols = symbols[offset:]
	// skip past sync
	symbols = symbols[16*sps:]
	pld := make([]Symbol, SymbolsPerPayload)
	for i := range pld {
		pld[i] = symbols[i*sps]
	}
	softBits := calcSoftbits(pld)
	// skip by most, but not all of the payload
	// if we skip everything we miss the next packet for some reason.
	symbols = symbols[(SymbolsPerPayload-offset-16)*sps:]
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
