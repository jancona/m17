package modem

import "testing"

func TestMMDVMHWTypeFromDescription(t *testing.T) {
	for _, c := range []struct {
		description string
		want        byte
	}{
		{"MMDVM 20200308 TCXO (D-Star/DMR/System Fusion/P25/NXDN/M17/POCSAG/FM/AX.25)", mmdvmHWTypeMMDVM},
		{"DVMEGA HR3.26", mmdvmHWTypeDVMEGA},
		{"MMDVM_HS_Hat-v1.6.1 20230522 14.7456MHz ADF7021", mmdvmHWTypeMMDVM_HS_HAT},
		{"MMDVM_HS_Dual_Hat-v1.6.1 20251011 14.7456MHz dual ADF7021", mmdvmHWTypeMMDVM_HS_DUAL_HAT},
		{"MMDVM_HS-ADF7021 v1.6.1", mmdvmHWTypeMMDVM_HS},
		{"SkyBridge-v1.6.1", mmdvmHWTypeSKYBRIDGE},
		{"Something else", mmdvmHWTypeMMDVM},
		{"", mmdvmHWTypeMMDVM},
	} {
		if got := mmdvmHWTypeFromDescription([]byte(c.description)); got != c.want {
			t.Errorf("mmdvmHWTypeFromDescription(%q) = %d, want %d", c.description, got, c.want)
		}
	}
}

// TestReadVersionNULTerminated: the ident stops at the NUL that ends the
// description, as sent by MMDVM_HS_Dual_Hat firmware v1.6.1.
func TestReadVersionNULTerminated(t *testing.T) {
	p := &fakeModemPort{}
	description := "MMDVM_HS_Dual_Hat-v1.6.1 20251011 14.7456MHz dual ADF7021 FW by CA6JAU GitID #403e47c"
	p.onWrite = func(cmd []byte) {
		if cmd[2] == mmdvmGetVersion {
			p.queue(modemMsg(mmdvmGetVersion, append(append([]byte{1}, description...), "\x00"+"00620046460000024E543547"...)))
		}
	}
	m := &MMDVM{port: p, sendCmds: make(chan []byte, 10)}
	if err := m.readVersion(); err != nil {
		t.Fatalf("readVersion: %v", err)
	}
	if m.Ident() != description || m.hwType != mmdvmHWTypeMMDVM_HS_DUAL_HAT {
		t.Errorf("ident %q, hardware %d; want %q, MMDVM_HS_Dual_Hat", m.Ident(), m.hwType, description)
	}
}

// TestReadVersionShortReply: a GetVersion reply too short to hold what its
// protocol version promises is skipped, not read past its end.
func TestReadVersionShortReply(t *testing.T) {
	p := &fakeModemPort{}
	replies := [][]byte{
		{},
		{2, 0x01, 0x02},
		append(append([]byte{2, 0x01, 0x02, 2}, make([]byte, 16)...), "DVMEGA HR3.26"...),
	}
	p.onWrite = func(cmd []byte) {
		if cmd[2] == mmdvmGetVersion && len(replies) > 0 {
			p.queue(modemMsg(mmdvmGetVersion, replies[0]))
			replies = replies[1:]
		}
	}
	m := &MMDVM{port: p, sendCmds: make(chan []byte, 10)}
	if err := m.readVersion(); err != nil {
		t.Fatalf("readVersion: %v", err)
	}
	if m.protocolVersion != 2 || m.hwType != mmdvmHWTypeDVMEGA || m.Ident() != "DVMEGA HR3.26" {
		t.Errorf("protocol %d, hardware %d, ident %q; want 2, DVMEGA, \"DVMEGA HR3.26\"", m.protocolVersion, m.hwType, m.Ident())
	}
}
