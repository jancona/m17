package inet

import (
	"net"
	"testing"
	"time"

	"github.com/jancona/m17"
)

func testAddr(ip byte, port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(10, 0, 0, ip), Port: port}
}

// A client reconnecting from the same address replaces the old one, and
// the old one's timeout must not remove its replacement.
func TestServerReconnectKeepsNewClient(t *testing.T) {
	s := NewServer("TEST", "127.0.0.1:0", nil)
	mod := &packetModule{}
	addr := testAddr(1, 17000)
	old := s.newClient(make([]byte, 6), mod, addr, false)
	if err := s.addClient(old); err != nil {
		t.Fatal(err)
	}
	c := s.newClient(make([]byte, 6), mod, addr, false)
	if err := s.addClient(c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.stop)
	if !old.stopped {
		t.Error("replaced client's timers not stopped")
	}
	// What the old client's timeout does when it fires
	s.removeClient(old)
	if got := s.lookupClient(addr); got != c {
		t.Errorf("lookupClient = %p, want the new client %p", got, c)
	}
}

func TestServerClientLimits(t *testing.T) {
	s := NewServer("TEST", "127.0.0.1:0", nil)
	s.maxClients = 4
	s.maxClientsPerIP = 2
	mod := &packetModule{}
	add := func(addr *net.UDPAddr) error {
		c := s.newClient(make([]byte, 6), mod, addr, false)
		t.Cleanup(c.stop)
		return s.addClient(c)
	}
	for _, a := range []*net.UDPAddr{testAddr(1, 1), testAddr(1, 2), testAddr(2, 1)} {
		if err := add(a); err != nil {
			t.Fatalf("add %s: %v", a, err)
		}
	}
	if err := add(testAddr(1, 3)); err == nil {
		t.Error("third client from one IP accepted")
	}
	// Reconnecting from an existing address is not a new client
	if err := add(testAddr(1, 2)); err != nil {
		t.Errorf("reconnect refused: %v", err)
	}
	if err := add(testAddr(3, 1)); err != nil {
		t.Fatalf("fourth client: %v", err)
	}
	if err := add(testAddr(4, 1)); err == nil {
		t.Error("client over the server limit accepted")
	}
}

// startTestServer starts a server with one module 'A' on a free port.
func startTestServer(t *testing.T) (*Server, *packetModule, *net.UDPAddr) {
	t.Helper()
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.LocalAddr().(*net.UDPAddr)
	l.Close()
	mod := &packetModule{}
	s := NewServer("TEST", addr.String(), map[byte]Module{'A': mod})
	go s.Start()
	t.Cleanup(func() { s.Close() })
	time.Sleep(100 * time.Millisecond)
	return s, mod, addr
}

// dialTestClient connects to the server with CONN or LSTN and waits for ACKN.
func dialTestClient(t *testing.T, addr *net.UDPAddr, magic, callsign string) *net.UDPConn {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	cs, err := m17.EncodeCallsign(callsign)
	if err != nil {
		t.Fatal(err)
	}
	msg := append([]byte(magic), cs[:]...)
	if _, err := conn.Write(append(msg, 'A')); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:4]) != m17.MagicACKN {
		t.Fatalf("%s: got %q, %v, want ACKN", magic, buf[:n], err)
	}
	return conn
}

func sendTestSMS(t *testing.T, conn *net.UDPConn, src string) {
	t.Helper()
	p, err := m17.NewPacket("W1AW", src, m17.PacketTypeSMS, []byte("hi\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append([]byte(m17.MagicM17Packet), p.ToBytes()...)); err != nil {
		t.Fatal(err)
	}
}

// A listen-only (LSTN) client can't transmit; a CONN client can.
func TestServerListenOnlyCannotSend(t *testing.T) {
	_, mod, addr := startTestServer(t)
	lstn := dialTestClient(t, addr, m17.MagicLSTN, "N1ADJ L")
	sendTestSMS(t, lstn, "N1ADJ L")
	time.Sleep(200 * time.Millisecond)
	if got := mod.received(); len(got) != 0 {
		t.Fatalf("module received %d packets from a listen-only client", len(got))
	}
	conn := dialTestClient(t, addr, m17.MagicCONN, "N1ADJ C")
	sendTestSMS(t, conn, "N1ADJ C")
	deadline := time.Now().Add(time.Second)
	for len(mod.received()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := mod.received(); len(got) != 1 {
		t.Errorf("module received %d packets from a connected client, want 1", len(got))
	}
}

// DISC only disconnects the client if its callsign matches.
func TestServerDISCChecksCallsign(t *testing.T) {
	s, _, addr := startTestServer(t)
	conn := dialTestClient(t, addr, m17.MagicCONN, "N1ADJ C")
	local := conn.LocalAddr().(*net.UDPAddr)
	disc := func(callsign string) {
		cs, err := m17.EncodeCallsign(callsign)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(append([]byte(m17.MagicDISC), cs[:]...)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	disc("W1AW")
	if s.lookupClient(local) == nil {
		t.Fatal("DISC with another callsign disconnected the client")
	}
	disc("N1ADJ C")
	if s.lookupClient(local) != nil {
		t.Error("DISC with the client's callsign did not disconnect it")
	}
}
