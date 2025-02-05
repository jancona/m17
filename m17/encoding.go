package m17

import (
	"fmt"
	"strings"
)

const (
	EncodedLen            = 6
	MaxCallsignLen        = 9
	DestinationAll        = "@ALL"
	EncodedDestinationAll = 0xFFFFFFFFFFFF
	MaxEncodedCallsign    = 0xEE6B27FFFFFF
	SpecialEncodedRange   = 268697600000000 //40^9+40^8
)

var EncodedDestinationAllBytes = []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

const m17Chars = " ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-/."

func EncodeCallsign(callsign string) ([]byte, error) {
	if len(callsign) > MaxCallsignLen {
		return nil, fmt.Errorf("callsign '%s' too long, max %d", callsign, MaxCallsignLen)
	}
	callsign = strings.ToUpper(callsign)
	if callsign == DestinationAll {
		return EncodedDestinationAllBytes, nil
	}
	start := 0
	if callsign[0] == '#' {
		start = 1
	}

	var address uint64 = 0 // the calculate address in host byte order
	var ret = make([]byte, 6)

	// process each char from the end to the beginning
	for i := min(len(callsign), 9) - 1; i >= start; i-- {
		var val byte = 0
		switch {
		case 'A' <= callsign[i] && callsign[i] <= 'Z':
			val = callsign[i] - 'A' + 1

		case '0' <= callsign[i] && callsign[i] <= '9':
			val = callsign[i] - '0' + 27
		case callsign[i] == '-':
			val = 37
		case callsign[i] == '/':
			val = 38
		case callsign[i] == '.':
			val = 39
		case 'a' <= callsign[i] && callsign[i] <= 'z':
			val = callsign[i] - 'a' + 1
		}
		address = 40*address + uint64(val)
	}

	if start == 1 { // starts with a hash?
		address += MaxEncodedCallsign + 1 //40^9
	}

	for i := 5; i >= 0; i-- { // put it in network byte order
		ret[i] = byte(address & 0xff)
		address /= 0x100
	}
	return ret, nil
}

func DecodeCallsign(encoded []byte) (string, error) {
	if len(encoded) != EncodedLen {
		return "", fmt.Errorf("encoded callsign length (%d) != %d", len(encoded), EncodedLen)
	}
	var callsign string
	if encoded == nil { // nothing in , nothing out
		return callsign, nil
	}
	// calculate the address in host byte order
	var address uint64 = 0

	for i := 0; i < 6; i++ {
		address = address*0x100 + uint64(encoded[i])
	}

	if address == EncodedDestinationAll {
		return DestinationAll, nil
	} else if address > MaxEncodedCallsign {
		if address >= SpecialEncodedRange {
			return "", fmt.Errorf("encoded callsign value (%x) is not valid", address)
		}
		callsign = "#"
		address -= MaxEncodedCallsign + 1
	}

	for i := 0; address != 0; i++ {
		// the current character is the address modulus 40
		callsign += string(m17Chars[address%40])
		address /= 40 // keep dividing the address until there ’s nothing left
	}
	return callsign, nil
}
