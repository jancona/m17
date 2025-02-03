package m17

import (
	"io"
)

func SendPacket(lsf LSF, packetData []byte, out io.Writer) error {
	var full_packet = make([]float32, 36*192*10) //full packet, symbols as floats - 36 "frames" max (incl. preamble, LSF, EoT), 192 symbols each, sps=10:
	var enc_bits [SymbolsPerPayload * 2]uint8    //type-2 bits, unpacked
	var rf_bits [SymbolsPerPayload * 2]uint8     //type-4 bits, unpacked
	var pkt_sym_cnt uint32

	//encode LSF data
	conv_encode_LSF(&enc_bits, &lsf)
	//fill preamble
	AppendPreamble(full_packet, PREAM_LSF)

	//send LSF syncword
	AppendSyncword(full_packet, LSFSync)

	//reorder bits
	reorder_bits(&rf_bits, &enc_bits)

	//randomize
	randomize_bits(&rf_bits)

	//fill packet with LSF
	gen_data(full_packet, &pkt_sym_cnt, &rf_bits)

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
func reorder_bits(outp *[SymbolsPerPayload * 2]uint8, inp *[SymbolsPerPayload * 2]uint8) {
	for i := 0; i < SymbolsPerPayload*2; i++ {
		outp[i] = inp[IntrlSeq[i]]
	}
}

/**
 * @brief Generate symbol stream for frame contents (without the syncword).
 * Can be used for both LSF and data frames.
 *
 * @param out Output buffer (184 floats).
 * @param cnt Pointer to a variable holding the number of written symbols.
 * @param in Data input - unpacked bits (1 bit per byte).
 */
func gen_data(out []float32, cnt *uint32, in *[SymbolsPerPayload * 2]uint8) {
	for i := 0; i < SymbolsPerPayload; i++ { //40ms * 4800 - 8 (syncword)
		out[(*cnt)] = float32(SymbolMap[in[2*i]*2+in[2*i+1]])
		(*cnt)++
	}
}
