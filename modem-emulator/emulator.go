package main

import (
	"encoding/binary"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/randomvariable/goterm/term"
)

const IDENT_STR = "CC1200-HAT 420-450 MHz\nFW v1.1 by Wojciech SP5WWP"
const (
	CMD_PING = iota
	//SET
	CMD_SET_RX_FREQ
	CMD_SET_TX_FREQ
	CMD_SET_TX_POWER
	CMD_SET_RESERVED
	CMD_SET_FREQ_CORR
	CMD_SET_AFC
	CMD_SET_TX_START
	CMD_SET_RX
)
const (
	//GET
	CMD_GET_IDENT = iota + 0x80
	CMD_GET_CAPS
	CMD_GET_RX_FREQ
	CMD_GET_TX_FREQ
	CMD_GET_TX_POWER
	CMD_GET_FREQ_CORR
)
const (
	TRX_IDLE = iota
	TRX_TX
	TRX_RX
)

const (
	ERR_OK      = iota //all good
	ERR_TRX_PLL        //TRX PLL lock error
	ERR_TRX_SPI        //TRX SPI comms error
	ERR_RANGE          //value out of range
)

// const (
// 	_IOC_VOID    uintptr = 0x20000000
// 	_IOC_OUT     uintptr = 0x40000000
// 	_IOC_IN      uintptr = 0x80000000
// 	_IOC_IN_OUT  uintptr = _IOC_OUT | _IOC_IN
// 	_IOC_DIRMASK         = _IOC_VOID | _IOC_OUT | _IOC_IN

// 	_IOC_PARAM_SHIFT = 13
// 	_IOC_PARAM_MASK  = (1 << _IOC_PARAM_SHIFT) - 1
// )

func main() {
	pty, err := term.OpenPTY()
	if err != nil {
		log.Fatalf("OpenPTY: %v", err)
	}
	name, err := pty.PTSName()
	if err != nil {
		log.Fatalf("PTSName: %v", err)
	}
	log.Printf("pty name: %s", name)

	attr, err := term.Attr(pty.Slave)
	if err != nil {
		log.Fatalf("term.Attr: %v", err)
	}
	attr.Raw()
	attr.Set(pty.Slave)
	attr, err = term.Attr(pty.Master)
	if err != nil {
		log.Fatalf("term.Attr: %v", err)
	}
	attr.Raw()
	attr.Set(pty.Master)

	// Cleanup the PTY
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		pty.Close()
		os.Exit(1)
	}()

	var devErr uint32 = ERR_OK
	var freq uint32
	var rxFreq uint32 = 433475000
	var txFreq uint32 = 433475000
	var txDbm float64 = 10 //10dBm default
	var txPwr float64 = 3  //3 to 63
	var freqCorrection uint16
	var afc bool
	var trxState byte = TRX_IDLE
	repeater := NewRepeater(10)
	rxbChan := make(chan []byte)
	const rxTickTime = 40 * time.Millisecond
	rxTicker := time.NewTicker(rxTickTime)
	// Stop it until we're in TRX_RX mode
	rxTicker.Stop()
	const txTimeout = 120 * time.Millisecond
	txTimer := time.NewTimer(txTimeout)
	// Stop it until we're in TRX_TX mode
	txTimer.Stop()
	go func() {
		for {
			// Read data from the connection.
			rxb, err := readBuffer(pty.Master, trxState)
			if err != nil {
				if err == io.EOF {
					// client disconnected
					log.Print("Received EOF from client")
					break
				}
				log.Fatalf("failed to read: %v", err)
			}
			log.Printf("trxState: %d, received rxb: % x", trxState, rxb)
			if len(rxb) > 0 {
				rxbChan <- rxb
			}
		}
	}()
	for {
		select {
		case rxb := <-rxbChan:
			if trxState != TRX_TX {
				switch rxb[0] {
				case CMD_PING:
					log.Printf("got CMD_PING")
					resp := append([]byte{}, CMD_PING, 0)
					resp, err = binary.Append(resp, binary.LittleEndian, devErr)
					if err != nil {
						log.Fatalf("failed to append devErr: %v", err)
					}
					resp[1] = byte(len(resp))
					// _, err = pty.Master.Write(append(resp, 10))
					_, err = pty.Master.Write(resp)
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}
				case CMD_SET_RX_FREQ:
					_, err = binary.Decode(rxb[2:rxb[1]], binary.LittleEndian, &freq)
					if err != nil {
						log.Fatalf("failed to decode rxFreq: %v", err)
					}
					if freq > 420e6 && freq < 450e6 {
						rxFreq = freq
						log.Printf("Set RX freq: %d", rxFreq)
						_, err = pty.Master.Write([]byte{CMD_SET_RX_FREQ, 3, ERR_OK})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					} else {
						log.Printf("Bad RX freq: %d", freq)
						_, err = pty.Master.Write([]byte{CMD_SET_RX_FREQ, 3, ERR_RANGE})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					}
				case CMD_SET_TX_FREQ:
					_, err = binary.Decode(rxb[2:rxb[1]], binary.LittleEndian, &freq)
					if err != nil {
						log.Fatalf("failed to decode rxFreq: %v", err)
					}
					if freq > 420e6 && freq < 450e6 {
						txFreq = freq
						log.Printf("Set TX freq: %d", txFreq)
						_, err = pty.Master.Write([]byte{CMD_SET_TX_FREQ, 3, ERR_OK})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					} else {
						log.Printf("Bad TX freq: %d", freq)
						_, err = pty.Master.Write([]byte{CMD_SET_TX_FREQ, 3, ERR_RANGE})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					}
				case CMD_SET_TX_POWER:
					val := float64(rxb[2])
					if val*0.25 >= -16.0 && val*0.25 <= 14.0 { //-16 to 14 dBm (0x03 to 0x3F)
						txDbm = val * 0.25
						txPwr = math.Floor((val*0.25+18.0)*2.0 - 1.0)
						log.Printf("Set TX power txDbm: %f, txPwr: %f", txDbm, txPwr)
						_, err = pty.Master.Write([]byte{CMD_SET_TX_POWER, 3, ERR_OK})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					} else {
						log.Printf("Bad TX power")
						_, err = pty.Master.Write([]byte{CMD_SET_TX_POWER, 3, ERR_RANGE})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					}
				case CMD_SET_FREQ_CORR:
					_, err = binary.Decode(rxb[2:rxb[1]], binary.LittleEndian, &freqCorrection)
					log.Printf("Set frequancy correction: %d", freqCorrection)
					if err != nil {
						log.Fatalf("failed to decode freqCorrection: %v", err)
					}
					_, err = pty.Master.Write([]byte{CMD_SET_FREQ_CORR, 3, ERR_OK})
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}
				case CMD_SET_AFC:
					afc = rxb[2] != 0
					log.Printf("Set AFC: %v", afc)
					_, err = pty.Master.Write([]byte{CMD_SET_AFC, 3, ERR_OK})
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}
				case CMD_SET_TX_START:
					if trxState != TRX_TX && devErr == ERR_OK {
						log.Printf("Got TX start")
						trxState = TRX_TX
						rxTicker.Stop()
						txTimer.Reset(txTimeout)
						// log.Print("txTimer.Reset(txTimeout)")
					} else {
						log.Printf("TX start error")
						resp := append([]byte{}, CMD_SET_TX_START, 6)
						resp, err = binary.Append(resp, binary.LittleEndian, devErr)
						if err != nil {
							log.Fatalf("failed to append devErr: %v", err)
						}
						resp[1] = byte(len(resp))
						_, err = pty.Master.Write(resp)
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					}
				case CMD_SET_RX:
					if rxb[2] != 0 { //start
						if trxState != TRX_RX && devErr == ERR_OK {
							log.Printf("Got RX start")
							trxState = TRX_RX
							rxTicker.Reset(rxTickTime)
						} else {
							log.Printf("RX start error")
							resp := append([]byte{}, CMD_SET_TX_START, 6)
							resp, err = binary.Append(resp, binary.LittleEndian, devErr)
							if err != nil {
								log.Fatalf("failed to append devErr: %v", err)
							}
							resp[1] = byte(len(resp))
							_, err = pty.Master.Write(resp)
							if err != nil {
								log.Fatalf("failed to write resp: %v", err)
							}
						}
					} else { //stop
						trxState = TRX_IDLE
						rxTicker.Stop()
						log.Printf("RX stop")
						_, err = pty.Master.Write([]byte{CMD_SET_FREQ_CORR, 3, ERR_OK})
						if err != nil {
							log.Fatalf("failed to write resp: %v", err)
						}
					}

				case CMD_GET_IDENT:
					log.Print("got CMD_GET_IDENT")
					//reply with RRU's IDENT string
					resp := []byte{CMD_GET_IDENT, byte(len(IDENT_STR) + 2)}
					resp = append(resp, []byte(IDENT_STR)...)
					resp[1] = byte(len(resp))
					_, err = pty.Master.Write(resp)
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}

				case CMD_GET_CAPS:
					log.Print("got CMD_GET_IDENT")
					//so far the CC1200-HAT can do FM only, half-duplex
					_, err = pty.Master.Write([]byte{CMD_GET_CAPS, 3, 0x2})
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}
				case CMD_GET_RX_FREQ:
					log.Print("got CMD_GET_RX_FREQ")
					resp := []byte{CMD_GET_RX_FREQ, 6}
					resp, err = binary.Append(resp, binary.LittleEndian, rxFreq)
					if err != nil {
						log.Fatalf("failed to append rxFreq: %v", err)
					}
					resp[1] = byte(len(resp))
					_, err = pty.Master.Write(resp)
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}
				case CMD_GET_TX_FREQ:
					log.Print("got CMD_GET_TX_FREQ")
					resp := []byte{CMD_GET_TX_FREQ, 0}
					resp, err = binary.Append(resp, binary.LittleEndian, txFreq)
					if err != nil {
						log.Fatalf("failed to append txFreq: %v", err)
					}
					resp[1] = byte(len(resp))
					_, err = pty.Master.Write(resp)
					if err != nil {
						log.Fatalf("failed to write resp: %v", err)
					}
				}
			} else { // trxState == TRX_TX
				//pass baseband samples to the buffer
				// log.Printf("Received %d TX bytes: % x", len(rxb), rxb)
				repeater.TXSamples() <- rxb
				txTimer.Reset(txTimeout)
				log.Printf("trxState: %d, txTimer.Reset(txTimeout)", trxState)
			}
		case <-rxTicker.C:
			samples := repeater.Next()
			_, err = pty.Master.Write(samples[:])
			if err != nil {
				log.Fatalf("failed to write receive samples: %v", err)
			}
		case <-txTimer.C:
			log.Printf("trxState: %d. TX timed out", trxState)
			trxState = TRX_IDLE
			txTimer.Stop()
		}
	}
}

func readBuffer(c *os.File, trxState byte) ([]byte, error) {
	rxb := make([]byte, 1000)
	var bufLen int
	var err error
	var n int
	if trxState != TRX_TX {
		bufLen = 2 // minimum command length
		// First char is command
		// Second is bufLen
		// Then read until bufLen
		for i := 0; i < bufLen; i += n {
			n, err = c.Read(rxb[i:bufLen])
			if err != nil {
				return nil, err
			}
			if i < 2 && i+n >= 2 {
				bufLen = int(rxb[1])
			}
		}
	} else {
		bufLen, err = c.Read(rxb)
		if err != nil {
			return nil, err
		}
	}
	// log.Printf("read: %#v", rxb[:cmdLen])
	return rxb[:bufLen], nil
}

const (
	SamplesPer40MS   = 960
	SamplesPerSecond = SamplesPer40MS * 1000 / 40
)

type Repeater struct {
	source      chan []byte // contains incoming []byte
	bytesBuffer chan byte   // buffer of bytes
	done        bool
}

// Create a new Repeater that buffers bufSize seconds of samples
func NewRepeater(bufSize int) Repeater {
	r := Repeater{
		source:      make(chan []byte), // Allow a little buffering here?
		bytesBuffer: make(chan byte, bufSize*SamplesPerSecond),
	}
	go r.handle()
	return r
}

func (r *Repeater) handle() {
	for !r.done {
		bytes, ok := <-r.source
		if !ok {
			r.done = true
			continue
		}
		// log.Printf("Handle bytes: % x", bytes)
		for _, b := range bytes {
			r.bytesBuffer <- b
		}
	}
}

// the channel used to supply samples
func (r *Repeater) TXSamples() chan []byte {
	return r.source
}

// Return 40ms of samples, filling with noise if we don't have enough
func (r *Repeater) Next() *[SamplesPer40MS]byte {
	ret := [SamplesPer40MS]byte{}
	var txBytes, randBytes int
	for i := range len(ret) {
		select {
		case ret[i] = <-r.bytesBuffer:
			// log.Printf("got real byte: %x", ret[i])
			txBytes++
		default:
			ret[i] = rand.N(byte(255))
			// log.Printf("got random byte: %x", ret[i])
			randBytes++
		}
	}
	if txBytes > 0 {
		log.Printf("Returning %d TX bytes, %d random bytes: % x", txBytes, randBytes, ret)
	}
	return &ret
}
