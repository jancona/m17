package dashboard

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jancona/m17"
)

// With no DashboardLog configured the gateway passes a nil *slog.Logger;
// logging must then do nothing rather than panic.
func TestNilLoggerIsNoop(t *testing.T) {
	lsf, err := m17.NewLSF("@ALL", "N0CALL", m17.LSFTypePacket, m17.LSFDataTypeData, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []*Logger{New(nil), nil} {
		l.Log("Reflector", "Connect", "name", "M17-M17")
		l.LogFrame(&lsf, "RF", "Packet")
		l.LogGNSS(&lsf, "RF")
	}
}

// TestLogFrameCAN checks the dashboard shows a radio's CAN as it sent it.
func TestLogFrameCAN(t *testing.T) {
	var buf bytes.Buffer
	l := New(slog.New(slog.NewJSONHandler(&buf, nil)))
	// TYPE an MD-UV390 on OpenRTX sent for voice on CAN 3
	lsf := m17.LSF{Type: [2]byte{0x01, 0x85}}
	l.LogFrame(&lsf, "RF", "Voice Start")
	var got struct{ CAN int }
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CAN != 3 {
		t.Errorf("logged can %d, want 3: %s", got.CAN, buf.Bytes())
	}
}
