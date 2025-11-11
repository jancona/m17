package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17"
	"gopkg.in/ini.v1"
	// _ "net/http/pprof"
)

var callsignAll, _ = m17.EncodeCallsign("@ALL")

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
}

func loadConfig(iniFile string, inFile string, outFile string) (config, error) {
	log.Printf("[INFO] Loading settings from '%s'", iniFile)
	cfg, err := ini.Load(iniFile)
	if err != nil {
		log.Fatalf("Fail to read config from %s: %v", iniFile, err)
	}
	callsign := cfg.Section("General").Key("Callsign").String()
	dashboardLog := cfg.Section("General").Key("DashboardLog").String()
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
	modemType = cfg.Section("Modem").Key("Type").In("BAD", []string{"cc1200", "mmdvm", "dummy"})
	if modemType == "BAD" {
		modemTypeErr = fmt.Errorf("bad Modem Type: %s", cfg.Section("Modem").Key("Type").String())
	}
	modemCfg := cfg.Section("Modem")

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
		modem, err = m17.NewCC1200Modem(cfg.rxFrequency, cfg.txFrequency, cfg.power, cfg.frequencyCorr, cfg.afc, cfg.modemCfg)
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
	relay            *m17.Relay
	duplex           bool
	done             bool
	dashboardLogger  *slog.Logger
	hostfile         *m17.Hostfile
	overrideHostfile *m17.Hostfile
	encodedCallsign  m17.EncodedCallsign

	lastLogTime    time.Time
	lastFrameTimer *time.Timer
	lastLSF        *m17.LSF // Workaround for reflectors that change the SRC during the stream
	lastStreamID   uint16
	echoMode       bool
	echoStream     []m17.StreamDatagram
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
		dashboardLogger:  cfg.dashboardLogger,
		hostfile:         cfg.hostfile,
		overrideHostfile: cfg.overrideHostfile,
		encodedCallsign:  *cs,
		lastStreamID:     0xFFFF,
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
	g.relay, err = m17.NewRelay(g.Name, g.Server, g.Port, g.Module, cfg.callsign, cfg.dashboardLogger, g.TransmitPacket, g.TransmitVoiceStream)
	if err != nil {
		return nil, fmt.Errorf("error creating relay: %v", err)
	}
	err = g.relay.Connect()
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s %s:%d %s: %v", g.Name, g.Server, g.Port, g.Module, err)
	}

	modem.Start()

	return &g, nil
}

func (g *Gateway) TransmitPacket(p m17.Packet) error {
	// log.Printf("[DEBUG] received packet from relay: %#v", p)
	if p.Type == m17.PacketTypeSMS && len(p.Payload) > 0 {
		msg := string(p.Payload[0 : len(p.Payload)-1])
		g.dashboardLogger.Info("", "type", "Internet", "subtype", "Packet", "src", p.LSF.Src.Callsign(), "dst", p.LSF.Dst.Callsign(), "can", p.LSF.CAN(), "packetType", p.Type, "smsMessage", msg)
	} else {
		g.dashboardLogger.Info("", "type", "Internet", "subtype", "Packet", "src", p.LSF.Src.Callsign(), "dst", p.LSF.Dst.Callsign(), "can", p.LSF.CAN(), "packetType", p.Type)
	}

	return g.modem.TransmitPacket(p)
}

func (g *Gateway) TransmitVoiceStream(sd m17.StreamDatagram) error {
	// log.Printf("[DEBUG] received voice stream data from relay: %#v", sd)
	gnss := sd.LSF.GNSS()
	sd.LSF.Dst = *callsignAll
	// Replace META with Extended Callsign Data
	sd.LSF.SetECD(g.encodedCallsign)
	// log.Printf("[DEBUG] Handle StreamDatagram id: %04x, lastStreamID: %04x, fn: %04x, last: %v", sd.StreamID, g.lastStreamID, sd.FrameNumber, sd.LastFrame)
	err := g.modem.TransmitVoiceStream(sd)
	if g.lastFrameTimer != nil {
		g.lastFrameTimer.Reset(time.Second)
	}
	if g.dashboardLogger != nil && g.lastStreamID != sd.StreamID {
		if g.lastFrameTimer != nil {
			g.lastFrameTimer.Stop()
		}
		log.Printf("[DEBUG] Start Internet voice stream: %s", sd)
		g.dashboardLogger.Info("", "type", "Internet", "subtype", "Voice Start", "src", sd.LSF.Src.Callsign(), "dst", sd.LSF.Dst.Callsign(), "can", sd.LSF.CAN())
		g.lastStreamID = sd.StreamID
		g.lastLSF = sd.LSF
		// Provide a backstop if we don't receive a last frame packet
		g.lastFrameTimer = time.AfterFunc(time.Second, func() {
			log.Printf("[DEBUG] Timed out Internet voice stream %04x", sd.StreamID)
			g.dashboardLogger.Info("", "type", "Internet", "subtype", "Voice End", "src", sd.LSF.Src.Callsign(), "dst", sd.LSF.Dst.Callsign(), "can", sd.LSF.CAN())
			g.lastStreamID = 0xFFFF
			g.lastFrameTimer = nil
			g.lastLSF = nil
			if g.echoMode {
				g.echoEnd()
			}
		})
	}
	if g.dashboardLogger != nil && gnss != nil && gnss.ValidAltitude && time.Since(g.lastLogTime) > 15*time.Second {
		g.lastLogTime = time.Now()
		args := []any{
			"type", "Internet",
			"subtype", "GNSS",
			"dataSource", gnss.DataSource,
			"stationType", gnss.StationType,
			"src", sd.LSF.Src.Callsign(),
			"latitude", json.Number(fmt.Sprintf("%f", gnss.Latitude)),
			"longitude", json.Number(fmt.Sprintf("%f", gnss.Longitude)),
		}
		if gnss.ValidAltitude {
			args = append(args,
				"altitude", json.Number(fmt.Sprintf("%.1f", gnss.Altitude)),
			)
		}
		if gnss.ValidBearingSpeed {
			args = append(args,
				"speed", json.Number(fmt.Sprintf("%.1f", gnss.Speed)),
				"bearing", gnss.Bearing,
			)
		}
		if gnss.ValidRadius {
			args = append(args,
				"radius", gnss.Radius,
			)
		}
		g.dashboardLogger.Info("", args...)
	}
	if g.dashboardLogger != nil && sd.LastFrame {
		log.Printf("[DEBUG] End Internet voice stream: %s", sd)
		g.dashboardLogger.Info("", "type", "Internet", "subtype", "Voice End", "src", g.lastLSF.Src.Callsign(), "dst", g.lastLSF.Dst.Callsign(), "can", g.lastLSF.CAN())
		g.lastStreamID = 0xFFFF
		g.lastLSF = nil
		g.lastFrameTimer.Stop()
		g.lastFrameTimer = nil
		if g.echoMode {
			err = g.echoEnd()
		}
	}
	return err
}

func (g *Gateway) receivedRFLSF(lsf *m17.LSF, ber float64) error {
	if lsf.Type[1]&byte(m17.LSFTypeStream) == byte(m17.LSFTypeStream) {
		g.dashboardLogger.Info("", "type", "RF", "subtype", "Voice Start", "src", lsf.Src.Callsign(), "dst", lsf.Dst.Callsign(), "can", lsf.CAN(), "mer", json.Number(fmt.Sprintf("%f", ber)))
		gnss := lsf.GNSS()
		if gnss != nil && gnss.ValidLatLon {
			g.lastLogTime = time.Now()
			args := []any{
				"type", "RF",
				"subtype", "GNSS",
				"src", lsf.Src.Callsign(),
				"dataSource", gnss.DataSource,
				"stationType", gnss.StationType,
				"latitude", json.Number(fmt.Sprintf("%f", gnss.Latitude)),
				"longitude", json.Number(fmt.Sprintf("%f", gnss.Longitude)),
			}
			if gnss.ValidAltitude {
				args = append(args,
					"altitude", json.Number(fmt.Sprintf("%.1f", gnss.Altitude)),
				)
			}
			if gnss.ValidBearingSpeed {
				args = append(args,
					"speed", json.Number(fmt.Sprintf("%.1f", gnss.Speed)),
					"bearing", gnss.Bearing,
				)
			}
			if gnss.ValidRadius {
				args = append(args,
					"radius", gnss.Radius,
				)
			}
			g.dashboardLogger.Info("", args...)
		}
	}
	switch lsf.Dst.Callsign() {
	case "ECHO", "#ECHO":
		g.echoStart()
	}
	// Replace META with Extended Callsign Data
	lsf.SetECD(g.encodedCallsign)
	// TODO: Should we be sending the RF LSF here?
	return nil
}
func (g *Gateway) receivedRFStreamFrame(lsf *m17.LSF, payload []byte, sid, fn uint16, ber float64) error {
	var err error
	if lsf == nil {
		return fmt.Errorf("nil lsf in receivedStreamRF")
	}
	sd := m17.NewStreamDatagram(sid, fn, lsf, payload)
	if g.echoMode {
		g.echoRecord(sd)
	} else {
		err = g.relay.SendStream(sd)
		if g.duplex {
			err2 := g.modem.TransmitVoiceStream(sd)
			err = errors.Join(err, err2)
		}
	}
	// TODO: Handle error?
	return err
}
func (g *Gateway) receivedRFStreamLICH(lsf *m17.LSF, ber float64) error {
	gnss := lsf.GNSS()
	if g.dashboardLogger != nil &&
		gnss != nil &&
		gnss.ValidLatLon &&
		time.Since(g.lastLogTime) > 15*time.Second {
		g.lastLogTime = time.Now()
		args := []any{
			"type", "RF",
			"subtype", "GNSS",
			"dataSource", gnss.DataSource,
			"stationType", gnss.StationType,
			"src", lsf.Src.Callsign(),
			"latitude", json.Number(fmt.Sprintf("%f", gnss.Latitude)),
			"longitude", json.Number(fmt.Sprintf("%f", gnss.Longitude)),
		}
		if gnss.ValidAltitude {
			args = append(args,
				"altitude", json.Number(fmt.Sprintf("%.1f", gnss.Altitude)),
			)
		}
		if gnss.ValidBearingSpeed {
			args = append(args,
				"speed", json.Number(fmt.Sprintf("%.1f", gnss.Speed)),
				"bearing", gnss.Bearing,
			)
		}
		if gnss.ValidRadius {
			args = append(args,
				"radius", gnss.Radius,
			)
		}
		g.dashboardLogger.Info("", args...)
	}
	return nil
}
func (g *Gateway) receivedRFStreamEOT(lsf *m17.LSF, sid, fn uint16, ber float64) error {
	var err error
	g.dashboardLogger.Info("", "type", "RF", "subtype", "Voice End",
		"src", lsf.Src.Callsign(), "dst", lsf.Dst.Callsign(), "can", lsf.CAN(),
		"mer", json.Number(fmt.Sprintf("%f", ber)))
	if g.echoMode {
		err = g.echoEnd()
	}
	return err
}
func (g *Gateway) receivedRFPacket(lsf *m17.LSF, payload []byte, ber float64) error {
	var err error
	p := m17.NewPacketFromBytes(append(lsf.ToBytes(), payload...))
	if g.dashboardLogger != nil {
		if p.Type == m17.PacketTypeSMS && len(p.Payload) > 0 {
			msg := string(p.Payload[0 : len(p.Payload)-1])
			g.dashboardLogger.Info("", "type", "RF", "subtype", "Packet",
				"src", lsf.Src.Callsign(), "dst", lsf.Dst.Callsign(), "can", lsf.CAN(),
				"mer", json.Number(fmt.Sprintf("%f", ber)),
				"packetType", p.Type, "smsMessage", msg)
		} else {
			g.dashboardLogger.Info("", "type", "RF", "subtype", "Packet",
				"src", lsf.Src.Callsign(), "dst", lsf.Dst.Callsign(), "can", lsf.CAN(),
				"mer", json.Number(fmt.Sprintf("%f", ber)),
				"packetType", p.Type)
		}
	}
	if g.echoMode {
		err = g.echoPacket(p)
	} else {
		// log.Printf("[DEBUG] send packet to reflector/relay: %v", p)
		err = g.relay.SendPacket(p)
		if err == nil && g.duplex {
			err = g.modem.TransmitPacket(p)
		}
	}
	return err
}

func (g *Gateway) echoStart() {
	log.Printf("[DEBUG] echoStart()")
	g.echoMode = true
	// g.echoStream = make([]m17.StreamDatagram, 0, 10*m17.FramesPerSecond)
	g.echoStream = make([]m17.StreamDatagram, 0)
}
func (g *Gateway) echoRecord(sd m17.StreamDatagram) {
	if g.echoMode {
		// log.Printf("[DEBUG] echoRecord(%v)", sd)
		g.echoStream = append(g.echoStream, sd)
	}
}
func (g *Gateway) echoPacket(p m17.Packet) error {
	var err error
	if g.echoMode {
		log.Printf("[DEBUG] echoPacket(%v)", p)
		p.LSF.Dst = p.LSF.Src
		p.LSF.Src = g.encodedCallsign
		p.LSF.CalcCRC()
		err = g.modem.TransmitPacket(p)
	}
	g.echoMode = false
	return err
}
func (g *Gateway) echoEnd() error {
	var err error
	if g.echoMode {
		log.Printf("[DEBUG] echoEnd() %d frames", len(g.echoStream))
		time.Sleep(250 * time.Millisecond)
		sid := uint16(rand.Intn(0x10000))
		for _, sd := range g.echoStream {
			sd.StreamID = sid
			sd.LSF.Dst = sd.LSF.Src
			sd.LSF.Src = g.encodedCallsign
			sd.LSF.CalcCRC()
			// log.Printf("[DEBUG] echoEnd id: %04x, fn: %04x, last: %v, payload: [% 02x]", sd.StreamID, sd.FrameNumber, sd.LastFrame, sd.Payload)
			err = g.modem.TransmitVoiceStream(sd)
			if err != nil {
				break
			}
		}
	}
	g.echoStream = nil
	g.echoMode = false
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
	g.relay.Close()
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
