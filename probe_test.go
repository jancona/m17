package m17

import "testing"

func TestProbePacket(t *testing.T) {
	src, err := EncodeCallsign("N1ADJ G")
	if err != nil {
		t.Fatal(err)
	}
	probe := NewProbe()
	p := probe.Packet(*src)
	if got := p.LSF.Dst.Callsign(); got != "PARROT" {
		t.Errorf("DST %q, want PARROT", got)
	}
	if p.LSF.Src != *src {
		t.Errorf("SRC %s, want N1ADJ G", p.LSF.Src.Callsign())
	}
	if p.LSF.LSFType() != LSFTypePacket || p.Type != PacketTypeRAW || len(p.Payload) != probeTagLen {
		t.Errorf("LSF type %v, packet type %v, payload % x; want a raw packet with a %d-byte tag",
			p.LSF.LSFType(), p.Type, p.Payload, probeTagLen)
	}
	if !p.LSF.CheckCRC() || !p.CheckCRC() {
		t.Error("bad LSF or payload CRC")
	}
	// The packet survives the wire.
	wire, err := NewPacketFromBytes(p.ToBytes())
	if err != nil {
		t.Fatal(err)
	}

	reply := ParrotReply(wire)
	if reply.LSF.Dst != EncodedDestinationAllBytes || !reply.LSF.CheckCRC() || !reply.CheckCRC() {
		t.Errorf("reply DST %s; want broadcast with valid CRCs", reply.LSF.Dst.Callsign())
	}
	if wire.LSF.Dst.Callsign() != "PARROT" {
		t.Error("ParrotReply changed the packet it was given")
	}
	if !probe.IsReply(reply) {
		t.Error("reply not recognized")
	}
	if NewProbe().IsReply(reply) {
		t.Error("another probe's reply recognized")
	}
	sms := reply
	sms.Type = PacketTypeSMS
	if probe.IsReply(sms) {
		t.Error("SMS with the same payload recognized as the reply")
	}
	if (Probe{}).IsReply(Packet{LSF: reply.LSF, Type: PacketTypeRAW}) {
		t.Error("zero probe recognized an empty raw packet")
	}
}

func TestIsParrot(t *testing.T) {
	for cs, want := range map[string]bool{"PARROT": true, "PARROT A": true, "W1AW": false, "@ALL": false} {
		e, err := EncodeCallsign(cs)
		if err != nil {
			t.Fatal(err)
		}
		if got := IsParrot(*e); got != want {
			t.Errorf("IsParrot(%q) = %v, want %v", cs, got, want)
		}
	}
}
