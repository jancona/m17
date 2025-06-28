package m17

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
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

// const txEndDuration = 400 * time.Millisecond

type Line interface {
	SetValue(value int) error
	Close() error
}

type Modem interface {
	io.ReadCloser
	TransmitPacket(p Packet) error
	// Read(buf []byte) (n int, err error)
	// WriteSymbols(s []Symbol) (n int, err error)
	// Close() error
	StartRX() error
	// EndRX() error
	// StartTX() error
	// StopTX()
	Reset() error
	SetAFC(afc bool) error
	SetFreqCorrection(corr int16) error
	SetRXFreq(freq uint32) error
	SetTXFreq(freq uint32) error
	SetTXPower(dbm float32) error
}
type DummyModem struct {
	In    io.ReadCloser
	Out   io.WriteCloser
	extra []byte
}

func (m *DummyModem) TransmitPacket(p Packet) error {
	encoded, err := p.Encode()
	if err != nil {
		return err
	}
	err = binary.Write(m.Out, binary.LittleEndian, encoded)
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}
	return nil
}

func (m *DummyModem) Read(p []byte) (n int, err error) {
	l := len(p)
	el := len(m.extra)
	// log.Printf("[DEBUG] Attempting to read %d bytes, el: %d", l, el)
	if el < l {
		rl := (l - el) / 5
		if rl%5 > 0 {
			// make sure we have enough symbols
			rl++
		}
		// Get whole symbols
		rl += rl % 4
		sBuff := make([]byte, rl)
		// log.Printf("[DEBUG] Attempting to read %d bytes", len(sBuff))
		nn, err := m.In.Read(sBuff)
		if err != nil {
			log.Printf("[ERROR] DummyModem Read failed: %v", err)
			if nn == 0 {
				return 0, err
			}
		}
		// log.Printf("[DEBUG] Read %d bytes: % x", nn, sBuff)
		if nn%4 != 0 {
			panic("handle this!")
		}
		for i := 0; i < nn; i += 4 {
			// Repeat each read symbol 5 times
			for range 5 {
				m.extra = append(m.extra, sBuff[i:i+4]...)
			}
		}
		l = min(l, len(m.extra))
	}
	n = copy(p, m.extra[:l])
	m.extra = m.extra[l:]
	// log.Printf("[DEBUG] Returning %d bytes: % x", n, p)
	return
}

func (m *DummyModem) Write(buf []byte) (n int, err error) {
	return m.Out.Write(buf)
}
func (m *DummyModem) Reset() error {
	return nil
}
func (m *DummyModem) SetAFC(afc bool) error {
	return nil
}
func (m *DummyModem) SetFreqCorrection(corr int16) error {
	return nil
}
func (m *DummyModem) SetRXFreq(freq uint32) error {
	return nil
}
func (m *DummyModem) SetTXFreq(freq uint32) error {
	return nil
}
func (m *DummyModem) SetTXPower(dbm float32) error {
	return nil
}
func (m *DummyModem) StartRX() error {
	return nil
}

func (m *DummyModem) Close() error {
	err := m.In.Close()
	err2 := m.Out.Close()
	return errors.Join(err, err2)
}

type CC1200Modem struct {
	modem     io.ReadWriteCloser
	rxSymbols chan float32
	// txSymbols chan float32
	s2s SymbolToSample

	trxMutex sync.Mutex
	trxState int
	// lastSend  time.Time
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
		s2s:       NewSymbolToSample(rrcTaps5, TXSymbolScalingCoeff*transmitGain, false, samplesPerSymbol),
		cmdSource: make(chan byte),
	}
	ret.trxState = trxIdle
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

	// txSamples, err := ret.txPipeline(ret.txSymbols)
	// if err != nil {
	// 	return nil, fmt.Errorf("tx pipeline setup: %w", err)
	// }
	// go ret.processTXSamples(txSamples)
	_, err = ret.commandWithResponse([]byte{cmdPing, 2})
	if err != nil {
		return nil, fmt.Errorf("test PING: %w", err)
	}
	return &ret, nil
}

func (m *CC1200Modem) processReceivedData(rxSource chan int8) {
	buf := make([]byte, 1)
	for {
		// log.Printf("[DEBUG] processReceivedData Read()")
		n, err := m.modem.Read(buf)
		if n > 0 {
			// log.Printf("[DEBUG] processReceivedData read %x, trxState: %d", buf[0], m.trxState.Load())
			m.trxMutex.Lock()
			if m.trxState == trxRX {
				m.trxMutex.Unlock()
				select {
				case rxSource <- int8(buf[0]):
					// sent
					// log.Printf("[DEBUG] processReceivedData rxSource <- : %x", buf[0])
				default:
					// pipeline is full, so drop it
					log.Printf("[DEBUG] processReceivedData dropped rx: %x", buf[0])
				}
			} else {
				m.trxMutex.Unlock()
				log.Printf("[DEBUG] processReceivedData cmdSource <- : %x", buf[0])
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
	m.StopRX()
	m.StopTX()
	m.nRST.Close()
	m.paEnable.Close()
	m.boot0.Close()
	return m.modem.Close()
}

// Read received symbols
func (m *CC1200Modem) Read(buf []byte) (n int, err error) {
	// log.Printf("[DEBUG] Modem.read requested %d bytes", len(buf))
	sBuf := make([]float32, len(buf)/4)
	for i := range sBuf {
		sBuf[i] = <-m.rxSymbols
	}
	sb, err := binary.Append(nil, binary.LittleEndian, sBuf)
	if err != nil {
		return 0, fmt.Errorf("append symbol: %w", err)
	}
	cnt := copy(buf, sb)
	// log.Printf("[DEBUG] Modem.read returned  %d bytes", cnt)
	return cnt, nil
}

// Send symbols to transmit. If no symbols are received for more than `txEndDuration` milliseconds,
// the transmission will end.
// func (m *CC1200Modem) Write(b []byte) (n int, err error) {
// 	symbols := make([]float32, len(b)/4)
// 	n, err = binary.Decode(b, binary.LittleEndian, symbols)
// 	if err != nil {
// 		err = fmt.Errorf("decode symbols: %w", err)
// 		return
// 	}
// 	// log.Printf("[DEBUG] Write symbols: % f", symbols)
// 	for _, s := range symbols {
// 		m.txSymbols <- s
// 	}
// 	// m.updateTXTimeout()
// 	if n < len(b) {
// 		// should only happen if len(b) is not a multiple of 4, i.e. the last symbol is incomplete
// 		err = fmt.Errorf("malformed transmit stream")
// 	}
// 	return
// }

func (m *CC1200Modem) TransmitPacket(p Packet) error {
	log.Printf("[DEBUG] TransmitPacket: %v", p)
	m.StopRX()
	time.Sleep(2 * time.Millisecond)
	m.StartTX()
	time.Sleep(10 * time.Millisecond)

	var syms []Symbol
	//fill preamble
	syms = AppendPreamble(syms, lsfPreamble)
	err := m.writeSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send preamble: %w", err)
	}

	//send LSF syncword
	syms = AppendSyncword(syms, LSFSync)

	b, err := ConvolutionalEncode(p.LSF.ToBytes(), LSFPuncturePattern, LSFFinalBit)
	if err != nil {
		return fmt.Errorf("unable to encode LSF: %w", err)
	}
	encodedBits := NewBits(b)
	// encodedBits[0:len(b)] = b[:]
	rfBits := InterleaveBits(encodedBits)
	rfBits = RandomizeBits(rfBits)
	// Append LSF to the oputput
	syms = AppendBits(syms, rfBits)
	err = m.writeSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send LSF: %w", err)
	}

	chunkCnt := 0
	packetData := p.PayloadBytes()
	for bytesLeft := len(packetData); bytesLeft > 0; bytesLeft -= 25 {
		syms = AppendSyncword(syms, PacketSync)
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
		encodedBits := NewBits(b)
		rfBits := InterleaveBits(encodedBits)
		rfBits = RandomizeBits(rfBits)
		// Append chunk to the output
		syms = AppendBits(syms, rfBits)
		err = m.writeSymbols(syms)
		if err != nil {
			return fmt.Errorf("failed to send: %w", err)
		}
		time.Sleep(40 * time.Millisecond)
		chunkCnt++
	}
	syms = AppendEOT(syms)
	err = m.writeSymbols(syms)
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}
	log.Printf("[DEBUG] Finished TransmitPacket")
	time.Sleep(10 * 40 * time.Millisecond)
	log.Printf("[DEBUG] Finished TransmitPacket wait")
	m.StopTX()
	m.StartRX()
	return nil
}

func (m *CC1200Modem) StartTX() error {
	m.trxMutex.Lock()
	defer m.trxMutex.Unlock()
	log.Printf("[DEBUG] StartTX()")
	err := m.command([]byte{cmdSetTXStart, 2})
	if err != nil {
		return fmt.Errorf("start TX: %w", err)
	}
	err = m.setPAEnableGPIO(true)
	if err != nil {
		log.Printf("[DEBUG] Start TX PAEnable: %v", err)
	}
	m.trxState = trxTX
	return nil
}

func (m *CC1200Modem) StopTX() {
	m.trxMutex.Lock()
	defer m.trxMutex.Unlock()
	// Only stop if we've started
	if m.trxState == trxRX {

		log.Print("[DEBUG] modem StopTX()")
		err := m.setPAEnableGPIO(false)
		if err != nil {
			log.Printf("[DEBUG] End TX PAEnable: %v", err)
		}
		m.trxState = trxIdle
	}
}

func (m *CC1200Modem) SetTXFreq(freq uint32) error {
	log.Printf("[DEBUG] SetTXFreq(%v)", freq)
	var err error
	cmd := []byte{cmdSetTXFreq, 0}
	cmd, err = binary.Append(cmd, binary.LittleEndian, freq)
	if err != nil {
		return fmt.Errorf("encode set TX freq: %w", err)
	}
	err = m.commandWithErrResponse(cmd)
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
	err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set TX power: %w", err)
	}
	return nil
}

func (m *CC1200Modem) StartRX() error {
	m.trxMutex.Lock()
	defer m.trxMutex.Unlock()
	log.Printf("[DEBUG] StartRX()")
	m.trxState = trxRX
	m.clearResponseBuf()
	var err error
	cmd := []byte{cmdSetRX, 0, 1}
	err = m.command(cmd)
	if err != nil {
		return fmt.Errorf("send set RX start: %w", err)
	}
	return nil
}

func (m *CC1200Modem) StopRX() error {
	m.trxMutex.Lock()
	defer m.trxMutex.Unlock()
	// Only stop if we've started
	if m.trxState == trxRX {
		log.Printf("[DEBUG] StopRX()")
		var err error
		cmd := []byte{cmdSetRX, 0, 0}
		// Theoretically this returns a response, but how to find it in the received data
		err = m.command(cmd)
		if err != nil {
			return fmt.Errorf("send set RX stop: %w", err)
		}
		m.clearResponseBuf()
		m.trxState = trxIdle
	}
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
	err = m.commandWithErrResponse(cmd)
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
	err = m.commandWithErrResponse(cmd)
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
	err = m.commandWithErrResponse(cmd)
	if err != nil {
		return fmt.Errorf("send set freq corr: %w", err)
	}
	return nil
}
func (m *CC1200Modem) writeSymbols(symbols []Symbol) error {
	// fmt.Printf("symbols: % v\n", symbols)
	buf := m.s2s.Transform(symbols)
	// fmt.Printf("writeSymbols: % 2x\n", buf)
	_, err := m.modem.Write(buf)
	return err
}
func (m *CC1200Modem) commandWithErrResponse(cmd []byte) error {
	var err error
	var respErr int
	respBuf, err := m.commandWithResponse(cmd)
	if err != nil {
		return fmt.Errorf("commandWithResponse error: %w", err)
	}
	log.Printf("[DEBUG] respBuf: % x", respBuf)
	switch len(respBuf) {
	case 1:
		respErr = int(respBuf[0])
	case 4:
		_, err = binary.Decode(respBuf, binary.LittleEndian, respErr)
		if err != nil {
			return fmt.Errorf("parse modem response: %d", respErr)
		}
	default:
		return fmt.Errorf("unexpected response: %#v", respBuf)
	}
	log.Printf("[DEBUG] respErr: %#v", respErr)
	if respErr != 0 {
		return fmt.Errorf("modem response: %d", respErr)
	}
	return nil
}

func (m *CC1200Modem) command(cmd []byte) error {
	if len(cmd) < 2 {
		return fmt.Errorf("command cmd length < 2")
	}
	cmd[1] = byte(len(cmd))
	var err error
	log.Printf("[DEBUG] modem command(): % 2x", cmd)
	_, err = m.modem.Write(cmd)
	if err != nil {
		return fmt.Errorf("command: %w", err)
	}
	return nil
}
func (m *CC1200Modem) commandWithResponse(cmd []byte) ([]byte, error) {
	log.Printf("[DEBUG] commandWithResponse() sending: % 2x", cmd)
	m.clearResponseBuf()
	err := m.command(cmd)
	if err != nil {
		return nil, err
	}
	resp, err := m.commandResponse()
	if err != nil {
		return nil, fmt.Errorf("commandWithResponse(): %w", err)
	}
	log.Printf("[DEBUG] commandWithResponse() received: % 2x", resp)
	return resp, nil
}

func (m *CC1200Modem) clearResponseBuf() {
	for {
		select {
		case b := <-m.cmdSource:
			log.Printf("[DEBUG] discarding: %2x", b)
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
	log.Printf("[DEBUG] commandResponse(): % x", buf)
	return buf, nil
}
