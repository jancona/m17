/*
Copyright (C) 2024 Steve Miller KC1AWV

This program is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License as published by the Free
Software Foundation, either version 3 of the License, or (at your option)
any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
more details.

You should have received a copy of the GNU General Public License along with
this program. If not, see <http://www.gnu.org/licenses/>.
*/

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
		want    []byte
		wantErr bool
	}{
		{name: "N1ADJ",
			args:    args{callsign: "N1ADJ"},
			want:    []byte{0, 0, 1, 138, 146, 174},
			wantErr: false,
		},
		{name: "n1adj",
			args:    args{callsign: "N1ADJ"},
			want:    []byte{0, 0, 1, 138, 146, 174},
			wantErr: false,
		},
		{name: "too long",
			args:    args{callsign: "very long call"},
			wantErr: true,
		},
		{name: "@all",
			args:    args{callsign: "@all"},
			want:    []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			wantErr: false,
		},
		{name: "#all",
			args:    args{callsign: "#ALL"},
			want:    []byte{238, 107, 40, 0, 76, 225},
			wantErr: false,
		},
		{name: "#OTHER",
			args:    args{callsign: "#OTHER"},
			want:    []byte{238, 107, 42, 196, 55, 47},
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
