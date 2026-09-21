# Splitting m17 into a lean protocol core

**Status:** implemented on branch `core-split`, 2026-09-19; awaiting hardware smoke test. Version 0.x: import paths change, behaviour does not.

## Problem

`github.com/jancona/m17` is one package holding the M17 protocol (addresses, LSF, packets, streams, CRC, FEC) together with three modem drivers, the SX1255 DSP chain, the M17_inet client, the hosts-file reader, and the dashboard logger. Importing it for the protocol alone links `go.bug.st/serial`, `go-zeromq/zmq4`, `yobert/alsa`, `warthog618/go-gpiocdev`, `golang.org/x/sys`, `golang.org/x/exp`, `gopkg.in/ini.v1`, and `sigurn/crc16`. Pigeon's roost re-implemented M17_inet framing, LSF, and CRC locally rather than take that on (pigeon `roost/m17frame.go`). Any other consumer that wants only the protocol will do the same, and the copies will drift.

Where the dependencies come from today:

| Files | Non-stdlib imports |
|---|---|
| `cc1200_modem.go`, `mmdvm_modem.go` | `serial`, `zmq4`, `ini` |
| `sx1255_*.go`, `modem_gpio_linux.go` | `alsa`, `gpiocdev`, `x/sys/unix`, `ini` |
| `transform.go` | `x/exp/constraints` |
| `crc.go` | `sigurn/crc16` |
| everything else (`encoding`, `lsf`, `packet`, `internet`, `codec`, `golay`, `decoder`, `hostfile`, `dashboard_logger`, `modem.go` interface, `dummy_modem`) | none |

## Goal

The root package `github.com/jancona/m17` is the protocol and nothing else, and depends on the standard library only. Device drivers, DSP, the network client and server, and the dashboard logger live in sub-packages that import the root. Every command in `cmd/` and the `server` package keep working unchanged in behaviour.

## Requirements

Numbered so a reviewer can check them off.

1. **Root package is stdlib-only.** `go list -deps github.com/jancona/m17` lists no module other than the standard library. This is the acceptance test for the whole change.
2. **No behaviour change.** `go build ./...` and `go test ./...` pass; `m17-gateway`, `m17-bridge`, `m17-inet`, `m17-message`, `m17-text-cli`, and `modem-emulator` build and run as before. Wire formats, timing, and log output are unchanged apart from package names in log lines.
3. **History preserved.** Files move with `git mv` so blame follows them.
4. **Layout** (module path unchanged, `github.com/jancona/m17`):

   | Package | Contents (moved from root unless noted) | Allowed imports |
   |---|---|---|
   | `m17` | `encoding.go` (addresses), `lsf.go`, `packet.go`, `stream.go` (`StreamDatagram`, moved out of `internet.go`), `crc.go`, `codec.go`, `golay.go`, `decoder.go` | stdlib |
   | `m17/inet` | `internet.go` → `client.go` (`InetClient` → `Client`); `hostfile.go`; `server/inet_server.go` → `server.go` (`InetServer` → `Server`, `Module` interface, plus `SendPacketToModule` so bridge modules need no access to the unexported client list) | stdlib, `m17` |
   | `m17/modem` | `modem.go` (interface), `cc1200_modem.go`, `mmdvm_modem.go`, `sx1255_*.go`, `modem_gpio_*.go`, `dummy_modem.go`, `transform.go` | stdlib, `m17`, the device libraries |
   | `m17/dashboard` | `dashboard_logger.go` | stdlib, `m17` |
   | `server` (existing) | the bridge modules `aprs_module.go`, `discord_module.go`, `irc_module.go`, which carry the Discord, IRC, and APRS dependencies | `m17`, `m17/inet`, their own deps |

   Whether `server` is renamed `m17/bridge` is the maintainer's call; the spec only requires that its heavy dependencies stay out of `inet`.
5. **`StreamDatagram` is protocol, not transport.** It is defined by the M17_inet framing but consumed by the codec (`ConvolutionalEncodeStream`) and the decoder, so it stays in the root. `inet` adds the magic prefix and CRC when sending.
6. **`inet.Client` does not depend on the dashboard logger.** Today `InetClient` calls `DashboardLogger.Log` on connect and disconnect. Replace that with an optional callback or small interface (`OnConnect(name, module)`, `OnDisconnect(...)`, or a single `func(event string, kv ...any)`) that `m17-gateway` wires to the dashboard logger. Nothing in `inet` imports `dashboard`.
7. **Drop `sigurn/crc16`.** The M17 CRC-16 (polynomial 0x5935, init 0xFFFF, no reflection, no final XOR) is 15 lines. Keep the existing tests and add the specification check value: `CRC("123456789") == 0x772B`.
8. **Drop `golang.org/x/exp/constraints`.** `transform.go` moves to `modem` anyway, but its `Number` constraint can be written locally (`~int | ~int8 | … | ~float32 | ~float64`) so `x/exp` leaves `go.mod`.
9. **Exported names.** Rename only where the package name would otherwise stutter or the old name was a prefix standing in for a package:

   | Before | After |
   |---|---|
   | `m17.InetClient`, `m17.NewInetClient` | `inet.Client`, `inet.NewClient` |
   | `m17.Magic*` | unchanged: `StreamDatagram.ToBytes` embeds the `"M17 "` magic, so the M17_inet magics are protocol constants and stay in the root |
   | `m17.Hostfile`, `m17.NewHostfile`, `m17.Host` | `inet.Hostfile`, `inet.NewHostfile`, `inet.Host` |
   | `server.InetServer`, `server.NewInetServer`, `server.Module` | `inet.Server`, `inet.NewServer`, `inet.Module` |
   | `m17.Modem`, `m17.NewCC1200Modem`, `m17.NewMMDVMModem`, `m17.NewSX1255Modem`, `m17.NewDummyModem` | `modem.Modem`, `modem.NewCC1200`, `modem.NewMMDVM`, `modem.NewSX1255`, `modem.NewDummy` |
   | `m17.DashboardLogger`, `m17.NewDashboardLogger` | `dashboard.Logger`, `dashboard.New` |
   | `m17.Symbol`, `m17.SoftBit`, `m17.Decoder`, `m17.LSF`, `m17.Packet`, `m17.StreamDatagram`, `m17.EncodeCallsign`, `m17.DecodeCallsign`, `m17.CRC` | unchanged |

   Anything not listed keeps its name. Types the modems share with the codec (`Symbol`, `SoftBit`, `Decoder`) stay in the root, which is why `modem` imports `m17` and not the reverse.
10. **Modem configuration stays as it is.** The modem constructors currently take `*ini.Section`. Changing them to typed config structs would remove `ini` from `modem` and belongs in a later change; it is not required here because `modem` is heavy by nature. Note it as a follow-up.
11. **Logging is out of scope.** Library packages use the global `log` with `[DEBUG]`-style prefixes throughout. Leave that alone in this change; a move to `log/slog` with an injected logger is a separate follow-up. The one exception is requirement 6, which removes a concrete dependency rather than restyling logs.
12. **Tests move with their files.** `*_test.go` follow their subjects into the new packages. Tests that reach across packages (for example a modem test using codec helpers) import the root like any other consumer. `go vet ./...` and `gofmt -l .` are clean at the end.
13. **`go.work` consumers keep building.** The pigeon workspace resolves `github.com/jancona/m17` locally; after the split, pigeon's roost replaces `roost/m17frame.go` with `m17` and `m17/inet`. That is pigeon's change, listed here so it is not forgotten.
14. **Documentation.** `README.md` shows the new import paths and a one-paragraph package map. Tag the result (`v0.x.0`) once the commands are verified on hardware.

## Order of work

Each step leaves `go build ./...` passing.

1. Replace `crc.go`'s dependency with the local table; add the check value test. Remove `sigurn/crc16` from `go.mod`.
2. `git mv` the modem, GPIO, SPI, ALSA, DSP, and `transform.go` files to `modem/`; rename constructors; update `cmd/m17-gateway` and `cmd/modem-emulator`. Write `Number` locally; remove `x/exp`.
3. `git mv dashboard_logger.go dashboard/`; rename; update callers.
4. Create `inet/`: move `hostfile.go`; split `internet.go` into `stream.go` (root, `StreamDatagram`) and `inet/client.go`; add the event callback of requirement 6; move `server/inet_server.go` to `inet/server.go`; update `server`'s modules and every command.
5. Run `go list -deps github.com/jancona/m17` and confirm requirement 1. Run all tests. Build the six commands for `linux/arm64` and smoke-test `m17-gateway` on a hotspot.
6. Update `README.md`, tag.

Estimated size: about 30 files moved, a few hundred lines of import and call-site edits, one new callback in `inet.Client`, and one CRC table. No new logic.

## Verification checklist

- [ ] `go list -deps github.com/jancona/m17 | grep -v '^[^.]*$'` prints nothing (no dotted module paths, i.e. stdlib only)
- [ ] `go build ./... && go test ./... && go vet ./...` clean; `gofmt -l .` empty
- [ ] `m17-gateway` links to a reflector and passes voice both ways on a hotspot
- [ ] `m17-bridge` starts with its modules
- [ ] `go.mod` no longer lists `sigurn/crc16` or `golang.org/x/exp`
- [ ] pigeon builds against the split (`go build ./...` in the workspace) before and after its follow-up removes `roost/m17frame.go`
