package modem

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"

	"github.com/jancona/m17"
)

// TestFMDemodAFCRestartsOnCarrier: idle noise that demodulates to a nonzero
// mean must not carry over into a transmission. A weak tone at +1000 Hz
// stands in for the biased idle noise; a strong carrier then arrives at
// -500 Hz. Within 20-40 ms of its arrival the AFC output must be near zero.
func TestFMDemodAFCRestartsOnCarrier(t *testing.T) {
	const fs = 25000.0
	sink := make(chan complex128, 100)
	f := NewFMDemodAFC(sink, 4000, 25, 2500)
	go func() {
		defer close(sink)
		for i := range int(fs) { // 1 s idle
			sink <- 0.01 * cmplx.Rect(1, 2*math.Pi*1000*float64(i)/fs)
		}
		for i := range int(fs / 5) { // 200 ms carrier
			sink <- cmplx.Rect(1, -2*math.Pi*500*float64(i)/fs)
		}
	}()
	var out []float64
	for v := range f.Source() {
		out = append(out, v)
	}
	var m float64
	a, b := int(fs)+int(fs*0.020), int(fs)+int(fs*0.040)
	for _, v := range out[a:b] {
		m += v
	}
	hz := m / float64(b-a) * fs / (2 * math.Pi)
	if math.Abs(hz) > 25 {
		t.Errorf("AFC output 20-40 ms after the carrier arrived averages %+.0f Hz, want near 0", hz)
	}
}

// TestFMDemodAFCSteadyCarrier: a carrier whose level wanders by a few dB
// over a long over must not restart the estimate. The carrier is 400 Hz off;
// after it has settled, the output must stay centred to the end.
func TestFMDemodAFCSteadyCarrier(t *testing.T) {
	const fs = 25000.0
	sink := make(chan complex128, 100)
	f := NewFMDemodAFC(sink, 4000, 25, 2500)
	go func() {
		defer close(sink)
		for i := range int(fs / 2) {
			sink <- 0.01 * cmplx.Rect(1, 2*math.Pi*1000*float64(i)/fs)
		}
		for i := range int(fs * 3) { // 3 s, level wandering +-3 dB at 2 Hz
			amp := math.Pow(10, 3*math.Sin(2*math.Pi*2*float64(i)/fs)/20)
			sink <- complex(amp, 0) * cmplx.Rect(1, 2*math.Pi*400*float64(i)/fs)
		}
	}()
	var out []float64
	for v := range f.Source() {
		out = append(out, v)
	}
	for _, sec := range []float64{1, 2, 3} {
		a := int(fs/2 + fs*(sec-0.2))
		var m float64
		for _, v := range out[a : a+int(fs/10)] {
			m += v
		}
		if hz := m / (fs / 10) * fs / (2 * math.Pi); math.Abs(hz) > 10 {
			t.Errorf("%.0f s into the carrier the AFC output averages %+.0f Hz, want near 0", sec, hz)
		}
	}
}

// TestFMDemodAFCPicketFencing: mobile flutter, with the carrier dropping to
// the noise for 15 ms every 80 ms, must not restart the estimate; the output
// stays centred.
func TestFMDemodAFCPicketFencing(t *testing.T) {
	const fs = 25000.0
	sink := make(chan complex128, 100)
	f := NewFMDemodAFC(sink, 4000, 25, 2500)
	go func() {
		defer close(sink)
		for i := range int(fs / 2) {
			sink <- 0.01 * cmplx.Rect(1, 2*math.Pi*1000*float64(i)/fs)
		}
		for i := range int(fs * 2) {
			ph := cmplx.Rect(1, 2*math.Pi*400*float64(i)/fs)
			if i%int(fs*0.080) < int(fs*0.015) {
				sink <- 0.01 * ph // dropout into the noise
			} else {
				sink <- ph
			}
		}
	}()
	var out []float64
	for v := range f.Source() {
		out = append(out, v)
	}
	for _, sec := range []float64{0.5, 1, 1.5, 2} {
		a := int(fs/2 + fs*(sec-0.16))
		var m float64
		for _, v := range out[a : a+int(fs*0.16)] { // two whole 80 ms cycles
			m += v
		}
		if hz := m / (fs * 0.16) * fs / (2 * math.Pi); math.Abs(hz) > 10 {
			t.Errorf("%.1f s into the fading carrier the AFC output averages %+.0f Hz, want near 0", sec, hz)
		}
	}
}

// TestFMDemodAFCNoiseSpike: a brief spike during idle must not stop the
// next transmission from restarting the estimate.
func TestFMDemodAFCNoiseSpike(t *testing.T) {
	const fs = 25000.0
	sink := make(chan complex128, 100)
	f := NewFMDemodAFC(sink, 4000, 25, 2500)
	go func() {
		defer close(sink)
		for i := range int(fs) { // 1 s idle, with a 2 ms spike at 0.5 s
			a := 0.01
			if i >= int(fs/2) && i < int(fs/2)+50 {
				a = 0.5
			}
			sink <- complex(a, 0) * cmplx.Rect(1, 2*math.Pi*1000*float64(i)/fs)
		}
		for i := range int(fs / 5) {
			sink <- cmplx.Rect(1, -2*math.Pi*500*float64(i)/fs)
		}
	}()
	var out []float64
	for v := range f.Source() {
		out = append(out, v)
	}
	var m float64
	a, b := int(fs)+int(fs*0.020), int(fs)+int(fs*0.040)
	for _, v := range out[a:b] {
		m += v
	}
	if hz := m / float64(b-a) * fs / (2 * math.Pi); math.Abs(hz) > 25 {
		t.Errorf("after an idle spike, AFC output 20-40 ms into a carrier averages %+.0f Hz, want near 0", hz)
	}
}

// TestSX1255ShortPreambleOffset: a packet with a 40 ms preamble (as OpenRTX
// sends) from a transmitter up to 700 Hz off frequency, after idle noise,
// decodes through the SX1255 receive chain with a clean LSF.
func TestSX1255ShortPreambleOffset(t *testing.T) {
	for _, off := range []float64{0, 300, -300, 700, -700} {
		lsf, pk := decodeIQ(synthPacket(t, off, "the quick brown fox jumps over the lazy dog"), sx1255RXPipeline)
		if len(lsf) != 1 || len(pk) != 1 {
			t.Errorf("offset %+.0f Hz: decoded %d LSF and %d packets, want 1 and 1", off, len(lsf), len(pk))
			continue
		}
		if lsf[0] > 0.5 || pk[0] > 0.5 {
			t.Errorf("offset %+.0f Hz: LSF BER %.2f%%, packet BER %.2f%%, want under 0.5%%", off, lsf[0], pk[0])
		}
	}
}

// synthPacket builds IQ at 125 kSa/s: noise, then a 40 ms preamble, LSF,
// packet frames and EOT, with the carrier offset by offHz and a DC leak.
func synthPacket(t *testing.T, offHz float64, text string) []complex128 {
	p, err := m17.NewPacket("N1ADJ", "N1ADJ 8", m17.PacketTypeSMS, append([]byte(text), 0))
	if err != nil {
		t.Fatal(err)
	}
	syms := m17.AppendPreamble(nil, m17.LSFPreamble)
	ls, err := generateLSFSymbols(p.LSF)
	if err != nil {
		t.Fatal(err)
	}
	syms = append(syms, ls...)
	data := p.PayloadBytes()
	for n := 0; n*25 < len(data); n++ {
		chunk := make([]byte, 26)
		left := copy(chunk, data[n*25:])
		if (n+1)*25 < len(data) {
			chunk[25] = byte(n << 2)
		} else {
			chunk[25] = byte(1<<7 | min(left, 25)<<2)
		}
		b, _ := m17.ConvolutionalEncode(chunk, m17.PacketPuncturePattern, m17.PacketModeFinalBit)
		syms = m17.AppendSyncwordSymbols(syms, m17.PacketSync)
		syms = m17.AppendBits(syms, m17.RandomizeBits(m17.InterleaveBits(m17.NewPayloadBits(b))))
	}
	syms = m17.AppendEOT(syms)
	shaper := NewTXPulseShaper(rrcTaps5, 5)
	rs := NewBatchResampler(125, 24)
	fm := NewBatchFMModulator(float64(sampleRateSX1255))
	bb := shaper.Process(syms)
	for i := range bb {
		bb[i] *= math.Sqrt(5)
	}
	sig := fm.Modulate(rs.Process(bb), 800)
	r := rand.New(rand.NewSource(1))
	noise := func() complex128 { return complex(r.NormFloat64(), r.NormFloat64()) * 0.003 }
	leak := complex(0.01, 0.004)
	var out []complex128
	for range sampleRateSX1255 / 2 { // 500 ms idle
		out = append(out, leak+noise())
	}
	w := 2 * math.Pi * offHz / float64(sampleRateSX1255)
	for i, s := range sig {
		out = append(out, 0.3*s*cmplx.Rect(1, w*float64(i))+leak+noise())
	}
	for range sampleRateSX1255 / 5 {
		out = append(out, leak+noise())
	}
	return out
}

func decodeIQ(iqs []complex128, pipe func(chan complex128) chan float32) (lsf, pk []float64) {
	dec := m17.NewDecoder(
		func(l m17.LSF, b float64) error { lsf = append(lsf, b); return nil },
		func(m17.LSF, []byte, uint16, uint16, float64) error { return nil },
		func(m17.LSF, float64) error { return nil },
		func(m17.LSF, uint16, uint16, float64) error { return nil },
		func(l m17.LSF, p []byte, b float64) error { pk = append(pk, b); return nil })
	iq := make(chan complex128, sampleRateSX1255/2)
	symbols := pipe(iq)
	go func() {
		defer close(iq)
		for _, x := range iqs {
			iq <- x
		}
	}()
	const sps = 5
	var syms []m17.Symbol
	for s := range symbols {
		syms = append(syms, m17.Symbol(s))
	}
	need := 2*(m17.SymbolsPerFrame*sps) + 16*sps
	for off := 0; off+need < len(syms); {
		dist, typ := m17.SyncDistance(syms, off, sps)
		thr := float32(5.0)
		if typ == m17.LSFSync || typ == m17.EOTMarker {
			thr = 4.5
		}
		if dist >= thr {
			off++
			continue
		}
		rest, pld, _ := extractPayload(dist, typ, syms[off:], sps)
		dec.DecodeFrame(typ, pld)
		off += len(syms[off:]) - len(rest)
	}
	return
}
