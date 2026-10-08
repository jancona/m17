package modem

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/yobert/alsa"
)

// alsaCandidate describes one ALSA PCM device for device selection. It holds
// only plain data, so the selection rules can be tested without hardware.
type alsaCandidate struct {
	Card      int    // card number, as in hw:CARD,DEVICE
	Device    int    // device number on the card
	CardTitle string // card name, e.g. "vc4-hdmi" or "GenericStereoAudioCodec"
	Title     string // PCM device name, e.g. "MAI PCM i2s-hifi-0"
	Path      string // device node, e.g. /dev/snd/pcmC1D0c
	Play      bool
	Record    bool
}

var hwDeviceRE = regexp.MustCompile(`^hw:(\d+)(?:,(\d+))?$`)

// isHDMI reports whether a device is an HDMI audio output. On a Raspberry Pi
// the HDMI PCM is named "MAI PCM i2s-hifi-0", so it matches a plain "i2s"
// search and must be excluded explicitly.
func (c alsaCandidate) isHDMI() bool {
	return strings.Contains(strings.ToLower(c.CardTitle), "hdmi") ||
		strings.HasPrefix(c.Title, "MAI PCM")
}

// matchesHint reports whether the device is the one named in the
// configuration: its device node path, its exact PCM name, or hw:CARD[,DEVICE].
func (c alsaCandidate) matchesHint(hint string) bool {
	if c.Path == hint || c.Title == hint {
		return true
	}
	if m := hwDeviceRE.FindStringSubmatch(hint); m != nil {
		card, _ := strconv.Atoi(m[1])
		dev := 0
		if m[2] != "" {
			dev, _ = strconv.Atoi(m[2])
		}
		return c.Card == card && c.Device == dev
	}
	return false
}

// selectALSADevice picks a capture (playback=false) or playback device and
// returns its index in candidates.
//
// A configured hint always wins and must match. Otherwise, in order:
//  1. a device on preferCard (the SX1255 card, found from the capture device;
//     -1 if not known yet), so playback follows capture onto the same card;
//  2. a device whose name contains "i2s", that is not HDMI;
//  3. any device that is not HDMI;
//  4. any device at all.
func selectALSADevice(candidates []alsaCandidate, playback bool, hint string, preferCard int) (int, error) {
	kind := "capture"
	if playback {
		kind = "playback"
	}
	usable := func(c alsaCandidate) bool {
		if playback {
			return c.Play
		}
		return c.Record
	}

	if hint != "" {
		for i, c := range candidates {
			if usable(c) && c.matchesHint(hint) {
				return i, nil
			}
		}
		return -1, fmt.Errorf("ALSA: no %s device matches %q; use a device name, a path like /dev/snd/pcmC1D0c, or hw:CARD,DEVICE", kind, hint)
	}

	rules := []func(c alsaCandidate) bool{
		func(c alsaCandidate) bool { return preferCard >= 0 && c.Card == preferCard },
		func(c alsaCandidate) bool { return !c.isHDMI() && strings.Contains(strings.ToLower(c.Title), "i2s") },
		func(c alsaCandidate) bool { return !c.isHDMI() },
		func(c alsaCandidate) bool { return true },
	}
	for _, rule := range rules {
		for i, c := range candidates {
			if usable(c) && rule(c) {
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("ALSA: no %s device found", kind)
}

// pickALSADevice lists the PCM devices on the given cards and selects one
// with selectALSADevice.
func pickALSADevice(cards []*alsa.Card, playback bool, hint string, preferCard int) (*alsa.Device, error) {
	var candidates []alsaCandidate
	var devices []*alsa.Device
	for _, card := range cards {
		devs, err := card.Devices()
		if err != nil {
			log.Printf("[DEBUG] ALSA: error listing devices on card %s: %v", card.Title, err)
			continue
		}
		for _, dev := range devs {
			if dev.Type != alsa.PCM {
				continue
			}
			c := alsaCandidate{
				Card:      card.Number,
				Device:    dev.Number,
				CardTitle: card.Title,
				Title:     dev.Title,
				Path:      dev.Path,
				Play:      dev.Play,
				Record:    dev.Record,
			}
			log.Printf("[DEBUG] ALSA: found device hw:%d,%d %q on card %q (%s)", c.Card, c.Device, c.Title, c.CardTitle, c.Path)
			candidates = append(candidates, c)
			devices = append(devices, dev)
		}
	}
	i, err := selectALSADevice(candidates, playback, hint, preferCard)
	if err != nil {
		return nil, err
	}
	c := candidates[i]
	log.Printf("[INFO] ALSA: using hw:%d,%d %q (%s)", c.Card, c.Device, c.Title, c.Path)
	return devices[i], nil
}

// cardFromPath returns the card number from a device node path such as
// /dev/snd/pcmC1D0c, or -1.
func cardFromPath(path string) int {
	var card, dev int
	var dir byte
	name := path[strings.LastIndex(path, "/")+1:]
	if n, _ := fmt.Sscanf(name, "pcmC%dD%d%c", &card, &dev, &dir); n < 2 {
		return -1
	}
	return card
}
