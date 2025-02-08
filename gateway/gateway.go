package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17text/m17"
)

var (
	isDebugArg  *bool   = flag.Bool("debug", false, "Emit debug log messages")
	isDuplex    *bool   = flag.Bool("duplex", false, "Operate in duplex mode")
	inArg       *string = flag.String("in", "", "M17 input (default stdin)")
	outArg      *string = flag.String("out", "", "M17 output (default stdout)")
	logDestArg  *string = flag.String("log", "", "Device/file for log (default stderr)")
	serverArg   *string = flag.String("server", "", "Relay/reflector server")
	portArg     *uint   = flag.Uint("port", 17000, "Port the relay/reflector listens on")
	moduleArg   *string = flag.String("module", "", "Module to connect to")
	helpArg     *bool   = flag.Bool("h", false, "Print arguments")
	callsignArg *string = flag.String("callsign", "GATEWAY", "Callsign used for messages from the gateway to the reflector")
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

	g.relay, err = m17.NewRelay(serverArg, portArg, moduleArg, *callsignArg, g.FromRelay)
	if err != nil {
		return nil, fmt.Errorf("error creating relay: %v", err)
	}
	err = g.relay.Connect()
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s:%d %s: %v", serverArg, portArg, moduleArg, err)
	}

	return &g, nil
}

func (g Gateway) FromRelay(p m17.Packet) error {
	// log.Printf("[DEBUG] received packet from relay: %#v", p)
	return p.Send(g.out)
}

func (g *Gateway) FromModem(lsfBytes []byte, packetBytes []byte) error {
	log.Printf("[DEBUG] received packet from modem: %x", packetBytes)
	p := m17.NewPacketFromBytes(append(lsfBytes, packetBytes...))
	log.Printf("[DEBUG] p: %#v", p)
	// TODO: Handle error?
	err := g.relay.SendPacket(p)
	return err
}

func (g *Gateway) Run() {
	signalChan := make(chan os.Signal, 1)
	// handle responses from reflector
	go func() {
		g.relay.Handle()
		// When Handle exits, we're done
		<-signalChan
	}()
	d := m17.NewDecoder()
	d.DecodeSamples(g.in, g.FromModem)
	// Run until we're terminated then clean up
	log.Print("[DEBUG] client: Waiting for close signal")
	// wait for a close signal then clean up
	cleanupDone := make(chan struct{})
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalChan
		log.Print("[DEBUG] client: Received an interrupt, stopping...")
		// Cleanup goes here
		close(cleanupDone)
	}()
	<-cleanupDone
}

func (g *Gateway) Close() {
	log.Print("[DEBUG] Gateway.Close()")
	g.done = true
	g.relay.Close()
	if g.in != os.Stdin {
		g.in.Close()
	}
	if g.out != os.Stdout {
		g.out.Close()
	}
}
