package main

import (
	"log"
	"time"

	"github.com/jancona/m17"
)

// Packets the gateway transmits (from the reflector, and its own /ECHO and
// /INFO replies) go through one queue and one sender, so that:
//
//   - a packet never starts while the channel is busy: while RF is being
//     received (a radio that is transmitting cannot hear it, and stopping
//     RX to transmit would cut that radio off), while the gateway is
//     transmitting voice from the reflector, or during its local voice
//     replies;
//   - consecutive packets are spaced by at least packetGap, giving the
//     receiving radio time to decode and store each one; and
//   - only one goroutine calls the modem to transmit a packet.
//
// The channel is timed from the last frame received or voice frame sent,
// not from the gateway state, because a stream whose EOT is missed leaves
// the state stuck until the next transmission; a timer cannot stick.

// packetQueueLen bounds packets waiting for the channel; more are dropped.
const packetQueueLen = 64

// channelPoll is how often a waiting packet rechecks the channel.
const channelPoll = 50 * time.Millisecond

// rfHeard notes channel activity: any frame decoded from the receiver, or
// a voice frame the gateway transmits (from the reflector, or its own
// replies).
func (g *Gateway) rfHeard() {
	g.stateMutex.Lock()
	g.lastRF = time.Now()
	g.stateMutex.Unlock()
}

// queuePacket queues a packet from the reflector for transmission.
func (g *Gateway) queuePacket(p m17.Packet) error {
	return g.enqueue(func() error { return g.transmitNetPacket(p) }, p)
}

// queueLocalPacket queues a packet the gateway built itself, sent as is.
func (g *Gateway) queueLocalPacket(p m17.Packet) error {
	return g.enqueue(func() error { return g.modem.TransmitPacket(p) }, p)
}

func (g *Gateway) enqueue(send func() error, p m17.Packet) error {
	select {
	case g.packetQueue <- send:
		return nil
	default:
		log.Printf("[ERROR] Packet queue full; dropping packet %s", p)
		return nil
	}
}

// sendPackets transmits queued packets one at a time, each when the channel
// is clear, until the queue is closed.
func (g *Gateway) sendPackets() {
	for send := range g.packetQueue {
		g.waitForChannel()
		if err := send(); err != nil {
			log.Printf("[ERROR] Error transmitting packet: %v", err)
		}
		g.stateMutex.Lock()
		g.lastPacketTX = time.Now()
		g.stateMutex.Unlock()
	}
}

// waitForChannel returns once the channel has been clear for packetGap.
func (g *Gateway) waitForChannel() {
	logged := false
	for {
		wait := g.channelWait(time.Now())
		if wait <= 0 {
			return
		}
		if !logged {
			log.Printf("[DEBUG] Packet waiting for the channel")
			logged = true
		}
		time.Sleep(min(wait, channelPoll))
	}
}

// channelWait reports how long a packet must still wait at now; zero or
// less means it may go.
func (g *Gateway) channelWait(now time.Time) time.Duration {
	g.stateMutex.Lock()
	defer g.stateMutex.Unlock()
	return max(g.lastRF.Add(g.packetGap).Sub(now), g.lastPacketTX.Add(g.packetGap).Sub(now))
}
