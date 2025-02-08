package m17

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"unicode/utf8"
)

type LSFType byte
type LSFDataType byte
type LSFEncryptionType byte
type LSFEncryptionSubtype byte

const (
	LSFTypePacket LSFType = iota
	LSFTypeStream
)

const (
	LSFDataTypeReserved LSFDataType = iota
	LSFDataTypeData
	LSFDataTypeVoice
	LSFDataTypeVoiceData
)

const (
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
	LSFLen = 30

	typeLen = 2
	metaLen = 112 / 8

	dstPos  = 0
	srcPos  = dstPos + EncodedCallsignLen
	typPos  = srcPos + EncodedCallsignLen
	metaPos = typPos + typeLen
	crcPos  = metaPos + metaLen
)

// Link Setup Frame
type LSF struct {
	Dst  []byte
	Src  []byte
	Type []byte
	Meta []byte
	CRC  []byte
}

func NewEmptyLSF() LSF {
	return LSF{
		Dst:  make([]byte, 0, EncodedCallsignLen),
		Src:  make([]byte, 0, EncodedCallsignLen),
		Type: make([]byte, 0, typeLen),
		Meta: make([]byte, 0, metaLen),
		CRC:  make([]byte, 0, CRCLen),
	}
}
func NewLSF(destCall, sourceCall string, t LSFType, dt LSFDataType, can byte) (LSF, error) {
	var err error
	lsf := NewEmptyLSF()
	lsf.Dst, err = EncodeCallsign(destCall)
	if err != nil {
		return lsf, fmt.Errorf("bad dst callsign: %w", err)
	}
	lsf.Src, err = EncodeCallsign(sourceCall)
	if err != nil {
		return lsf, fmt.Errorf("bad src callsign: %w", err)
	}
	lsf.Type = append(lsf.Type, (can & 0x7), (byte(t)&0x1)|((byte(dt)&0x3)<<1))
	// lsf.Type[0] = (can & 0x7)
	// lsf.Type[1] = (byte(t) & 0x1) | ((byte(dt) & 0x3) << 1)
	return lsf, nil
}

func NewLSFFromBytes(buf []byte) LSF {
	var lsf LSF
	lsf.Dst = buf[dstPos:srcPos]
	lsf.Src = buf[srcPos:typPos]
	lsf.Type = buf[typPos:metaPos]
	lsf.Meta = buf[metaPos:crcPos]
	lsf.CRC = buf[crcPos : crcPos+CRCLen]
	return lsf
}

// Convert this LSF to a byte slice suitable for transmission
func (l *LSF) ToBytes() []byte {
	b := make([]byte, LSFLen)

	copy(b[dstPos:srcPos], l.Dst)
	copy(b[srcPos:typPos], l.Src)
	copy(b[typPos:metaPos], l.Type)
	copy(b[metaPos:crcPos], l.Meta)
	copy(b[crcPos:crcPos+CRCLen], l.CRC)
	// log.Printf("[DEBUG] LSF.ToBytes(): %#v", b)

	return b
}

// Calculate CRC for this LSF
func (l *LSF) CalcCRC() uint16 {
	a := l.ToBytes()
	crc := CRC(a[:LSFLen-2])
	l.CRC, _ = binary.Append(nil, binary.BigEndian, crc)
	return crc
}

// M17 packet
type Packet struct {
	LSF     LSF
	Type    PacketType
	Payload []byte
	CRC     uint16
}

func NewPacketFromBytes(buf []byte) Packet {
	var p Packet
	p.LSF = NewLSFFromBytes(buf[:LSFLen])
	t, size := utf8.DecodeRune(buf[LSFLen:])
	p.Type = PacketType(t)
	p.Payload = buf[LSFLen+size : len(buf)-2]
	_, err := binary.Decode(buf[len(buf)-2:], binary.BigEndian, &p.CRC)
	if err != nil {
		// should never happen
		log.Printf("[ERROR] Error decoding CRC: %v", err)
	}
	return p
}
func NewPacket(lsf LSF, t PacketType, data []byte) Packet {
	p := Packet{
		LSF:  lsf,
		Type: t,
	}
	p.Payload = append(p.Payload, data...)
	pb := p.PayloadBytes()
	p.CRC = CRC(pb[:len(pb)-2])
	return p
}

// Convert this Packet to a byte slice suitable for transmission
func (p *Packet) ToBytes() []byte {
	pb := p.PayloadBytes()
	b := make([]byte, LSFLen+len(pb))
	copy(b[:LSFLen], p.LSF.ToBytes())
	copy(b[LSFLen:], pb)
	return b
}

// Convert the payload (type, message and CRC) to a byte slice suitable for transmission
func (p *Packet) PayloadBytes() []byte {
	b := utf8.AppendRune(nil, rune(p.Type))
	b = append(b, p.Payload...)
	b, err := binary.Append(b, binary.BigEndian, p.CRC)
	if err != nil {
		// should never happen
		log.Printf("[ERROR] Error encoding CRC: %v", err)
	}
	return b
}

func (p *Packet) Send(out io.Writer) error {
	var full_packet = make([]float32, 0, 36*192*10) //full packet, symbols as floats - 36 "frames" max (incl. preamble, LSF, EoT), 192 symbols each, sps=10:
	var enc_bits [SymbolsPerPayload * 2]uint8       //type-2 bits, unpacked
	var rf_bits [SymbolsPerPayload * 2]uint8        //type-4 bits, unpacked
	// var pkt_sym_cnt uint32
	var pkt_chunk = make([]uint8, 25+1) //chunk of Packet Data, up to 25 bytes plus 6 bits of Packet Metadata
	var full_packet_data = p.PayloadBytes()

	//encode LSF data
	// lcrc := p.LSF.CalcCRC()
	// pcrc := CRC(full_packet_data[:len(full_packet_data)-2])
	// log.Printf("[DEBUG] lsf: %#v, lcrc: %x, packetData: %#v, pcrc: %x", p.LSF.ToBytes(), lcrc, full_packet_data, pcrc)
	conv_encode_LSF(&enc_bits, &p.LSF)
	//fill preamble
	full_packet = AppendPreamble(full_packet, PREAM_LSF)

	//send LSF syncword
	full_packet = AppendSyncword(full_packet, LSFSync)

	//reorder bits
	reorder_bits(&rf_bits, &enc_bits)

	//randomize
	randomize_bits(&rf_bits)

	//fill packet with LSF
	full_packet = gen_data(full_packet, &rf_bits)

	pkt_cnt := 0
	numBytes := len(full_packet_data)
	tmp := numBytes
	// log.Printf("[DEBUG] numBytes: %d, full_packet_data: %#v", numBytes, full_packet_data)
	for numBytes > 0 {
		//send packet frame syncword
		full_packet = gen_syncword(full_packet, PacketSync)

		//the following examples produce exactly 25 bytes, which exactly one frame, but >= meant this would never produce a final frame with EOT bit set
		//echo -en "\x05Testing M17 packet mo\x00" | ./m17-packet-encode -S N0CALL -D ALL -C 10 -n 23 -o float.sym -f
		//./m17-packet-encode -S N0CALL -D ALL -C 10 -o float.sym -f -T 'this is a simple text'
		if numBytes > 25 { //fix for frames that, with terminating byte and crc, land exactly on 25 bytes (or %25==0)
			// 		memcpy(pkt_chunk, &full_packet_data[pkt_cnt*25], 25)
			copy(pkt_chunk, full_packet_data[pkt_cnt*25:pkt_cnt*25+25])
			pkt_chunk[25] = uint8(pkt_cnt << 2)
			log.Printf("[DEBUG] FN:%02d (full frame)", pkt_cnt)

			//encode the packet frame
			conv_encode_packet_frame(&enc_bits, pkt_chunk)

			//reorder bits
			reorder_bits(&rf_bits, &enc_bits)

			//randomize
			randomize_bits(&rf_bits)

			//fill packet with frame data
			full_packet = gen_data(full_packet, &rf_bits)

			numBytes -= 25
		} else {
			// 		memcpy(pkt_chunk, &full_packet_data[pkt_cnt*25], numBytes)
			copy(pkt_chunk, full_packet_data[pkt_cnt*25:pkt_cnt*25+numBytes])
			// 		memset(&pkt_chunk[numBytes], 0, 25-numBytes) //zero-padding
			for i := numBytes; i < 25; i++ {
				pkt_chunk[i] = 0
			}

			//EOT bit set to 1, set counter to the amount of bytes in this (the last) frame
			if numBytes%25 == 0 {
				pkt_chunk[25] = (1 << 7) | ((25) << 2)

			} else {
				pkt_chunk[25] = uint8((1 << 7) | ((numBytes % 25) << 2))

			}

			// 		fprintf(stderr, "FN:-- (ending frame)\n")

			//encode the packet frame
			conv_encode_packet_frame(&enc_bits, pkt_chunk)

			//reorder bits
			reorder_bits(&rf_bits, &enc_bits)

			//randomize
			randomize_bits(&rf_bits)

			//fill packet with frame data
			full_packet = gen_data(full_packet, &rf_bits)

			numBytes = 0
		}

		//debug dump
		//for(uint8_t i=0; i<26; i++)
		//fprintf(stderr, "%02X", pkt_chunk[i]);
		//fprintf(stderr, "\n");
		// log.Printf("[DEBUG] numBytes: %d", numBytes)
		pkt_cnt++
	}

	numBytes = tmp //bring back the numBytes value
	// fprintf (stderr, "PKT:");
	// for i=0; i<pkt_cnt*25; i++    {
	//     if ( (i != 0) && ((i%25) == 0) )
	//         fprintf (stderr, "\n    ");

	//     fprintf (stderr, " %02X", full_packet_data[i]);
	// }
	// fprintf(stderr, "\n");

	//send EOT
	full_packet = gen_eot(full_packet)

	//debug mode - symbols multiplied by 7168 scaling factor
	/*for(uint16_t i=0; i<pkt_sym_cnt; i++)
	  {
	      int16_t val=roundf(full_packet[i]*RRC_DEV);
	      fwrite(&val, 2, 1, fp);
	  }*/
	// log.Printf("[DEBUG] Sending %#v", full_packet)

	for _, val := range full_packet {
		f := float32(math.Round(float64(val)))
		// b, _ := binary.Append(nil, binary.LittleEndian, f)
		// log.Printf("[DEBUG] val: %f, f: %5.3f, bytes: %v", val, f, b)
		err := binary.Write(out, binary.LittleEndian, f)
		if err != nil {
			return fmt.Errorf("failed to send: %w", err)
		}
	}

	return nil
}

/**
 * @brief Generate symbol stream for a preamble.
 *
 * @param out Frame buffer (192 floats).
 * @param cnt Pointer to a variable holding the number of written symbols.
 * @param type Preamble type (pre-BERT or pre-LSF).
 */
func AppendPreamble(out []float32, typ Pream) []float32 {
	if typ == PREAM_BERT { //pre-BERT
		for i := 0; i < SymbolsPerFrame/2; i++ { //40ms * 4800 = 192
			out = append(out, -3.0, +3.0)
		}
	} else { // type==PREAM_LSF //pre-LSF
		for i := 0; i < SymbolsPerFrame/2; i++ { //40ms * 4800 = 192
			out = append(out, +3.0, -3.0)
		}
	}
	return out
}

// Generate symbol stream for a syncword.
func AppendSyncword(out []float32, syncword uint16) []float32 {
	for i := 0; i < SymbolsPerSyncword*2; i += 2 {
		out = append(out, float32(SymbolMap[(syncword>>(14-i))&3]))
	}
	return out
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
func gen_data(out []float32, in *[SymbolsPerPayload * 2]uint8) []float32 {
	// log.Printf("[DEBUG] gen_data(%#v, %#v)", out, *in)
	for i := 0; i < SymbolsPerPayload; i++ { //40ms * 4800 - 8 (syncword)
		out = append(out, float32(SymbolMap[in[2*i]*2+in[2*i+1]]))
	}
	return out
}

/**
 * @brief Generate symbol stream for a syncword.
 *
 * @param out Output buffer (8 floats).
 * @param cnt Pointer to a variable holding the number of written symbols.
 * @param syncword Syncword.
 */
func gen_syncword(out []float32, syncword uint16) []float32 {
	for i := 0; i < SymbolsPerSyncword*2; i += 2 {
		out = append(out, SymbolMap[(syncword>>(14-i))&3])
	}
	return out
}

// convol.c
/**
 * @brief Encode M17 packet frame using convolutional encoder with puncturing.
 *
 * @param out Output - unpacked array of bits, 368 type-3 bits.
 * @param in Input - packed array of uint8_t, 206 type-1 bits
 *   (200 bits of data, 1 bit End of Frame indicator, 5 bits frame/byte counter).
 */
func conv_encode_packet_frame(out *[368]byte, in []byte) [368]byte {
	pp_len := len(PuncturePattern3)
	p := 0                      //puncturing pattern index
	var pb uint16 = 0           //pushed punctured bits
	ud := make([]byte, 206+4+4) //unpacked data

	//unpack data
	for i := 0; i < 26; i++ {
		for j := 0; j < 8; j++ {
			if i <= 24 || j <= 5 {
				ud[4+i*8+j] = (in[i] >> (7 - j)) & 1
			}
		}
	}

	//encode
	for i := 0; i < 206+4; i++ {
		G1 := (ud[i+4] + ud[i+1] + ud[i+0]) % 2
		G2 := (ud[i+4] + ud[i+3] + ud[i+2] + ud[i+0]) % 2

		//fprintf(stderr, "%d%d", G1, G2);

		if PuncturePattern3[p] != 0 {
			out[pb] = G1
			pb++
		}

		p++
		p %= pp_len

		if PuncturePattern3[p] != 0 {
			out[pb] = G2
			pb++
		}

		p++
		p %= pp_len
	}
	return *out
	//fprintf(stderr, "pb=%d\n", pb);
}

/**
 * @brief Generate symbol stream for the End of Transmission marker.
 *
 * @param out Output buffer (192 floats).
 * @param cnt Pointer to a variable holding the number of written symbols.
 */
func gen_eot(out []float32) []float32 {
	for i := 0; i < SymbolsPerFrame; i++ { //40ms * 4800 = 192
		out = append(out, EOTSymbols[i%8])
	}
	return out
}
