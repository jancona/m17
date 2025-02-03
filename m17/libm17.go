package m17

import (
	"container/ring"
	"fmt"
	"log"
	"math"

	"github.com/sigurn/crc16"
)

// m17.h
/**
 * @brief Preamble type (0 for LSF, 1 for BERT).
 */
type Pream byte

const (
	PREAM_LSF Pream = iota
	PREAM_BERT
)

// M17 C library - frame type
/**
 * @brief Frame type (0 - LSF, 1 - stream, 2 - packet).
 */
type Frame byte

const (
	FRAME_LSF Frame = iota
	FRAME_STR
	FRAME_PKT
)

// M17 C library - payload
/**
 * @brief Structure holding Link Setup Frame data.
 */
type LSF struct {
	Dst  [6]uint8
	Src  [6]uint8
	Type [2]uint8
	Meta [112 / 8]uint8
	CRC  [2]uint8
}

const (
	// M17 C library - lib/lib.c
	BasebandSamplesPerSymbol = 10                                              //samples per symbol
	FltSpan                  = 8                                               //baseband RRC filter span in symbols
	SymbolsPerSyncword       = 8                                               //symbols per syncword
	SyncwordLen              = (BasebandSamplesPerSymbol * SymbolsPerSyncword) //syncword detector length
	SymbolsPerPayload        = 184                                             //symbols per payload in a frame
	SymbolsPerFrame          = 192                                             //symbols per whole 40 ms frame
	// RRCDeviation             = 7168.0                                          //.rrc file deviation for +1.0 symbol
)

const (
	LSFSync    = uint16(0x55F7)
	StreamSync = uint16(0xFF5D)
	PacketSync = uint16(0x75FF)
	BERTSync   = uint16(0xDF55)
	EOTMarker  = uint16(0x555D)
)

var (
	LSFSyncSymbols    = []float64{+3, +3, +3, +3, -3, -3, +3, -3}
	StreamSyncSymbols = []float64{-3, -3, -3, -3, +3, +3, -3, +3}
	PacketSyncSymbols = []float64{+3, -3, +3, +3, -3, -3, -3, -3}
	BERTSyncSymbols   = []float64{-3, +3, -3, -3, +3, +3, +3, +3}
)

// decode/symbols.c
// dibits-symbols map (TX)
var (
	SymbolMap = []float32{+1, +3, -1, -3}

	// symbol list (RX)
	SymbolList = []float32{-3, -1, +1, +3}

	// End of Transmission symbol pattern
	EOTSymbols = []float32{+3, +3, +3, +3, +3, +3, -3, +3}
)

// Calculate distance between recent samples and sync patterns
func SyncDistance(r *ring.Ring) (float32, uint16) {
	var lsf, pkt, str, bert float64

	for i := 0; i < 8; i++ {
		var v float64
		if r.Value != nil {
			v = float64(r.Value.(float32))
		}
		lsf += math.Pow(v-LSFSyncSymbols[i], 2)
		pkt += math.Pow(v-PacketSyncSymbols[i], 2)
		str += math.Pow(v-StreamSyncSymbols[i], 2)
		bert += math.Pow(v-BERTSyncSymbols[i], 2)
		r = r.Next()
	}

	switch min(lsf, pkt, str, bert) {
	case bert:
		return float32(bert), BERTSync
	case pkt:
		return float32(pkt), PacketSync
	case str:
		return float32(str), StreamSync
	// case lsf:
	default:
		return float32(lsf), LSFSync
	}
}

// Utility function returning the absolute value of a difference between two fixed-point values.
func QAbsDiff(v1 uint16, v2 uint16) uint16 {
	if v2 > v1 {
		return v2 - v1
	}
	return v1 - v2
}

// randomize.c

// randomizing pattern
var RandSeq = []uint8{
	0xD6, 0xB5, 0xE2, 0x30, 0x82, 0xFF, 0x84, 0x62, 0xBA, 0x4E, 0x96, 0x90, 0xD8, 0x98, 0xDD, 0x5D, 0x0C, 0xC8, 0x52, 0x43, 0x91, 0x1D, 0xF8,
	0x6E, 0x68, 0x2F, 0x35, 0xDA, 0x14, 0xEA, 0xCD, 0x76, 0x19, 0x8D, 0xD5, 0x80, 0xD1, 0x33, 0x87, 0x13, 0x57, 0x18, 0x2D, 0x29, 0x78, 0xC3,
}

/**
 * @brief Randomize type-4 unpacked bits.
 *
 * @param inp Input 368 unpacked type-4 bits.
 */
func randomize_bits(inp *[SymbolsPerPayload * 2]uint8) {
	for i := 0; i < SymbolsPerPayload*2; i++ {
		if ((RandSeq[i/8] >> (7 - (i % 8))) & 1) != 0 { //flip bit if '1'
			if inp[i] != 0 {
				inp[i] = 0
			} else {
				inp[i] = 1
			}
		}
	}
}

// phy/interleave.c
// interleaver pattern
var IntrlSeq = []uint16{
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

// convol.c

// P_3 puncture pattern for packet frames.
var PuncturePattern1 = []uint8{
	1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1,
	1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1,
	1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1,
	1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1,
}
var PuncturePattern3 = []uint8{1, 1, 1, 1, 1, 1, 1, 0}

/**
 * @brief Encode M17 stream frame using convolutional encoder with puncturing.
 *
 * @param out Output array, unpacked.
 * @param in Input - pointer to a struct holding the Link Setup Frame.
 */
func conv_encode_LSF(out *[SymbolsPerPayload * 2]uint8, in *LSF) {
	p := 0                    //puncturing pattern index
	pb := uint16(0)           //pushed punctured bits
	var ud [240 + 4 + 4]uint8 //unpacked data

	//unpack DST
	for i := 0; i < 8; i++ {
		ud[4+i] = ((in.Dst[0]) >> (7 - i)) & 1
		ud[4+i+8] = ((in.Dst[1]) >> (7 - i)) & 1
		ud[4+i+16] = ((in.Dst[2]) >> (7 - i)) & 1
		ud[4+i+24] = ((in.Dst[3]) >> (7 - i)) & 1
		ud[4+i+32] = ((in.Dst[4]) >> (7 - i)) & 1
		ud[4+i+40] = ((in.Dst[5]) >> (7 - i)) & 1
	}

	//unpack SRC
	for i := 0; i < 8; i++ {
		ud[4+i+48] = ((in.Src[0]) >> (7 - i)) & 1
		ud[4+i+56] = ((in.Src[1]) >> (7 - i)) & 1
		ud[4+i+64] = ((in.Src[2]) >> (7 - i)) & 1
		ud[4+i+72] = ((in.Src[3]) >> (7 - i)) & 1
		ud[4+i+80] = ((in.Src[4]) >> (7 - i)) & 1
		ud[4+i+88] = ((in.Src[5]) >> (7 - i)) & 1
	}

	//unpack TYPE
	for i := 0; i < 8; i++ {
		ud[4+i+96] = ((in.Type[0]) >> (7 - i)) & 1
		ud[4+i+104] = ((in.Type[1]) >> (7 - i)) & 1
	}

	//unpack META
	for i := 0; i < 8; i++ {
		ud[4+i+112] = ((in.Meta[0]) >> (7 - i)) & 1
		ud[4+i+120] = ((in.Meta[1]) >> (7 - i)) & 1
		ud[4+i+128] = ((in.Meta[2]) >> (7 - i)) & 1
		ud[4+i+136] = ((in.Meta[3]) >> (7 - i)) & 1
		ud[4+i+144] = ((in.Meta[4]) >> (7 - i)) & 1
		ud[4+i+152] = ((in.Meta[5]) >> (7 - i)) & 1
		ud[4+i+160] = ((in.Meta[6]) >> (7 - i)) & 1
		ud[4+i+168] = ((in.Meta[7]) >> (7 - i)) & 1
		ud[4+i+176] = ((in.Meta[8]) >> (7 - i)) & 1
		ud[4+i+184] = ((in.Meta[9]) >> (7 - i)) & 1
		ud[4+i+192] = ((in.Meta[10]) >> (7 - i)) & 1
		ud[4+i+200] = ((in.Meta[11]) >> (7 - i)) & 1
		ud[4+i+208] = ((in.Meta[12]) >> (7 - i)) & 1
		ud[4+i+216] = ((in.Meta[13]) >> (7 - i)) & 1
	}

	//unpack CRC
	for i := 0; i < 8; i++ {
		ud[4+i+224] = ((in.CRC[0]) >> (7 - i)) & 1
		ud[4+i+232] = ((in.CRC[1]) >> (7 - i)) & 1
	}

	//encode
	for i := 0; i < 240+4; i++ {
		G1 := (ud[i+4] + ud[i+1] + ud[i+0]) % 2
		G2 := (ud[i+4] + ud[i+3] + ud[i+2] + ud[i+0]) % 2

		//printf("%d%d", G1, G2);

		if PuncturePattern1[p] != 0 {
			out[pb] = G1
			pb++
		}

		p++
		p %= len(PuncturePattern1)

		if PuncturePattern1[p] != 0 {
			out[pb] = G2
			pb++
		}

		p++
		p %= len(PuncturePattern1)
	}

	//printf("pb=%d\n", pb);
}

// viterbi.c
const (
	ConvolutionK      = 5                         //constraint length K=5
	ConvolutionStates = (1 << (ConvolutionK - 1)) //number of states of the convolutional encoder
)

var (
	prevMetrics     = make([]uint32, ConvolutionStates)
	currMetrics     = make([]uint32, ConvolutionStates)
	prevMetricsData = make([]uint32, ConvolutionStates)
	currMetricsData = make([]uint32, ConvolutionStates)
	viterbiHistory  = make([]uint16, 244)
)

// Decode unpunctured convolutionally encoded data.
func ViterbiDecode(out []uint8, in []uint16) (int, error) {
	if len(in) > 244*2 {
		return 0, fmt.Errorf("input size %d exceeds max history", len(in))
	}

	ViterbiReset()

	pos := 0
	for i := 0; i < len(in); i += 2 {
		s0 := in[i]
		s1 := in[i+1]

		ViterbiDecodeBit(s0, s1, pos)
		pos++
	}
	// m := ""
	// for i := 0; i < len(viterbiHistory); i++ {
	// 	m += fmt.Sprintf("%04X", viterbiHistory[i])
	// }
	// log.Printf("[DEBUG] viterbiHistory: %s", m)

	return int(ViterbiChainback(out, pos, len(in)/2)), nil
}

// Decode punctured convolutionally encoded data.
func ViterbiDecodePunctured(out []uint8, in []uint16, punct []uint8) (int, error) {
	if len(in) > 244*2 {
		return 0, fmt.Errorf("input size %d exceeds max history", len(in))
	}
	log.Printf("[DEBUG] ViterbiDecodePunctured len(out): %d, len(in): %d, len(punct): %d", len(out), len(in), len(punct))

	umsg := make([]uint16, 244*2) //unpunctured message
	p := 0                        //puncturer matrix entry
	u := 0                        //bits count - unpunctured message
	i := 0                        //bits read from the input message

	for i < len(in) {
		// log.Printf("i: %d, p: %d, u: %d", i, p, u)

		if punct[p] != 0 {
			umsg[u] = in[i]
			// log.Printf("punct[%d]: %d\numsg[%d]=in[%d]: %d", p, punct[p], u, i, umsg[u])
			i++
		} else {
			umsg[u] = 0x7FFF
			// log.Printf("punct[%d]: %d\numsg[%d]=0x7FFF: %d", p, punct[p], u, umsg[u])
		}

		u++
		p++
		p %= len(punct)
	}
	umsg = umsg[:u]
	// m := ""
	// for i := 0; i < len(umsg); i++ {
	// 	m += fmt.Sprintf("%04X", umsg[i])
	// }
	// log.Printf("[DEBUG] u: %d, len(umsg): %d", u, len(umsg))

	ret, err := ViterbiDecode(out, umsg)
	if err != nil {
		return 0, err
	}
	return ret - (u-len(in))*0x7FFF, nil
}

// Decode one bit and update trellis.
func ViterbiDecodeBit(s0 uint16, s1 uint16, pos int) {
	COST_TABLE_0 := []uint16{0, 0, 0, 0, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF}
	COST_TABLE_1 := []uint16{0, 0xFFFF, 0xFFFF, 0, 0, 0xFFFF, 0xFFFF, 0}

	for i := 0; i < ConvolutionStates/2; i++ {
		metric := uint32(QAbsDiff(COST_TABLE_0[i], s0)) + uint32(QAbsDiff(COST_TABLE_1[i], s1))
		// log.Printf("[DEBUG] i: %d, metric: %d", i, metric)

		m0 := prevMetrics[i] + metric
		m1 := prevMetrics[i+ConvolutionStates/2] + (0x1FFFE - metric)

		m2 := prevMetrics[i] + (0x1FFFE - metric)
		m3 := prevMetrics[i+ConvolutionStates/2] + metric

		i0 := 2 * i
		i1 := i0 + 1

		if m0 >= m1 {
			viterbiHistory[pos] |= (1 << i0)
			currMetrics[i0] = m1
		} else {
			viterbiHistory[pos] &= ^(1 << i0)
			currMetrics[i0] = m0
		}

		if m2 >= m3 {
			viterbiHistory[pos] |= (1 << i1)
			currMetrics[i1] = m3
		} else {
			viterbiHistory[pos] &= ^(1 << i1)
			currMetrics[i1] = m2
		}
	}

	//swap
	tmp := make([]uint32, ConvolutionStates)
	for i := 0; i < ConvolutionStates; i++ {
		tmp[i] = currMetrics[i]
	}
	for i := 0; i < ConvolutionStates; i++ {
		currMetrics[i] = prevMetrics[i]
		prevMetrics[i] = tmp[i]
	}
}

// History chainback to obtain final byte array.
func ViterbiChainback(out []uint8, pos int, l int) uint32 {
	state := uint8(0)
	bitPos := l + 4

	//  memset(out, 0, (len-1)/8+1);
	for i := 0; i < len(out); i++ {
		out[i] = 0
	}

	for pos > 0 {
		bitPos--
		pos--
		bit := viterbiHistory[pos] & (1 << (state >> 4))
		state >>= 1
		if bit != 0 {
			state |= 0x80
			out[bitPos/8] |= 1 << (7 - (bitPos % 8))
		}
	}

	cost := prevMetrics[0]

	for i := 0; i < ConvolutionStates; i++ {
		m := prevMetrics[i]
		if m < cost {
			cost = m
		}
	}

	return cost
}

// Reset the decoder state. No args.
func ViterbiReset() {
	// viterbiHistory = make([]uint16, 244)
	// currMetrics = make([]uint32, ConvolutionStates)
	// prevMetrics = make([]uint32, ConvolutionStates)
	// currMetricsData = make([]uint32, ConvolutionStates)
	// prevMetricsData = make([]uint32, ConvolutionStates)

	for i := range viterbiHistory {
		viterbiHistory[i] = 0
	}
	for i := 0; i < ConvolutionStates; i++ {
		currMetrics[i] = 0
		prevMetrics[i] = 0
		currMetricsData[i] = 0
		prevMetricsData[i] = 0
	}

	// memset((uint8_t*)viterbi_history, 0, 2*244);
	// memset((uint8_t*)currMetrics, 0, 4*M17_CONVOL_STATES);
	// memset((uint8_t*)prevMetrics, 0, 4*M17_CONVOL_STATES);
	// memset((uint8_t*)currMetricsData, 0, 4*M17_CONVOL_STATES);
	// memset((uint8_t*)prevMetricsData, 0, 4*M17_CONVOL_STATES);

}

//

// M17 CRC polynomial
var m17CRCParams = crc16.Params{
	Poly: 0x5935,
	Init: 0xffff,
	Name: "M17",
}

// Calculate CRC value.
func CRC(in []uint8) bool {
	table := crc16.MakeTable(m17CRCParams)

	return crc16.Checksum(in, table) == 0
}
