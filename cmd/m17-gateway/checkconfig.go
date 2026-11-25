package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jancona/m17/inet"
	"github.com/jancona/m17/modem"
	"gopkg.in/ini.v1"
)

// knownKeys are the keys m17-gateway reads, by section. Others are
// probably mistyped.
var knownKeys = map[string][]string{
	"General":   {"Callsign", "DashboardLog", "AudioDir"},
	"Radio":     {"RXFrequency", "TXFrequency", "Power", "AFC", "FrequencyCorr", "Duplex", "RXHoldoff", "PacketGap"},
	"Reflector": {"HostFile", "OverrideHostFile", "Name", "Module"},
	"Log":       {"Level", "Path", "Root"},
	"MQTT":      {"Enable", "Host", "Port", "Auth", "Username", "Password", "Keepalive", "Name", "LogLevel"},
	"Modem":     modem.ConfigKeys(),
}

// checkConfig checks the settings in iniFile, opening nothing else:
// -check-config. It writes one problem per line to w, "error: " or
// "warning: " then the message, and returns the exit status: 1 if there
// are errors, else 0. Warnings are settings it would ignore.
func checkConfig(iniFile string, w io.Writer) int {
	cfg, err := ini.Load(iniFile)
	if err != nil {
		fmt.Fprintf(w, "error: can't read %s: %v\n", iniFile, err)
		return 1
	}
	status := 0
	if _, err := parseConfig(cfg, "", "", true); err != nil {
		for _, l := range strings.Split(err.Error(), "\n") {
			if l != "" {
				fmt.Fprintf(w, "error: %s\n", l)
			}
		}
		status = 1
	}
	// The reflector it links to at startup must be in a host file, or it
	// won't start. (parseConfig has read the files, if it found no errors.)
	sec := cfg.Section("Reflector")
	if name := sec.Key("Name").String(); name != "" && status == 0 {
		found := false
		for _, key := range []string{"OverrideHostFile", "HostFile"} {
			if path := sec.Key(key).String(); path != "" {
				if hf, err := inet.NewHostfile(path); err == nil {
					_, ok := hf.Hosts[name]
					found = found || ok
				}
			}
		}
		if !found {
			fmt.Fprintf(w, "error: reflector %s isn't in the host files, so the gateway won't start\n", name)
			status = 1
		}
	}
	// Then what it would ignore
	for _, sec := range cfg.Sections() {
		name := sec.Name()
		if name == ini.DefaultSection {
			if len(sec.Keys()) > 0 {
				fmt.Fprintf(w, "warning: settings before the first [section] are ignored: %s\n", strings.Join(sec.KeyStrings(), ", "))
			}
			continue
		}
		keys, ok := knownKeys[name]
		if !ok {
			fmt.Fprintf(w, "warning: section [%s] is ignored\n", name)
			continue
		}
		for _, k := range sec.KeyStrings() {
			if !slices.Contains(keys, k) {
				fmt.Fprintf(w, "warning: [%s] %s is ignored\n", name, k)
			}
		}
	}
	return status
}
