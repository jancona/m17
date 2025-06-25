package m17

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
)

const (
	LSFSync    = uint16(0x55F7)
	StreamSync = uint16(0xFF5D)
	PacketSync = uint16(0x75FF)
	BERTSync   = uint16(0xDF55)
	EOTMarker  = uint16(0x555D)
)

var (
	LSFPreambleSymbols = []float64{+3, -3, +3, -3, +3, -3, +3, -3}
	LSFSyncSymbols     = []float64{+3, +3, +3, +3, -3, -3, +3, -3}
	ExtLSFSyncSymbols  = append(LSFPreambleSymbols, LSFSyncSymbols...)
	StreamSyncSymbols  = []float64{-3, -3, -3, -3, +3, +3, -3, +3}
	PacketSyncSymbols  = []float64{+3, -3, +3, +3, -3, -3, -3, -3}
	BERTSyncSymbols    = []float64{-3, +3, -3, -3, +3, +3, +3, +3}
)

type Decoder struct {
	synced bool
	//look-back buffer
	// symbolBuf *ring.Ring
	//raw frame symbols
	// softBit  []Symbol //raw frame soft bits
	// dSoftBit []Symbol //deinterleaved soft bits

	lsf LSF
	// lsf        []byte //complete LSF (one byte extra needed for the Viterbi decoder)
	// frameData  []byte //decoded frame data, 206 bits, plus 4 flushing bits
	packetData []byte //whole packet data

	timeoutCnt         int
	firstLSFFrame      bool // Frame=0 of LSF=1
	gotLSF             bool
	lastPacketFrameNum int // last packet frame number received (-1 when idle)
	lastStreamFrameNum int // last stream frame number received(-1 when idle)
	lichParts          int

	// skipPayloadCRCCheck //skip payload CRC check
}

// 8 preamble symbols, 8 for the syncword, and 960 for the payload.
// floor(sps/2)=2 extra samples for timing error correction
const SymbolBufSize = 8*5 + 2*(8*5+4800/25*5) + 2

func NewDecoder() *Decoder {
	d := Decoder{
		//raw frame symbols
		// pld:      make([]Symbol, SymbolsPerPayload),
		// softBit:  make([]Symbol, 2*SymbolsPerPayload), //raw frame soft bits
		// dSoftBit: make([]Symbol, 2*SymbolsPerPayload), //deinterleaved soft bits

		// lsf:                  make([]byte, 30+1),  //complete LSF (one byte extra needed for the Viterbi decoder)
		// frameData:            make([]byte, 26+1),  //decoded frame data, 206 bits, plus 4 flushing bits
		// packetData:           make([]byte, 33*25), //whole packet data
		lastPacketFrameNum: -1,
		lastStreamFrameNum: -1,
	}
	return &d
}
func (d *Decoder) DecodeSymbols(in io.Reader, fromModem func([]byte, []byte) error) error {
	// var cnt int
	bufIn := bufio.NewReaderSize(in, SymbolBufSize*4)
	for {
		// Looking for a sync burst
		//calculate euclidean norm
		dist, typ, err := syncDistance(bufIn, 0)
		if err == io.EOF {
			return err
		}
		// log.Printf("[DEBUG] dist: %3.5f, typ: %x", dist, typ)
		switch {
		case typ == LSFSync && dist < 4.5 && !d.synced:
			log.Printf("[DEBUG] Received LSFSync, distance: %f, type: %x", dist, typ)
			var pld []Symbol
			pld, _, err = d.extractPayload(dist, typ, bufIn)
			if err == io.EOF {
				return err
				// } else if err != nil {
				// 	// Was logged in extractPayload
			}
			d.lsf = decodeLSF(pld)
			log.Printf("[DEBUG] Received RF LSF: %s", d.lsf)
			if d.lsf.CheckCRC() {
				d.gotLSF = true
				d.synced = true
				d.timeoutCnt = 0
				d.lastStreamFrameNum = -1
				d.lastPacketFrameNum = -1

				if d.lsf.Type[1]&byte(LSFTypeStream) == byte(LSFTypeStream) {
					// TODO: Initialize stream mode and send LSF to reflector
					d.synced = false // for now
				} else { // packet mode
					d.packetData = make([]byte, 33*25)
				}
			} else {
				log.Print("[DEBUG] Bad LSF CRC")
			}

		case typ == PacketSync && dist < 5.0 && d.synced:
			log.Printf("[DEBUG] Received PacketSync, distance: %f, type: %x", dist, typ)
			pld, _, err := d.extractPayload(dist, typ, bufIn)
			if err == io.EOF {
				return err
				// } else if err != nil {
				// 	// Was logged in extractPayload
			}
			pktFrame, e := d.decodePacketFrame(pld /*, fromModem*/)
			// log.Printf("[DEBUG] pktFrame: % x", pktFrame)
			lastFrame := (pktFrame[25] >> 7) != 0

			// If lastFrame is true, this value is the byte count in the frame,
			// otherwise it's the frame number
			frameNumOrByteCnt := int((pktFrame[25] >> 2) & 0x1F)

			if lastFrame && frameNumOrByteCnt > 25 {
				log.Printf("[INFO] Fixing overrun in last frame: %d > 25", frameNumOrByteCnt)
				frameNumOrByteCnt = 25
			}

			log.Printf("[DEBUG] pktFrame[25]: %b, frameNumOrByteCnt: %d, last: %v", pktFrame[25], frameNumOrByteCnt, lastFrame)
			if lastFrame {
				log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", d.lastPacketFrameNum+1, e)
			} else {
				log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", frameNumOrByteCnt, e)
			}
			// log.Printf("[DEBUG] frameData: % x %s", pktFrame, pktFrame)

			//copy data - might require some fixing
			if frameNumOrByteCnt <= 31 && frameNumOrByteCnt == d.lastPacketFrameNum+1 && !lastFrame {
				copy(d.packetData[frameNumOrByteCnt*25:(frameNumOrByteCnt+1)*25], pktFrame)
				d.lastPacketFrameNum++
			} else if lastFrame {
				// log.Printf("[DEBUG] packetData[%d:%d], frameData[%d:%d] len(frameData): %d", ((d.lastPacketFrameNum + 1) * 25), ((d.lastPacketFrameNum+1)*25 + frameNumOrByteCnt), 1, (frameNumOrByteCnt + 1), len(pkt))
				copy(d.packetData[(d.lastPacketFrameNum+1)*25:(d.lastPacketFrameNum+1)*25+frameNumOrByteCnt], pktFrame[:frameNumOrByteCnt])
				d.packetData = d.packetData[:(d.lastPacketFrameNum+1)*25+frameNumOrByteCnt]
				// fprintf(stderr, " \033[93mContent\033[39m\n");
				if CRC(d.packetData) == 0 {
					// log.Printf("[DEBUG] d.lsf: %v, d.packetData: %v", d.lsf, d.packetData)
					fromModem(d.lsf.ToBytes(), d.packetData)
				} else {
					log.Printf("[DEBUG] Bad CRC not forwarded: %x", CRC(d.packetData))
				}
				// cleanup
				d.resetPacket()
			}

		case typ == StreamSync && dist < 5.0:
			log.Printf("[DEBUG] Received StreamSync, distance: %f, type: %x", dist, typ)
			_, _, err := d.extractPayload(dist, typ, bufIn)
			if err == io.EOF {
				return err
				// } else if err != nil {
				// 	// Was logged in extractPayload
			}
		case typ == BERTSync && dist < 5.0:
			log.Printf("[DEBUG] Received BERTSync, distance: %f, type: %x", dist, typ)
			_, _, err := d.extractPayload(dist, typ, bufIn)
			if err == io.EOF {
				return err
				// } else if err != nil {
				// 	// Was logged in extractPayload
			}
		default:
			// No one read anything, so advance one symbol
			_, err := bufIn.Discard(4)
			// err = binary.Read(bufIn, binary.LittleEndian, &symbol)
			if err == io.EOF {
				log.Printf("DecodeSamples binary.Discard EOF")
				return nil
			} else if err != nil {
				// TODO: return error here?
				log.Printf("DecodeSamples binary.Discard failed: %v", err)
			}
			// cnt++
			// log.Printf("[DEBUG] symbol(%d): %f", cnt, symbol)
			// fmt.Printf("%.1f ", symbol)
			// fmt.Printf("%s\n", toString(d.symbolBuf))

		}

		//RX sync timeout
		if d.synced {
			d.timeoutCnt++
			if d.timeoutCnt > 960*2 {
				d.synced = false
				d.timeoutCnt = 0
				d.firstLSFFrame = true
				d.lastStreamFrameNum = -1
				d.lastPacketFrameNum = -1
				d.lichParts = 0
				d.gotLSF = false
				d.resetPacket()
			}
		}
	}
}

func (d *Decoder) extractPayload(dist float32, typ uint16, in *bufio.Reader) ([]Symbol, float32, error) {
	// m := "["
	// d.symbolBuf.Do(func(sym any) {
	// 	m += fmt.Sprintf("%v ", sym)
	// })
	// m += "]"
	// log.Printf("[DEBUG] %s", m)
	// Check for better match
	offset := 0
	for i := range 2 {
		d, t, err := syncDistance(in, i+1)
		if err == io.EOF {
			log.Printf("extractPayload syncDistance EOF")
			return nil, 0, err
		} else if err != nil {
			// TODO: return error here?
			log.Printf("extractPayload syncDistance failed: %v", err)
		}
		if t == typ && d < dist {
			dist = d
			offset = i + 1
		}
	}
	// skip offset
	for range offset * 4 {
		_, err := in.ReadByte()
		if err == io.EOF {
			log.Printf("extractPayload ReadByte EOF")
			return nil, 0, err
		} else if err != nil {
			// TODO: return error here?
			log.Printf("extractPayload ReadByte failed: %v", err)
		}
	}
	// skip past sync
	syncSize := 8
	if typ == LSFSync {
		syncSize = 16
	}
	dummy := make([]Symbol, syncSize*5)
	err := binary.Read(in, binary.LittleEndian, dummy)
	if err == io.EOF {
		log.Printf("extractPayload binary.Read EOF")
		return nil, 0, err
	} else if err != nil {
		// TODO: return error here?
		log.Printf("extractPayload binary.Read failed: %v", err)
	}
	// log.Printf("[DEBUG] dummy: %#v", dummy)
	all := make([]Symbol, SymbolsPerPayload*5)
	err = binary.Read(in, binary.LittleEndian, all)
	if err == io.EOF {
		log.Printf("extractPayload binary.Read EOF")
		return nil, 0, err
	} else if err != nil {
		// TODO: return error here?
		log.Printf("extractPayload binary.Read failed: %v", err)
	}
	pld := make([]Symbol, SymbolsPerPayload)
	for i := range pld {
		pld[i] = all[i*5]
	}
	// log.Printf("[DEBUG] pld: % .2f", pld)
	return pld, dist, nil
}

func decodeLSF(pld []Symbol) LSF {
	softBit := calcSoftbits(pld)
	// log.Printf("[DEBUG] softBit: %#v", softBit)

	//derandomize
	softBit = derandomizeSymbols(softBit)
	// log.Printf("[DEBUG] derandomized softBit: %#v", softBit)

	//deinterleave
	dSoftBit := deinterleaveSymbols(softBit)
	// log.Printf("[DEBUG] dSoftBit: %#v", dSoftBit)

	//decode
	vd := ViterbiDecoder{}
	lsf, e := vd.DecodePunctured(dSoftBit, LSFPuncturePattern)

	//shift the buffer 1 position left - get rid of the encoded flushing bits
	// copy(lsf, lsf[1:])
	lsf = lsf[1 : LSFLen+1]
	// log.Printf("[DEBUG] lsf: %x", lsf)
	if CRC(lsf) != 0 {
		log.Printf("[DEBUG] Bad LSF CRC: %x", CRC(lsf))
	} else {
		dst, err := DecodeCallsign(lsf[0:6])
		if err != nil {
			log.Printf("[ERROR] Bad dst callsign: %v", err)
		}
		src, err := DecodeCallsign(lsf[6:12])
		if err != nil {
			log.Printf("[ERROR] Bad src callsign: %v", err)
		}
		log.Printf("[DEBUG] dest: %s, src: %s", dst, src)
	}
	log.Printf("[DEBUG] LSF Viterbi error: %1.1f", e/softTrue)
	return NewLSFFromBytes(lsf)
}

func (d *Decoder) decodePacketFrame(pld []Symbol /*, fromModem func([]byte, []byte) error*/) ([]byte, float64) {
	// log.Printf("[DEBUG] pld: %#v", pld)

	softBit := calcSoftbits(pld)
	// log.Printf("[DEBUG] softBit: %#v", softBit)

	//derandomize
	softBit = derandomizeSymbols(softBit)
	// log.Printf("[DEBUG] derandomized softBit: %#v", softBit)

	//deinterleave
	dSoftBit := deinterleaveSymbols(softBit)
	// log.Printf("[DEBUG] dSoftBit: %#v", dSoftBit)

	//decode
	vd := ViterbiDecoder{}
	pkt, e := vd.DecodePunctured(dSoftBit, PacketPuncturePattern)
	// log.Printf("[DEBUG] pkt: %#v", pkt)

	return pkt[1:], e
}

func calcSoftbits(pld []Symbol) []Symbol {
	softBit := make([]Symbol, 2*SymbolsPerPayload) //raw frame soft bits

	for i, sym := range pld {

		//bit 0
		if sym >= SymbolList[3] {
			softBit[i*2+1] = softTrue
		} else if sym >= SymbolList[2] {
			softBit[i*2+1] = -softTrue/((SymbolList[3]-SymbolList[2])*SymbolList[2]) + sym*softTrue/(SymbolList[3]-SymbolList[2])
		} else if sym >= SymbolList[1] {
			softBit[i*2+1] = softFalse
		} else if sym >= SymbolList[0] {
			softBit[i*2+1] = softTrue/((SymbolList[1]-SymbolList[0])*SymbolList[1]) - sym*softTrue/(SymbolList[1]-SymbolList[0])
		} else {
			softBit[i*2+1] = softTrue
		}

		//bit 1
		if sym >= SymbolList[2] {
			softBit[i*2] = softFalse
		} else if sym >= SymbolList[1] {
			softBit[i*2] = softMaybe - (sym * softTrue / (SymbolList[2] - SymbolList[1]))
		} else {
			softBit[i*2] = softTrue
		}
	}
	return softBit
}

func (d *Decoder) resetPacket() {
	d.synced = false
	d.lsf = LSF{}
}
