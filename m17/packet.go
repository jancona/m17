package m17

import (
	"encoding/binary"
	"fmt"
	"log"
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
	Dst  [EncodedCallsignLen]byte
	Src  [EncodedCallsignLen]byte
	Type [typeLen]byte
	Meta [metaLen]byte
	CRC  [CRCLen]byte
}

func NewEmptyLSF() LSF {
	return LSF{}
}

func NewLSF(destCall, sourceCall string, t LSFType, dt LSFDataType, can byte) (LSF, error) {
	var err error
	lsf := NewEmptyLSF()
	dst, err := EncodeCallsign(destCall)
	if err != nil {
		return lsf, fmt.Errorf("bad dst callsign: %w", err)
	}
	lsf.Dst = *dst
	src, err := EncodeCallsign(sourceCall)
	if err != nil {
		return lsf, fmt.Errorf("bad src callsign: %w", err)
	}
	lsf.Src = *src
	if t == 0 {
		// Data Type is only defined for stream mode
		dt = 0
	}
	lsf.Type[0] = (can & 0x7)
	lsf.Type[1] = (byte(t) & 0x1) | ((byte(dt) & 0x3) << 1)
	return lsf, nil
}

func NewLSFFromBytes(buf []byte) LSF {
	var lsf LSF
	copy(lsf.Dst[:], buf[dstPos:srcPos])
	copy(lsf.Src[:], buf[srcPos:typPos])
	copy(lsf.Type[:], buf[typPos:metaPos])
	copy(lsf.Meta[:], buf[metaPos:crcPos])
	copy(lsf.CRC[:], buf[crcPos:crcPos+CRCLen])
	return lsf
}

// Convert this LSF to a byte slice suitable for transmission
func (l *LSF) ToBytes() []byte {
	b := make([]byte, 0, LSFLen)

	b = append(b, l.Dst[:]...)
	b = append(b, l.Src[:]...)
	b = append(b, l.Type[:]...)
	b = append(b, l.Meta[:]...)
	b = append(b, l.CRC[:]...)
	// log.Printf("[DEBUG] LSF.ToBytes(): %#v", b)

	return b
}

// Calculate CRC for this LSF
func (l *LSF) CalcCRC() uint16 {
	a := l.ToBytes()
	crc := CRC(a[:LSFLen-CRCLen])
	crcb, _ := binary.Append(nil, binary.BigEndian, crc)
	copy(l.CRC[:], crcb)
	return crc
}

// Check if the CRC is correct
func (l *LSF) CheckCRC() bool {
	a := l.ToBytes()
	return CRC(a) == 0
}

func (l LSF) String() string {
	dst, _ := DecodeCallsign(l.Dst[:])
	src, _ := DecodeCallsign(l.Src[:])
	return fmt.Sprintf(`{
	Dst: %s,
	Src: %s,
	Type: %#v,
	Meta: %#v,
	CRC: %#v`, dst, src, l.Type, l.Meta, l.CRC)
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

func NewPacket(dst, src string, t PacketType, data []byte) (*Packet, error) {
	lsf, err := NewLSF(dst, src, LSFTypePacket, LSFDataTypeData, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create LSF for Packet: %w", err)
	}
	lsf.CalcCRC()
	p := Packet{
		LSF:  lsf,
		Type: t,
	}
	p.Payload = append(p.Payload, data...)
	pb := p.PayloadBytes()
	p.CRC = CRC(pb[:len(pb)-2])
	return &p, nil
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

// Check if the CRC is correct
func (p *Packet) CheckCRC() bool {
	a := p.PayloadBytes()
	return CRC(a) == 0
}

func (p *Packet) Encode() ([]Symbol, error) {
	outPacket := make([]Symbol, 0, 36*192*10) //full packet, symbols as floats - 36 "frames" max (incl. preamble, LSF, EoT), 192 symbols each, sps=10:
	b, err := ConvolutionalEncode(p.LSF.ToBytes(), LSFPuncturePattern, LSFFinalBit)
	if err != nil {
		return nil, fmt.Errorf("unable to encode LSF: %w", err)
	}
	encodedBits := NewBits(b)
	// encodedBits[0:len(b)] = b[:]
	//fill preamble
	outPacket = appendPreamble(outPacket, lsfPreamble)

	//send LSF syncword
	outPacket = appendSyncword(outPacket, LSFSync)

	rfBits := interleaveBits(encodedBits)
	rfBits = randomizeBits(rfBits)
	// Append LSF to the oputput
	outPacket = appendBits(outPacket, rfBits)

	chunkCnt := 0
	packetData := p.PayloadBytes()
	for bytesLeft := len(packetData); bytesLeft > 0; bytesLeft -= 25 {
		outPacket = appendSyncword(outPacket, PacketSync)
		chunk := make([]byte, 25+1) // 25 bytes from the packet plus 6 bits of metadata
		if bytesLeft > 25 {
			// not the last chunk
			copy(chunk, packetData[chunkCnt*25:chunkCnt*25+25])
			chunk[25] = byte(chunkCnt << 2)
		} else {
			// last chunk
			copy(chunk, packetData[chunkCnt*25:chunkCnt*25+bytesLeft])
			//EOT bit set to 1, set counter to the amount of bytes in this (the last) chunk
			if bytesLeft%25 == 0 {
				chunk[25] = (1 << 7) | ((25) << 2)
			} else {
				chunk[25] = uint8((1 << 7) | ((bytesLeft % 25) << 2))
			}
		}
		//encode the packet chunk
		b, err := ConvolutionalEncode(chunk, PacketPuncturePattern, PacketModeFinalBit)
		if err != nil {
			return nil, fmt.Errorf("unable to encode packet: %w", err)
		}
		encodedBits := NewBits(b)
		rfBits := interleaveBits(encodedBits)
		rfBits = randomizeBits(rfBits)
		// Append chunk to the output
		outPacket = appendBits(outPacket, rfBits)
		chunkCnt++
	}
	outPacket = appendEOT(outPacket)
	return outPacket, nil
}

func (p *Packet) Send(modem Modem /*io.Writer*/) error {
	packet, err := p.Encode()
	if err != nil {
		return fmt.Errorf("failure emcoding packet: %w", err)
	}

	log.Printf("[DEBUG] Sending: %#v", packet)
	err = binary.Write(modem, binary.LittleEndian, packet)
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}
	// for _, val := range packet {
	// 	f := float32(math.Round(float64(val)))
	// 	// b, _ := binary.Append(nil, binary.LittleEndian, f)
	// 	// log.Printf("[DEBUG] val: %f, f: %5.3f, bytes: %v", val, f, b)
	// 	err := binary.Write(out, binary.LittleEndian, f)
	// 	if err != nil {
	// 		return fmt.Errorf("failed to send: %w", err)
	// 	}
	// }

	return nil
}

func (p Packet) String() string {
	var pl string
	if p.Type == 5 {
		pl = string(p.Payload[:len(p.Payload)-1])
	} else {
		pl = fmt.Sprintf("%#v", p.Payload)
	}

	return fmt.Sprintf(`{
	LSF: %s,
	Type: %#v,
	Payload: %s,
	CRC: %#v
}`, p.LSF, p.Type, pl, p.CRC)
}
