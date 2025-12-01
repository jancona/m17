//go:build !linux

package m17

func (m *CC1200Modem) gpioSetup(_, _ int) error {
	return nil
}

func (m *CC1200ModemV2) gpioSetup(_, _ int) error {
	return nil
}
