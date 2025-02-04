package m17

import (
	"encoding/binary"
	"io"
	"unicode/utf8"
)

type LSFType byte
type LSFDataType byte
type LSFEncryptionType byte
type LSFEncryptionSubtype byte

const (
	LSFTypePacket LSFType = iota
	LSFTypeStream

	LSFDataTypeReserved LSFDataType = iota
	LSFDataTypeData
	LSFDataTypeVoice
	LSFDataTypeVoiceData

	LSFEncryptionTypeNone LSFEncryptionType = iota
	LSFEncryptionTypeScrambler
	LSFEncryptionTypeAES
	LSFEncryptionTypeOther
)

type PacketType rune

const (
	PacketTypeRAW     PacketType = 0x00
	PacketTypeAX25    PacketType = 0x01
	PacketTypeAPRS    PacketType = 0x02
	PacketType6LoWPAN PacketType = 0x03
	PacketTypeIPv4    PacketType = 0x04
	PacketTypeSMS     PacketType = 0x05
	PacketTypeWinlink PacketType = 0x06
)

const (
	LSFSize = 30

	metaSize = 112 / 8
)

// Link Setup Frame
type LSF struct {
	Dst  []byte
	Src  []byte
	Type []byte
	Meta []byte
	CRC  []byte
}

func NewLSFFromBytes(buf []byte) LSF {
	var lsf LSF
	lsf.Dst = buf[0:6]
	lsf.Src = buf[6:12]
	lsf.Type = buf[12:14]
	lsf.Meta = buf[14 : 14+metaSize]
	lsf.CRC = buf[14+metaSize : 14+metaSize+2]
	return lsf
}

// func NewLSF(dst string, src string, signed bool, can byte, typ LSFType, dt LSFDataType, et LSFEncryptionType, est LSFEncryptionSubtype, meta []byte) (LSF, error) {
// 	var lsf = LSF{
// 		Type: make([]byte, 2),
// 	}
// 	var err error

// 	lsf.Dst, err = EncodeCallsign(dst)
// 	if err != nil {
// 		return lsf, fmt.Errorf("error encoding dst: %w", err)
// 	}
// 	lsf.Src, err = EncodeCallsign(src)
// 	if err != nil {
// 		return lsf, fmt.Errorf("error encoding src: %w", err)
// 	}
// 	t1 := can & 0x0f
// 	if signed {
// 		t1 |= 0x10
// 	}
// 	t2 := byte(typ&0x1) | byte(dt&0x3<<1) | byte(et&0x3<<3) | byte(est&0x3<<5)
// 	lsf.Type[0] = t1
// 	lsf.Type[1] = t2
// 	lsf.Meta = meta[:metaSize]
// 	lsf.CalcCRC()
// 	return lsf, err
// }

// Convert this LSF to a byte slice suitable for transmission
func (l *LSF) ToBytes() []byte {
	b := make([]byte, LSFSize)

	copy(b[0:6], l.Dst)
	copy(b[6:12], l.Src)
	copy(b[12:14], l.Type)
	copy(b[14:14+metaSize], l.Meta)
	copy(b[14+metaSize:14+metaSize+2], l.CRC)
	// log.Printf("[DEBUG] LSF.ToBytes(): %#v", b)

	return b
}

// Calculate CRC for this LSF
func (l *LSF) CalcCRC() []byte {
	a := l.ToBytes()
	l.CRC, _ = binary.Append(nil, binary.BigEndian, CRC(a[:LSFSize-2]))
	return l.CRC
}

// M17 packet
type Packet struct {
	LSF     LSF
	Type    PacketType
	Payload []byte
}

func NewPacketFromBytes(buf []byte) Packet {
	var p Packet
	p.LSF = NewLSFFromBytes(buf[:LSFSize])
	t, size := utf8.DecodeRune(buf[LSFSize:])
	p.Type = PacketType(t)
	p.Payload = buf[LSFSize+size:]
	return p
}
func NewPacket(lsf LSF, t PacketType, data []byte) Packet {
	p := Packet{
		LSF:  lsf,
		Type: t,
	}
	p.Payload = append(p.Payload, data...)
	p.Payload, _ = binary.Append(p.Payload, binary.BigEndian, CRC(data))
	return p
}

// Convert this Packet to a byte slice suitable for transmission
func (p *Packet) ToBytes() []byte {
	b := make([]byte, LSFSize+1+len(p.Payload))
	copy(b[:LSFSize], p.LSF.ToBytes())
	size := utf8.EncodeRune(b[LSFSize:], rune(p.Type))
	copy(b[LSFSize+size:], p.Payload)
	return b
}

func SendPacket(p Packet, out io.Writer) error {
	var full_packet = make([]float32, 36*192*10) //full packet, symbols as floats - 36 "frames" max (incl. preamble, LSF, EoT), 192 symbols each, sps=10:
	var enc_bits [SymbolsPerPayload * 2]uint8    //type-2 bits, unpacked
	var rf_bits [SymbolsPerPayload * 2]uint8     //type-4 bits, unpacked
	var pkt_sym_cnt uint32

	//encode LSF data
	conv_encode_LSF(&enc_bits, &p.LSF)
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
