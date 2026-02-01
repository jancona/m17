package m17

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"time"
)

type DashboardLogger struct {
	*slog.Logger
	lastLogTime time.Time
}

func NewDashboardLogger(l *slog.Logger) *DashboardLogger {
	return &DashboardLogger{l, time.Now()}
}
func (l *DashboardLogger) Log(logType string, logSubtype string, addlArgs ...any) {
	if l == nil {
		return
	}
	args := []any{"type", logType, "subtype", logSubtype}
	args = append(args, addlArgs...)
	l.Info("", args...)
}
func (l *DashboardLogger) LogFrame(lsf *LSF, logType string, logSubtype string, addlArgs ...any) {
	args := []any{"src", lsf.Src.Callsign(), "dst", lsf.Dst.Callsign(), "can", lsf.CAN()}
	args = append(args, addlArgs...)
	l.Log(logType, logSubtype, args...)
}
func (l *DashboardLogger) LogGNSS(lsf *LSF, logType string) {
	if lsf.GNSS() != nil && lsf.GNSS().ValidLatLon && time.Since(l.lastLogTime) > 15*time.Second {
		log.Printf("[DEBUG] Writing GNSS data to dashboard.log: %s", lsf.GNSS().String())
		l.lastLogTime = time.Now()
		args := []any{
			"dataSource", lsf.GNSS().DataSource,
			"stationType", lsf.GNSS().StationType,
			"src", lsf.Src.Callsign(),
			"latitude", json.Number(fmt.Sprintf("%f", lsf.GNSS().Latitude)),
			"longitude", json.Number(fmt.Sprintf("%f", lsf.GNSS().Longitude)),
		}
		if lsf.GNSS().ValidAltitude {
			args = append(args,
				"altitude", json.Number(fmt.Sprintf("%.1f", lsf.GNSS().Altitude)),
			)
		}
		if lsf.GNSS().ValidBearingSpeed {
			args = append(args,
				"speed", json.Number(fmt.Sprintf("%.1f", lsf.GNSS().Speed)),
				"bearing", lsf.GNSS().Bearing,
			)
		}
		if lsf.GNSS().ValidRadius {
			args = append(args,
				"radius", lsf.GNSS().Radius,
			)
		}
		l.Log(logType, "GNSS", args...)
	}
}
