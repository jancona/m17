package modem

import (
	"fmt"

	"github.com/jancona/m17"
)

const (
	samplesPerSecond = 24000
)

type Modem interface {
	StartDecoding(sink func(typ uint16, softBits []m17.SoftBit))
	Start() error
	Reset() error
	Close() error
	TransmitPacket(m17.Packet) error
	TransmitVoiceStream(m17.StreamDatagram) error
}

// processSymbolStream reads RRC-filtered symbols from rxSymbols, detects sync bursts,
// extracts payloads, and delivers soft bits to frameSink. Used by both CC1200 and SX1255 modems.
// sps is the number of samples per symbol (5 for CC1200, 1 for SX1255 after max-abs decimation).
func processSymbolStream(rxSymbols <-chan float32, frameSink func(typ uint16, softBits []m17.SoftBit), sps int) {
	var symbols []m17.Symbol

	// m17.Symbol buffer size: 8 preamble symbols, 8 for the syncword, and m17.SymbolsPerFrame for the payload,
	// times two for lookahead, floor(sps/2) extra for timing error correction, plus padding.
	bufSize := 8*sps + 2*(8*sps+m17.SymbolsPerFrame*sps) + sps/2 + 256

	// Diagnostic: track minimum sync distance seen per interval
	// var minDist float32 = 999
	// var minDistType uint16
	// diagTicker := time.NewTicker(5 * time.Second)
	// defer diagTicker.Stop()

	for {
		// Refill symbol buffer
		for range bufSize - len(symbols) {
			symbols = append(symbols, m17.Symbol(<-rxSymbols))
		}

		// Looking for a sync burst
		// calculate euclidean norm
		dist, typ := m17.SyncDistance(symbols, 0, sps)

		// // Track minimum distance for diagnostics
		// if dist < minDist {
		// 	minDist = dist
		// 	minDistType = typ
		// }
		// select {
		// case <-diagTicker.C:
		// 	typName := "?"
		// 	switch minDistType {
		// 	case m17.LSFSync:
		// 		typName = "LSF"
		// 	case m17.StreamSync:
		// 		typName = "Stream"
		// 	case m17.PacketSync:
		// 		typName = "Packet"
		// 	case m17.EOTMarker:
		// 		typName = "EOT"
		// 	}
		// 	log.Printf("[DEBUG] sync: minDist=%.2f type=%s (thresholds: LSF/EOT<4.5, Stream/Pkt<5.0)", minDist, typName)
		// 	minDist = 999
		// default:
		// }

		switch {
		case typ == m17.LSFSync && dist < 4.5:
			var pld []m17.SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols, sps)
			frameSink(typ, pld)

		case typ == m17.PacketSync && dist < 5.0:
			var pld []m17.SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols, sps)
			frameSink(typ, pld)

		case typ == m17.StreamSync && dist < 5.0:
			var pld []m17.SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols, sps)
			frameSink(typ, pld)

		case typ == m17.EOTMarker && dist < 4.5:
			symbols = symbols[16*sps:]
			frameSink(typ, nil)

		default:
			// No sync found, advance one symbol
			symbols = symbols[1:]
		}
	}
}

func extractPayload(dist float32, typ uint16, symbols []m17.Symbol, sps int) ([]m17.Symbol, []m17.SoftBit, float32) {
	offset := 0
	for i := range sps / 2 {
		d, t := m17.SyncDistance(symbols, i+1, sps)
		if t == typ && d < dist {
			dist = d
			offset = i + 1
		}
	}
	// skip offset
	symbols = symbols[offset:]
	// skip past sync
	symbols = symbols[16*sps:]
	pld := make([]m17.Symbol, m17.SymbolsPerPayload)
	for i := range pld {
		pld[i] = symbols[i*sps]
	}
	softBits := m17.CalcSoftbits(pld)
	// skip by most, but not all of the payload
	// if we skip everything we miss the next packet for some reason.
	symbols = symbols[(m17.SymbolsPerPayload-offset-16)*sps:]
	return symbols, softBits, dist
}

func generateLSFBits(l m17.LSF) ([]m17.Bit, error) {
	bits := unpackBits(m17.LSFSyncBytes)

	b, err := m17.ConvolutionalEncode(l.ToBytes(), m17.LSFPuncturePattern, m17.LSFFinalBit)
	if err != nil {
		return nil, fmt.Errorf("unable to encode LSF: %w", err)
	}
	encodedBits := m17.NewPayloadBits(b)
	// encodedBits[0:len(b)] = b[:]
	rfBits := m17.InterleaveBits(encodedBits)
	rfBits = m17.RandomizeBits(rfBits)
	// Append m17.LSF to the output
	bits = append(bits, rfBits[:]...)
	return bits, nil
}

func generateLSFSymbols(l *m17.LSF) ([]m17.Symbol, error) {
	// log.Printf("[DEBUG] generateLSFSymbols(%v)", *l)
	// bits, err := generateLSFBits(l)
	// if err != nil {
	// 	return nil, fmt.Errorf("unable to encode LSF: %w", err)
	// }
	// return m17.AppendBits(nil, m17.NewPayloadBits(bits)), nil
	syms := m17.AppendSyncwordSymbols(nil, m17.LSFSync)
	b, err := m17.ConvolutionalEncode(l.ToBytes(), m17.LSFPuncturePattern, m17.LSFFinalBit)
	if err != nil {
		return nil, fmt.Errorf("unable to encode LSF: %w", err)
	}
	encodedBits := m17.NewPayloadBits(b)
	// encodedBits[0:len(b)] = b[:]
	rfBits := m17.InterleaveBits(encodedBits)
	rfBits = m17.RandomizeBits(rfBits)
	// Append m17.LSF to the output
	syms = m17.AppendBits(syms, rfBits)
	return syms, err
}

func generateStreamBits(sd m17.StreamDatagram) ([]m17.Bit, error) {
	bits := unpackBits(m17.StreamSyncBytes)
	lich := extractLICH(int((sd.FrameNumber&0x7fff)%6), sd.LSF)
	encodedLICH := m17.EncodeLICH(lich)
	lichBits := unpackBits(encodedLICH)
	b, err := m17.ConvolutionalEncodeStream(lichBits, sd)
	if err != nil {
		return nil, fmt.Errorf("encode stream: %w", err)
	}
	encodedBits := m17.NewPayloadBits(b)
	rfBits := m17.InterleaveBits(encodedBits)
	rfBits = m17.RandomizeBits(rfBits)
	bits = append(bits, rfBits[:]...)
	return bits, nil
}

func generateStreamSymbols(sd m17.StreamDatagram) ([]m17.Symbol, error) {
	syms := m17.AppendSyncwordSymbols(nil, m17.StreamSync)
	lich := extractLICH(int((sd.FrameNumber&0x7fff)%6), sd.LSF)
	encodedLICH := m17.EncodeLICH(lich)
	lichBits := unpackBits(encodedLICH)
	b, err := m17.ConvolutionalEncodeStream(lichBits, sd)
	if err != nil {
		return syms, fmt.Errorf("encode stream: %w", err)
	}
	encodedBits := m17.NewPayloadBits(b)
	rfBits := m17.InterleaveBits(encodedBits)
	rfBits = m17.RandomizeBits(rfBits)
	syms = m17.AppendBits(syms, rfBits)
	// log.Printf("[DEBUG] len(syms): %d, syms: [% v]", len(syms), syms)
	return syms, nil
}

func extractLICH(lichCnt int, lsf *m17.LSF) []byte {
	lich := lsf.ToBytes()[lichCnt*5 : lichCnt*5+5]
	return append(lich, byte(lichCnt)<<5)
}

func unpackBits(in []byte) []m17.Bit {
	bits := make([]m17.Bit, 8*len(in))
	for i := range in {
		for j := range 8 {
			bits[i*8+j].Set((in[i] >> (7 - j)) & 1)
		}
	}
	return bits
}
func packBits(in []m17.Bit) []byte {
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
