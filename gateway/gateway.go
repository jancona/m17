package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/hashicorp/logutils"
	"github.com/jancona/m17text/m17"
)

const (
	gatewaySrcCall = "ABCDE" // Invalid callsign used for messages from the gateway to the reflector
)

var (
	isDebugArg *bool   = flag.Bool("debug", false, "Emit debug log messages")
	inArg      *string = flag.String("in", "", "M17 input (default stdin)")
	outArg     *string = flag.String("out", "", "M17 output (default stdout)")
	logDestArg *string = flag.String("log", "", "Device/file for log (default stderr)")
	serverArg  *string = flag.String("server", "", "Relay/reflector server")
	portArg    *uint   = flag.Uint("port", 17000, "Port the relay/reflector listens on")
	moduleArg  *string = flag.String("module", "T", "Module to connect to")
	helpArg    *bool   = flag.Bool("h", false, "Print arguments")
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

	g.relay, err = m17.NewM17Relay(serverArg, portArg, moduleArg, gatewaySrcCall, g.FromRelay)
	if err != nil {
		return nil, fmt.Errorf("error creating relay: %v", err)
	}
	err = g.relay.Connect()
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s:%d %s: %v", serverArg, portArg, moduleArg, err)
	}

	return &g, nil
}

func (g Gateway) FromRelay(buf []byte) error {
	log.Printf("[DEBUG] received packet from relay: %x", buf)
	// A packet is an LSF + type code 0x05 for SMS + data up to 823 bytes
	// dst := m17.DecodeCallsign(buf[4:10])
	// src := m17.DecodeCallsign(buf[10:16])
	// typ := buf[16]
	// data := buf[17:]
	// lsf := m17.LSF{
	// 	// A packet is an LSF + type code 0x05 for SMS + data up to 823 bytes
	// 	Dst: [6]uint8([]byte(m17.DecodeCallsign(buf[4:10]))),
	// 	Src: [6]uint8([]byte(m17.DecodeCallsign(buf[10:16]))),
	// }

	// // encode packet and send to g.out
	// return m17.SendPacket(lsf, buf[16:], g.out)
	return nil
}

func (g *Gateway) FromClient(lsf []byte, buf []byte) error {
	log.Printf("[DEBUG] received packet from client: %x", buf)
	l := len(buf)
	t := buf[0]                  // type
	text := string(buf[1 : l-3]) // skip terminating null and CRC
	var crc uint16
	b := bytes.NewReader(buf[l-2:])
	err := binary.Read(b, binary.LittleEndian, &crc)
	if err != nil {
		log.Printf("[INFO] binary.Read failed: %v", err)
	}
	log.Printf("[DEBUG] length: %d, crc: %x, CRC ok: %v, type %02X: %s", l, crc, m17.CRC(buf), t, text)
	if m17.CRC(buf) {
		g.relay.SendMessage(lsf[0:6], text)
	}
	return nil
}

// const m17PacketSize = 33 * 25
var (
	syncd bool
	//look-back buffer for finding syncwords
	last []float32 = make([]float32, 8)
	//Euclidean distance for finding syncwords in the symbol stream
	dist float32
	//raw frame symbols
	pld      = make([]float32, m17.SymbolsPerPayload)
	softBit  = make([]uint16, 2*m17.SymbolsPerPayload) //raw frame soft bits
	dSoftBit = make([]uint16, 2*m17.SymbolsPerPayload) //deinterleaved soft bits

	lsf        = make([]uint8, 30+1)  //complete LSF (one byte extra needed for the Viterbi decoder)
	frameData  = make([]uint8, 26+1)  //decoded frame data, 206 bits, plus 4 flushing bits
	packetData = make([]uint8, 33*25) //whole packet data

	// uint8_t syncd=0;                    //syncword found?
	fl     bool  //Frame=0 of LSF=1
	lastFN int8  //last received frame number (-1 when idle)
	pushed uint8 //counter for pushed symbols

	skipPayloadCRCCheck = false //skip payload CRC check
	// uint8_t callsigns=0;                //decode callsigns?
	// uint8_t show_viterbi=0;             //show Viterbi errors?
	// uint8_t text_only=0;                //display text only (for text message mode)
)

func (g *Gateway) Run() {
	for !g.done {
		var sample float32
		err := binary.Read(g.in, binary.LittleEndian, &sample)
		if err == io.EOF {
			g.done = true
			break
		} else if err != nil {
			log.Printf("binary.Read failed: %v", err)
		}

		const distThresh = 2.0 //distance threshold for the L2 metric (for syncword detection)

		if !syncd {
			//push new symbol
			for i := 0; i < 7; i++ {
				last[i] = last[i+1]
			}

			last[7] = sample

			//calculate euclidean norm
			dist = m17.EuclNorm(last, m17.PktSyncSymbols)
			// log.Printf("[DEBUG] sample: %f, pkt_sync dist: %f", sample, dist)

			//fprintf(stderr, "pkt_sync dist: %3.5f\n", dist);
			if dist < distThresh { //frame syncword detected
				// log.Printf("[DEBUG] pkt_sync dist: %f", dist)
				syncd = true
				pushed = 0
				fl = false
			} else {
				//calculate euclidean norm again, this time against LSF syncword
				dist = m17.EuclNorm(last, m17.LsfSyncSymbols)
				// log.Printf("[DEBUG] sample: %f, lsf_sync dist: %f", sample, dist)

				//fprintf(stderr, "lsf_sync dist: %3.5f\n", dist);
				if dist < distThresh { //LSF syncword
					// log.Printf("[DEBUG] lsf_sync dist: %f", dist)
					syncd = true
					pushed = 0
					lastFN = -1
					packetData = make([]uint8, 33*25)
					fl = true
				}
			}
		} else {
			pld[pushed] = sample
			pushed++
			if pushed == m17.SymbolsPerPayload { //frame acquired
				//get current time
				// now := time.Now()
				// struct tm* tm_now = localtime(&now);

				for i := 0; i < m17.SymbolsPerPayload; i++ {

					//bit 0
					if pld[i] >= float32(m17.SymbolList[3]) {
						softBit[i*2+1] = 0xFFFF
					} else if pld[i] >= float32(m17.SymbolList[2]) {
						softBit[i*2+1] = uint16(-float32(0xFFFF)/float32((m17.SymbolList[3]-m17.SymbolList[2])*m17.SymbolList[2]) + pld[i]*float32(0xFFFF)/float32((m17.SymbolList[3]-m17.SymbolList[2])))
					} else if pld[i] >= float32(m17.SymbolList[1]) {
						softBit[i*2+1] = 0x0000
					} else if pld[i] >= float32(m17.SymbolList[0]) {
						softBit[i*2+1] = uint16(float32(0xFFFF)/float32((m17.SymbolList[1]-m17.SymbolList[0])*m17.SymbolList[1]) - pld[i]*float32(0xFFFF)/float32((m17.SymbolList[1]-m17.SymbolList[0])))
					} else {
						softBit[i*2+1] = 0xFFFF
					}

					//bit 1
					if pld[i] >= float32(m17.SymbolList[2]) {
						softBit[i*2] = 0x0000
					} else if pld[i] >= float32(m17.SymbolList[1]) {
						softBit[i*2] = 0x7FFF - uint16(pld[i]*float32(0xFFFF)/float32(m17.SymbolList[2]-m17.SymbolList[1]))
					} else {
						softBit[i*2] = 0xFFFF
					}
				}

				//derandomize
				for i := 0; i < m17.SymbolsPerPayload*2; i++ {
					if (m17.RandSeq[i/8]>>(7-(i%8)))&1 != 0 { //soft XOR. flip soft bit if "1"
						softBit[i] = 0xFFFF - softBit[i]
					}
				}

				//deinterleave
				for i := 0; i < m17.SymbolsPerPayload*2; i++ {
					dSoftBit[i] = softBit[m17.IntrlSeq[i]]
				}

				//if it is a frame
				if !fl {
					m := ""
					for i := 0; i < len(dSoftBit); i++ {
						m += fmt.Sprintf("%04X", dSoftBit[i])
					}
					// log.Printf("[DEBUG] len(dSoftBit): %d, dSoftBit: %s", len(dSoftBit), m)
					//decode
					_, err := m17.ViterbiDecodePunctured(frameData, dSoftBit, m17.PuncturePattern3)
					if err != nil {
						log.Printf("Error calling ViterbiDecodePunctured: %v", err)
					}

					//dump FN
					rx_fn := (frameData[26] >> 2) & 0x1F
					rx_last := frameData[26] >> 7

					//fprintf(stderr, "FN%d, (%d)\n", rx_fn, rx_last);

					//             if(show_viterbi)
					//             {
					//                 fprintf(stderr, "   \033[93mFrame %d Viterbi error:\033[39m %1.1f\n", rx_last?lastFN+1:rx_fn, (float)e/0xFFFF);
					//             }
					// log.Printf("[DEBUG] FN%d, (%d)", rx_fn, rx_last)
					// if rx_last != 0 {
					// 	log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", lastFN+1, float32(e)/float32(0xFFFF))
					// } else {
					// 	log.Printf("[DEBUG] Frame %d Viterbi error: %1.1f", rx_fn, float32(e)/float32(0xFFFF))
					// }
					// log.Printf("[DEBUG] frameData: %x %s", frameData[1:26], frameData[1:26])

					//copy data - might require some fixing
					if rx_fn <= 31 && rx_fn == uint8(lastFN)+1 && rx_last == 0 {
						// memcpy(&packetData[rx_fn*25], &frameData[1], 25)
						copy(packetData[rx_fn*25:(rx_fn+1)*25], frameData[1:26])
						lastFN++
					} else if rx_last != 0 {
						// memcpy(&packetData[(lastFN+1)*25], &frameData[1], rx_fn)
						copy(packetData[(lastFN+1)*25:uint8(lastFN+1)*25+rx_fn], frameData[1:rx_fn+1])
						packetData = packetData[:uint8(lastFN+1)*25+rx_fn]
						// fprintf(stderr, " \033[93mContent\033[39m\n");

						g.FromClient(lsf, packetData)
						//dump data
						if packetData[0] == 0x05 { //if a text message
							// fprintf(stderr, " ├ \033[93mType:\033[39m SMS\n");

							if skipPayloadCRCCheck {
								// fprintf(stderr, " └ \033[93mText:\033[39m %s\n", &packetData[1]);
							} else {
								// uint16_t p_len=strlen((const char*)packetData);

								// fprintf(stderr, " ├ \033[93mText:\033[39m %s\n", &packetData[1]);

								//CRC
								// fprintf(stderr, " └ \033[93mPayload CRC:\033[39m");
								// if(CRC_M17(packetData, p_len+3)) //3: terminating null plus a 2-byte CRC
								//     fprintf(stderr, " \033[91mmismatch\033[39m\n");
								// else
								//     fprintf(stderr, " \033[92mmatch\033[39m\n");
							}
						} else {
							// if(!text_only)					                    {
							//     fprintf(stderr, " └ \033[93mPayload:\033[39m ");
							//     for(uint16_t i=0; i<(lastFN+1)*25+rx_fn; i++)
							//     {
							//         if(i!=0 && (i%25)==0)
							//             fprintf(stderr, "\n     ");
							//         fprintf(stderr, " %02X", packetData[i]);
							//     }
							//     fprintf(stderr, "\n");
							// }
						}
					}

				} else { //if it is LSF
					// fprintf(stderr, "\033[96m[%02d:%02d:%02d] \033[92mPacket received\033[39m\n", tm_now->tm_hour, tm_now->tm_min, tm_now->tm_sec);
					// m := ""
					// for i := 0; i < 61; i++ {
					// 	m += fmt.Sprintf("%04X", dSoftBit[i])
					// }
					// log.Printf("[DEBUG] dSoftBit: %s", m)
					//decode
					e, err := m17.ViterbiDecodePunctured(lsf, dSoftBit, m17.PuncturePattern1)
					if err != nil {
						log.Printf("Error calling ViterbiDecodePunctured: %v", err)
					}

					//shift the buffer 1 position left - get rid of the encoded flushing bits
					// copy(lsf, lsf[1:])
					lsf = lsf[1:]
					log.Printf("[DEBUG] lsf: %x", lsf)
					dst, err := m17.DecodeCallsign(lsf[0:6])
					if err != nil {
						log.Printf("[ERROR] Bad dst callsign: %v", err)
					}
					src, err := m17.DecodeCallsign(lsf[6:12])
					if err != nil {
						log.Printf("[ERROR] Bad src callsign: %v", err)
					}
					log.Printf("[DEBUG] dest: %s, src: %s", dst, src)

					// if(!text_only)
					// {
					//     //dump data
					//     if(callsigns)
					//     {
					//         uint8_t d_dst[12], d_src[12]; //decoded strings

					//         decode_callsign_bytes(d_dst, &lsf[0]);
					//         decode_callsign_bytes(d_src, &lsf[6]);

					//         //DST
					//         fprintf(stderr, " ├ \033[93mDestination:\033[39m %s\n", d_dst);

					//         //SRC
					//         fprintf(stderr, " ├ \033[93mSource:\033[39m %s\n", d_src);
					//     }
					//     else
					//     {
					//         //DST
					//         fprintf(stderr, " ├ \033[93mDestination:\033[39m ");
					//         for(uint8_t i=0; i<6; i++)
					//             fprintf(stderr, "%02X", lsf[i]);
					//         fprintf(stderr, "\n");

					//         //SRC
					//         fprintf(stderr, " ├ \033[93mSource:\033[39m ");
					//         for(uint8_t i=0; i<6; i++)
					//             fprintf(stderr, "%02X", lsf[6+i]);
					//         fprintf(stderr, "\n");
					//     }

					//     //TYPE
					//     fprintf(stderr, " ├ \033[93mType:\033[39m ");
					//     for(uint8_t i=0; i<2; i++)
					//         fprintf(stderr, "%02X", lsf[12+i]);
					//     fprintf(stderr, "\n");

					//     //META
					//     fprintf(stderr, " ├ \033[93mMeta:\033[39m ");
					//     for(uint8_t i=0; i<14; i++)
					//         fprintf(stderr, "%02X", lsf[14+i]);
					//     fprintf(stderr, "\n");

					//     //Viterbi decoder errors
					//     if(show_viterbi)
					//     {
					//         fprintf(stderr, " ├ \033[93mLSF Viterbi error:\033[39m %1.1f\n", (float)e/0xFFFF);
					//     }
					log.Printf("[DEBUG] LSF Viterbi error: %1.1f", float32(e)/float32(0xFFFF))

					//     //CRC
					//     fprintf(stderr, " └ \033[93mLSF CRC:\033[39m");
					//     if(CRC_M17(lsf, 30))
					//         fprintf(stderr, " \033[91mmismatch\033[39m\n");
					//     else
					//         fprintf(stderr, " \033[92mmatch\033[39m\n");
					// }
				}

				//job done
				syncd = false
				pushed = 0

				for i := 0; i < 8; i++ {
					last[i] = 0.0
				}
			}

		}

	}
	g.Close()
	// // Run until we're terminated then clean up
	// log.Print("[DEBUG] client: Waiting for close signal")
	// // wait for a close signal then clean up
	// signalChan := make(chan os.Signal, 1)
	// cleanupDone := make(chan struct{})
	// signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	// go func() {
	// 	<-signalChan
	// 	log.Print("client: Received an interrupt, stopping...")
	// 	// Cleanup goes here
	// 	g.Close()
	// 	close(cleanupDone)
	// }()
	// <-cleanupDone
}
func (g *Gateway) Close() {
	g.done = true
	g.relay.Close()
	if g.in != os.Stdin {
		g.in.Close()
	}
	if g.out != os.Stdout {
		g.out.Close()
	}
}
