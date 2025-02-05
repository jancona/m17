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
				[]byte{0, 0, 0, 0, 0, 0},
				[]byte{0, 0, 0, 0, 0, 0},
				[]byte{0, 0},
				[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
				[]byte{0, 0},
			},
		},
		{"happy",
			args{[]byte{0, 0, 1, 138, 146, 174, 0, 0, 75, 19, 209, 6, 0x0f, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
			LSF{
				Dst:  gog.Must(EncodeCallsign("N1ADJ")),
				Src:  gog.Must(EncodeCallsign("N0CALL")),
				Type: []byte{0x0f, 0x7f},
				Meta: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  []byte{0xff, 0xff},
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
		Dst  []byte
		Src  []byte
		Type []byte
		Meta []byte
		CRC  []byte
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
				Dst:  gog.Must(EncodeCallsign("N1ADJ")),
				Src:  gog.Must(EncodeCallsign("N0CALL")),
				Type: []byte{0x0f, 0x7f},
				Meta: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  []byte{0xff, 0xff},
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
		Dst  []byte
		Src  []byte
		Type []byte
		Meta []byte
		CRC  []byte
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{"empty",
			fields{},
			[]byte{149, 224},
		},
		{"happy",
			fields{
				Dst:  gog.Must(EncodeCallsign("N1ADJ")),
				Src:  gog.Must(EncodeCallsign("N0CALL")),
				Type: []byte{0x0f, 0x7f},
				Meta: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				CRC:  []byte{0xff, 0xff},
			},
			[]byte{203, 77},
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
					Dst:  []uint8{0x0, 0x0, 0x1, 0x8a, 0x92, 0xae},
					Src:  []uint8{0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6},
					Type: []uint8{0x0, 0x0},
					Meta: []uint8{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
					CRC:  []uint8{0x59, 0x4c}},
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
					Dst:  []uint8{0x0, 0x0, 0x1, 0x8a, 0x92, 0xae},
					Src:  []uint8{0x0, 0x0, 0x4b, 0x13, 0xd1, 0x6},
					Type: []uint8{0x0, 0x0},
					Meta: []uint8{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
					CRC:  []uint8{0x59, 0x4c}},
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
		lsf  LSF
		t    PacketType
		data []byte
	}
	tests := []struct {
		name string
		args args
		want Packet
	}{
		{"empty",
			args{
				NewLSFFromBytes([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
				PacketType(0),
				[]byte{0},
			},
			Packet{
				LSF:     NewLSFFromBytes([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
				Type:    PacketType(0),
				Payload: []byte{0},
				CRC:     0x4C14,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPacket(tt.args.lsf, tt.args.t, tt.args.data); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPacket() = %v, want %v", got, tt.want)
			}
		})
	}
}
