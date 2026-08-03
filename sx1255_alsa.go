package m17

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strings"
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

	// captureReadFramesSX1255 is how many frames each capture read asks for:
	// 20 ms at the SX1255's true sample rate.
	captureReadFramesSX1255 = sampleRateSX1255 * 20 / 1000

	// rxScalingCoeffSX1255 scales the RRC matched-filter output so that symbols
	// land on the ±1/±3 grid. It matters more than it looks: syncDistance
	// compares symbols to ±3 in absolute terms against a fixed threshold, so a
	// constellation that is a factor k off the grid carries a sync-distance
	// floor of 12·|k−1| before any noise, against thresholds of 4.5 and 5.0.
	//
	// Tuned empirically against real hardware. Note the historical comment here
	// read "symbol avg was 3.73 when target is 3.0 → scale by 3.0/3.73", which
	// is 0.804, not this value — the derivation and the constant disagree, so
	// treat the number as measured-by-experiment rather than derived.
	rxScalingCoeffSX1255 = 1.54

	// basebandDCAvgCntSX1255 is the averaging window of the software AFC, in
	// samples at the post-decimation rate (12.5 kSa/s). 2000 samples = 160 ms,
	// about four M17 frames. See sx1255RXPipelineTuned for why this exists.
	//
	// Swept against the issue #6 capture (TestSX1255CaptureSweep): accepted
	// syncs rise from 13 with no correction to ~104 at 10 ms and ~124 at 1.6 s,
	// so the exact window matters little. The choice is a trade-off between
	// averaging over enough frames not to track the data (M17 is only roughly
	// DC-balanced over a frame) and converging early enough in an over to catch
	// the LSF. 160 ms converges in about four frames.
	basebandDCAvgCntSX1255 = 2000

	// captureBackoffMinSX1255 and captureBackoffMaxSX1255 bound the retry delay
	// after a failed capture recovery, so an unrecoverable device (for example a
	// stopped I2S master clock) does not spin or flood the log.
	captureBackoffMinSX1255 = 100 * time.Millisecond
	captureBackoffMaxSX1255 = 2 * time.Second

	// captureReopenAfterSX1255 is the number of consecutive failed recoveries
	// after which the capture device is closed and reopened from scratch.
	captureReopenAfterSX1255 = 5
)

// logALSADevice reports what a device actually negotiated.
//
// The rate is advisory. NegotiateRate is deliberately skipped (see
// sx1255OpenCapture), so the driver resolves the open interval itself: a kernel
// patched to accept 125000 pins it there, while a stock bcm2835-i2s settles on
// its minimum. Either way the SX1255 is the I2S master and clocks the bus at
// sampleRateSX1255, and every ALSA threshold is counted in frames rather than
// seconds, so a rate the driver merely believes changes nothing — hence
// "advisory" rather than a warning about a mismatch.
//
// Buffer and period size are worth logging because they set the underrun
// margin: playback starts after one period and xruns after two buffers.
func logALSADevice(kind string, dev *alsa.Device, bufSize, periodSize int) {
	bf := dev.BufferFormat()
	rateNote := "advisory"
	if bf.Rate != sampleRateSX1255 {
		rateNote = fmt.Sprintf("advisory, differs from the SX1255's %d Hz", sampleRateSX1255)
	}
	log.Printf("[INFO] ALSA %s device opened: %s, %d channels, %v, buffer=%d, period=%d frames; driver rate %d Hz (%s)",
		kind, dev.Path, bf.Channels, bf.SampleFormat, bufSize, periodSize, bf.Rate, rateNote)
}

// recoverALSA returns a device to the PREPARED state after an xrun.
//
// Device.Prepare() issues SNDRV_PCM_IOCTL_HW_PARAMS first, and the kernel only
// accepts that in the OPEN or SETUP state — from XRUN it fails with EBADFD
// ("file descriptor in bad state"). Drop() moves the stream to SETUP from any
// running or errored state, after which Prepare() is legal. Calling Prepare()
// alone can therefore never recover a stream that has actually xrun.
func recoverALSA(dev *alsa.Device) error {
	if err := dev.Drop(); err != nil {
		return fmt.Errorf("ALSA drop: %w", err)
	}
	if err := dev.Prepare(); err != nil {
		return fmt.Errorf("ALSA prepare: %w", err)
	}
	return nil
}

// sx1255OpenCapture finds and opens an ALSA PCM capture device.
// deviceHint matches against the device Path (e.g., "/dev/snd/pcmC0D1c")
// or the device Title. If empty, a device whose title contains "i2s" is
// preferred; falls back to first found.
func sx1255OpenCapture(deviceHint string) (*alsa.Device, error) {
	cards, err := alsa.OpenCards()
	if err != nil {
		return nil, fmt.Errorf("ALSA open cards: %w", err)
	}
	defer alsa.CloseCards(cards)

	var captureDevice *alsa.Device
	var firstFound *alsa.Device
	for _, card := range cards {
		devices, err := card.Devices()
		if err != nil {
			log.Printf("[DEBUG] ALSA: error listing devices on card %s: %v", card.Title, err)
			continue
		}
		for _, dev := range devices {
			if dev.Type == alsa.PCM && dev.Record {
				log.Printf("[DEBUG] ALSA: found capture device: %s (%s)", dev.Title, dev.Path)
				if deviceHint != "" {
					if dev.Path == deviceHint || dev.Title == deviceHint {
						captureDevice = dev
						break
					}
				} else {
					if firstFound == nil {
						firstFound = dev
					}
					if captureDevice == nil && strings.Contains(strings.ToLower(dev.Title), "i2s") {
						captureDevice = dev
					}
				}
			}
		}
		if deviceHint != "" && captureDevice != nil {
			break
		}
	}

	if captureDevice == nil {
		captureDevice = firstFound
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

	bufSize, err := captureDevice.NegotiateBufferSize(captureBufferSizeSX1255, 8192, 4096)
	if err != nil {
		log.Printf("[DEBUG] ALSA: buffer size negotiation failed: %v, using default", err)
		bufSize = 0
	}

	periodSize, err := captureDevice.NegotiatePeriodSize(capturePeriodSizeSX1255, 2048, 1024)
	if err != nil {
		log.Printf("[DEBUG] ALSA: period size negotiation failed: %v, using default", err)
		periodSize = 0
	}

	err = captureDevice.Prepare()
	if err != nil {
		captureDevice.Close()
		return nil, fmt.Errorf("ALSA prepare: %w", err)
	}

	logALSADevice("capture", captureDevice, bufSize, periodSize)
	return captureDevice, nil
}

// captureLoop reads IQ samples from ALSA and sends them as complex128 to the channel.
// Stereo S32_LE: left=I, right=Q. The SX1255 data is MSB-aligned within 32-bit frames.
func (m *SX1255Modem) captureLoop(dev *alsa.Device, iqSamples chan<- complex128) {
	// Pin this goroutine to an OS thread for real-time audio performance
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	bytesPerFrame := 8 // 2 channels × 4 bytes per sample (S32_LE)

	// Optional raw IQ capture, for off-line analysis with the tools in
	// sx1255_capture_test.go. ALSA hw devices are exclusive, so arecord cannot
	// read the SX1255 while the gateway holds it — this is the only way to get a
	// capture that reflects the register configuration the gateway actually
	// applied. Opened here so it stays local to this goroutine: nothing else
	// touches it, so there is no shared state to guard.
	//
	// Bounded by duration: at 125 kSa/s stereo S32_LE this writes 1 MB/s, which
	// would fill a Pi's card in short order if left running.
	var rawIQ *os.File
	rawIQLeft := m.rawIQSeconds * sampleRateSX1255 * bytesPerFrame
	if m.rawIQPath != "" {
		f, err := os.Create(m.rawIQPath)
		if err != nil {
			log.Printf("[ERROR] Cannot open raw IQ capture %s: %v", m.rawIQPath, err)
		} else {
			rawIQ = f
			log.Printf("[INFO] Writing %d s of raw IQ to %s (S32_LE stereo, %d Sa/s)",
				m.rawIQSeconds, m.rawIQPath, sampleRateSX1255)
		}
	}
	defer func() {
		if rawIQ != nil {
			rawIQ.Close()
		}
	}()
	// Deliberately not dev.NewBufferDuration(): that sizes from the rate the
	// driver negotiated, which is only 125000 on a kernel patched to accept it.
	// A stock bcm2835-i2s rejects 125000 and the kernel resolves the open
	// interval to its minimum (8000), which would make each read a 15th of the
	// audio we expect and multiply the syscall rate to match. Since the SX1255
	// is the I2S master the true rate is sampleRateSX1255 either way, so size
	// reads from that and ignore the driver's belief.
	readBuf := make([]byte, captureReadFramesSX1255*bytesPerFrame)

	consecutiveErrors := 0
	backoff := captureBackoffMinSX1255

	for {
		err := dev.Read(readBuf)
		if err != nil {
			consecutiveErrors++
			// Log the first few, then only occasionally: a dead I2S clock
			// produces one of these every read, indefinitely.
			if consecutiveErrors <= 3 || consecutiveErrors%50 == 0 {
				log.Printf("[ERROR] ALSA capture read error (%d): %v", consecutiveErrors, err)
			}
			if rerr := recoverALSA(dev); rerr != nil {
				if consecutiveErrors <= 3 || consecutiveErrors%50 == 0 {
					log.Printf("[ERROR] ALSA capture recovery failed (%d): %v", consecutiveErrors, rerr)
				}
				// Recovery is not getting us anywhere — the device itself may
				// be wedged. Close and reopen it from scratch.
				if consecutiveErrors%captureReopenAfterSX1255 == 0 {
					newDev, oerr := sx1255OpenCapture(m.alsaCapture)
					if oerr != nil {
						log.Printf("[ERROR] ALSA capture reopen failed: %v", oerr)
					} else {
						log.Printf("[WARN] ALSA capture device reopened after %d errors", consecutiveErrors)
						dev.Close()
						dev = newDev
						m.setCaptureDevice(newDev)
					}
				}
			}
			time.Sleep(backoff)
			backoff = min(backoff*2, captureBackoffMaxSX1255)
			continue
		}
		if consecutiveErrors > 0 {
			log.Printf("[INFO] ALSA capture recovered after %d errors", consecutiveErrors)
		}
		consecutiveErrors = 0
		backoff = captureBackoffMinSX1255

		if rawIQ != nil {
			n := min(len(readBuf), rawIQLeft)
			if _, werr := rawIQ.Write(readBuf[:n]); werr != nil {
				log.Printf("[ERROR] Raw IQ capture write failed: %v", werr)
				rawIQ.Close()
				rawIQ = nil
			} else if rawIQLeft -= n; rawIQLeft <= 0 {
				log.Printf("[INFO] Raw IQ capture complete: %s", m.rawIQPath)
				rawIQ.Close()
				rawIQ = nil
			}
		}

		// Parse stereo int32 pairs into complex128
		numFrames := len(readBuf) / bytesPerFrame

		for i := range numFrames {
			offset := i * bytesPerFrame
			iSample := int32(binary.LittleEndian.Uint32(readBuf[offset : offset+4]))
			qSample := int32(binary.LittleEndian.Uint32(readBuf[offset+4 : offset+8]))

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

// openALSACapture opens the ALSA capture device, starts the capture goroutine,
// and builds the RX DSP pipeline it feeds.
func (m *SX1255Modem) openALSACapture() error {
	dev, err := sx1255OpenCapture(m.alsaCapture)
	if err != nil {
		return err
	}
	m.setCaptureDevice(dev)

	iqSamples := make(chan complex128, sampleRateSX1255/2) // ~500ms buffer
	go m.captureLoop(dev, iqSamples)
	m.rxSymbols = sx1255RXPipeline(iqSamples)

	return nil
}

// setCaptureDevice records the live capture device. captureLoop may swap the
// device out on reopen while Close() reads it from another goroutine, so the
// handle is guarded.
func (m *SX1255Modem) setCaptureDevice(dev *alsa.Device) {
	m.captMutex.Lock()
	defer m.captMutex.Unlock()
	m.captDev = dev
}

// captureDevice returns the live capture device, or nil if it is not open.
func (m *SX1255Modem) captureDevice() *alsa.Device {
	m.captMutex.Lock()
	defer m.captMutex.Unlock()
	return m.captDev
}

// sx1255OpenPlayback finds and opens an ALSA PCM playback device.
// deviceHint matches against the device Path or Title. If empty, a device
// whose title contains "i2s" is preferred (to avoid selecting onboard audio
// such as bcm2835 Headphones on Pi 3/4); falls back to first found.
// The device is configured for S32_LE stereo at the I2S master rate (125 kSa/s).
func sx1255OpenPlayback(deviceHint string) (*alsa.Device, error) {
	cards, err := alsa.OpenCards()
	if err != nil {
		return nil, fmt.Errorf("ALSA open cards: %w", err)
	}
	defer alsa.CloseCards(cards)

	var playbackDevice *alsa.Device
	var firstFound *alsa.Device
	for _, card := range cards {
		devices, err := card.Devices()
		if err != nil {
			log.Printf("[DEBUG] ALSA: error listing devices on card %s: %v", card.Title, err)
			continue
		}
		for _, dev := range devices {
			if dev.Type == alsa.PCM && dev.Play {
				log.Printf("[DEBUG] ALSA: found playback device: %s (%s)", dev.Title, dev.Path)
				if deviceHint != "" {
					if dev.Path == deviceHint || dev.Title == deviceHint {
						playbackDevice = dev
						break
					}
				} else {
					if firstFound == nil {
						firstFound = dev
					}
					if playbackDevice == nil && strings.Contains(strings.ToLower(dev.Title), "i2s") {
						playbackDevice = dev
					}
				}
			}
		}
		if deviceHint != "" && playbackDevice != nil {
			break
		}
	}

	if playbackDevice == nil {
		playbackDevice = firstFound
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

	bufSize, err := playbackDevice.NegotiateBufferSize(captureBufferSizeSX1255, 8192, 4096)
	if err != nil {
		log.Printf("[DEBUG] ALSA playback: buffer size negotiation failed: %v, using default", err)
		bufSize = 0
	}

	periodSize, err := playbackDevice.NegotiatePeriodSize(capturePeriodSizeSX1255, 2048, 1024)
	if err != nil {
		log.Printf("[DEBUG] ALSA playback: period size negotiation failed: %v, using default", err)
		periodSize = 0
	}

	err = playbackDevice.Prepare()
	if err != nil {
		playbackDevice.Close()
		return nil, fmt.Errorf("ALSA playback prepare: %w", err)
	}

	logALSADevice("playback", playbackDevice, bufSize, periodSize)
	return playbackDevice, nil
}

// openALSAPlayback opens the ALSA playback device for TX.
func (m *SX1255Modem) openALSAPlayback() error {
	dev, err := sx1255OpenPlayback(m.alsaPlayback)
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
		if perr := recoverALSA(m.playDev); perr != nil {
			log.Printf("[ERROR] ALSA playback recovery failed: %v", perr)
			return fmt.Errorf("ALSA playback recovery: %w", perr)
		}
		err = m.playDev.Write(buf, frames)
		if err != nil {
			return fmt.Errorf("ALSA playback write after recovery: %w", err)
		}
	}
	return nil
}

// sx1255RXPipeline assembles the SX1255 RX DSP chain:
//
//	125 kSa/s complex IQ
//	→ DC removal
//	→ Polyphase decimating FIR (125k → 12.5k, channel filter)
//	→ FM demodulator (complex IQ → real instantaneous frequency)
//	→ Rational resampler (12.5k → 24k, ratio 48/25)
//	→ RRC matched filter (5 sps at 24 kSa/s → symbols)
//
// It takes the IQ source as a channel rather than an ALSA device so the chain
// can also be driven from a recorded capture — see sx1255_capture_test.go.
// Closing iqSamples shuts the whole chain down and closes the returned channel.
func sx1255RXPipeline(iqSamples chan complex128) chan float32 {
	return sx1255RXPipelineTuned(iqSamples, basebandDCAvgCntSX1255, rxScalingCoeffSX1255)
}

// sx1255RXPipelineTuned is sx1255RXPipeline with the two empirical parameters
// exposed, so they can be swept against a recorded capture.
func sx1255RXPipelineTuned(iqSamples chan complex128, basebandDCAvgCnt int, scalingCoeff float64) chan float32 {
	// DC removal (exponential moving average high-pass)
	dcr := NewComplexDCRemoval(iqSamples, 0.9999)

	// Polyphase decimating FIR: 125 kSa/s → 12.5 kSa/s
	// Channel filter: 12.5 kHz bandwidth at 125 kSa/s input
	channelTaps := designChannelFilter(12500, float64(sampleRateSX1255), 200)
	decimator := NewPolyphaseDecimator(dcr.Source(), channelTaps, 10)

	// FM demodulator: complex IQ → real instantaneous frequency (radians/sample)
	fmDemod := NewFMDemodulator(decimator.Source())

	// Baseband DC removal — software AFC.
	//
	// A receive/transmit carrier offset lands here as a constant offset on the
	// demodulated frequency. The complex DC removal above cannot touch it: that
	// one clears LO leakage in the IQ, whereas a frequency error is DC *after*
	// demodulation. The SX1255 has no microcontroller and so no hardware AFC
	// (on CC1200 and MMDVM this is a firmware feature), which left this path
	// with no offset correction at all.
	//
	// It matters because syncDistance compares symbols to ±3 in absolute terms:
	// a bias of b symbol units adds 4·|b| to every sync distance, against
	// thresholds of 4.5 and 5.0. A capture from issue #6 showed −0.87 units
	// (≈ −700 Hz, only ~1.6 ppm at 431 MHz — ordinary crystal tolerance), which
	// spent 3.48 of that 4.5 budget and left the receiver unable to sync.
	//
	// The averaging window is a compromise: long enough not to track the data
	// (M17 is only DC-balanced over a frame or so), short enough to converge
	// early in an over.
	// A count of 1 would make DCFilter a first-difference filter rather than a
	// no-op, so anything below 2 means "no correction at all".
	demodulated := fmDemod.Source()
	if basebandDCAvgCnt > 1 {
		basebandDC, err := NewDCFilter(demodulated, basebandDCAvgCnt)
		if err != nil {
			// Unreachable: NewDCFilter only rejects counts below 1.
			panic(fmt.Sprintf("SX1255 baseband DC filter: %v", err))
		}
		demodulated = basebandDC.Source()
	}

	// Rational resampler: 12.5 kSa/s → 24 kSa/s (ratio 48/25)
	resampler := NewRationalResampler(demodulated, 48, 25)

	// No IIR pre-emphasis filter — that's CC1200-specific. The FM demodulator
	// output doesn't need the same compensation as the CC1200's baseband.

	// RRC matched filter → symbols
	s2s := NewSampleToSymbol(resampler.Source(), rrcTaps5, scalingCoeff)

	return s2s.Source()
}
