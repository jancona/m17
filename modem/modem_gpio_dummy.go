//go:build !linux

package modem

func (m *CC1200) gpioSetup(_, _ int) error {
	return nil
}
