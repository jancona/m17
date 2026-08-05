package m17

// SX1255 analog gain register tables.
//
// These live outside the Linux-only SPI file so they can be unit-tested on any
// platform. Both fields were wrong for a long time in a way that is hard to see
// from the code alone — each counts in the opposite direction to what a naive
// reading suggests — so they are pinned by TestSX1255GainTables.

// sx1255LNAGains maps the rx_lna_gain field (reg 0x0C bits [7:5]) to gain in dB.
//
// Per the SX1255 datasheet the codes are attenuation from the LNA's maximum, in
// uneven steps, and they count *down* in gain as the code rises:
//
//	000 = not used      100 = max − 24 dB
//	001 = max −  0 dB   101 = max − 36 dB
//	010 = max −  6 dB   110 = max − 48 dB
//	011 = max − 12 dB   111 = not used
//
// They are presented here as absolute gain, taking the maximum as 48 dB, which
// is how the setting is expressed in configuration. Only these six values exist
// — there is no 6, 18 or 30 dB setting — and codes 000 and 111 must never be
// written.
var sx1255LNAGains = []struct {
	db   uint8
	code byte
}{
	{48, 1}, {42, 2}, {36, 3}, {24, 4}, {12, 5}, {0, 6},
}

// sx1255DACGains maps the tx_dac_gain field (reg 0x08 bits [6:4]) to gain in dB
// relative to the DAC's maximum.
//
// Per the SX1255 datasheet the codes count *up* from the quietest setting:
//
//	000 = max − 9 dB    010 = max − 3 dB
//	001 = max − 6 dB    011 = max, 0 dBFS (rail-to-rail)
//
// Note this is a three-bit field. Codes 100..111 repeat the same four levels but
// with a test Vref voltage and are explicitly not recommended, so bit 6 is
// always written zero rather than preserved.
var sx1255DACGains = []struct {
	db   int8
	code byte
}{
	{-9, 0}, {-6, 1}, {-3, 2}, {0, 3},
}

// sx1255LNACode returns the register code for the supported LNA gain nearest to
// db, and the gain that code actually selects.
//
// The steps are uneven, so a request can fall exactly between two of them — 18
// is 6 dB from both 12 and 24. Both tables are ordered high to low and the
// search keeps only a strictly better match, so such ties resolve to the higher
// gain. That is the useful direction for a receiver: noise figure is set by the
// front end, and too little gain costs sensitivity that no later stage recovers.
func sx1255LNACode(db uint8) (code byte, actual uint8) {
	best := sx1255LNAGains[0]
	for _, g := range sx1255LNAGains[1:] {
		if absDiffInt(int(db), int(g.db)) < absDiffInt(int(db), int(best.db)) {
			best = g
		}
	}
	return best.code, best.db
}

// sx1255DACCode returns the register code for the supported DAC gain nearest to
// db, and the gain that code actually selects.
func sx1255DACCode(db int8) (code byte, actual int8) {
	best := sx1255DACGains[0]
	for _, g := range sx1255DACGains[1:] {
		if absDiffInt(int(db), int(g.db)) < absDiffInt(int(db), int(best.db)) {
			best = g
		}
	}
	return best.code, best.db
}

func absDiffInt(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
