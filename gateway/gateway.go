package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17text/m17"
	"gopkg.in/ini.v1"
)

type config struct {
	callsign        string
	duplex          bool
	rxFrequency     uint32
	txFrequency     uint32
	power           float32
	afc             bool
	frequencyCorr   int16
	reflectorName   string
	reflectorAddr   string
	reflectorPort   uint
	reflectorModule string
	logLevel        string
	logPath         string
	logRoot         string
	modemPort       string
	modemSpeed      int
	nRSTPin         int
	paEnablePin     int
	boot0Pin        int
	symbolsIn       *os.File
	symbolsOut      *os.File
}

func loadConfig(iniFile string, inFile string, outFile string) (config, error) {
	cfg, err := ini.Load(iniFile)
	if err != nil {
		log.Fatalf("Fail to read config from %s: %v", iniFile, err)
	}
	callsign := cfg.Section("General").Key("Callsign").String()
	rxFrequency, rxFrequencyErr := cfg.Section("Radio").Key("RXFrequency").Uint()
	txFrequency, txFrequencyErr := cfg.Section("Radio").Key("TXFrequency").Uint()
	power, powerErr := cfg.Section("Radio").Key("Power").Float64()
	afc, afcErr := cfg.Section("Radio").Key("AFC").Bool()
	frequencyCorr, frequencyCorrErr := cfg.Section("Radio").Key("FrequencyCorr").Int()
	duplex, duplexErr := cfg.Section("Radio").Key("Duplex").Bool()
	reflectorName := cfg.Section("Reflector").Key("Name").String()
	reflectorAddr := cfg.Section("Reflector").Key("Address").String()
	reflectorPort := cfg.Section("Reflector").Key("Port").MustUint(17000)
	reflectorModule := cfg.Section("Reflector").Key("Module").String()
	logLevel := cfg.Section("Log").Key("Level").String()
	logPath := cfg.Section("Log").Key("Path").String()
	logRoot := cfg.Section("Log").Key("Root").String()
	modemPort := cfg.Section("Modem").Key("Port").String()
	modemSpeed, modemSpeedErr := cfg.Section("Modem").Key("Speed").Int()
	nRSTPin, nRSTPinErr := cfg.Section("Modem").Key("NRSTPin").Int()
	paEnablePin, paEnablePinErr := cfg.Section("Modem").Key("PAEnablePin").Int()
	boot0Pin, boot0PinErr := cfg.Section("Modem").Key("Boot0Pin").Int()

	_, callsignErr := m17.EncodeCallsign(callsign)
	// TODO: Lots of these validations are CC1200 specific
	if rxFrequencyErr == nil {
		if rxFrequency < 420e6 || rxFrequency > 450e6 {
			rxFrequencyErr = fmt.Errorf("configured RXFrequency %d out of range (420 to 450 MHz)", rxFrequency)
		}
	}
	if txFrequencyErr == nil {
		if txFrequency < 420e6 || txFrequency > 450e6 {
			txFrequencyErr = fmt.Errorf("configured TXFrequency %d out of range (420 to 450 MHz)", txFrequency)
		}
	}
	if powerErr == nil {
		if power < -16 || power > 14 {
			powerErr = fmt.Errorf("configured Power %f out of range (-16 to 14 dBm)", power)
		}
	}
	var reflectorAddrErr error
	if reflectorAddr == "" {
		reflectorAddrErr = fmt.Errorf("configured Reflector Address is empty")
	}
	var reflectorModuleErr error
	if len(reflectorModule) > 1 {
		reflectorModuleErr = fmt.Errorf("configured Reflector Module must be zero or one character")
	}
	var logLevelErr error
	if logLevel != "ERROR" && logLevel != "INFO" && logLevel != "DEBUG" {
		logLevelErr = fmt.Errorf("configured Log Level must be one of ERROR, INFO or DEBUG")
	}

	var symbolsInErr, symbolsOutErr error
	symbolsIn := os.Stdin
	if inFile != "" {
		symbolsIn, symbolsInErr = os.Open(inFile)
	}
	symbolsOut := os.Stdout
	if outFile != "" {
		symbolsOut, symbolsOutErr = os.Create(outFile)
	}

	err = errors.Join(
		rxFrequencyErr,
		txFrequencyErr,
		powerErr,
		afcErr,
		frequencyCorrErr,
		duplexErr,
		modemSpeedErr,
		nRSTPinErr,
		paEnablePinErr,
		boot0PinErr,
		callsignErr,
		reflectorAddrErr,
		reflectorModuleErr,
		// reflectorPortErr,
		logLevelErr,
		symbolsInErr,
		symbolsOutErr,
	)

	return config{
		callsign:        callsign,
		duplex:          duplex,
		rxFrequency:     uint32(rxFrequency),
		txFrequency:     uint32(txFrequency),
		power:           float32(power),
		afc:             afc,
		frequencyCorr:   int16(frequencyCorr),
		reflectorName:   reflectorName,
		reflectorAddr:   reflectorAddr,
		reflectorModule: reflectorModule,
		reflectorPort:   reflectorPort,
		logLevel:        logLevel,
		logPath:         logPath,
		logRoot:         logRoot,
		modemPort:       modemPort,
		modemSpeed:      modemSpeed,
		nRSTPin:         nRSTPin,
		paEnablePin:     paEnablePin,
		boot0Pin:        boot0Pin,
		symbolsIn:       symbolsIn,
		symbolsOut:      symbolsOut,
	}, err
}

var (
	inArg      *string = flag.String("in", "", "M17 symbol input (default stdin)")
	outArg     *string = flag.String("out", "", "M17 symbol output (default stdout)")
	configFile *string = flag.String("config", "./gateway.ini", "Configuration file")
	helpArg    *bool   = flag.Bool("h", false, "Print arguments")
)

func main() {
	var err error

	flag.Parse()

	if *helpArg {
		flag.Usage()
		return
	}
	cfg, err := loadConfig(*configFile, *inArg, *outArg)
	if err != nil {
		log.Fatalf("Bad configuration: %v", err)
	}

	setupLogging(cfg)

	var g *Gateway
	var modem *m17.CC1200Modem
	if cfg.modemPort != "" {
		modem, err = m17.NewCC1200Modem(cfg.modemPort, cfg.nRSTPin, cfg.paEnablePin, cfg.boot0Pin, cfg.modemSpeed)
		if err != nil {
			log.Fatalf("Error connecting to modem: %v", err)
		}
		modem.SetRXFreq(cfg.rxFrequency)
		modem.SetTXFreq(cfg.txFrequency)
		modem.SetTXPower(cfg.power)
		modem.SetFreqCorrection(cfg.frequencyCorr)
		modem.SetAFC(cfg.afc)
		log.Printf("[INFO] Connected to modem on %s", cfg.modemPort)
	}

	log.Printf("[DEBUG] Creating gateway cfg: %#v, modem %#v", cfg, modem)
	g, err = NewGateway(cfg, modem)
	if err != nil {
		log.Fatalf("Error creating Gateway: %v", err)
	}
	defer g.Close()
	g.Run()
}

func setupLogging(c config) {
	var err error
	minLogLevel := c.logLevel
	logWriter := os.Stderr

	if c.logRoot != "" {
		logWriter, err = os.OpenFile(c.logPath+"/"+c.logRoot+".log", os.O_WRONLY|os.O_CREATE|os.O_SYNC, 0644)
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

	modem  *m17.CC1200Modem
	in     *os.File
	out    *os.File
	relay  *m17.Relay
	duplex bool
	done   bool
}

func NewGateway(cfg config, modem *m17.CC1200Modem) (*Gateway, error) {
	var err error

	g := Gateway{
		Server: cfg.reflectorAddr,
		Port:   cfg.reflectorPort,
		Module: cfg.reflectorModule,
		modem:  modem,
		in:     cfg.symbolsIn,
		out:    cfg.symbolsOut,
		duplex: cfg.duplex,
	}

	log.Printf("[DEBUG] Connecting to %s:%d, module %s", g.Server, g.Port, g.Module)
	g.relay, err = m17.NewRelay(g.Server, g.Port, g.Module, cfg.callsign, g.FromRelay)
	if err != nil {
		return nil, fmt.Errorf("error creating relay: %v", err)
	}
	err = g.relay.Connect()
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s:%d %s: %v", g.Server, g.Port, g.Module, err)
	}

	modem.StartRX()

	return &g, nil
}

func (g Gateway) FromRelay(p m17.Packet) error {
	// log.Printf("[DEBUG] received packet from relay: %#v", p)
	if g.modem != nil {
		return p.Send(g.modem)
	}
	return p.Send(g.out)
}

func (g *Gateway) FromModem(lsfBytes []byte, packetBytes []byte) error {
	// log.Printf("[DEBUG] received packet from modem: % x", packetBytes)
	p := m17.NewPacketFromBytes(append(lsfBytes, packetBytes...))
	// log.Printf("[DEBUG] p: %#v", p)
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
	if g.modem != nil {
		go d.DecodeSymbols(g.modem, g.FromModem)
	} else {
		go d.DecodeSymbols(g.in, g.FromModem)
	}
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
