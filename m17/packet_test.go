package m17

import (
	"reflect"
	"testing"

	"github.com/icza/gog"
)

func TestNewLSFFromBytes(t *testing.T) {
	type args struct {
		buf []byte
	}
	tests := []struct {
		name string
		args args
		want LSF
	}{
		{"empty",
			args{[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
			LSF{
				[6]byte{0, 0, 0, 0, 0, 0},
				[6]byte{0, 0, 0, 0, 0, 0},
				[2]byte{0, 0},
				[14]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
				[2]byte{0, 0},
			},
		},
		{"happy",
			args{[]byte{0, 0, 1, 138, 146, 174, 0, 0, 75, 19, 209, 6, 0x0f, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
			LSF{
				Dst:  *gog.Must(EncodeCallsign("N1ADJ")),
				Src:  *gog.Must(EncodeCallsign("N0CALL")),
				Type: [2]byte{0x0f, 0x7f},
				Meta: [14]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  [2]byte{0xff, 0xff},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewLSFFromBytes(tt.args.buf); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewLSFFromBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLSF_ToBytes(t *testing.T) {
	type fields struct {
		Dst  [6]byte
		Src  [6]byte
		Type [2]byte
		Meta [14]byte
		CRC  [2]byte
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{"empty",
			fields{},
			[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{"happy",
			fields{
				Dst:  *gog.Must(EncodeCallsign("N1ADJ")),
				Src:  *gog.Must(EncodeCallsign("N0CALL")),
				Type: [2]byte{0x0f, 0x7f},
				Meta: [14]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  [2]byte{0xff, 0xff},
			},
			[]byte{0, 0, 1, 138, 146, 174, 0, 0, 75, 19, 209, 6, 0x0f, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &LSF{
				Dst:  tt.fields.Dst,
				Src:  tt.fields.Src,
				Type: tt.fields.Type,
				Meta: tt.fields.Meta,
				CRC:  tt.fields.CRC,
			}
			if got := l.ToBytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LSF.ToBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLSF_CalcCRC(t *testing.T) {
	type fields struct {
		Dst  [6]byte
		Src  [6]byte
		Type [2]byte
		Meta [14]byte
		CRC  [2]byte
	}
	tests := []struct {
		name   string
		fields fields
		want   uint16
	}{
		{"empty",
			fields{},
			0x95e0,
		},
		{"happy",
			fields{
				Dst:  *gog.Must(EncodeCallsign("N1ADJ")),
				Src:  *gog.Must(EncodeCallsign("N0CALL")),
				Type: [2]byte{0x0f, 0x7f},
				Meta: [14]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  [2]byte{0xff, 0xff},
			},
			0xcb4d,
		},
		{"happy2",
			fields{
				Dst:  *gog.Must(EncodeCallsign("N1ADJ")),
				Src:  *gog.Must(EncodeCallsign("N0CALL")),
				Type: [2]byte{0x0f, 0x7f},
				Meta: [14]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  [2]byte{0xcb, 0x4d},
			},
			CRC([]byte{0, 0, 1, 138, 146, 174, 0, 0, 75, 19, 209, 6, 0x0f, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}),
		},
		{"bad1",
			fields{
				Dst:  [6]uint8{0x0, 0x0, 0x1, 0x8a, 0x92, 0xae},
				Src:  [6]uint8{0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6},
				Type: [2]uint8{0x0, 0x0},
				Meta: [14]uint8{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
				CRC:  [2]uint8{0x59, 0x4c},
			},
			0x594c,
		},
		{"n7tae LSF",
			fields{
				// Dst:  []byte{0x47, 0x86, 0x8c, 0xc4, 0xcc, 0x5e},
				// Src:  []byte{0x00, 0x00, 0x01, 0x8a, 0x92, 0xae},
				// Type: []byte{0x00, 0x00},
				// Meta: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				// CRC:  []byte{0x00, 0x00},

				Dst:  [6]uint8{0xb, 0xf0, 0x90, 0x0, 0xba, 0xed},
				Src:  [6]uint8{0x0, 0x0, 0x1, 0x8a, 0x92, 0xae},
				Type: [2]uint8{0x0, 0x0},
				Meta: [14]uint8{},
				CRC:  [2]uint8{},
			},
			0x2630},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &LSF{
				Dst:  tt.fields.Dst,
				Src:  tt.fields.Src,
				Type: tt.fields.Type,
				Meta: tt.fields.Meta,
				CRC:  tt.fields.CRC,
			}
			if got := l.CalcCRC(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LSF.CalcCRC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewPacketFromBytes(t *testing.T) {
	type args struct {
		buf []byte
	}
	tests := []struct {
		name string
		args args
		want Packet
	}{
		{"empty",
			args{[]byte{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0,
				0, 0,
			}},
			Packet{
				LSF:     NewLSFFromBytes([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
				Type:    PacketType(0),
				Payload: []byte{},
				CRC:     0,
			},
		},
		{"happy",
			args{[]byte{
				0x0, 0x0, 0x1, 0x8a, 0x92, 0xae,
				0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6,
				0x0, 0x0,
				0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
				0x59, 0x4c,
				0x5,
				0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d,
				0x65, 0x21,
			}},
			Packet{
				LSF: LSF{
					Dst:  [6]uint8{0x0, 0x0, 0x1, 0x8a, 0x92, 0xae},
					Src:  [6]uint8{0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6},
					Type: [2]uint8{0x0, 0x0},
					Meta: [14]uint8{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
					CRC:  [2]uint8{0x59, 0x4c}},
				Type:    5,
				Payload: []uint8{0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d},
				CRC:     0x6521,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPacketFromBytes(tt.args.buf); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPacketFromBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPacket_ToBytes(t *testing.T) {
	type fields struct {
		LSF     LSF
		Type    PacketType
		Payload []byte
		CRC     uint16
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{"empty",
			fields{
				LSF:     NewLSFFromBytes([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
				Type:    PacketType(0),
				Payload: []byte{},
				CRC:     0,
			},
			[]byte{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0,
			},
		},
		{"happy",
			fields{
				LSF: LSF{
					Dst:  [6]uint8{0x0, 0x0, 0x1, 0x8a, 0x92, 0xae},
					Src:  [6]uint8{0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6},
					Type: [2]uint8{0x0, 0x0},
					Meta: [14]uint8{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
					CRC:  [2]uint8{0x59, 0x4c}},
				Type:    5,
				Payload: []uint8{0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d},
				CRC:     0x6521,
			},
			[]byte{
				0x0, 0x0, 0x1, 0x8a, 0x92, 0xae,
				0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6,
				0x0, 0x0,
				0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
				0x59, 0x4c,
				0x5,
				0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x66, 0x72, 0x6f, 0x6d, 0x20, 0x6d,
				0x65, 0x21,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Packet{
				LSF:     tt.fields.LSF,
				Type:    tt.fields.Type,
				Payload: tt.fields.Payload,
				CRC:     tt.fields.CRC,
			}
			if got := p.ToBytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Packet.ToBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewPacket(t *testing.T) {
	type args struct {
		dst  string
		src  string
		t    PacketType
		data []byte
	}
	tests := []struct {
		name    string
		args    args
		want    *Packet
		wantErr bool
	}{
		{"simple",
			args{
				"A",
				"B",
				PacketType(0),
				[]byte{0},
			},
			&Packet{
				LSF:     NewLSFFromBytes([]byte{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 71, 150}),
				Type:    PacketType(0),
				Payload: []byte{0},
				CRC:     26476,
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewPacket(tt.args.dst, tt.args.src, tt.args.t, tt.args.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPacket() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPacket() = %v, want %v", got, tt.want)
			}
		})
	}
}
