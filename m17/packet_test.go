package m17

import (
	"reflect"
	"testing"
)

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
				"A1A",
				"B2B",
				PacketType(0),
				[]byte{0},
			},
			&Packet{
				LSF:     NewLSFFromBytes([]byte{0, 0, 0, 0, 10, 161, 0, 0, 0, 0, 17, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 92, 86}),
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
