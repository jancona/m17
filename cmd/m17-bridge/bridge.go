package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17/server"
	"gopkg.in/ini.v1"
)

type config struct {
	name          string
	listenAddress string
	listenPort    string
	modules       map[byte]*ini.Section
	logLevel      string
	logPath       string
	logRoot       string
}

func loadConfig(iniFile string) (*config, error) {
	log.Printf("[INFO] Loading settings from '%s'", iniFile)
	cfg, err := ini.Load(iniFile)
	if err != nil {
		log.Fatalf("Fail to read config from %s: %v", iniFile, err)
	}
	name := cfg.Section("General").Key("Name").String()
	listenAddress := cfg.Section("General").Key("ListenAddress").String()
	listenPort := cfg.Section("General").Key("ListenPort").String()
	mods := cfg.Section("General").Key("Modules").String()
	modules := map[byte]*ini.Section{}
	for _, m := range []byte(mods) {
		s := cfg.Section("Module-" + string(m))
		if s == nil {
			return nil, fmt.Errorf("missing configuration section [Module-%s]", string(m))
		}
		modules[m] = s
	}

	logLevel := cfg.Section("Log").Key("Level").String()
	logPath := cfg.Section("Log").Key("Path").String()
	logRoot := cfg.Section("Log").Key("Root").String()
	var logLevelErr error
	if logLevel != "ERROR" && logLevel != "INFO" && logLevel != "DEBUG" {
		logLevelErr = fmt.Errorf("configured Log Level must be one of ERROR, INFO or DEBUG")
	}

	err = errors.Join(
		logLevelErr,
	)

	c := config{
		name:          name,
		listenAddress: listenAddress,
		listenPort:    listenPort,
		modules:       modules,
		logLevel:      logLevel,
		logPath:       logPath,
		logRoot:       logRoot,
	}
	return &c, err
}

var (
	configFile *string = flag.String("config", "./m17-bridge.ini", "Configuration file")
	helpArg    *bool   = flag.Bool("h", false, "Print arguments")
)

func main() {
	var err error

	flag.Parse()

	if *helpArg {
		flag.Usage()
		return
	}
	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Bad configuration: %v", err)
	}

	setupLogging(cfg)

	var b *Bridge
	log.Printf("[DEBUG] Creating Bridge cfg: %#v", cfg)
	b, err = NewBridge(cfg)
	if err != nil {
		log.Fatalf("Error creating Bridge: %v", err)
	}
	defer b.Close()
	b.Run()
}

func setupLogging(c *config) {
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

type Bridge struct {
	server *server.InetServer
}

func NewBridge(cfg *config) (*Bridge, error) {
	var err error
	ret := Bridge{}
	modules := map[byte]server.Module{}
	ret.server = server.NewInetServer(cfg.name, cfg.listenAddress+":"+cfg.listenPort, modules)
	for k, m := range cfg.modules {
		switch m.Key("Type").String() {
		case "Discord":
			modules[k], err = server.NewDiscordModule(
				k,
				ret.server,
				m.Key("ChannelName").String(),
				m.Key("WebhookURL").String(),
				m.Key("BotToken").String(),
			)
			if err != nil {
				return nil, err
			}
		case "IRC":
			port, err := m.Key("Port").Uint()
			if err != nil {
				return nil, err
			}
			useTLS, err := m.Key("UseTLS").Bool()
			if err != nil {
				return nil, err
			}
			modules[k], err = server.NewIRCModule(
				k,
				ret.server,
				m.Key("Server").String(),
				port,
				useTLS,
				m.Key("ServerPassword").String(),
			)
			if err != nil {
				return nil, err
			}
		case "APRS":
			staleMinutes := m.Key("StaleMinutes").MustInt(60)
			modules[k], err = server.NewAPRSModule(
				k,
				ret.server,
				m.Key("Server").String(),
				m.Key("Callsign").String(),
				m.Key("Symbol").String(),
				time.Duration(staleMinutes)*time.Minute,
			)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown module type '%s'", m.Key("Type").String())
		}
	}
	return &ret, nil
}
func (b *Bridge) Run() {
	b.server.Start()
}
func (b *Bridge) Close() {
	b.server.Close()
}
