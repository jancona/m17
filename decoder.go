package m17

import (
	"fmt"
	"log"
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
	LSFSyncBytes    = []byte{0x55, 0xF7}
	StreamSyncBytes = []byte{0xFF, 0x5D}
	PacketSyncBytes = []byte{0x75, 0xFF}
	BERTSyncBytes   = []byte{0xDF, 0x55}
	EOTMarkerBytes  = []byte{0x55, 0x5D}
)

type Decoder struct {
	receivedRFLSF        func(lsf LSF, ber float64) error
	receivedRFStream     func(lsf LSF, payload []byte, sid, fn uint16, ber float64) error
	receivedRFStreamLICH func(lsf LSF, ber float64) error
	receivedRFStreamEOT  func(lsf LSF, sid, fn uint16, ber float64) error
	receivedRFPacket     func(lsf LSF, payload []byte, ber float64) error
	syncedType           uint16

	lsf *LSF

	frameData  []byte //decoded frame data, 206 bits, plus 4 flushing bits
	packetData []byte //whole packet data

	timeoutCnt   int
	gotLSF       bool
	lastPacketFN byte   // last packet frame number received (0xff when idle)
	lastStreamFN uint16 // last stream frame number received (0xffff when idle)
	lichParts    int
	streamID     uint16
	streamFN     uint16
	lsfBytes     []byte
	errors       int
	bits         int
}

// 8 preamble symbols, 8 for the syncword, and 960 for the payload.
// floor(sps/2)=2 extra samples for timing error correction
// plus some extra so we can make larger reads
const symbolBufSize = 8*5 + 2*(8*5+4800/25*5) + 2 + 256

func NewDecoder(
	receivedRFLSF func(lsf LSF, ber float64) error,
	receivedRFStream func(lsf LSF, payload []byte, sid, fn uint16, ber float64) error,
	receivedRFStreamLICH func(lsf LSF, ber float64) error,
	receivedRFStreamEOT func(lsf LSF, sid, fn uint16, ber float64) error,
	receivedRFPacket func(lsf LSF, payload []byte, ber float64) error,
) *Decoder {
	d := Decoder{
		receivedRFLSF:        receivedRFLSF,
		receivedRFStream:     receivedRFStream,
		receivedRFStreamLICH: receivedRFStreamLICH,
		receivedRFStreamEOT:  receivedRFStreamEOT,
		receivedRFPacket:     receivedRFPacket,
		lastPacketFN:         0xff,
		lastStreamFN:         0xffff,
		lsfBytes:             make([]byte, 30),
	}
	return &d
}

func (d *Decoder) DecodeFrame(typ uint16, softBits []SoftBit) {
	switch {
	case typ == LSFSync && d.syncedType == 0:
		d.gotLSF = false
		var e int
		d.lsf, e = decodeLSF(softBits)
		if d.lsf.CheckCRC() {
			log.Printf("[DEBUG] Received RF LSF: %s", d.lsf)
			d.gotLSF = true
			d.timeoutCnt = 0
			d.lastStreamFN = 0xffff
			d.lastPacketFN = 0xff
			d.errors = e
			d.bits = 368

			if d.lsf.Type[1]&byte(LSFTypeStream) == byte(LSFTypeStream) {
				d.syncedType = StreamSync
				d.lichParts = 0
				d.streamFN = 0
				d.streamID = uint16(rand.Intn(0x10000))
			} else { // packet mode
				d.syncedType = PacketSync
				d.packetData = make([]byte, 33*25)
			}
			d.receivedRFLSF(*d.lsf, float64(e)/3.68)
			// } else {
			// 	log.Printf("[DEBUG] Received RF LSF with bad CRC: %s", d.lsf)
		}

	case typ == PacketSync && d.syncedType == PacketSync:
		pktFrame, e := d.decodePacketFrame(softBits)
		d.errors += e
		d.bits += 368
		// log.Printf("[DEBUG] pktFrame: % x", pktFrame)
		lastFrame := (pktFrame[25] >> 7) != 0

		// If lastFrame is true, this value is the byte count in the frame,
		// otherwise it's the frame number
		frameNumOrByteCnt := byte((pktFrame[25] >> 2) & 0x1F)

		if lastFrame && frameNumOrByteCnt > 25 {
			log.Printf("[INFO] Fixing overrun in last frame: %d > 25", frameNumOrByteCnt)
			frameNumOrByteCnt = 25
		}

		log.Printf("[DEBUG] pktFrame[25]: %b, frameNumOrByteCnt: %d, last: %v", pktFrame[25], frameNumOrByteCnt, lastFrame)
		if lastFrame {
			log.Printf("[DEBUG] Frame %d BER: %1.1f", d.lastPacketFN+1, float64(e)/3.68)
		} else {
			log.Printf("[DEBUG] Frame %d BER: %1.1f", frameNumOrByteCnt, float64(e)/3.68)
		}
		// log.Printf("[DEBUG] frameData: % x %s", pktFrame, pktFrame)

		if frameNumOrByteCnt <= 31 && frameNumOrByteCnt == d.lastPacketFN+1 && !lastFrame {
			copy(d.packetData[frameNumOrByteCnt*25:(frameNumOrByteCnt+1)*25], pktFrame)
			d.lastPacketFN++
		} else if lastFrame {
			copy(d.packetData[(d.lastPacketFN+1)*25:(d.lastPacketFN+1)*25+frameNumOrByteCnt], pktFrame[:frameNumOrByteCnt])
			d.packetData = d.packetData[:(d.lastPacketFN+1)*25+frameNumOrByteCnt]
			// log.Printf("[DEBUG] pktFrame[:frameNumOrByteCnt]: % 0x, d.packetData: % 0x", pktFrame[:frameNumOrByteCnt], d.packetData)
			log.Printf("[DEBUG] d.packetData: [% 2x]", d.packetData)
			if CRC(d.packetData) == 0 {
				d.receivedRFPacket(*d.lsf, d.packetData, float64(d.errors)/float64(d.bits)*100)
			} else {
				log.Printf("[DEBUG] Bad CRC not forwarded: %x", CRC(d.packetData))
			}
			// cleanup
			d.reset()
		}

	case typ == StreamSync:
		var lich []byte
		var lichCnt byte
		var e int
		var fn uint16
		d.frameData, lich, fn, lichCnt, e = d.decodeStreamFrame(softBits)
		// log.Printf("[DEBUG] frameData: [% 2x], lich: %02x, lichCnt: %d, d.lichParts: %04x, fn: %04x, d.lastStreamFN: %04x, e: %d", d.frameData, lich, lichCnt, d.lichParts, fn, d.lastStreamFN, e)
		d.errors += e
		d.bits += 272
		if d.lastStreamFN+1 <= fn&0x7fff {
			if d.lichParts != 0x3F && lichCnt < 6 { //6 chunks = 0b111111
				//reconstruct LSF chunk by chunk
				copy(d.lsfBytes[lichCnt*5:lichCnt*5+5], lich)
				d.lichParts |= (1 << lichCnt)
				if d.lichParts == 0x3F {
					d.lichParts = 0
					lsfB := NewLSFFromBytes(d.lsfBytes)
					if lsfB.CheckCRC() {
						d.lsf = lsfB
						d.gotLSF = true
						d.timeoutCnt = 0
						// log.Printf("[DEBUG] Received stream LSF: %v", lsfB)
						d.receivedRFStreamLICH(*d.lsf, float64(d.errors)/float64(d.bits)*100)
					} else {
						log.Printf("[DEBUG] Stream LSF CRC error: %v", lsfB)
					}
				}
			}
			// log.Printf("[DEBUG] Received stream frame: FN:%04X, LICH_CNT:%d, e: %d, BER: %1.1f", fn, lichCnt, e, float64(e)/2.72)
			// The last-frame flag is a single bit inside the convolutionally
			// coded payload, so a Viterbi failure can set it and end the over
			// early — after which the decoder resets and the gateway stops
			// forwarding until the operator re-keys. Honour it only on a
			// plausibly sequential frame: a burst large enough to flip bit 15
			// will usually disturb its neighbours too, and so fail this check.
			//
			// Rejecting a genuine last frame here is cheap: the transmitter
			// sends an EOT marker immediately afterwards, and that path (below)
			// terminates the stream. This only gives up the fast path, not the
			// reliable one.
			lastFrame := fn&0x8000 == 0x8000 && fn&0x7fff == (d.lastStreamFN+1)&0x7fff
			if d.gotLSF {
				d.streamFN = fn
				d.receivedRFStream(*d.lsf, d.frameData, d.streamID, d.streamFN, float64(d.errors)/float64(d.bits)*100)
				d.timeoutCnt = 0
				if lastFrame {
					log.Printf("[DEBUG] Last frame for RF voice stream %04x", d.streamID)
					d.receivedRFStreamEOT(*d.lsf, d.streamID, d.streamFN, float64(d.errors)/float64(d.bits)*100)
				}
			}
			if lastFrame {
				d.reset()
			} else {
				d.lastStreamFN = fn
			}
		}
	case typ == EOTMarker && d.syncedType == StreamSync:
		if d.gotLSF {
			// If this was already done above, gotLSF will be false
			d.streamFN = uint16(d.lastStreamFN+1) | 0x8000
			d.receivedRFStreamEOT(*d.lsf, d.streamID, d.streamFN, float64(d.errors)/float64(d.bits)*100)
		}
		// reset
		d.reset()
	}
	//RX sync timeout
	if d.syncedType != 0 {
		d.timeoutCnt++
		if d.timeoutCnt > 960*2 {
			if d.syncedType == StreamSync && d.gotLSF && d.lastStreamFN&0x8000 != 0x8000 {
				// If we timed out of a voice stream without a last frame, send the Voice End here
				log.Printf("[DEBUG] Timed out RF voice stream %04x", d.streamID)
				d.receivedRFStreamEOT(*d.lsf, d.streamID, d.streamFN, float64(d.errors)/float64(d.bits)*100)
			}
			d.reset()
		}
	}
}

func decodeLSF(softBit []SoftBit) (*LSF, int) {
	// log.Printf("[DEBUG] decodeLSF: len(pld): %d", len(pld))
	// log.Printf("[DEBUG] softBit: %#v", softBit)

	softBit = DerandomizeSoftBits(softBit)

	dSoftBit := DeinterleaveSoftBits(softBit)

	//decode
	vd := ViterbiDecoder{}
	lsf, e := vd.DecodePunctured(dSoftBit, LSFPuncturePattern)
	e = e - len(LSFPuncturePattern) + 1

	//shift the buffer 1 position left - get rid of the encoded flushing bits
	lsf = lsf[1 : LSFLen+1]
	// log.Printf("[DEBUG] lsf: %x", lsf)
	// if CRC(lsf) == 0 {
	// 	dst, err := DecodeCallsign(lsf[0:6])
	// 	if err != nil {
	// 		log.Printf("[ERROR] Bad dst callsign: %v", err)
	// 	}
	// 	src, err := DecodeCallsign(lsf[6:12])
	// 	if err != nil {
	// 		log.Printf("[ERROR] Bad src callsign: %v", err)
	// 	}
	// 	log.Printf("[DEBUG] dest: %s, src: %s", dst, src)
	// 	log.Printf("[DEBUG] LSF BER: %1.1f", float64(e)/3.68)
	// 	// } else {
	// 	// 	log.Printf("[DEBUG] Bad LSF CRC: %x", CRC(lsf))
	// }
	l := NewLSFFromBytes(lsf)
	return l, e
}

func (d *Decoder) decodeStreamFrame(softBit []SoftBit) (frameData []byte, lich []byte, fn uint16, lichCnt byte, e int) {
	// log.Printf("[DEBUG] decodeStreamFrame: len(pld): %d", len(pld))
	// log.Printf("[DEBUG] pld: [% 1.1f]", pld)

	softBit = DerandomizeSoftBits(softBit)

	dSoftBit := DeinterleaveSoftBits(softBit)

	lich = DecodeLICH(dSoftBit[:96])
	lichCnt = lich[5] >> 5

	//decode
	vd := ViterbiDecoder{}
	frameData, e = vd.DecodePunctured(dSoftBit[96:], StreamPuncturePattern)
	e = e - len(StreamPuncturePattern)

	// log.Printf("[DEBUG] frameData[:3]: [% 02x]", frameData[:3])
	fn = (uint16(frameData[1]) << 8) | uint16(frameData[2])

	//shift 1+2 positions left - get rid of the encoded flushing bits and FN
	frameData = frameData[1+2:]

	return frameData, lich, fn, lichCnt, e
}

func (d *Decoder) decodePacketFrame(softBit []SoftBit) ([]byte, int) {
	// log.Printf("[DEBUG] decodePacketFrame: len(pld): %d", len(pld))
	// log.Printf("[DEBUG] pld: %#v", pld)

	softBit = DerandomizeSoftBits(softBit)
	// log.Printf("[DEBUG] derandomized softBit: %#v", softBit)

	dSoftBit := DeinterleaveSoftBits(softBit)
	// log.Printf("[DEBUG] dSoftBit: %#v", dSoftBit)

	//decode
	vd := ViterbiDecoder{}
	pkt, e := vd.DecodePunctured(dSoftBit, PacketPuncturePattern)
	// log.Printf("[DEBUG] pkt: %#v", pkt)
	e = e - len(PacketPuncturePattern)

	return pkt[1:], e
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

func (d *Decoder) reset() {
	d.syncedType = 0
	d.lsf = nil
	d.gotLSF = false
	d.timeoutCnt = 0
	d.lastPacketFN = 0xff
	d.lastStreamFN = 0xffff
	d.lichParts = 0
	d.errors = 0
	d.bits = 0
}
