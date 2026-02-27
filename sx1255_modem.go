package m17

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yobert/alsa"
	"gopkg.in/ini.v1"
)

// SX1255 TX state constants
const (
	txIdleSX1255   = iota
	txPacketSX1255 // TX PA enabled, transmitting
	txStreamSX1255
)

// txTimeoutSX1255 is the safety timeout — auto-disable TX PA if no data
// sent within this duration. Must be longer than the Drain + TX tail time.
const txTimeoutSX1255 = endTXWait + 2*FrameTime

// SX1255 register addresses
const (
	regControlSX1255    = 0x00
	regRXFreqMSBSX1255  = 0x01
	regRXFreqMidSX1255  = 0x02
	regRXFreqLSBSX1255  = 0x03
	regTXFreqMSBSX1255  = 0x04
	regTXFreqMidSX1255  = 0x05
	regTXFreqLSBSX1255  = 0x06
	regVersionSX1255    = 0x07
	regTXDACGainSX1255  = 0x08
	regTXFilterBWSX1255 = 0x0A
	regTXDACTapsSX1255  = 0x0B
	regRXLNAPGASX1255   = 0x0C
	regRXBWFilterSX1255 = 0x0D
	regRXPLLBWSX1255    = 0x0E
	regLoopbackSX1255   = 0x10
	regPLLStatusSX1255  = 0x11
	regI2SRate0SX1255   = 0x12
	regI2SRate1SX1255   = 0x13
)

// SX1255 clock frequency (32 MHz crystal)
const clkFreqSX1255 = 32.0e6

// sampleRateSX1255 is the I2S sample rate from the SX1255 chip (125 kSa/s).
const sampleRateSX1255 = 125000

// Expected chip version
const expectedVersionSX1255 = 0x11

// SX1255Modem implements the Modem interface for the SX1255 RF transceiver HAT.
// Unlike MMDVM and CC1200 modems which have on-board microcontrollers, the SX1255
// is a raw IQ analog front-end — all baseband DSP is performed in software.
//
// The SX1255 supports full-duplex operation: RX runs continuously even during TX.
type SX1255Modem struct {
	spi       *spiDevice
	resetPin  gpioLine
	captClose func() // closes the ALSA capture device (platform-specific)
	playDev   *alsa.Device
	rxSymbols chan float32
	frameSink func(typ uint16, softBits []SoftBit)
	rxFreq    uint32
	txFreq    uint32

	// TX state (full-duplex: RX never stops)
	txMutex sync.Mutex
	txCond  *sync.Cond
	txState int         // txIdleSX1255, txPacketSX1255 or txStreamSX1255  (protected by mutexs above)
	txTimer *time.Timer // safety timeout to disable TX PA

	// TX DSP state — persists across writeSymbols calls within a transmission
	txRRC       *TXPulseShaper
	txResampler *BatchResampler
	txFMMod     *BatchFMModulator

	// Configuration
	spiPath      string
	gpioChip     string
	resetPinN    int
	alsaCapture  string
	alsaPlayback string
	lnaGain      uint8
	pgaGain      uint8
	dacGain      int8
	mixerGain    float32
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
	alsaCapture := modemCfg.Key("ALSACaptureDevice").MustString("")
	alsaPlayback := modemCfg.Key("ALSAPlaybackDevice").MustString("")
	lnaGain := modemCfg.Key("LNAGain").MustUint(24)
	pgaGain := modemCfg.Key("PGAGain").MustUint(12)
	dacGain := modemCfg.Key("DACGain").MustInt(0)
	mixerGain := modemCfg.Key("MixerGain").MustFloat64(-12)

	m := &SX1255Modem{
		txState:      txIdleSX1255,
		rxSymbols:    make(chan float32, 1),
		rxFreq:       rxFrequency,
		txFreq:       txFrequency,
		spiPath:      spiPath,
		gpioChip:     gpioChip,
		resetPinN:    resetPin,
		alsaCapture:  alsaCapture,
		alsaPlayback: alsaPlayback,
		lnaGain:      uint8(lnaGain),
		pgaGain:      uint8(pgaGain),
		dacGain:      int8(dacGain),
		mixerGain:    float32(mixerGain),
	}
	m.txCond = sync.NewCond(&m.txMutex)

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

	// TX PA is disabled by default — it is enabled on demand by startTX()
	// when TransmitPacket or TransmitVoiceStream is called.
	err = m.sx1255EnableTX(false)
	if err != nil {
		log.Printf("[WARN] SX1255 disable TX: %v", err)
	}

	// Readback control register to verify state
	ctrlReg, _ := m.spi.readReg(regControlSX1255)
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

	// Open ALSA playback device for TX
	err = m.openALSAPlayback()
	if err != nil {
		m.spi.close()
		return nil, fmt.Errorf("SX1255 ALSA playback: %w", err)
	}

	// Initialize TX DSP pipeline state
	m.txRRC = NewTXPulseShaper(rrcTaps5, 5)
	m.txResampler = NewBatchResampler(125, 24) // 24 kSa/s → 125 kSa/s
	m.txFMMod = NewBatchFMModulator(float64(sampleRateSX1255))

	// TX safety timer: auto-disable TX PA on timeout
	m.txTimer = time.AfterFunc(txTimeoutSX1255, func() {
		log.Printf("[WARN] SX1255 TX timeout — auto-disabling TX PA")
		m.stopTX()
	})
	m.txTimer.Stop() // don't start until we actually TX

	log.Printf("[INFO] SX1255 modem initialized: RX=%d Hz, TX=%d Hz, ALSA Capture=%s, ALSA Playback=%s", rxFrequency, txFrequency, alsaCapture, alsaPlayback)
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
	m.stopTX()
	m.sx1255EnableRX(false)
	m.sx1255EnableTX(false)
	if m.txTimer != nil {
		m.txTimer.Stop()
	}
	if m.captClose != nil {
		m.captClose()
	}
	m.playDev.Close()
	if m.resetPin != nil {
		m.resetPin.Close()
	}
	if m.spi != nil {
		m.spi.close()
	}
	return nil
}

// startTX enables the SX1255 TX PA, resets DSP state, and starts the safety timer.
// Full-duplex: RX is NOT stopped.
func (m *SX1255Modem) startTX(txState int) (bool, error) {
	if txState == txIdleSX1255 {
		return false, errors.New("cannot start txIdleSX1255")
	}
	m.txMutex.Lock()
	defer m.txMutex.Unlock()
	if m.txState == txState {
		// Already in the proper state, so this is not the first frame of a stream
		return false, nil
	}
	for m.txState != txIdleSX1255 {
		m.txCond.Wait() // blocks until Broadcast()
	}
	log.Printf("[DEBUG] SX1255 startTX()")

	// Prepare the ALSA playback device for a fresh transmission.
	// The stream state may be stale if time has passed since the last TX
	// (or since device open). Prepare() resets the stream to PREPARED state
	// so the next Write() can succeed.
	// If Prepare() fails (EBADFD), try closing and reopening the device.
	if err := m.playDev.Prepare(); err != nil {
		log.Printf("[WARN] SX1255 ALSA playback prepare failed: %v, reopening device", err)
		m.playDev.Close()
		m.playDev = nil
		if err := m.openALSAPlayback(); err != nil {
			return false, fmt.Errorf("SX1255 ALSA playback reopen: %w", err)
		}
	}

	err := m.sx1255EnableTX(true)
	if err != nil {
		return false, fmt.Errorf("SX1255 enable TX: %w", err)
	}

	// Reset TX DSP state for clean waveform start
	m.txRRC.Reset()
	m.txResampler.Reset()
	m.txFMMod.Reset()

	m.txState = txState
	m.txTimer.Reset(txTimeoutSX1255)
	return true, nil
}

// stopTX drains the ALSA playback buffer, disables the SX1255 TX PA,
// and stops the safety timer. Drain() blocks until all buffered samples
// have been played out, then transitions the stream to SETUP state —
// ready for a clean Prepare() on the next transmission.
func (m *SX1255Modem) stopTX() {
	m.txMutex.Lock()
	defer m.txMutex.Unlock()
	if m.txState == txIdleSX1255 {
		return
	}
	m.txState = txIdleSX1255
	m.txCond.Broadcast() // wake all waiting start() calls
	log.Print("[DEBUG] SX1255 stopTX()")

	t := time.Now()
	// Drain waits for all buffered samples to play out, then the stream
	// transitions to SETUP state — ready for a clean Prepare() next time.
	if err := m.playDev.Drain(); err != nil {
		log.Printf("[WARN] SX1255 ALSA playback drain: %v", err)
	}
	time.Sleep(time.Until(t.Add(endTXWait)))
	if err := m.sx1255EnableTX(false); err != nil {
		log.Printf("[WARN] SX1255 disable TX: %v", err)
	}
	if m.txTimer != nil {
		m.txTimer.Stop()
	}
}

// TransmitPacket sends a packet over RF.
// Full-duplex: RX continues running during TX.
func (m *SX1255Modem) TransmitPacket(p Packet) error {
	log.Printf("[DEBUG] SX1255 TransmitPacket: %v", p)
	_, err := m.startTX(txPacketSX1255)
	defer m.stopTX()
	if err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond) // TX PA settle time

	// Preamble
	syms := AppendPreamble(nil, lsfPreamble)
	err = m.sx1255WriteSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send preamble: %w", err)
	}

	// LSF
	syms, err = generateLSFSymbols(p.LSF)
	if err != nil {
		return fmt.Errorf("failed to generate LSF symbols: %w", err)
	}
	err = m.sx1255WriteSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send LSF: %w", err)
	}

	// Packet chunks
	chunkCnt := 0
	packetData := p.PayloadBytes()
	for bytesLeft := len(packetData); bytesLeft > 0; bytesLeft -= 25 {
		syms = AppendSyncwordSymbols(nil, PacketSync)
		chunk := make([]byte, 25+1)
		if bytesLeft > 25 {
			copy(chunk, packetData[chunkCnt*25:chunkCnt*25+25])
			chunk[25] = byte(chunkCnt << 2)
		} else {
			copy(chunk, packetData[chunkCnt*25:chunkCnt*25+bytesLeft])
			if bytesLeft%25 == 0 {
				chunk[25] = (1 << 7) | ((25) << 2)
			} else {
				chunk[25] = uint8((1 << 7) | ((bytesLeft % 25) << 2))
			}
		}
		b, err := ConvolutionalEncode(chunk, PacketPuncturePattern, PacketModeFinalBit)
		if err != nil {
			return fmt.Errorf("unable to encode packet: %w", err)
		}
		encodedBits := NewPayloadBits(b)
		rfBits := InterleaveBits(encodedBits)
		rfBits = RandomizeBits(rfBits)
		syms = AppendBits(syms, rfBits)
		err = m.sx1255WriteSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send packet chunk: %w", err)
		}
		chunkCnt++
	}

	// EOT
	syms = AppendEOT(nil)
	err = m.sx1255WriteSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send EOT: %w", err)
	}

	log.Printf("[DEBUG] SX1255 Finished TransmitPacket")
	return nil
}

// TransmitVoiceStream sends a voice stream frame over RF.
// Full-duplex: RX continues running during TX.
func (m *SX1255Modem) TransmitVoiceStream(sd StreamDatagram) error {
	firstFrame, err := m.startTX(txStreamSX1255)
	if err != nil {
		return err
	}
	var syms []Symbol

	if firstFrame {
		// First frame: enable TX, send preamble + LSF
		log.Printf("[DEBUG] SX1255 Sending LSF for stream %x, lsf: %v", sd.StreamID, sd.LSF)
		time.Sleep(10 * time.Millisecond) // TX PA settle time

		// Preamble
		syms = AppendPreamble(nil, lsfPreamble)
		err = m.sx1255WriteSymbols(syms)
		if err != nil {
			m.stopTX()
			return fmt.Errorf("failed to send preamble: %w", err)
		}

		// LSF
		syms, err = generateLSFSymbols(sd.LSF)
		if err != nil {
			m.stopTX()
			return fmt.Errorf("failed to generate LSF symbols: %w", err)
		}
		err = m.sx1255WriteSymbols(syms)
		if err != nil {
			m.stopTX()
			return fmt.Errorf("failed to send LSF: %w", err)
		}
	}

	// Stream frame
	syms, err = generateStreamSymbols(sd)
	if err != nil {
		return fmt.Errorf("failed to generate stream symbols: %w", err)
	}
	err = m.sx1255WriteSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send stream frame: %w", err)
	}

	// Reset safety timer
	m.txTimer.Reset(txTimeoutSX1255)

	if sd.LastFrame {
		// Send EOT
		log.Printf("[DEBUG] SX1255 Sending EOT for stream %04x, fn %04x", sd.StreamID, sd.FrameNumber)
		syms = AppendEOT(nil)
		err = m.sx1255WriteSymbols(syms)
		if err != nil {
			m.stopTX()
			return fmt.Errorf("failed to send EOT: %w", err)
		}
		log.Printf("[DEBUG] SX1255 Finished TransmitVoiceStream")
		m.stopTX()
	}

	return nil
}
