package m17

import (
	"bytes"
	"crypto/rand"
	"strings"
)

// Current M17 reflectors (mrefd 1.0.0 and later) forward traffic to its
// real destination and answer PARROT. Legacy ones (all urfd, and mrefd
// before 1.0.0) forward only streams addressed to their reflector and
// module, and support neither PARROT nor packet mode. There is no reliable
// way to tell them apart by name, so a client sends PARROT a Probe when it
// links (inet.Client does): a current reflector sends it back, readdressed
// to broadcast.

const probeTagLen = 8

// A Probe is a raw packet to PARROT carrying a random tag.
type Probe struct {
	tag []byte
}

// NewProbe returns a probe with a new random tag.
func NewProbe() Probe {
	tag := make([]byte, probeTagLen)
	rand.Read(tag)
	return Probe{tag: tag}
}

// Packet returns the probe as a packet from src, with valid LSF and
// payload CRCs (mrefd answers a packet with a bad CRC with an SMS saying
// so, not an echo).
func (p Probe) Packet(src EncodedCallsign) Packet {
	lsf, _ := NewLSF("PARROT", "PARROT", LSFTypePacket, LSFDataTypeData, 0)
	lsf.Src = src
	lsf.CalcCRC()
	pkt := Packet{LSF: &lsf, Type: PacketTypeRAW, Payload: bytes.Clone(p.tag)}
	pkt.CalcCRC()
	return pkt
}

// IsReply reports whether pkt is the reply to the probe. Only the payload
// is compared: the reflector readdresses the reply to broadcast.
func (p Probe) IsReply(pkt Packet) bool {
	return len(p.tag) > 0 && pkt.Type == PacketTypeRAW && bytes.Equal(pkt.Payload, p.tag)
}

// IsParrot reports whether dst is PARROT. Like mrefd, any callsign
// containing PARROT counts.
func IsParrot(dst EncodedCallsign) bool {
	return strings.Contains(dst.Callsign(), "PARROT")
}

// ParrotReply returns a reflector's reply to a packet sent to PARROT: the
// same packet readdressed to broadcast, as mrefd sends it. pkt is not
// changed.
func ParrotReply(pkt Packet) Packet {
	lsf := *pkt.LSF
	lsf.Dst = EncodedDestinationAllBytes
	lsf.CalcCRC()
	pkt.LSF = &lsf
	pkt.Payload = bytes.Clone(pkt.Payload)
	return pkt
}
