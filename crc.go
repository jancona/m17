package m17

const CRCLen = 2

// The M17 CRC-16: polynomial 0x5935, initial value 0xFFFF, no input or
// output reflection, no final XOR. The check value for "123456789" is 0x772B.
const crcPoly = 0x5935

var crcTable = func() (t [256]uint16) {
	for i := range t {
		crc := uint16(i) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ crcPoly
			} else {
				crc <<= 1
			}
		}
		t[i] = crc
	}
	return t
}()

// CRC calculates the M17 CRC of in.
func CRC(in []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range in {
		crc = crc<<8 ^ crcTable[byte(crc>>8)^b]
	}
	return crc
}
