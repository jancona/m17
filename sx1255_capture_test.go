package m17

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"
)

// TestSX1255Capture replays a recorded SX1255 IQ capture through the real RX
// DSP chain and reports what comes out. It is a diagnostic harness, not an
// assertion-heavy unit test: it exists so the pipeline can be measured against
// ground-truth hardware data instead of a reimplementation of it.
//
// The capture is raw interleaved stereo S32_LE at 125 kSa/s — exactly what
// captureLoop reads from ALSA (left=I, right=Q). Produce one with:
//
//	arecord -D hw:0,1 -f S32_LE -c 2 -r 125000 -d 20 rx-test.raw
//
// Skipped unless a path is given:
//
//	go test -run TestSX1255Capture -v . -args -capture /path/to/rx-test.raw
//	M17_RX_CAPTURE=/path/to/rx-test.raw go test -run TestSX1255Capture -v .
func TestSX1255Capture(t *testing.T) {
	path := os.Getenv("M17_RX_CAPTURE")
	if path == "" {
		t.Skip("set M17_RX_CAPTURE to a raw S32_LE stereo 125 kSa/s capture to run this")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	const bytesPerFrame = 8
	nFrames := len(raw) / bytesPerFrame
	t.Logf("capture: %s, %d frames = %.2f s at %d Sa/s",
		path, nFrames, float64(nFrames)/float64(sampleRateSX1255), sampleRateSX1255)

	// Drive the real pipeline. Feeding and draining must be concurrent: the
	// stages are connected by unbuffered channels.
	iqSamples := make(chan complex128, sampleRateSX1255/2)
	symbols := sx1255RXPipeline(iqSamples)

	go func() {
		defer close(iqSamples)
		for i := range nFrames {
			off := i * bytesPerFrame
			iS := int32(binary.LittleEndian.Uint32(raw[off : off+4]))
			qS := int32(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
			// Same normalisation captureLoop applies.
			iqSamples <- complex(float64(iS)/2147483648.0, float64(qS)/2147483648.0)
		}
	}()

	var syms []Symbol
	for s := range symbols {
		syms = append(syms, Symbol(s))
	}
	if len(syms) == 0 {
		t.Fatal("pipeline produced no symbols")
	}
	t.Logf("pipeline produced %d symbols at %d sps", len(syms), 5)

	// Drop the first 0.5 s: the huge first ALSA sample and the DC-removal
	// filter's settling ring through the whole chain and skew every statistic.
	const sps = 5
	if skip := 24000 / 2; len(syms) > skip*2 {
		syms = syms[skip:]
	}

	// --- constellation: how well do symbols sit on the ±1/±3 grid? ---
	// The scale error and a carrier offset confound each other: a DC offset
	// makes a grid fit shrink everything toward zero. So measure and remove the
	// offset first, then fit scale. We do not know the symbol phase, so try all
	// 5 and keep the best — that is what a correctly timed decoder would see.
	bestPhase, bestRMS, bestScale, bestDC := 0, math.MaxFloat64, 1.0, 0.0
	for phase := range sps {
		dc := symbolMean(syms, phase, sps)
		rms, scale := gridFit(syms, phase, sps, dc)
		if rms < bestRMS {
			bestPhase, bestRMS, bestScale, bestDC = phase, rms, scale, dc
		}
	}
	t.Logf("best symbol phase %d: RMS distance to ±1/±3 grid = %.3f (after removing DC)",
		bestPhase, bestRMS)

	// Level statistics at the best phase.
	var sumAbs, maxAbs float64
	n := 0
	for i := bestPhase; i < len(syms); i += sps {
		v := float64(syms[i]) - bestDC
		sumAbs += math.Abs(v)
		maxAbs = math.Max(maxAbs, math.Abs(v))
		n++
	}
	meanAbs := sumAbs / float64(n)
	t.Logf("symbol stats (DC removed): mean|v| %.3f, max|v| %.3f, n=%d", meanAbs, maxAbs, n)

	t.Logf("")
	t.Logf("--- carrier offset ---")
	t.Logf("DC at symbol instants: %+.3f symbol units = %+.0f Hz (1 unit = 800 Hz)",
		bestDC, bestDC*800)
	t.Logf("a constant bias b adds 4*|b| = %.2f to every sync distance", 4*math.Abs(bestDC))

	t.Logf("")
	t.Logf("--- symbol scale ---")
	t.Logf("current rxScalingCoeffSX1255 = %.3f", rxScalingCoeffSX1255)
	t.Logf("grid fit          -> factor %.3f -> coefficient %.3f", bestScale, rxScalingCoeffSX1255*bestScale)
	// An ideal, equiprobable M17 constellation has mean|symbol| = (1+1+3+3)/4 = 2.
	t.Logf("mean|v| vs ideal 2.0 -> factor %.3f -> coefficient %.3f",
		2.0/meanAbs, rxScalingCoeffSX1255*2.0/meanAbs)
	k := meanAbs / 2.0
	t.Logf("constellation is %.0f%% off the grid; that alone puts a floor of 12*|k-1| = %.2f on sync distance",
		100*(k-1), 12*math.Abs(k-1))

	// --- what the real sync detector makes of it ---
	// syncDistance needs 2 frame strides of lookahead.
	need := 2*(SymbolsPerFrame*sps) + 16*sps
	hist := map[uint16]int{}
	var best float32 = math.MaxFloat32
	// Advance one sample at a time, as processSymbolStream does: the symbol
	// phase is unknown, and stepping by sps would only ever test phase 0.
	for off := 0; off+need < len(syms); off++ {
		dist, typ := syncDistance(syms, off, sps)
		if dist < best {
			best = dist
		}
		thr := float32(5.0)
		if typ == LSFSync || typ == EOTMarker {
			thr = 4.5
		}
		if dist < thr {
			hist[typ]++
		}
	}
	t.Logf("best sync distance seen: %.3f (thresholds: LSF/EOT 4.5, Stream/Packet 5.0)", best)
	t.Logf("syncs accepted: LSF=%d Stream=%d Packet=%d EOT=%d",
		hist[LSFSync], hist[StreamSync], hist[PacketSync], hist[EOTMarker])

	expFrames := float64(nFrames) / float64(sampleRateSX1255) / 0.04
	t.Logf("~%.0f M17 frames expected in this capture if it were continuous", expFrames)

	// --- what would fixing each defect buy us? ---
	// Re-run the real detector over symbols with the carrier offset removed
	// and/or the scale corrected, to attribute the failure between the two.
	t.Logf("")
	t.Logf("--- attribution: rerunning syncDistance on corrected symbols ---")
	scaleFix := 2.0 / meanAbs
	for _, c := range []struct {
		name  string
		dc    float64
		scale float64
	}{
		{"as shipped", 0, 1},
		{"carrier offset removed", bestDC, 1},
		{"scale corrected", 0, scaleFix},
		{"both", bestDC, scaleFix},
	} {
		fixed := make([]Symbol, len(syms))
		for i, s := range syms {
			fixed[i] = Symbol((float64(s) - c.dc) * c.scale)
		}
		acc, bd := scanSyncs(fixed, sps)
		t.Logf("  %-24s best distance %.2f, syncs accepted %d", c.name, bd, acc)
	}
}

// TestSX1255CaptureSweep replays a capture through the pipeline repeatedly,
// sweeping the software-AFC window and the symbol scaling coefficient, and
// reports how many syncs the production detector accepts for each. Use it to
// pick those two constants against real data rather than by eye.
//
// Same capture requirement as TestSX1255Capture.
func TestSX1255CaptureSweep(t *testing.T) {
	path := os.Getenv("M17_RX_CAPTURE")
	if path == "" {
		t.Skip("set M17_RX_CAPTURE to a raw S32_LE stereo 125 kSa/s capture to run this")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	const sps = 5

	runGain := func(dcAvgCnt int, coeff, gain float64) (int, float32) {
		iq := make(chan complex128, sampleRateSX1255/2)
		out := sx1255RXPipelineTuned(iq, dcAvgCnt, coeff)
		go func() {
			defer close(iq)
			for off := 0; off+8 <= len(raw); off += 8 {
				iS := int32(binary.LittleEndian.Uint32(raw[off : off+4]))
				qS := int32(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
				iq <- complex(float64(iS)/2147483648.0*gain, float64(qS)/2147483648.0*gain)
			}
		}()
		var syms []Symbol
		for s := range out {
			syms = append(syms, Symbol(s))
		}
		if skip := 24000 / 2; len(syms) > skip*2 {
			syms = syms[skip:]
		}
		return scanSyncs(syms, sps)
	}
	run := func(dcAvgCnt int, coeff float64) (int, float32) {
		return runGain(dcAvgCnt, coeff, 1.0)
	}

	// Does raw amplitude matter? An FM demodulator works on the angle, so
	// scaling IQ should change nothing — if it does not, then raising the
	// SX1255's LNA/PGA gain helps by improving the front-end noise figure, not
	// by making the samples bigger, and no amount of software gain substitutes.
	t.Logf("--- IQ amplitude sweep (does level alone matter?) ---")
	t.Logf("%-12s %-8s %s", "gain", "syncs", "best dist")
	for _, g := range []float64{0.25, 1, 4, 16, 64} {
		acc, best := runGain(basebandDCAvgCntSX1255, rxScalingCoeffSX1255, g)
		t.Logf("%-12.2f %-8d %.2f", g, acc, best)
	}
	t.Logf("")

	// A count of 1 skips the filter entirely: the behaviour before software AFC.
	t.Logf("--- software AFC window sweep (coefficient held at %.2f) ---", rxScalingCoeffSX1255)
	t.Logf("%-12s %-10s %-8s %s", "avgCnt", "window", "syncs", "best dist")
	for _, cnt := range []int{1, 125, 250, 500, 1000, 2000, 4000, 8000, 20000} {
		acc, best := run(cnt, rxScalingCoeffSX1255)
		label := fmt.Sprintf("%.0f ms", float64(cnt)/12500*1000)
		if cnt <= 1 {
			label = "off"
		}
		t.Logf("%-12d %-10s %-8d %.2f", cnt, label, acc, best)
	}

	t.Logf("")
	t.Logf("--- scaling coefficient sweep (AFC window held at %d) ---", basebandDCAvgCntSX1255)
	t.Logf("%-12s %-8s %s", "coeff", "syncs", "best dist")
	for _, c := range []float64{0.9, 1.0, 1.1, 1.2, 1.3, 1.38, 1.45, 1.54, 1.7} {
		acc, best := run(basebandDCAvgCntSX1255, c)
		t.Logf("%-12.2f %-8d %.2f", c, acc, best)
	}
}

// TestSX1255CaptureDecode replays a capture through the full receive path —
// DSP, sync detection, and the real Decoder — and reports what the decoder
// makes of it: LSFs, stream frames, and end-of-stream events.
//
// It is the end-to-end counterpart to TestSX1255Capture, and the only way to
// exercise the decoder's stream-termination logic against real off-air data.
//
// Same capture requirement as TestSX1255Capture.
func TestSX1255CaptureDecode(t *testing.T) {
	path := os.Getenv("M17_RX_CAPTURE")
	if path == "" {
		t.Skip("set M17_RX_CAPTURE to a raw S32_LE stereo 125 kSa/s capture to run this")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}

	var lsfs, frames, eots, lichs int
	dec := NewDecoder(
		func(lsf LSF, ber float64) error { lsfs++; return nil },
		func(lsf LSF, payload []byte, sid, fn uint16, ber float64) error { frames++; return nil },
		func(lsf LSF, ber float64) error { lichs++; return nil },
		func(lsf LSF, sid, fn uint16, ber float64) error {
			eots++
			t.Logf("  end of stream %04x at frame %04x (ber %.2f%%)", sid, fn, ber)
			return nil
		},
		func(lsf LSF, payload []byte, ber float64) error { return nil },
	)

	iq := make(chan complex128, sampleRateSX1255/2)
	symbols := sx1255RXPipeline(iq)
	go func() {
		defer close(iq)
		for off := 0; off+8 <= len(raw); off += 8 {
			iS := int32(binary.LittleEndian.Uint32(raw[off : off+4]))
			qS := int32(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
			iq <- complex(float64(iS)/2147483648.0, float64(qS)/2147483648.0)
		}
	}()

	// processSymbolStream blocks forever waiting to refill its buffer, so drive
	// the sync search here rather than reusing it.
	const sps = 5
	var syms []Symbol
	for s := range symbols {
		syms = append(syms, Symbol(s))
	}
	need := 2*(SymbolsPerFrame*sps) + 16*sps
	for off := 0; off+need < len(syms); {
		dist, typ := syncDistance(syms, off, sps)
		thr := float32(5.0)
		if typ == LSFSync || typ == EOTMarker {
			thr = 4.5
		}
		if dist >= thr {
			off++ // no sync here; advance one sample, as production does
			continue
		}
		rest, pld, _ := extractPayload(dist, typ, syms[off:], sps)
		dec.DecodeFrame(typ, pld)
		off += len(syms[off:]) - len(rest)
	}

	t.Logf("decoded: %d LSF, %d stream frames, %d LICH reassemblies, %d end-of-stream events",
		lsfs, frames, lichs, eots)
	dur := float64(len(raw)/8) / float64(sampleRateSX1255)
	t.Logf("capture is %.1f s; a single continuous over would be ~%.0f frames and 1 end-of-stream",
		dur, dur/0.04)
}

// scanSyncs runs the production sync detector across a symbol stream and
// returns how many syncs pass their threshold, plus the best distance seen.
func scanSyncs(syms []Symbol, sps int) (accepted int, best float32) {
	best = math.MaxFloat32
	need := 2*(SymbolsPerFrame*sps) + 16*sps
	// One sample at a time, matching processSymbolStream: stepping by sps would
	// only ever sample symbol phase 0 and miss every other alignment.
	for off := 0; off+need < len(syms); off++ {
		dist, typ := syncDistance(syms, off, sps)
		if dist < best {
			best = dist
		}
		thr := float32(5.0)
		if typ == LSFSync || typ == EOTMarker {
			thr = 4.5
		}
		if dist < thr {
			accepted++
		}
	}
	return accepted, best
}

// symbolMean returns the mean symbol value at the given phase — the carrier
// offset, in symbol units, as the decoder sees it.
func symbolMean(syms []Symbol, phase, sps int) float64 {
	var sum float64
	n := 0
	for i := phase; i < len(syms); i += sps {
		sum += float64(syms[i])
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// gridFit returns the RMS distance of symbols at the given phase to the nearest
// ideal level, and the scale factor that would minimise it, after removing dc.
func gridFit(syms []Symbol, phase, sps int, dc float64) (rms, scale float64) {
	levels := []float64{-3, -1, 1, 3}
	bestRMS, bestScale := math.MaxFloat64, 1.0
	for s := 0.05; s <= 3.00; s += 0.005 {
		var sse float64
		n := 0
		for i := phase; i < len(syms); i += sps {
			v := (float64(syms[i]) - dc) * s
			nearest := levels[0]
			for _, l := range levels[1:] {
				if math.Abs(v-l) < math.Abs(v-nearest) {
					nearest = l
				}
			}
			d := v - nearest
			sse += d * d
			n++
		}
		if n == 0 {
			continue
		}
		if r := math.Sqrt(sse / float64(n)); r < bestRMS {
			bestRMS, bestScale = r, s
		}
	}
	return bestRMS, bestScale
}
