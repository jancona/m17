package modem

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

// Kinds of [Modem] values, for CheckConfig.
const (
	kindString = iota
	kindInt
	kindUint
	kindBool
	kindFloat
)

type modemKey struct {
	name string
	kind int
}

// modemKeys are the [Modem] keys each modem type reads, and their kinds.
var modemKeys = map[string][]modemKey{
	"cc1200": {{"Port", kindString}, {"Speed", kindInt}, {"NRSTPin", kindInt}, {"Boot0Pin", kindInt}, {"ZMQPort", kindInt}},
	"mmdvm": {{"Protocol", kindString}, {"Port", kindString}, {"Speed", kindInt},
		{"RXInvert", kindBool}, {"TXInvert", kindBool}, {"PTTInvert", kindBool}, {"Debug", kindBool},
		{"TXDCOffset", kindInt}, {"TXDelay", kindUint}, {"RXLevel", kindUint}, {"RXDCOffset", kindInt},
		{"TXLevel", kindUint}, {"TXHang", kindUint}, {"RFLevel", kindFloat}},
	"sx1255": {{"SPIDevice", kindString}, {"GPIOChip", kindString}, {"ResetPin", kindInt},
		{"ALSACaptureDevice", kindString}, {"ALSAPlaybackDevice", kindString},
		{"LNAGain", kindUint}, {"PGAGain", kindUint}, {"DACGain", kindInt}, {"MixerGain", kindFloat},
		{"RawIQCapture", kindString}, {"RawIQSeconds", kindInt}},
	"dummy": nil,
}

// ConfigKeys are the [Modem] keys any modem type reads, and Type. Others
// are probably mistyped.
func ConfigKeys() []string {
	keys := []string{"Type"}
	for _, ks := range modemKeys {
		for _, k := range ks {
			if !slices.Contains(keys, k.name) {
				keys = append(keys, k.name)
			}
		}
	}
	slices.Sort(keys)
	return keys
}

// CheckConfig checks the [Modem] settings for modem type t without opening
// anything: what the modem would refuse when it starts, and values of the
// wrong kind, which it would otherwise quietly replace with the default.
func CheckConfig(t string, sec *ini.Section) error {
	if t == "cc1200v2" {
		t = "cc1200"
	}
	keys, ok := modemKeys[t]
	if !ok {
		return fmt.Errorf("unknown modem type %q", t)
	}
	var errs []error
	for _, k := range keys {
		if !sec.HasKey(k.name) {
			continue
		}
		v := strings.TrimSpace(sec.Key(k.name).String())
		if v == "" {
			continue
		}
		var err error
		var want string
		switch k.kind {
		case kindInt:
			_, err = strconv.Atoi(v)
			want = "a whole number"
		case kindUint:
			_, err = strconv.ParseUint(v, 10, 64)
			want = "a whole number, 0 or more"
		case kindBool:
			_, err = sec.Key(k.name).Bool()
			want = "true or false"
		case kindFloat:
			_, err = strconv.ParseFloat(v, 64)
			want = "a number"
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("modem %s is %q, must be %s", k.name, v, want))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	switch t {
	case "cc1200":
		_, err := cc1200Settings(sec)
		errs = append(errs, err)
	case "mmdvm":
		_, _, _, _, err := mmdvmSettings(sec, false)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
