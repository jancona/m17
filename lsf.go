package m17

import (
	"encoding/binary"
	"fmt"
	"log"
)

type LSFType byte

func (t LSFType) String() string {
	switch t {
	case 0:
		return "0 - Packet mode"
	case 1:
		return "1 - Stream mode"
	default:
		return "ERROR"
	}
}

type LSFDataType byte

func (t LSFDataType) String() string {
	switch t {
	case 0:
		return "0 - Reserved"
	case 1:
		return "1 - Data"
	case 2:
		return "2 - Voice"
	case 3:
		return "3 - Voice+Data"
	default:
		return "ERROR"
	}
}

type LSFEncryptionType byte

func (t LSFEncryptionType) String() string {
	switch t {
	case 0:
		return "0 - None"
	case 1:
		return "1 - Scrambler"
	case 2:
		return "2 - AES"
	case 3:
		return "3 - Other/Reserved"
	default:
		return "ERROR"
	}
}

type EncodedCallsign [EncodedCallsignLen]byte

func (e EncodedCallsign) Callsign() string {
	cs, _ := DecodeCallsign(e[:])
	return cs
}

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
	Dst  EncodedCallsign
	Src  EncodedCallsign
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

func NewLSFFromBytes(buf []byte) *LSF {
	var lsf LSF
	copy(lsf.Dst[:], buf[dstPos:srcPos])
	copy(lsf.Src[:], buf[srcPos:typPos])
	copy(lsf.Type[:], buf[typPos:metaPos])
	copy(lsf.Meta[:], buf[metaPos:crcPos])
	copy(lsf.CRC[:], buf[crcPos:crcPos+CRCLen])
	return &lsf
}

func NewLSFFromLSD(lsd []byte) *LSF {
	var lsf LSF
	copy(lsf.Dst[:], lsd[dstPos:srcPos])
	copy(lsf.Src[:], lsd[srcPos:typPos])
	copy(lsf.Type[:], lsd[typPos:metaPos])
	copy(lsf.Meta[:], lsd[metaPos:crcPos])
	lsf.CalcCRC()
	return &lsf
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

func (l *LSF) DataType() LSFDataType {
	return LSFDataType((l.Type[1] >> 1) & 0x3)
}

func (l *LSF) EncryptionType() LSFEncryptionType {
	return LSFEncryptionType((l.Type[1] >> 3) & 0x3)
}

func (l *LSF) EncryptionSubtype() byte {
	return (l.Type[1] >> 5) & 0x3
}

func (l *LSF) GNSS() *GNSS {
	if l.EncryptionType() == 0 && l.EncryptionSubtype() == 0x1 {
		g := NewGNSSFromMeta(l.Meta)
		return g
	}
	return nil
}

func (l *LSF) ECD() *ECD {
	if l.EncryptionType() == 0 && l.EncryptionSubtype() == 0x2 {
		e := NewECDFromMeta(l.Meta)
		return &e
	}
	return nil
}

// Replace META with Extended Callsign Data
func (l *LSF) SetECD(slot1, slot2 *EncodedCallsign) {
	l.Type[1] &= 0x9F     // zero out Encryption Subtype
	l.Type[1] |= 0x2 << 5 // Set it to ECS
	copy(l.Meta[:], slot1[:])
	// slot2 is optional
	if slot2 != nil {
		copy(l.Meta[6:], slot2[:])
	}
	l.Meta[12] = 0
	l.Meta[13] = 0
	l.CalcCRC()
}

func (l *LSF) CAN() byte {
	return (l.Type[0] & 0x7)
}

func (l LSF) String() string {
	s := fmt.Sprintf(`{
	Dst: %s
	Src: %s
	Type: %#v
	Meta: %#v
	CRC: %#v
	LSFType: %v
	DataType: %v
	EncryptionType: %v
	EncryptionSubtype: %v
`,
		l.Dst.Callsign(),
		l.Src.Callsign(),
		l.Type,
		l.Meta,
		l.CRC,
		l.LSFType(),
		l.DataType(),
		l.EncryptionType(),
		l.EncryptionSubtype())
	if l.EncryptionType() == 0 {
		switch l.EncryptionSubtype() {
		case 0x0: // Text Data
			if l.Meta[0] != 0 {
				log.Printf("[DEBUG] Received LSF Text Data")
			}
		case 0x1: // GNSS Position Data
			log.Printf("[DEBUG] Received LSF GNSS Position Data")
			g := l.GNSS()
			if g != nil {
				s += g.String()
			}
		case 0x2: // Extended Callsign Data
			log.Printf("[DEBUG] Received LSF Extended Callsign Data")
			e := l.ECD()
			if e != nil {
				s += e.String()
			}
		}
	}
	s += "\n}"
	return s
}

type GNSS struct {
	DataSource        byte
	StationType       byte
	Radius            byte
	Bearing           uint16
	Latitude          float32
	Longitude         float32
	Altitude          float32
	Speed             float32
	ValidLatLon       bool
	ValidAltitude     bool
	ValidBearingSpeed bool
	ValidRadius       bool
}

func NewGNSSFromMeta(meta [14]byte) *GNSS {
	validity := meta[1] >> 4
	if validity == 0 {
		// Either there are no valid fields or this is V1 GNSS data
		log.Printf("[DEBUG] Empty or V1 GNSS data: [% 02x]", meta)
		return nil
	}
	g := GNSS{
		DataSource:        meta[0] >> 4,
		StationType:       meta[0] & 0x0f,
		Radius:            1 << ((meta[1] & 0x0e) >> 1),
		Bearing:           uint16(meta[1]&0x1)*256 + uint16(meta[2]),
		Speed:             float32((uint16(meta[11])<<4)|uint16(meta[12]>>4)) / 2,
		ValidLatLon:       (validity & 0x08) == 0x08,
		ValidAltitude:     (validity & 0x04) == 0x04,
		ValidBearingSpeed: (validity & 0x02) == 0x02,
		ValidRadius:       (validity & 0x01) == 0x01,
	}

	// The latest spec defines the latitude and longitude fractions as 3 byte
	// 2's complement integers. We OR the three bytes of the int24 value into
	// the *upper* 24 bits of an int32, then rotate the int32 8 bits to the right
	// in order to eliminate the low byte while preserving the sign.
	fraction := ((int32(meta[3]) << 24) | (int32(meta[4]) << 16) | int32(meta[5])<<8) >> 8
	g.Latitude = float32(fraction) / 8388607 * 90
	fraction = ((int32(meta[6]) << 24) | (int32(meta[7]) << 16) | int32(meta[8])<<8) >> 8
	g.Longitude = float32(fraction) / 8388607 * 180

	altitude := (uint16(meta[9]) << 8) | uint16(meta[10])
	g.Altitude = float32(altitude)/2 - 500

	return &g
}

func (g GNSS) String() string {
	s := "	GNSS: {"
	if g.ValidLatLon {
		s += fmt.Sprintf(`
		DataSource:  %02x
		StationType: %02x
		Latitude:    %5.3f
		Longitude:   %5.3f`,
			g.DataSource,
			g.StationType,
			g.Latitude,
			g.Longitude,
		)
	}
	if g.ValidAltitude {
		s += fmt.Sprintf(`
		Altitude:    %5.1f`, g.Altitude)
	}
	if g.ValidBearingSpeed {
		s += fmt.Sprintf(`
		Bearing:     %d
		Speed:       %5.1f`, g.Bearing, g.Speed)
	}
	if g.ValidRadius {
		s += fmt.Sprintf(`
		Radius:      %d`, g.Radius)
	}
	s += "\n	}"
	return s
}

type ECD struct {
	Callsign1 *EncodedCallsign
	Callsign2 *EncodedCallsign
}

func NewECDFromMeta(meta [14]byte) ECD {
	return ECD{
		Callsign1: (*EncodedCallsign)(meta[:6]),
		Callsign2: (*EncodedCallsign)(meta[6:12]),
	}
}

func (e ECD) String() string {
	cs1, err := DecodeCallsign(e.Callsign1[:])
	if err != nil {
		cs1 = err.Error()
	}
	cs2, err := DecodeCallsign(e.Callsign2[:])
	if err != nil {
		cs2 = err.Error()
	}
	s := fmt.Sprintf(`	ECD: {
		Callsign1: %s
		Callsign2: %s
	}`, cs1, cs2)
	return s
}
