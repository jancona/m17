package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17"
	"gopkg.in/ini.v1"
	// _ "net/http/pprof"
)

// var callsignAll, _ = m17.EncodeCallsign("@ALL")

type config struct {
	callsign         string
	dashboardLogger  *slog.Logger
	duplex           bool
	rxFrequency      uint32
	txFrequency      uint32
	power            float32
	afc              bool
	frequencyCorr    int16
	defaultReflector string
	defaultModule    string
	logLevel         string
	logPath          string
	logRoot          string
	modemType        string
	modemCfg         *ini.Section
	// modemPort        string
	// modemSpeed       int
	// nRSTPin          int
	// boot0Pin         int
	symbolsIn        *os.File
	symbolsOut       *os.File
	hostfile         *m17.Hostfile
	overrideHostfile *m17.Hostfile
	audioDir         string
}

func loadConfig(iniFile string, inFile string, outFile string) (config, error) {
	log.Printf("[INFO] Loading settings from '%s'", iniFile)
	cfg, err := ini.Load(iniFile)
	if err != nil {
		log.Fatalf("Fail to read config from %s: %v", iniFile, err)
	}
	callsign := cfg.Section("General").Key("Callsign").String()
	dashboardLog := cfg.Section("General").Key("DashboardLog").String()
	audioDir := cfg.Section("General").Key("AudioDir").MustString("audio/")
	if !filepath.IsAbs(audioDir) {
		exe, err := os.Executable()
		if err != nil {
			log.Fatalf("Unable to find executable location: %v", err)
		}
		audioDir = filepath.Join(filepath.Dir(exe), audioDir)
	}

	rxFrequency, rxFrequencyErr := cfg.Section("Radio").Key("RXFrequency").Uint()
	txFrequency, txFrequencyErr := cfg.Section("Radio").Key("TXFrequency").Uint()
	power, powerErr := cfg.Section("Radio").Key("Power").Float64()
	afc, afcErr := cfg.Section("Radio").Key("AFC").Bool()
	frequencyCorr, frequencyCorrErr := cfg.Section("Radio").Key("FrequencyCorr").Int()
	duplex, duplexErr := cfg.Section("Radio").Key("Duplex").Bool()

	hostFile := cfg.Section("Reflector").Key("HostFile").String()
	overrideHostFile := cfg.Section("Reflector").Key("OverrideHostFile").String()
	reflectorName := cfg.Section("Reflector").Key("Name").String()
	reflectorModule := cfg.Section("Reflector").Key("Module").String()
	logLevel := cfg.Section("Log").Key("Level").String()
	logPath := cfg.Section("Log").Key("Path").String()
	logRoot := cfg.Section("Log").Key("Root").String()
	var modemType string
	var modemTypeErr error
	if !cfg.Section("Modem").HasKey("Type") {
		cfg.Section("Modem").Key("Type").SetValue("cc1200")
	}
	modemType = cfg.Section("Modem").Key("Type").In("BAD", []string{"cc1200", "cc1200v2", "mmdvm", "dummy", "sx1255"})
	if modemType == "BAD" {
		modemTypeErr = fmt.Errorf("bad Modem Type: %s", cfg.Section("Modem").Key("Type").String())
	}
	modemCfg := cfg.Section("Modem")

	callsign = m17.NormalizeCallsignModule(callsign)
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

	var reflectorHostfile, reflectorOverrideHostfile *m17.Hostfile
	var reflectorHostfileErr, reflectorOverrideHostfileErr error
	if hostFile != "" {
		reflectorHostfile, reflectorHostfileErr = m17.NewHostfile(hostFile)
	}
	if overrideHostFile != "" {
		reflectorOverrideHostfile, reflectorOverrideHostfileErr = m17.NewHostfile(overrideHostFile)
	}
	var reflectorModuleErr error
	if len(reflectorModule) > 1 {
		reflectorModuleErr = fmt.Errorf("configured Reflector Module must be zero or one character")
	}
	if reflectorModule == " " {
		reflectorModule = ""
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

	var dashboardLogFile *os.File
	var dashboardLogErr error
	var dashboardLogger *slog.Logger
	if dashboardLog != "" {
		dashboardLogFile, dashboardLogErr = os.OpenFile(dashboardLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if dashboardLogFile != nil {
			opts := &slog.HandlerOptions{
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					if a.Key == slog.LevelKey || a.Key == slog.MessageKey {
						return slog.Attr{} // Remove the attribute
					}
					return a
				},
			}
			dashboardLogger = slog.New(slog.NewJSONHandler(dashboardLogFile, opts))
		}
	}

	err = errors.Join(
		rxFrequencyErr,
		txFrequencyErr,
		powerErr,
		afcErr,
		frequencyCorrErr,
		duplexErr,
		modemTypeErr,
		// modemSpeedErr,
		// nRSTPinErr,
		// boot0PinErr,
		callsignErr,
		reflectorModuleErr,
		logLevelErr,
		symbolsInErr,
		symbolsOutErr,
		dashboardLogErr,
		reflectorHostfileErr,
		reflectorOverrideHostfileErr,
	)

	return config{
		callsign:         callsign,
		duplex:           duplex,
		rxFrequency:      uint32(rxFrequency),
		txFrequency:      uint32(txFrequency),
		power:            float32(power),
		afc:              afc,
		frequencyCorr:    int16(frequencyCorr),
		defaultReflector: reflectorName,
		defaultModule:    reflectorModule,
		logLevel:         logLevel,
		logPath:          logPath,
		logRoot:          logRoot,
		modemType:        modemType,
		modemCfg:         modemCfg,
		// modemPort:        modemPort,
		// modemSpeed:       modemSpeed,
		// nRSTPin:          nRSTPin,
		// boot0Pin:         boot0Pin,
		symbolsIn:        symbolsIn,
		symbolsOut:       symbolsOut,
		dashboardLogger:  dashboardLogger,
		hostfile:         reflectorHostfile,
		overrideHostfile: reflectorOverrideHostfile,
		audioDir:         audioDir,
	}, err
}

var (
	inArg      *string = flag.String("in", "", "M17 symbol input (default stdin)")
	outArg     *string = flag.String("out", "", "M17 symbol output (default stdout)")
	configFile *string = flag.String("config", "./gateway.ini", "Configuration file")
	reset      *bool   = flag.Bool("reset", false, "Reset modem and exit")
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

	// // Server for pprof
	// go func() {
	// 	fmt.Println(http.ListenAndServe(":6060", nil))
	// }()

	var g *Gateway
	var modem m17.Modem
	switch cfg.modemType {
	case "cc1200":
		fallthrough
	case "cc1200v2":
		modem, err = m17.NewCC1200Modem(cfg.rxFrequency, cfg.txFrequency, int8(cfg.power), cfg.frequencyCorr, cfg.afc, cfg.modemCfg)
		if err != nil {
			log.Fatalf("Error creating CC1200 modem: %v", err)
		}
		log.Printf("[INFO] Connected to CC1200 modem on %s", cfg.modemCfg.Key("Port").String())
	case "mmdvm":
		modem, err = m17.NewMMDVMModem(cfg.rxFrequency, cfg.txFrequency, cfg.power, cfg.frequencyCorr, cfg.afc, cfg.modemCfg, cfg.duplex)
		if err != nil {
			log.Fatalf("Error creating MMDVM modem: %v", err)
		}
		log.Printf("[INFO] Connected to MMDVM modem on %s", cfg.modemCfg.Key("Port").String())
	case "sx1255":
		modem, err = m17.NewSX1255Modem(cfg.rxFrequency, cfg.txFrequency, cfg.frequencyCorr, cfg.modemCfg)
		if err != nil {
			log.Fatalf("Error creating SX1255 modem: %v", err)
		}
		log.Printf("[INFO] Connected to SX1255 modem on %s", cfg.modemCfg.Key("SPIDevice").MustString("/dev/spidev0.0"))
	case "dummy":
		modem = &m17.DummyModem{
			In:  cfg.symbolsIn,
			Out: cfg.symbolsOut,
		}
	}

	if *reset {
		log.Print("[INFO] Resetting modem")
		err = modem.Reset()
		if err != nil {
			log.Printf("[ERROR] Error resetting modem: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
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
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	// log.SetFlags(0)
	log.Print("[DEBUG] Debug is on")
}

type gatewayState int

const (
	Idle gatewayState = iota
	RFStreamRX
	RFPacketRX
	NetStreamRX
	NetPacketRX
	Echo
	LocalCommand
)

// Gateway connects to a reflector, converts traffic to/from audio format on stdout,
// so it can be used in a pipeline with other tools
type Gateway struct {
	Name   string
	Server string
	Port   uint
	Module string

	modem            m17.Modem
	in               *os.File
	out              *os.File
	inetClient       *m17.InetClient
	duplex           bool
	done             bool
	dashLog          *m17.DashboardLogger
	hostfile         *m17.Hostfile
	overrideHostfile *m17.Hostfile
	encodedCallsign  m17.EncodedCallsign
	callsign         string
	stateMutex       sync.Mutex
	state            gatewayState

	lastFrameTimer *time.Timer
	lastLSF        *m17.LSF // Workaround for reflectors that change the SRC during the stream
	lastStreamID   uint16
	echoStream     []m17.StreamDatagram
	audioClips     map[string][]byte
}

func NewGateway(cfg config, modem m17.Modem) (*Gateway, error) {
	var err error
	cs, err := m17.EncodeCallsign(cfg.callsign)
	if err != nil {
		return nil, fmt.Errorf("bad callsign %s: %w", cfg.callsign, err)
	}

	g := Gateway{
		Name:             cfg.defaultReflector,
		Module:           cfg.defaultModule,
		modem:            modem,
		duplex:           cfg.duplex,
		dashLog:          m17.NewDashboardLogger(cfg.dashboardLogger),
		hostfile:         cfg.hostfile,
		overrideHostfile: cfg.overrideHostfile,
		encodedCallsign:  *cs,
		callsign:         cfg.callsign,
		state:            Idle,
		lastStreamID:     0xFFFF,
	}
	err = g.loadAudioClips(cfg.audioDir, cfg.callsign)
	if err != nil {
		return nil, err
	}
	h, ok := g.overrideHostfile.Hosts[g.Name]
	if !ok {
		h, ok = g.hostfile.Hosts[g.Name]
		if !ok {
			return nil, fmt.Errorf("reflector %s not found", g.Name)
		}
	}
	g.Server = h.Server
	g.Port = h.Port
	log.Printf("[DEBUG] Connecting to %s, %s:%d, module %s", g.Name, g.Server, g.Port, g.Module)
	g.inetClient, err = m17.NewInetClient(g.Name, g.Server, g.Port, g.Module, cfg.callsign, m17.NewDashboardLogger(cfg.dashboardLogger), g.TransmitPacket, g.TransmitVoiceStream)
	if err != nil {
		return nil, fmt.Errorf("error creating client: %v", err)
	}
	err = g.inetClient.Connect()
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s %s:%d %s: %v", g.Name, g.Server, g.Port, g.Module, err)
	}

	modem.Start()

	return &g, nil
}

func (g *Gateway) TransmitPacket(p m17.Packet) error {
	lsf := *p.LSF
	// Replace META with Extended Callsign Data
	// Don't swap Src for Packet
	p.LSF.SetECD(&g.encodedCallsign, g.inetClient.EncodedName)
	//	p.LSF.Src = g.encodedCallsign
	err := g.modem.TransmitPacket(p)
	if err != nil {
		log.Printf("[ERROR] Error transmitting packet: %v", err)
		return err
	}
	// log.Printf("[DEBUG] received packet from server: %#v", p)
	if p.Type == m17.PacketTypeSMS && len(p.Payload) > 0 {
		msg := string(p.Payload[0 : len(p.Payload)-1])
		g.dashLog.LogFrame(&lsf, "Internet", "Packet", "packetType", p.Type, "smsMessage", msg)
	} else {
		g.dashLog.LogFrame(&lsf, "Internet", "Packet", "packetType", p.Type)
	}
	g.dashLog.LogGNSS(&lsf, "Internet")

	return nil
}

func (g *Gateway) TransmitVoiceStream(sd m17.StreamDatagram) error {
	// Make a copy
	lsf := *sd.LSF
	// log.Printf("[DEBUG] Handle StreamDatagram id: %04x, lastStreamID: %04x, fn: %04x, last: %v", sd.StreamID, g.lastStreamID, sd.FrameNumber, sd.LastFrame)
	// Shouldn't need the next line with modern reflectors
	// sd.LSF.Dst = *callsignAll
	// Replace META with Extended Callsign Data
	sd.LSF.SetECD(&sd.LSF.Src, g.inetClient.EncodedName)
	sd.LSF.Src = g.encodedCallsign
	sd.LSF.CalcCRC()
	err := g.modem.TransmitVoiceStream(sd)
	if err != nil {
		log.Printf("[ERROR] Error transmitting voice stream: %v", err)
		return err
	}
	if g.lastFrameTimer != nil {
		g.lastFrameTimer.Reset(time.Second)
	}
	// log.Printf("[DEBUG] received voice stream data from server: %#v", sd)
	if g.lastStreamID != sd.StreamID {
		if g.lastFrameTimer != nil {
			g.lastFrameTimer.Stop()
		}
		log.Printf("[DEBUG] Start Internet voice stream: %s", sd)
		g.dashLog.LogFrame(&lsf, "Internet", "Voice Start")
		g.lastStreamID = sd.StreamID
		g.lastLSF = &lsf
		// Provide a backstop if we don't receive a last frame packet
		g.lastFrameTimer = time.AfterFunc(time.Second, func() {
			log.Printf("[DEBUG] Timed out Internet voice stream %04x", sd.StreamID)
			g.dashLog.LogFrame(g.lastLSF, "Internet", "Voice End")
			g.lastStreamID = 0xFFFF
			g.lastFrameTimer = nil
			g.lastLSF = nil
			if g.getState() == Echo {
				g.echoStreamEnd()
			}
		})
	}
	g.dashLog.LogGNSS(&lsf, "Internet")
	if sd.LastFrame {
		log.Printf("[DEBUG] End Internet voice stream: %s", sd)
		g.dashLog.LogFrame(g.lastLSF, "Internet", "Voice End")
		g.lastStreamID = 0xFFFF
		g.lastLSF = nil
		g.lastFrameTimer.Stop()
		g.lastFrameTimer = nil
	}
	return err
}

func (g *Gateway) getState() gatewayState {
	g.stateMutex.Lock()
	defer g.stateMutex.Unlock()
	return g.state
}

func (g *Gateway) setState(state gatewayState) {
	// log.Printf("[DEBUG] setState(%v)", state)
	g.stateMutex.Lock()
	defer g.stateMutex.Unlock()
	g.state = state
}

func (g *Gateway) receivedRFLSF(lsf m17.LSF, ber float64) error {
	if g.getState() == Idle &&
		lsf.Type[1]&byte(m17.LSFTypeStream) == byte(m17.LSFTypeStream) {
		g.dashLog.LogFrame(&lsf, "RF", "Voice Start", "mer", json.Number(fmt.Sprintf("%f", ber)))
		g.dashLog.LogGNSS(&lsf, "RF")
		switch lsf.Dst.Callsign() {
		case "/ECHO", "#ECHO":
			log.Printf("[DEBUG] receivedRFLSF() Echo")
			g.setState(Echo)
			g.echoStreamStart()
		case "/INFO", "#INFO":
			log.Printf("[DEBUG] receivedRFLSF() Info")
			g.setState(LocalCommand)
		default:
			log.Printf("[DEBUG] receivedRFLSF() RFStream: %s", lsf.Dst.Callsign())
			g.setState(RFStreamRX)
		}
		// TODO: Should we be sending the RF LSF here?
	}
	return nil
}
func (g *Gateway) receivedRFStreamFrame(lsf m17.LSF, payload []byte, sid, fn uint16, ber float64) error {
	var err error
	sd := m17.NewStreamDatagram(sid, fn, &lsf, payload)
	switch g.getState() {
	case Echo:
		g.echoStreamRecord(sd)
	case RFStreamRX:
		err = g.inetClient.SendStream(sd)
		if g.duplex {
			// Replace META with Extended Callsign Data
			sd.LSF.SetECD(&sd.LSF.Src, nil)
			sd.LSF.Src = g.encodedCallsign
			err2 := g.modem.TransmitVoiceStream(sd)
			err = errors.Join(err, err2)
		}
	}
	// TODO: Handle error?
	return err
}
func (g *Gateway) receivedRFStreamLICH(lsf m17.LSF, ber float64) error {
	if g.getState() == RFStreamRX {
		g.dashLog.LogGNSS(&lsf, "RF")
	}
	return nil
}
func (g *Gateway) receivedRFStreamEOT(lsf m17.LSF, sid, fn uint16, ber float64) error {
	switch g.getState() {
	case Echo:
		go g.echoStreamEnd()
	case LocalCommand:
		switch lsf.Dst.Callsign() {
		case "/INFO", "#INFO":
			go g.playMessage("welcome", "callsign", "is_linked_to", g.inetClient.Name+" "+string(g.inetClient.Module))
		}
	case RFStreamRX:
		log.Printf("[DEBUG] receivedRFStreamEOT() setState(Idle)")
		g.setState(Idle)
	}
	g.dashLog.LogFrame(&lsf, "RF", "Voice End", "mer", json.Number(fmt.Sprintf("%f", ber)))
	return nil
}
func (g *Gateway) receivedRFPacket(lsf m17.LSF, payload []byte, ber float64) error {
	var err error
	p := m17.NewPacketFromBytes(append(lsf.ToBytes(), payload...))
	g.dashLog.LogGNSS(&lsf, "RF")
	if p.Type == m17.PacketTypeSMS && len(p.Payload) > 0 {
		msg := string(p.Payload[0 : len(p.Payload)-1])
		g.dashLog.LogFrame(&lsf, "RF", "Packet", "mer", json.Number(fmt.Sprintf("%f", ber)), "packetType", p.Type, "smsMessage", msg)
	} else {
		g.dashLog.LogFrame(&lsf, "RF", "Packet", "mer", json.Number(fmt.Sprintf("%f", ber)), "packetType", p.Type)
	}
	switch lsf.Dst.Callsign() {
	case "/ECHO", "#ECHO":
		g.setState(Echo)
		log.Printf("[DEBUG] receivedRFPacket() Echo")
		go g.echoPacket(p)
	case "/INFO", "#INFO":
		g.setState(LocalCommand)
		log.Printf("[DEBUG] receivedRFPacket() Info")
		go g.infoPacket(p)
	default:
		log.Printf("[DEBUG] receivedRFPacket() packet dst: %s", lsf.Dst.Callsign())
		err = g.inetClient.SendPacket(p)
		if err == nil && g.duplex {
			// Replace META with Extended Callsign Data
			// Don't swap Src for packet
			p.LSF.SetECD(&g.encodedCallsign, nil)
			// p.LSF.Src = g.encodedCallsign
			err = g.modem.TransmitPacket(p)
		}
	}
	return err
}

func (g *Gateway) Run() {
	signalChan := make(chan os.Signal, 1)
	d := m17.NewDecoder(
		g.receivedRFLSF,
		g.receivedRFStreamFrame,
		g.receivedRFStreamLICH,
		g.receivedRFStreamEOT,
		g.receivedRFPacket,
	)
	g.modem.StartDecoding(d.DecodeFrame)
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
	g.inetClient.Close()
	if g.modem != nil {
		g.modem.Close()
	}
	if g.in != os.Stdin {
		g.in.Close()
	}
	if g.out != os.Stdout {
		g.out.Close()
	}
}

func (g *Gateway) echoPacket(p m17.Packet) error {
	defer g.setState(Idle)
	var err error
	log.Printf("[DEBUG] echoPacket(%v)", p)
	p.LSF.Dst = p.LSF.Src
	p.LSF.Src = g.encodedCallsign
	p.LSF.CalcCRC()
	err = g.modem.TransmitPacket(p)
	return err
}

func (g *Gateway) infoPacket(p m17.Packet) error {
	defer g.setState(Idle)
	var err error
	log.Printf("[DEBUG] infoPacket(%v)", p)
	p.LSF.Dst = p.LSF.Src
	p.LSF.Src = g.encodedCallsign
	p.LSF.CalcCRC()
	msg := g.callsign + " is linked to " + g.inetClient.Name + " " + string(g.inetClient.Module)
	p.Payload = append(([]byte)(msg), 0) // NULL terminate the string
	p.CalcCRC()
	err = g.modem.TransmitPacket(p)
	return err
}
