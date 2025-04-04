package m17

import (
	"reflect"
	"testing"
)

func TestEncodeCallsign(t *testing.T) {
	type args struct {
		callsign string
	}
	tests := []struct {
		name    string
		args    args
		want    *[6]byte
		wantErr bool
	}{
		{name: "N1ADJ",
			args:    args{callsign: "N1ADJ"},
			want:    &[6]byte{0, 0, 1, 138, 146, 174},
			wantErr: false,
		},
		{name: "N1ADJ R",
			args:    args{callsign: "N1ADJ R"},
			want:    &[6]byte{0, 17, 44, 18, 146, 174},
			wantErr: false,
		},
		{name: "n1adj",
			args:    args{callsign: "N1ADJ"},
			want:    &[6]byte{0, 0, 1, 138, 146, 174},
			wantErr: false,
		},
		{name: "too long",
			args:    args{callsign: "very long call"},
			wantErr: true,
		},
		{name: "Bad Char",
			args:    args{callsign: "N1ADJ*"},
			wantErr: true,
		},
		{name: "@all",
			args:    args{callsign: "@all"},
			want:    &[6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			wantErr: false,
		},
		{name: "#all",
			args:    args{callsign: "#ALL"},
			want:    &[6]byte{238, 107, 40, 0, 76, 225},
			wantErr: false,
		},
		{name: "#OTHER",
			args:    args{callsign: "#OTHER"},
			want:    &[6]byte{238, 107, 42, 196, 55, 47},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeCallsign(tt.args.callsign)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodeCallsign() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EncodeCallsign() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeCallsign(t *testing.T) {
	type args struct {
		encoded []byte
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{name: "too long",
			args: args{
				encoded: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			},
			wantErr: true,
		},
		{name: "N1ADJ",
			args: args{
				encoded: []byte{0, 0, 1, 138, 146, 174},
			},
			want:    "N1ADJ",
			wantErr: false,
		},
		{name: "@ALL",
			args: args{
				encoded: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			},
			want:    "@ALL",
			wantErr: false,
		},
		{name: "#all",
			args: args{
				encoded: []byte{238, 107, 40, 0, 76, 225},
			},
			want:    "#ALL",
			wantErr: false,
		},
		{name: "#OTHER",
			args: args{
				encoded: []byte{238, 107, 42, 196, 55, 47},
			},
			want: "#OTHER",
		},
		{name: "M17-TAE B",
			args: args{
				encoded: []byte{0xb, 0xf0, 0x90, 0x0, 0xba, 0xed},
			},
			want: "M17-TAE B",
		},
		{name: "N7TAE",
			args: args{
				encoded: []byte{0x47, 0x86, 0x8c, 0xc4, 0xcc, 0x5e},
			},
			want: "N7TAE   L",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeCallsign(tt.args.encoded)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeCallsign() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DecodeCallsign() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeCallsignModule(t *testing.T) {
	type args struct {
		callsign string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{"N1ADJ",
			args{"N1ADJ"},
			"N1ADJ"},
		{"N1ADJ A",
			args{"N1ADJ A"},
			"N1ADJ   A"},
		{"#ALL A",
			args{"#ALL A"},
			"#ALL A"},
		{"N1ADJABCD A",
			args{"N1ADJABCD A"},
			"N1ADJABCD A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCallsignModule(tt.args.callsign); got != tt.want {
				t.Errorf("NormalizeCallsignModule() = %v, want %v", got, tt.want)
			}
		})
	}
}
