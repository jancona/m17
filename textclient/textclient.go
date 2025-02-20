package main

// Inspired by https://github.com/jhudson8/golang-chat-example/

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jancona/m17text/m17"
)

var (
	serverArg   *string = flag.String("server", "", "Reflector server")
	portArg     *uint   = flag.Uint("port", 17000, "Port the reflector listens on")
	moduleArg   *string = flag.String("module", "T", "Module to connect to")
	callsignArg *string = flag.String("callsign", "N0CALL", "User's callsign")
	helpArg     *bool   = flag.Bool("h", false, "Print arguments")
)

var encodedCallsign []byte

func main() {
	flag.Parse()
	if *helpArg {
		flag.Usage()
		return
	}

	var err error
	encodedCallsign, err = m17.EncodeCallsign(*callsignArg)
	if err != nil {
		fmt.Printf("Bad callsign %s: %v", *callsignArg, err)
		os.Exit(1)
	}

	r, err := m17.NewRelay(*serverArg, *portArg, *moduleArg, *callsignArg, handleM17)
	if err != nil {
		fmt.Printf("Error creating client: %v", err)
		os.Exit(1)
	}
	err = r.Connect()
	if err != nil {
		fmt.Printf("Error connecting to %s:%d %s: %v", *serverArg, *portArg, *moduleArg, err)
		os.Exit(1)
	}
	defer r.Close()

	// handle responses from reflector
	go func() {
		r.Handle()
		// When Handle exits, we're done
		os.Exit(0)
	}()

	handleConsoleInput(r)
}

func handleM17(p m17.Packet) error {
	// // A packet is an LSF + type code 0x05 for SMS + data up to 823 bytes
	// log.Printf("[DEBUG] p: %#v", p)
	dst, err := m17.DecodeCallsign(p.LSF.Dst)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad dst callsign: %v", err)
	}
	src, err := m17.DecodeCallsign(p.LSF.Src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad src callsign: %v", err)
	}
	msg := string(p.Payload)
	if p.Type == m17.PacketTypeSMS /*&& (dst == *callsignArg || dst == m17.DestinationAll)*/ {
		fmt.Printf("\n%s %s>%s: %s\n> ", time.Now().Format(time.DateTime), src, dst, msg)
	}
	return nil
}

// keep watching for console input
// send the "message" command to the chat server when we have some
func handleConsoleInput(c *m17.Relay) {
	var done bool

	reader := bufio.NewReader(os.Stdin)

	for !done {
		print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Couldn't read from console: %v", err)
			os.Exit(1)
		}

		input = strings.TrimSpace(input)
		if input != "" {
			command, callsign, message, ok := parseInput(input)
			if !ok {
				// Ignore bad input
				fmt.Printf("Error parsing command \"%s\"\n", command)
				continue
			}

			if command == "" {
				err := c.SendSMS(callsign, *callsignArg, message)
				if err != nil {
					fmt.Printf("Error sending message: %v\n", err)
					continue
				}
			} else {
				switch command {
				case "quit":
					done = true

				default:
					fmt.Printf("Unknown command \"%s\"\n", command)
				}
			}
		}
	}
}

// Inputs are of the forms:
//
//	/command message
//	callsign: message
func parseInput(input string) (command, callsign, message string, ok bool) {
	if input[0] == '/' {
		// It's a command
		command, message, _ = strings.Cut(input[1:], " ")
		ok = true
		return
	}
	callsign, message, ok = strings.Cut(input, ": ")
	return
}
