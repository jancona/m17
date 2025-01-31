package m17

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
)

const distThresh = 2.0 //distance threshold for the L2 metric (for syncword detection)
// const m17PacketSize = 33 * 25
var (
	syncd bool
	//look-back buffer for finding syncwords
	last []float32 = make([]float32, 8)
	//Euclidean distance for finding syncwords in the symbol stream
	dist float32
	//raw frame symbols
	pld      = make([]float32, SymbolsPerPayload)
	softBit  = make([]uint16, 2*SymbolsPerPayload) //raw frame soft bits
	dSoftBit = make([]uint16, 2*SymbolsPerPayload) //deinterleaved soft bits

	lsf        = make([]uint8, 30+1)  //complete LSF (one byte extra needed for the Viterbi decoder)
	frameData  = make([]uint8, 26+1)  //decoded frame data, 206 bits, plus 4 flushing bits
	packetData = make([]uint8, 33*25) //whole packet data

	// uint8_t syncd=0;                    //syncword found?
	fl     bool  //Frame=0 of LSF=1
	lastFN int8  //last received frame number (-1 when idle)
	pushed uint8 //counter for pushed symbols

	skipPayloadCRCCheck = false //skip payload CRC check
	// uint8_t callsigns=0;                //decode callsigns?
	// uint8_t show_viterbi=0;             //show Viterbi errors?
	// uint8_t text_only=0;                //display text only (for text message mode)
)

func ProcessSamples(in io.Reader, fromClient func([]uint8, []uint8) error) error {
	for {
		var sample float32
		err := binary.Read(in, binary.LittleEndian, &sample)
		if err == io.EOF {
			return nil
		} else if err != nil {
			// TODO: return error here?
			log.Printf("binary.Read failed: %v", err)
		}
		if !syncd {
			//push new symbol
			for i := 0; i < 7; i++ {
				last[i] = last[i+1]
			}

			last[7] = sample

			//calculate euclidean norm
			dist = EuclNorm(last, PktSyncSymbols)
			// log.Printf("[DEBUG] sample: %f, pkt_sync dist: %f", sample, dist)

			//fprintf(stderr, "pkt_sync dist: %3.5f\n", dist);
			if dist < distThresh { //frame syncword detected
				// log.Printf("[DEBUG] pkt_sync dist: %f", dist)
				syncd = true
				pushed = 0
				fl = false
			} else {
				//calculate euclidean norm again, this time against LSF syncword
				dist = EuclNorm(last, LsfSyncSymbols)
				// log.Printf("[DEBUG] sample: %f, lsf_sync dist: %f", sample, dist)

				//fprintf(stderr, "lsf_sync dist: %3.5f\n", dist);
				if dist < distThresh { //LSF syncword
					// log.Printf("[DEBUG] lsf_sync dist: %f", dist)
					syncd = true
					pushed = 0
					lastFN = -1
					packetData = make([]uint8, 33*25)
					fl = true
				}
			}
		} else {
			pld[pushed] = sample
			pushed++
			if pushed == SymbolsPerPayload { //frame acquired
				//get current time
				// now := time.Now()
				// struct tm* tm_now = localtime(&now);

				for i := 0; i < SymbolsPerPayload; i++ {

					//bit 0
					if pld[i] >= float32(SymbolList[3]) {
						softBit[i*2+1] = 0xFFFF
					} else if pld[i] >= float32(SymbolList[2]) {
						softBit[i*2+1] = uint16(-float32(0xFFFF)/float32((SymbolList[3]-SymbolList[2])*SymbolList[2]) + pld[i]*float32(0xFFFF)/float32((SymbolList[3]-SymbolList[2])))
					} else if pld[i] >= float32(SymbolList[1]) {
						softBit[i*2+1] = 0x0000
					} else if pld[i] >= float32(SymbolList[0]) {
						softBit[i*2+1] = uint16(float32(0xFFFF)/float32((SymbolList[1]-SymbolList[0])*SymbolList[1]) - pld[i]*float32(0xFFFF)/float32((SymbolList[1]-SymbolList[0])))
					} else {
						softBit[i*2+1] = 0xFFFF
					}

					//bit 1
					if pld[i] >= float32(SymbolList[2]) {
						softBit[i*2] = 0x0000
					} else if pld[i] >= float32(SymbolList[1]) {
						softBit[i*2] = 0x7FFF - uint16(pld[i]*float32(0xFFFF)/float32(SymbolList[2]-SymbolList[1]))
					} else {
						softBit[i*2] = 0xFFFF
					}
				}

				//derandomize
				for i := 0; i < SymbolsPerPayload*2; i++ {
					if (RandSeq[i/8]>>(7-(i%8)))&1 != 0 { //soft XOR. flip soft bit if "1"
						softBit[i] = 0xFFFF - softBit[i]
					}
				}

				//deinterleave
				for i := 0; i < SymbolsPerPayload*2; i++ {
					dSoftBit[i] = softBit[IntrlSeq[i]]
				}

				//if it is a frame
				if !fl {
					m := ""
					for i := 0; i < len(dSoftBit); i++ {
						m += fmt.Sprintf("%04X", dSoftBit[i])
					}
					// log.Printf("[DEBUG] len(dSoftBit): %d, dSoftBit: %s", len(dSoftBit), m)
					//decode
					e, err := ViterbiDecodePunctured(frameData, dSoftBit, PuncturePattern3)
					if err != nil {
						log.Printf("Error calling ViterbiDecodePunctured: %v", err)
					}

					//dump FN
					rx_fn := (frameData[26] >> 2) & 0x1F
					rx_last := frameData[26] >> 7

					//fprintf(stderr, "FN%d, (%d)\n", rx_fn, rx_last);

					//             if(show_viterbi)
					//             {
					//                 fprintf(stderr, "   \033[93mFrame %d Viterbi error:\033[39m %1.1f\n", rx_last?lastFN+1:rx_fn, (float)e/0xFFFF);
					//             }
					// log.Printf("[DEBUG] FN%d, (%d)", rx_fn, rx_last)
					if rx_last != 0 {
						log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", lastFN+1, float32(e)/float32(0xFFFF))
					} else {
						log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", rx_fn, float32(e)/float32(0xFFFF))
					}
					// log.Printf("[DEBUG] frameData: %x %s", frameData[1:26], frameData[1:26])

					//copy data - might require some fixing
					if rx_fn <= 31 && rx_fn == uint8(lastFN)+1 && rx_last == 0 {
						// memcpy(&packetData[rx_fn*25], &frameData[1], 25)
						copy(packetData[int(rx_fn)*25:(int(rx_fn)+1)*25], frameData[1:26])
						lastFN++
					} else if rx_last != 0 {
						// memcpy(&packetData[(lastFN+1)*25], &frameData[1], rx_fn)
						copy(packetData[(int(lastFN)+1)*25:(int(lastFN)+1)*25+int(rx_fn)], frameData[1:rx_fn+1])
						packetData = packetData[:(int(lastFN)+1)*25+int(rx_fn)]
						// fprintf(stderr, " \033[93mContent\033[39m\n");

						if CRC(packetData) {
							fromClient(lsf, packetData)
						}
						//dump data
						if packetData[0] == 0x05 { //if a text message
							// fprintf(stderr, " ├ \033[93mType:\033[39m SMS\n");

							if skipPayloadCRCCheck {
								// fprintf(stderr, " └ \033[93mText:\033[39m %s\n", &packetData[1]);
							} else {
								// uint16_t p_len=strlen((const char*)packetData);

								// fprintf(stderr, " ├ \033[93mText:\033[39m %s\n", &packetData[1]);

								//CRC
								// fprintf(stderr, " └ \033[93mPayload CRC:\033[39m");
								// if(CRC_M17(packetData, p_len+3)) //3: terminating null plus a 2-byte CRC
								//     fprintf(stderr, " \033[91mmismatch\033[39m\n");
								// else
								//     fprintf(stderr, " \033[92mmatch\033[39m\n");
							}
						} else {
							// if(!text_only)					                    {
							//     fprintf(stderr, " └ \033[93mPayload:\033[39m ");
							//     for(uint16_t i=0; i<(lastFN+1)*25+rx_fn; i++)
							//     {
							//         if(i!=0 && (i%25)==0)
							//             fprintf(stderr, "\n     ");
							//         fprintf(stderr, " %02X", packetData[i]);
							//     }
							//     fprintf(stderr, "\n");
							// }
						}
						// cleanup
						lsf = make([]uint8, 30+1)         //complete LSF (one byte extra needed for the Viterbi decoder)
						frameData = make([]uint8, 26+1)   //decoded frame data, 206 bits, plus 4 flushing bits
						packetData = make([]uint8, 33*25) //whole packet data

					}

				} else { //if it is LSF
					// fprintf(stderr, "\033[96m[%02d:%02d:%02d] \033[92mPacket received\033[39m\n", tm_now->tm_hour, tm_now->tm_min, tm_now->tm_sec);
					// m := ""
					// for i := 0; i < 61; i++ {
					// 	m += fmt.Sprintf("%04X", dSoftBit[i])
					// }
					// log.Printf("[DEBUG] dSoftBit: %s", m)
					//decode
					e, err := ViterbiDecodePunctured(lsf, dSoftBit, PuncturePattern1)
					if err != nil {
						log.Printf("Error calling ViterbiDecodePunctured: %v", err)
					}

					//shift the buffer 1 position left - get rid of the encoded flushing bits
					// copy(lsf, lsf[1:])
					lsf = lsf[1:]
					log.Printf("[DEBUG] lsf: %x", lsf)
					dst, err := DecodeCallsign(lsf[0:6])
					if err != nil {
						log.Printf("[ERROR] Bad dst callsign: %v", err)
					}
					src, err := DecodeCallsign(lsf[6:12])
					if err != nil {
						log.Printf("[ERROR] Bad src callsign: %v", err)
					}
					log.Printf("[DEBUG] dest: %s, src: %s", dst, src)

					// if(!text_only)
					// {
					//     //dump data
					//     if(callsigns)
					//     {
					//         uint8_t d_dst[12], d_src[12]; //decoded strings

					//         decode_callsign_bytes(d_dst, &lsf[0]);
					//         decode_callsign_bytes(d_src, &lsf[6]);

					//         //DST
					//         fprintf(stderr, " ├ \033[93mDestination:\033[39m %s\n", d_dst);

					//         //SRC
					//         fprintf(stderr, " ├ \033[93mSource:\033[39m %s\n", d_src);
					//     }
					//     else
					//     {
					//         //DST
					//         fprintf(stderr, " ├ \033[93mDestination:\033[39m ");
					//         for(uint8_t i=0; i<6; i++)
					//             fprintf(stderr, "%02X", lsf[i]);
					//         fprintf(stderr, "\n");

					//         //SRC
					//         fprintf(stderr, " ├ \033[93mSource:\033[39m ");
					//         for(uint8_t i=0; i<6; i++)
					//             fprintf(stderr, "%02X", lsf[6+i]);
					//         fprintf(stderr, "\n");
					//     }

					//     //TYPE
					//     fprintf(stderr, " ├ \033[93mType:\033[39m ");
					//     for(uint8_t i=0; i<2; i++)
					//         fprintf(stderr, "%02X", lsf[12+i]);
					//     fprintf(stderr, "\n");

					//     //META
					//     fprintf(stderr, " ├ \033[93mMeta:\033[39m ");
					//     for(uint8_t i=0; i<14; i++)
					//         fprintf(stderr, "%02X", lsf[14+i]);
					//     fprintf(stderr, "\n");

					//     //Viterbi decoder errors
					//     if(show_viterbi)
					//     {
					//         fprintf(stderr, " ├ \033[93mLSF Viterbi error:\033[39m %1.1f\n", (float)e/0xFFFF);
					//     }
					log.Printf("[DEBUG] LSF Viterbi error: %1.1f", float32(e)/float32(0xFFFF))

					//     //CRC
					//     fprintf(stderr, " └ \033[93mLSF CRC:\033[39m");
					//     if(CRC_M17(lsf, 30))
					//         fprintf(stderr, " \033[91mmismatch\033[39m\n");
					//     else
					//         fprintf(stderr, " \033[92mmatch\033[39m\n");
					// }
				}

				//job done
				syncd = false
				pushed = 0

				last = make([]float32, 8)

			}
		}
	}
}

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
	AppendSyncword(full_packet, SYNC_LSF)

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
