package m17

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/go-zeromq/zmq4"
	"go.bug.st/serial"
	"gopkg.in/ini.v1"
)

// CC1200 V2 commands
const (
	cc1200V2CmdPing = iota
	//SET
	cc1200V2CmdSetRXFreq
	cc1200V2CmdSetTXFreq
	cc1200V2CmdSetTXPower
	cc1200V2CmdSetReserved
	cc1200V2CmdSetFreqCorr
	cc1200V2CmdSetAFC
	cc1200V2CmdTXStart
	cc1200V2CmdRXStart
	cc1200V2CmdRXData
	cc1200V2CmdTXData
	cc1200V2CmdDbgEnable
	cc1200V2CmdDbgTxt
	//GET
	cc1200V2CmdGetIdent = iota + 0x80
	cc1200V2CmdGetCaps
	cc1200V2CmdGetRXFreq
	cc1200V2CmdGetTXFreq
	cc1200V2CmdGetTXPower
	cc1200V2CmdGetFreqCorr
	cc1200V2CmdGetBSBBuff
	cc1200V2CmdGetRSSI
)

var errBadCmd = errors.New("bad command")

type commandV2 struct {
	cmd  byte
	size uint16
	data []byte
}

func newCommandV2FromBytes(buf []byte) (commandV2, error) {
	var ret commandV2
	ret.cmd = buf[0]
	_, err := binary.Decode(buf[1:3], binary.LittleEndian, &ret.size)
	if err != nil {
		return ret, fmt.Errorf("parse commandV2 size: %v", err)
	}
	if ret.size < 4 || ret.size > 963 {
		return ret, errBadCmd
	}
	// Heuristics to detect bad commands
	switch {
	// All commands the firmware sends as of Dec. 2025
	case ret.cmd == cc1200V2CmdPing && ret.size == 7:
		fallthrough
	case ret.cmd == cc1200V2CmdSetRXFreq && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdSetTXFreq && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdSetTXPower && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdSetFreqCorr && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdSetAFC && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdTXStart && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdRXStart && ret.size == 4:
		fallthrough
	case ret.cmd == cc1200V2CmdRXData && ret.size == 963:
		fallthrough
	case ret.cmd == cc1200V2CmdTXData && ret.size == 4:
		fallthrough
	// case ret.cmd == cc1200V2CmdDbgEnable && ret.size == 4:
	// 	fallthrough
	case ret.cmd == cc1200V2CmdDbgTxt && ret.size <= 131:
		fallthrough
	case ret.cmd == cc1200V2CmdGetIdent && ret.size <= 131:
		fallthrough
	case ret.cmd == cc1200V2CmdGetCaps && ret.size <= 4:
		fallthrough
	case ret.cmd == cc1200V2CmdGetRXFreq && ret.size <= 7:
		fallthrough
	case ret.cmd == cc1200V2CmdGetTXFreq && ret.size <= 7:
		fallthrough
	// case ret.cmd == cc1200V2CmdGetTXPower  && ret.size <= :
	// 	fallthrough
	// case ret.cmd == cc1200V2CmdGetFreqCorr  && ret.size <= :
	// 	fallthrough
	case ret.cmd == cc1200V2CmdGetBSBBuff && ret.size <= 4:
		fallthrough
	case ret.cmd == cc1200V2CmdGetRSSI && ret.size <= 4:
		ret.data = make([]byte, ret.size-3)
		copy(ret.data, buf[3:])
		return ret, nil
	default:
		return ret, errBadCmd
	}
}

func newCommandV2(cmd byte, data []byte) commandV2 {
	var ret commandV2
	ret.cmd = cmd
	ret.size = uint16(len(data) + 3)
	ret.data = data
	// log.Printf("[DEBUG] newCommandV2(%d, [% x]): %v", cmd, data, ret)
	return ret
}

func (c commandV2) Bytes() ([]byte, error) {
	ret := make([]byte, c.size)
	ret[0] = c.cmd
	_, err := binary.Encode(ret[1:3], binary.LittleEndian, c.size)
	if err == nil {
		copy(ret[3:], c.data)
	}
	return ret, err
}
func (c commandV2) String() string {
	return fmt.Sprintf("cmd: %d, size: %d, data: [% x]", c.cmd, c.size, c.data)
}

const (
	txIdleV2 = iota
	txTXV2
)

// txTimeout must be greater than this!
const txVoiceStreamWait = 8 * FrameTime
const txTimeout = txVoiceStreamWait + 2*FrameTime

// Values calculated by SP5WWP to apply a 48us pre-emphasis
var iirBParam = []float64{2.8233128196365653, -1.0349763850514728}
var iirAParam = []float64{1.0, 0.7883364345850924}

type gpioLine interface {
	SetValue(value int) error
	Close() error
}

type CC1200ModemV2 struct {
	modem     io.ReadWriteCloser
	rxSymbols chan float32
	s2s       SymbolToSample
	frameSink func(typ uint16, softBits []SoftBit)

	mutex      sync.Mutex
	txState    int // protected by mutex
	cmdSource  chan commandV2
	nRST       gpioLine
	boot0      gpioLine
	debugLog   *os.File
	lastTXData time.Time
}

func NewCC1200ModemV2(
	rxFrequency uint32,
	txFrequency uint32,
	power int8,
	frequencyCorr int16,
	afc bool,
	modemCfg *ini.Section) (*CC1200ModemV2, error) {
	port := modemCfg.Key("Port").String()
	baudRate, baudRateErr := modemCfg.Key("Speed").Int()
	nRSTPin, nRSTPinErr := modemCfg.Key("NRSTPin").Int()
	boot0Pin, boot0PinErr := modemCfg.Key("Boot0Pin").Int()
	zmqPort := modemCfg.Key("ZMQPort").MustInt()

	var err error
	err = errors.Join(
		baudRateErr,
		nRSTPinErr,
		boot0PinErr,
	)
	if err != nil {
		return nil, err
	}

	ret := &CC1200ModemV2{
		rxSymbols:  make(chan float32, 1),
		s2s:        NewSymbolToSample(rrcTaps5, TXSymbolScalingCoeff*transmitGain, false, 5),
		cmdSource:  make(chan commandV2, 1),
		lastTXData: time.Now(),
	}
	ret.txState = txIdleV2

	log.Printf("[DEBUG] Opening modem")
	err = ret.gpioSetup(nRSTPin, boot0Pin)
	if err != nil {
		return nil, err
	}
	mode := &serial.Mode{
		BaudRate: baudRate,
	}
	ret.modem, err = serial.Open(port, mode)
	if err != nil {
		return nil, fmt.Errorf("modem open: %w", err)
	}
	rxSource := make(chan int8, samplesPerSecond)
	ret.rxSymbols, err = ret.rxPipeline(rxSource)
	if err != nil {
		return nil, fmt.Errorf("rx pipeline setup: %w", err)
	}
	var zmqSource chan byte
	if zmqPort > 0 {
		zmqSource = make(chan byte, samplesPerSecond)
		pub := zmq4.NewPub(context.Background())
		// defer pub.Close()
		err := pub.Listen(fmt.Sprintf("tcp://*:%d", zmqPort))
		if err != nil {
			log.Fatalf("Could not listen on ZeroMQ port %d: %v", zmqPort, err)
		}
		go func() {
			buf := make([]byte, 0, 2048)
			for {
				buf = append(buf, <-zmqSource)
				if len(buf) == 2048 {
					pub.Send(zmq4.NewMsg(buf))
					buf = make([]byte, 0, 2048)
				}
			}
		}()
	}

	go ret.processReceivedData(rxSource, zmqSource)
	log.Printf("[DEBUG] ping()")
	_, err = ret.commandWithResponse(newCommandV2(cc1200V2CmdPing, []byte{}))
	if err != nil {
		return nil, fmt.Errorf("test PING: %w", err)
	}
	// ret.debugLog, err = os.OpenFile("/home/jim/debug.sym", os.O_CREATE|os.O_WRONLY, 0644)
	// if err != nil {
	// 	log.Printf("[DEBUG] Failure opening debug log: %v", err)
	// } else {
	// 	log.Printf("[DEBUG] Opened debug log: %v", ret.debugLog)
	// }
	err = ret.setRXFreq(rxFrequency)
	if err != nil {
		return nil, fmt.Errorf("setRXFreq: %w", err)
	}
	err = ret.setTXFreq(txFrequency)
	if err != nil {
		return nil, fmt.Errorf("setTXFreq: %w", err)
	}
	err = ret.setTXPower(power)
	if err != nil {
		return nil, fmt.Errorf("setTXPower: %w", err)
	}
	err = ret.setFreqCorrection(frequencyCorr)
	if err != nil {
		return nil, fmt.Errorf("setFreqCorrection: %w", err)
	}
	err = ret.setAFC(afc)
	if err != nil {
		return nil, fmt.Errorf("setAFC: %w", err)
	}

	return ret, nil
}

func (m *CC1200ModemV2) StartDecoding(sink func(typ uint16, softBits []SoftBit)) {
	m.frameSink = sink
	go m.processSymbols()
}

func (m *CC1200ModemV2) processReceivedData(rxSource chan int8, zmqSource chan byte) {
	var buf []byte
	var prevCmd commandV2
	var badBuf []byte
	var badCnt int
	for {
		var n int
		var err error
		if badBuf == nil {
			buf = make([]byte, 3) // cmd + size
			n, err = io.ReadFull(m.modem, buf)
			if err != nil {
				log.Printf("[ERROR] Error reading cmd from modem: %v", err)
				break
			}
		} else {
			// Advance one byte and try again
			// Shift the last two bytes to the left and add an empty one to read into
			buf = append(buf[1:], 0)
			// Read buf[2] so now we have three again
			n, err = io.ReadFull(m.modem, buf[2:])
			if err != nil {
				log.Printf("[ERROR] Error reading cmd from modem: %v", err)
				break
			}
		}
		// buf has cmd + size but not data
		cmd, err := newCommandV2FromBytes(buf)
		if err == errBadCmd {
			// save the bad bytes to log them
			badBuf = append(badBuf, buf[3-n:]...)
			badCnt += n
			continue
		} else if err != nil {
			log.Printf("[ERROR] Error building command: %v", err)
			break
		}

		_, err = io.ReadFull(m.modem, cmd.data)
		if err != nil {
			log.Printf("[ERROR] Error reading data from modem: %v", err)
			break
		}
		if badCnt > 0 {
			log.Printf("[ERROR] received %v bad bytes: [% x]\n    prev cmd: %v\n    next cmd: %v", badCnt, badBuf, prevCmd, cmd)
		}
		badBuf = nil
		badCnt = 0
		prevCmd = cmd
		if cmd.cmd == cc1200V2CmdRXData {
			for _, b := range cmd.data {
				select {
				case rxSource <- int8(b):
					// sent
				default:
					// pipeline is full, so drop it
					log.Printf("[DEBUG] processReceivedData dropped rx: %02x", b)
				}
				if zmqSource != nil {
					select {
					case zmqSource <- b:
						// sent
					default:
						// pipeline is full, so drop it
					}
				}
			}
		} else {
			select {
			case m.cmdSource <- cmd:
			default:
				// Channel is full, so drop this cmd
				log.Printf("[ERROR] processReceivedData dropped cmd: %d, size: %d, prevCmd: %d, size: %d",
					cmd.cmd, cmd.size, prevCmd.cmd, prevCmd.size)
			}
		}
	}
}

func (m *CC1200ModemV2) processSymbols() {
	var symbols []Symbol
	// logTicker := time.NewTicker(time.Second)
	// defer logTicker.Stop()

	for {
		// Refill symbol buffer
		// log.Printf("[DEBUG] Refill symbol buffer: %d", symbolBufSize-len(symbols))
		for range symbolBufSize - len(symbols) {
			symbols = append(symbols, Symbol(<-m.rxSymbols))
		}
		// select {
		// case <-logTicker.C:
		// 	log.Printf("[DEBUG] symbols: %v", symbols)
		// default:
		// 	//
		// }

		// Looking for a sync burst
		//calculate euclidean norm
		dist, typ := syncDistance(symbols, 0)
		switch {
		case typ == LSFSync && dist < 4.5:
			log.Printf("[DEBUG] Received LSFSync, distance: %f, type: %x", dist, typ)
			// log.Printf("[DEBUG] symbols: %v", symbols)
			var pld []SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols)
			m.frameSink(typ, pld)

		case typ == PacketSync && dist < 5.0:
			log.Printf("[DEBUG] Received PacketSync, distance: %f, type: %x", dist, typ)
			// log.Printf("[DEBUG] symbols: %v", symbols)
			var pld []SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols)
			m.frameSink(typ, pld)

		case typ == StreamSync && dist < 5.0:
			log.Printf("[DEBUG] Received StreamSync, distance: %f, type: %x", dist, typ)
			// log.Printf("[DEBUG] symbols: %v", symbols)
			var pld []SoftBit
			symbols, pld, _ = extractPayload(dist, typ, symbols)
			m.frameSink(typ, pld)
		case typ == EOTMarker && dist < 4.5:
			log.Printf("[DEBUG] Received EOTMarker, distance: %f, type: %x", dist, typ)
			// log.Printf("[DEBUG] symbols: %v", symbols)
			symbols = symbols[16*5:]
			m.frameSink(typ, nil)
		default:
			// No one read anything, so advance one symbol
			symbols = symbols[1:]
		}
	}
}

func (m *CC1200ModemV2) rxPipeline(sampleSource chan int8) (chan float32, error) {
	// modem samples --> to float64 --> IIR filter --> RRC filter & scale
	var err error

	conv := NewConverter[int8, float64](sampleSource)

	// dcf, err := NewDCFilter(sampleSource, 200) //len(rrcTaps5))
	// if err != nil {
	// 	return nil, fmt.Errorf("dc filter: %w", err)
	// }

	iir, err := NewIIRFilter(conv.Source(), iirBParam, iirAParam)
	if err != nil {
		return nil, fmt.Errorf("iir filter: %w", err)
	}

	s2s := NewSampleToSymbol(iir.Source(), rrcTaps5, RXSymbolScalingCoeff)
	// ds, err := NewDownsampler(s2s.Source(), 5, 0)
	// if err != nil {
	// 	return nil, fmt.Errorf("downsampler: %w", err)
	// }
	return s2s.Source(), nil
}

func (m *CC1200ModemV2) setNRSTGPIO(set bool) error {
	if m.nRST == nil {
		// Emulation mode
		return nil
	}
	log.Printf("[DEBUG] setNRSTGPIO(%v)", set)
	if set {
		return m.nRST.SetValue(1)
	}
	return m.nRST.SetValue(0)
}

func (m *CC1200ModemV2) setBoot0GPIO(set bool) error {
	if m.boot0 == nil {
		// Emulation mode
		return nil
	}
	log.Printf("[DEBUG] setBoot0GPIO(%v)", set)
	if set {
		return m.boot0.SetValue(1)
	}
	return m.boot0.SetValue(0)
}

// Reset the modem
func (m *CC1200ModemV2) Reset() error {
	log.Print("[DEBUG] modem Reset()")
	err1 := m.setBoot0GPIO(false)
	err2 := m.setNRSTGPIO(false)
	time.Sleep(50 * time.Millisecond)
	err3 := m.setNRSTGPIO(true)
	errs := errors.Join(err1, err2, err3)
	if errs != nil {
		return fmt.Errorf("modem reset: %w", errs)
	}
	return nil
}

// Close the modem
func (m *CC1200ModemV2) Close() error {
	log.Print("[DEBUG] modem Close()")
	m.stopRX()
	m.stopTX()
	m.nRST.Close()
	m.boot0.Close()
	if m.debugLog != nil {
		m.debugLog.Close()
	}
	return m.modem.Close()
}

func (m *CC1200ModemV2) TransmitPacket(p Packet) error {
	log.Printf("[DEBUG] TransmitPacket: %v", p)
	m.stopRX()
	time.Sleep(2 * time.Millisecond)
	m.startTX()
	time.Sleep(10 * time.Millisecond)

	var syms []Symbol
	//fill preamble
	syms = AppendPreamble(syms, lsfPreamble)
	err := m.writeSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send preamble: %w", err)
	}
	syms, err = generateLSFSymbols(p.LSF)
	if err != nil {
		return fmt.Errorf("failed to generate LSF symbols: %w", err)
	}
	err = m.writeSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send LSF: %w", err)
	}

	chunkCnt := 0
	packetData := p.PayloadBytes()
	for bytesLeft := len(packetData); bytesLeft > 0; bytesLeft -= 25 {
		syms = AppendSyncwordSymbols(syms, PacketSync)
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
		syms = AppendBits(syms, rfBits)
		err = m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send: %w", err)
		}
		time.Sleep(FrameTime)
		chunkCnt++
	}
	syms = AppendEOT(syms)
	err = m.writeSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send EOT: %w", err)
	}
	log.Printf("[DEBUG] Finished TransmitPacket")
	time.Sleep(10 * FrameTime)
	log.Printf("[DEBUG] Finished TransmitPacket wait")
	m.stopTX()
	m.Start()
	return nil
}

func (m *CC1200ModemV2) TransmitVoiceStream(sd StreamDatagram) error {
	// log.Printf("[DEBUG] TransmitVoiceStream id: %04x, fn: %04x, last: %v", sd.StreamID, sd.FrameNumber, sd.LastFrame)
	m.mutex.Lock()
	if m.txState != txTXV2 {
		// First frame
		m.mutex.Unlock()
		log.Printf("[DEBUG] Sending first frame of stream %x, fn %d, lsf: %v", sd.StreamID, sd.FrameNumber, sd.LSF)
		m.stopRX()
		time.Sleep(2 * time.Millisecond)
		m.startTX()
		m.lastTXData = time.Now()
		time.Sleep(10 * time.Millisecond)

		var syms []Symbol
		//fill preamble
		syms = AppendPreamble(syms, lsfPreamble)
		err := m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send preamble: %w", err)
		}
		syms, err = generateLSFSymbols(sd.LSF)
		if err != nil {
			return fmt.Errorf("failed to generate LSF symbols: %w", err)
		}
		err = m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send LSF: %w", err)
		}
		syms, err = generateStreamSymbols(sd)
		if err != nil {
			return fmt.Errorf("failed to generate LSF symbols: %w", err)
		}
		err = m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send stream frame: %w", err)
		}
	} else {
		m.mutex.Unlock()
		// log.Printf("[DEBUG] Sending frame of stream %x, fn %d", sd.StreamID, sd.FrameNumber)
		syms, err := generateStreamSymbols(sd)
		if err != nil {
			return fmt.Errorf("failed to generate LSF symbols: %w", err)
		}
		err = m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send stream frame: %w", err)
		}
	}
	if sd.LastFrame {
		// send EOT
		log.Printf("[DEBUG] Sending EOT for stream %04x, fn %04x", sd.StreamID, sd.FrameNumber)
		syms := AppendEOT(nil)
		err := m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send EOT: %w", err)
		}
		log.Printf("[DEBUG] Finished TransmitVoiceStream")
		time.Sleep(txVoiceStreamWait)
		log.Printf("[DEBUG] Finished TransmitVoiceStream wait")
		m.stopTX()
		m.Start()
	}
	return nil
}

func (m *CC1200ModemV2) startTX() error {
	log.Printf("[DEBUG] startTX()")
	err := m.commandWithErrResponse(newCommandV2(cc1200V2CmdTXStart, []byte{1}))
	if err != nil {
		log.Printf("[ERROR] startTX(): %v", err)
		return fmt.Errorf("start TX: %w", err)
	}
	m.mutex.Lock()
	m.txState = txTXV2
	m.mutex.Unlock()
	return nil
}

func (m *CC1200ModemV2) stopTX() {
	log.Print("[DEBUG] modem stopTX()")
	m.mutex.Lock()
	// Only stop if we've started
	if m.txState == txTXV2 {
		m.mutex.Unlock()
		// log.Print("[DEBUG] modem stopping TX")
		err := m.commandWithErrResponse(newCommandV2(cc1200V2CmdTXStart, []byte{0}))
		if err != nil {
			log.Printf("[ERROR] stopTX(): %v", err)
		}
		m.mutex.Lock()
		m.txState = txIdleV2
	}
	m.mutex.Unlock()
}

func (m *CC1200ModemV2) setTXFreq(freq uint32) error {
	log.Printf("[DEBUG] setTXFreq(%v)", freq)
	data, err := binary.Append(nil, binary.LittleEndian, freq)
	if err != nil {
		return fmt.Errorf("encode set TX freq: %w", err)
	}

	cmd := newCommandV2(cc1200V2CmdSetTXFreq, data)
	err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set TX freq: %w", err)
	}
	return nil
}
func (m *CC1200ModemV2) setTXPower(dbm int8) error {
	log.Printf("[DEBUG] setTXPower(%v)", dbm)
	cmd := newCommandV2(cc1200V2CmdSetTXPower, []byte{byte(dbm)})
	err := m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set TX power: %w", err)
	}
	return nil
}

func (m *CC1200ModemV2) Start() error {
	// log.Printf("[DEBUG] Start()")
	m.mutex.Lock()
	m.txState = txIdleV2
	m.mutex.Unlock()
	// log.Printf("[DEBUG] sending start cmd")
	err := m.commandWithErrResponse(newCommandV2(cc1200V2CmdRXStart, []byte{1}))
	if err != nil {
		log.Printf("[ERROR] Start(): %v", err)
		return fmt.Errorf("send set RX start error: %w", err)
	}
	// log.Printf("[DEBUG] end Start()")
	return nil
}

func (m *CC1200ModemV2) stopRX() error {
	m.mutex.Lock()
	// Only stop if we've started
	if m.txState == txIdleV2 {
		m.mutex.Unlock()
		log.Printf("[DEBUG] stopRX()")
		err := m.commandWithErrResponse(newCommandV2(cc1200V2CmdRXStart, []byte{0}))
		if err != nil {
			log.Printf("[ERROR] stopRX(): %v", err)
			return fmt.Errorf("send set RX stop: %w", err)
		}
		m.mutex.Lock()
	}
	m.mutex.Unlock()
	return nil
}
func (m *CC1200ModemV2) setRXFreq(freq uint32) error {
	log.Printf("[DEBUG] setRXFreq(%v)", freq)
	data, err := binary.Append(nil, binary.LittleEndian, freq)
	if err != nil {
		return fmt.Errorf("encode set RX freq: %w", err)
	}

	cmd := newCommandV2(cc1200V2CmdSetRXFreq, data)
	err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set RX freq: %w", err)
	}
	return nil
}
func (m *CC1200ModemV2) setAFC(afc bool) error {
	log.Printf("[DEBUG] setAFC(%v)", afc)
	var err error
	var a byte
	if afc {
		a = 1
	}
	cmd := newCommandV2(cc1200V2CmdSetAFC, []byte{a})
	err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set AFC: %w", err)
	}
	return nil
}
func (m *CC1200ModemV2) setFreqCorrection(corr int16) error {
	log.Printf("[DEBUG] setFreqCorrection(%v)", corr)
	data, err := binary.Append(nil, binary.LittleEndian, corr)
	if err != nil {
		return fmt.Errorf("encode set RX freq: %w", err)
	}
	cmd := newCommandV2(cc1200V2CmdSetFreqCorr, data)
	err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set freq corr: %w", err)
	}
	return nil
}
func (m *CC1200ModemV2) writeSymbols(symbols []Symbol) error {
	buf := m.s2s.Transform(symbols)
	if m.debugLog != nil {
		_, err := m.debugLog.Write(buf)
		if err != nil {
			log.Printf("[DEBUG] Failed to write to debug log: %v", err)
		}
	}
	cmd := newCommandV2(cc1200V2CmdTXData, buf)
	err := m.commandWithErrResponse(cmd)
	since := time.Since(m.lastTXData)
	if since > 4*FrameTime {
		log.Printf("[DEBUG] Last TX data sent %v ago", since.Round(time.Millisecond))
	}
	m.lastTXData = time.Now()
	return err
}
func (m *CC1200ModemV2) commandWithErrResponse(cmd commandV2) error {
	var err error
	var respErr int
	respCmd, err := m.commandWithResponse(cmd)
	if err != nil {
		return fmt.Errorf("commandWithResponse error: %w", err)
	}
	// log.Printf("[DEBUG] respBuf: % x", respBuf)
	switch len(respCmd.data) {
	case 1:
		respErr = int(respCmd.data[0])
	case 4:
		_, err = binary.Decode(respCmd.data, binary.LittleEndian, respErr)
		if err != nil {
			return fmt.Errorf("parse modem response: %v", err)
		}
	default:
		return fmt.Errorf("unexpected response: %#v", respCmd)
	}
	// log.Printf("[DEBUG] respErr: %#v", respErr)
	if respErr != 0 {
		return fmt.Errorf("modem response: %d", respErr)
	}
	return nil
}

func (m *CC1200ModemV2) command(cmd commandV2) error {
	// log.Printf("[DEBUG] command(): %v", cmd)
	b, err := cmd.Bytes()
	if err != nil {
		return fmt.Errorf("command: %w", err)
	}
	// log.Printf("[DEBUG] modem.Write(): [% x]", b)
	_, err = m.modem.Write(b)
	if err != nil {
		return fmt.Errorf("command: %w", err)
	}
	return nil
}
func (m *CC1200ModemV2) commandWithResponse(cmd commandV2) (commandV2, error) {
	// log.Printf("[DEBUG] commandWithResponse() sending: %v", cmd)
	// clear old responses
	for more := true; more; {
		select {
		case c := <-m.cmdSource:
			log.Printf("[DEBUG] old response: %v", c)
			more = true
		default:
			more = false
		}
	}

	err := m.command(cmd)
	if err != nil {
		return commandV2{}, err
	}
	var resp commandV2
	select {
	case resp = <-m.cmdSource:
	case <-time.After(1 * time.Second):
		err = fmt.Errorf("response to command %v timed out", cmd)
	}
	return resp, err
}
