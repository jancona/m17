package m17

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDCFilter(t *testing.T) {
	type newparams struct {
		sink     chan int8
		averageN int
	}
	tests := []struct {
		name      string
		newparams newparams
		in        []int8
		want      []int8
	}{
		{"simple",
			newparams{
				make(chan int8),
				2,
			},
			[]int8{1, -1, 0, 0, -1, 1, 0, 1, -1, 0},
			[]int8{1, -1, 0, 0, -1, 1, 0, 1, -1, 0},
		},
		{"offset",
			newparams{
				make(chan int8),
				3,
			},
			[]int8{20, 0, 10, 10, 0, 20, 10, 20, 0, 10},
			[]int8{14, -4, 4, 3, -4, 11, 1, 8, -8, 2},
		},
		{"empty",
			newparams{
				make(chan int8),
				1,
			},
			[]int8{},
			[]int8{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewDCFilter(tt.newparams.sink, tt.newparams.averageN)
			if err != nil {
				t.Error(err)
				t.FailNow()
			}
			go func() {
				for _, i := range tt.in {
					tt.newparams.sink <- i
				}
				close(tt.newparams.sink)
			}()
			got := []int8{}
			for r := range f.Source() {
				got = append(got, r)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DCFilter got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSampleToSymbol(t *testing.T) {
	type newparams struct {
		sink         chan int8
		rrcTaps      []float32
		scalingCoeff float32
	}
	tests := []struct {
		name      string
		newparams newparams
		warmup    []int8
		in        []int8
		want      []float32
	}{
		{"simple",
			newparams{
				make(chan int8),
				rrcTaps5,
				RXSymbolScalingCoeff,
			},
			[]int8{
				42, 44, 49, 57, 61, 56, 38, 9, -23, -49,
				-59, -49, -23, 9, 38, 56, 61, 57, 49, 44,
				42, 44, 47, 48, 47, 46, 45, 45, 46, 47,
				48, 47, 46, 45, 45, 46, 47, 48, 47, 44,
				42},
			[]int8{
				44, 49, 57, 61, 56, 38, 9, -23, -49, -59,
				-49, -23, 9, 38, 56, 61, 57, 49, 44, 42,
				44, 47, 48, 47, 46, 45, 45, 46, 47, 48,
				47, 46, 45, 45, 46, 47, 48, 47, 44, 42,
				44},
			[]float32{
				// 4.2, 4.0, 3.3, 2.0, 0.2, -1.5, -2.8, -3.3, -2.8, -1.5, 0.2, 2.0, 3.3, 4.0, 4.2, 3.9, 3.6, 3.3,
				3.2, 3.2, 3.2, 3.3, 3.3, 3.3, 3.3, 3.3, 3.3, 3.3,
				3.3, 3.3, 3.3, 3.3, 3.3, 3.3, 3.2, 3.2, 3.2, 3.3,
				3.6, 3.9, 4.2, 4.0, 3.3, 2.0, 0.2, -1.5, -2.8, -3.3,
				-2.8, -1.5, 0.2, 2.0, 3.3, 4.0, 4.2, 3.9, 3.6, 3.3,
				3.2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewSampleToSymbol(tt.newparams.sink, tt.newparams.rrcTaps, tt.newparams.scalingCoeff)
			go func() {
				for _, i := range tt.warmup {
					tt.newparams.sink <- i
				}
				for _, i := range tt.in {
					tt.newparams.sink <- i
				}
				close(tt.newparams.sink)
			}()
			got := []float32{}
			// discard results from warmup
			for range len(tt.warmup) {
				<-f.Source()
			}

			for r := range f.Source() {
				got = append(got, r)
			}
			if len(got) != len(tt.want) {
				t.Errorf("SampleToSymbol len(got): %d, len(tt.want): %d", len(got), len(tt.want))
				t.FailNow()
			}
			for i := range got {
				if fmt.Sprintf("%.1f", got[i]) != fmt.Sprintf("%.1f", tt.want[i]) {
					t.Errorf("SampleToSymbol got[%d] %v, want %v", i, fmt.Sprintf("%.1f", got[i]), fmt.Sprintf("%.1f", tt.want[i]))
				}
			}
		})
	}
}
func TestDownsampler(t *testing.T) {
	type newparams struct {
		sink   chan int8
		factor int
		offset int
	}
	tests := []struct {
		name      string
		newparams newparams
		in        []int8
		want      []int8
	}{
		{"simple",
			newparams{
				make(chan int8),
				4,
				0,
			},
			[]int8{1, 2, 3, 4, 5, 6, 7, 8, 9},
			[]int8{1, 5, 9},
		},
		{"offset",
			newparams{
				make(chan int8),
				4,
				3,
			},
			[]int8{1, 2, 3, 4, 5, 6, 7, 8, 9},
			[]int8{4, 8},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewDownsampler(tt.newparams.sink, tt.newparams.factor, tt.newparams.offset)
			if err != nil {
				t.Error(err)
				t.FailNow()
			}
			go func() {
				for _, i := range tt.in {
					tt.newparams.sink <- i
				}
				close(tt.newparams.sink)
			}()
			got := []int8{}
			for r := range f.Source() {
				got = append(got, r)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Downsampler got %v, want %v", got, tt.want)
			}
		})
	}
}
