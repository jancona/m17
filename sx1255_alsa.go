//go:build linux

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
	// sx1255SampleRate is the I2S sample rate from the SX1255 chip
	sx1255SampleRate = 125000

	// sx1255Channels is stereo (I=left, Q=right)
	sx1255Channels = 2

	// sx1255CaptureBufferSize is the ALSA buffer size in frames.
	// Power of two for BCM2835 compatibility.
	sx1255CaptureBufferSize = 16384

	// sx1255CapturePeriodSize is the ALSA period size in frames.
	sx1255CapturePeriodSize = 4096
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

	_, err = captureDevice.NegotiateChannels(sx1255Channels)
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

	_, err = captureDevice.NegotiateBufferSize(sx1255CaptureBufferSize, 8192, 4096)
	if err != nil {
		log.Printf("[DEBUG] ALSA: buffer size negotiation failed: %v, using default", err)
	}

	_, err = captureDevice.NegotiatePeriodSize(sx1255CapturePeriodSize, 2048, 1024)
	if err != nil {
		log.Printf("[DEBUG] ALSA: period size negotiation failed: %v, using default", err)
	}

	err = captureDevice.Prepare()
	if err != nil {
		captureDevice.Close()
		return nil, fmt.Errorf("ALSA prepare: %w", err)
	}

	log.Printf("[INFO] ALSA capture device opened: %s, rate=%d (I2S master), channels=%d, format=S32_LE",
		captureDevice.Path, sx1255SampleRate, sx1255Channels)
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

	// Diagnostic counters
	var readCount uint64
	var totalFrames uint64
	var droppedFrames uint64
	var peakI, peakQ float64
	diagTicker := time.NewTicker(5 * time.Second)
	defer diagTicker.Stop()

	for {
		// Check if it's time for a diagnostic report
		select {
		case <-diagTicker.C:
			log.Printf("[DEBUG] ALSA capture: reads=%d, frames=%d, dropped=%d, peakI=%.6f, peakQ=%.6f, chanLen=%d/%d",
				readCount, totalFrames, droppedFrames, peakI, peakQ, len(iqSamples), cap(iqSamples))
			peakI = 0
			peakQ = 0
			droppedFrames = 0
		default:
		}

		err := dev.Read(buf.Data)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors <= 3 {
				log.Printf("[ERROR] ALSA capture read error (%d): %v", consecutiveErrors, err)
			}
			// Try to recover from xrun
			dev.Prepare()
			continue
		}
		if consecutiveErrors > 0 {
			log.Printf("[INFO] ALSA capture recovered after %d errors", consecutiveErrors)
		}
		consecutiveErrors = 0
		readCount++

		// Parse stereo int32 pairs into complex128
		numFrames := len(buf.Data) / bytesPerFrame
		totalFrames += uint64(numFrames)

		// Log raw bytes of the first frame on the very first read
		if readCount == 1 && numFrames > 0 {
			log.Printf("[DEBUG] ALSA first read: %d frames, first 8 bytes: %02x",
				numFrames, buf.Data[:min(8, len(buf.Data))])
		}

		for i := range numFrames {
			offset := i * bytesPerFrame
			iSample := int32(binary.LittleEndian.Uint32(buf.Data[offset : offset+4]))
			qSample := int32(binary.LittleEndian.Uint32(buf.Data[offset+4 : offset+8]))

			// Normalize int32 to float64 [-1.0, 1.0]
			iFloat := float64(iSample) / 2147483648.0
			qFloat := float64(qSample) / 2147483648.0

			// Track peak amplitude for diagnostics
			if abs := math.Abs(iFloat); abs > peakI {
				peakI = abs
			}
			if abs := math.Abs(qFloat); abs > peakQ {
				peakQ = abs
			}

			select {
			case iqSamples <- complex(iFloat, qFloat):
			default:
				droppedFrames++
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

// monitorFloat64Chan logs peak amplitude stats from a float64 channel for diagnostics.
func monitorFloat64Chan(name string, in chan float64, interval time.Duration) chan float64 {
	out := make(chan float64, cap(in))
	go func() {
		var count uint64
		var peak, sum float64
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for s := range in {
			count++
			if abs := math.Abs(s); abs > peak {
				peak = abs
			}
			sum += math.Abs(s)
			select {
			case <-ticker.C:
				avg := float64(0)
				if count > 0 {
					avg = sum / float64(count)
				}
				log.Printf("[DEBUG] %s: count=%d, peak=%.6f, avg=%.6f", name, count, peak, avg)
				peak = 0
				sum = 0
				count = 0
			default:
			}
			out <- s
		}
		close(out)
	}()
	return out
}

// monitorFloat32Chan logs peak amplitude stats from a float32 channel for diagnostics.
func monitorFloat32Chan(name string, in chan float32, interval time.Duration) chan float32 {
	out := make(chan float32, cap(in))
	go func() {
		var count uint64
		var peak float64
		var sum float64
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for s := range in {
			count++
			if abs := math.Abs(float64(s)); abs > peak {
				peak = abs
			}
			sum += math.Abs(float64(s))
			select {
			case <-ticker.C:
				avg := float64(0)
				if count > 0 {
					avg = sum / float64(count)
				}
				log.Printf("[DEBUG] %s: count=%d, peak=%.6f, avg=%.6f", name, count, peak, avg)
				peak = 0
				sum = 0
				count = 0
			default:
			}
			out <- s
		}
		close(out)
	}()
	return out
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
	const diagInterval = 5 * time.Second

	iqSamples := make(chan complex128, sx1255SampleRate/2) // ~500ms buffer

	// Start ALSA capture goroutine
	go m.captureLoop(dev, iqSamples)

	// DC removal (exponential moving average high-pass)
	dcr := NewComplexDCRemoval(iqSamples, 0.9999)

	// Polyphase decimating FIR: 125 kSa/s → 12.5 kSa/s
	// Channel filter: 12.5 kHz bandwidth at 125 kSa/s input
	// (no monitor on this 125k/s path — the extra goroutine hop costs throughput)
	channelTaps := designChannelFilter(12500, float64(sx1255SampleRate), 200)
	decimator := NewPolyphaseDecimator(dcr.Source(), channelTaps, 10)

	// FM demodulator: complex IQ → real instantaneous frequency (radians/sample)
	fmDemod := NewFMDemodulator(decimator.Source())

	// Monitor post-FM-demod
	postFMDemod := monitorFloat64Chan("post-FMdemod", fmDemod.Source(), diagInterval)

	// Rational resampler: 12.5 kSa/s → 24 kSa/s (ratio 48/25)
	resampler := NewRationalResampler(postFMDemod, 48, 25)

	// No IIR pre-emphasis filter — that's CC1200-specific. The FM demodulator
	// output doesn't need the same compensation as the CC1200's baseband.

	// RRC matched filter → symbols
	// Scaling coefficient tuned empirically against real hardware.
	// With IIR bypassed, symbol avg was 3.73 when target is 3.0 → scale by 3.0/3.73.
	const sx1255RXScalingCoeff = 1.54
	s2s := NewSampleToSymbol(resampler.Source(), rrcTaps5, sx1255RXScalingCoeff)

	// Monitor final symbols (at 24k samples/sec, 5 sps)
	postSymbols := monitorFloat32Chan("symbols", s2s.Source(), diagInterval)

	return postSymbols, nil
}
