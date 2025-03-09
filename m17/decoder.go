package m17

import (
	"container/ring"
	"encoding/binary"
	"io"
	"log"
)

const decoderDistThresh = 2.0 //distance threshold for the L2 metric (for syncword detection)
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

type Decoder struct {
	synced bool
	//look-back buffer for finding syncwords
	last *ring.Ring
	//raw frame symbols
	pld      []Symbol
	softBit  []Symbol //raw frame soft bits
	dSoftBit []Symbol //deinterleaved soft bits

	lsf        []byte //complete LSF (one byte extra needed for the Viterbi decoder)
	frameData  []byte //decoded frame data, 206 bits, plus 4 flushing bits
	packetData []byte //whole packet data

	firstLSFFrame  bool //Frame=0 of LSF=1
	latestFrameNum int  //last received frame number (-1 when idle)
	pushedCnt      int  //counter for pushed symbols

	// skipPayloadCRCCheck //skip payload CRC check
}

func NewDecoder() *Decoder {
	d := Decoder{
		last: ring.New(8),
		//raw frame symbols
		pld:      make([]Symbol, SymbolsPerPayload),
		softBit:  make([]Symbol, 2*SymbolsPerPayload), //raw frame soft bits
		dSoftBit: make([]Symbol, 2*SymbolsPerPayload), //deinterleaved soft bits

		lsf:        make([]byte, 30+1),  //complete LSF (one byte extra needed for the Viterbi decoder)
		frameData:  make([]byte, 26+1),  //decoded frame data, 206 bits, plus 4 flushing bits
		packetData: make([]byte, 33*25), //whole packet data
	}
	return &d
}
func (d *Decoder) DecodeSymbols(in io.Reader, fromModem func([]byte, []byte) error) error {
	for {
		var symbols = make([]Symbol, symbolsPer40MS)
		// log.Printf("DecodeSamples binary.Read %d", len(symbols))
		err := binary.Read(in, binary.LittleEndian, symbols)
		if err == io.EOF {
			log.Printf("DecodeSamples binary.Read EOF")
			return nil
		} else if err != nil {
			// TODO: return error here?
			log.Printf("DecodeSamples binary.Read failed: %v", err)
		}
		for _, symbol := range symbols {
			if !d.synced {
				// Looking for a sync burst
				d.last.Value = symbol
				d.last = d.last.Next()
				//calculate euclidean norm
				dist, typ := syncDistance(d.last)
				// log.Printf("[DEBUG] symbol: %3.5f, dist: %3.5f, typ: %x", symbol, dist, typ)
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
						d.packetData = make([]byte, 33*25)
						d.firstLSFFrame = true
					default:
						log.Printf("[ERROR] Unexpected sync type: 0x%x", typ)
					}
				}
			} else { //synced
				d.pld[d.pushedCnt] = symbol
				d.pushedCnt++
				if d.pushedCnt == SymbolsPerPayload { //frame acquired
					// log.Printf("[DEBUG] d.pld: %#v", d.pld)
					for i := 0; i < SymbolsPerPayload; i++ {

						//bit 0
						if d.pld[i] >= SymbolList[3] {
							d.softBit[i*2+1] = softTrue
						} else if d.pld[i] >= SymbolList[2] {
							d.softBit[i*2+1] = -softTrue/((SymbolList[3]-SymbolList[2])*SymbolList[2]) + d.pld[i]*softTrue/(SymbolList[3]-SymbolList[2])
						} else if d.pld[i] >= SymbolList[1] {
							d.softBit[i*2+1] = softFalse
						} else if d.pld[i] >= SymbolList[0] {
							d.softBit[i*2+1] = softTrue/((SymbolList[1]-SymbolList[0])*SymbolList[1]) - d.pld[i]*softTrue/(SymbolList[1]-SymbolList[0])
						} else {
							d.softBit[i*2+1] = softTrue
						}

						//bit 1
						if d.pld[i] >= SymbolList[2] {
							d.softBit[i*2] = softFalse
						} else if d.pld[i] >= SymbolList[1] {
							d.softBit[i*2] = softMaybe - (d.pld[i] * softTrue / (SymbolList[2] - SymbolList[1]))
						} else {
							d.softBit[i*2] = softTrue
						}
					}
					// log.Printf("[DEBUG] d.softBit: %#v", d.softBit)

					//derandomize
					d.softBit = derandomizeSymbols(d.softBit)
					// log.Printf("[DEBUG] derandomized d.softBit: %#v", d.softBit)

					//deinterleave
					d.dSoftBit = deinterleaveSymbols(d.softBit)
					// log.Printf("[DEBUG] d.dSoftBit: %#v", d.dSoftBit)

					if d.firstLSFFrame { //if it is LSF
						//decode
						vd := ViterbiDecoder{}
						lsf, e := vd.DecodePunctured(d.dSoftBit, LSFPuncturePattern)

						//shift the buffer 1 position left - get rid of the encoded flushing bits
						// copy(lsf, lsf[1:])
						d.lsf = lsf[1 : LSFLen+1]
						// log.Printf("[DEBUG] d.lsf: %x", d.lsf)
						if CRC(d.lsf) != 0 {
							log.Printf("[DEBUG] Bad LSF CRC: %x", CRC(d.lsf))
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
						log.Printf("[DEBUG] LSF Viterbi error: %1.1f", e/softTrue)

					} else { // non-LSF frame
						// m := ""
						// for i := 0; i < len(d.dSoftBit); i++ {
						// 	m += fmt.Sprintf("%04X", d.dSoftBit[i])
						// }
						// log.Printf("[DEBUG] len(dSoftBit): %d, dSoftBit: %s", len(dSoftBit), m)
						//decode
						var e float64
						vd := ViterbiDecoder{}
						d.frameData, e = vd.DecodePunctured(d.dSoftBit, PacketPuncturePattern)

						lastFrame := (d.frameData[26] >> 7) != 0

						// If lastFrame is true, this value is the byte count in the frame,
						// otherwise it's the frame number
						frameNumOrByteCnt := int((d.frameData[26] >> 2) & 0x1F)

						if lastFrame && frameNumOrByteCnt > 25 {
							log.Printf("[INFO] Fixing overrun in last frame: %d > 25", frameNumOrByteCnt)
							frameNumOrByteCnt = 25
						}

						// log.Printf("[DEBUG] d.frameData[26]: %b, frameNumOrByteCnt: %d, last: %v", d.frameData[26], frameNumOrByteCnt, lastFrame)
						if lastFrame {
							log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", d.latestFrameNum+1, float32(e)/float32(0xFFFF))
						} else {
							log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", frameNumOrByteCnt, float32(e)/float32(0xFFFF))
						}
						// log.Printf("[DEBUG] frameData: %x %s", d.frameData[1:26], d.frameData[1:26])

						//copy data - might require some fixing
						if frameNumOrByteCnt <= 31 && frameNumOrByteCnt == d.latestFrameNum+1 && !lastFrame {
							// memcpy(&packetData[rx_fn*25], &frameData[1], 25)
							copy(d.packetData[frameNumOrByteCnt*25:(frameNumOrByteCnt+1)*25], d.frameData[1:26])
							d.latestFrameNum++
						} else if lastFrame {
							// memcpy(&packetData[(lastFN+1)*25], &frameData[1], rx_fn)
							// log.Printf("[DEBUG] packetData[%d:%d], frameData[%d:%d] len(frameData): %d", ((d.latestFrameNum + 1) * 25), ((d.latestFrameNum+1)*25 + frameNumOrByteCnt), 1, (frameNumOrByteCnt + 1), len(d.frameData))
							copy(d.packetData[(d.latestFrameNum+1)*25:(d.latestFrameNum+1)*25+frameNumOrByteCnt], d.frameData[1:frameNumOrByteCnt+1])
							d.packetData = d.packetData[:(d.latestFrameNum+1)*25+frameNumOrByteCnt]
							// fprintf(stderr, " \033[93mContent\033[39m\n");
							// log.Printf("[DEBUG] d.lsf: %#v, d.packetData: %#v", d.lsf, d.packetData)
							if CRC(d.lsf) == 0 && CRC(d.packetData) == 0 {
								fromModem(d.lsf, d.packetData)
							} else {
								log.Printf("[DEBUG] Bad CRC not forwarded. LSF: %x, packet %x", CRC(d.lsf), CRC(d.packetData))
							}
							// cleanup
							d.resetPacket()
						}
					}
					//job done
					d.resetFrame()
				}
			}
		}
	}
}

func (d *Decoder) resetFrame() {
	d.synced = false
	d.pushedCnt = 0
	d.last = ring.New(8)
}

func (d *Decoder) resetPacket() {
	d.lsf = make([]byte, 30+1)         //complete LSF (one byte extra needed for the Viterbi decoder)
	d.frameData = make([]byte, 26+1)   //decoded frame data, 206 bits, plus 4 flushing bits
	d.packetData = make([]byte, 33*25) //whole packet data
}
