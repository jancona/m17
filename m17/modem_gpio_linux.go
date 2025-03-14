//go:build linux

package m17

import (
	"fmt"
	"time"

	"github.com/warthog618/go-gpiocdev"
)

func (m *CC1200Modem) gpioSetup(nRSTPin, paEnablePin, boot0Pin int) error {
	var err error
	// Do GPIO setup
	m.nRST, err = gpiocdev.RequestLine("gpiochip0", nRSTPin, gpiocdev.AsOutput(0))
	if err != nil {
		return fmt.Errorf("request nRST line: %w", err)
	}
	m.paEnable, err = gpiocdev.RequestLine("gpiochip0", paEnablePin, gpiocdev.AsOutput(0))
	if err != nil {
		return fmt.Errorf("request paEnable line: %w", err)
	}
	m.boot0, err = gpiocdev.RequestLine("gpiochip0", boot0Pin, gpiocdev.AsOutput(0))
	if err != nil {
		return fmt.Errorf("request boot0 line: %w", err)
	}
	err = m.setNRSTGPIO(false)
	if err != nil {
		return fmt.Errorf("unset NRST: %w", err)
	}
	time.Sleep(50 * time.Millisecond)
	err = m.setNRSTGPIO(true)
	if err != nil {
		return fmt.Errorf("set NRST: %w", err)
	}
	time.Sleep(time.Second) // wait for hat to boot
	return nil
}
