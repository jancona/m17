package modem

import "testing"

func TestTXPowerByte(t *testing.T) {
	// The firmware reads the byte as int8 and multiplies by 0.25 to get dBm.
	for _, dbm := range []int8{-16, -1, 0, 10, 14} {
		got := float64(int8(txPowerByte(dbm))) * 0.25
		if got != float64(dbm) {
			t.Errorf("txPowerByte(%d) decodes to %.2f dBm, want %d", dbm, got, dbm)
		}
	}
}
