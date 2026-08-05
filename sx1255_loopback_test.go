package m17

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
)

// TestSX1255Loopback feeds a synthetic, spec-perfect M17 signal through the
// SX1255 receive chain and checks where the symbols land.
//
// The transmitter is this repo's own TX DSP at the nominal ±2400 Hz deviation
// (deviationHz=800 × symbol 3), so the input is by construction compliant: no
// carrier offset, no noise, no over-deviation. That makes it a calibration
// reference for rxScalingCoeffSX1255 — with a correct coefficient the recovered
// symbols must sit on the ±1/±3 grid, and any residual error is ours, not a
// transmitter's.
//
// It also tells us how to read measurements taken from real captures: sampling
// instantaneous frequency at symbol centres overshoots nominal deviation for
// RRC-shaped 4FSK, and this quantifies that bias.
func TestSX1255Loopback(t *testing.T) {
	// --- transmit: symbols -> RRC -> resample 24k->125k -> FM -> IQ ---
	lsf, err := NewLSF("@ALL", "N0CALL", LSFTypeStream, LSFDataTypeVoice, 0)
	if err != nil {
		t.Fatalf("NewLSF: %v", err)
	}
	syms := AppendPreamble(nil, lsfPreamble)
	lsfSyms, err := generateLSFSymbols(&lsf)
	if err != nil {
		t.Fatalf("generateLSFSymbols: %v", err)
	}
	syms = append(syms, lsfSyms...)
	for fn := range uint16(40) {
		sd := NewStreamDatagram(0x1234, fn, &lsf, make([]byte, 16))
		s, err := generateStreamSymbols(sd)
		if err != nil {
			t.Fatalf("generateStreamSymbols: %v", err)
		}
		syms = append(syms, s...)
	}
	t.Logf("synthesised %d symbols (%.2f s)", len(syms), float64(len(syms))/4800)

	// Same chain and constants as sx1255WriteSymbols.
	shaper := NewTXPulseShaper(rrcTaps5, 5)
	resampler := NewBatchResampler(125, 24)
	fm := NewBatchFMModulator(float64(sampleRateSX1255))
	baseband := shaper.Process(syms)
	for i := range baseband {
		baseband[i] *= math.Sqrt(5)
	}
	// Is the transmitter itself calibrated? At symbol centres the shaped
	// baseband should equal the symbol value, so that ±3 becomes ±2400 Hz via
	// deviationHz=800. Fit the gain against the symbols we actually sent,
	// searching for the filter's group delay. This separates a TX scaling error
	// from an RX one — the loopback alone only measures their product.
	bestD, bestG, bestErr := 0, 1.0, math.MaxFloat64
	for d := 0; d < 60; d++ {
		var num, den float64
		for k, s := range syms {
			i := k*5 + d
			if i >= len(baseband) {
				break
			}
			num += baseband[i] * float64(s)
			den += float64(s) * float64(s)
		}
		if den == 0 {
			continue
		}
		g := num / den
		var sse float64
		n := 0
		for k, s := range syms {
			i := k*5 + d
			if i >= len(baseband) {
				break
			}
			e := baseband[i] - g*float64(s)
			sse += e * e
			n++
		}
		if n > 0 && sse/float64(n) < bestErr {
			bestD, bestG, bestErr = d, g, sse/float64(n)
		}
	}
	t.Logf("TX baseband at symbol centres (delay %d): gain %.4f vs symbol value, residual RMS %.4f",
		bestD, bestG, math.Sqrt(bestErr))
	t.Logf("=> transmitted deviation for symbol ±3: %.0f Hz (M17 nominal 2400)", bestG*3*800)

	iq := fm.Modulate(resampler.Process(baseband), 800.0)
	t.Logf("modulated %d IQ samples at %d Sa/s", len(iq), sampleRateSX1255)

	// What deviation did we actually transmit? M17 is ±2400 Hz for symbol ±3.
	// This distinguishes a TX scaling error from an RX one: both would show up
	// as a constellation off the grid after the loopback, but only a TX error
	// puts the wrong signal on the air for every other receiver too.
	dev := make([]float64, len(iq)-1)
	for i := 1; i < len(iq); i++ {
		d := iq[i] * complex(real(iq[i-1]), -imag(iq[i-1]))
		dev[i-1] = math.Atan2(imag(d), real(d)) * float64(sampleRateSX1255) / (2 * math.Pi)
	}
	var peak float64
	for _, v := range dev {
		peak = math.Max(peak, math.Abs(v))
	}
	t.Logf("transmitted deviation: peak %.0f Hz (M17 nominal for symbol ±3 is 2400 Hz, ratio %.2f)",
		peak, peak/2400)
	t.Log("  (peak includes RRC overshoot: TX applies a single root-raised-cosine,")
	t.Log("   so symbol centres are only ISI-free after the receiver's matched filter)")

	// Optionally dump the reference signal in the same format as a real capture,
	// so identical off-line measurements can be run against both and any bias in
	// the measurement itself cancels out.
	if out := os.Getenv("M17_LOOPBACK_OUT"); out != "" {
		buf := make([]byte, len(iq)*8)
		for i, s := range iq {
			binary.LittleEndian.PutUint32(buf[i*8:], uint32(int32(real(s)*2147483647.0)))
			binary.LittleEndian.PutUint32(buf[i*8+4:], uint32(int32(imag(s)*2147483647.0)))
		}
		if err := os.WriteFile(out, buf, 0o644); err != nil {
			t.Fatalf("write reference capture: %v", err)
		}
		t.Logf("wrote reference capture to %s (S32_LE stereo, %d Sa/s)", out, sampleRateSX1255)
	}

	// --- receive: through the production pipeline ---
	in := make(chan complex128, sampleRateSX1255/2)
	out := sx1255RXPipeline(in)
	go func() {
		defer close(in)
		for _, s := range iq {
			in <- s
		}
	}()
	var got []Symbol
	for s := range out {
		got = append(got, Symbol(s))
	}
	if len(got) == 0 {
		t.Fatal("pipeline produced no symbols")
	}

	// Skip the AFC/filter settling transient at the start.
	const sps = 5
	if skip := 24000 / 4; len(got) > skip*2 {
		got = got[skip:]
	}

	bestPhase, bestRMS := 0, math.MaxFloat64
	for phase := range sps {
		dc := symbolMean(got, phase, sps)
		rms, _ := gridFit(got, phase, sps, dc)
		if rms < bestRMS {
			bestPhase, bestRMS = phase, rms
		}
	}
	dc := symbolMean(got, bestPhase, sps)
	var sumAbs float64
	n := 0
	for i := bestPhase; i < len(got); i += sps {
		sumAbs += math.Abs(float64(got[i]) - dc)
		n++
	}
	meanAbs := sumAbs / float64(n)

	t.Logf("recovered %d symbols, best phase %d, RMS distance to grid %.3f", len(got), bestPhase, bestRMS)
	t.Logf("mean|symbol| %.3f  (ideal 2.0 for an equiprobable ±1/±3 constellation)", meanAbs)
	t.Logf("=> with rxScalingCoeffSX1255 = %.3f, a spec-perfect signal lands %+.1f%% off the grid",
		rxScalingCoeffSX1255, 100*(meanAbs/2.0-1))
	t.Logf("=> coefficient implied by this reference signal: %.3f",
		rxScalingCoeffSX1255*2.0/meanAbs)

	acc, best := scanSyncs(got, sps)
	t.Logf("sync detector on a perfect signal: %d accepted, best distance %.3f", acc, best)

	// Sweep the coefficient over the same reference signal. If syncs appear at a
	// lower value then the chain is sound and only the constant is wrong; if
	// nothing decodes at any setting, the fault is elsewhere and this test is
	// measuring something other than what it claims.
	t.Logf("")
	t.Logf("--- coefficient sweep over the reference signal ---")
	t.Logf("%-10s %-8s %s", "coeff", "syncs", "best dist")
	for _, c := range []float64{1.00, 1.10, 1.20, 1.24, 1.30, 1.38, 1.45, 1.54} {
		in2 := make(chan complex128, sampleRateSX1255/2)
		out2 := sx1255RXPipelineTuned(in2, basebandDCAvgCntSX1255, c)
		go func() {
			defer close(in2)
			for _, s := range iq {
				in2 <- s
			}
		}()
		var g2 []Symbol
		for s := range out2 {
			g2 = append(g2, Symbol(s))
		}
		if skip := 24000 / 4; len(g2) > skip*2 {
			g2 = g2[skip:]
		}
		a, b := scanSyncs(g2, sps)
		t.Logf("%-10.2f %-8d %.3f", c, a, b)
	}
}
