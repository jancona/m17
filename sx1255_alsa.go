package m17

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"runtime"
	"time"

	"github.com/yobert/alsa"
)

const (
	// channelsSX1255 is stereo (I=left, Q=right)
	channelsSX1255 = 2

	// captureBufferSizeSX1255 is the ALSA buffer size in frames.
	// Power of two for BCM2835 compatibility.
	captureBufferSizeSX1255 = 16384

	// capturePeriodSizeSX1255 is the ALSA period size in frames.
	capturePeriodSizeSX1255 = 4096
)

// sx1255OpenCapture finds and opens an ALSA PCM capture device.
// deviceHint matches against the device Path (e.g., "/dev/snd/pcmC0D1c")
// or the device Title. If empty, the first recording PCM device is used.
func sx1255OpenCapture(deviceHint string) (*alsa.Device, error) {
	cards, err := alsa.OpenCards()
	if err != nil {
		return nil, fmt.Errorf("ALSA open cards: %w", err)
	}
	defer alsa.CloseCards(cards)

	var captureDevice *alsa.Device
	for _, card := range cards {
		devices, err := card.Devices()
		if err != nil {
			log.Printf("[DEBUG] ALSA: error listing devices on card %s: %v", card.Title, err)
			continue
		}
		for _, dev := range devices {
			if dev.Type == alsa.PCM && dev.Record {
				log.Printf("[DEBUG] ALSA: found capture device: %s (%s)", dev.Title, dev.Path)
				if deviceHint == "" || dev.Path == deviceHint || dev.Title == deviceHint {
					captureDevice = dev
					break
				}
			}
		}
		if captureDevice != nil {
			break
		}
	}

	if captureDevice == nil {
		return nil, fmt.Errorf("ALSA: no capture device found (hint: %q)", deviceHint)
	}

	err = captureDevice.Open()
	if err != nil {
		return nil, fmt.Errorf("ALSA open capture device %s: %w", captureDevice.Path, err)
	}

	_, err = captureDevice.NegotiateChannels(channelsSX1255)
	if err != nil {
		captureDevice.Close()
		return nil, fmt.Errorf("ALSA negotiate channels: %w", err)
	}

	// Skip NegotiateRate. The SX1255 is the I2S master and clocks at
	// 125 kSa/s. After Open() + NegotiateChannels, the driver already
	// constrains the rate interval to [125000, 125000]. Calling
	// NegotiateRate triggers a refine ioctl that fails due to
	// interdependent buffer/period constraints even though the rate
	// itself is valid. Letting Prepare() commit the hwparams directly
	// works because it uses SNDRV_PCM_HW_PARAMS (not HW_REFINE).

	// Use S32_LE to match the genericstereoaudiocodec overlay's 32-bit TDM slot width.
	_, err = captureDevice.NegotiateFormat(alsa.S32_LE)
	if err != nil {
		captureDevice.Close()
		return nil, fmt.Errorf("ALSA negotiate format S32_LE: %w", err)
	}

	_, err = captureDevice.NegotiateBufferSize(captureBufferSizeSX1255, 8192, 4096)
	if err != nil {
		log.Printf("[DEBUG] ALSA: buffer size negotiation failed: %v, using default", err)
	}

	_, err = captureDevice.NegotiatePeriodSize(capturePeriodSizeSX1255, 2048, 1024)
	if err != nil {
		log.Printf("[DEBUG] ALSA: period size negotiation failed: %v, using default", err)
	}

	err = captureDevice.Prepare()
	if err != nil {
		captureDevice.Close()
		return nil, fmt.Errorf("ALSA prepare: %w", err)
	}

	log.Printf("[INFO] ALSA capture device opened: %s, rate=%d (I2S master), channels=%d, format=S32_LE",
		captureDevice.Path, sampleRateSX1255, channelsSX1255)
	return captureDevice, nil
}

// captureLoop reads IQ samples from ALSA and sends them as complex128 to the channel.
// Stereo S32_LE: left=I, right=Q. The SX1255 data is MSB-aligned within 32-bit frames.
func (m *SX1255Modem) captureLoop(dev *alsa.Device, iqSamples chan<- complex128) {
	// Pin this goroutine to an OS thread for real-time audio performance
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	buf := dev.NewBufferDuration(20 * time.Millisecond)
	bytesPerFrame := 8 // 2 channels × 4 bytes per sample (S32_LE)

	consecutiveErrors := 0

	for {
		err := dev.Read(buf.Data)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors <= 3 {
				log.Printf("[ERROR] ALSA capture read error (%d): %v", consecutiveErrors, err)
			}
			// Try to recover from xrun
			if perr := dev.Prepare(); perr != nil {
				log.Printf("[ERROR] ALSA capture prepare recovery failed: %v", perr)
			}
			continue
		}
		if consecutiveErrors > 0 {
			log.Printf("[INFO] ALSA capture recovered after %d errors", consecutiveErrors)
		}
		consecutiveErrors = 0

		// Parse stereo int32 pairs into complex128
		numFrames := len(buf.Data) / bytesPerFrame

		for i := range numFrames {
			offset := i * bytesPerFrame
			iSample := int32(binary.LittleEndian.Uint32(buf.Data[offset : offset+4]))
			qSample := int32(binary.LittleEndian.Uint32(buf.Data[offset+4 : offset+8]))

			// Normalize int32 to float64 [-1.0, 1.0]
			iFloat := float64(iSample) / 2147483648.0
			qFloat := float64(qSample) / 2147483648.0

			select {
			case iqSamples <- complex(iFloat, qFloat):
			default:
				// Channel full — drop sample to prevent blocking the audio thread
			}
		}
	}
}

// openALSACapture opens the ALSA capture device and builds the RX DSP pipeline.
func (m *SX1255Modem) openALSACapture() error {
	dev, err := sx1255OpenCapture(m.alsaDev)
	if err != nil {
		return err
	}
	m.captClose = dev.Close // store close func for platform-agnostic cleanup

	m.rxSymbols, err = m.rxPipeline(dev)
	if err != nil {
		dev.Close()
		m.captClose = nil
		return fmt.Errorf("SX1255 RX pipeline: %w", err)
	}

	return nil
}

// sx1255OpenPlayback finds and opens an ALSA PCM playback device.
// deviceHint matches against the device Path or Title. If empty, the first
// playback PCM device is used. The device is configured for S32_LE stereo
// at the I2S master rate (125 kSa/s).
func sx1255OpenPlayback(deviceHint string) (*alsa.Device, error) {
	cards, err := alsa.OpenCards()
	if err != nil {
		return nil, fmt.Errorf("ALSA open cards: %w", err)
	}
	defer alsa.CloseCards(cards)

	var playbackDevice *alsa.Device
	for _, card := range cards {
		devices, err := card.Devices()
		if err != nil {
			log.Printf("[DEBUG] ALSA: error listing devices on card %s: %v", card.Title, err)
			continue
		}
		for _, dev := range devices {
			if dev.Type == alsa.PCM && dev.Play {
				log.Printf("[DEBUG] ALSA: found playback device: %s (%s)", dev.Title, dev.Path)
				if deviceHint == "" || dev.Path == deviceHint || dev.Title == deviceHint {
					playbackDevice = dev
					break
				}
			}
		}
		if playbackDevice != nil {
			break
		}
	}

	if playbackDevice == nil {
		return nil, fmt.Errorf("ALSA: no playback device found (hint: %q)", deviceHint)
	}

	err = playbackDevice.Open()
	if err != nil {
		return nil, fmt.Errorf("ALSA open playback device %s: %w", playbackDevice.Path, err)
	}

	_, err = playbackDevice.NegotiateChannels(channelsSX1255)
	if err != nil {
		playbackDevice.Close()
		return nil, fmt.Errorf("ALSA negotiate channels: %w", err)
	}

	// Skip NegotiateRate — see comment in sx1255OpenCapture.
	// The SX1255 I2S master clock constrains the rate to 125 kSa/s.

	_, err = playbackDevice.NegotiateFormat(alsa.S32_LE)
	if err != nil {
		playbackDevice.Close()
		return nil, fmt.Errorf("ALSA negotiate format S32_LE: %w", err)
	}

	_, err = playbackDevice.NegotiateBufferSize(captureBufferSizeSX1255, 8192, 4096)
	if err != nil {
		log.Printf("[DEBUG] ALSA playback: buffer size negotiation failed: %v, using default", err)
	}

	_, err = playbackDevice.NegotiatePeriodSize(capturePeriodSizeSX1255, 2048, 1024)
	if err != nil {
		log.Printf("[DEBUG] ALSA playback: period size negotiation failed: %v, using default", err)
	}

	err = playbackDevice.Prepare()
	if err != nil {
		playbackDevice.Close()
		return nil, fmt.Errorf("ALSA playback prepare: %w", err)
	}

	log.Printf("[INFO] ALSA playback device opened: %s, rate=%d (I2S master), channels=%d, format=S32_LE",
		playbackDevice.Path, sampleRateSX1255, channelsSX1255)
	return playbackDevice, nil
}

// openALSAPlayback opens the ALSA playback device for TX.
func (m *SX1255Modem) openALSAPlayback() error {
	dev, err := sx1255OpenPlayback(m.alsaDev)
	if err != nil {
		return err
	}
	m.playDev = dev
	return nil
}

// sx1255WriteSymbols converts M17 symbols to IQ samples via the TX DSP
// pipeline and writes them to the ALSA playback device.
//
// Pipeline: symbols → RRC pulse shaping → resample 24k→125k → FM mod → IQ → ALSA
//
// The DSP state (pulse shaper, resampler, FM modulator) persists on the modem
// struct so that consecutive calls produce a continuous waveform.
func (m *SX1255Modem) sx1255WriteSymbols(symbols []Symbol) error {
	// 1. RRC pulse shaping: symbols → baseband at 24 kSa/s (5 sps)
	baseband24k := m.txRRC.Process(symbols)

	// Apply sqrt(sps) gain to match M17 TX deviation.
	// The rrcTaps5 filter has sqrt(5) gain baked into the taps, but pulse
	// shaping via zero-stuffing + convolution divides it out. We need to
	// restore it so symbol ±3 produces ±2400 Hz deviation.
	for i := range baseband24k {
		baseband24k[i] *= math.Sqrt(5)
	}

	// 2. Rational resample: 24 kSa/s → 125 kSa/s (ratio 125/24)
	baseband125k := m.txResampler.Process(baseband24k)

	// 3. FM modulate: baseband → complex IQ at 125 kSa/s
	// deviationHz=800: symbol +1 → +800 Hz, symbol +3 → +2400 Hz
	const deviationHz = 800.0
	iq := m.txFMMod.Modulate(baseband125k, deviationHz)

	// 4. Pack complex IQ → stereo S32_LE bytes and write to ALSA
	return m.sx1255WriteIQ(iq)
}

// sx1255WriteIQ packs complex128 IQ samples as stereo S32_LE and writes
// them to the ALSA playback device.
func (m *SX1255Modem) sx1255WriteIQ(iq []complex128) error {
	const bytesPerFrame = 8 // 2 channels × 4 bytes (S32_LE)
	buf := make([]byte, len(iq)*bytesPerFrame)

	for i, sample := range iq {
		// Scale float64 [-1.0, 1.0] → int32, with clamping
		iVal := real(sample)
		qVal := imag(sample)

		// Clamp to prevent int32 overflow
		if iVal > 1.0 {
			iVal = 1.0
		} else if iVal < -1.0 {
			iVal = -1.0
		}
		if qVal > 1.0 {
			qVal = 1.0
		} else if qVal < -1.0 {
			qVal = -1.0
		}

		iSample := int32(iVal * 2147483647.0)
		qSample := int32(qVal * 2147483647.0)

		offset := i * bytesPerFrame
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(iSample))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(qSample))
	}

	frames := len(iq)
	err := m.playDev.Write(buf, frames)
	if err != nil {
		log.Printf("[WARN] ALSA playback write error, recovering: %v", err)
		// Try to recover from underrun
		if perr := m.playDev.Prepare(); perr != nil {
			log.Printf("[ERROR] ALSA playback prepare recovery failed: %v", perr)
			return fmt.Errorf("ALSA playback prepare recovery: %w", perr)
		}
		err = m.playDev.Write(buf, frames)
		if err != nil {
			return fmt.Errorf("ALSA playback write after recovery: %w", err)
		}
	}
	return nil
}

// rxPipeline assembles the SX1255 RX DSP chain:
//
//	ALSA 125 kSa/s stereo IQ
//	→ DC removal
//	→ Polyphase decimating FIR (125k → 12.5k, channel filter)
//	→ FM demodulator (complex IQ → real instantaneous frequency)
//	→ Rational resampler (12.5k → 24k, ratio 48/25)
//	→ RRC matched filter (5 sps at 24 kSa/s → symbols)
func (m *SX1255Modem) rxPipeline(dev *alsa.Device) (chan float32, error) {
	iqSamples := make(chan complex128, sampleRateSX1255/2) // ~500ms buffer

	// Start ALSA capture goroutine
	go m.captureLoop(dev, iqSamples)

	// DC removal (exponential moving average high-pass)
	dcr := NewComplexDCRemoval(iqSamples, 0.9999)

	// Polyphase decimating FIR: 125 kSa/s → 12.5 kSa/s
	// Channel filter: 12.5 kHz bandwidth at 125 kSa/s input
	channelTaps := designChannelFilter(12500, float64(sampleRateSX1255), 200)
	decimator := NewPolyphaseDecimator(dcr.Source(), channelTaps, 10)

	// FM demodulator: complex IQ → real instantaneous frequency (radians/sample)
	fmDemod := NewFMDemodulator(decimator.Source())

	// Rational resampler: 12.5 kSa/s → 24 kSa/s (ratio 48/25)
	resampler := NewRationalResampler(fmDemod.Source(), 48, 25)

	// No IIR pre-emphasis filter — that's CC1200-specific. The FM demodulator
	// output doesn't need the same compensation as the CC1200's baseband.

	// RRC matched filter → symbols
	// Scaling coefficient tuned empirically against real hardware.
	// With IIR bypassed, symbol avg was 3.73 when target is 3.0 → scale by 3.0/3.73.
	const rxScalingCoeffSX1255 = 1.54
	s2s := NewSampleToSymbol(resampler.Source(), rrcTaps5, rxScalingCoeffSX1255)

	return s2s.Source(), nil
}
