package main

import "testing"

func TestCheckRadio(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		modem                  string
		rx, tx                 uint64
		power                  float64
		wantRX, wantTX, wantPw bool // want an error
	}{
		{"cc1200 in range", "cc1200", 439_000_000, 439_000_000, 10, false, false, false},
		{"cc1200v2 edges", "cc1200v2", 420_000_000, 450_000_000, -16, false, false, false},
		{"cc1200 2 m", "cc1200", 145_000_000, 145_000_000, 10, true, true, false},
		{"cc1200 above the HAT", "cc1200", 439_000_000, 460_000_000, 10, false, true, false},
		{"cc1200 power too high", "cc1200", 439_000_000, 439_000_000, 15, false, false, true},
		{"cc1200 power too low", "cc1200", 439_000_000, 439_000_000, -17, false, false, true},
		{"sx1255 in range", "sx1255", 433_925_000, 438_925_000, 10, false, false, false},
		{"sx1255 edges", "sx1255", 400_000_000, 510_000_000, 10, false, false, false},
		{"sx1255 2 m", "sx1255", 145_000_000, 145_000_000, 10, true, true, false},
		{"sx1255 ignores power", "sx1255", 439_000_000, 439_000_000, 30, false, false, false},
		// The firmware decides (MMDVM_HS), or the external radio (repeaters)
		{"mmdvm 2 m", "mmdvm", 145_500_000, 145_500_000, 10, false, false, false},
		{"mmdvm 10 m repeater", "mmdvm", 29_620_000, 29_520_000, 10, false, false, false},
		{"mmdvm ignores power", "mmdvm", 446_450_000, 441_450_000, 30, false, false, false},
		{"dummy", "dummy", 0, 0, 99, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rxErr, txErr, pwErr := checkRadio(tc.modem, tc.rx, tc.tx, tc.power)
			if (rxErr != nil) != tc.wantRX || (txErr != nil) != tc.wantTX || (pwErr != nil) != tc.wantPw {
				t.Errorf("got rx %v, tx %v, power %v", rxErr, txErr, pwErr)
			}
		})
	}
}
