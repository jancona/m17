# m17-gateway configuration reference

m17-gateway reads one ini file, given with `-config`. The default is `./gateway.ini`; the packages install `/etc/m17-gateway.ini`. [`m17-gateway.ini.sample`](../cmd/m17-gateway/packaging/m17-gateway.ini.sample) is a starting point.

This page lists every key the gateway reads, what it does, which modems use it, and what values it accepts. It describes m17-gateway as of the release after 0.9.6.

## How values are read

- **Required** keys must be present and valid. If one isn't, the gateway logs `Bad configuration: …`, naming each bad key, and exits. A service manager will then keep restarting it, so check the log.
- **Optional** keys have a default. A key that is missing, empty or unparseable silently takes its default; no error is logged. So a typo such as `TXDelay=1OO` runs with 100 without complaint. A number outside a key's range is an error, though, where this page says so.
- **Booleans** accept `true`/`false`, `yes`/`no`, `on`/`off` (all lower case, capitalized or upper case), `1`/`0`, `t`/`f` and `y`/`n`.
- **Durations** are Go durations: `500ms`, `1s`, `1.5s`.
- **Frequencies** are in hertz: `439000000`, not `439.0`.
- Key and section names are case-sensitive.
- Keys for other modems are ignored, so one file can hold settings for every modem type, as the sample does.

## Modem types at a glance

| `[Modem] Type` | Hardware | Connected by |
|---|---|---|
| `cc1200` (default) | CC1200 HAT | UART, plus two GPIOs for reset and boot mode |
| `cc1200v2` | Same as `cc1200`; an alias | |
| `sx1255` | SX1255 HAT | SPI for control, I2S (ALSA) for samples, one GPIO for reset |
| `mmdvm` | MMDVM_HS hotspot boards, MMDVM repeater boards | UART |
| `dummy` | None: symbols on stdin and stdout, for testing | |

Summary of which keys each modem uses (details in the sections below):

| Key | cc1200 | sx1255 | mmdvm |
|---|---|---|---|
| `[Radio] RXFrequency`, `TXFrequency` | ✓ (420–450 MHz) | ✓ (400–510 MHz) | ✓ (firmware decides) |
| `[Radio] Power` | ✓ | not needed | not needed |
| `[Radio] AFC` | ✓ | not needed | not needed |
| `[Radio] FrequencyCorr` | ✓ (firmware units) | ✓ (Hz) | ✓ (Hz) |
| `[Radio] Duplex` | must be `false` | ✓ | ✓ |
| `[Modem] Port`, `Speed` | ✓ | | ✓ |
| `[Modem] NRSTPin`, `Boot0Pin`, `ZMQPort` | ✓ | | |
| `[Modem] Protocol`, `RXInvert` … `TXHang`, `RFLevel` | | | ✓ |
| `[Modem] SPIDevice` … `RawIQSeconds` | | ✓ | |

## [General]

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `Callsign` | yes | | A valid amateur callsign, at most 9 characters, optionally with a prefix (`EA/N1ADJ`) or a suffix after a space, `-`, `/` or `.` (`N1ADJ G`, `N1ADJ-1`) | The hotspot's callsign. It's converted to upper case, and `N1ADJ G` is padded to `N1ADJ    G` (the letter in the ninth position), as reflectors expect. The gateway links to reflectors with it and uses it as the source of what it transmits itself: announcements, echo playback and repeats in duplex. An empty or missing value is a configuration error. |
| `DashboardLog` | no | empty: no dashboard log | A file path | File of JSON events, one per line (streams, packets, reflector links), that rpi-dashboard reads. Appended to; created if missing. A path that can't be opened stops the gateway. See [DashboardLog.md](../DashboardLog.md). |
| `AudioDir` | no | `audio/` | A directory | Voice clips (`*.dat`, plus `speak.dat` and `speak.index` for spoken callsigns) used for link announcements. A relative path is relative to the executable's directory, not the working directory. The directory must exist, or the gateway won't start. |

## [Radio]

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `RXFrequency` | yes | | Hz; see the limits below | Receive frequency. In simplex it is also the transmit frequency, and `TXFrequency` should equal it. |
| `TXFrequency` | yes | | Hz; see the limits below | Transmit frequency. Different from `RXFrequency` in duplex. |
| `Power` | CC1200 only | | dBm. **CC1200: -16 to 14**, whole numbers (a fraction is dropped) | CC1200 HAT transmit power. Other modems don't use it; it can be left out, but if present it must be a number. The SX1255 HAT's level is set with `[Modem] DACGain` and `MixerGain`, and an MMDVM_HS board's with `[Modem] RFLevel`. |
| `AFC` | CC1200 only | | Boolean | CC1200 HAT automatic frequency control: the receiver tracks the incoming signal's offset. The firmware applies a change at the next switch from transmit to receive. Other modems don't use it; it can be left out, but if present it must be a boolean. |
| `FrequencyCorr` | yes | | Integer; see the next column | Corrects the radio's reference oscillator. **CC1200:** passed to the firmware's FREQOFF register, in the firmware's own units, and it must be -200 to 200, or the HAT refuses it and the gateway stops. **SX1255 and MMDVM:** hertz, added to both frequencies. Any modem: -32768 to 32767, or it's a configuration error. If the radio transmits high, use a negative value. `0` for none. |
| `Duplex` | yes | | Boolean | Duplex (repeater) operation: receive and transmit at the same time on different frequencies. In duplex the gateway repeats received RF streams and packets on its transmit frequency, as well as sending them to the reflector. SX1255 and MMDVM support it (MMDVM_HS duplex boards such as the Dual Hat, and repeater boards). **The CC1200 HAT is half-duplex: `true` is a configuration error.** |
| `RXHoldoff` | no | `500ms` | Duration | Before transmitting a packet (an SMS), wait until nothing has been received for this long, so a packet doesn't start while another radio is still transmitting. |
| `PacketGap` | no | `250ms` | Duration | Minimum time between the end of the gateway's own last transmission (packet or voice) and the start of a packet. |

### Frequency and power limits

Checked when the gateway starts; a value outside them is a configuration error.

| Modem | Frequencies | Power |
|---|---|---|
| CC1200 HAT | 420–450 MHz (its filters) | -16 to 14 dBm |
| SX1255 HAT | 400–510 MHz (the chip's datasheet range) | not checked |
| MMDVM | not checked by the gateway. MMDVM_HS firmware accepts 144–148, 219–225, 420–475 and 842–950 MHz, except 145.8–146.0 and 435–438 MHz (satellite segments), and refuses others, which stops the gateway with *the modem refused RXFrequency … outside the bands its firmware allows*. Repeater boards ignore the frequency: the radio attached to them sets it. | not checked |

Staying inside the amateur band you're licensed for is up to you; the gateway checks only what the hardware can do.

## [Reflector]

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `Name` | no | empty: RF only | A reflector name from a host file, e.g. `M17-M17` | The reflector to link to at startup. Empty runs without a reflector: nothing goes to or comes from the network, local RF works as usual, and a duplex hotspot still repeats. A name in neither host file stops the gateway (`reflector … not found`). |
| `Module` | no | empty | One letter, `A`–`Z`, or empty | The reflector module to link to. Normally set it; empty sends a connect request with no module, which reflectors generally don't accept. A single space counts as empty. |
| `HostFile` | no | empty: none | A file path | The reflector list: one reflector per line, `NAME ADDRESS PORT`, separated by spaces or tabs; `#` starts a comment. The address may be IPv4, IPv6 or a hostname. On the M17 hotspot image a daily job keeps it up to date. A path that can't be read stops the gateway. |
| `OverrideHostFile` | no | empty: none | A file path | Same format as `HostFile`; your own entries. A name in this file wins over the same name in `HostFile`, and it isn't overwritten by updates. |

## [Log]

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `Level` | yes | | `ERROR`, `INFO` or `DEBUG` | The lowest level logged. `DEBUG` is verbose, including every command sent to the modem. |
| `Root` | no | empty | A file name, without `.log` | If set, the log goes to `Path/Root.log` (appended) instead of standard error. Leave empty under systemd or OpenRC, which collect standard error. |
| `Path` | no | empty | A directory | Directory for the `Root` log file. Used only when `Root` is set. |

## [MQTT]

Status over MQTT, in the format MMDVMHost and G4KLX's gateway programs use. [mqtt-design.md](mqtt-design.md) describes the messages. It's independent of `[General] DashboardLog`. Every key is optional.

| Key | Default | Values | What it does |
|---|---|---|---|
| `Enable` | `false` | Boolean | Publish status. When it's off, the other keys aren't read. |
| `Host` | `127.0.0.1` | Host name or address | The broker. |
| `Port` | `1883` | 1–65535 | The broker's port. |
| `Auth` | `false` | Boolean (`0`/`1` too, as MMDVMHost) | Log in with `Username` and `Password`. |
| `Username`, `Password` | empty | Text | The broker login, when `Auth` is on. |
| `Keepalive` | `60` | Seconds, at least 5 | How often the connection is checked. |
| `Name` | `m17-gateway` | Text without `/`, `#` or `+` | The topic prefix: events on `<Name>/json`, log lines on `<Name>/log`, current state (retained) on `<Name>/state/<type>`. |
| `LogLevel` | `INFO` | `NONE`, `ERROR`, `WARN`, `INFO` or `DEBUG` | The lowest level sent to `<Name>/log`. It's separate from `[Log] Level`. |

A value outside these ranges is a configuration error when `Enable` is on. The gateway starts even if the broker can't be reached, and keeps trying in the background. While it's unreachable, messages wait in a small queue, and once that's full the newest are dropped.

## [Modem]

`Type` selects the modem; the other keys apply to one modem type each.

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `Type` | no | `cc1200` | `cc1200`, `cc1200v2`, `sx1255`, `mmdvm`, `dummy` | The radio hardware. Anything else is a configuration error. |

### CC1200 HAT (`cc1200`, `cc1200v2`)

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `Port` | yes | | A serial device, e.g. `/dev/ttyAMA0` | The UART the HAT is on. |
| `Speed` | yes | | Baud rate; `460800` for the HAT's firmware | UART speed. |
| `NRSTPin` | yes | | GPIO line number on `gpiochip0` (the BCM number); `21` for the HAT | The HAT's reset line. The gateway resets the HAT at startup. |
| `Boot0Pin` | yes | | GPIO line number on `gpiochip0`; `20` for the HAT | The microcontroller's BOOT0 line, held low so the HAT runs its firmware (high is for flashing). |
| `ZMQPort` | no | `0`: off | TCP port | Debugging: publishes the received baseband samples (signed 8-bit, 24000 a second, in 2048-byte messages) on a ZeroMQ PUB socket, `tcp://*:ZMQPort`. It listens on every interface, so use it only on a trusted network. A port that can't be opened stops the gateway. |

### SX1255 HAT (`sx1255`)

All optional. The defaults suit the SX1255 HAT on a Raspberry Pi with the `m17-sx1255` overlay.

| Key | Default | Values | What it does |
|---|---|---|---|
| `SPIDevice` | `/dev/spidev0.0` | A spidev device | The chip's control interface. |
| `GPIOChip` | `gpiochip0` | A GPIO chip name | The chip that `ResetPin` is on. |
| `ResetPin` | `22` | GPIO line number | The SX1255's reset line. |
| `ALSACaptureDevice` | empty: automatic | `hw:CARD,DEVICE` (e.g. `hw:1,0`), a device node (e.g. `/dev/snd/pcmC1D0c`) or an exact device name | Where received I/Q samples come from. Automatic picks the first I2S capture device. Card numbers can change between boots; device names don't. |
| `ALSAPlaybackDevice` | empty: automatic | As above, e.g. `/dev/snd/pcmC1D1p` | Where transmitted I/Q samples go. Automatic uses the capture device's card, so HDMI audio is never picked. |
| `LNAGain` | `24` | dB: `0`, `12`, `24`, `36`, `42` or `48`. Others snap to the nearest, with a warning | Receive low-noise amplifier gain. There's no AGC, so this is a compromise: too little and weak signals are lost in the noise; too much and strong ones overload the front end. Both show as a rising bit error rate, logged at the end of each received stream. A 1 W handheld a few metres away has overloaded the receiver at 36 dB and above. |
| `PGAGain` | `0` | dB: 0–30 in 2 dB steps (odd values round down, above 30 is 30) | Receive programmable-gain amplifier, after the mixer. |
| `DACGain` | `-3` | dB: `0`, `-3`, `-6` or `-9`. Others snap to the nearest, with a warning | Transmit DAC gain; 0 is full scale. Part of the transmit level. Releases before 0.5.9 had this setting inverted (0 gave -9 dB); negate an old value to keep the same level. |
| `MixerGain` | `-12` | dB: -37.5 to -7.5 in 2 dB steps (-37.5, -35.5, … -7.5). Others round to the nearest step, so `-12` gives -11.5 | Transmit mixer gain; with `DACGain` it sets the transmit level. Both are relative to the chip's full scale, not dBm at the antenna, so the output power depends on the HAT. |
| `RawIQCapture` | empty: off | A file path | Diagnostics: write the raw received I/Q to this file, then stop. The format is interleaved stereo S32_LE at 125000 samples a second (left I, right Q). ALSA hardware devices are exclusive, so this is how to capture what the gateway itself receives. About 1 MB a second. |
| `RawIQSeconds` | `20` | Seconds | Length of the `RawIQCapture` capture. |

`[Radio] Power` and `AFC` aren't used by the SX1255 and can be left out. `Port` and `Speed` aren't used.

### MMDVM boards (`mmdvm`)

These match MMDVMHost's settings of the same names.

| Key | Required | Default | Values | What it does |
|---|---|---|---|---|
| `Protocol` | yes | | `uart` | How the board is connected. Only UART is supported. |
| `Port` | yes | | A serial device, e.g. `/dev/ttyAMA0` | The board's UART. |
| `Speed` | yes | | `1200`, `2400`, `4800`, `9600`, `19200`, `38400`, `57600`, `115200`, `230400` or `460800` | UART speed. MMDVM_HS boards typically use 115200 or 460800; it must match the firmware. |
| `RXInvert` | no | `false` | Boolean | Invert the received signal. |
| `TXInvert` | no | `true` | Boolean | Invert the transmitted signal. |
| `PTTInvert` | no | `false` | Boolean | Invert the PTT line (repeater boards). |
| `Debug` | no | `false` | Boolean | Ask the firmware for debug messages; they're logged at DEBUG level. |
| `TXDelay` | no | `100` | Milliseconds, 0–2550. Above is a configuration error | Extra delay after keying up before data is sent, sent to the firmware in 10 ms units, so the last digit is dropped. |
| `TXHang` | no | `5` | Seconds, 0–255. Above is a configuration error | How long the transmitter stays keyed after a transmission ends (M17 TX hang). |
| `RXLevel` | no | `50` | Percent, 0–100. Above is a configuration error | Receive level. |
| `TXLevel` | no | `50` | Percent, 0–100. Above is a configuration error | M17 transmit level (deviation). |
| `RXDCOffset` | no | `0` | -128 to 127 | Receive DC offset. Outside the range is a configuration error. |
| `TXDCOffset` | no | `0` | -128 to 127 | Transmit DC offset. Outside the range is a configuration error. |
| `RFLevel` | no | `100` | Percent, 0–100; fractions allowed. Outside the range is a configuration error | MMDVM_HS transmit power, sent to the firmware as 0–255, which sets the ADF7021's power amplifier (0–63). It's not in dB, and the actual output depends on the board. Repeater boards ignore it: the attached radio sets their power. m17-gateway 0.9.5 and earlier didn't send it, so MMDVM_HS boards always transmitted at full power. |

`[Radio] Power` and `AFC` aren't used by MMDVM boards and can be left out. `[Radio] Duplex` is sent to the board, so set it to match: duplex hotspot boards and repeaters `true`, simplex hotspots `false`.

### Dummy (`dummy`)

No `[Modem]` keys. Symbols are read from standard input and written to standard output, or from and to the files given with `-in` and `-out`. The `[Radio]` keys are still required and checked (no frequency limits apply).

## Command-line options

| Option | What it does |
|---|---|
| `-config FILE` | The configuration file (default `./gateway.ini`). |
| `-reset` | Reset the modem and exit. |
| `-in FILE`, `-out FILE` | Symbol input and output for the `dummy` modem (default standard input and output). |
| `-h` | List the options. |
