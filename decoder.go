package m17

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/rand"
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
	syncedType uint16

	lsf *LSF

	frameData  []byte //decoded frame data, 206 bits, plus 4 flushing bits
	packetData []byte //whole packet data

	timeoutCnt   int
	gotLSF       bool
	lastPacketFN int // last packet frame number received (-1 when idle)
	lastStreamFN int // last stream frame number received (-1 when idle)
	lichParts    int
	streamID     uint16
	streamFN     uint16
	lsfBytes     []byte
	dashLog      *slog.Logger
}

// 8 preamble symbols, 8 for the syncword, and 960 for the payload.
// floor(sps/2)=2 extra samples for timing error correction
// plus some extra so we can make larger reads
const symbolBufSize = 8*5 + 2*(8*5+4800/25*5) + 2 + 256

func NewDecoder(dashLog *slog.Logger) *Decoder {
	d := Decoder{
		lastPacketFN: -1,
		lastStreamFN: -1,
		lsfBytes:     make([]byte, 30),
		dashLog:      dashLog,
	}
	return &d
}
func (d *Decoder) DecodeSymbols(in io.Reader, sendToNetwork func(lsf *LSF, payload []byte, sid, fn uint16) error) error {
	var symbols []Symbol
	var err error

	for {
		l := len(symbols)
		if symbolBufSize-l >= 256 {
			// refill the buffer
			symbols = append(symbols, make([]Symbol, symbolBufSize-l)...)
			err = binary.Read(in, binary.LittleEndian, symbols[l:])
			if err == io.EOF {
				log.Printf("refill binary.Read EOF")
				return fmt.Errorf("failed to refill symbol buffer: %v", err)
			} else if err != nil {
				log.Printf("refill binary.Read failed: %v", err)
				return fmt.Errorf("failed to refill symbol buffer: %v", err)
			}
		}

		// Looking for a sync burst
		//calculate euclidean norm
		dist, typ, err := syncDistance(symbols, 0)
		if err == io.EOF {
			return err
		}
		// if dist < 10 {
		// 	log.Printf("[DEBUG] dist: %3.5f, typ: %x", dist, typ)
		// }
		switch {
		case typ == LSFSync && dist < 4.5 && d.syncedType == 0:
			log.Printf("[DEBUG] Received LSFSync, distance: %f, type: %x", dist, typ)
			var pld []Symbol
			symbols, pld, _, err = d.extractPayload(dist, typ, symbols)
			if err == io.EOF {
				return err
				// } else if err != nil {
				// 	// Was logged in extractPayload
			}
			d.gotLSF = false
			d.lsf = decodeLSF(pld)
			log.Printf("[DEBUG] Received RF LSF: %s", d.lsf)
			if d.lsf.CheckCRC() {
				d.gotLSF = true
				d.timeoutCnt = 0
				d.lastStreamFN = -1
				d.lastPacketFN = -1

				if d.lsf.Type[1]&byte(LSFTypeStream) == byte(LSFTypeStream) {
					d.syncedType = StreamSync
					d.lichParts = 0x3F
					d.streamFN = 0
					d.streamID = uint16(rand.Intn(0x10000))
					sendToNetwork(d.lsf, nil, d.streamID, d.streamFN)
					if d.dashLog != nil {
						d.dashLog.Info("", "type", "RF", "subtype", "Voice Start", "src", d.lsf.Src.Callsign(), "dst", d.lsf.Dst.Callsign(), "can", d.lsf.CAN())
					}
				} else { // packet mode
					d.syncedType = PacketSync
					d.packetData = make([]byte, 33*25)
				}
			} else {
				log.Print("[DEBUG] Bad LSF CRC")
			}

		case typ == PacketSync && dist < 5.0 && d.syncedType == PacketSync:
			var pld []Symbol
			log.Printf("[DEBUG] Received PacketSync, distance: %f, type: %x", dist, typ)
			symbols, pld, _, err = d.extractPayload(dist, typ, symbols)
			if err != nil {
				return err
			}
			pktFrame, e := d.decodePacketFrame(pld)
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
				log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", d.lastPacketFN+1, e)
			} else {
				log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", frameNumOrByteCnt, e)
			}
			// log.Printf("[DEBUG] frameData: % x %s", pktFrame, pktFrame)

			//copy data - might require some fixing
			if frameNumOrByteCnt <= 31 && frameNumOrByteCnt == d.lastPacketFN+1 && !lastFrame {
				copy(d.packetData[frameNumOrByteCnt*25:(frameNumOrByteCnt+1)*25], pktFrame)
				d.lastPacketFN++
			} else if lastFrame {
				// log.Printf("[DEBUG] packetData[%d:%d], frameData[%d:%d] len(frameData): %d", ((d.lastPacketFrameNum + 1) * 25), ((d.lastPacketFrameNum+1)*25 + frameNumOrByteCnt), 1, (frameNumOrByteCnt + 1), len(pkt))
				copy(d.packetData[(d.lastPacketFN+1)*25:(d.lastPacketFN+1)*25+frameNumOrByteCnt], pktFrame[:frameNumOrByteCnt])
				d.packetData = d.packetData[:(d.lastPacketFN+1)*25+frameNumOrByteCnt]
				// fprintf(stderr, " \033[93mContent\033[39m\n");
				if CRC(d.packetData) == 0 {
					// log.Printf("[DEBUG] d.lsf: %v, d.packetData: %v", d.lsf, d.packetData)
					sendToNetwork(d.lsf, d.packetData, 0, 0)
					if d.dashLog != nil {
						d.dashLog.Info("", "type", "RF", "subtype", "Packet", "src", d.lsf.Src.Callsign(), "dst", d.lsf.Dst.Callsign(), "can", d.lsf.CAN())
					}
				} else {
					log.Printf("[DEBUG] Bad CRC not forwarded: %x", CRC(d.packetData))
				}
				// cleanup
				d.resetPacket()
			}

		case typ == StreamSync && dist < 5.0:
			var pld []Symbol
			log.Printf("[DEBUG] Received StreamSync, distance: %f, type: %x", dist, typ)
			symbols, pld, _, err = d.extractPayload(dist, typ, symbols)
			if err != nil {
				return err
			}
			var lich []byte
			var lichCnt byte
			var vd float64
			var fn uint16
			d.frameData, lich, fn, lichCnt, vd = d.decodeStreamFrame(pld)
			log.Printf("[DEBUG] frameData: [% 2x], lich: %x, lichCnt: %d, fn: %x, vd: %1.1f", d.frameData, lich, lichCnt, fn, vd)

			if d.lastStreamFN != int(fn) {
				if d.lichParts != 0x3F && lichCnt < 6 { //6 chunks = 0b111111
					//reconstruct LSF chunk by chunk
					copy(d.lsfBytes[lichCnt*5:lichCnt*5+5], lich)
					d.lichParts |= (1 << lichCnt)
					if d.lichParts == 0x3F && !d.gotLSF {
						lsfB := NewLSFFromBytes(d.lsfBytes)
						if lsfB.CheckCRC() {
							d.lsf = &lsfB
							d.gotLSF = true
							d.timeoutCnt = 0
							d.streamID = uint16(rand.Intn(0x10000))
							log.Printf("[DEBUG] Received stream LSF: %v", lsfB)
						} else {
							log.Printf("[DEBUG] Stream LSF CRC error: %v", lsfB)
							d.lichParts = 0
							d.gotLSF = false
						}
					}
				}
				log.Printf("[DEBUG] Received stream frame: FN:%04X, LICH_CNT:%d, Viterbi error: %1.1f", fn, lichCnt, vd)
				if d.gotLSF {
					// log.Printf("[DEBUG] Sending stream frame")
					// Not sure why we have to flip the bytes here
					d.streamFN = (fn >> 8) | ((fn & 0xFF) << 8)
					sendToNetwork(d.lsf, d.frameData, d.streamID, d.streamFN)
					d.timeoutCnt = 0
					// This doesn't work because the high bit is never set in actual frams received from my CS7000
					if d.dashLog != nil && fn&0x8000 == 0x8000 {
						d.dashLog.Info("", "type", "RF", "subtype", "Voice End", "src", d.lsf.Src.Callsign(), "dst", d.lsf.Dst.Callsign(), "can", d.lsf.CAN())
					}
				}
				d.lastStreamFN = int(fn)
			}
		default:
			// No one read anything, so advance one symbol
			symbols = symbols[1:]
		}

		//RX sync timeout
		if d.syncedType != 0 {
			d.timeoutCnt++
			if d.timeoutCnt > 960*2 {
				d.syncedType = 0
				d.timeoutCnt = 0
				d.lastStreamFN = -1
				d.lastPacketFN = -1
				d.lichParts = 0
				d.gotLSF = false
				d.resetPacket()
			}
		}
	}
}

func (d *Decoder) extractPayload(dist float32, typ uint16, symbols []Symbol) ([]Symbol, []Symbol, float32, error) {
	offset := 0
	for i := range 2 {
		d, t, err := syncDistance(symbols, i+1)
		if err == io.EOF {
			log.Printf("extractPayload syncDistance EOF")
			return nil, nil, 0, err
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
	symbols = symbols[offset:]
	// skip past sync
	syncSize := 16
	if typ == PacketSync {
		syncSize = 8
	}
	symbols = symbols[syncSize*5:]
	pld := make([]Symbol, SymbolsPerPayload)
	for i := range pld {
		pld[i] = symbols[i*5]
	}
	// log.Printf("[DEBUG] pld: % .2f", pld)
	// skip by most, but not all of the payload
	// if we skip everything we miss the next packet for some reason.
	symbols = symbols[(SymbolsPerPayload-offset-syncSize)*5:]
	return symbols, pld, dist, nil
}

func decodeLSF(pld []Symbol) *LSF {
	// log.Printf("[DEBUG] decodeLSF: len(pld): %d", len(pld))
	softBit := calcSoftbits(pld)
	// log.Printf("[DEBUG] softBit: %#v", softBit)

	//derandomize
	softBit = DerandomizeSoftBits(softBit)
	// log.Printf("[DEBUG] derandomized softBit: %#v", softBit)

	//deinterleave
	dSoftBit := DeinterleaveSoftBits(softBit)
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
	l := NewLSFFromBytes(lsf)
	return &l
}

func (d *Decoder) decodeStreamFrame(pld []Symbol) (frameData []byte, lich []byte, fn uint16, lichCnt byte, e float64) {
	// log.Printf("[DEBUG] decodeStreamFrame: len(pld): %d", len(pld))
	// log.Printf("[DEBUG] pld: [% 1.1f]", pld)

	softBit := calcSoftbits(pld)
	// log.Printf("[DEBUG] softBit: [% 04x]", softBit)

	//derandomize
	softBit = DerandomizeSoftBits(softBit)
	// log.Printf("[DEBUG] derandomized softBit: [% 04x]", softBit)

	//deinterleave
	dSoftBit := DeinterleaveSoftBits(softBit)
	// log.Printf("[DEBUG] deinterleaved softBit: [% 04x]", dSoftBit)
	lich = DecodeLICH(dSoftBit[:96])
	lichCnt = lich[5] >> 5

	//decode
	vd := ViterbiDecoder{}
	frameData, e = vd.DecodePunctured(dSoftBit[96:], StreamPuncturePattern)

	fn = (uint16(frameData[1]) << 8) | uint16(frameData[2])

	//shift 1+2 positions left - get rid of the encoded flushing bits and FN
	frameData = frameData[1+2:]

	return frameData, lich, fn, lichCnt, e / softTrue
}

func (d *Decoder) decodePacketFrame(pld []Symbol) ([]byte, float64) {
	// log.Printf("[DEBUG] decodePacketFrame: len(pld): %d", len(pld))
	// log.Printf("[DEBUG] pld: %#v", pld)

	softBit := calcSoftbits(pld)
	// log.Printf("[DEBUG] softBit: %#v", softBit)

	//derandomize
	softBit = DerandomizeSoftBits(softBit)
	// log.Printf("[DEBUG] derandomized softBit: %#v", softBit)

	//deinterleave
	dSoftBit := DeinterleaveSoftBits(softBit)
	// log.Printf("[DEBUG] dSoftBit: %#v", dSoftBit)

	//decode
	vd := ViterbiDecoder{}
	pkt, e := vd.DecodePunctured(dSoftBit, PacketPuncturePattern)
	// log.Printf("[DEBUG] pkt: %#v", pkt)

	return pkt[1:], e / softTrue
}

func calcSoftbits(pld []Symbol) []SoftBit {
	if len(pld) > SymbolsPerPayload {
		panic(fmt.Sprintf("pld contains %d symbols (>%d)", len(pld), SymbolsPerPayload))
	}
	softBit := make([]SoftBit, 2*SymbolsPerPayload) //raw frame soft bits

	for i, sym := range pld {

		//bit 0
		if sym >= SymbolList[3] {
			softBit[i*2+1] = softTrue
		} else if sym >= SymbolList[2] {
			softBit[i*2+1] = SoftBit(-softTrue/((SymbolList[3]-SymbolList[2])*SymbolList[2]) + sym*softTrue/(SymbolList[3]-SymbolList[2]))
		} else if sym >= SymbolList[1] {
			softBit[i*2+1] = softFalse
		} else if sym >= SymbolList[0] {
			softBit[i*2+1] = SoftBit(softTrue/((SymbolList[1]-SymbolList[0])*SymbolList[1]) - sym*softTrue/(SymbolList[1]-SymbolList[0]))
		} else {
			softBit[i*2+1] = softTrue
		}

		//bit 1
		if sym >= SymbolList[2] {
			softBit[i*2] = softFalse
		} else if sym >= SymbolList[1] {
			softBit[i*2] = SoftBit(softMaybe - (sym * softTrue / (SymbolList[2] - SymbolList[1])))
		} else {
			softBit[i*2] = softTrue
		}
	}
	return softBit
}

func (d *Decoder) resetPacket() {
	d.syncedType = 0
	d.lsf = nil
}
