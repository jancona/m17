package main

import "fmt"

// radioLimits is what a modem type accepts in [Radio]: frequencies in Hz
// (zero for no check) and whether Power is used, and its range in dBm.
type radioLimits struct {
	minHz, maxHz       uint64
	usesPower          bool
	minPower, maxPower float64
	duplex             bool // can receive and transmit at once
}

// limitsFor returns the [Radio] limits for a modem type.
//
//   - The CC1200 HAT covers 420-450 MHz (its filters) and transmits at -16
//     to 14 dBm. It's half-duplex.
//   - The SX1255 HAT tunes 400-510 MHz (its datasheet range; the PLL
//     register tops out at 512 MHz with the 32 MHz crystal). Its transmit
//     level is [Modem] DACGain and MixerGain, relative to the chip's full
//     scale, so Power isn't used.
//   - MMDVM boards check the frequency themselves: MMDVM_HS firmware
//     allows its bands and refuses the rest with a NAK, and repeater
//     boards ignore it, the external radio setting the frequency. Power
//     isn't sent to them.
func limitsFor(modemType string) radioLimits {
	switch modemType {
	case "cc1200", "cc1200v2":
		return radioLimits{minHz: 420e6, maxHz: 450e6, usesPower: true, minPower: -16, maxPower: 14}
	case "sx1255":
		return radioLimits{minHz: 400e6, maxHz: 510e6, duplex: true}
	}
	return radioLimits{duplex: true}
}

// checkRadio checks the configured frequencies and power for modemType,
// returning an error for each value that modem can't use.
func checkRadio(modemType string, rxHz, txHz uint64, power float64) (rxErr, txErr, powerErr error) {
	l := limitsFor(modemType)
	inRange := func(name string, hz uint64) error {
		if l.maxHz == 0 || (hz >= l.minHz && hz <= l.maxHz) {
			return nil
		}
		return fmt.Errorf("configured %s %d out of range for modem type %s (%g to %g MHz)",
			name, hz, modemType, float64(l.minHz)/1e6, float64(l.maxHz)/1e6)
	}
	rxErr = inRange("RXFrequency", rxHz)
	txErr = inRange("TXFrequency", txHz)
	if l.usesPower && (power < l.minPower || power > l.maxPower) {
		powerErr = fmt.Errorf("configured Power %g out of range for modem type %s (%g to %g dBm)",
			power, modemType, l.minPower, l.maxPower)
	}
	return rxErr, txErr, powerErr
}
