package modem

import (
	"errors"
	"io"
	"strings"
	"testing"

	"gopkg.in/ini.v1"
)

// TestOpenFrequencyRefused: MMDVM_HS firmware answers SetFrequency with a
// NAK for a frequency outside its bands; open() reports that, naming the
// frequencies, instead of a bare "modem returned NAK".
func TestOpenFrequencyRefused(t *testing.T) {
	p := &fakeModemPort{}
	p.answerHandshake()
	handshake := p.onWrite
	p.onWrite = func(cmd []byte) {
		if cmd[2] == mmdvmSetFrequency {
			p.queue(modemMsg(mmdvmNAK, []byte{mmdvmSetFrequency, 4}))
			return
		}
		handshake(cmd)
	}
	m := &MMDVM{sendCmds: make(chan []byte, 10), portName: "fake", rxFrequency: 300_000_000, txFrequency: 300_000_000}
	m.openPort = func() (io.ReadWriteCloser, error) { return p, nil }
	err := m.open()
	if !errors.Is(err, ErrModemNAK) {
		t.Fatalf("open: %v, want ErrModemNAK", err)
	}
	if !strings.Contains(err.Error(), "RXFrequency 300000000") || !strings.Contains(err.Error(), "bands its firmware allows") {
		t.Errorf("error doesn't explain the refusal: %v", err)
	}
}

// TestOpenSendsRFLevel: SetFrequency carries the RF level after the two
// frequencies, so MMDVM_HS firmware doesn't fall back to full power.
func TestOpenSendsRFLevel(t *testing.T) {
	p := &fakeModemPort{}
	p.answerHandshake()
	handshake := p.onWrite
	var got []byte
	p.onWrite = func(cmd []byte) {
		if cmd[2] == mmdvmSetFrequency {
			got = append([]byte(nil), cmd...)
		}
		handshake(cmd)
	}
	m := &MMDVM{sendCmds: make(chan []byte, 10), portName: "fake",
		rxFrequency: 439_000_000, txFrequency: 434_000_000, rfLevel: 128}
	m.openPort = func() (io.ReadWriteCloser, error) { return p, nil }
	if err := m.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	want := []byte{mmdvmFrameStart, 13, mmdvmSetFrequency, 0,
		0xC0, 0x9B, 0x2A, 0x1A, // 439000000, little-endian
		0x80, 0x50, 0xDE, 0x19, // 434000000
		128}
	if string(got) != string(want) {
		t.Errorf("SetFrequency\n got % x\nwant % x", got, want)
	}
}

func TestRFLevel(t *testing.T) {
	for _, tc := range []struct {
		value   string // "" for no key
		want    byte
		wantErr bool
	}{
		{"", 255, false},
		{"100", 255, false},
		{"50", 128, false},
		{"0", 0, false},
		{"12.5", 32, false},
		{"101", 0, true},
		{"-1", 0, true},
	} {
		cfg := ini.Empty()
		sec := cfg.Section("Modem")
		if tc.value != "" {
			sec.Key("RFLevel").SetValue(tc.value)
		}
		got, err := mmdvmRFLevel(sec)
		if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
			t.Errorf("RFLevel %q: got %d, %v; want %d, error %v", tc.value, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestUintSetting(t *testing.T) {
	for _, tc := range []struct {
		key, value string // value "" for no key
		def, max   uint
		want       uint
		wantErr    bool
	}{
		{"TXDelay", "", 100, 500, 100, false},
		{"TXDelay", "300", 100, 500, 300, false}, // wrapped to 44 before
		{"TXDelay", "500", 100, 500, 500, false},
		{"TXDelay", "1000", 100, 500, 0, true}, // firmware NAKs over 50 (500 ms)
		{"RXLevel", "100", 50, 100, 100, false},
		{"RXLevel", "101", 50, 100, 0, true},
		{"TXHang", "256", 5, 255, 0, true},
	} {
		sec := ini.Empty().Section("Modem")
		if tc.value != "" {
			sec.Key(tc.key).SetValue(tc.value)
		}
		got, err := uintSetting(sec, tc.key, tc.def, tc.max, "")
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s=%q: got %d, %v", tc.key, tc.value, got, err)
		}
	}
}
