package inet

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jancona/m17"
)

// A short M17P datagram from anyone on the internet must be dropped, not
// panic the server.
func TestServerSurvivesShortPacket(t *testing.T) {
	// Find a free port
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.LocalAddr().String()
	l.Close()

	s := NewServer("TEST", addr, nil)
	done := make(chan struct{})
	go func() {
		s.Start()
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, n := range []int{0, 1, 10, m17.LSFLen, m17.LSFLen + 2} {
		if _, err := conn.Write(append([]byte(m17.MagicM17Packet), make([]byte, n)...)); err != nil {
			t.Fatal(err)
		}
	}
	// An SMS packet with an empty payload is long enough to parse
	sms := make([]byte, m17.LSFLen+1+m17.CRCLen)
	sms[m17.LSFLen] = byte(m17.PacketTypeSMS)
	if _, err := conn.Write(append([]byte(m17.MagicM17Packet), sms...)); err != nil {
		t.Fatal(err)
	}
	// A panic in the server goroutine would have ended the test binary by now
	time.Sleep(200 * time.Millisecond)

	s.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

// Clients come and go on timer goroutines while packets are routed from
// others. Under -race this fails if the client map is read without the lock;
// without -race, Go aborts on the concurrent map access often enough.
func TestServerClientMapConcurrency(t *testing.T) {
	s := NewServer("TEST", "127.0.0.1:0", nil)
	mod := &packetModule{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 2000 {
			c := &client{module: mod, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10000 + i%50}}
			s.addClient(c)
			s.removeClient(c)
		}
	}()
	go func() {
		defer wg.Done()
		for range 2000 {
			s.lookupClientsByModule('A')
		}
	}()
	wg.Wait()
}
