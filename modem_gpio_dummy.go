//go:build !linux

package m17

func (m *CC1200Modem) gpioSetup(_, _ int) error {
	return nil
}
