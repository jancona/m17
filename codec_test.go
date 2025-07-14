package m17

import (
	"math"
	"reflect"
	"testing"
)

func Test_ConvolutionalEncode(t *testing.T) {
	type args struct {
		in              []byte
		puncturePattern PuncturePattern
		finalBits       byte
	}
	tests := []struct {
		name    string
		args    args
		want    *[]Bit
		wantErr bool
	}{
		{"empty",
			args{
				make([]byte, 0),
				PacketPuncturePattern,
				PacketModeFinalBit,
			},
			nil,
			true,
		},
		{"empty packet",
			args{
				make([]byte, 26),
				PacketPuncturePattern,
				PacketModeFinalBit,
			},
			&[]Bit{
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false, // 100
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false, // 200
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false, // 300
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false, false, false,
				false, false, false, false, false, false, false, false},
			false,
		},
		{"packet",
			args{
				[]byte{0x5, 0x68, 0x69, 0x0, 0x99, 0xfb, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x98},
				PacketPuncturePattern,
				PacketModeFinalBit,
			},
			&[]Bit{false, false, false, false, false, false, false, false, false, true, true, false, true, true, true, true, false, true, false, false, true, false, false, false, false, true, false, true, true, true, true, true, true, false, false, false, false, false, false, true, false, false, true, false, false, true, true, false, true, false, false, false, false, false, false, false, true, true, false, true, false, true, false, false, true, false, false, true, true, true, false, true, true, true, false, true, true, true, false, false, true, true, true, true, true, true, true, true, false, true, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true, true, false, true, false, true, false, false, true, false, false, true, true, false, true, true, false, false},
			false,
		},
		{"lsf",
			args{
				[]byte{0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6, 0x0, 0x0, 0x1, 0x8a, 0x92, 0xae, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xf5, 0x4e},
				LSFPuncturePattern,
				LSFFinalBit,
			},
			&[]Bit{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true, false, true, true, false, true, false, true, false, false, true, true, true, false, true, false, false, true, true, false, true, false, true, true, false, false, true, true, false, false, true, true, false, false, false, true, false, true, false, true, false, false, true, true, false, false, true, true, false, true, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true, true, false, false, false, true, false, true, false, false, true, false, true, true, true, true, true, false, false, true, false, false, true, true, true, false, false, true, true, true, true, true, false, false, false, false, true, false, false, false, true, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true, false, true, false, true, false, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true, true, false, true, false, true, false, true, true, false, false, true, true, false, true, true, true, false, false, false, true, true, true, false, false, false, true, true, false, false},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvolutionalEncode(tt.args.in, tt.args.puncturePattern, tt.args.finalBits)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvolutionalEncode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvolutionalEncode() = %v, want %v", got, tt.want)
			}
		})
	}
}

// func TestViterbiDecoder_DecodePunctured(t *testing.T) {
// 	type fields struct {
// 		history         []uint16
// 		prevMetrics     []float64
// 		currMetrics     []float64
// 		prevMetricsData []float32
// 		currMetricsData []float32
// 	}
// 	type args struct {
// 		puncturedSoftBits []Symbol
// 		puncturePattern   PuncturePattern
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		args   args
// 		want   []byte
// 		want1  float64
// 	}{
// 		{"nonoise LSF",
// 			fields{},
// 			args{
// 				puncturedSoftBits: []Symbol{1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 1, 0.99998474, 0, 1.5259022e-05, 1, 0.99998474, 1, 0, 0, 1, 1, 0, 1, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0.99998474, 1, 1, 0, 1, 0, 1, 1, 0, 1, 0, 0, 0, 1, 0, 0, 1.5259022e-05, 1, 1, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.99998474, 1, 1, 0, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 0.99998474, 0, 1.5259022e-05, 1, 1.5259022e-05, 1, 1, 0, 1, 1, 1.5259022e-05, 1, 0.99998474, 1, 1, 0, 1, 0, 0, 0, 0, 1, 0, 1, 0, 1, 1, 0, 1, 1, 0.99998474, 0, 0, 1, 0, 1, 0.99998474, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.99998474, 0, 0.99998474, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1, 0, 0.99998474, 0, 1, 0, 0, 1, 0, 0, 1.5259022e-05, 0, 0, 0, 1, 0, 0.99998474, 1, 0, 1, 0.99998474, 1, 1.5259022e-05, 0, 0, 1, 1.5259022e-05, 1, 0.99998474, 1},
// 				puncturePattern:   LSFPuncturePattern,
// 			},
// 			[]byte{0x0, 0x0, 0x0, 0x1, 0x8a, 0x92, 0xae, 0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8d, 0x6d},
// 			0.0,
// 		},
// 		{"noise LSF",
// 			fields{},
// 			args{
// 				puncturedSoftBits: []Symbol{0.18899825, 0, 0, 0, 0, 0.041702908, 0, 0.48090333, 0.49155414, 0.26399633, 0.060273137, 0.3825742, 0, 0, 0, 0.2156405, 0, 0, 0.05397116, 0.09822232, 0.3081712, 0.01713588, 0.088136114, 0, 0, 0.1824979, 0, 0, 0.07754635, 0, 0, 0, 0.25326926, 0, 0.41664758, 0.8652781, 0.80781263, 0.013931487, 0.36023498, 0.5455711, 0.95384145, 1, 0.011062791, 0.24281682, 1, 1, 0, 1, 0, 1, 1, 0.6768902, 0, 0, 1, 0.25169757, 0, 0, 0.7934539, 0.6802777, 1, 0.24115358, 1, 0, 0.9018387, 1, 0, 1, 0, 0.18927291, 0, 1, 0.178439, 0, 0, 1, 0.8191806, 0.23900206, 0, 0.4700847, 0, 0.30045015, 0, 0.2307927, 0, 0.01466392, 0, 0.14299229, 0.17180133, 0, 0, 0, 0, 0.17033646, 0.3422751, 0.022812238, 0, 0.386038, 0.80430305, 1, 1, 0, 1, 0.6504006, 0.893019, 0, 0, 0.32870984, 0, 0.7270161, 1, 0.8386511, 0.9819028, 0, 0, 0.81944, 0, 1, 1, 0, 1, 1, 0.34398413, 1, 0.6341039, 0.8039826, 0.7840391, 0, 1, 0, 0, 0, 0, 0.733608, 0, 0.753933, 0, 1, 1, 0.28848708, 1, 0.6716411, 1, 0.11216907, 0, 1, 0, 1, 0.75826657, 0, 0, 0.12977798, 0, 0, 0, 0, 0, 0, 0, 0, 0.4435645, 0.31709772, 0, 0, 0, 0, 1, 0.18399328, 0.8037385, 0, 1, 0, 1, 0, 0.49912262, 0, 0, 0, 0, 0, 0, 0.45215535, 0.34639505, 0.20297551, 0, 0, 0, 0, 0.22049287, 0, 0.30666056, 0.026108187, 0, 0.17009231, 0.053131916, 0, 0.08242924, 0.40546274, 0.0146791795, 0, 0.25593957, 0, 0, 0.22975509, 0, 0, 0.4893721, 0.13344015, 0, 0, 0, 0.08596933, 0, 0, 0.39003587, 0.29475853, 0, 0.22078279, 0, 0, 0, 0.49939728, 0, 0.39835203, 0, 0, 0, 0, 0.22578774, 0.49910736, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.48323795, 0, 0, 0, 0.47791258, 0, 0.049637597, 0.3765774, 0.21785305, 0, 0, 0.25294882, 0, 0.03930724, 0, 0, 0, 0.27336538, 0, 0, 0, 0, 0, 0, 0.44753185, 0.13051042, 0, 0, 0, 0.130251, 0.036682688, 0.2538796, 0.28644237, 0, 0.053910125, 0, 0.15619135, 0, 0, 0, 0.41826504, 0.3815213, 0.19984742, 0, 0.105638206, 0, 0.36925307, 0, 0, 0.28447396, 0, 0.48868543, 0, 0, 0.0767071, 0.044251163, 0.08226138, 0.04396124, 0, 0, 0.44475472, 0, 0.03743038, 0, 0, 0, 0.28059816, 0, 0.17622644, 0, 0, 0, 0, 0.2134432, 0, 0.111177236, 0.4217441, 0, 0, 0, 0, 0, 0.101014726, 0, 0, 0.2625925, 0, 0, 0, 0.30074006, 0, 1, 0.06915389, 1, 0.33923858, 1, 0, 0.059967957, 1, 0, 0.02854963, 0.169543, 0.19403373, 0, 0, 0.60343325, 0, 0.77265584, 1, 0, 1, 0.77137405, 1, 0.35127795, 0.11015488, 0, 1, 0, 1, 1, 0.5651942},
// 				puncturePattern:   LSFPuncturePattern,
// 			},
// 			[]byte{0x0, 0x0, 0x0, 0x1, 0x8a, 0x92, 0xae, 0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8d, 0x6d},
// 			32.7,
// 		},
// 		{"nonoise packet",
// 			fields{},
// 			args{
// 				puncturedSoftBits: []Symbol{0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 1, 1, 0, 1, 1, 0.99998474, 1, 0, 1, 0.99998474, 1, 0.99998474, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1, 1, 0.99998474, 0, 0, 1, 0.99998474, 1, 1.5259022e-05, 1, 0, 1, 1, 1, 1.5259022e-05, 1, 0, 0, 0.99998474, 0, 0, 1, 1, 1, 1, 1, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1.5259022e-05, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0, 1.5259022e-05, 1, 1.5259022e-05, 0, 1, 0, 0.99998474, 0, 0.99998474, 1, 0, 1, 1, 0, 0, 0, 0.99998474, 1, 0.99998474, 0, 1.5259022e-05, 1, 1, 1, 0, 0, 1, 0, 0.99998474, 1, 1, 0, 1.5259022e-05, 1, 1, 1, 0, 0, 1.5259022e-05, 1, 0, 1, 1.5259022e-05, 1, 0, 1, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 0.99998474, 1, 0.99998474, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0.99998474, 0, 0, 1, 0.99998474, 0, 1, 1, 0, 1, 0.99998474, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 1, 0.99998474, 1, 1, 0, 0, 0, 0, 1, 0, 0, 0.99998474, 1, 1, 1, 0, 1, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1.5259022e-05, 1, 1, 1, 0, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 0.99998474, 0, 0, 0, 0, 1, 0, 1, 0.99998474, 1, 1, 1, 0, 0, 1, 1, 0.99998474, 0, 0, 0, 0, 0, 1, 1, 0.99998474, 0, 1, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0.99998474, 1, 1, 0, 0, 0, 1, 1, 0, 1, 0, 0, 0.99998474, 1, 1, 1, 0, 0},
// 				puncturePattern:   PacketPuncturePattern,
// 			},
// 			[]byte{0x0, 0x5, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d, 0x65, 0x21, 0x0, 0xbb, 0x6a, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc8},
// 			0.0,
// 		},
// 		{"noise packet",
// 			fields{},
// 			args{
// 				puncturedSoftBits: []Symbol{0, 0.31305408, 0, 0.33408102, 0, 0, 0.17094682, 0, 0, 1, 1, 0, 0.87530327, 1, 1, 0.8719005, 0, 0.9471427, 0.9222095, 1, 0.900618, 0, 1, 1, 0.39502555, 0.1486839, 0.65030897, 0.81832606, 1, 1, 1, 1, 1, 0.46440834, 0, 0.7308766, 1, 1, 0.44917983, 0.9815213, 0, 1, 1, 0.7305562, 0, 1, 0.34024566, 0, 1, 0, 0, 1, 0.75152206, 0.96774244, 1, 1, 0, 1, 0, 0, 0.8569314, 0, 0, 0, 0, 0.8043641, 1, 1, 1, 1, 0, 1, 0, 0.05349813, 1, 0, 0, 0.4140383, 0, 0.7404288, 1, 0.4494087, 0, 0.048157472, 0.17935455, 1, 0, 0, 1, 0.42935836, 1, 0.39790952, 1, 0.75590146, 0.41965362, 1, 0.9459373, 0.020965897, 0, 0, 0.95759517, 1, 0.6637827, 0.0767834, 0, 0.8138857, 1, 0.5459831, 0, 0, 1, 0, 1, 1, 1, 0, 0, 1, 1, 0.66009, 0, 0.1142443, 0, 1, 0.042832073, 0.9999542, 0.1655146, 0.75495535, 0, 1, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0.47064927, 0, 1, 1, 1, 0.8469673, 0.76366824, 1, 0.48688486, 0.49065384, 1, 1, 0.56536204, 1, 0, 0.1727779, 0, 0.6840467, 0, 0, 0.5536889, 1, 0, 1, 1, 0, 1, 1, 0, 0, 0.41690698, 1, 1, 1, 0.2363775, 0, 0, 0, 1, 0.5602655, 1, 1, 0.25540552, 0.010345617, 0, 0, 0.88906693, 0, 0.2758831, 1, 1, 0.5016556, 0.7336843, 0, 1, 0, 0.8101625, 0.7437552, 1, 0.8311742, 0, 0.41748685, 1, 1, 0.21971466, 1, 0.89239335, 0.33369955, 0.53636986, 0.50608075, 1, 0, 1, 0, 0.52663463, 1, 0.24924086, 1, 0.4296788, 0, 0.044037536, 0, 0, 0, 0.24983597, 1, 0.87251085, 0, 0.5063554, 0.89437705, 0.12243839, 0, 0, 0, 1, 0, 0.8828107, 0.663386, 1, 0.6037995, 1, 0, 0.057206072, 1, 0.63636225, 0.7864347, 0.18117037, 0, 0, 0, 0.05836576, 0.7216602, 0.71638054, 0.65841156, 0.21405356, 1, 0, 1, 0.9064622, 0, 0.07841612, 0, 0, 0.28416875, 0, 0, 0, 0, 0.04493782, 0, 0, 0, 0.26060882, 0.076188296, 0.45236897, 0, 0, 0.29465172, 0, 0, 0.07565423, 0.3562066, 0, 0.47336537, 0.25825894, 0, 0, 0, 0.3381094, 0, 0, 0, 0, 0.27196154, 0, 0, 0.37407494, 0, 0.2694133, 0.32402533, 0.3948272, 0.003418021, 0.15904479, 0.2102388, 0.3117113, 0, 0.17189288, 0.29044023, 0, 0, 0, 0, 0.036255438, 0, 0.14035249, 0, 0, 0, 0.08479439, 0, 0, 0, 0.0057679103, 0, 0, 0, 0.41898224, 0.19980164, 0, 0, 0, 0.24098574, 0.47718012, 0, 0, 0, 0, 0, 0, 0, 0.01918059, 0, 0.30200657, 0, 0, 0, 0, 0, 0.22188143, 0.2567788, 0, 1, 1, 1, 0, 0.37749293, 0.12103456, 1, 1, 0, 0.7970092, 0, 0.19485772, 0.76102847, 0.837995, 1, 1, 0.3286183, 0},
// 				puncturePattern:   PacketPuncturePattern,
// 			},
// 			[]byte{0x0, 0x5, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d, 0x65, 0x21, 0x0, 0xbb, 0x6a, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc8},
// 			33.2,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			v := &ViterbiDecoder{
// 				history:         tt.fields.history,
// 				prevMetrics:     tt.fields.prevMetrics,
// 				currMetrics:     tt.fields.currMetrics,
// 				prevMetricsData: tt.fields.prevMetricsData,
// 				currMetricsData: tt.fields.currMetricsData,
// 			}
// 			got, got1 := v.DecodePunctured(tt.args.puncturedSoftBits, tt.args.puncturePattern)
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("ViterbiDecoder.DecodePunctured() got = %v, want %v", got, tt.want)
// 			}
// 			if math.Abs(got1-tt.want1) > 0.1 {
// 				t.Errorf("ViterbiDecoder.DecodePunctured() got1 = %v, want %v", got1, tt.want1)
// 			}
// 		})
// 	}
// }

func TestViterbiDecoder_DecodePunctured(t *testing.T) {
	type fields struct {
		history         []uint16
		prevMetrics     []uint32
		currMetrics     []uint32
		prevMetricsData []uint32
		currMetricsData []uint32
	}
	type args struct {
		puncturedSoftBits []SoftBit
		puncturePattern   PuncturePattern
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []byte
		want1  float64
	}{
		{"LSF",
			fields{},
			args{
				puncturedSoftBits: []SoftBit{0x0, 0xb25, 0x0, 0x0, 0x0, 0x4071, 0x0, 0x0, 0x0, 0x3162, 0xf4f, 0x0, 0x0, 0x0, 0x0, 0x33c3, 0x0, 0x919, 0x0, 0x3273, 0x0, 0x0, 0x61e0, 0x0, 0x0, 0x44e5, 0xffff, 0xe510, 0x0, 0xe32a, 0x2a02, 0xef8b, 0x0, 0x0, 0xe76e, 0x0, 0x0, 0xffff, 0xee6, 0xc463, 0x1503, 0x28d7, 0x0, 0x0, 0xffff, 0xcd93, 0xf9a4, 0x4471, 0xffff, 0xffff, 0x0, 0x0, 0xffff, 0xffff, 0x0, 0xffff, 0xffff, 0x107e, 0x3d9, 0xffff, 0x0, 0xcbfb, 0xffff, 0xffff, 0x0, 0xa61, 0x0, 0x43a9, 0xffff, 0xeaf, 0x0, 0xa21, 0xffff, 0xffff, 0x1751, 0x0, 0x2773, 0x3e55, 0x1dc8, 0x0, 0x0, 0x184a, 0x0, 0x0, 0x741, 0x38fa, 0x0, 0x69d7, 0x1820, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2e72, 0x0, 0x1ba1, 0x41f8, 0x0, 0x0, 0x0, 0x0, 0x57d3, 0x548f, 0x0, 0x0, 0xffff, 0xffff, 0xc2, 0x0, 0x3d8a, 0xffff, 0x39, 0xffff, 0x0, 0x6818, 0xffff, 0x0, 0xffff, 0xffff, 0xffff, 0xffff, 0xfd86, 0x435f, 0x1786, 0xd611, 0x2b80, 0x0, 0xffff, 0xffff, 0xcee0, 0x0, 0x0, 0xe01f, 0xf66e, 0xffff, 0xca8b, 0xffff, 0x3465, 0x0, 0x1d6a, 0x0, 0xff8e, 0xd12, 0x5831, 0x0, 0xffff, 0xd2bc, 0x0, 0x0, 0x401a, 0x0, 0x0, 0x0, 0x3a82, 0x39c9, 0x2977, 0x0, 0x0, 0x3e65, 0x3cc9, 0x0, 0x4b1f, 0xe46b, 0xc2a0, 0x0, 0xffff, 0x0, 0xffff, 0xffff, 0x0, 0xffff, 0xe1f8, 0xf1a6, 0x0, 0x0, 0x0, 0x116e, 0x1b5e, 0x0, 0x2394, 0x0, 0x0, 0x0, 0x0, 0x0, 0xbb7, 0x0, 0x12e7, 0x5334, 0x100d, 0x0, 0x33e3, 0x0, 0x0, 0x0, 0x487, 0x2a27, 0x159a, 0x0, 0x1144, 0x0, 0x2a08, 0x0, 0x267e, 0x0, 0x3d57, 0x0, 0x8b7, 0x0, 0x0, 0xdf6, 0x0, 0x0, 0x0, 0x4da3, 0x2cd0, 0x19e5, 0x1004, 0x0, 0x0, 0x0, 0x3149, 0x0, 0x0, 0x0, 0x1afe, 0x0, 0x3bef, 0x0, 0x274, 0x0, 0x2f06, 0x0, 0x0, 0x0, 0x0, 0x4db, 0x53d3, 0x0, 0x0, 0x0, 0x5fa0, 0x0, 0xdea, 0x0, 0xa45, 0x0, 0x159d, 0x0, 0x18e0, 0x22e8, 0x0, 0x1b21, 0x13b7, 0x0, 0x3b5d, 0x0, 0x41f9, 0x0, 0x0, 0x0, 0x0, 0x38e, 0x1633, 0x3f11, 0x4900, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2de9, 0x2a0c, 0x1d16, 0x0, 0x14f5, 0x0, 0x22ee, 0x0, 0x0, 0x0, 0xd2c, 0x1697, 0x0, 0x0, 0x4f76, 0x0, 0x3254, 0x182e, 0x558, 0x0, 0x0, 0x31df, 0x0, 0x0, 0x0, 0x0, 0xcc9, 0x0, 0x0, 0x0, 0x4c5, 0x0, 0x2b36, 0x5858, 0x0, 0x0, 0x280c, 0x679, 0x0, 0x0, 0xa87, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1497, 0xeac, 0x0, 0x497d, 0x3f08, 0x0, 0x31e3, 0x0, 0x0, 0x0, 0x4c9b, 0x353, 0x2e3, 0x5b86, 0x3dc7, 0x0, 0x0, 0x2a50, 0x185, 0x372, 0xf317, 0xffff, 0xd621, 0x0, 0x197b, 0x0, 0xffff, 0xffff, 0xd9f1, 0xc515, 0xffff, 0xaae7, 0xffff, 0x4f4b, 0xffff, 0x0, 0x0, 0x0, 0xfdac, 0x0, 0xffff, 0x0, 0x3317, 0xffff, 0xad85, 0x0, 0xe1d6, 0x0, 0x1a6f},
				puncturePattern:   LSFPuncturePattern,
			},
			[]byte{0x0, 0x0, 0x0, 0x7c, 0x6d, 0xf4, 0xb8, 0x0, 0x0, 0x1, 0x8a, 0x92, 0xae, 0x0, 0x5, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x6c, 0xba},
			72.8,
		},
		{"Stream",
			fields{},
			args{
				puncturedSoftBits: []SoftBit{0x23b1, 0x3226, 0x201c, 0x0, 0x0, 0x0, 0x0, 0x1d5f, 0x0, 0x4945, 0x0, 0x0, 0x621b, 0x17f0, 0x0, 0x6be, 0x0, 0x0, 0x0, 0x0, 0x0, 0x771, 0x0, 0x37ce, 0x0, 0x0, 0x0, 0x25d2, 0x0, 0x0, 0x0, 0x0, 0xffff, 0x3bc, 0xffff, 0x0, 0xffff, 0xfe9, 0xd6d1, 0xffff, 0x0, 0xffff, 0x0, 0x71f1, 0x0, 0x0, 0x0, 0xec14, 0x12a, 0xffff, 0x0, 0x0, 0x0, 0xffff, 0xffff, 0x0, 0x1a64, 0x0, 0x0, 0xffff, 0xffff, 0x88d, 0xffff, 0x2b36, 0xffff, 0xfb97, 0x301e, 0x7620, 0xffff, 0x4540, 0xffff, 0xe84a, 0x0, 0xf8c2, 0xedf1, 0x0, 0xffff, 0x0, 0xffff, 0x4ed0, 0x0, 0x0, 0xffff, 0xcf91, 0xffff, 0xe1de, 0xffff, 0x875b, 0xf61c, 0xffff, 0x0, 0xffff, 0x0, 0xf5ac, 0xffff, 0xf10f, 0x0, 0x706, 0x0, 0xb926, 0xffff, 0xffff, 0x0, 0x27ed, 0xb232, 0x3f14, 0x9f04, 0xea5b, 0xc391, 0x7f6e, 0xffff, 0xade, 0xffff, 0x47c6, 0x6c73, 0x853d, 0x1f85, 0x0, 0x0, 0xffff, 0xffff, 0xfdf5, 0xcb1d, 0x617e, 0xd1b8, 0x0, 0xffff, 0x0, 0xb5cd, 0xc06a, 0xe41, 0xffff, 0xffff, 0x9889, 0xffff, 0x0, 0x0, 0xffff, 0xffff, 0x0, 0xfe30, 0x41, 0xffff, 0xeb7d, 0x0, 0xb424, 0x0, 0x1d8b, 0x0, 0xffff, 0x2715, 0x0, 0x0, 0xffff, 0x0, 0xb1ce, 0xffff, 0xff81, 0x1d92, 0x2db9, 0x0, 0x0, 0x2c59, 0x1c36, 0xffff, 0x30a2, 0xf7c9, 0xed37, 0x0, 0x0, 0x378f, 0x42f5, 0x0, 0xdf6c, 0x0, 0x0, 0x0, 0xf0c7, 0x1c66, 0x132d, 0xffff, 0x4ed5, 0x0, 0x423d, 0x0, 0x0, 0xffff, 0x5ad7, 0x6391, 0xc7ff, 0x0, 0xfe45, 0xffff, 0xffff, 0xf467, 0xf777, 0xf9d8, 0xbf6a, 0x0, 0xe35a, 0x9160, 0x7cc1, 0xa0e2, 0xffff, 0xffff, 0x20d8, 0xfb04, 0x9f7d, 0xffff, 0x3557, 0x0, 0xffff, 0x0, 0x1294, 0xcebb, 0xec5e, 0xffff, 0xba76, 0x0, 0x16af, 0xffff, 0x1692, 0x0, 0xffff, 0xffff, 0xffff, 0xffff, 0x718, 0x0, 0x24af, 0xcf65, 0xe43a, 0x0, 0x36f3, 0xc5a3, 0xb6e3, 0x730f, 0xb0d0, 0xffff, 0xea0c, 0xffff, 0x0, 0x0, 0xffff, 0xffff, 0x0, 0xffff, 0xe3c1, 0xe730, 0xdba5, 0x0, 0xffff, 0x0, 0xffff, 0x0, 0xb73f, 0xffff, 0xe9d4, 0xffff, 0x0, 0xff73, 0x0, 0x9af6, 0xcd0f, 0xffff, 0xffff, 0x0, 0x0, 0x0, 0x405d, 0x2961, 0x3275},
				puncturePattern:   StreamPuncturePattern,
			},
			[]byte{0x0, 0x0, 0x0, 0x4b, 0xc0, 0x8f, 0xc3, 0xd4, 0xfc, 0x25, 0x28, 0xc0, 0x5c, 0x6e, 0xc3, 0x9c, 0xe4, 0x21, 0x8},
			46.2,
		},
		{"Packet",
			fields{},
			args{
				puncturedSoftBits: []SoftBit{0x3016, 0x3a11, 0x0, 0x0, 0x0, 0x0, 0x360, 0x0, 0x0, 0xce5d, 0xffff, 0x1986, 0xe649, 0xe955, 0xffff, 0xffff, 0x0, 0xffff, 0x0, 0x5088, 0xc9fc, 0xe21e, 0xffff, 0x0, 0xcdda, 0xffff, 0xffff, 0xf91b, 0x0, 0xc65b, 0xffff, 0x3bfe, 0x0, 0x0, 0xffff, 0xc4d1, 0xe28a, 0xe982, 0x0, 0xe260, 0x14b1, 0x225f, 0xe4df, 0x0, 0x3b1e, 0x383a, 0xd007, 0x2f23, 0x0, 0xa6b2, 0xffff, 0xffff, 0x0, 0x2685, 0xe8bf, 0xdeb8, 0xffff, 0x8dc, 0xffff, 0xfe63, 0xffff, 0xd631, 0xffff, 0x0, 0x4bc, 0x30a, 0x0, 0x16ed, 0xffff, 0xffff, 0xffff, 0x14cf, 0x40b7, 0xcef5, 0x0, 0xffff, 0x27a, 0x1644, 0x0, 0xffff, 0xffff, 0xf7fd, 0xffff, 0x0, 0x2b51, 0x1f15, 0xdd3, 0xffff, 0x0, 0x0, 0x0, 0xe022, 0x0, 0x0, 0x0, 0x3db0, 0xffff, 0xffff, 0x136b, 0x0, 0xffff, 0xab0c, 0xffff, 0x1f1b, 0xffff, 0xd272, 0x0, 0xffff, 0xffff, 0x42b4, 0x0, 0xffff, 0xb98d, 0x0, 0xffff, 0xe3e0, 0x3467, 0x297c, 0x0, 0x516, 0x0, 0x0, 0x0, 0x77c, 0x41c, 0x0, 0x0, 0x0, 0x0, 0xa6a, 0xffff, 0xf901, 0x0, 0xf97b, 0x0, 0xcacc, 0xffff, 0xffff, 0x0, 0x0, 0x0, 0xc810, 0xffff, 0xffff, 0xdd17, 0x48e7, 0xffff, 0xbf, 0x3fb, 0xffff, 0x0, 0xfd8a, 0x0, 0xf32b, 0x0, 0x3cdc, 0x1ad, 0x0, 0x0, 0x21c6, 0x0, 0x0, 0x0, 0x43f, 0x0, 0x42f, 0x0, 0x30c4, 0x0, 0x1644, 0x0, 0xba5, 0x0, 0x0, 0x0, 0x1300, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xf05, 0x0, 0x0, 0x1c37, 0x0, 0x0, 0x1e39, 0x0, 0xe24, 0x15a, 0x1dce, 0x1973, 0x39f8, 0x0, 0x0, 0x0, 0x30ac, 0x2831, 0x0, 0x13f6, 0x0, 0x0, 0x0, 0x0, 0x1be6, 0x0, 0x0, 0x0, 0x24bf, 0x0, 0x1082, 0x143a, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2e04, 0x0, 0x0, 0x50a, 0x3e21, 0x0, 0x245d, 0x0, 0x0, 0x0, 0x0, 0xb36, 0x1716, 0x1f1c, 0x164c, 0x0, 0x1c36, 0x0, 0xfa9, 0x0, 0x0, 0x0, 0xf76, 0x5ba, 0x0, 0x0, 0x243, 0x0, 0x0, 0x2a4c, 0x157e, 0x2ad1, 0x288f, 0x0, 0x5c1, 0x0, 0x0, 0x0, 0x64f, 0x12b4, 0x2978, 0x0, 0x1c5f, 0x0, 0x0, 0x0, 0x87d, 0x0, 0x2b01, 0x2328, 0x0, 0x0, 0x4b36, 0x0, 0x2f97, 0x0, 0x0, 0x6e8, 0x338d, 0x0, 0x0, 0x0, 0x1803, 0x0, 0x210f, 0x0, 0x1e3, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1451, 0x0, 0x24fa, 0xc20, 0x887, 0x0, 0x774, 0x0, 0x11a3, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1138, 0x0, 0x22e5, 0x0, 0x3eb, 0x0, 0x0, 0x375, 0x19cf, 0xcae, 0x0, 0x0, 0x0, 0x0, 0x718, 0x0, 0x0, 0xc93, 0x532, 0x4b7a, 0x1818, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1671, 0x1b16, 0x164e, 0x0, 0x0, 0x2994, 0x0, 0x132e, 0xb4f, 0x0, 0x555b, 0x0, 0x28cd, 0xffff, 0xf2b6, 0x0, 0xf076, 0xffff, 0x0, 0xf39f, 0x335, 0xe55f, 0x28e0, 0x0, 0xd19b, 0xffff, 0xea7a, 0x0, 0xffff, 0xffff, 0xe61e},
				puncturePattern:   PacketPuncturePattern,
			},
			[]byte{0x0, 0x5, 0x61, 0x64, 0x67, 0x6a, 0x6d, 0x70, 0x74, 0x0, 0x29, 0xd0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xac},
			62.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &ViterbiDecoder{
				history:         tt.fields.history,
				prevMetrics:     tt.fields.prevMetrics,
				currMetrics:     tt.fields.currMetrics,
				prevMetricsData: tt.fields.prevMetricsData,
				currMetricsData: tt.fields.currMetricsData,
			}
			got, got1 := v.DecodePunctured(tt.args.puncturedSoftBits, tt.args.puncturePattern)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ViterbiDecoder.DecodePunctured() got = %v, want %v", got, tt.want)
			}
			if math.Abs(float64(got1-tt.want1)) > 0.1 {
				t.Errorf("ViterbiDecoder.DecodePunctured() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
