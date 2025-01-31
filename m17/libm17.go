package m17

import (
	"fmt"
	"io"
	"log"
	"math"
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

// sync.c
const (
	SYNC_LSF = uint16(0x55F7)
	SYNC_STR = uint16(0xFF5D)
	SYNC_PKT = uint16(0x75FF)
	SYNC_BER = uint16(0xDF55)
	EOT_MRKR = uint16(0x555D)
)

// encode/symbols.c
// syncword patterns (RX)
// TODO: Compute those at runtime from the consts below
var (
	LsfSyncSymbols = []int8{+3, +3, +3, +3, -3, -3, +3, -3}
	StrSyncSymbols = []int8{-3, -3, -3, -3, +3, +3, -3, +3}
	PktSyncSymbols = []int8{+3, -3, +3, +3, -3, -3, -3, -3}
)

// decode/symbols.c
// dibits-symbols map (TX)
var (
	SymbolMap = []int8{+1, +3, -1, -3}

	// symbol list (RX)
	SymbolList = []int8{-3, -1, +1, +3}

	// End of Transmission symbol pattern
	EOTSymbols = []int8{+3, +3, +3, +3, +3, +3, -3, +3}
)

// math.c

// Calculate L2 norm between two n-dimensional vectors of floats.
func EuclNorm(in1 []float32, in2 []int8) float32 {
	var tmp float32

	for i := range in1 {
		tmp += (in1[i] - float32(in2[i])) * (in1[i] - float32(in2[i]))
	}

	return float32(math.Sqrt(float64(tmp)))
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
	// log.Printf("[DEBUG] ViterbiDecodePunctured len(out): %d, len(in): %d, len(punct): %d", len(out), len(in), len(punct))

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
	// log.Printf("[DEBUG] u: %d, len(umsg): %d, umsg: %s", u, len(umsg), m)

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
	if (l-1)/8+1 > len(out) {
		log.Printf("[INFO] Prevented array overflow (l-1)/8+1: %d len(out): %d\n(out: %#v, pos: %d, l: %d)", (l-1)/8+1, len(out), out, pos, l)
	}
	for i := 0; i < min(len(out), (l-1)/8+1); i++ {
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
const M17CRCPoly = 0x5935

// Calculate CRC value.
func CRC(in []uint8) bool {
	var crc uint32 = 0xFFFF //init val

	for i := range in {
		crc ^= uint32(in[i]) << 8
		for j := 0; j < 8; j++ {
			crc <<= 1
			if crc&0x10000 != 0 {
				crc = (crc ^ M17CRCPoly) & 0xFFFF
			}
		}
	}

	return uint16(crc&(0xFFFF)) == 0
}

func SendPacket(lsf LSF, packetData []byte, out io.Writer) error {
	// var full_packet = make([]float32, 0, 36*192*10)   //full packet, symbols as floats - 36 "frames" max (incl. preamble, LSF, EoT), 192 symbols each, sps=10:
	// var enc_bits = make([]uint8, SymbolsPerPayload*2) //type-2 bits, unpacked
	// var rf_bits = make([]uint8, SymbolsPerPayload*2)  //type-4 bits, unpacked
	// //encode LSF data
	// conv_encode_LSF(enc_bits, &lsf)
	// //fill preamble
	// // memset((uint8_t*)full_packet, 0, 36*192*10*sizeof(float));
	// AppendPreamble(full_packet, PREAM_LSF)

	// //send LSF syncword
	// AppendSyncword(full_packet, SYNC_LSF)

	// //reorder bits
	// reorder_bits(rf_bits, enc_bits)

	// //randomize
	// randomize_bits(rf_bits)

	// //fill packet with LSF
	// gen_data(full_packet, &pkt_sym_cnt, rf_bits)

	// for numBytes := len(packet); numBytes > 0; {
	// 	//send packet frame syncword
	// 	GenSyncword(packet, &pkt_sym_cnt, SYNC_PKT)

	// 	//the following examples produce exactly 25 bytes, which exactly one frame, but >= meant this would never produce a final frame with EOT bit set
	// 	//echo -en "\x05Testing M17 packet mo\x00" | ./m17-packet-encode -S N0CALL -D ALL -C 10 -n 23 -o float.sym -f
	// 	//./m17-packet-encode -S N0CALL -D ALL -C 10 -o float.sym -f -T 'this is a simple text'
	// 	if numBytes > 25 { //fix for frames that, with terminating byte and crc, land exactly on 25 bytes (or %25==0)
	// 		memcpy(pkt_chunk, &full_packet_data[pkt_cnt*25], 25)
	// 		pkt_chunk[25] = pkt_cnt << 2
	// 		fprintf(stderr, "FN:%02d (full frame)\n", pkt_cnt)

	// 		//encode the packet frame
	// 		conv_encode_packet_frame(enc_bits, pkt_chunk)

	// 		//reorder bits
	// 		reorder_bits(rf_bits, enc_bits)

	// 		//randomize
	// 		randomize_bits(rf_bits)

	// 		//fill packet with frame data
	// 		gen_data(full_packet, &pkt_sym_cnt, rf_bits)

	// 		numBytes -= 25
	// 	} else {
	// 		memcpy(pkt_chunk, &full_packet_data[pkt_cnt*25], numBytes)
	// 		memset(&pkt_chunk[numBytes], 0, 25-numBytes) //zero-padding

	// 		//EOT bit set to 1, set counter to the amount of bytes in this (the last) frame
	// 		if numBytes%25 == 0 {
	// 			pkt_chunk[25] = (1 << 7) | ((25) << 2)

	// 		} else {
	// 			pkt_chunk[25] = (1 << 7) | ((numBytes % 25) << 2)

	// 		}

	// 		fprintf(stderr, "FN:-- (ending frame)\n")

	// 		//encode the packet frame
	// 		conv_encode_packet_frame(enc_bits, pkt_chunk)

	// 		//reorder bits
	// 		reorder_bits(rf_bits, enc_bits)

	// 		//randomize
	// 		randomize_bits(rf_bits)

	// 		//fill packet with frame data
	// 		gen_data(full_packet, &pkt_sym_cnt, rf_bits)

	// 		numBytes = 0
	// 	}

	// 	//debug dump
	// 	//for(uint8_t i=0; i<26; i++)
	// 	//fprintf(stderr, "%02X", pkt_chunk[i]);
	// 	//fprintf(stderr, "\n");

	// 	pkt_cnt++
	// }

	return nil
}

/**
 * @brief Generate symbol stream for a preamble.
 *
 * @param out Frame buffer (192 floats).
 * @param cnt Pointer to a variable holding the number of written symbols.
 * @param type Preamble type (pre-BERT or pre-LSF).
 */
func AppendPreamble(out []float32, typ Pream) {
	if typ == PREAM_BERT { //pre-BERT
		for i := 0; i < SymbolsPerFrame/2; i++ { //40ms * 4800 = 192
			out = append(out, -3.0, +3.0)
		}
	} else { // type==PREAM_LSF //pre-LSF
		for i := 0; i < SymbolsPerFrame/2; i++ { //40ms * 4800 = 192
			out = append(out, +3.0, -3.0)
		}
	}
}

// Generate symbol stream for a syncword.
func AppendSyncword(out []float32, syncword uint16) {
	for i := 0; i < SymbolsPerSyncword*2; i += 2 {
		out = append(out, float32(SymbolMap[(syncword>>(14-i))&3]))
	}
}

/**
 * @brief Reorder (interleave) 368 unpacked payload bits.
 *
 * @param outp Reordered, unpacked type-4 bits.
 * @param inp Input unpacked type-2/3 bits.
 */
func reorder_bits(outp []uint8, inp []uint8) {
	for i := 0; i < SymbolsPerPayload*2; i++ {
		outp[i] = inp[IntrlSeq[i]]
	}
}
