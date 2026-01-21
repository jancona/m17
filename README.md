# M17 library, gateway and clients, written in Go 

M17 Packet Mode is defined in the [spec](https://spec.m17project.org), and messaging is one obvious application. This project started as a set of tools to jump-start messaging and data communications using the [M17 ham radio mode](https://m17foundation.org/). It has evolved to provide more general tool and library support for M17.

There are several tools and a library here:

## Tools

### M17 Gateway

[m17-gateway](./cmd/m17-gateway/) makes allows a computer and modem to act as a repeater/hotspot. It also connects RF clients to Internet services such as reflectors. It currently supports the [CC1200 Pi HAT](https://github.com/M17-Project/CC1200_HAT-hw) and MMDVM-compatible hotspots and modem. When run on a Raspberry Pi with a CC1200 HAT, it can forward M17 voice and packet traffic to and from a reflector, making the Pi/CC1200 HAT an M17 voice and packet hotspot. 

The easiest way to get a working CC1200 hotspot, including `m17-gateway` and a [web dashboard](https://github.com/M17-Project/rpi-dashboard) is using DK1MI's [excellent installer script](https://github.com/DK1MI/cc1200-hotspot-installer). Highly recommended!

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

### CC1200 Modem Emulator

This program emulates the [CC1200 Modem firmware](https://github.com/M17-Project/CC1200_HAT-fw). It accepts samples from the gateway and echos them back. It was used for development of the gateway until I had a real CC1200 hat to test with.

## Library

The root directory of the project contains the Go library (`github.com/jancona/m17`) used to implement the M17 protocol parts of the tools. It's pretty rough right now, but I hope to improve it and make it more general and useful over time.
