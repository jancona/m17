package modem

import (
	"errors"
	"io"
	"strings"
	"testing"
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
