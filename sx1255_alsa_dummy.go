//go:build !linux

package m17

import "fmt"

// openALSACapture is a no-op on non-Linux platforms.
func (m *SX1255Modem) openALSACapture() error {
	return fmt.Errorf("SX1255 ALSA capture not supported on this platform")
}
