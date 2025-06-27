package m17

import (
	"bufio"
	"encoding/binary"
	"errors"
	"log"
	"math"
)

const (
	SymbolsPerSyncword = 8   //symbols per syncword
	SymbolsPerPayload  = 184 //symbols per payload in a frame
	SymbolsPerFrame    = 192 //symbols per whole 40 ms frame, 40ms * 4800 = 192
	BitsPerSymbol      = 2
	BitsPerPayload     = SymbolsPerPayload * BitsPerSymbol
)

const (
	PacketModeFinalBit = 5 // use 6 bits of final byte
	LSFFinalBit        = 7 // use entire final byte
)

const (
	ConvolutionK      = 5                         //constraint length K=5
	ConvolutionStates = (1 << (ConvolutionK - 1)) //number of states of the convolutional encoder
)

const (
	softTrue  = 1.0
	softMaybe = 0.5
	softFalse = 0.0
)

type Symbol float32

var (
	// TX symbols
	SymbolMap = []Symbol{+1, +3, -1, -3}

	// symbol list (RX)
	SymbolList = []Symbol{-3, -1, +1, +3}

	// End of Transmission symbol pattern
	EOTSymbols = []Symbol{+3, +3, +3, +3, +3, +3, -3, +3}

	costTable0 = []Symbol{softFalse, softFalse, softFalse, softFalse, softTrue, softTrue, softTrue, softTrue}
	costTable1 = []Symbol{softFalse, softTrue, softTrue, softFalse, softFalse, softTrue, softTrue, softFalse}
)

// Preamble type (0 for LSF, 1 for BERT).
type Preamble byte

const (
	lsfPreamble Preamble = iota
	bertPreamble
)

type Bit bool

func (b *Bit) Byte() byte {
	if *b {
		return 1
	}
	return 0
}
func (b *Bit) Set(by byte) {
	*b = by != 0
}

type Bits [BitsPerPayload]Bit

func NewBits(bs *[]Bit) *Bits {
	var bits Bits
	copy(bits[:], *bs)
	return &bits
}

type PuncturePattern []Bit

var LSFPuncturePattern = PuncturePattern{
	true, true, false, true, true, true, false, true,
	true, true, false, true, true, true, false, true,
	true, true, false, true, true, true, false, true,
	true, true, false, true, true, true, false, true,
	true, true, false, true, true, true, false, true,
	true, true, false, true, true, true, false, true,
	true, true, false, true, true, true, false, true,
	true, true, false, true, true,
}

var StreamPuncturePattern = PuncturePattern{true, true, true, true, true, true, true, true, true, true, true, false}

var PacketPuncturePattern = PuncturePattern{true, true, true, true, true, true, true, false}

// Calculate distance between recent samples and sync patterns
func syncDistance(in *bufio.Reader, offset int) (float32, uint16, error) {
	var lsf, pkt, str, bert float64

	symBuf, err := in.Peek(16*5*4 + offset*4) // Taking every fifth symbol 16 times, plus offset
	if err != nil {
		log.Printf("[ERROR] Error peeking sync symbols: %v", err)
		return 0, 0, err
	}
	symbols := make([]Symbol, 16*5)
	_, err = binary.Decode(symBuf[offset*4:], binary.LittleEndian, symbols)
	if err != nil {
		// should never happen
		log.Printf("[ERROR] Error decoding sync symbols: %v", err)
		return 0, 0, err
	}

	// log.Printf("[DEBUG] offset: %d, symbols: %#v", offset, symbols)
	// msg := "[DEBUG] sync: ["
	for i, s := range symbols {
		if i%5 == 0 {
			v := float64(s)
			// msg += fmt.Sprintf("%3.5f, ", v)
			lsf += math.Pow(v-ExtLSFSyncSymbols[i/5], 2)
			if i/5 < 8 {
				pkt += math.Pow(v-PacketSyncSymbols[i/5], 2)
			}
			// if i/5 > 7 {
			// 	str += math.Pow(v-StreamSyncSymbols[i/5-8], 2)
			// 	bert += math.Pow(v-BERTSyncSymbols[i/5-8], 2)
			// }
		}
	}
	lsf = math.Sqrt(lsf)
	pkt = math.Sqrt(pkt)
	// str = math.Sqrt(str)
	// bert = math.Sqrt(bert)
	// fmt.Printf(msg+"] lsf: %3.5f, pkt: %3.5f\n", lsf, pkt)

	switch min(lsf, pkt /*, str, bert*/) {
	case lsf:
		return float32(lsf), LSFSync, nil
	case pkt:
		return float32(pkt), PacketSync, nil
	case str:
		return float32(str), StreamSync, nil
	// case bert:
	default:
		return float32(bert), BERTSync, nil
	}
}

// func syncDistance(in *bufio.Reader, offset int) (float32, uint16, error) {
// 	var lsf, pkt, str, stra, strb float64

// 	symBuf, err := in.Peek(960 + 16*5*4 + offset*4) // Taking every fifth symbol 16 times, plus offset
// 	if err != nil {
// 		log.Printf("[ERROR] Error peeking sync symbols: %v", err)
// 		return 0, 0, err
// 	}
// 	symbols := make([]Symbol, 16*5)
// 	_, err = binary.Decode(symBuf[offset*4:], binary.LittleEndian, symbols)
// 	if err != nil {
// 		// should never happen
// 		log.Printf("[ERROR] Error decoding sync symbols: %v", err)
// 		return 0, 0, err
// 	}

// 	// log.Printf("[DEBUG] offset: %d, symbols: %#v", offset, symbols)
// 	// msg := "[DEBUG] sync: ["
// 	for i, s := range symbols {
// 		if i%5 == 0 {
// 			v := float64(s)
// 			// msg += fmt.Sprintf("%3.5f, ", v)
// 			lsf += math.Pow(v-ExtLSFSyncSymbols[i/5], 2)
// 			if i/5 < 8 {
// 				pkt += math.Pow(v-PacketSyncSymbols[i/5], 2)
// 			}
// 			if i/5 > 7 {
// 				stra += math.Pow(v-StreamSyncSymbols[i/5-8], 2)
// 				// bert += math.Pow(v-BERTSyncSymbols[i/5-8], 2)
// 			}
// 		}
// 	}
// 	_, err = binary.Decode(symBuf[960+offset*4:], binary.LittleEndian, symbols)
// 	if err != nil {
// 		// should never happen
// 		log.Printf("[ERROR] Error decoding sync symbols: %v", err)
// 		return 0, 0, err
// 	}
// 	for i, s := range symbols {
// 		if i%5 == 0 {
// 			v := float64(s)
// 			if i/5 > 7 {
// 				strb += math.Pow(v-StreamSyncSymbols[i/5-8], 2)
// 			}
// 		}
// 	}

// 	lsf = math.Sqrt(lsf)
// 	pkt = math.Sqrt(pkt)
// 	str = math.Sqrt(stra + strb)
// 	// fmt.Printf(msg+"] lsf: %3.5f, pkt: %3.5f\n", lsf, pkt)

// 	switch min(lsf, pkt, str /*, bert*/) {
// 	case lsf:
// 		return float32(lsf), LSFSync, nil
// 	case pkt:
// 		return float32(pkt), PacketSync, nil
// 	// case str:
// 	default:
// 		return float32(str), StreamSync, nil
// 	}
// }

// AppendPreamble generates symbol stream for a preamble.
func AppendPreamble(out []Symbol, typ Preamble) []Symbol {
	if typ == bertPreamble {
		for i := 0; i < SymbolsPerFrame/2; i++ {
			out = append(out, -3.0, +3.0)
		}
	} else {
		for i := 0; i < SymbolsPerFrame/2; i++ {
			out = append(out, +3.0, -3.0)
		}
	}
	return out
}

// AppendSyncword generates the symbol stream for a syncword.
func AppendSyncword(out []Symbol, syncword uint16) []Symbol {
	for i := 0; i < SymbolsPerSyncword*2; i += 2 {
		out = append(out, SymbolMap[(syncword>>(14-i))&3])
	}
	return out
}

var interleaveSequence = [BitsPerPayload]uint16{
	0, 137, 90, 227, 180, 317, 270, 39, 360, 129, 82, 219, 172, 309, 262, 31,
	352, 121, 74, 211, 164, 301, 254, 23, 344, 113, 66, 203, 156, 293, 246, 15,
	336, 105, 58, 195, 148, 285, 238, 7, 328, 97, 50, 187, 140, 277, 230, 367,
	320, 89, 42, 179, 132, 269, 222, 359, 312, 81, 34, 171, 124, 261, 214, 351,
	304, 73, 26, 163, 116, 253, 206, 343, 296, 65, 18, 155, 108, 245, 198, 335,
	288, 57, 10, 147, 100, 237, 190, 327, 280, 49, 2, 139, 92, 229, 182, 319,
	272, 41, 362, 131, 84, 221, 174, 311, 264, 33, 354, 123, 76, 213, 166, 303,
	256, 25, 346, 115, 68, 205, 158, 295, 248, 17, 338, 107, 60, 197, 150, 287,
	240, 9, 330, 99, 52, 189, 142, 279, 232, 1, 322, 91, 44, 181, 134, 271,
	224, 361, 314, 83, 36, 173, 126, 263, 216, 353, 306, 75, 28, 165, 118, 255,
	208, 345, 298, 67, 20, 157, 110, 247, 200, 337, 290, 59, 12, 149, 102, 239,
	192, 329, 282, 51, 4, 141, 94, 231, 184, 321, 274, 43, 364, 133, 86, 223,
	176, 313, 266, 35, 356, 125, 78, 215, 168, 305, 258, 27, 348, 117, 70, 207,
	160, 297, 250, 19, 340, 109, 62, 199, 152, 289, 242, 11, 332, 101, 54, 191,
	144, 281, 234, 3, 324, 93, 46, 183, 136, 273, 226, 363, 316, 85, 38, 175,
	128, 265, 218, 355, 308, 77, 30, 167, 120, 257, 210, 347, 300, 69, 22, 159,
	112, 249, 202, 339, 292, 61, 14, 151, 104, 241, 194, 331, 284, 53, 6, 143,
	96, 233, 186, 323, 276, 45, 366, 135, 88, 225, 178, 315, 268, 37, 358, 127,
	80, 217, 170, 307, 260, 29, 350, 119, 72, 209, 162, 299, 252, 21, 342, 111,
	64, 201, 154, 291, 244, 13, 334, 103, 56, 193, 146, 283, 236, 5, 326, 95,
	48, 185, 138, 275, 228, 365, 318, 87, 40, 177, 130, 267, 220, 357, 310, 79,
	32, 169, 122, 259, 212, 349, 302, 71, 24, 161, 114, 251, 204, 341, 294, 63,
	16, 153, 106, 243, 196, 333, 286, 55, 8, 145, 98, 235, 188, 325, 278, 47,
}

// Interleave payload bits.
func InterleaveBits(in *Bits) *Bits {
	var out Bits
	for i := 0; i < SymbolsPerPayload*2; i++ {
		out[i] = in[interleaveSequence[i]]
	}
	return &out
}
func DeinterleaveSymbols(symbols []Symbol) []Symbol {
	var dSymbols []Symbol
	for i := range SymbolsPerPayload * 2 {
		dSymbols = append(dSymbols, symbols[interleaveSequence[i]])
	}
	return dSymbols
}

var randomizeSeq = []byte{
	0xD6, 0xB5, 0xE2, 0x30, 0x82, 0xFF, 0x84, 0x62, 0xBA, 0x4E,
	0x96, 0x90, 0xD8, 0x98, 0xDD, 0x5D, 0x0C, 0xC8, 0x52, 0x43,
	0x91, 0x1D, 0xF8, 0x6E, 0x68, 0x2F, 0x35, 0xDA, 0x14, 0xEA,
	0xCD, 0x76, 0x19, 0x8D, 0xD5, 0x80, 0xD1, 0x33, 0x87, 0x13,
	0x57, 0x18, 0x2D, 0x29, 0x78, 0xC3,
}

func RandomizeBits(bits *Bits) *Bits {
	for i := 0; i < len(bits); i++ {
		if ((randomizeSeq[i/8] >> (7 - (i % 8))) & 1) != 0 {
			// flip bit
			bits[i] = !bits[i]
		}
	}
	return bits
}
func DerandomizeSymbols(symbols []Symbol) []Symbol {
	for i := 0; i < len(symbols); i++ {
		if (randomizeSeq[i/8]>>(7-(i%8)))&1 != 0 { //soft XOR. flip soft bit if "1"
			symbols[i] = softTrue - symbols[i]
		}
	}
	return symbols
}

func AppendBits(out []Symbol, data *Bits) []Symbol {
	for i := 0; i < SymbolsPerPayload; i++ { //40ms * 4800 - 8 (syncword)
		d := 0
		if data[2*i+1] {
			d += 1
		}
		if data[2*i] {
			d += 2
		}
		out = append(out, SymbolMap[d])
	}
	return out
}

// Generate symbol stream for the End of Transmission marker.
func AppendEOT(out []Symbol) []Symbol {
	for i := 0; i < SymbolsPerFrame; i++ { //40ms * 4800 = 192
		out = append(out, EOTSymbols[i%8])
	}
	return out
}

// ConvolutionalEncode takes a slice of bytes and a puncture pattern and returns an
// a slice of bool with each element representing one bit in the encoded message
//
// in 				Input bytes
// puncturePattern 	the puncture pattern to use
// finalBit 		The last bit of the final byte to encode. A number between 0 and 7. (That is, the number of bits from the last byte to use minus one.)
func ConvolutionalEncode(in []byte, puncturePattern PuncturePattern, finalBit byte) (*[]Bit, error) {
	if len(in) == 0 {
		return nil, errors.New("empty input not allowed")
	}
	if finalBit > 7 {
		return nil, errors.New("finalBits must be between 0 and 7")
	}
	unpackedBits := make([]byte, 4) // 4 leading bits
	for i, byt := range in {
		for j := 0; j < 8; j++ {
			if i < len(in)-1 || j <= int(finalBit) {
				unpackedBits = append(unpackedBits, (byt>>(7-j))&1)
			}
		}
	}
	// Add 4 tail bits
	for i := 0; i < 4; i++ {
		unpackedBits = append(unpackedBits, 0)
	}
	p := 0
	out := make([]Bit, 0, 2*len(unpackedBits))
	// log.Printf("[DEBUG] len(in): %d, len(unpackedBits): %d, len(out): %d", len(in), len(unpackedBits), len(out))
	ppLen := 1
	if puncturePattern != nil {
		ppLen = len(puncturePattern)
	}
	// pb := 0
	for i := range len(unpackedBits) - 4 {
		if puncturePattern == nil || puncturePattern[p] {
			g2 := (unpackedBits[i+4] + unpackedBits[i+1] + unpackedBits[i+0]) % 2
			out = append(out, g2 != 0)
		}

		p++
		p %= ppLen

		if puncturePattern == nil || puncturePattern[p] {
			g2 := (unpackedBits[i+4] + unpackedBits[i+3] + unpackedBits[i+2] + unpackedBits[i+0]) % 2
			out = append(out, g2 != 0)
		}

		p++
		p %= ppLen
	}
	// log.Printf("[DEBUG] len(out): %d", len(out))

	return &out, nil
}

type ViterbiDecoder struct {
	history []uint16

	prevMetrics     []float64
	currMetrics     []float64
	prevMetricsData []float32
	currMetricsData []float32
}

func (v *ViterbiDecoder) Init(l int) {
	v.history = make([]uint16, l/2+l%2)
	v.prevMetrics = make([]float64, ConvolutionStates)
	v.currMetrics = make([]float64, ConvolutionStates)
	v.prevMetricsData = make([]float32, ConvolutionStates)
	v.currMetricsData = make([]float32, ConvolutionStates)
}

func (v *ViterbiDecoder) DecodePunctured(puncturedSoftBits []Symbol, puncturePattern PuncturePattern) ([]byte, float64) {
	// log.Printf("[DEBUG] DecodePunctured len(puncturedSoftBits): %d, len(puncturePattern): %d", len(puncturedSoftBits), len(puncturePattern))
	// unpuncture input
	var softBits = make([]Symbol, 2*len(puncturedSoftBits))

	p := 0
	u := 0
	for i := 0; i < len(puncturedSoftBits); {
		if puncturePattern[p] {
			softBits[u] = puncturedSoftBits[i]
			i++
		} else {
			softBits[u] = softMaybe
		}
		u++
		p++
		p %= len(puncturePattern)
	}
	softBits = softBits[:u]

	out, e := v.decode(softBits)
	return out, e - float64(u-len(puncturedSoftBits))*softMaybe
}

func (v *ViterbiDecoder) decode(softBits []Symbol) ([]byte, float64) {
	// log.Printf("[DEBUG] decode() len(softBits): %d, softBits: %#v", len(softBits), softBits)
	v.Init(len(softBits))
	pos := 0
	for i := 0; i < len(softBits); i += 2 {
		sb0 := softBits[i]
		sb1 := softBits[i+1]
		// log.Printf("[DEBUG] decode i: %d", i)
		v.decodeBit(sb0, sb1, pos)
		pos++
	}

	out, e := v.chainback(pos, len(softBits)/2)
	// log.Printf("[DEBUG] decode() return len(out): %d, out: %#v, e: %f", len(out), out, e)
	return out, e
}

func (v *ViterbiDecoder) decodeBit(sb0, sb1 Symbol, pos int) {

	for i := 0; i < ConvolutionStates/2; i++ {
		metric := math.Abs(float64(costTable0[i]-sb0)) + math.Abs(float64(costTable1[i]-sb1))
		// log.Printf("[DEBUG] i: %d, sb0: %f, sb1: %f, metric: %f", i, sb0, sb1, metric)

		m0 := v.prevMetrics[i] + metric
		m1 := v.prevMetrics[i+ConvolutionStates/2] + (2.0 - metric)

		m2 := v.prevMetrics[i] + (2.0 - metric)
		m3 := v.prevMetrics[i+ConvolutionStates/2] + metric

		i0 := 2 * i
		i1 := i0 + 1

		if m0 >= m1 {
			v.history[pos] |= (1 << i0)
			v.currMetrics[i0] = m1
		} else {
			v.history[pos] &= ^(1 << i0)
			v.currMetrics[i0] = m0
		}

		if m2 >= m3 {
			v.history[pos] |= (1 << i1)
			v.currMetrics[i1] = m3
		} else {
			v.history[pos] &= ^(1 << i1)
			v.currMetrics[i1] = m2
		}
	}

	//swap
	tmp := make([]float64, ConvolutionStates)
	for i := 0; i < ConvolutionStates; i++ {
		tmp[i] = v.currMetrics[i]
	}
	for i := 0; i < ConvolutionStates; i++ {
		v.currMetrics[i] = v.prevMetrics[i]
		v.prevMetrics[i] = tmp[i]
	}
}

func (v *ViterbiDecoder) chainback(pos, l int) ([]byte, float64) {
	state := byte(0)
	bitPos := l + 4
	out := make([]byte, (l-1)/8+1)

	for pos > 0 {
		bitPos--
		pos--
		bit := v.history[pos] & (1 << (state >> 4))
		// log.Printf("[DEBUG] chainback pos: %d, bitPos: %d, v.history[pos]: %x, 1 << (state >> 4)): %x, bit: %x", pos, bitPos, v.history[pos], (1 << (state >> 4)), bit)
		state >>= 1
		if bit != 0 && bitPos/8 < len(out) {
			// log.Printf("[DEBUG] chainback pos: %d, bitPos: %d", pos, bitPos)
			state |= 0x80
			out[bitPos/8] |= 1 << (7 - (bitPos % 8))
		}
	}

	cost := v.prevMetrics[0]

	for i := 0; i < ConvolutionStates; i++ {
		m := v.prevMetrics[i]
		if m < cost {
			cost = m
		}
	}
	// log.Printf("[DEBUG] chainback(%d, %d) cost: %f", pos, l, cost)

	return out, cost
}
