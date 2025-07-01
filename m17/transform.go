package m17

import (
	"container/ring"
	"fmt"
	"math"

	"golang.org/x/exp/constraints"
)

const (
	//CC1200 User's Guide, p. 24
	//0xAD is `DEVIATION_M`, 2097152=2^21
	//+1.0 is the symbol for +0.8kHz
	//40.0e3 is F_TCXO in kHz
	//129 is `CFM_RX_DATA_OUT` register value at max. F_DEV
	//datasheet might have this wrong (it says 64)
	RXSymbolScalingCoeff = (1.0 / (0.8 / (40.0e3 / 2097152 * 0xAD) * 129.0))
	//0xAD is `DEVIATION_M`, 2097152=2^21
	//+0.8kHz is the deviation for symbol +1
	//40.0e3 is F_TCXO in kHz
	//64 is `CFM_TX_DATA_IN` register value for max. F_DEV
	TXSymbolScalingCoeff = (0.8 / ((40.0e3 / 2097152) * 0xAD) * 64.0)
)

var transmitGain = float32(math.Sqrt(5))

// alpha=0.5, span=8, sps=10, gain=sqrt(sps)
// var rrcTaps10 = []float32{
// 	-0.003195702904062,
// 	-0.002930279157647,
// 	-0.001940667871554,
// 	-0.000356087678024,
// 	0.001547011339078,
// 	0.003389554791180,
// 	0.004761898604226,
// 	0.005310860846139,
// 	0.004824746306020,
// 	0.003297923526849,
// 	0.000958710871219,
// 	-0.001749908029792,
// 	-0.004238694106631,
// 	-0.005881783042102,
// 	-0.006150256456781,
// 	-0.004745376707652,
// 	-0.001704189656474,
// 	0.002547854551540,
// 	0.007215575568845,
// 	0.011231038205364,
// 	0.013421952197061,
// 	0.012730475385624,
// 	0.008449554307304,
// 	0.000436744366018,
// 	-0.010735380379192,
// 	-0.023726883538258,
// 	-0.036498030780605,
// 	-0.046500883189991,
// 	-0.050979050576000,
// 	-0.047340680079891,
// 	-0.033554880492652,
// 	-0.008513823955726,
// 	0.027696543159614,
// 	0.073664520037517,
// 	0.126689053778116,
// 	0.182990955139334,
// 	0.238080025892860,
// 	0.287235637987092,
// 	0.326040247765297,
// 	0.350895727088113,
// 	0.359452932027608,
// 	0.350895727088113,
// 	0.326040247765297,
// 	0.287235637987092,
// 	0.238080025892860,
// 	0.182990955139334,
// 	0.126689053778116,
// 	0.073664520037517,
// 	0.027696543159614,
// 	-0.008513823955726,
// 	-0.033554880492652,
// 	-0.047340680079891,
// 	-0.050979050576000,
// 	-0.046500883189991,
// 	-0.036498030780605,
// 	-0.023726883538258,
// 	-0.010735380379192,
// 	0.000436744366018,
// 	0.008449554307304,
// 	0.012730475385624,
// 	0.013421952197061,
// 	0.011231038205364,
// 	0.007215575568845,
// 	0.002547854551540,
// 	-0.001704189656474,
// 	-0.004745376707652,
// 	-0.006150256456781,
// 	-0.005881783042102,
// 	-0.004238694106631,
// 	-0.001749908029792,
// 	0.000958710871219,
// 	0.003297923526849,
// 	0.004824746306020,
// 	0.005310860846139,
// 	0.004761898604226,
// 	0.003389554791180,
// 	0.001547011339078,
// 	-0.000356087678024,
// 	-0.001940667871554,
// 	-0.002930279157647,
// 	-0.003195702904062,
// }

// alpha=0.5, span=8, sps=5, gain=sqrt(sps)
var rrcTaps5 = []float32{
	-0.004519384154389,
	-0.002744505321971,
	0.002187793653660,
	0.006734308458208,
	0.006823188093192,
	0.001355815246317,
	-0.005994389201970,
	-0.008697733303330,
	-0.002410076268276,
	0.010204314627992,
	0.018981413448435,
	0.011949415510291,
	-0.015182045838927,
	-0.051615756197679,
	-0.072094910038768,
	-0.047453533621088,
	0.039168634270669,
	0.179164496628150,
	0.336694345124862,
	0.461088271869920,
	0.508340710642860,
	0.461088271869920,
	0.336694345124862,
	0.179164496628150,
	0.039168634270669,
	-0.047453533621088,
	-0.072094910038768,
	-0.051615756197679,
	-0.015182045838927,
	0.011949415510291,
	0.018981413448435,
	0.010204314627992,
	-0.002410076268276,
	-0.008697733303330,
	-0.005994389201970,
	0.001355815246317,
	0.006823188093192,
	0.006734308458208,
	0.002187793653660,
	-0.002744505321971,
	-0.004519384154389,
}

type Number interface {
	constraints.Integer | constraints.Float
}

// Generic transformation
type Transform[I any, O any] struct {
	sink      chan I
	source    chan O
	transform func(I) []O
}

func NewTransform[I any, O any](sink chan I, transform func(I) []O, sourceSize int) Transform[I, O] {
	ret := Transform[I, O]{
		sink:      sink,
		source:    make(chan O, sourceSize),
		transform: transform,
	}
	go ret.handle()
	return ret
}

func (t *Transform[I, O]) Source() chan O {
	return t.source
}

func (t *Transform[I, O]) handle() {
	for {
		sample, ok := <-t.sink
		if !ok {
			break
		}
		for _, s := range t.transform(sample) {
			t.source <- s
		}
	}
	close(t.source)
}

// Filter DC from int8 samples by subtracting a moving average
type DCFilter struct {
	Transform[int8, int8]
	averageN  int
	movingAvg int8
}

func NewDCFilter(sink chan int8, averageN int) (DCFilter, error) {
	ret := DCFilter{
		averageN: averageN,
	}
	if averageN < 1 {
		return ret, fmt.Errorf("averageN must be greater than zero")
	}
	ret.Transform = NewTransform(sink, ret.dcFilter, 0)
	return ret, nil
}
func (t *DCFilter) dcFilter(sample int8) []int8 {
	t.movingAvg = int8((int(t.movingAvg)*(t.averageN-1) + int(sample)) / t.averageN)
	// log.Printf("[DEBUG] movingAvg: %d, sample: %d, ret: %d", t.movingAvg, sample, sample-t.movingAvg)
	return []int8{sample - t.movingAvg}
}

// scale samples by a factor
type Scaler[T Number] struct {
	Transform[T, T]
	factor T
}

func NewScaler[T Number](sink chan T, factor T) Scaler[T] {
	ret := Scaler[T]{
		factor: factor,
	}
	ret.Transform = NewTransform(sink, ret.scale, 0)
	return ret
}
func (t *Scaler[T]) scale(sample T) []T {
	return []T{sample * t.factor}
}

// Transform int8 samples to float32 symbols by RRC filtering them
type SampleToSymbol struct { // Make this generic in the future?
	Transform[int8, float32]
	fltBuff      *ring.Ring //length of this has to match RRC filter's length
	rrcTaps      []float32
	scalingCoeff float32
}

func NewSampleToSymbol(sink chan int8, rrcTaps []float32, scalingCoeff float32) SampleToSymbol {
	ret := SampleToSymbol{
		fltBuff:      ring.New(len(rrcTaps)),
		rrcTaps:      rrcTaps,
		scalingCoeff: scalingCoeff,
	}
	for range rrcTaps {
		ret.fltBuff.Value = float32(0)
		ret.fltBuff = ret.fltBuff.Next()
	}
	ret.Transform = NewTransform(sink, ret.transform, 0)
	return ret
}

func (t *SampleToSymbol) Source() chan float32 {
	return t.source
}

func (t *SampleToSymbol) transform(sample int8) []float32 {
	var symbol float32
	t.fltBuff.Value = float32(sample)
	t.fltBuff = t.fltBuff.Next()

	i := 0
	t.fltBuff.Do(func(p any) {
		f := p.(float32)
		symbol += t.rrcTaps[i] * f
		// log.Printf("[DEBUG] i: %d, f: %f, symbol: %f", i, f, symbol)
		i++
	})
	symbol *= t.scalingCoeff
	return []float32{symbol}
}

// Transform float32 symbols to int8 samples
type SymbolToSample struct {
	// Transform[float32, int8]
	last             *ring.Ring //length of this has to match RRC filter's length
	rrcTaps          []float32
	scalingCoeff     float32
	phaseInvert      bool
	samplesPerSymbol int
}

func NewSymbolToSample(rrcTaps []float32, scalingCoeff float32, phaseInvert bool, samplesPerSymbol int) SymbolToSample {
	ret := SymbolToSample{
		last:             ring.New(len(rrcTaps)),
		rrcTaps:          rrcTaps,
		scalingCoeff:     scalingCoeff,
		phaseInvert:      phaseInvert,
		samplesPerSymbol: samplesPerSymbol,
	}
	for range rrcTaps {
		ret.last.Value = float32(0)
		ret.last = ret.last.Next()
	}
	// ret.Transform = NewTransform(sink, ret.transform, 0)
	return ret
}

func (t *SymbolToSample) Transform(symbols []Symbol) []byte {
	ret := make([]byte, len(symbols)*t.samplesPerSymbol)
	for i, symbol := range symbols {
		for j := 0; j < t.samplesPerSymbol; j++ {
			if j == 0 {
				if t.phaseInvert {
					t.last.Value = -float32(symbol)
				} else {
					t.last.Value = float32(symbol)
				}
			} else {
				t.last.Value = float32(0)
			}
			t.last = t.last.Next()
			var acc float32
			k := 0
			t.last.Do(func(p any) {
				f := p.(float32)
				acc += t.rrcTaps[k] * f
				k++
			})
			ret[i*t.samplesPerSymbol+j] = byte(acc * t.scalingCoeff)
		}
	}
	return ret
}

// Downsample a stream by returning one out of each N values
type Downsampler[T any] struct {
	Transform[T, T]
	factor int
	offset int
	count  int
}

func NewDownsampler[T any](sink chan T, factor int, offset int) (Downsampler[T], error) {
	ret := Downsampler[T]{
		factor: factor,
		offset: offset,
	}
	if offset < 0 || offset >= factor {
		return ret, fmt.Errorf("offset must be between 0 and %d", factor)
	}
	ret.Transform = NewTransform(sink, ret.downsample, 0)
	return ret, nil
}

func (t *Downsampler[T]) downsample(sample T) []T {
	ret := []T{}
	if t.count%t.factor == t.offset {
		ret = []T{sample}
		t.count = t.offset
	}
	t.count++
	return ret
}
