package m17

import "testing"

// TestSX1255GainTables pins the LNA and TX DAC gain mappings to the SX1255
// datasheet.
//
// Both fields previously counted in the wrong direction, which is easy to do
// and hard to see: an LNA setting of 6 dB selected maximum gain, 36 dB selected
// minimum, 0 and 48 dB both landed on the reserved code 000, and every DAC
// setting produced the opposite transmit level. None of that is visible without
// the datasheet in hand, so it is written down here.
//
// Reference: SX1255 datasheet rev 3.1, registers 0x0C (RXFE1) and 0x08 (TXFE1).
func TestSX1255GainTables(t *testing.T) {
	t.Run("LNA codes match the datasheet", func(t *testing.T) {
		// 001 is the highest gain and 110 the lowest; 000 and 111 are "not used".
		want := map[uint8]byte{48: 1, 42: 2, 36: 3, 24: 4, 12: 5, 0: 6}
		for db, code := range want {
			got, actual := sx1255LNACode(db)
			if got != code || actual != db {
				t.Errorf("sx1255LNACode(%d) = code %d (%d dB), want code %d (%d dB)",
					db, got, actual, code, db)
			}
		}
		for db := range uint8(49) {
			code, _ := sx1255LNACode(db)
			if code == 0 || code > 6 {
				t.Errorf("sx1255LNACode(%d) = %d, which is reserved on this chip", db, code)
			}
		}
	})

	t.Run("DAC codes match the datasheet", func(t *testing.T) {
		// 011 is full scale, 000 is 9 dB down; 100..111 are test modes.
		want := map[int8]byte{0: 3, -3: 2, -6: 1, -9: 0}
		for db, code := range want {
			got, actual := sx1255DACCode(db)
			if got != code || actual != db {
				t.Errorf("sx1255DACCode(%d) = code %d (%d dB), want code %d (%d dB)",
					db, got, actual, code, db)
			}
		}
		for db := int8(-12); db <= 3; db++ {
			code, _ := sx1255DACCode(db)
			if code > 3 {
				t.Errorf("sx1255DACCode(%d) = %d, which selects a test-Vref mode", db, code)
			}
		}
	})

	t.Run("unsupported values snap to the nearest supported one", func(t *testing.T) {
		// The chip's LNA steps are uneven, so these are neither rounded down nor
		// evenly spaced. Exact ties resolve to the higher gain — see the note on
		// sx1255LNACode.
		for _, c := range []struct{ req, want uint8 }{
			{6, 12},  // tie between 0 and 12
			{18, 24}, // tie between 12 and 24
			{30, 36}, // tie between 24 and 36
			{45, 48}, // tie between 42 and 48
			{44, 42}, // not a tie: nearer 42
			{47, 48},
		} {
			if _, actual := sx1255LNACode(c.req); actual != c.want {
				t.Errorf("sx1255LNACode(%d) snapped to %d dB, want %d dB", c.req, actual, c.want)
			}
		}
		if _, actual := sx1255DACCode(-5); actual != -6 {
			t.Errorf("sx1255DACCode(-5) snapped to %d dB, want -6 dB", actual)
		}
	})
}
