package modem

import "testing"

// A Raspberry Pi with HDMI audio (card 0) and the SX1255 HAT through the
// genericstereoaudiocodec overlay (card 1): capture is device 0, playback 1.
var piWithHDMI = []alsaCandidate{
	{Card: 0, Device: 0, CardTitle: "vc4-hdmi", Title: "MAI PCM i2s-hifi-0", Path: "/dev/snd/pcmC0D0p", Play: true},
	{Card: 1, Device: 0, CardTitle: "GenericStereoAudioCodec", Title: "3f203000.i2s-dir-hifi dir-hifi-0", Path: "/dev/snd/pcmC1D0c", Record: true},
	{Card: 1, Device: 1, CardTitle: "GenericStereoAudioCodec", Title: "3f203000.i2s-dit-hifi dit-hifi-1", Path: "/dev/snd/pcmC1D1p", Play: true},
}

func TestSelectALSADevice(t *testing.T) {
	tests := []struct {
		name       string
		candidates []alsaCandidate
		playback   bool
		hint       string
		preferCard int
		want       int // index, -1 for an error
	}{
		{"capture skips HDMI", piWithHDMI, false, "", -1, 1},
		{"playback follows the capture card", piWithHDMI, true, "", 1, 2},
		{"playback without capture card still skips HDMI", piWithHDMI, true, "", -1, 2},
		{"hint by path", piWithHDMI, true, "/dev/snd/pcmC1D1p", 1, 2},
		{"hint by name", piWithHDMI, false, "3f203000.i2s-dir-hifi dir-hifi-0", -1, 1},
		{"hint hw:CARD,DEVICE", piWithHDMI, true, "hw:1,1", -1, 2},
		{"hint hw:CARD means device 0", piWithHDMI, false, "hw:1", -1, 1},
		{"explicit hint may choose HDMI", piWithHDMI, true, "hw:0,0", 1, 0},
		{"hint for the wrong direction fails", piWithHDMI, true, "hw:1,0", -1, -1},
		{"unknown hint fails", piWithHDMI, false, "hw:5,0", -1, -1},
		{"plughw is not accepted", piWithHDMI, false, "plughw:1,0", -1, -1},
		{"only HDMI is still used for playback", piWithHDMI[:1], true, "", -1, 0},
		{"no capture device", piWithHDMI[:1], false, "", -1, -1},
		{"no devices at all", nil, true, "", -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectALSADevice(tt.candidates, tt.playback, tt.hint, tt.preferCard)
			if tt.want == -1 {
				if err == nil {
					t.Errorf("got device %d, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got device %d (%s), want %d (%s)", got, tt.candidates[got].Path, tt.want, tt.candidates[tt.want].Path)
			}
		})
	}
}

func TestCardFromPath(t *testing.T) {
	tests := map[string]int{
		"/dev/snd/pcmC1D0c":  1,
		"/dev/snd/pcmC12D3p": 12,
		"/dev/snd/controlC0": -1,
		"":                   -1,
	}
	for path, want := range tests {
		if got := cardFromPath(path); got != want {
			t.Errorf("cardFromPath(%q) = %d, want %d", path, got, want)
		}
	}
}
