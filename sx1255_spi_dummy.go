//go:build !linux

package m17

import "log"

// spiDevice is a no-op stub for non-Linux platforms.
type spiDevice struct{}

func openSPI(_ string) (*spiDevice, error) {
	log.Print("[DEBUG] SX1255 SPI: no-op (non-Linux)")
	return &spiDevice{}, nil
}

func (s *spiDevice) writeReg(_, _ byte) error { return nil }
func (s *spiDevice) readReg(_ byte) (byte, error) {
	return expectedVersionSX1255, nil // return expected version so init doesn't fail
}
func (s *spiDevice) close() error { return nil }

func (m *SX1255Modem) sx1255Init() error    { return nil }
func (m *SX1255Modem) sx1255Reset() error   { return nil }
func (m *SX1255Modem) sx1255SetRXFreq(_ uint32) error { return nil }
func (m *SX1255Modem) sx1255SetTXFreq(_ uint32) error { return nil }
func (m *SX1255Modem) sx1255SetRate() error { return nil }
func (m *SX1255Modem) sx1255SetLNAGain(_ uint8) error  { return nil }
func (m *SX1255Modem) sx1255SetPGAGain(_ uint8) error  { return nil }
func (m *SX1255Modem) sx1255SetDACGain(_ int8) error   { return nil }
func (m *SX1255Modem) sx1255SetMixerGain(_ float32) error { return nil }
func (m *SX1255Modem) sx1255SetRXPLLBW(_ uint16) error { return nil }
func (m *SX1255Modem) sx1255SetTXPLLBW(_ uint16) error { return nil }
func (m *SX1255Modem) sx1255EnableRX(_ bool) error  { return nil }
func (m *SX1255Modem) sx1255EnableTX(_ bool) error  { return nil }
func (m *SX1255Modem) sx1255EnableRFLoopback(_ bool) error { return nil }
func (m *SX1255Modem) sx1255GetPLLStatus() (bool, bool) { return true, true }
func (m *SX1255Modem) sx1255GetChipVersion() byte { return expectedVersionSX1255 }
func (m *SX1255Modem) gpioSetup(_ string, _ int) error { return nil }
