package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17text/m17"
)

const (
	gatewaySrcCall = "ABCDE" // Invalid callsign used for messages from the gateway to the reflector
)

var (
	isDebugArg *bool   = flag.Bool("debug", false, "Emit debug log messages")
	inArg      *string = flag.String("in", "", "M17 input (default stdin)")
	outArg     *string = flag.String("out", "", "M17 output (default stdout)")
	logDestArg *string = flag.String("log", "", "Device/file for log (default stderr)")
	serverArg  *string = flag.String("server", "", "Relay/reflector server")
	portArg    *uint   = flag.Uint("port", 17000, "Port the relay/reflector listens on")
	moduleArg  *string = flag.String("module", "T", "Module to connect to")
	helpArg    *bool   = flag.Bool("h", false, "Print arguments")
)

func main() {
	var err error

	flag.Parse()

	if *helpArg {
		flag.Usage()
		return
	}

	if *serverArg == "" {
		flag.Usage()
		log.Fatal("-server argument is required")
	}
	setupLogging()

	g, err := NewGateway(*serverArg, *portArg, *moduleArg, *inArg, *outArg)
	if err != nil {
		log.Fatalf("Error creating Gateway: %v", err)
	}
	defer g.Close()
	g.Run()
}

func setupLogging() {
	var err error
	minLogLevel := "INFO"
	if *isDebugArg {
		minLogLevel = "DEBUG"
	}
	logWriter := os.Stderr
	if *logDestArg != "" {
		logWriter, err = os.OpenFile(*logDestArg, os.O_WRONLY|os.O_CREATE|os.O_SYNC, 0644)
		if err != nil {
			log.Fatalf("Error opening server output, exiting: %v", err)
		}
	}

	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "ERROR"},
		MinLevel: logutils.LogLevel(minLogLevel),
		Writer:   logWriter,
	}
	log.SetOutput(filter)
	log.Print("[DEBUG] Debug is on")
}

// Gateway connects to a reflector, converts traffic to/from audio format on stdout,
// so it can be used in a pipeline with other tools
type Gateway struct {
	Server string
	Port   uint
	Module string

	in    *os.File
	out   *os.File
	relay *m17.Relay
	done  bool
}

func NewGateway(serverArg string, portArg uint, moduleArg string, in string, out string) (*Gateway, error) {
	var err error

	g := Gateway{
		Server: serverArg,
		Port:   portArg,
		Module: moduleArg,
		in:     os.Stdin,
		out:    os.Stdout,
	}

	if in != "" {
		g.in, err = os.Open(*inArg)
		if err != nil {
			return nil, fmt.Errorf("failed to open M17 input '%s': %w", in, err)
		}
	}

	if out != "" {
		g.out, err = os.Create(*outArg)
		if err != nil {
			return nil, fmt.Errorf("failed to open M17 output '%s': %w", out, err)
		}
	}

	g.relay, err = m17.NewM17Relay(serverArg, portArg, moduleArg, gatewaySrcCall, g.FromRelay)
	if err != nil {
		return nil, fmt.Errorf("error creating relay: %v", err)
	}
	err = g.relay.Connect()
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s:%d %s: %v", serverArg, portArg, moduleArg, err)
	}

	return &g, nil
}

func (g Gateway) FromRelay(buf []byte) error {
	log.Printf("[DEBUG] received packet from relay: %x", buf)
	// A packet is an LSF + type code 0x05 for SMS + data up to 823 bytes
	// dst,_ := m17.DecodeCallsign(buf[4:10])
	// src,_ := m17.DecodeCallsign(buf[10:16])
	// typ := buf[16]
	// data := buf[17:]
	lsf := m17.LSF{
		// A packet is an LSF + type code 0x05 for SMS + data up to 823 bytes
		Dst: [6]uint8(buf[4:10]),
		Src: [6]uint8(buf[10:16]),
	}

	// // encode packet and send to g.out
	return m17.SendPacket(lsf, buf[16:], g.out)
}

func (g *Gateway) FromClient(lsf []byte, buf []byte) error {
	log.Printf("[DEBUG] received packet from client: %x", buf)
	l := len(buf)
	t := buf[0]                  // type
	text := string(buf[1 : l-3]) // skip terminating null and CRC
	var crc uint16
	b := bytes.NewReader(buf[l-2:])
	err := binary.Read(b, binary.LittleEndian, &crc)
	if err != nil {
		log.Printf("[INFO] binary.Read failed: %v", err)
	}
	log.Printf("[DEBUG] length: %d, crc: %x, CRC ok: %v, type %02X: %s", l, crc, m17.CRC(buf), t, text)
	// TODO: Handle error?
	g.relay.SendMessage(lsf[0:6], lsf[6:12], text)
	return nil
}

func (g *Gateway) Run() {
	m17.ProcessSamples(g.in, g.FromClient)
	g.Close()
	// // Run until we're terminated then clean up
	// log.Print("[DEBUG] client: Waiting for close signal")
	// // wait for a close signal then clean up
	// signalChan := make(chan os.Signal, 1)
	// cleanupDone := make(chan struct{})
	// signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	// go func() {
	// 	<-signalChan
	// 	log.Print("client: Received an interrupt, stopping...")
	// 	// Cleanup goes here
	// 	g.Close()
	// 	close(cleanupDone)
	// }()
	// <-cleanupDone
}
func (g *Gateway) Close() {
	g.done = true
	g.relay.Close()
	if g.in != os.Stdin {
		g.in.Close()
	}
	if g.out != os.Stdout {
		g.out.Close()
	}
}
