//go:build !linux

package m17

func (m *CC1200Modem) gpioSetup(nRSTPin, paEnablePin, boot0Pin int) error {
	return nil
}
