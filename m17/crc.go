package m17

import "github.com/sigurn/crc16"

// M17 CRC polynomial
var m17CRCParams = crc16.Params{
	Poly: 0x5935,
	Init: 0xffff,
	Name: "M17",
}

// Calculate CRC value.
func CRC(in []byte) uint16 {
	table := crc16.MakeTable(m17CRCParams)

	return crc16.Checksum(in, table)
}
