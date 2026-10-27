package server

import (
	"strings"
	"testing"
)

func TestValidAPRSCallsign(t *testing.T) {
	for cs, want := range map[string]bool{
		"N1ADJ":      true,
		"W2JJT-7":    true,
		"VK2ABC-15":  true,
		"N0/":        false, // would make a g/N0/* filter, which matches everything
		"N0":         true,
		"W1AW/P":     false,
		"W1AW.":      false,
		"":           false,
		"W1AW-":      false,
		"W1AW-123":   false,
		"ABCDEFGH-1": false, // too long for APRS-IS
	} {
		if got := validAPRSCallsign(cs); got != want {
			t.Errorf("validAPRSCallsign(%q) = %v, want %v", cs, got, want)
		}
	}
}

// A callsign from M17 that is not a valid APRS callsign must not reach the
// APRS-IS filter.
func TestGetOrAddUserRejectsFilterSyntax(t *testing.T) {
	m := &APRSModule{users: map[string]*aprsUser{}}
	if u := m.getOrAddUser("N0/"); u != nil {
		t.Errorf("getOrAddUser(%q) = %+v, want nil", "N0/", u)
	}
	if u := m.getOrAddUser("N1ADJ 7"); u == nil || u.aprsCallsign != "N1ADJ-7" {
		t.Errorf("getOrAddUser(%q) = %+v, want N1ADJ-7", "N1ADJ 7", u)
	}
	f := m.buildFilter()
	if f != "g/N1ADJ-7" {
		t.Errorf("filter = %q, want %q", f, "g/N1ADJ-7")
	}
	if strings.Count(f, "/") != 1 {
		t.Errorf("filter %q has more than one entry", f)
	}
}
