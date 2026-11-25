package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkIni is configIni with the CC1200 HAT's modem settings and a reflector
// in a host file; tests add or change lines.
func checkIni(t *testing.T, extra ...string) string {
	t.Helper()
	dir := t.TempDir()
	hosts := filepath.Join(dir, "M17Hosts.txt")
	os.WriteFile(hosts, []byte("M17-M17 192.0.2.1 17000\n"), 0o644)
	ini := strings.Replace(configIni, "Name=\nModule=", "HostFile="+hosts+"\nName=M17-M17\nModule=C", 1) +
		"Port=/dev/ttyAMA0\nSpeed=460800\nNRSTPin=21\nBoot0Pin=20\n"
	for i := 0; i+1 < len(extra); i += 2 {
		ini = strings.Replace(ini, extra[i], extra[i+1], 1)
	}
	p := filepath.Join(dir, "gateway.ini")
	os.WriteFile(p, []byte(ini), 0o644)
	return p
}

func runCheck(t *testing.T, path string) (int, []string) {
	t.Helper()
	var b strings.Builder
	status := checkConfig(path, &b)
	return status, strings.Split(strings.TrimSpace(b.String()), "\n")
}

func TestCheckConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   []string // replacements in checkIni
		status  int
		want    []string // lines that must appear
		notWant string   // must not appear anywhere
	}{
		{name: "good", status: 0},
		{"bad radio", []string{"RXFrequency=439000000", "RXFrequency=139000000"}, 1,
			[]string{"error: configured RXFrequency 139000000 out of range"}, ""},
		{"several errors, one per line", []string{"Level=ERROR", "Level=LOUD", "Callsign=N1ADJ", "Callsign="}, 1,
			[]string{"error: configured Callsign must be set", "error: configured Log Level must be one of"}, ""},
		// The modem's own settings, which only the modem checked before
		{"CC1200 pin missing", []string{"NRSTPin=21\n", ""}, 1,
			[]string{"error: modem NRSTPin must be set to a whole number"}, ""},
		{"wrong kind", []string{"Boot0Pin=20", "Boot0Pin=twenty"}, 1,
			[]string{`error: modem Boot0Pin is "twenty", must be a whole number`}, ""},
		{"MMDVM range", []string{"Type=cc1200", "Type=mmdvm\nProtocol=uart\nTXLevel=150"}, 1,
			[]string{"error: modem TXLevel is 150, must be between 0 and 100%"}, ""},
		{"SX1255 gain not a number", []string{"Type=cc1200", "Type=sx1255\nMixerGain=loud"}, 1,
			[]string{`error: modem MixerGain is "loud", must be a number`}, "Power"},
		// The reflector must be in a host file, or the gateway won't start
		{"unknown reflector", []string{"Name=M17-M17", "Name=M17-XXX"}, 1,
			[]string{"error: reflector M17-XXX isn't in the host files, so the gateway won't start"}, ""},
		{"no reflector", []string{"Name=M17-M17", "Name="}, 0, nil, ""},
		// Settings it would ignore are warnings: exit 0
		{"mistyped key", []string{"Duplex=false", "Duplex=false\nDuplx=true"}, 0,
			[]string{"warning: [Radio] Duplx is ignored"}, ""},
		{"unknown section", []string{"[Log]", "[Logs]\nLevel=INFO\n[Log]"}, 0,
			[]string{"warning: section [Logs] is ignored"}, ""},
		{"keys of another modem type are fine", []string{"Boot0Pin=20", "Boot0Pin=20\nLNAGain=24\nTXLevel=50"}, 0, nil, "warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, lines := runCheck(t, checkIni(t, tc.extra...))
			out := strings.Join(lines, "\n")
			if status != tc.status {
				t.Errorf("status %d, want %d:\n%s", status, tc.status, out)
			}
			for _, w := range tc.want {
				found := false
				for _, l := range lines {
					found = found || strings.HasPrefix(l, w)
				}
				if !found {
					t.Errorf("no line starting %q in:\n%s", w, out)
				}
			}
			if tc.notWant != "" && strings.Contains(out, tc.notWant) {
				t.Errorf("output has %q:\n%s", tc.notWant, out)
			}
			for _, l := range lines {
				if l != "" && !strings.HasPrefix(l, "error: ") && !strings.HasPrefix(l, "warning: ") {
					t.Errorf("line %q is neither an error nor a warning", l)
				}
			}
		})
	}
}

// TestCheckConfigOpensNothing: a dashboard log isn't created, only its
// directory checked; a file that can't be read is an error.
func TestCheckConfigOpensNothing(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "dashboard.log")
	if status, lines := runCheck(t, checkIni(t, "[General]", "[General]\nDashboardLog="+log)); status != 0 {
		t.Errorf("status %d: %q", status, lines)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("-check-config created the dashboard log")
	}
	if status, lines := runCheck(t, checkIni(t, "[General]", "[General]\nDashboardLog=/no/such/dir/dashboard.log")); status != 1 || !strings.Contains(lines[0], "directory that doesn't exist") {
		t.Errorf("missing log directory: %d %q", status, lines)
	}
	if status, lines := runCheck(t, filepath.Join(dir, "none.ini")); status != 1 || !strings.HasPrefix(lines[0], "error: can't read") {
		t.Errorf("missing file: %d %q", status, lines)
	}
}
