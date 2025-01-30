/*
Copyright (C) 2024 Steve Miller KC1AWV

This program is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License as published by the Free
Software Foundation, either version 3 of the License, or (at your option)
any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
more details.

You should have received a copy of the GNU General Public License along with
this program. If not, see <http://www.gnu.org/licenses/>.
*/

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

const base40Chars = " ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-/."

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
	address := uint64(0)

	for i := len(callsign) - 1; i >= start; i-- {
		c := callsign[i]
		val := 0
		switch {
		case 'A' <= c && c <= 'Z':
			val = int(c-'A') + 1
		case '0' <= c && c <= '9':
			val = int(c-'0') + 27
		case c == '-':
			val = 37
		case c == '/':
			val = 38
		case c == '.':
			val = 39
		default:
			return nil, fmt.Errorf("invalid character in callsign: %c", c)
		}

		address = address*40 + uint64(val)
	}

	if start == 1 { //starts with a hash?
		address += MaxEncodedCallsign + 1 //40^9
	}

	result := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		result[i] = byte(address & 0xFF)
		address >>= 8
	}

	return result, nil
}

func DecodeCallsign(encoded []byte) (string, error) {
	if len(encoded) != EncodedLen {
		return "", fmt.Errorf("encoded callsign length (%d) != %d", len(encoded), EncodedLen)
	}
	var address uint64
	for _, b := range encoded {
		address = address*256 + uint64(b)
	}

	callsign := ""
	if address == EncodedDestinationAll {
		return DestinationAll, nil
	} else if address > MaxEncodedCallsign {
		if address >= SpecialEncodedRange {
			return "", fmt.Errorf("encoded callsign value (%x) is not valid", address)
		}
		callsign = "#"
		address -= MaxEncodedCallsign + 1
	}

	for address > 0 {
		idx := address % 40
		callsign += string(base40Chars[idx])
		address /= 40
	}

	return callsign, nil
}
