package dashboard

import (
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
