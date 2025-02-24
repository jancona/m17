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

func TestViterbiDecoder_DecodePunctured(t *testing.T) {
	type fields struct {
		history         []uint16
		prevMetrics     []float64
		currMetrics     []float64
		prevMetricsData []float32
		currMetricsData []float32
	}
	type args struct {
		puncturedSoftBits []Symbol
		puncturePattern   PuncturePattern
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []byte
		want1  float64
	}{
		{"nonoise LSF",
			fields{},
			args{
				puncturedSoftBits: []Symbol{1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 1, 0.99998474, 0, 1.5259022e-05, 1, 0.99998474, 1, 0, 0, 1, 1, 0, 1, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0.99998474, 1, 1, 0, 1, 0, 1, 1, 0, 1, 0, 0, 0, 1, 0, 0, 1.5259022e-05, 1, 1, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.99998474, 1, 1, 0, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 0.99998474, 0, 1.5259022e-05, 1, 1.5259022e-05, 1, 1, 0, 1, 1, 1.5259022e-05, 1, 0.99998474, 1, 1, 0, 1, 0, 0, 0, 0, 1, 0, 1, 0, 1, 1, 0, 1, 1, 0.99998474, 0, 0, 1, 0, 1, 0.99998474, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.99998474, 0, 0.99998474, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1, 0, 0.99998474, 0, 1, 0, 0, 1, 0, 0, 1.5259022e-05, 0, 0, 0, 1, 0, 0.99998474, 1, 0, 1, 0.99998474, 1, 1.5259022e-05, 0, 0, 1, 1.5259022e-05, 1, 0.99998474, 1},
				puncturePattern:   LSFPuncturePattern,
			},
			[]byte{0x0, 0x0, 0x0, 0x1, 0x8a, 0x92, 0xae, 0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8d, 0x6d},
			0.0,
		},
		{"noise LSF",
			fields{},
			args{
				puncturedSoftBits: []Symbol{0.18899825, 0, 0, 0, 0, 0.041702908, 0, 0.48090333, 0.49155414, 0.26399633, 0.060273137, 0.3825742, 0, 0, 0, 0.2156405, 0, 0, 0.05397116, 0.09822232, 0.3081712, 0.01713588, 0.088136114, 0, 0, 0.1824979, 0, 0, 0.07754635, 0, 0, 0, 0.25326926, 0, 0.41664758, 0.8652781, 0.80781263, 0.013931487, 0.36023498, 0.5455711, 0.95384145, 1, 0.011062791, 0.24281682, 1, 1, 0, 1, 0, 1, 1, 0.6768902, 0, 0, 1, 0.25169757, 0, 0, 0.7934539, 0.6802777, 1, 0.24115358, 1, 0, 0.9018387, 1, 0, 1, 0, 0.18927291, 0, 1, 0.178439, 0, 0, 1, 0.8191806, 0.23900206, 0, 0.4700847, 0, 0.30045015, 0, 0.2307927, 0, 0.01466392, 0, 0.14299229, 0.17180133, 0, 0, 0, 0, 0.17033646, 0.3422751, 0.022812238, 0, 0.386038, 0.80430305, 1, 1, 0, 1, 0.6504006, 0.893019, 0, 0, 0.32870984, 0, 0.7270161, 1, 0.8386511, 0.9819028, 0, 0, 0.81944, 0, 1, 1, 0, 1, 1, 0.34398413, 1, 0.6341039, 0.8039826, 0.7840391, 0, 1, 0, 0, 0, 0, 0.733608, 0, 0.753933, 0, 1, 1, 0.28848708, 1, 0.6716411, 1, 0.11216907, 0, 1, 0, 1, 0.75826657, 0, 0, 0.12977798, 0, 0, 0, 0, 0, 0, 0, 0, 0.4435645, 0.31709772, 0, 0, 0, 0, 1, 0.18399328, 0.8037385, 0, 1, 0, 1, 0, 0.49912262, 0, 0, 0, 0, 0, 0, 0.45215535, 0.34639505, 0.20297551, 0, 0, 0, 0, 0.22049287, 0, 0.30666056, 0.026108187, 0, 0.17009231, 0.053131916, 0, 0.08242924, 0.40546274, 0.0146791795, 0, 0.25593957, 0, 0, 0.22975509, 0, 0, 0.4893721, 0.13344015, 0, 0, 0, 0.08596933, 0, 0, 0.39003587, 0.29475853, 0, 0.22078279, 0, 0, 0, 0.49939728, 0, 0.39835203, 0, 0, 0, 0, 0.22578774, 0.49910736, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.48323795, 0, 0, 0, 0.47791258, 0, 0.049637597, 0.3765774, 0.21785305, 0, 0, 0.25294882, 0, 0.03930724, 0, 0, 0, 0.27336538, 0, 0, 0, 0, 0, 0, 0.44753185, 0.13051042, 0, 0, 0, 0.130251, 0.036682688, 0.2538796, 0.28644237, 0, 0.053910125, 0, 0.15619135, 0, 0, 0, 0.41826504, 0.3815213, 0.19984742, 0, 0.105638206, 0, 0.36925307, 0, 0, 0.28447396, 0, 0.48868543, 0, 0, 0.0767071, 0.044251163, 0.08226138, 0.04396124, 0, 0, 0.44475472, 0, 0.03743038, 0, 0, 0, 0.28059816, 0, 0.17622644, 0, 0, 0, 0, 0.2134432, 0, 0.111177236, 0.4217441, 0, 0, 0, 0, 0, 0.101014726, 0, 0, 0.2625925, 0, 0, 0, 0.30074006, 0, 1, 0.06915389, 1, 0.33923858, 1, 0, 0.059967957, 1, 0, 0.02854963, 0.169543, 0.19403373, 0, 0, 0.60343325, 0, 0.77265584, 1, 0, 1, 0.77137405, 1, 0.35127795, 0.11015488, 0, 1, 0, 1, 1, 0.5651942},
				puncturePattern:   LSFPuncturePattern,
			},
			[]byte{0x0, 0x0, 0x0, 0x1, 0x8a, 0x92, 0xae, 0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8d, 0x6d},
			32.7,
		},
		{"nonoise packet",
			fields{},
			args{
				puncturedSoftBits: []Symbol{0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 1, 1, 0, 1, 1, 0.99998474, 1, 0, 1, 0.99998474, 1, 0.99998474, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1, 1, 0.99998474, 0, 0, 1, 0.99998474, 1, 1.5259022e-05, 1, 0, 1, 1, 1, 1.5259022e-05, 1, 0, 0, 0.99998474, 0, 0, 1, 1, 1, 1, 1, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1.5259022e-05, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0, 1.5259022e-05, 1, 1.5259022e-05, 0, 1, 0, 0.99998474, 0, 0.99998474, 1, 0, 1, 1, 0, 0, 0, 0.99998474, 1, 0.99998474, 0, 1.5259022e-05, 1, 1, 1, 0, 0, 1, 0, 0.99998474, 1, 1, 0, 1.5259022e-05, 1, 1, 1, 0, 0, 1.5259022e-05, 1, 0, 1, 1.5259022e-05, 1, 0, 1, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 0.99998474, 1, 0.99998474, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0.99998474, 0, 0, 1, 0.99998474, 0, 1, 1, 0, 1, 0.99998474, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 1, 0.99998474, 1, 1, 0, 0, 0, 0, 1, 0, 0, 0.99998474, 1, 1, 1, 0, 1, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1.5259022e-05, 1, 1, 1, 0, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 0.99998474, 0, 0, 0, 0, 1, 0, 1, 0.99998474, 1, 1, 1, 0, 0, 1, 1, 0.99998474, 0, 0, 0, 0, 0, 1, 1, 0.99998474, 0, 1, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1.5259022e-05, 0, 0.99998474, 1, 1, 0, 0, 0, 1, 1, 0, 1, 0, 0, 0.99998474, 1, 1, 1, 0, 0},
				puncturePattern:   PacketPuncturePattern,
			},
			[]byte{0x0, 0x5, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d, 0x65, 0x21, 0x0, 0xbb, 0x6a, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc8},
			0.0,
		},
		{"noise packet",
			fields{},
			args{
				puncturedSoftBits: []Symbol{0, 0.31305408, 0, 0.33408102, 0, 0, 0.17094682, 0, 0, 1, 1, 0, 0.87530327, 1, 1, 0.8719005, 0, 0.9471427, 0.9222095, 1, 0.900618, 0, 1, 1, 0.39502555, 0.1486839, 0.65030897, 0.81832606, 1, 1, 1, 1, 1, 0.46440834, 0, 0.7308766, 1, 1, 0.44917983, 0.9815213, 0, 1, 1, 0.7305562, 0, 1, 0.34024566, 0, 1, 0, 0, 1, 0.75152206, 0.96774244, 1, 1, 0, 1, 0, 0, 0.8569314, 0, 0, 0, 0, 0.8043641, 1, 1, 1, 1, 0, 1, 0, 0.05349813, 1, 0, 0, 0.4140383, 0, 0.7404288, 1, 0.4494087, 0, 0.048157472, 0.17935455, 1, 0, 0, 1, 0.42935836, 1, 0.39790952, 1, 0.75590146, 0.41965362, 1, 0.9459373, 0.020965897, 0, 0, 0.95759517, 1, 0.6637827, 0.0767834, 0, 0.8138857, 1, 0.5459831, 0, 0, 1, 0, 1, 1, 1, 0, 0, 1, 1, 0.66009, 0, 0.1142443, 0, 1, 0.042832073, 0.9999542, 0.1655146, 0.75495535, 0, 1, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0.47064927, 0, 1, 1, 1, 0.8469673, 0.76366824, 1, 0.48688486, 0.49065384, 1, 1, 0.56536204, 1, 0, 0.1727779, 0, 0.6840467, 0, 0, 0.5536889, 1, 0, 1, 1, 0, 1, 1, 0, 0, 0.41690698, 1, 1, 1, 0.2363775, 0, 0, 0, 1, 0.5602655, 1, 1, 0.25540552, 0.010345617, 0, 0, 0.88906693, 0, 0.2758831, 1, 1, 0.5016556, 0.7336843, 0, 1, 0, 0.8101625, 0.7437552, 1, 0.8311742, 0, 0.41748685, 1, 1, 0.21971466, 1, 0.89239335, 0.33369955, 0.53636986, 0.50608075, 1, 0, 1, 0, 0.52663463, 1, 0.24924086, 1, 0.4296788, 0, 0.044037536, 0, 0, 0, 0.24983597, 1, 0.87251085, 0, 0.5063554, 0.89437705, 0.12243839, 0, 0, 0, 1, 0, 0.8828107, 0.663386, 1, 0.6037995, 1, 0, 0.057206072, 1, 0.63636225, 0.7864347, 0.18117037, 0, 0, 0, 0.05836576, 0.7216602, 0.71638054, 0.65841156, 0.21405356, 1, 0, 1, 0.9064622, 0, 0.07841612, 0, 0, 0.28416875, 0, 0, 0, 0, 0.04493782, 0, 0, 0, 0.26060882, 0.076188296, 0.45236897, 0, 0, 0.29465172, 0, 0, 0.07565423, 0.3562066, 0, 0.47336537, 0.25825894, 0, 0, 0, 0.3381094, 0, 0, 0, 0, 0.27196154, 0, 0, 0.37407494, 0, 0.2694133, 0.32402533, 0.3948272, 0.003418021, 0.15904479, 0.2102388, 0.3117113, 0, 0.17189288, 0.29044023, 0, 0, 0, 0, 0.036255438, 0, 0.14035249, 0, 0, 0, 0.08479439, 0, 0, 0, 0.0057679103, 0, 0, 0, 0.41898224, 0.19980164, 0, 0, 0, 0.24098574, 0.47718012, 0, 0, 0, 0, 0, 0, 0, 0.01918059, 0, 0.30200657, 0, 0, 0, 0, 0, 0.22188143, 0.2567788, 0, 1, 1, 1, 0, 0.37749293, 0.12103456, 1, 1, 0, 0.7970092, 0, 0.19485772, 0.76102847, 0.837995, 1, 1, 0.3286183, 0},
				puncturePattern:   PacketPuncturePattern,
			},
			[]byte{0x0, 0x5, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d, 0x65, 0x21, 0x0, 0xbb, 0x6a, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc8},
			33.2,
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
			if math.Abs(got1-tt.want1) > 0.1 {
				t.Errorf("ViterbiDecoder.DecodePunctured() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
