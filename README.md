# M17 library, gateway and clients, written in Go 

M17 Packet Mode is defined in the [spec](https://spec.m17project.org), and messaging is one obvious application. This project started as a set of tools to jump-start messaging and data communications using the [M17 ham radio mode](https://m17foundation.org/). It has evolved to provide more general tool and library support for M17.

There are several tools and a library here:

## Tools

### M17 Messaging Bridge

[m17-bridge](./cmd/m17-bridge/) is an experimental network service that bridges M17 SMS messages to and from other messaging protocols. It speaks the [M17 Internet protocol](https://github.com/M17-Project/M17_inet/blob/main/M17%20Internet%20Interface.md), so it looks like a reflector to hotspots/repeaters that connect to it.

It bridges to these systems:
* **Discord:** An M17 module letter is connected to a Discord channel. All M17 messages are send to the channel and all channel messages are sent to connected hotspots/repeaters. Since all received traffic is sent over the air, membertship in the Discord channels should be restricted to licensed hams.
* **IRC:** An M17 module letter is connected to an IRC server and allows sending and receiving private messages via M17 SMS. Because received traffic is sent over the air, accounts on the server should probably be restricted to licensed hams. 
* **APRS:** An M17 module letter is connected to an APRS-IS server. Radios can use M17 SMS messaging to send APRS messages. When it sees M17 GNSS data, the bridge will also send periodiic APRS position reports. The bridge will send APRS messages to connected radios. A radio must send a message in order to register to receive APRS messages.

### M17 Gateway

[m17-gateway](./cmd/m17-gateway/) allows a computer and modem to act as a repeater/hotspot. It also connects RF clients to Internet services such as reflectors. It currently supports the [CC1200 Pi HAT](https://github.com/M17-Project/CC1200_HAT-hw), the [SX1255 Pi HAT](https://github.com/M17-Project/SX1255_HAT-hw) and MMDVM-compatible hotspots and modems. When run on a Raspberry Pi with a compatible HAT or modem, it can forward M17 voice and packet traffic to and from a reflector, making it an M17 voice and packet hotspot. When used with the SX1255 HAT or an MMDVM modem it can form the heart of an M17 repeater.

The easiest way to get a working hotspot, including `m17-gateway` and a [web dashboard](https://github.com/M17-Project/rpi-dashboard) is using DK1MI's [excellent installer script](https://github.com/DK1MI/m17-hotspot-installer). Highly recommended!

Another way to install just the gateway is using the APT package from a release on Github. To install it:

1. Copy the URL for the latest `deb` package from https://github.com/jancona/m17/releases
2. From a shell on the Pi do:
```
wget <latest deb URL>
sudo dpkg -i m17-gateway_<version>_arm64.deb
```

To build it just run `go build` in the `m17-gateway` directory. Because Go natively supports cross-compilation, you can build a Raspberry Pi executable on any machine by running `GOOS=linux GOARCH=arm64 go build`, the using `scp` to copy the resulting executable to the Pi.

```
Usage of gateway:
  -config string
    	Configuration file (default "./gateway.ini")
  -h	Print arguments
  -in string
    	M17 symbol input (default stdin)
  -out string
    	M17 symbol output (default stdout)
```

#### Configuration

Bu default, the gateway looks for configuration in `gateway.ini` in the working directory. See `m17-gateway/gateway.ini.sample` for details.

### GUI Messaging Client

[m17-message](./cmd/m17-message/) is a cross-platform GUI network messaging client. It's based on [Fybro](https://github.com/andydotxyz/fybro), a  messaging app built using [Fyne](https://fyne.io/), a framework for building multi-platform GUI apps in Go. To build the client just run `go build` in the `m17-message` directory. For more packaging options, see the [Fyne docs](https://docs.fyne.io/started/packaging).

### CLI Messaging Client

[m17-text-cli](./cmd/m17-text-cli/) is a rudimentary network messaging client--think Droidstar but for text messages (and not nearly as nice looking). Note that since I started writing this a number of tools (including DroidStar) have added M17 messaging support.

To build the client just run `go build` in the `m17-text-cli` directory. 

Example: `./m17-text-cli -server m17.openquad.net -callsign N1ADJ`

The program will respond with a prompt `> `. To send a message, enter `callsign: message`. Incoming messages for you will appear starting with `< `. To quit, enter `/quit`.

Sample session:
```
$ ./m17-text-cli -server m17.openquad.net
> N1ADJ: Hi from my other window!
>
2025-02-06 14:45:45 N0CALL>@ALL: Hi back
> /quit
```

Command line arguments:
```
Usage of ./m17-text-cli:
  -callsign string
    	User's callsign (default "N0CALL")
  -h	Print arguments
  -module string
      Module to connect to (default "P")
  -port uint
    	Port the reflector listens on (default 17000)
  -server string
    	Reflector server
```

### Reflector Traffic Monitor

[m17-inet](./cmd/m17-inet/) connects to an M17 reflector and monitors voice stream traffic, reporting per-stream packet rate and packet loss (detected by gaps in frame numbers).

To build it just run `go build` in the `m17-inet` directory.

Example: `./m17-inet -host m17.kansascitywide.com:17000 -module A`

Sample output:
```
Connected to m17.kansascitywide.com:17000 module A, monitoring traffic...
Stream 0x1A2B started: W1ABC > @ALL
Stream 0x1A2B  W1ABC > @ALL  Duration: 12.3s  Frames (received/expected): 305/308  Rate: 24.8/s  Loss: 0.97%
```

Command line arguments:
```
Usage of ./m17-inet:
  -host string
    	Reflector address with port (e.g. 172.234.217.28:17000)
  -module string
    	Module to connect to (A-Z) (default "A")
  -callsign string
    	Callsign to identify to the reflector (default "N0CALL")
```

### CC1200 Modem Emulator

This program emulates the [CC1200 Modem firmware](https://github.com/M17-Project/CC1200_HAT-fw). It accepts samples from the gateway and echos them back. It was used for development of the gateway until I had a real CC1200 hat to test with.

## Library

The root directory of the project contains the Go library (`github.com/jancona/m17`) used to implement the M17 protocol parts of the tools. It's pretty rough right now, but I hope to improve it and make it more general and useful over time.
