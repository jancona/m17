package m17

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestFMDemodulator_ConstantFrequency(t *testing.T) {
	// Generate a complex tone at a known frequency and verify the demodulator
	// recovers it. A tone at frequency f has phase increment 2*pi*f/Fs per sample.
	sampleRate := 125000.0
	toneFreq := 2400.0 // Hz (M17 outer deviation)
	numSamples := 1000
	phasePerSample := 2 * math.Pi * toneFreq / sampleRate

	sink := make(chan complex128, numSamples)
	demod := NewFMDemodulator(sink)

	// Feed a constant-frequency complex tone
	phase := 0.0
	for range numSamples {
		sink <- cmplx.Rect(1.0, phase)
		phase += phasePerSample
	}
	close(sink)

	// Read outputs and check they converge to the expected phase increment
	var outputs []float64
	for v := range demod.Source() {
		outputs = append(outputs, v)
	}

	if len(outputs) != numSamples {
		t.Fatalf("expected %d outputs, got %d", numSamples, len(outputs))
	}

	// Skip first sample (transient from initial state) and check the rest
	for i := 5; i < len(outputs); i++ {
		diff := math.Abs(outputs[i] - phasePerSample)
		if diff > 1e-6 {
			t.Errorf("sample %d: expected phase increment %.6f, got %.6f (diff %.6f)",
				i, phasePerSample, outputs[i], diff)
		}
	}
}

func TestFMDemodulator_4FSK(t *testing.T) {
	// Simulate M17 4FSK at 125 kSa/s: symbols at ±2.4 kHz and ±0.8 kHz deviation
	sampleRate := 125000.0
	samplesPerSymbol := 26 // ~125000/4800 ≈ 26
	symbols := []float64{2400, -2400, 800, -800, 2400, 800, -800, -2400}

	sink := make(chan complex128, len(symbols)*samplesPerSymbol+10)
	demod := NewFMDemodulator(sink)

	phase := 0.0
	for _, freqDev := range symbols {
		phaseInc := 2 * math.Pi * freqDev / sampleRate
		for range samplesPerSymbol {
			sink <- cmplx.Rect(1.0, phase)
			phase += phaseInc
		}
	}
	close(sink)

	var outputs []float64
	for v := range demod.Source() {
		outputs = append(outputs, v)
	}

	// Check the middle of each symbol period
	for i, freqDev := range symbols {
		mid := i*samplesPerSymbol + samplesPerSymbol/2
		expectedPhaseInc := 2 * math.Pi * freqDev / sampleRate
		if mid >= len(outputs) {
			break
		}
		diff := math.Abs(outputs[mid] - expectedPhaseInc)
		if diff > 1e-4 {
			t.Errorf("symbol %d (dev=%.0f Hz): expected phase inc %.6f, got %.6f",
				i, freqDev, expectedPhaseInc, outputs[mid])
		}
	}
}

func TestComplexDCRemoval(t *testing.T) {
	// Input: constant DC + small AC signal. Verify DC is removed.
	numSamples := 10000
	dcI := 100.0
	dcQ := -50.0
	sink := make(chan complex128, numSamples)

	dcr := NewComplexDCRemoval(sink, 0.999)

	for i := range numSamples {
		// DC offset + small sinusoidal signal
		sig := math.Sin(2 * math.Pi * float64(i) / 100.0)
		sink <- complex(dcI+sig, dcQ+sig)
	}
	close(sink)

	var outputs []complex128
	for v := range dcr.Source() {
		outputs = append(outputs, v)
	}

	// After convergence, DC should be mostly removed
	// Check the last 100 samples
	for i := len(outputs) - 100; i < len(outputs); i++ {
		ri := real(outputs[i])
		rq := imag(outputs[i])
		// The remaining signal should be roughly the AC component (amplitude ~1)
		// DC offset should be < 1.0 after convergence
		if math.Abs(ri) > 5.0 {
			t.Errorf("sample %d: I channel DC not sufficiently removed, got %.2f", i, ri)
		}
		if math.Abs(rq) > 5.0 {
			t.Errorf("sample %d: Q channel DC not sufficiently removed, got %.2f", i, rq)
		}
	}
}

func TestRationalResampler_48_25(t *testing.T) {
	// Resample 12500 Hz → 24000 Hz (ratio 48/25)
	// Input: 1 second of a 1200 Hz sine wave at 12500 Sa/s
	// Output should be the same sine wave at 24000 Sa/s
	inputRate := 12500.0
	outputRate := 24000.0
	interpFactor := 48
	decimFactor := 25
	toneFreq := 1200.0
	duration := 0.1 // seconds
	numInputSamples := int(inputRate * duration)
	expectedOutputSamples := int(outputRate * duration)

	sink := make(chan float64, numInputSamples+10)
	rs := NewRationalResampler(sink, interpFactor, decimFactor)

	// Generate input
	for i := range numInputSamples {
		sink <- math.Sin(2 * math.Pi * toneFreq * float64(i) / inputRate)
	}
	close(sink)

	// Collect output
	var outputs []float64
	for v := range rs.Source() {
		outputs = append(outputs, v)
	}

	// Check output count is approximately correct (allow some tolerance for filter delay)
	countDiff := math.Abs(float64(len(outputs)-expectedOutputSamples)) / float64(expectedOutputSamples)
	if countDiff > 0.05 {
		t.Errorf("expected ~%d output samples, got %d (%.1f%% off)",
			expectedOutputSamples, len(outputs), countDiff*100)
	}

	// After filter settling, verify the output is a sine wave at the correct frequency.
	// Find zero crossings in the settled portion to estimate frequency.
	settleTime := len(outputs) / 4 // skip first quarter for filter settling
	var zeroCrossings int
	for i := settleTime + 1; i < len(outputs); i++ {
		if (outputs[i-1] >= 0 && outputs[i] < 0) || (outputs[i-1] < 0 && outputs[i] >= 0) {
			zeroCrossings++
		}
	}
	// Each full cycle has 2 zero crossings
	measuredCycles := float64(zeroCrossings) / 2.0
	measureDuration := float64(len(outputs)-settleTime) / outputRate
	measuredFreq := measuredCycles / measureDuration

	freqError := math.Abs(measuredFreq-toneFreq) / toneFreq
	if freqError > 0.05 {
		t.Errorf("measured frequency %.1f Hz, expected %.1f Hz (%.1f%% error)",
			measuredFreq, toneFreq, freqError*100)
	}
}

func TestDesignLowpassFIR(t *testing.T) {
	// Basic sanity checks on the FIR filter design
	numTaps := 480 // 10 taps per phase for 48-phase resampler
	cutoffNorm := 1.0 / 48.0
	interpFactor := 48

	taps := designLowpassFIR(numTaps, cutoffNorm, interpFactor)

	if len(taps) != numTaps {
		t.Errorf("expected %d taps, got %d", numTaps, len(taps))
	}

	// Filter should be symmetric
	for i := range len(taps) / 2 {
		j := len(taps) - 1 - i
		if math.Abs(taps[i]-taps[j]) > 1e-10 {
			t.Errorf("filter not symmetric: taps[%d]=%.6f != taps[%d]=%.6f", i, taps[i], j, taps[j])
		}
	}

	// Peak should be at center
	centerIdx := len(taps) / 2
	peakVal := taps[centerIdx]
	for i, v := range taps {
		if v > peakVal*1.01 {
			t.Errorf("tap %d (%.6f) exceeds center tap %d (%.6f)", i, v, centerIdx, peakVal)
		}
	}
}

func TestPolyphaseDecimator(t *testing.T) {
	// Decimate by 10: 125 kSa/s → 12.5 kSa/s
	// Input: constant-frequency complex tone within the passband
	inputRate := 125000.0
	decimFactor := 10
	outputRate := inputRate / float64(decimFactor)
	toneFreq := 1200.0 // well within 6.25 kHz passband
	numInputSamples := 10000

	// Design channel filter: 12.5 kHz channel at 125 kSa/s
	taps := designChannelFilter(12500, inputRate, 100)

	sink := make(chan complex128, numInputSamples)
	pd := NewPolyphaseDecimator(sink, taps, decimFactor)

	// Generate input tone
	for i := range numInputSamples {
		phase := 2 * math.Pi * toneFreq * float64(i) / inputRate
		sink <- cmplx.Rect(1.0, phase)
	}
	close(sink)

	var outputs []complex128
	for v := range pd.Source() {
		outputs = append(outputs, v)
	}

	// Should get approximately numInputSamples/decimFactor outputs
	expectedOutputs := numInputSamples / decimFactor
	if math.Abs(float64(len(outputs)-expectedOutputs)) > 1 {
		t.Errorf("expected ~%d outputs, got %d", expectedOutputs, len(outputs))
	}

	// After settling, the output should be a complex tone at the same frequency
	// but at the output sample rate. Check phase increments.
	expectedPhaseInc := 2 * math.Pi * toneFreq / outputRate
	settleIdx := len(outputs) / 4
	for i := settleIdx + 1; i < len(outputs); i++ {
		// Phase difference between consecutive samples
		diff := cmplx.Phase(outputs[i] * cmplx.Conj(outputs[i-1]))
		phaseErr := math.Abs(diff - expectedPhaseInc)
		if phaseErr > 0.1 {
			t.Errorf("output %d: phase increment %.4f, expected %.4f (err %.4f)",
				i, diff, expectedPhaseInc, phaseErr)
			break // don't spam
		}
	}
}
