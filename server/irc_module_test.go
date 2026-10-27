package server

import (
	"testing"
	"time"

	"github.com/jancona/m17"
)

func TestIRCTargetAllowed(t *testing.T) {
	for dst, want := range map[string]bool{
		"N1ADJ":    true,
		"W2JJT 7":  true,
		"NICKSERV": false,
		"ChanServ": false,
		"HOSTSERV": false,
	} {
		if got := ircTargetAllowed(dst); got != want {
			t.Errorf("ircTargetAllowed(%q) = %v, want %v", dst, got, want)
		}
	}
}

func TestIRCRemoveIdleUsers(t *testing.T) {
	m := &IRCModule{users: map[string]*ircUser{}}
	now := time.Now()
	m.users["N1ADJ"] = &ircUser{lastGet: now.Add(-time.Hour)}
	m.users["W2JJT"] = &ircUser{lastGet: now}
	if n := m.removeIdleUsers(now.Add(-ircIdleTimeout)); n != 1 {
		t.Errorf("removed %d users, want 1", n)
	}
	if _, ok := m.users["N1ADJ"]; ok {
		t.Error("idle user not removed")
	}
	if _, ok := m.users["W2JJT"]; !ok {
		t.Error("active user removed")
	}
}

// Once maxIRCUsers connections are open, a new callsign gets none, and
// no connection is attempted.
func TestIRCUserCap(t *testing.T) {
	m := &IRCModule{users: map[string]*ircUser{}, serverName: "irc.invalid", port: 6667}
	for i := range maxIRCUsers {
		m.users[string(rune('A'+i%26))+string(rune('0'+i/26))] = &ircUser{lastGet: time.Now()}
	}
	if u := m.getIRCUser("N1ADJ"); u != nil {
		t.Errorf("getIRCUser over the cap = %v, want nil", u)
	}
}

// HandlePacket runs on the reflector's receive loop and must not block,
// even with nothing draining the queue.
func TestIRCHandlePacketDoesNotBlock(t *testing.T) {
	m := &IRCModule{queue: make(chan m17.Packet, 1)}
	p, err := m17.NewPacket("W1AW", "N1ADJ", m17.PacketTypeSMS, []byte("hi\x00"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for range 3 {
			m.HandlePacket(*p)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("HandlePacket blocked on a full queue")
	}
}
