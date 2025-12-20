package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jancona/m17"
)

var speakWords = strings.Split("space a b c d e f g h i j k l m n o p q r s t u v w x y z 0 1 2 3 4 5 6 7 8 9 dash slash dot alpha bravo charlie delta echo foxtrot golf hotel india juliette kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey x-ray yankee zulu m17", " ")
var alphaIndex = slices.Index(speakWords, "alpha")

var codec2silence3200 = []byte{0x01, 0x00, 0x09, 0x43, 0x9C, 0xE4, 0x21, 0x08, 0x01, 0x00, 0x09, 0x43, 0x9C, 0xE4, 0x21, 0x08}

func (g *Gateway) loadAudioClips(audioDir string, callsign string) error {
	g.audioClips = map[string][]byte{}
	files, err := os.ReadDir(audioDir)
	if err != nil {
		return fmt.Errorf("unable to read audio clips from %s: %w", audioDir, err)
	}
	for _, f := range files {
		if f.Type().IsRegular() && strings.HasSuffix(f.Name(), ".dat") {
			name := f.Name()[:len(f.Name())-4]
			fullname := filepath.Join(audioDir, f.Name())
			contents, err := os.ReadFile(fullname)
			if err != nil {
				return fmt.Errorf("unable to read audio file %s: %w", fullname, err)
			}
			if name == "speak" {
				// speak.dat contains multiple words
				log.Printf("[DEBUG] len(speak.dat): %d", len(contents))
				fullname := filepath.Join(audioDir, "speak.index")
				data, err := os.ReadFile(fullname)
				if err != nil {
					return fmt.Errorf("unable to read index file %s: %w", fullname, err)
				}
				for line := range strings.SplitSeq(string(data), "\n") {
					if len(line) == 0 || line[0] == '#' {
						continue
					}
					ifs := strings.Fields(line)
					idx, err := strconv.Atoi(ifs[0])
					if err != nil {
						return fmt.Errorf("unable reading index file field %s: %w", ifs[0], err)
					}
					start, err := strconv.Atoi(ifs[1])
					if err != nil {
						return fmt.Errorf("unable reading index file field %s: %w", ifs[1], err)
					}
					stop, err := strconv.Atoi(ifs[2])
					if err != nil {
						return fmt.Errorf("unable reading index file field %s: %w", ifs[2], err)
					}
					// length, err = strconv.Atoi(ifs[3])
					// if err != nil {
					// 	return  fmt.Errorf("unable reading index file field %s: %w", ifs[3], err)
					// }
					// log.Printf("[DEBUG] speak.list line: %s, start: %d, stop: %d", line, start, stop)
					log.Printf("[DEBUG] Adding audioClips[%s]", speakWords[idx])
					g.audioClips[speakWords[idx]] = padClip(contents[start*8 : (stop+1)*8])
				}
			} else {
				g.audioClips[name] = padClip(contents)
			}
		}
	}
	// synomyms
	g.audioClips[" "] = g.audioClips["space"]
	g.audioClips["-"] = g.audioClips["dash"]
	g.audioClips["/"] = g.audioClips["slash"]
	g.audioClips["."] = g.audioClips["dot"]
	// Add callsign
	contents := g.sayCallsign(callsign)
	g.audioClips["callsign"] = padClip(contents)
	return nil
}

func padClip(contents []byte) []byte {
	l := len(contents) % 16
	if l > 0 {
		// if clip isn't a multiple of 16 bytes (40ms) long, pad it
		contents = append(contents, codec2silence3200[:16-l]...)
	}
	return contents
}

func (g *Gateway) sayCallsign(callsign string) []byte {
	log.Printf("[DEBUG] sayCallsign(%s)", callsign)
	var contents []byte
	first := false
	gap := false
	callsign = strings.ToLower(callsign)
	for i := 0; i < len(callsign); i++ {
		if callsign[i] == ' ' {
			gap = true
			continue
		}
		if !first {
			for range 4 { // 4 frames == 160ms of silence between letters
				contents = append(contents, codec2silence3200...)
			}
		}
		if strings.HasPrefix(callsign[i:], "m17") {
			contents = append(contents, g.audioClips["m17"]...)
			i += 2
		} else {
			letter := string(callsign[i])
			clip, ok := g.audioClips[letter]
			if ok {
				if !gap || callsign[i] < 'a' || callsign[i] > 'z' {
					// before a space or the suffix is not a letter
					contents = append(contents, clip...)
				} else {
					// phonetically
					offset := int(callsign[i] - 'a')
					word := speakWords[alphaIndex+offset]
					contents = append(contents, g.audioClips[word]...)
				}
			}
		}
	}
	return padClip(contents)
}

func (g *Gateway) playMessage(msg ...string) error {
	log.Printf("[DEBUG] playMessage() setState(LocalCommand)")
	defer g.setState(Idle)
	lsf, err := m17.NewLSF(m17.DestinationAll, g.encodedCallsign.Callsign(), m17.LSFTypeStream, m17.LSFDataTypeVoice, 0)
	if err != nil {
		return fmt.Errorf("unable to make LSF: %w", err)
	}
	sid := uint16(rand.Intn(0x10000))
	fn := uint16(0)
	err = g.sendSilence(sid, &fn, lsf, 8*m17.FrameTime)
	if err != nil {
		return err
	}
	first := true
	for _, word := range msg {
		word = strings.ToLower(word)
		log.Printf("[DEBUG] Sending audio clip for '%s'", word)
		clip, ok := g.audioClips[word]
		if !ok {
			clip = g.sayCallsign(word)
			g.audioClips[word] = clip
		}
		if !first {
			err = g.sendSilence(sid, &fn, lsf, 4*m17.FrameTime)
			if err != nil {
				return err
			}
		}
		for i := 0; i < len(clip); i += 16 {
			sd := m17.NewStreamDatagram(sid, fn, &lsf, clip[i:i+16])
			err = g.modem.TransmitVoiceStream(sd)
			if err != nil {
				return fmt.Errorf("unable to send message frame: %w", err)
			}
			fn++
		}
	}
	// Send silent frame followed by EOT
	fn |= 0x8000
	err = g.sendSilence(sid, &fn, lsf, m17.FrameTime)
	log.Printf("[DEBUG] playMessage() setState(Idle)")
	return err
}

func (g *Gateway) sendSilence(sid uint16, fn *uint16, lsf m17.LSF, duration time.Duration) error {
	log.Printf("[DEBUG] Sending %v of silence. sid: %x, fn: %x, lsf: %s", duration, sid, *fn, lsf)
	for elapsed := time.Duration(0); elapsed < duration; elapsed += m17.FrameTime {
		sd := m17.NewStreamDatagram(sid, *fn, &lsf, codec2silence3200)
		err := g.modem.TransmitVoiceStream(sd)
		if err != nil {
			return fmt.Errorf("unable to send silent frame: %w", err)
		}
		*fn++
	}
	return nil
}

func (g *Gateway) echoStreamStart() {
	log.Printf("[DEBUG] echoStreamStart()")
	g.echoStream = make([]m17.StreamDatagram, 0)
}
func (g *Gateway) echoStreamRecord(sd m17.StreamDatagram) {
	// log.Printf("[DEBUG] echoStreamRecord(%v)", sd)
	g.echoStream = append(g.echoStream, sd)
}
func (g *Gateway) echoStreamEnd() error {
	defer g.setState(Idle)
	var err error
	if g.getState() == Echo {
		log.Printf("[DEBUG] echoStreamEnd() %d frames", len(g.echoStream))
		time.Sleep(250 * time.Millisecond)
		sid := uint16(rand.Intn(0x10000))
		for _, sd := range g.echoStream {
			sd.StreamID = sid
			sd.LSF.Dst = sd.LSF.Src
			sd.LSF.Src = g.encodedCallsign
			sd.LSF.CalcCRC()
			// log.Printf("[DEBUG] echoStreamEnd id: %04x, fn: %04x, last: %v, payload: [% 02x]", sd.StreamID, sd.FrameNumber, sd.LastFrame, sd.Payload)
			err = g.modem.TransmitVoiceStream(sd)
			if err != nil {
				log.Printf("[ERROR] Error transmitting voice stream in echoStreamEnd(): %v", err)
				break
			}
		}
	}
	g.echoStream = nil
	log.Printf("[DEBUG] echoStreamEnd() setState(Idle)")
	return err
}
