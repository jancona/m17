package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configIni is a valid CC1200 configuration; tests change one key at a time
const configIni = `[General]
Callsign=N1ADJ
[Radio]
RXFrequency=439000000
TXFrequency=439000000
Power=10
AFC=true
FrequencyCorr=0
Duplex=false
[Reflector]
Name=
Module=
[Log]
Level=ERROR
[Modem]
Type=cc1200
`

// withKey returns configIni with key in section set to value; value "-"
// removes the key.
func withKey(section, key, value string) string {
	var out []string
	in := ""
	for _, line := range strings.Split(configIni, "\n") {
		if strings.HasPrefix(line, "[") {
			in = strings.Trim(line, "[]")
		}
		if in == section && strings.HasPrefix(line, key+"=") {
			if value != "-" {
				out = append(out, key+"="+value)
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func TestLoadConfigChecks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ini     string
		wantErr string // "" for no error
	}{
		{"valid", configIni, ""},
		{"empty callsign", withKey("General", "Callsign", ""), "Callsign must be set"},
		{"no callsign", withKey("General", "Callsign", "-"), "Callsign must be set"},
		{"cc1200 duplex", withKey("Radio", "Duplex", "true"), "half-duplex"},
		{"sx1255 duplex", strings.Replace(withKey("Radio", "Duplex", "true"), "Type=cc1200", "Type=sx1255", 1), ""},
		{"FrequencyCorr too big", withKey("Radio", "FrequencyCorr", "40000"), "FrequencyCorr 40000 out of range"},
		{"FrequencyCorr negative", withKey("Radio", "FrequencyCorr", "-3000"), ""},
		{"cc1200 needs Power", withKey("Radio", "Power", "-"), "Power"},
		{"cc1200 needs AFC", withKey("Radio", "AFC", "-"), "AFC"},
		{"mmdvm without Power", strings.Replace(withKey("Radio", "Power", "-"), "Type=cc1200", "Type=mmdvm", 1), ""},
		{"mmdvm without AFC", strings.Replace(withKey("Radio", "AFC", "-"), "Type=cc1200", "Type=mmdvm", 1), ""},
		{"mmdvm with a bad Power", strings.Replace(withKey("Radio", "Power", "lots"), "Type=cc1200", "Type=mmdvm", 1), "Power"},
		{"MQTT on", configIni + "[MQTT]\nEnable=true\n", ""},
		{"MQTT off ignores bad values", configIni + "[MQTT]\nEnable=false\nPort=0\n", ""},
		{"MQTT bad port", configIni + "[MQTT]\nEnable=true\nPort=70000\n", "MQTT Port 70000"},
		{"MQTT bad name", configIni + "[MQTT]\nEnable=true\nName=a/b\n", "MQTT Name"},
		{"MQTT bad log level", configIni + "[MQTT]\nEnable=true\nLogLevel=LOUD\n", "MQTT LogLevel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "gateway.ini")
			if err := os.WriteFile(p, []byte(tc.ini), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := loadConfig(p, "", "")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("got %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestMQTTConfigDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gateway.ini")
	if err := os.WriteFile(p, []byte(configIni+"[MQTT]\nEnable=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(p, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.mqtt == nil {
		t.Fatal("Enable=1 didn't turn MQTT on")
	}
	p2 := filepath.Join(t.TempDir(), "gateway.ini")
	os.WriteFile(p2, []byte(configIni), 0o644)
	if c, _ := loadConfig(p2, "", ""); c.mqtt != nil {
		t.Error("MQTT on without an [MQTT] section")
	}
}
