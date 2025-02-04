package m17

import (
	"container/ring"
	"encoding/binary"
	"io"
	"log"
)

const decoderDistThresh = 2.0 //distance threshold for the L2 metric (for syncword detection)
// const m17PacketSize = 33 * 25
type Decoder struct {
	synced bool
	//look-back buffer for finding syncwords
	last *ring.Ring
	//raw frame symbols
	pld      []float32
	softBit  []uint16 //raw frame soft bits
	dSoftBit []uint16 //deinterleaved soft bits

	lsf        []uint8 //complete LSF (one byte extra needed for the Viterbi decoder)
	frameData  []uint8 //decoded frame data, 206 bits, plus 4 flushing bits
	packetData []uint8 //whole packet data

	firstLSFFrame  bool //Frame=0 of LSF=1
	latestFrameNum int  //last received frame number (-1 when idle)
	pushedCnt      int  //counter for pushed symbols

	// skipPayloadCRCCheck //skip payload CRC check
}

func NewDecoder() *Decoder {
	d := Decoder{
		last: ring.New(8),
		//raw frame symbols
		pld:      make([]float32, SymbolsPerPayload),
		softBit:  make([]uint16, 2*SymbolsPerPayload), //raw frame soft bits
		dSoftBit: make([]uint16, 2*SymbolsPerPayload), //deinterleaved soft bits

		lsf:        make([]uint8, 30+1),  //complete LSF (one byte extra needed for the Viterbi decoder)
		frameData:  make([]uint8, 26+1),  //decoded frame data, 206 bits, plus 4 flushing bits
		packetData: make([]uint8, 33*25), //whole packet data
	}
	return &d
}
func (d *Decoder) DecodeSamples(in io.Reader, fromClient func([]uint8, []uint8) error) error {
	for {
		var sample float32
		err := binary.Read(in, binary.LittleEndian, &sample)
		if err == io.EOF {
			return nil
		} else if err != nil {
			// TODO: return error here?
			log.Printf("binary.Read failed: %v", err)
		}
		if !d.synced {
			// Looking for a sync burst
			d.last.Value = sample
			d.last = d.last.Next()
			//calculate euclidean norm
			dist, typ := SyncDistance(d.last)
			if dist < decoderDistThresh { //frame syncword detected
				log.Printf("[DEBUG] sync distance: %f, type: %x", dist, typ)

				d.synced = true
				d.pushedCnt = 0
				switch typ {
				case BERTSync:
					// To be implemented
				case PacketSync:
					d.firstLSFFrame = false
				case StreamSync:
					// To be implemented
				case LSFSync:
					d.latestFrameNum = -1
					d.packetData = make([]uint8, 33*25)
					d.firstLSFFrame = true
				default:
					log.Printf("[ERROR] Unexpected sync type")
				}
			}
		} else { //synced
			d.pld[d.pushedCnt] = sample
			d.pushedCnt++
			if d.pushedCnt == SymbolsPerPayload { //frame acquired

				for i := 0; i < SymbolsPerPayload; i++ {

					//bit 0
					if d.pld[i] >= float32(SymbolList[3]) {
						d.softBit[i*2+1] = 0xFFFF
					} else if d.pld[i] >= float32(SymbolList[2]) {
						d.softBit[i*2+1] = uint16(-float32(0xFFFF)/float32((SymbolList[3]-SymbolList[2])*SymbolList[2]) + d.pld[i]*float32(0xFFFF)/float32((SymbolList[3]-SymbolList[2])))
					} else if d.pld[i] >= float32(SymbolList[1]) {
						d.softBit[i*2+1] = 0x0000
					} else if d.pld[i] >= float32(SymbolList[0]) {
						d.softBit[i*2+1] = uint16(float32(0xFFFF)/float32((SymbolList[1]-SymbolList[0])*SymbolList[1]) - d.pld[i]*float32(0xFFFF)/float32((SymbolList[1]-SymbolList[0])))
					} else {
						d.softBit[i*2+1] = 0xFFFF
					}

					//bit 1
					if d.pld[i] >= float32(SymbolList[2]) {
						d.softBit[i*2] = 0x0000
					} else if d.pld[i] >= float32(SymbolList[1]) {
						d.softBit[i*2] = 0x7FFF - uint16(d.pld[i]*float32(0xFFFF)/float32(SymbolList[2]-SymbolList[1]))
					} else {
						d.softBit[i*2] = 0xFFFF
					}
				}

				//derandomize
				for i := 0; i < SymbolsPerPayload*2; i++ {
					if (RandSeq[i/8]>>(7-(i%8)))&1 != 0 { //soft XOR. flip soft bit if "1"
						d.softBit[i] = 0xFFFF - d.softBit[i]
					}
				}

				//deinterleave
				for i := 0; i < SymbolsPerPayload*2; i++ {
					d.dSoftBit[i] = d.softBit[IntrlSeq[i]]
				}

				if d.firstLSFFrame { //if it is LSF
					//decode
					e, err := ViterbiDecodePunctured(d.lsf, d.dSoftBit, PuncturePattern1)
					if err != nil {
						log.Printf("[ERROR] Error calling ViterbiDecodePunctured: %v", err)
					}

					//shift the buffer 1 position left - get rid of the encoded flushing bits
					// copy(lsf, lsf[1:])
					d.lsf = d.lsf[1:]
					log.Printf("[DEBUG] d.lsf: %x", d.lsf)
					if CRC(d.lsf) != 0 {
						log.Printf("[DEBUG] Bad LSF CRC.")
					} else {
						dst, err := DecodeCallsign(d.lsf[0:6])
						if err != nil {
							log.Printf("[ERROR] Bad dst callsign: %v", err)
						}
						src, err := DecodeCallsign(d.lsf[6:12])
						if err != nil {
							log.Printf("[ERROR] Bad src callsign: %v", err)
						}
						log.Printf("[DEBUG] dest: %s, src: %s", dst, src)
					}
					log.Printf("[DEBUG] LSF Viterbi error: %1.1f", float32(e)/float32(0xFFFF))

				} else { // non-LSF frame
					// m := ""
					// for i := 0; i < len(d.dSoftBit); i++ {
					// 	m += fmt.Sprintf("%04X", d.dSoftBit[i])
					// }
					// log.Printf("[DEBUG] len(dSoftBit): %d, dSoftBit: %s", len(dSoftBit), m)
					//decode
					e, err := ViterbiDecodePunctured(d.frameData, d.dSoftBit, PuncturePattern3)
					if err != nil {
						log.Printf("[ERROR] Error calling ViterbiDecodePunctured: %v", err)
					}

					lastFrame := (d.frameData[26] >> 7) != 0

					// If lastFrame is true, this value is the byte count in the frame,
					// otherwise it's the frame number
					frameNumOrByteCnt := int((d.frameData[26] >> 2) & 0x1F)

					log.Printf("[DEBUG] d.frameData[26]: %b, frameNumOrByteCnt: %d, last: %v", d.frameData[26], frameNumOrByteCnt, lastFrame)
					if lastFrame {
						log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", d.latestFrameNum+1, float32(e)/float32(0xFFFF))
					} else {
						log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", frameNumOrByteCnt, float32(e)/float32(0xFFFF))
					}
					// log.Printf("[DEBUG] frameData: %x %s", frameData[1:26], frameData[1:26])

					//copy data - might require some fixing
					if frameNumOrByteCnt <= 31 && frameNumOrByteCnt == d.latestFrameNum+1 && !lastFrame {
						// memcpy(&packetData[rx_fn*25], &frameData[1], 25)
						copy(d.packetData[frameNumOrByteCnt*25:(frameNumOrByteCnt+1)*25], d.frameData[1:26])
						d.latestFrameNum++
					} else if lastFrame {
						// memcpy(&packetData[(lastFN+1)*25], &frameData[1], rx_fn)
						copy(d.packetData[(d.latestFrameNum+1)*25:(d.latestFrameNum+1)*25+frameNumOrByteCnt], d.frameData[1:frameNumOrByteCnt+1])
						d.packetData = d.packetData[:(d.latestFrameNum+1)*25+frameNumOrByteCnt]
						// fprintf(stderr, " \033[93mContent\033[39m\n");

						if CRC(d.lsf) == 0 && CRC(d.packetData) == 0 {
							fromClient(d.lsf, d.packetData)
						}
						// cleanup
						d.lsf = make([]uint8, 30+1)         //complete LSF (one byte extra needed for the Viterbi decoder)
						d.frameData = make([]uint8, 26+1)   //decoded frame data, 206 bits, plus 4 flushing bits
						d.packetData = make([]uint8, 33*25) //whole packet data

					}

				}

				//job done
				d.ResetFrame()
			}
		}
	}
}

func (d *Decoder) ResetFrame() {
	d.synced = false
	d.pushedCnt = 0

	d.last = ring.New(8)
}
