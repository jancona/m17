package m17

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
)

const (
	samplesPerSecond = 24000
	samplesPer40MS   = samplesPerSecond / 1000 * 40
	samplesPerSymbol = 5
	symbolsPerSecond = samplesPerSecond / samplesPerSymbol
	symbolsPer40MS   = symbolsPerSecond / 1000 * 40
)

// CC1200 commands
const (
	cmdPing = iota
	//SET
	cmdSetRXFreq
	cmdSetTXFreq
	cmdSetTXPower
	cmdSetReserved
	cmdSetFreqCorr
	cmdSetAFC
	cmdSetTXStart
	cmdSetRX
)

// const (
//
//	//GET
//	cmdGetIdent = iota + 0x80
//	cmdGetCaps
//	cmdGetRXFreq
//	cmdGetTXFreq
//	cmdGetTXPower
//	cmdGetFreqCorr
//
// )
const (
	trxIdle = iota
	trxRX
	trxTX
)

const txEndDuration = 240 * time.Millisecond

type Line interface {
	SetValue(value int) error
	Close() error
}

type CC1200Modem struct {
	modem     io.ReadWriteCloser
	rxSymbols chan float32
	txSymbols chan float32
	// trxMutex  sync.Mutex
	txStart   atomic.Value // time.Time
	txTicker  *time.Ticker
	trxState  atomic.Int32
	cmdSource chan byte
	nRST      Line
	paEnable  Line
	boot0     Line
}

func NewCC1200Modem(
	port string,
	nRSTPin int,
	paEnablePin int,
	boot0Pin int,
	baudRate int) (*CC1200Modem, error) {
	ret := CC1200Modem{
		rxSymbols: make(chan float32),
		txSymbols: make(chan float32, symbolsPerSecond),
		txTicker:  time.NewTicker(txEndDuration / 2),
		cmdSource: make(chan byte),
	}
	ret.trxState.Store(trxIdle)
	ret.txStart.Store(time.Time{})
	ret.txTicker.Stop()
	var err error
	fi, err := os.Stat(port)
	if err != nil {
		return nil, fmt.Errorf("modem stat: %w", err)
	}
	// err = ret.gpioSetup(nRSTPin, paEnablePin, boot0Pin)
	// if err != nil {
	// 	return nil, err
	// }
	if fi.Mode()&os.ModeSocket == os.ModeSocket {
		log.Printf("[DEBUG] Opening emulator")
		ret.modem, err = net.Dial("unix", port)
		if err != nil {
			return nil, fmt.Errorf("modem socket open: %w", err)
		}
		// This is the emulator so don't initialize GPIO
	} else {
		log.Printf("[DEBUG] Opening modem")
		err = ret.gpioSetup(nRSTPin, paEnablePin, boot0Pin)
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
	}
	rxSource := make(chan int8, samplesPerSecond)
	ret.rxSymbols, err = ret.rxPipeline(rxSource)
	if err != nil {
		return nil, fmt.Errorf("rx pipeline setup: %w", err)
	}
	go ret.processReceivedData(rxSource)

	txSamples, err := ret.txPipeline(ret.txSymbols)
	if err != nil {
		return nil, fmt.Errorf("tx pipeline setup: %w", err)
	}
	go ret.processTXSamples(txSamples)
	_, err = ret.commandWithResponse([]byte{cmdPing, 2})
	if err != nil {
		return nil, fmt.Errorf("test PING: %w", err)
	}
	return &ret, nil
}
func (m *CC1200Modem) processTXSamples(txSamples chan int8) {
	ticker := time.NewTicker(40 * time.Millisecond)
	w := bufio.NewWriterSize(m.modem, samplesPer40MS)
	for {
		select {
		case sample := <-txSamples:
			// TODO: Mutex?
			_, err := w.Write([]byte{byte(sample)})
			if err != nil {
				log.Printf("[ERROR] Error writing to modem: %v", err)
				return
			}
		case <-ticker.C:
			// TODO: Mutex?
			w.Flush()
		}
	}
}
func (m *CC1200Modem) txPipeline(symbolSource chan float32) (chan int8, error) {
	// gateway symbols -> upsample and RRC filter -> modem
	s2s := NewSymbolToSample(symbolSource, rrcTaps5, TXSymbolScalingCoeff*transmitGain, false, samplesPerSymbol)
	return s2s.Source(), nil
}
func (m *CC1200Modem) processReceivedData(rxSource chan int8) {
	buf := make([]byte, 1)
	for {
		n, err := m.modem.Read(buf)
		if n > 0 {
			if m.trxState.Load() == trxRX {
				select {
				case rxSource <- int8(buf[0]):
					// sent
					// log.Printf("[DEBUG] sent rx: %x", buf[0])
				default:
					// pipeline is full, so drop it
					log.Printf("[DEBUG] processReceivedData dropped rx: %x", buf[0])
				}
			} else {
				log.Printf("[DEBUG] processReceivedData cmd: %x", buf[0])
				m.cmdSource <- buf[0]
			}
		}
		if err != nil {
			log.Printf("[ERROR] Error reading from modem: %v", err)
			break
		}
	}
}
func (m *CC1200Modem) rxPipeline(sampleSource chan int8) (chan float32, error) {
	// modem samples -> DC filter --> RRC filter & scale
	var err error
	dcf, err := NewDCFilter(sampleSource, len(rrcTaps5))
	if err != nil {
		return nil, fmt.Errorf("dc filter: %w", err)
	}
	s2s := NewSampleToSymbol(dcf.Source(), rrcTaps5, RXSymbolScalingCoeff)
	// ds, err := NewDownsampler(s2s.Source(), samplesPerSymbol, 0)
	// if err != nil {
	// 	return nil, fmt.Errorf("downsampler: %w", err)
	// }
	return s2s.Source(), nil
}
func (m *CC1200Modem) isTransmitting() bool {
	tx := !m.txStart.Load().(time.Time).IsZero()
	return tx
}
func (m *CC1200Modem) updateTXTimeout() {
	m.txStart.Store(time.Now())
}
func (m *CC1200Modem) txWatchdog() {
	for {
		<-m.txTicker.C
		timedOut := time.Since(m.txStart.Load().(time.Time)) > txEndDuration
		if timedOut {
			m.txTicker.Stop()
		}
		if timedOut && m.isTransmitting() {
			m.StopTX()
			err := m.setPAEnableGPIO(false)
			if err != nil {
				log.Printf("[DEBUG] Stop TX PA disable: %v", err)
			}
			m.StartRX()
			return
		}

	}
}

func (m *CC1200Modem) setNRSTGPIO(set bool) error {
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

func (m *CC1200Modem) setPAEnableGPIO(set bool) error {
	if m.paEnable == nil {
		// Emulation mode
		return nil
	}
	log.Printf("[DEBUG] setPAEnableGPIO(%v)", set)
	if set {
		return m.paEnable.SetValue(1)
	}
	return m.paEnable.SetValue(0)
}

func (m *CC1200Modem) setBoot0GPIO(set bool) error {
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
func (m *CC1200Modem) Reset() error {
	log.Print("[DEBUG] modem Reset()")
	err1 := m.setBoot0GPIO(false)
	err2 := m.setPAEnableGPIO(false)
	err3 := m.setNRSTGPIO(false)
	time.Sleep(50 * time.Millisecond)
	err4 := m.setNRSTGPIO(true)
	errs := errors.Join(err1, err2, err3, err4)
	if errs != nil {
		return fmt.Errorf("modem reset: %w", errs)
	}
	return nil
}

// Close the modem
func (m *CC1200Modem) Close() error {
	log.Print("[DEBUG] modem Close()")
	if m.isTransmitting() {
		m.StopTX()
		err := m.setPAEnableGPIO(false)
		if err != nil {
			log.Printf("[DEBUG] Close PA disable: %v", err)
		}
	}
	m.nRST.Close()
	m.paEnable.Close()
	m.boot0.Close()
	return m.modem.Close()
}

func (m *CC1200Modem) StopTX() {
	log.Print("[DEBUG] modem StopTX()")
	m.txStart.Store(time.Time{})
	m.trxState.Store(trxIdle)
	time.Sleep(txEndDuration)
}

// Read received symbols
func (m *CC1200Modem) Read(buf []byte) (n int, err error) {
	sBuf := make([]float32, len(buf)/4)
	for i := range sBuf {
		sBuf[i] = <-m.rxSymbols
	}
	sb, err := binary.Append(nil, binary.LittleEndian, sBuf)
	if err != nil {
		return 0, fmt.Errorf("append symbol: %w", err)
	}
	cnt := copy(buf, sb)
	// log.Printf("[DEBUG] Modem.read requested %d, returned %d bytes", req, cnt)
	return cnt, nil
}

// Send symbols to transmit. If no symbols are received for more than `txEndDuration` milliseconds,
// the transmission will end.
func (m *CC1200Modem) Write(b []byte) (n int, err error) {
	if !m.isTransmitting() {
		log.Printf("[DEBUG] Switch to transmit")
		err = m.EndRX()
		if err != nil {
			err = fmt.Errorf("end RX: %w", err)
			return
		}
		time.Sleep(2 * time.Millisecond)
		err = m.setPAEnableGPIO(true)
		if err != nil {
			log.Printf("[DEBUG] Start TX PAEnable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
		err = m.StartTX()
		if err != nil {
			err = fmt.Errorf("start TX: %w", err)
			return
		}
	}
	symbols := make([]float32, len(b)/4)
	n, err = binary.Decode(b, binary.LittleEndian, symbols)
	if err != nil {
		err = fmt.Errorf("decode symbols: %w", err)
		return
	}
	log.Printf("[DEBUG] Write symbols: % f", symbols)
	for _, s := range symbols {
		m.txSymbols <- s
	}
	m.updateTXTimeout()
	if n < len(b) {
		// should only happen if len(b) is not a multiple of 4, i.e. the last symbol is incomplete
		err = fmt.Errorf("malformed transmit stream")
	}
	return
}

func (m *CC1200Modem) StartTX() error {
	log.Printf("[DEBUG] StartTX()")
	if m.isTransmitting() {
		return fmt.Errorf("already transmitting")
	}
	_, err := m.commandWithErrResponse([]byte{cmdSetTXStart, 2})
	if err != nil {
		return fmt.Errorf("start TX: %w", err)
	}
	m.txStart.Store(time.Now())
	m.trxState.Store(trxTX)
	m.txTicker.Reset(txEndDuration / 2)
	go m.txWatchdog()
	return nil
}

func (m *CC1200Modem) SetTXFreq(freq uint32) error {
	log.Printf("[DEBUG] SetTXFreq(%v)", freq)
	var err error
	cmd := []byte{cmdSetTXFreq, 0}
	cmd, err = binary.Append(cmd, binary.LittleEndian, freq)
	if err != nil {
		return fmt.Errorf("encode set TX freq: %w", err)
	}
	_, err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set TX freq: %w", err)
	}
	return nil
}
func (m *CC1200Modem) SetTXPower(dbm float32) error {
	log.Printf("[DEBUG] SetTXPower(%v)", dbm)
	var err error
	cmd := []byte{cmdSetTXPower, 0}
	cmd, err = binary.Append(cmd, binary.LittleEndian, int8(dbm*4))
	if err != nil {
		return fmt.Errorf("encode set TX power: %w", err)
	}
	_, err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set TX power: %w", err)
	}
	return nil
}

func (m *CC1200Modem) StartRX() error {
	log.Printf("[DEBUG] StartRX()")
	m.trxState.Store(trxRX)
	var err error
	cmd := []byte{cmdSetRX, 0, 1}
	err = m.command(cmd)
	if err != nil {
		return fmt.Errorf("send set RX start: %w", err)
	}
	return nil
}

func (m *CC1200Modem) EndRX() error {
	log.Printf("[DEBUG] EndRX()")
	var err error
	cmd := []byte{cmdSetRX, 0, 0}
	err = m.command(cmd)
	if err != nil {
		return fmt.Errorf("send set RX stop: %w", err)
	}
	m.trxState.Store(trxIdle)
	return nil
}
func (m *CC1200Modem) SetRXFreq(freq uint32) error {
	log.Printf("[DEBUG] SetRXFreq(%v)", freq)
	var err error
	cmd := []byte{cmdSetRXFreq, 0}
	cmd, err = binary.Append(cmd, binary.LittleEndian, freq)
	if err != nil {
		return fmt.Errorf("encode set RX freq: %w", err)
	}
	_, err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set RX freq: %w", err)
	}
	return nil
}
func (m *CC1200Modem) SetAFC(afc bool) error {
	log.Printf("[DEBUG] SetAFC(%v)", afc)
	var err error
	var a byte
	if afc {
		a = 1
	}
	cmd := []byte{cmdSetAFC, 0, a}
	_, err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set AFC: %w", err)
	}
	return nil
}
func (m *CC1200Modem) SetFreqCorrection(corr int16) error {
	log.Printf("[DEBUG] SetFreqCorrection(%v)", corr)
	var err error
	cmd := []byte{cmdSetFreqCorr, 0}
	cmd, err = binary.Append(cmd, binary.LittleEndian, corr)
	if err != nil {
		return fmt.Errorf("encode set freq corr: %w", err)
	}
	_, err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set freq corr: %w", err)
	}
	return nil
}

func (m *CC1200Modem) commandWithErrResponse(cmd []byte) (int, error) {
	var err error
	var respErr int
	respBuf, err := m.commandWithResponse(cmd)
	if err != nil {
		return respErr, fmt.Errorf("send cmd: %w", err)
	}
	// log.Printf("[DEBUG] respBuf: % x", respBuf)
	switch len(respBuf) {
	case 1:
		respErr = int(respBuf[0])
	case 4:
		_, err = binary.Decode(respBuf, binary.LittleEndian, respErr)
		if err != nil {
			return respErr, fmt.Errorf("parse modem response: %d", respErr)
		}
	default:
		return 0, fmt.Errorf("unexpected response: %#v", respBuf)
	}
	// log.Printf("[DEBUG] respErr: %#v", respErr)
	if respErr != 0 {
		return respErr, fmt.Errorf("modem response: %d", respErr)
	}
	return 0, nil
}

func (m *CC1200Modem) command(cmd []byte) error {
	if len(cmd) < 2 {
		return fmt.Errorf("command cmd length < 2")
	}
	cmd[1] = byte(len(cmd))
	var err error
	_, err = m.modem.Write(cmd)
	if err != nil {
		return fmt.Errorf("command: %w", err)
	}
	log.Printf("[DEBUG] sent cmd: %#v", cmd)
	return nil
}
func (m *CC1200Modem) commandWithResponse(cmd []byte) ([]byte, error) {
	m.clearResponseBuf()
	err := m.command(cmd)
	if err != nil {
		return nil, err
	}
	resp, err := m.commandResponse()
	if err != nil {
		return nil, fmt.Errorf("sendCommand response: %w", err)
	}
	// log.Printf("[DEBUG] got resp: %#v", resp)
	return resp, nil
}

func (m *CC1200Modem) clearResponseBuf() {
	for {
		select {
		case b := <-m.cmdSource:
			log.Printf("[DEBUG] discarding: %x", b)
		default:
			return
		}
	}
}
func (m *CC1200Modem) commandResponse() ([]byte, error) {
	buf := make([]byte, 2)
	// log.Printf("[DEBUG] reading 2 bytes")
	buf[0] = <-m.cmdSource
	buf[1] = <-m.cmdSource
	// log.Printf("[DEBUG] reading rest: %d", buf[1]-2)
	buf = make([]byte, buf[1]-2)
	for i := range buf {
		buf[i] = <-m.cmdSource
	}
	log.Printf("[DEBUG] resp: % x", buf)
	return buf, nil
}
