package modem

import "testing"

func TestCC1200GetCommandNumbers(t *testing.T) {
	// Values from the firmware's interface_cmds.h
	for _, c := range []struct {
		name string
		got  byte
		want byte
	}{
		{"GetIdent", cc1200CmdGetIdent, 0x80},
		{"GetCaps", cc1200CmdGetCaps, 0x81},
		{"GetRSSI", cc1200CmdGetRSSI, 0x87},
	} {
		if c.got != c.want {
			t.Errorf("cc1200Cmd%s = 0x%02x, want 0x%02x", c.name, c.got, c.want)
		}
	}
}

func TestCC1200IdentResponse(t *testing.T) {
	// The firmware replies with the IDENT string after a header of command
	// byte and little-endian size, which counts the header.
	ident := "CC1200-HAT 420-450 MHz\nFW v2.3 by Wojciech SP5WWP"
	size := len(ident) + 3
	cmd, err := newCommandV2FromBytes([]byte{0x80, byte(size), byte(size >> 8)})
	if err != nil {
		t.Fatalf("newCommandV2FromBytes: %v", err)
	}
	if cmd.cmd != cc1200CmdGetIdent || len(cmd.data) != len(ident) {
		t.Errorf("got cmd 0x%02x with %d data bytes, want 0x%02x with %d", cmd.cmd, len(cmd.data), cc1200CmdGetIdent, len(ident))
	}
}
