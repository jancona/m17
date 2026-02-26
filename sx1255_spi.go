//go:build linux

package m17

import (
	"fmt"
	"log"
	"math"
	"time"
	"unsafe"

	"github.com/warthog618/go-gpiocdev"
	"golang.org/x/sys/unix"
)

// SPI constants for Linux spidev.
// ioctl numbers use _IOW('k', nr, size) encoding:
//
//	(1<<30) | (size<<16) | ('k'<<8) | nr
const (
	spiIOCWRMode        = 0x40016B01 // _IOW('k', 1, sizeof(__u8))
	spiIOCWRBitsPerWord = 0x40016B03 // _IOW('k', 3, sizeof(__u8))
	spiIOCWRMaxSpeedHz  = 0x40046B04 // _IOW('k', 4, sizeof(__u32))
	spiMode0            = 0
	spiDefaultSpeed     = 500000 // 500 kHz
	spiDefaultBPW       = 8
)

// spiIOCTransfer represents a single SPI transfer for ioctl
type spiIOCTransfer struct {
	txBuf       uint64
	rxBuf       uint64
	len         uint32
	speedHz     uint32
	delayUsecs  uint16
	bitsPerWord uint8
	csChange    uint8
	txNbits     uint8
	rxNbits     uint8
	wordDelay   uint8
	pad         uint8
}

// spiIOCMessageN returns the ioctl request number for SPI_IOC_MESSAGE(n).
// This encodes: _IOW('k', 0, n * sizeof(struct spi_ioc_transfer))
func spiIOCMessageN(n int) uintptr {
	size := uintptr(n) * unsafe.Sizeof(spiIOCTransfer{})
	// _IOW('k', 0, size): dir=01 (write), size, type='k'(0x6B), nr=0
	return uintptr(1)<<30 | size<<16 | uintptr('k')<<8 //| 0
}

// spiDevice wraps a Linux spidev file descriptor
type spiDevice struct {
	fd int
}

func openSPI(path string) (*spiDevice, error) {
	fd, err := unix.Open(path, unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open SPI device %s: %w", path, err)
	}

	s := &spiDevice{fd: fd}

	// Set SPI mode 0 (CPOL=0, CPHA=0)
	mode := uint8(spiMode0)
	err = s.ioctl(spiIOCWRMode, uintptr(unsafe.Pointer(&mode)))
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("set SPI mode: %w", err)
	}

	// Set bits per word
	bpw := uint8(spiDefaultBPW)
	err = s.ioctl(spiIOCWRBitsPerWord, uintptr(unsafe.Pointer(&bpw)))
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("set SPI bits per word: %w", err)
	}

	// Set max speed
	speed := uint32(spiDefaultSpeed)
	err = s.ioctl(spiIOCWRMaxSpeedHz, uintptr(unsafe.Pointer(&speed)))
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("set SPI speed: %w", err)
	}

	return s, nil
}

func (s *spiDevice) ioctl(request uintptr, arg uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(s.fd), request, arg)
	if errno != 0 {
		return fmt.Errorf("SPI ioctl 0x%x: %w", request, errno)
	}
	return nil
}

func (s *spiDevice) transfer(tx, rx []byte) error {
	xfer := spiIOCTransfer{
		txBuf:       uint64(uintptr(unsafe.Pointer(&tx[0]))),
		rxBuf:       uint64(uintptr(unsafe.Pointer(&rx[0]))),
		len:         uint32(len(tx)),
		speedHz:     spiDefaultSpeed,
		bitsPerWord: spiDefaultBPW,
	}
	return s.ioctl(spiIOCMessageN(1), uintptr(unsafe.Pointer(&xfer)))
}

func (s *spiDevice) writeReg(addr, val byte) error {
	tx := []byte{addr | 0x80, val}
	rx := make([]byte, 2)
	err := s.transfer(tx, rx)
	if err != nil {
		return fmt.Errorf("SPI write reg 0x%02X: %w", addr, err)
	}
	time.Sleep(10 * time.Millisecond) // match libsx1255 timing
	return nil
}

func (s *spiDevice) readReg(addr byte) (byte, error) {
	tx := []byte{addr & 0x7F, 0x00}
	rx := make([]byte, 2)
	err := s.transfer(tx, rx)
	if err != nil {
		return 0, fmt.Errorf("SPI read reg 0x%02X: %w", addr, err)
	}
	time.Sleep(10 * time.Millisecond) // match libsx1255 timing
	return rx[1], nil
}

func (s *spiDevice) close() error {
	return unix.Close(s.fd)
}

// --- SX1255 high-level control functions ---

func (m *SX1255Modem) sx1255Init() error {
	// Hardware reset
	err := m.sx1255Reset()
	if err != nil {
		return fmt.Errorf("SX1255 reset: %w", err)
	}

	// Write initialization registers (per libsx1255)
	// Reg 0x0D: RX bandwidth filter config
	err = m.spi.writeReg(regRXBWFilterSX1255, (0x01<<5)|(0x05<<2)|0x03)
	if err != nil {
		return fmt.Errorf("SX1255 init reg 0x0D: %w", err)
	}
	// Reg 0x0B: TX DAC taps
	err = m.spi.writeReg(regTXDACTapsSX1255, 5)
	if err != nil {
		return fmt.Errorf("SX1255 init reg 0x0B: %w", err)
	}

	// Verify chip version
	ver := m.sx1255GetChipVersion()
	if ver != expectedVersionSX1255 {
		return fmt.Errorf("SX1255 unexpected chip version: 0x%02X (expected 0x%02X)", ver, expectedVersionSX1255)
	}
	log.Printf("[DEBUG] SX1255 chip version: 0x%02X", ver)

	return nil
}

func (m *SX1255Modem) sx1255Reset() error {
	if m.resetPin == nil {
		return nil // no GPIO configured (e.g., emulation)
	}
	log.Print("[DEBUG] SX1255 hardware reset")
	err := m.resetPin.SetValue(1)
	if err != nil {
		return fmt.Errorf("set reset HIGH: %w", err)
	}
	time.Sleep(100 * time.Millisecond)
	err = m.resetPin.SetValue(0)
	if err != nil {
		return fmt.Errorf("set reset LOW: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	return nil
}

// sx1255FreqToReg converts a frequency in Hz to the 24-bit PLL register value.
// Formula: val = round(freq * 2^20 / f_xtal)
func sx1255FreqToReg(hz uint32) uint32 {
	return uint32(math.Round(float64(hz) * 1048576.0 / clkFreqSX1255))
}

func (m *SX1255Modem) sx1255SetRXFreq(hz uint32) error {
	val := sx1255FreqToReg(hz)
	log.Printf("[DEBUG] SX1255 set RX freq: %d Hz (reg val: 0x%06X)", hz, val)
	if err := m.spi.writeReg(regRXFreqMSBSX1255, byte(val>>16)); err != nil {
		return err
	}
	if err := m.spi.writeReg(regRXFreqMidSX1255, byte(val>>8)); err != nil {
		return err
	}
	return m.spi.writeReg(regRXFreqLSBSX1255, byte(val))
}

func (m *SX1255Modem) sx1255SetTXFreq(hz uint32) error {
	val := sx1255FreqToReg(hz)
	log.Printf("[DEBUG] SX1255 set TX freq: %d Hz (reg val: 0x%06X)", hz, val)
	if err := m.spi.writeReg(regTXFreqMSBSX1255, byte(val>>16)); err != nil {
		return err
	}
	if err := m.spi.writeReg(regTXFreqMidSX1255, byte(val>>8)); err != nil {
		return err
	}
	return m.spi.writeReg(regTXFreqLSBSX1255, byte(val))
}

func (m *SX1255Modem) sx1255SetRate() error {
	// Configure I2S for 125 kSa/s in Mode B2 (standard I2S with muxed I/Q).
	// Must write reg 0x13 before 0x12 (per libsx1255 init order).
	//
	// Reg 0x13 (DIG_BRIDGE): decimation/interpolation + truncation
	//   bit 7:   int_dec_mantisse = 0  → 1st set (mantissa = 8)
	//   bit 6:   int_dec_m        = 0  → m = 0
	//   bit 5-3: int_dec_n        = 5  → n = 5
	//   bit 2:   IISM_truncation  = 1  → MSB-aligned (LSB truncated)
	//   Decimation factor R = 8 × 3^0 × 2^5 = 256
	//   Sample rate = 32 MHz / 256 = 125 kSa/s
	err := m.spi.writeReg(regI2SRate1SX1255, 0x2C)
	if err != nil {
		return fmt.Errorf("SX1255 set rate reg 0x13: %w", err)
	}
	// Reg 0x12 (IISM): I2S mode + CLK_OUT divider
	//   bit 7:   iism_rx_disable  = 0  → RX I2S enabled
	//   bit 6:   iism_tx_disable  = 0  → TX I2S enabled
	//   bit 5-4: iism_mode        = 2  → Mode B2 (standard I2S)
	//   bit 3-0: iism_clk_div     = 2  → XTAL / 4 = 8 MHz CLK_OUT
	err = m.spi.writeReg(regI2SRate0SX1255, 0x22)
	if err != nil {
		return fmt.Errorf("SX1255 set rate reg 0x12: %w", err)
	}
	// Read back and verify
	reg12, err := m.spi.readReg(regI2SRate0SX1255)
	if err != nil {
		log.Printf("[WARN] SX1255 readback reg 0x12 failed: %v", err)
	}
	reg13, err := m.spi.readReg(regI2SRate1SX1255)
	if err != nil {
		log.Printf("[WARN] SX1255 readback reg 0x13 failed: %v", err)
	}
	log.Printf("[DEBUG] SX1255 I2S configured: Mode B2, 125 kSa/s, CLK_OUT=8 MHz (reg12=0x%02X, reg13=0x%02X)", reg12, reg13)

	// Check IISM status flag (bit 1 of reg 0x13): 1 = error, IISM off
	if reg13&0x02 != 0 {
		log.Print("[ERROR] SX1255 IISM status flag set — I2S interface error")
	}
	return nil
}

// sx1255SetLNAGain sets the RX LNA gain (0, 6, 12, 18, 24, 30, 36, 42, or 48 dB).
// Values are rounded down to the nearest valid step.
func (m *SX1255Modem) sx1255SetLNAGain(db uint8) error {
	// LNA gain is in reg 0x0C bits [7:5], 6 dB per step, 0-48 dB
	step := db / 6
	if step > 8 {
		step = 8
	}
	reg, err := m.spi.readReg(regRXLNAPGASX1255)
	if err != nil {
		return err
	}
	reg = (reg & 0x1F) | (step << 5)
	log.Printf("[DEBUG] SX1255 set LNA gain: %d dB (step %d)", step*6, step)
	return m.spi.writeReg(regRXLNAPGASX1255, reg)
}

// sx1255SetPGAGain sets the RX PGA gain (0-30 dB in 2 dB steps).
func (m *SX1255Modem) sx1255SetPGAGain(db uint8) error {
	// PGA gain is in reg 0x0C bits [4:1], 2 dB per step, 0-30 dB
	step := db / 2
	if step > 15 {
		step = 15
	}
	reg, err := m.spi.readReg(regRXLNAPGASX1255)
	if err != nil {
		return err
	}
	reg = (reg & 0xE1) | (step << 1)
	log.Printf("[DEBUG] SX1255 set PGA gain: %d dB (step %d)", step*2, step)
	return m.spi.writeReg(regRXLNAPGASX1255, reg)
}

// sx1255SetDACGain sets the TX DAC gain (0, -3, -6, or -9 dB).
func (m *SX1255Modem) sx1255SetDACGain(db int8) error {
	// DAC gain is in reg 0x08 bits [5:4]
	var code uint8
	switch {
	case db >= 0:
		code = 0
	case db >= -3:
		code = 1
	case db >= -6:
		code = 2
	default:
		code = 3
	}
	reg, err := m.spi.readReg(regTXDACGainSX1255)
	if err != nil {
		return err
	}
	reg = (reg & 0xCF) | (code << 4)
	log.Printf("[DEBUG] SX1255 set DAC gain: %d dB (code %d)", -int(code)*3, code)
	return m.spi.writeReg(regTXDACGainSX1255, reg)
}

// sx1255SetMixerGain sets the TX mixer gain (-37.5 to -7.5 dB in 2 dB steps).
func (m *SX1255Modem) sx1255SetMixerGain(db float32) error {
	// Mixer gain is in reg 0x08 bits [3:0]
	// Code 0 = -37.5 dB, code 15 = -7.5 dB, 2 dB per step
	step := int(math.Round(float64(db+37.5) / 2.0))
	if step < 0 {
		step = 0
	}
	if step > 15 {
		step = 15
	}
	reg, err := m.spi.readReg(regTXDACGainSX1255)
	if err != nil {
		return err
	}
	reg = (reg & 0xF0) | byte(step)
	log.Printf("[DEBUG] SX1255 set mixer gain: %.1f dB (step %d)", -37.5+float64(step)*2, step)
	return m.spi.writeReg(regTXDACGainSX1255, reg)
}

// sx1255SetRXPLLBW sets the RX PLL bandwidth (75, 150, 225, or 300 kHz).
func (m *SX1255Modem) sx1255SetRXPLLBW(bwKHz uint16) error {
	var code uint8
	switch {
	case bwKHz <= 75:
		code = 0
	case bwKHz <= 150:
		code = 1
	case bwKHz <= 225:
		code = 2
	default:
		code = 3
	}
	reg, err := m.spi.readReg(regRXPLLBWSX1255)
	if err != nil {
		return err
	}
	reg = (reg & 0xF9) | (code << 1)
	log.Printf("[DEBUG] SX1255 set RX PLL BW: %d kHz (code %d)", (uint16(code)+1)*75, code)
	return m.spi.writeReg(regRXPLLBWSX1255, reg)
}

// sx1255SetTXPLLBW sets the TX PLL bandwidth (75, 150, 225, or 300 kHz).
func (m *SX1255Modem) sx1255SetTXPLLBW(bwKHz uint16) error {
	var code uint8
	switch {
	case bwKHz <= 75:
		code = 0
	case bwKHz <= 150:
		code = 1
	case bwKHz <= 225:
		code = 2
	default:
		code = 3
	}
	reg, err := m.spi.readReg(regTXFilterBWSX1255)
	if err != nil {
		return err
	}
	reg = (reg & 0x9F) | (code << 5)
	log.Printf("[DEBUG] SX1255 set TX PLL BW: %d kHz (code %d)", (uint16(code)+1)*75, code)
	return m.spi.writeReg(regTXFilterBWSX1255, reg)
}

func (m *SX1255Modem) sx1255EnableRX(enable bool) error {
	reg, err := m.spi.readReg(regControlSX1255)
	if err != nil {
		return err
	}
	if enable {
		reg |= 0x02 // bit 1
	} else {
		reg &= ^byte(0x02)
	}
	log.Printf("[DEBUG] SX1255 enable RX: %v", enable)
	return m.spi.writeReg(regControlSX1255, reg)
}

func (m *SX1255Modem) sx1255EnableTX(enable bool) error {
	reg, err := m.spi.readReg(regControlSX1255)
	if err != nil {
		return err
	}
	if enable {
		reg |= 0x0C // bits 2 and 3 (TX frontend + PA)
	} else {
		reg &= ^byte(0x0C)
	}
	log.Printf("[DEBUG] SX1255 enable TX: %v", enable)
	return m.spi.writeReg(regControlSX1255, reg)
}

func (m *SX1255Modem) sx1255EnableRFLoopback(enable bool) error {
	reg, err := m.spi.readReg(regLoopbackSX1255)
	if err != nil {
		return err
	}
	if enable {
		reg |= 0x04 // bit 2
	} else {
		reg &= ^byte(0x04)
	}
	log.Printf("[DEBUG] SX1255 enable RF loopback: %v", enable)
	return m.spi.writeReg(regLoopbackSX1255, reg)
}

func (m *SX1255Modem) sx1255GetPLLStatus() (txLocked, rxLocked bool) {
	reg, err := m.spi.readReg(regPLLStatusSX1255)
	if err != nil {
		log.Printf("[ERROR] SX1255 read PLL status: %v", err)
		return false, false
	}
	txLocked = reg&0x01 != 0
	rxLocked = reg&0x02 != 0
	return
}

func (m *SX1255Modem) sx1255GetChipVersion() byte {
	ver, err := m.spi.readReg(regVersionSX1255)
	if err != nil {
		log.Printf("[ERROR] SX1255 read chip version: %v", err)
		return 0
	}
	return ver
}

// gpioSetup configures the GPIO reset pin for the SX1255 HAT.
func (m *SX1255Modem) gpioSetup(gpioChip string, resetPin int) error {
	var err error
	log.Printf("[DEBUG] SX1255 GPIO setup: chip=%s, resetPin=%d", gpioChip, resetPin)
	m.resetPin, err = gpiocdev.RequestLine(gpioChip, resetPin, gpiocdev.AsOutput(0))
	if err != nil {
		return fmt.Errorf("request SX1255 reset line: %w", err)
	}
	return nil
}
