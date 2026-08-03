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

// SX1255 control register (0x00) bits
const (
	// modeRefEnableSX1255 powers the reference oscillator and PLL bias. Nothing
	// else in the chip clocks without it, including the I2S master output.
	modeRefEnableSX1255    = 0x01
	modeRXEnableSX1255     = 0x02
	modeTXEnableSX1255     = 0x04 // TX frontend
	modeDriverEnableSX1255 = 0x08 // PA driver

	// modeMaskSX1255 covers the bits above; the rest of 0x00 is reserved.
	modeMaskSX1255 = modeRefEnableSX1255 | modeRXEnableSX1255 | modeTXEnableSX1255 | modeDriverEnableSX1255
)

// pllLockTimeoutSX1255 bounds how long bring-up waits for the RX PLL to lock.
const pllLockTimeoutSX1255 = 500 * time.Millisecond

// txPLLLockTimeoutSX1255 bounds the TX PLL check in startTX. It is much shorter
// than the RX budget because it sits directly in front of the preamble on every
// transmission — the check is a diagnostic, so it must not delay a TX that is
// otherwise fine. One SPI readback is ~10 ms, and the PLL locks well inside this.
const txPLLLockTimeoutSX1255 = 100 * time.Millisecond

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
	spi      *spiDevice
	resetPin gpioLine

	// captDev is swapped out by captureLoop when it has to reopen a wedged
	// device, so it is guarded by captMutex. Use captureDevice()/setCaptureDevice().
	captMutex sync.Mutex
	captDev   *alsa.Device

	playDev   *alsa.Device
	rxSymbols chan float32
	frameSink func(typ uint16, softBits []SoftBit)

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
	rawIQPath    string // if set, captureLoop dumps raw IQ here for off-line analysis
	rawIQSeconds int
	lnaGain      uint8
	pgaGain      uint8
	dacGain      int8
	mixerGain    float32
}

// NewSX1255Modem creates and initializes an SX1255 modem from INI configuration.
func NewSX1255Modem(
	rxFrequency uint32,
	txFrequency uint32,
	frequencyCorr int16,
	modemCfg *ini.Section,
) (*SX1255Modem, error) {
	spiPath := modemCfg.Key("SPIDevice").MustString("/dev/spidev0.0")
	gpioChip := modemCfg.Key("GPIOChip").MustString("gpiochip0")
	resetPin := modemCfg.Key("ResetPin").MustInt(22)
	alsaCapture := modemCfg.Key("ALSACaptureDevice").MustString("")
	alsaPlayback := modemCfg.Key("ALSAPlaybackDevice").MustString("")
	lnaGain := modemCfg.Key("LNAGain").MustUint(0)
	pgaGain := modemCfg.Key("PGAGain").MustUint(0)
	dacGain := modemCfg.Key("DACGain").MustInt(0)
	mixerGain := modemCfg.Key("MixerGain").MustFloat64(-12)
	// Diagnostics: dump raw IQ for off-line analysis. ALSA hw devices are
	// exclusive, so arecord cannot read the SX1255 while the gateway holds it.
	rawIQPath := modemCfg.Key("RawIQCapture").MustString("")
	rawIQSeconds := modemCfg.Key("RawIQSeconds").MustInt(20)

	m := &SX1255Modem{
		txState:      txIdleSX1255,
		rxSymbols:    make(chan float32, 1),
		spiPath:      spiPath,
		gpioChip:     gpioChip,
		resetPinN:    resetPin,
		alsaCapture:  alsaCapture,
		alsaPlayback: alsaPlayback,
		rawIQPath:    rawIQPath,
		rawIQSeconds: rawIQSeconds,
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

	// Reset and configure the chip, then confirm the RX PLL actually locked.
	// The SX1255 is the I2S bus master, so an unlocked RX PLL means no BCLK/WS
	// and an ALSA capture device that can never deliver a sample — better to
	// fail loudly here than to run for hours looking healthy but deaf.
	err = m.configure(rxFrequency+uint32(frequencyCorr), txFrequency+uint32(frequencyCorr))
	if err != nil {
		m.spi.close()
		return nil, err
	}
	if !m.sx1255WaitPLLLock(false, pllLockTimeoutSX1255) {
		log.Print("[WARN] SX1255 RX PLL not locked, retrying reset and configuration")
		err = m.configure(rxFrequency+uint32(frequencyCorr), txFrequency+uint32(frequencyCorr))
		if err != nil {
			m.spi.close()
			return nil, err
		}
		if !m.sx1255WaitPLLLock(false, pllLockTimeoutSX1255) {
			m.spi.close()
			return nil, errors.New("SX1255 RX PLL failed to lock: chip is producing no I2S clock — check the reference oscillator and supply")
		}
	}
	// TX PLL lock is deliberately not checked here: the TX path is disabled
	// until startTX() enables it, so its PLL cannot be locked yet. It is
	// checked in startTX() instead.

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

// configure resets the SX1255 and applies the full register configuration:
// frequencies, I2S sample rate, gains, and RX on / TX off. It is safe to call
// repeatedly — bring-up retries it if the RX PLL does not lock the first time.
func (m *SX1255Modem) configure(rxFreq, txFreq uint32) error {
	if err := m.sx1255Init(); err != nil {
		return fmt.Errorf("SX1255 init: %w", err)
	}
	if err := m.sx1255SetRXFreq(rxFreq); err != nil {
		return fmt.Errorf("SX1255 set RX freq: %w", err)
	}
	if err := m.sx1255SetTXFreq(txFreq); err != nil {
		return fmt.Errorf("SX1255 set TX freq: %w", err)
	}
	// Set sample rate (125 kSa/s)
	if err := m.sx1255SetRate(); err != nil {
		return fmt.Errorf("SX1255 set rate: %w", err)
	}

	// Gain failures are not fatal — the radio still works, just not at the
	// configured levels.
	if err := m.sx1255SetLNAGain(m.lnaGain); err != nil {
		log.Printf("[ERROR] SX1255 set LNA gain: %v", err)
	}
	if err := m.sx1255SetPGAGain(m.pgaGain); err != nil {
		log.Printf("[ERROR] SX1255 set PGA gain: %v", err)
	}
	if err := m.sx1255SetDACGain(m.dacGain); err != nil {
		log.Printf("[ERROR] SX1255 set DAC gain: %v", err)
	}
	if err := m.sx1255SetMixerGain(m.mixerGain); err != nil {
		log.Printf("[ERROR] SX1255 set mixer gain: %v", err)
	}

	if err := m.sx1255EnableRX(true); err != nil {
		return fmt.Errorf("SX1255 enable RX: %w", err)
	}
	// TX PA is disabled by default — it is enabled on demand by startTX()
	// when TransmitPacket or TransmitVoiceStream is called.
	if err := m.sx1255EnableTX(false); err != nil {
		log.Printf("[WARN] SX1255 disable TX: %v", err)
	}

	ctrlReg, _ := m.spi.readReg(regControlSX1255)
	log.Printf("[DEBUG] SX1255 control reg 0x00 = 0x%02X (ref_enable=%v, rx_enable=%v, tx_enable=%v, driver_enable=%v)",
		ctrlReg, ctrlReg&modeRefEnableSX1255 != 0, ctrlReg&modeRXEnableSX1255 != 0,
		ctrlReg&modeTXEnableSX1255 != 0, ctrlReg&modeDriverEnableSX1255 != 0)
	return nil
}

// sx1255WaitPLLLock polls the PLL status register until the requested PLL
// reports lock or timeout expires. readReg already paces itself at ~10 ms per
// access, so this polls at roughly that rate.
func (m *SX1255Modem) sx1255WaitPLLLock(tx bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		txLocked, rxLocked := m.sx1255GetPLLStatus()
		if (tx && txLocked) || (!tx && rxLocked) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
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
//
// This deliberately leaves the chip quiescent — no frequencies, sample rate,
// gains, or RX/TX enable. It backs the "-reset" flag, which resets the hardware
// and exits, so leaving the chip in its post-reset default state is the point.
// Anything that wants a working radio afterwards should call configure().
func (m *SX1255Modem) Reset() error {
	log.Print("[DEBUG] SX1255 modem Reset()")
	// sx1255Init begins with a hardware reset, so calling sx1255Reset first
	// would reset the chip twice.
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
	if dev := m.captureDevice(); dev != nil {
		dev.Close()
	}
	if m.playDev != nil {
		m.playDev.Close()
	}
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

	// Return the ALSA playback device to PREPARED for a fresh transmission.
	// The stream state may be stale if time has passed since the last TX (or
	// since device open), and it may have xrun, so Drop() first — see
	// recoverALSA. If even that fails the device is wedged; reopen it.
	if err := recoverALSA(m.playDev); err != nil {
		log.Printf("[WARN] SX1255 ALSA playback recovery failed: %v, reopening device", err)
		// Hold on to the old handle until the reopen succeeds: openALSAPlayback
		// leaves m.playDev untouched on failure, so a failed reopen must not
		// leave a nil device behind for stopTX/Close to dereference.
		old := m.playDev
		if err := m.openALSAPlayback(); err != nil {
			return false, fmt.Errorf("SX1255 ALSA playback reopen: %w", err)
		}
		old.Close()
	}

	err := m.sx1255EnableTX(true)
	if err != nil {
		return false, fmt.Errorf("SX1255 enable TX: %w", err)
	}

	// Now that the TX path is powered its PLL should lock. If it does not, the
	// chip is not transmitting on frequency — and a supply that browns out when
	// the PA switches on can also stall the I2S master clock, which shows up
	// downstream as playback underruns.
	if !m.sx1255WaitPLLLock(true, txPLLLockTimeoutSX1255) {
		log.Print("[WARN] SX1255 TX PLL not locked after enabling TX — check the supply and antenna load")
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
	if m.playDev != nil {
		if err := m.playDev.Drain(); err != nil {
			log.Printf("[WARN] SX1255 ALSA playback drain: %v", err)
		}
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
