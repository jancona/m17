package m17

import (
	"math"
	"math/cmplx"
)

// --- Complex DC Removal ---

// ComplexDCRemoval removes DC offset from a complex IQ stream using an
// exponential moving average high-pass filter applied independently to I and Q.
type ComplexDCRemoval struct {
	Transform[complex128, complex128]
	avgI  float64
	avgQ  float64
	alpha float64 // smoothing factor, e.g. 0.9999
}

func NewComplexDCRemoval(sink chan complex128, alpha float64) ComplexDCRemoval {
	ret := ComplexDCRemoval{
		alpha: alpha,
	}
	ret.Transform = NewTransform(sink, ret.process, 0)
	return ret
}

func (f *ComplexDCRemoval) process(sample complex128) []complex128 {
	i := real(sample)
	q := imag(sample)
	f.avgI = f.alpha*f.avgI + (1-f.alpha)*i
	f.avgQ = f.alpha*f.avgQ + (1-f.alpha)*q
	return []complex128{complex(i-f.avgI, q-f.avgQ)}
}

// --- FM Demodulator ---

// FMDemodulator extracts instantaneous frequency from a complex IQ stream
// using the conjugate-multiply-and-arg method.
// Output is in radians per sample.
type FMDemodulator struct {
	Transform[complex128, float64]
	prev complex128
}

func NewFMDemodulator(sink chan complex128) FMDemodulator {
	ret := FMDemodulator{
		prev: complex(1, 0), // initialize to avoid zero-divide on first sample
	}
	ret.Transform = NewTransform(sink, ret.process, 0)
	return ret
}

func (f *FMDemodulator) process(sample complex128) []float64 {
	// Conjugate multiply: sample * conj(prev)
	// The angle of the result is the instantaneous phase difference
	product := sample * cmplx.Conj(f.prev)
	f.prev = sample
	return []float64{cmplx.Phase(product)}
}

// --- Polyphase Decimating FIR Filter ---

// PolyphaseDecimator performs efficient decimation with FIR filtering using
// a polyphase decomposition. The input is complex IQ; the output is complex IQ
// at a lower sample rate. The FIR acts as a lowpass anti-alias/channel filter.
//
// The full-rate FIR H(z) is decomposed into M subfilters E_0..E_{M-1}.
// For each block of M input samples, ONE output is produced:
//
//	y[n] = Σ_{p=0}^{M-1} Σ_{k} E_p[k] · x[nM − p − kM]
//
// All M input samples contribute to the output through their respective
// phase subfilters, providing proper anti-alias filtering before decimation.
type PolyphaseDecimator struct {
	Transform[complex128, complex128]
	decimFactor int
	phases      [][]float64  // phases[phase][tap] — M subfilters
	buffer      []complex128 // circular delay line, length = numTaps (full prototype length)
	bufIdx      int          // write position in circular buffer
	count       int          // input sample counter
}

// NewPolyphaseDecimator creates a polyphase decimating FIR filter.
// taps is the full prototype lowpass FIR filter (length should be a multiple of decimFactor).
// decimFactor is the decimation ratio.
func NewPolyphaseDecimator(sink chan complex128, taps []float64, decimFactor int) PolyphaseDecimator {
	// Pad taps to a multiple of decimFactor
	padded := taps
	if len(padded)%decimFactor != 0 {
		pad := decimFactor - len(padded)%decimFactor
		padded = make([]float64, len(taps)+pad)
		copy(padded, taps)
	}

	tapsPerPhase := len(padded) / decimFactor
	phases := make([][]float64, decimFactor)
	for p := range decimFactor {
		phases[p] = make([]float64, tapsPerPhase)
		for t := range tapsPerPhase {
			phases[p][t] = padded[t*decimFactor+p]
		}
	}

	ret := PolyphaseDecimator{
		decimFactor: decimFactor,
		phases:      phases,
		// Full-length buffer holds all recent input samples so that every
		// sample contributes to the output through the correct phase subfilter.
		buffer: make([]complex128, len(padded)),
	}
	ret.Transform = NewTransform(sink, ret.process, 0)
	return ret
}

func (f *PolyphaseDecimator) process(sample complex128) []complex128 {
	// Store EVERY input sample into the full-length circular buffer.
	f.buffer[f.bufIdx] = sample
	f.bufIdx = (f.bufIdx + 1) % len(f.buffer)
	f.count++

	// Produce output only on every M-th input sample (end of each block).
	if f.count%f.decimFactor != 0 {
		return nil
	}

	// Compute the polyphase output.
	// The most recent sample (just stored) corresponds to phase M-1
	// (the last sample in the decimation block). Walking backwards:
	//   buffer[bufIdx-1] = newest = phase M-1
	//   buffer[bufIdx-2] = phase M-2
	//   ...
	//   buffer[bufIdx-M] = phase 0 (oldest in this block)
	//
	// For each phase p, we apply subfilter E_p to samples spaced M apart.
	// Phase p's samples in the buffer are at positions: bufIdx-M+p, bufIdx-2M+p, ...
	var acc complex128
	M := f.decimFactor
	tapsPerPhase := len(f.phases[0])
	for p := range M {
		// Starting position for this phase: the sample that arrived at
		// position p within the most recent block (0 = oldest, M-1 = newest).
		// In the circular buffer, that's at bufIdx - M + p.
		startIdx := f.bufIdx - M + p
		if startIdx < 0 {
			startIdx += len(f.buffer)
		}
		for t := range tapsPerPhase {
			// Walk backwards by M samples for each tap
			idx := startIdx - t*M
			for idx < 0 {
				idx += len(f.buffer)
			}
			acc += f.buffer[idx] * complex(f.phases[p][t], 0)
		}
	}

	return []complex128{acc}
}

// --- Rational Resampler ---

// RationalResampler converts between sample rates using a polyphase FIR filter.
// Output rate = input rate * interpFactor / decimFactor.
// The prototype lowpass filter runs at interpFactor × the input rate.
type RationalResampler struct {
	Transform[float64, float64]
	interpFactor int
	decimFactor  int
	phases       [][]float64 // polyphase decomposition: phases[phase][tap]
	buffer       []float64   // circular delay line
	bufIdx       int
	outPhase     int // current output phase counter
}

// NewRationalResampler creates a rational resampler.
// interpFactor/decimFactor is the rate change ratio.
// For 12500 → 24000 Hz, use interp=48, decim=25.
func NewRationalResampler(sink chan float64, interpFactor, decimFactor int) RationalResampler {
	// Design the prototype lowpass filter
	// Cutoff at the lower of the two Nyquist rates, normalized to the upsampled rate
	// cutoff = min(1/interp, 1/decim) in normalized frequency
	// Cutoff at the input Nyquist, normalized to the upsampled rate's Nyquist.
	// For L=48, M=25: cutoff = 1/max(L,M) = 1/48 of the upsampled Nyquist.
	cutoffNorm := 1.0 / float64(max(interpFactor, decimFactor))
	// Use enough taps per phase for a clean filter. More taps = sharper rolloff.
	tapsPerPhase := 24
	taps := designLowpassFIR(interpFactor*tapsPerPhase, cutoffNorm, interpFactor)

	// Polyphase decomposition
	tapsPerPhase = len(taps) / interpFactor
	phases := make([][]float64, interpFactor)
	for p := range interpFactor {
		phases[p] = make([]float64, tapsPerPhase)
		for t := range tapsPerPhase {
			phases[p][t] = taps[t*interpFactor+p]
		}
	}

	ret := RationalResampler{
		interpFactor: interpFactor,
		decimFactor:  decimFactor,
		phases:       phases,
		buffer:       make([]float64, tapsPerPhase),
	}
	ret.Transform = NewTransform(sink, ret.process, 0)
	return ret
}

func (r *RationalResampler) process(sample float64) []float64 {
	// Insert new input sample into circular buffer
	r.buffer[r.bufIdx] = sample
	r.bufIdx = (r.bufIdx + 1) % len(r.buffer)

	// Polyphase rational resampling:
	// Output samples are produced at times t_out = k / (Fs_in * L)
	// where L = interpFactor, and we skip every M = decimFactor output phases.
	// outPhase tracks our position in the polyphase filter bank (0..interpFactor-1).
	// For each input sample, produce all outputs whose phase index < interpFactor,
	// then subtract interpFactor to signal we need the next input.
	var outputs []float64
	for r.outPhase < r.interpFactor {
		phase := r.outPhase
		var acc float64
		idx := r.bufIdx
		for t := range len(r.buffer) {
			idx--
			if idx < 0 {
				idx = len(r.buffer) - 1
			}
			acc += r.buffer[idx] * r.phases[phase][t]
		}
		outputs = append(outputs, acc)
		r.outPhase += r.decimFactor
	}
	r.outPhase -= r.interpFactor

	return outputs
}

// --- FIR Filter Design ---

// designLowpassFIR designs a lowpass FIR filter using a windowed sinc method.
// numTaps: number of filter taps (will be rounded up to a multiple of interpFactor)
// cutoffNorm: normalized cutoff frequency (0 to 1, where 1 = Fs/2 of the upsampled rate)
// interpFactor: interpolation factor (used for gain normalization)
func designLowpassFIR(numTaps int, cutoffNorm float64, interpFactor int) []float64 {
	// Round up to multiple of interpFactor
	if numTaps%interpFactor != 0 {
		numTaps += interpFactor - numTaps%interpFactor
	}

	taps := make([]float64, numTaps)
	center := float64(numTaps-1) / 2.0
	// cutoffNorm is 0..1 where 1 = Fs/2 (Nyquist).
	// Normalized angular cutoff: wc = cutoffNorm * pi.
	wc := cutoffNorm * math.Pi

	for i := range numTaps {
		n := float64(i) - center
		// Windowed sinc: h[n] = sin(wc * n) / (pi * n)
		var h float64
		if math.Abs(n) < 1e-10 {
			h = wc / math.Pi // limit of sin(wc*n)/(pi*n) as n→0
		} else {
			h = math.Sin(wc*n) / (math.Pi * n)
		}
		// Blackman window
		w := 0.42 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(numTaps-1)) +
			0.08*math.Cos(4*math.Pi*float64(i)/float64(numTaps-1))
		taps[i] = h * w * float64(interpFactor) // scale by interpFactor for unity gain
	}

	return taps
}

// --- Max-Abs Symbol Decimator ---

// MaxAbsDecimator reduces the sample rate by picking the sample with the
// largest absolute value from each group of N input samples. This performs
// a simple form of symbol clock recovery: the RRC-filtered symbol peak has
// the largest magnitude, so picking max-abs approximates optimal timing.
type MaxAbsDecimator struct {
	Transform[float32, float32]
	factor int
	buf    []float32
	count  int
}

func NewMaxAbsDecimator(sink chan float32, factor int) MaxAbsDecimator {
	ret := MaxAbsDecimator{
		factor: factor,
		buf:    make([]float32, factor),
	}
	ret.Transform = NewTransform(sink, ret.process, 0)
	return ret
}

func (d *MaxAbsDecimator) process(sample float32) []float32 {
	d.buf[d.count] = sample
	d.count++
	if d.count < d.factor {
		return nil
	}
	d.count = 0

	// Pick the sample with the largest absolute value
	best := d.buf[0]
	bestAbs := math.Abs(float64(best))
	for _, s := range d.buf[1:] {
		if a := math.Abs(float64(s)); a > bestAbs {
			best = s
			bestAbs = a
		}
	}
	return []float32{best}
}

// designChannelFilter designs a lowpass FIR suitable for extracting a
// single radio channel from a wideband IQ stream before decimation.
// channelBWHz: desired channel bandwidth in Hz (e.g., 12500 for M17)
// sampleRate: input sample rate in Hz (e.g., 125000)
// numTaps: number of filter taps
func designChannelFilter(channelBWHz, sampleRate float64, numTaps int) []float64 {
	// Cutoff at half the channel bandwidth, normalized to Nyquist (Fs/2).
	// For a 12.5 kHz channel at 125 kSa/s: cutoff = 6250 / 62500 = 0.1
	cutoffNorm := channelBWHz / sampleRate // channelBW/2 divided by Fs/2 = channelBW/Fs
	return designLowpassFIR(numTaps, cutoffNorm, 1)
}
