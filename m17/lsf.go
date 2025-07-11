package m17

import (
	"encoding/binary"
	"fmt"
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

const (
	LSFLen = 30
	LSDLen = 28

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

func NewLSFFromLSD(lsd []byte) LSF {
	var lsf LSF
	copy(lsf.Dst[:], lsd[dstPos:srcPos])
	copy(lsf.Src[:], lsd[srcPos:typPos])
	copy(lsf.Type[:], lsd[typPos:metaPos])
	copy(lsf.Meta[:], lsd[metaPos:crcPos])
	lsf.CalcCRC()
	return lsf
}

// Convert this LSF to a byte slice suitable for transmission
func (l *LSF) ToLSDBytes() []byte {
	b := make([]byte, 0, LSDLen)

	b = append(b, l.Dst[:]...)
	b = append(b, l.Src[:]...)
	b = append(b, l.Type[:]...)
	b = append(b, l.Meta[:]...)
	return b
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

func (l *LSF) LSFType() LSFType {
	return LSFType(l.Type[1] & 0x1)
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
