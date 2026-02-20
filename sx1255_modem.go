package m17

import (
	"fmt"
	"log"
	"sync"
	"time"

	"gopkg.in/ini.v1"
)

// SX1255 register addresses
const (
	sx1255RegControl    = 0x00
	sx1255RegRXFreqMSB  = 0x01
	sx1255RegRXFreqMid  = 0x02
	sx1255RegRXFreqLSB  = 0x03
	sx1255RegTXFreqMSB  = 0x04
	sx1255RegTXFreqMid  = 0x05
	sx1255RegTXFreqLSB  = 0x06
	sx1255RegVersion    = 0x07
	sx1255RegTXDACGain  = 0x08
	sx1255RegTXFilterBW = 0x0A
	sx1255RegTXDACTaps  = 0x0B
	sx1255RegRXLNAPGA   = 0x0C
	sx1255RegRXBWFilter = 0x0D
	sx1255RegRXPLLBW    = 0x0E
	sx1255RegLoopback   = 0x10
	sx1255RegPLLStatus  = 0x11
	sx1255RegI2SRate0   = 0x12
	sx1255RegI2SRate1   = 0x13
)

// SX1255 clock frequency (32 MHz crystal)
const sx1255ClkFreq = 32.0e6

// Expected chip version
const sx1255ExpectedVersion = 0x11

// SX1255Modem implements the Modem interface for the SX1255 RF transceiver HAT.
// Unlike MMDVM and CC1200 modems which have on-board microcontrollers, the SX1255
// is a raw IQ analog front-end — all baseband DSP is performed in software.
type SX1255Modem struct {
	spi       *spiDevice
	resetPin  gpioLine
	captClose func() // closes the ALSA capture device (platform-specific)
	rxSymbols chan float32
	frameSink func(typ uint16, softBits []SoftBit)
	rxFreq    uint32
	txFreq    uint32

	// Configuration
	spiPath   string
	gpioChip  string
	resetPinN int
	alsaDev   string
	lnaGain   uint8
	pgaGain   uint8
	dacGain   int8
	mixerGain float32

	mutex sync.Mutex
}

// NewSX1255Modem creates and initializes an SX1255 modem from INI configuration.
func NewSX1255Modem(
	rxFrequency uint32,
	txFrequency uint32,
	modemCfg *ini.Section,
) (*SX1255Modem, error) {
	spiPath := modemCfg.Key("SPIDevice").MustString("/dev/spidev0.0")
	gpioChip := modemCfg.Key("GPIOChip").MustString("gpiochip0")
	resetPin := modemCfg.Key("ResetPin").MustInt(22)
	alsaDev := modemCfg.Key("ALSADevice").MustString("")
	lnaGain := modemCfg.Key("LNAGain").MustUint(24)
	pgaGain := modemCfg.Key("PGAGain").MustUint(12)
	dacGain := modemCfg.Key("DACGain").MustInt(0)
	mixerGain := modemCfg.Key("MixerGain").MustFloat64(-12)

	m := &SX1255Modem{
		rxSymbols: make(chan float32, 1),
		rxFreq:    rxFrequency,
		txFreq:    txFrequency,
		spiPath:   spiPath,
		gpioChip:  gpioChip,
		resetPinN: resetPin,
		alsaDev:   alsaDev,
		lnaGain:   uint8(lnaGain),
		pgaGain:   uint8(pgaGain),
		dacGain:   int8(dacGain),
		mixerGain: float32(mixerGain),
	}

	// Open SPI
	var err error
	m.spi, err = openSPI(spiPath)
	if err != nil {
		return nil, fmt.Errorf("SX1255 open SPI: %w", err)
	}

	// Setup GPIO
	err = m.gpioSetup(gpioChip, resetPin)
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 GPIO setup: %w", err)
	}

	// Initialize chip: reset, verify, configure
	err = m.sx1255Init()
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 init: %w", err)
	}

	// Set frequencies
	err = m.sx1255SetRXFreq(rxFrequency)
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 set RX freq: %w", err)
	}
	err = m.sx1255SetTXFreq(txFrequency)
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 set TX freq: %w", err)
	}

	// Set sample rate (125 kSa/s)
	err = m.sx1255SetRate()
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 set rate: %w", err)
	}

	// Configure gains
	err = m.sx1255SetLNAGain(m.lnaGain)
	if err != nil {
		log.Printf("[ERROR] SX1255 set LNA gain: %v", err)
	}
	err = m.sx1255SetPGAGain(m.pgaGain)
	if err != nil {
		log.Printf("[ERROR] SX1255 set PGA gain: %v", err)
	}
	err = m.sx1255SetDACGain(m.dacGain)
	if err != nil {
		log.Printf("[ERROR] SX1255 set DAC gain: %v", err)
	}
	err = m.sx1255SetMixerGain(m.mixerGain)
	if err != nil {
		log.Printf("[ERROR] SX1255 set mixer gain: %v", err)
	}

	// Enable RX path
	err = m.sx1255EnableRX(true)
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 enable RX: %w", err)
	}

	// TX is disabled by default — TX is not yet implemented and enabling it
	// causes the SX1255 to transmit on the TX frequency.
	err = m.sx1255EnableTX(false)
	if err != nil {
		log.Printf("[WARN] SX1255 disable TX: %v", err)
	}

	// Readback control register to verify state
	ctrlReg, _ := m.spi.readReg(sx1255RegControl)
	log.Printf("[DEBUG] SX1255 control reg 0x00 = 0x%02X (ref_enable=%v, rx_enable=%v, tx_enable=%v, driver_enable=%v)",
		ctrlReg, ctrlReg&0x01 != 0, ctrlReg&0x02 != 0, ctrlReg&0x04 != 0, ctrlReg&0x08 != 0)

	// Verify PLL lock
	txLock, rxLock := m.sx1255GetPLLStatus()
	if !rxLock {
		log.Print("[ERROR] SX1255 RX PLL not locked")
	}
	if !txLock {
		log.Print("[ERROR] SX1255 TX PLL not locked")
	}
	log.Printf("[DEBUG] SX1255 PLL status: TX locked=%v, RX locked=%v", txLock, rxLock)

	// Wait for the I2S clock to stabilize before opening ALSA.
	// The SX1255 is the I2S bus master; the BCM2835 I2S peripheral (slave)
	// needs a stable BCLK/WS before it can synchronize. Without this delay,
	// the first ALSA reads fail with EIO because the peripheral hasn't
	// locked onto the external clock yet.
	log.Print("[DEBUG] Waiting 500ms for I2S clock to stabilize...")
	time.Sleep(500 * time.Millisecond)

	// Open ALSA capture device and build RX DSP pipeline
	err = m.openALSACapture()
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 ALSA capture: %w", err)
	}

	log.Printf("[INFO] SX1255 modem initialized: RX=%d Hz, TX=%d Hz, ALSA=%s", rxFrequency, txFrequency, alsaDev)
	return m, nil
}

// StartDecoding registers the frame sink callback and starts the symbol processing goroutine.
func (m *SX1255Modem) StartDecoding(sink func(typ uint16, softBits []SoftBit)) {
	m.frameSink = sink
	go processSymbolStream(m.rxSymbols, m.frameSink, 5)
}

// Start enables the RX path.
func (m *SX1255Modem) Start() error {
	return m.sx1255EnableRX(true)
}

// Reset performs a hardware reset of the SX1255 chip and re-initializes it.
func (m *SX1255Modem) Reset() error {
	log.Print("[DEBUG] SX1255 modem Reset()")
	err := m.sx1255Reset()
	if err != nil {
		return fmt.Errorf("SX1255 reset: %w", err)
	}
	// Re-init after reset
	return m.sx1255Init()
}

// Close shuts down the SX1255 modem, releasing all resources.
func (m *SX1255Modem) Close() error {
	log.Print("[DEBUG] SX1255 modem Close()")
	m.sx1255EnableRX(false)
	m.sx1255EnableTX(false)
	if m.captClose != nil {
		m.captClose()
	}
	if m.resetPin != nil {
		m.resetPin.Close()
	}
	if m.spi != nil {
		m.spi.close()
	}
	return nil
}

// TransmitPacket sends a packet over RF. Not yet implemented for SX1255.
func (m *SX1255Modem) TransmitPacket(p Packet) error {
	return fmt.Errorf("SX1255 TX not yet implemented")
}

// TransmitVoiceStream sends a voice stream frame over RF. Not yet implemented for SX1255.
func (m *SX1255Modem) TransmitVoiceStream(sd StreamDatagram) error {
	return fmt.Errorf("SX1255 TX not yet implemented")
}
