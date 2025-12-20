package m17

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"sync"
	"time"

	"go.bug.st/serial"
	"gopkg.in/ini.v1"
)

var (
	ErrMMDVMReadTimeout         = errors.New("read timeout")
	ErrUnsupportedModemProtocol = errors.New("unsupported MMDVM protocol version")
	ErrModemNAK                 = errors.New("modem returned NAK")
)

var mmdvmValidSpeeds = []int{1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800}

const (
	mmdvmFrameStart   = 0xE0
	mmdvmGetVersion   = 0x00
	mmdvmGetStatus    = 0x01
	mmdvmSetConfig    = 0x02
	mmdvmSetMode      = 0x03
	mmdvmSetFrequency = 0x04

	mmdvmM17LinkSetup = 0x45
	mmdvmM17Stream    = 0x46
	mmdvmM17Packet    = 0x47
	mmdvmM17Lost      = 0x48
	mmdvmM17EOT       = 0x49

	mmdvmSerialData = 0x80

	mmdvmTransparent = 0x90
	// mmdvmQSOInfo     = 0x91

	mmdvmDebug1    = 0xF1
	mmdvmDebug2    = 0xF2
	mmdvmDebug3    = 0xF3
	mmdvmDebug4    = 0xF4
	mmdvmDebug5    = 0xF5
	mmdvmDebugDump = 0xFA
	mmdvmACK       = 0x70
	mmdvmNAK       = 0x7F

	mmdvmReadBufferLen = 2000

	mmdvmCapabilities1DSTAR  = 0x01
	mmdvmCapabilities1DMR    = 0x02
	mmdvmCapabilities1YSF    = 0x04
	mmdvmCapabilities1P25    = 0x08
	mmdvmCapabilities1NXDN   = 0x10
	mmdvmCapabilities1M17    = 0x20
	mmdvmCapabilities1FM     = 0x40
	mmdvmCapabilities2POCSAG = 0x01
	mmdvmCapabilities2AX25   = 0x02

	mmdvmModeIdle = 0
	// mmdvmModeDSTAR  = 1
	// mmdvmModeDMR    = 2
	// mmdvmModeYSF    = 3
	// mmdvmModeP25    = 4
	// mmdvmModeNXDN   = 5
	// mmdvmModePOCSAG = 6
	mmdvmModeM17 = 7

	// mmdvmModeFM = 10

	// mmdvmModeCW      = 98
	// mmdvmModeLockout = 99
	// mmdvmModeError   = 100
	// mmdvmModeQuit    = 110

	// mmdvmTagHeader = 0x00
	// mmdvmTagData   = 0x01
	// mmdvmTagLost   = 0x02
	// mmdvmTagEOT    = 0x03
)
const (
	mmdvmSerialStateStart = iota
	mmdvmSerialStateLength1
	mmdvmSerialStateLength2
	mmdvmSerialStateType
	mmdvmSerialStateData
)
const (
	mmdvmHWTypeMMDVM = iota
	mmdvmHWTypeDVMEGA
	mmdvmHWTypeMMDVM_ZUMSPOT
	mmdvmHWTypeMMDVM_HS_HAT
	mmdvmHWTypeMMDVM_HS_DUAL_HAT
	mmdvmHWTypeNANO_HOTSPOT
	mmdvmHWTypeNANO_DV
	mmdvmHWTypeD2RG_MMDVM_HS
	mmdvmHWTypeMMDVM_HS
	mmdvmHWTypeOPENGD77_HS
	mmdvmHWTypeSKYBRIDGE
)

type MMDVMConfig struct {
	duplex     bool
	rxInvert   bool
	txInvert   bool
	pttInvert  bool
	debug      bool
	txDCOffset byte
	txDelay    byte
	rxLevel    float32
	rxDCOffset byte

	m17Enabled bool
	m17TXLevel float32
	m17TXHang  byte
}

type MMDVMModem struct {
	frameSink func(typ uint16, softBits []SoftBit)
	config    MMDVMConfig

	port     io.ReadWriteCloser
	sendCmds chan []byte

	mutex sync.Mutex
	// protected by mutex
	exit            bool
	lastStatusCheck time.Time
	lastTXData      time.Time
	space           int
	// end protected by mutex

	capabilities    [2]byte
	hwType          byte
	protocolVersion byte
}

func NewMMDVMModem(
	rxFrequency uint32,
	txFrequency uint32,
	power float32,
	frequencyCorr int16,
	afc bool,
	modemCfg *ini.Section,
	duplex bool) (*MMDVMModem, error) {
	var protocolErr, portErr error
	protocol := modemCfg.Key("Protocol").In("BAD", []string{"uart"})
	if protocol == "BAD" {
		protocolErr = fmt.Errorf("modem Protocol value is '%s', must be 'uart'", modemCfg.Key("Protocol").String())
	}
	port := modemCfg.Key("Port").String()
	if port == "" {
		portErr = errors.New("modem Port must have a value")
	}
	speed, speedErr := modemCfg.Key("Speed").Int()
	if speedErr == nil && !slices.Contains(mmdvmValidSpeeds, speed) {
		speedErr = fmt.Errorf("modem Speed value is %d, must be one of %d", speed, mmdvmValidSpeeds)
	}
	// Defaults here are values that typically work
	rxInvert := modemCfg.Key("RXInvert").MustBool(false)
	txInvert := modemCfg.Key("TXInvert").MustBool(true)
	pttInvert := modemCfg.Key("PTTInvert").MustBool(false)
	debug := modemCfg.Key("Debug").MustBool(false)
	txDCOffset := modemCfg.Key("TXDCOffset").MustUint(0)
	txDelay := modemCfg.Key("TXDelay").MustUint(100)
	rxLevel := modemCfg.Key("RXLevel").MustUint(50)
	rxDCOffset := modemCfg.Key("RXDCOffset").MustUint(0)
	txLevel := modemCfg.Key("TXLevel").MustUint(50)
	txHang := modemCfg.Key("TXHang").MustUint(5)

	var err error
	err = errors.Join(
		protocolErr,
		portErr,
		speedErr,
	)
	if err != nil {
		return nil, err
	}

	m := &MMDVMModem{
		sendCmds:   make(chan []byte, 100), // 4 seconds
		lastTXData: time.Now(),
		config: MMDVMConfig{
			duplex:     duplex,
			rxInvert:   rxInvert,
			txInvert:   txInvert,
			pttInvert:  pttInvert,
			debug:      debug,
			txDCOffset: byte(txDCOffset),
			txDelay:    byte(txDelay),
			rxLevel:    float32(rxLevel),
			rxDCOffset: byte(rxDCOffset),
			m17Enabled: true,
			m17TXLevel: float32(txLevel),
			m17TXHang:  byte(txHang),
		},
	}
	log.Printf("[DEBUG] Opening modem")
	mode := &serial.Mode{
		BaudRate: speed,
	}
	p, err := serial.Open(port, mode)
	if err != nil {
		return nil, fmt.Errorf("modem open: %w", err)
	}
	p.SetReadTimeout(0) // Non-blocking reads
	m.port = p
	err = m.readVersion()
	if err != nil {
		return nil, err
	}
	err = m.setFrequency(rxFrequency+uint32(frequencyCorr), txFrequency+uint32(frequencyCorr), power)
	if err != nil {
		return nil, err
	}
	err = m.writeConfig()
	if err != nil {
		return nil, err
	}
	err = m.setMode(mmdvmModeM17)
	if err != nil {
		return nil, err
	}
	m.setExit(false)
	go m.modemReceive()
	go m.modemSend()
	return m, nil
}

func (m *MMDVMModem) isExit() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.exit
}
func (m *MMDVMModem) setExit(exit bool) {
	m.mutex.Lock()
	m.exit = exit
	m.mutex.Unlock()
}
func (m *MMDVMModem) modemReceive() {
	log.Printf("[DEBUG] modemReceive starting")
	for !m.isExit() {
		m.checkStatus()
		responseType, buf, err := m.getResponse()
		if err == ErrMMDVMReadTimeout {
			// nothing to do
		} else if err != nil {
			log.Printf("[DEBUG] modemReceive getReponse() err: %v", err)
		} else {
			// log.Printf("[DEBUG] modemReceive getReponse(): %x", responseType)
			switch responseType {
			case mmdvmM17LinkSetup:
				log.Printf("[DEBUG] Received M17 LSF: [% 02x]", buf)
				sb := bytesToSoftBits(buf[3 : 46+3])
				m.frameSink(LSFSync, sb)
			case mmdvmM17Stream:
				log.Printf("[DEBUG] Received M17 Stream frame: [% 02x]", buf)
				sb := bytesToSoftBits(buf[3 : 46+3])
				m.frameSink(StreamSync, sb)
			case mmdvmM17Packet:
				log.Printf("[DEBUG] Received M17 Packet frame: [% 02x]", buf)
				sb := bytesToSoftBits(buf[3 : 46+3])
				m.frameSink(PacketSync, sb)
			case mmdvmM17EOT:
				log.Printf("[DEBUG] Received M17 EOT: [% 02x]", buf)
				sb := bytesToSoftBits(buf[3 : 46+3])
				m.frameSink(EOTMarker, sb)
			case mmdvmM17Lost:
				// log.Printf("[DEBUG] Received M17 Lost: [% 02x]", buf)
			case mmdvmGetStatus:
				// log.Printf("[DEBUG] Received Get Status: [% 02x]", buf)
				switch m.protocolVersion {
				case 1:
					adcOverflow := (buf[2] & 0x02) == 0x02
					if adcOverflow {
						log.Print("[ERROR] MMDVM ADC levels have overflowed")
					}
					rxOverflow := (buf[2] & 0x04) == 0x04
					if rxOverflow {
						log.Print("[ERROR] MMDVM RX buffer has overflowed")
					}
					txOverflow := (buf[2] & 0x08) == 0x08
					if txOverflow {
						log.Print("[ERROR] MMDVM TX buffer has overflowed")
					}
					dacOverflow := (buf[2] & 0x20) == 0x20
					if dacOverflow {
						log.Print("[ERROR] MMDVM DAC levels have overflowed")
					}
					if len(buf) > 10 {
						m.setSpace(buf[10])
					} else {
						m.setSpace(0)
					}
				case 2:
					adcOverflow := (buf[1] & 0x02) == 0x02
					if adcOverflow {
						log.Print("[ERROR] MMDVM ADC levels have overflowed")
					}
					rxOverflow := (buf[1] & 0x04) == 0x04
					if rxOverflow {
						log.Print("[ERROR] MMDVM RX buffer has overflowed")
					}
					txOverflow := (buf[1] & 0x08) == 0x08
					if txOverflow {
						log.Print("[ERROR] MMDVM TX buffer has overflowed")
					}
					dacOverflow := (buf[1] & 0x20) == 0x20
					if dacOverflow {
						log.Print("[ERROR] MMDVM DAC levels have overflowed")
					}
					m.setSpace(buf[9])
				}
			case mmdvmTransparent:
				log.Printf("[DEBUG] Received Transparent: [% 02x]", buf)
			case mmdvmGetVersion:
				// ignore
			case mmdvmSerialData:
				log.Printf("[DEBUG] Received Serial Data: [% 02x]", buf)
			case mmdvmACK:
				// ignore
				// log.Printf("[DEBUG] Received ACK: [% 02x]", buf)
			case mmdvmNAK:
				log.Printf("[WARN] Modem run getReponse() received a NAK, command: %02x, reason: %d", buf[0], buf[1])
			case mmdvmDebug1, mmdvmDebug2, mmdvmDebug3, mmdvmDebug4, mmdvmDebug5, mmdvmDebugDump:
				switch responseType {
				case mmdvmDebug1:
					log.Printf("[DEBUG] MMDVM debug: %s", buf)
				case mmdvmDebug2:
					val1 := decodeValue(buf[len(buf)-2:])
					log.Printf("[DEBUG] MMDVM debug: %s %d", buf[:len(buf)-2], val1)
				case mmdvmDebug3:
					val1 := decodeValue(buf[len(buf)-4 : len(buf)-2])
					val2 := decodeValue(buf[len(buf)-2:])
					log.Printf("[DEBUG] MMDVM debug: %s %d %d", buf[:len(buf)-4], val1, val2)
				case mmdvmDebug4:
					val1 := decodeValue(buf[len(buf)-6 : len(buf)-4])
					val2 := decodeValue(buf[len(buf)-4 : len(buf)-2])
					val3 := decodeValue(buf[len(buf)-2:])
					log.Printf("[DEBUG] MMDVM debug: %s %d %d %d", buf[:len(buf)-6], val1, val2, val3)
				case mmdvmDebug5:
					val1 := decodeValue(buf[len(buf)-8 : len(buf)-6])
					val2 := decodeValue(buf[len(buf)-6 : len(buf)-4])
					val3 := decodeValue(buf[len(buf)-4 : len(buf)-2])
					val4 := decodeValue(buf[len(buf)-2:])
					log.Printf("[DEBUG] MMDVM debug: %s %d %d %d %d", buf[:len(buf)-8], val1, val2, val3, val4)
				case mmdvmDebugDump:
					log.Printf("[DEBUG] MMDVM debug data: [% 2x]", buf)
				}
			default:
				log.Printf("[DEBUG] Unexpected modem response, type: %2x, buf: [% 2x]", responseType, buf)
			}
		}

	}
	log.Printf("[DEBUG] modemReceive stopping")
}

func (m *MMDVMModem) modemSend() {
	log.Printf("[DEBUG] modemSend starting")
	for !m.isExit() {
		if m.getSpace() > 1 {
			_, err := m.port.Write(<-m.sendCmds)
			if err != nil {
				log.Printf("[WARN] modemSend: Error writing to modem: %v", err)
			}
			m.decrementSpace()
		}
	}
	log.Printf("[DEBUG] modemSend stopping")
}

func decodeValue(buf []byte) int16 {
	var val int16
	_, err := binary.Decode(buf, binary.BigEndian, &val)
	if err != nil {
		// should never happen
		log.Printf("[ERROR] Error decoding value: %v", err)
	}
	return val
}

func bytesToSoftBits(buf []byte) []SoftBit {
	var ret []SoftBit
	for _, b := range buf {
		for range 8 {
			if b&0x80 == 0x80 {
				ret = append(ret, softTrue)
			} else {
				ret = append(ret, softFalse)
			}
			b <<= 1
		}
	}
	return ret
}

func (m *MMDVMModem) checkStatus() error {
	m.mutex.Lock()
	checkStatus := time.Since(m.lastStatusCheck) > 250*time.Millisecond
	m.mutex.Unlock()
	if !checkStatus {
		return nil
	}
	cmd := []byte{mmdvmFrameStart, 3, mmdvmGetStatus}
	_, err := m.port.Write(cmd)
	if err != nil {
		return fmt.Errorf("error writing GetStatus cmd: %w", err)
	}
	m.mutex.Lock()
	m.lastStatusCheck = time.Now()
	m.mutex.Unlock()
	return nil
}

func (m *MMDVMModem) readVersion() error {
	time.Sleep(2 * time.Second)
	cmd := []byte{mmdvmFrameStart, 3, mmdvmGetVersion}
	log.Printf("[DEBUG] Trying GetVersion")
	_, err := m.port.Write(cmd)
	if err != nil {
		return fmt.Errorf("error writing GetVersion cmd: %w", err)
	}
retry:
	for range 6 {
		time.Sleep(10 * time.Millisecond)
		responseType, buf, err := m.getResponse()
		log.Printf("[DEBUG] GetVersion response: [% x], err: %v", buf, err)
		if err == nil && responseType == mmdvmGetVersion {
			switch {
			case string(buf[1:1+6]) == "MMDVM " || string(buf[23:23+6]) == "MMDVM ":
				m.hwType = mmdvmHWTypeMMDVM
			case string(buf[1:1:6]) == "DVMEGA":
				m.hwType = mmdvmHWTypeDVMEGA
			case string(buf[1:1+7]) == "ZUMspot":
				m.hwType = mmdvmHWTypeMMDVM_ZUMSPOT
			case string(buf[1:1+12]) == "MMDVM_HS_Hat":
				m.hwType = mmdvmHWTypeMMDVM_HS_HAT
			case string(buf[1:1+17]) == "MMDVM_HS_Dual_Hat":
				m.hwType = mmdvmHWTypeMMDVM_HS_DUAL_HAT
			case string(buf[1:1+12]) == "Nano_hotSPOT":
				m.hwType = mmdvmHWTypeNANO_HOTSPOT
			case string(buf[1:1+7]) == "Nano_DV":
				m.hwType = mmdvmHWTypeNANO_DV
			case string(buf[1:1+13]) == "D2RG_MMDVM_HS":
				m.hwType = mmdvmHWTypeD2RG_MMDVM_HS
			case string(buf[1:1+9]) == "MMDVM_HS-":
				m.hwType = mmdvmHWTypeMMDVM_HS
			case string(buf[1:1+11]) == "OpenGD77_HS":
				m.hwType = mmdvmHWTypeOPENGD77_HS
			case string(buf[1:1+9]) == "SkyBridge":
				m.hwType = mmdvmHWTypeSKYBRIDGE
			}
			m.protocolVersion = buf[0]
			switch m.protocolVersion {
			case 1:
				log.Printf("[INFO] MMDVM protocol version: 1, description: %s", buf[1:])
				m.capabilities[0] =
					mmdvmCapabilities1DSTAR | mmdvmCapabilities1DMR | mmdvmCapabilities1YSF |
						mmdvmCapabilities1P25 | mmdvmCapabilities1NXDN | mmdvmCapabilities1M17
				m.capabilities[1] = mmdvmCapabilities2POCSAG
				break retry
			case 2:
				log.Printf("[INFO] MMDVM protocol version: 2, description: %s", buf[20:])
				switch buf[3] {
				case 0:
					log.Printf("[INFO] CPU: Atmel ARM, UDID: %02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X", buf[4], buf[5], buf[6], buf[7], buf[8], buf[9], buf[10], buf[11], buf[12], buf[13], buf[14], buf[15], buf[16], buf[17], buf[18], buf[19])
				case 1:
					log.Printf("[INFO] CPU: NXP ARM, UDID: %02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X", buf[4], buf[5], buf[6], buf[7], buf[8], buf[9], buf[10], buf[11], buf[12], buf[13], buf[14], buf[15], buf[16], buf[17], buf[18], buf[19])
				case 2:
					log.Printf("[INFO] CPU: ST-Micro ARM, UDID: %02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X%02X", buf[4], buf[5], buf[6], buf[7], buf[8], buf[9], buf[10], buf[11], buf[12], buf[13], buf[14], buf[15])
				default:
					log.Printf("[INFO] CPU: Unknown type: %d", buf[3])
				}
				m.capabilities[0] = buf[1]
				m.capabilities[1] = buf[2]
				break retry
			default:
				log.Printf("[ERROR] Unsupported MMDVM protocol version: %d", m.protocolVersion)
				return ErrUnsupportedModemProtocol
			}
		}
		time.Sleep(1500 * time.Millisecond)
	}
	modeText := "Modes:"
	if (m.capabilities[0] & mmdvmCapabilities1DSTAR) == mmdvmCapabilities1DSTAR {
		modeText += " D-Star"
	}
	if (m.capabilities[0] & mmdvmCapabilities1DMR) == mmdvmCapabilities1DMR {
		modeText += " DMR"
	}
	if (m.capabilities[0] & mmdvmCapabilities1YSF) == mmdvmCapabilities1YSF {
		modeText += " YSF"
	}
	if (m.capabilities[0] & mmdvmCapabilities1P25) == mmdvmCapabilities1P25 {
		modeText += " P25"
	}
	if (m.capabilities[0] & mmdvmCapabilities1NXDN) == mmdvmCapabilities1NXDN {
		modeText += " NXDN"
	}
	if (m.capabilities[0] & mmdvmCapabilities1M17) == mmdvmCapabilities1M17 {
		modeText += " M17"
	}
	if (m.capabilities[0] & mmdvmCapabilities1FM) == mmdvmCapabilities1FM {
		modeText += " FM"
	}
	if (m.capabilities[0] & mmdvmCapabilities2POCSAG) == mmdvmCapabilities2POCSAG {
		modeText += " POCSAG"
	}
	if (m.capabilities[0] & mmdvmCapabilities2AX25) == mmdvmCapabilities2AX25 {
		modeText += " AX.25"
	}
	log.Printf("[INFO] %s", modeText)
	return nil
}

func (m *MMDVMModem) setFrequency(rxFreq, txFreq uint32, power float32) error {
	log.Printf("[DEBUG] setFrequency(%d, %d, %f)", rxFreq, txFreq, power)

	cmd := make([]byte, 12)
	cmd[0] = mmdvmFrameStart
	cmd[1] = byte(len(cmd))
	cmd[2] = mmdvmSetFrequency
	cmd[3] = 0x00

	cmd[4] = byte((rxFreq >> 0) & 0xFF)
	cmd[5] = byte((rxFreq >> 8) & 0xFF)
	cmd[6] = byte((rxFreq >> 16) & 0xFF)
	cmd[7] = byte((rxFreq >> 24) & 0xFF)

	cmd[8] = byte((txFreq >> 0) & 0xFF)
	cmd[9] = byte((txFreq >> 8) & 0xFF)
	cmd[10] = byte((txFreq >> 16) & 0xFF)
	cmd[11] = byte((txFreq >> 24) & 0xFF)

	_, err := m.port.Write(cmd)
	if err != nil {
		return fmt.Errorf("error writing setFrequency cmd: %w", err)
	}

	err = m.getEmptyResponse()
	return err
}

func (m *MMDVMModem) writeConfig() error {
	switch m.protocolVersion {
	case 1:
		return m.setProtocol1Config()
	case 2:
		return m.setProtocol2Config()
	default:
		return ErrUnsupportedModemProtocol
	}
}

func (m *MMDVMModem) setProtocol1Config() error {
	cmd := make([]byte, 30)

	cmd[0] = mmdvmFrameStart

	cmd[1] = 26

	cmd[2] = mmdvmSetConfig

	if m.config.rxInvert {
		cmd[3] |= 0x01
	}
	if m.config.txInvert {
		cmd[3] |= 0x02
	}
	if m.config.pttInvert {
		cmd[3] |= 0x04
	}
	// if (m.config.ysfLoDev) {
	// 	cmd[3] |= 0x08
	// }
	if m.config.debug {
		cmd[3] |= 0x10
	}
	// if (m.config.useCOSAsLockout) {
	// 	cmd[3] |= 0x20
	// }
	if !m.config.duplex {
		cmd[3] |= 0x80
	}
	// if (m.config.dstarEnabled)
	// 	cmd[4] |= 0x01
	// if (m.config.dmrEnabled)
	// 	cmd[4] |= 0x02
	// if (m.config.ysfEnabled)
	// 	cmd[4] |= 0x04
	// if (m.config.p25Enabled)
	// 	cmd[4] |= 0x08
	// if (m.config.nxdnEnabled)
	// 	cmd[4] |= 0x10
	// if (m.config.pocsagEnabled)
	// 	cmd[4] |= 0x20
	if m.config.m17Enabled {
		cmd[4] |= 0x40
	}

	cmd[5] = m.config.txDelay / 10 // In 10ms units

	cmd[6] = mmdvmModeIdle

	cmd[7] = byte(m.config.rxLevel*2.55 + 0.5)

	// cmd[8] = byte(m.config.cwIdTXLevel*2.55 + 0.5)
	cmd[8] = 25

	// cmd[9] = m.config.dmrColorCode

	// cmd[10] = m.config.dmrDelay

	cmd[11] = 128 // Was OscOffset

	// cmd[12] = byte(m.config.dstarTXLevel*2.55 + 0.5)
	cmd[12] = 128
	// cmd[13] = byte(m.config.dmrTXLevel*2.55 + 0.5)
	cmd[13] = 128
	// cmd[14] = byte(m.config.ysfTXLevel*2.55 + 0.5)
	cmd[14] = 128
	// cmd[15] = byte(m.config.p25TXLevel*2.55 + 0.5)
	cmd[15] = 128

	cmd[16] = byte(m.config.txDCOffset + 128)
	cmd[17] = byte(m.config.rxDCOffset + 128)

	// cmd[18] = byte(m.config.nxdnTXLevel*2.55 + 0.5)
	cmd[18] = 128

	// cmd[19] = byte(m.config.ysfTXHang)
	cmd[19] = 0xE6

	// cmd[20] = byte(m.config.pocsagTXLevel*2.55 + 0.5)
	cmd[20] = 128

	// cmd[21] = byte(m.config.fmTXLevel*2.55 + 0.5)
	cmd[21] = 128

	// cmd[22] = byte(m.config.p25TXHang)

	// cmd[23] = byte(m.config.nxdnTXHang)
	cmd[23] = 4

	cmd[24] = byte(m.config.m17TXLevel*2.55 + 0.5)

	cmd[25] = byte(m.config.m17TXHang)

	// log.Printf("[DEBUG] cmd: [% x]", cmd)
	_, err := m.port.Write(cmd)
	if err != nil {
		return fmt.Errorf("error writing SetConfig cmd: %w", err)
	}

	err = m.getEmptyResponse()
	return err
}

func (m *MMDVMModem) setProtocol2Config() error {
	// log.Printf("[DEBUG] setProtocol2Config()")
	cmd := make([]byte, 40)

	cmd[0] = mmdvmFrameStart

	cmd[1] = 40

	cmd[2] = mmdvmSetConfig

	if m.config.rxInvert {
		cmd[3] |= 0x01
	}
	if m.config.txInvert {
		cmd[3] |= 0x02
	}
	if m.config.pttInvert {
		cmd[3] |= 0x04
	}
	// if (m.config.ysfLoDev) {
	// 	cmd[3] |= 0x08
	// }
	if m.config.debug {
		cmd[3] |= 0x10
	}
	// if (m.config.useCOSAsLockout) {
	// 	cmd[3] |= 0x20
	// }
	if !m.config.duplex {
		cmd[3] |= 0x80
	}

	// if (m.config.dstarEnabled)
	// 	cmd[4] |= 0x01
	// if (m.config.dmrEnabled)
	// 	cmd[4] |= 0x02
	// if (m.config.ysfEnabled)
	// 	cmd[4] |= 0x04
	// if (m.config.p25Enabled)
	// 	cmd[4] |= 0x08
	// if (m.config.nxdnEnabled)
	// 	cmd[4] |= 0x10
	// if (m.config.fmEnabled)
	// 	cmd[4] |= 0x20
	if m.config.m17Enabled {
		cmd[4] |= 0x40
	}

	// if pocsagEnabled
	// 	cmd[5] |= 0x01
	// if ax25Enabled
	// 	cmd[5] |= 0x02

	cmd[6] = m.config.txDelay / 10 // In 10ms units
	cmd[7] = mmdvmModeIdle
	cmd[8] = byte(m.config.txDCOffset + 128)
	cmd[9] = byte(m.config.rxDCOffset + 128)
	cmd[10] = byte(m.config.rxLevel*2.55 + 0.5)

	// cmd[11] = byte(m.config.cwIdTXLevel*2.55 + 0.5)
	cmd[11] = 25
	// cmd[12] = byte(m.config.dstarTXLevel*2.55 + 0.5)
	cmd[12] = 128
	// cmd[13] = byte(m.config.dmrTXLevel*2.55 + 0.5)
	cmd[13] = 128
	// cmd[14] = byte(m.config.ysfTXLevel*2.55 + 0.5)
	cmd[14] = 128
	// cmd[15] = byte(m.config.p25TXLevel*2.55 + 0.5)
	cmd[15] = 128

	// nxdnTXLevel
	cmd[16] = 128
	cmd[17] = byte(m.config.m17TXLevel*2.55 + 0.5)

	// pocsagTXLevel
	cmd[18] = 128
	// cmd[19] = byte(m.config.fmTXLevel*2.55 + 0.5)
	cmd[19] = 128
	// m_ax25TXLevel
	cmd[20] = 128

	// ysfTXHang
	cmd[23] = 0x04

	// p25TXHang
	cmd[24] = 0x05
	// nxdnTXHang
	cmd[25] = 0x05
	cmd[26] = byte(m.config.m17TXHang)

	// dmrColorCode
	cmd[29] = 0x01

	// cmd[30] = dmrDelay

	// ax25RXTwist
	cmd[31] = 0x86
	// ax25TXDelay
	cmd[32] = 0x1E
	// ax25SlotTime
	cmd[33] = 0x03
	// ax25PPersist
	cmd[34] = 0x80

	// log.Printf("[DEBUG] cmd: [% x]", cmd)
	_, err := m.port.Write(cmd)
	if err != nil {
		return fmt.Errorf("error writing SetConfig cmd: %w", err)
	}

	err = m.getEmptyResponse()
	return err
}

func (m *MMDVMModem) setMode(mode byte) error {
	log.Printf("[DEBUG] setMode(%02x)", mode)
	cmd := []byte{mmdvmFrameStart, 4, mmdvmSetMode, mode}
	_, err := m.port.Write(cmd)
	return err
}

func (m *MMDVMModem) getEmptyResponse() error {
	var responseType byte
	var buf []byte
	var err error
	for i := range 30 {
		time.Sleep(10 * time.Millisecond)
		responseType, buf, err = m.getResponse()
		if i == 29 && err == nil && responseType != mmdvmACK && responseType != mmdvmNAK {
			log.Printf("[ERROR] Modem not responding to command")
			return errors.New("modem not responding")
		} else if (err != nil && err != ErrMMDVMReadTimeout) || responseType == mmdvmACK || responseType == mmdvmNAK {
			break
		}
	}
	if err == nil && responseType == mmdvmNAK {
		log.Printf("[ERROR] Received a NAK to the SetConfig command from the modem: % x", buf)
		return ErrModemNAK
	}
	return err
}

func (m *MMDVMModem) getResponse() (byte, []byte, error) {
	var n int
	var err error
	var state byte
	var buffer []byte
	var offset int
	var length int
	var responseType byte

	state = mmdvmSerialStateStart
	buffer = make([]byte, mmdvmReadBufferLen)

	if state == mmdvmSerialStateStart {
		offset = 0
		n, err = m.port.Read(buffer[offset : offset+1])
		if err != nil {
			log.Printf("[ERROR] mmdvmSerialStateStart read error: %v", err)
			return responseType, nil, fmt.Errorf("mmdvmSerialStateStart read: %w", err)
		} else if n == 0 {
			return responseType, nil, ErrMMDVMReadTimeout
		}
		if buffer[0] != mmdvmFrameStart {
			log.Printf("[DEBUG] getResponse expected mmdvmFrameStart, got 0x%x", buffer[offset])
			return responseType, nil, ErrMMDVMReadTimeout
		}
		// log.Printf("[DEBUG] Modem mmdvmSerialStateStart read 0x%x", buffer[offset])
		state = mmdvmSerialStateLength1
		offset++
		length = 1
	}

	if state == mmdvmSerialStateLength1 {
		n, err = m.port.Read(buffer[offset : offset+1])
		if err != nil {
			log.Printf("[ERROR] mmdvmSerialStateLength1 read error: %v", err)
			state = mmdvmSerialStateStart
			return responseType, nil, fmt.Errorf("mmdvmSerialStateLength1 read: %w", err)
		} else if n == 0 {
			log.Printf("[DEBUG] mmdvmSerialStateLength1 read timeout")
			return responseType, nil, ErrMMDVMReadTimeout
		}
		// log.Printf("[DEBUG] Modem mmdvmSerialStateLength1 read %d", buffer[offset])
		length = int(buffer[offset])
		offset++
		if length == 0 {
			state = mmdvmSerialStateLength2
		} else {
			state = mmdvmSerialStateType
		}
	}

	if state == mmdvmSerialStateLength2 {
		n, err = m.port.Read(buffer[offset : offset+1])
		if err != nil {
			log.Printf("[ERROR] mmdvmSerialStateLength2 read error: %v", err)
			state = mmdvmSerialStateStart
			return responseType, nil, fmt.Errorf("mmdvmSerialStateLength2 read: %w", err)
		} else if n == 0 {
			log.Printf("[DEBUG] mmdvmSerialStateLength2 read timeout")
			return responseType, nil, ErrMMDVMReadTimeout
		}
		length = int(buffer[offset]) + 255
		offset++
		state = mmdvmSerialStateType
	}

	if state == mmdvmSerialStateType {
		n, err = m.port.Read(buffer[offset : offset+1])
		if err != nil {
			log.Printf("[ERROR] mmdvmSerialStateType read error: %v", err)
			state = mmdvmSerialStateStart
			return responseType, nil, fmt.Errorf("mmdvmSerialStateType read: %w", err)
		} else if n == 0 {
			log.Printf("[DEBUG] mmdvmSerialStateType read timeout")
			return responseType, nil, ErrMMDVMReadTimeout
		}
		responseType = buffer[offset]
		// log.Printf("[DEBUG] Modem mmdvmSerialStateType read 0x%x", buffer[offset])
		offset++
		state = mmdvmSerialStateData
	}

	if state == mmdvmSerialStateData {
		time.Sleep(10 * time.Millisecond)
		for start := offset; start < length; start += n {
			n, err = m.port.Read(buffer[start:length])
			if err != nil {
				log.Printf("[ERROR] mmdvmSerialStateData read error: %v", err)
				state = mmdvmSerialStateStart
				return responseType, nil, fmt.Errorf("mmdvmSerialStateData read: %w", err)
			} else if n == 0 {
				log.Printf("[DEBUG] mmdvmSerialStateData read timeout")
				return responseType, nil, ErrMMDVMReadTimeout
			}
		}
	}
	buffer = buffer[offset:length]
	// log.Printf("[DEBUG] getResponse() responseType: 0x%x, buffer: [% x], len: %d, err: %v", responseType, buffer, len(buffer), err)
	return responseType, buffer, err
}

func (m *MMDVMModem) StartDecoding(sink func(typ uint16, softBits []SoftBit)) {
	m.frameSink = sink
}

// Reset the modem
func (m *MMDVMModem) Reset() error {
	log.Print("[DEBUG] modem Reset()")
	return nil
}

// Close the modem
func (m *MMDVMModem) Close() error {
	log.Print("[DEBUG] modem Close()")
	m.setExit(true)
	close(m.sendCmds)
	return m.port.Close()
}

func (m *MMDVMModem) TransmitPacket(p Packet) error {
	log.Printf("[DEBUG] TransmitPacket: %v", p)
	var bits []Bit

	err := m.transmitLSF(*p.LSF)
	if err != nil {
		return fmt.Errorf("failed to send packet LSF: %w", err)
	}

	chunkCnt := 0
	packetData := p.PayloadBytes()
	for bytesLeft := len(packetData); bytesLeft > 0; bytesLeft -= 25 {
		bits = unpackBits(PacketSyncBytes)
		chunk := make([]byte, 25+1) // 25 bytes from the packet plus 6 bits of metadata
		if bytesLeft > 25 {
			// not the last chunk
			copy(chunk, packetData[chunkCnt*25:chunkCnt*25+25])
			chunk[25] = byte(chunkCnt << 2)
		} else {
			// last chunk
			copy(chunk, packetData[chunkCnt*25:chunkCnt*25+bytesLeft])
			//EOT bit set to 1, set counter to the amount of bytes in this (the last) chunk
			if bytesLeft%25 == 0 {
				chunk[25] = (1 << 7) | ((25) << 2)
			} else {
				chunk[25] = uint8((1 << 7) | ((bytesLeft % 25) << 2))
			}
		}
		//encode the packet chunk
		b, err := ConvolutionalEncode(chunk, PacketPuncturePattern, PacketModeFinalBit)
		if err != nil {
			return fmt.Errorf("unable to encode packet: %w", err)
		}
		encodedBits := NewPayloadBits(b)
		rfBits := InterleaveBits(encodedBits)
		rfBits = RandomizeBits(rfBits)
		// Append chunk to the output
		bits = append(bits, rfBits[:]...)
		m.writeBits(mmdvmM17Packet, bits)
		chunkCnt++
	}
	m.writeEOT()
	return nil
}

func (m *MMDVMModem) transmitLSF(lsf LSF) error {
	// log.Printf("[DEBUG] transmitLSF: %s", lsf)
	bits, err := generateLSFBits(lsf)
	if err != nil {
		return fmt.Errorf("failed to generate LSF bits: %w", err)
	}
	m.writeBits(mmdvmM17LinkSetup, bits)
	return nil
}

func (m *MMDVMModem) TransmitVoiceStream(sd StreamDatagram) error {
	log.Printf("[DEBUG] TransmitVoiceStream id: %04x, fn: %04x, last: %v", sd.StreamID, sd.FrameNumber, sd.LastFrame)
	var bits []Bit
	var err error
	if sd.FrameNumber == 0 && sd.LSF != nil { // first frame
		time.Sleep(time.Until(m.lastTXData.Add(FrameTime)))
		err = m.transmitLSF(*sd.LSF)
		if err != nil {
			return fmt.Errorf("failed to send stream LSF: %w", err)
		}
	}
	bits, err = generateStreamBits(sd)
	if err != nil {
		return fmt.Errorf("failed to generate stream bits: %w", err)
	}
	time.Sleep(time.Until(m.lastTXData.Add(FrameTime)))

	m.writeBits(mmdvmM17Stream, bits)
	if sd.LastFrame {
		// send EOT
		time.Sleep(time.Until(m.lastTXData.Add(FrameTime)))
		m.writeEOT()
	}
	return nil
}

func (m *MMDVMModem) writeBits(typ byte, bits []Bit) {
	buf := packBits(bits)
	// log.Printf("[DEBUG] writeBits type: %02x, len: %d, buf: % 02x", typ, len(buf), buf)
	cmd := []byte{mmdvmFrameStart, byte(4 + len(buf)), typ, 0}
	cmd = append(cmd, buf...)
	m.sendToModem(cmd)
}

func (m *MMDVMModem) writeEOT() {
	// log.Printf("[DEBUG] writeEOT")
	var buf []byte
	for i := 0; i < BytesPerFrame/len(EOTMarkerBytes); i++ {
		buf = append(buf, EOTMarkerBytes...)
	}
	cmd := []byte{mmdvmFrameStart, byte(4 + len(buf)), mmdvmM17EOT, 0}
	cmd = append(cmd, buf...)
	m.sendToModem(cmd)
}

func (m *MMDVMModem) sendToModem(cmd []byte) {
	select {
	case m.sendCmds <- cmd:
		// okay
		since := time.Since(m.lastTXData)
		if since > 100*time.Millisecond {
			log.Printf("[DEBUG] Last TX data sent %v ago", since.Round(time.Millisecond))
		}
	default:
		log.Println("[DEBUG] sendToModem message send failed")
	}
	m.lastTXData = time.Now()
}

func (m *MMDVMModem) getSpace() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.space
}

func (m *MMDVMModem) setSpace(space byte) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.space = int(space)
}

func (m *MMDVMModem) decrementSpace() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.space--
	return m.space
}

func (m *MMDVMModem) Start() error {
	log.Printf("[DEBUG] Start()")
	return nil
}
