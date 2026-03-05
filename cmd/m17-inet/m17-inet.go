package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jancona/m17"
)

var (
	hostArg     = flag.String("host", "", "Reflector address with port (e.g. 172.234.217.28:17000)")
	moduleArg   = flag.String("module", "A", "Module to connect to (A-Z)")
	callsignArg = flag.String("callsign", "N0CALL", "Callsign to identify to the reflector")
)

type streamInfo struct {
	src       string
	dst       string
	startTime time.Time
	lastTime  time.Time
	firstFN   uint16
	lastFN    uint16
	received  int
}

var (
	streams   = make(map[uint16]*streamInfo)
	streamsMu sync.Mutex
)

func main() {
	flag.Parse()
	if *hostArg == "" {
		fmt.Fprintln(os.Stderr, "Error: -host is required")
		flag.Usage()
		os.Exit(1)
	}

	server, portStr, err := net.SplitHostPort(*hostArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: bad host %q: %v\n", *hostArg, err)
		os.Exit(1)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: bad port %q: %v\n", portStr, err)
		os.Exit(1)
	}

	*callsignArg = m17.NormalizeCallsignModule(*callsignArg)
	_, err = m17.EncodeCallsign(*callsignArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: bad callsign %q: %v\n", *callsignArg, err)
		os.Exit(1)
	}

	client, err := m17.NewInetClient(server, server, uint(port), *moduleArg, *callsignArg, nil, nil, handleStream)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating client: %v\n", err)
		os.Exit(1)
	}
	err = client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to %s module %s: %v\n", *hostArg, *moduleArg, err)
		os.Exit(1)
	}

	fmt.Printf("Connected to %s module %s, monitoring traffic...\n", *hostArg, *moduleArg)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println()
	printActiveStreams()
	client.Close()
}

func handleStream(sd m17.StreamDatagram) error {
	fn := sd.FrameNumber & 0x7FFF
	now := time.Now()

	streamsMu.Lock()
	defer streamsMu.Unlock()

	info, exists := streams[sd.StreamID]
	if !exists {
		info = &streamInfo{
			src:       sd.LSF.Src.Callsign(),
			dst:       sd.LSF.Dst.Callsign(),
			startTime: now,
			firstFN:   fn,
		}
		streams[sd.StreamID] = info
		fmt.Printf("Stream 0x%04X started: %s > %s\n", sd.StreamID, info.src, info.dst)
	}

	info.received++
	if fn > info.lastFN {
		info.lastFN = fn
	}
	info.lastTime = now

	if sd.LastFrame {
		printStreamSummary(sd.StreamID, info)
		delete(streams, sd.StreamID)
	}
	return nil
}

func printStreamSummary(streamID uint16, info *streamInfo) {
	duration := info.lastTime.Sub(info.startTime).Seconds()
	expected := int(info.lastFN-info.firstFN) + 1
	var rate float64
	if duration > 0 {
		rate = float64(info.received) / duration
	}
	var loss float64
	if expected > 0 {
		loss = float64(expected-info.received) / float64(expected) * 100
	}
	fmt.Printf("Stream 0x%04X  %s > %s  Duration: %.1fs  Frames (received/expected): %d/%d  Rate: %.1f/s  Loss: %.2f%%\n",
		streamID, info.src, info.dst, duration, info.received, expected, rate, loss)
}

func printActiveStreams() {
	streamsMu.Lock()
	defer streamsMu.Unlock()

	for id, info := range streams {
		printStreamSummary(id, info)
	}
}
