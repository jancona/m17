# MQTT status for m17-gateway: design

Status: **draft for review**. Nothing here is implemented yet.

## Goal

m17-gateway publishes what it's doing over MQTT, in the format MMDVMHost already uses, so that:

- one dashboard can show either an MMDVMHost hotspot or an m17-gateway one;
- the hotspot image's m17-admin (and later a Go UI) can show live activity and keep last-heard history without parsing log files;
- people can feed a hotspot into Home Assistant, Node-RED or a network-wide dashboard.

`dashboard.log` stays as it is. MQTT is a second, independent output, off by default, so the Debian installer and rpi-dashboard keep working unchanged.

## What MMDVMHost does

From M17-Project/MMDVMHost `master` (its `schema.json`, `MQTTConnection.cpp`, `Log.cpp`, `MMDVM-Host.cpp` and `M17Control.cpp`):

- **Connection:** `[MQTT]` `Host` (127.0.0.1), `Port` (1883), `Auth` (0/1), `Username`, `Password`, `Keepalive` (60 s), `Name`. It reconnects in the background. There's no will message and no retained message.
- **Topics:** `<Name>/json` for events and `<Name>/log` for log lines. QoS 2 (exactly once), not retained. MMDVMHost also subscribes to `<Name>/command`, `display-in` and `qso-info` for remote control; the gateway doesn't need those.
- **`<Name>/json`:** each message is one object with a single key naming its type, e.g. `{"M17": {...}}`. Every object has a `timestamp` in UTC with milliseconds: `2026-11-20T01:23:45.678Z`.
- **`<Name>/log`:** `L: YYYY-MM-DD HH:MM:SS.mmm text` in UTC, where `L` is the level letter. `[Log] MQTTLevel` sets the lowest level sent; 0 sends nothing.
- **Types it publishes that matter here:**
  - `MMDVM` `{mode}`: the current mode (`idle`, `M17`, `error`, `lockout`). `MMDVM` `{message}` is a lifecycle message such as "MMDVM-Host is starting".
  - `Modem` `{open, protocol, description, modes, connection, port, speed}`: when the modem opens or closes.
  - `M17` `{src_callsign, dst_callsign, source, action, traffic_type}` at the start of a voice stream. `{action, duration, ber, rssi}` at its end.
    - `source` is `rf` or `network`.
    - `action` is `start`, `late_entry`, `end`, `lost` or `rejected`.
    - `duration` is in seconds: frames ÷ 25.
    - `ber` is a percentage.
    - The end message repeats neither callsign: a consumer pairs it with the start.
  - `M17Packet` `{source, src_callsign, dst_callsign, protocol, length, text | data}`: `text` for SMS (protocol 5), otherwise `data` in hex.
  - `BER`, `RSSI`, `Text`: running values during a stream, and decoded META text.
- **Callsigns are not padded.** The schema says an `m17_callsign` is exactly 9 characters, but MMDVMHost sends the decoded callsign as is: `N1ADJ`, `N1ADJ   G` (with a module letter), `ALL` for broadcast, `#ROOM`. **The gateway will send what MMDVMHost sends, not what the schema says**, and the schema should be corrected (see the schema changes below).

## Configuration

A new `[MQTT]` section, with MMDVMHost's keys plus `Enable`:

```ini
[MQTT]
Enable=false
Host=127.0.0.1
Port=1883
Auth=false
Username=
Password=
Keepalive=60
Name=m17-gateway
; Lowest log level sent to <Name>/log: NONE, ERROR, INFO or DEBUG
LogLevel=INFO
```

- `Enable` is the on/off switch. `[General] DashboardLog` is already the switch for `dashboard.log`: empty turns it off. The two are independent.
- `Auth` accepts `0`/`1`, like MMDVMHost, as well as `true`/`false`.
- `Name` defaults to `m17-gateway` rather than MMDVMHost's `mmdvm`, so both can share a broker.
- `LogLevel` is in `[MQTT]` rather than `[Log] MQTTLevel`, and uses the gateway's level names instead of MMDVMHost's numbers.

## Messages

All go to `<Name>/json`, using MMDVMHost's object names and fields unless marked **new**. A new object or field must be added to M17-Project/MMDVMHost's `schema.json` (see below) before it's released.

### Gateway lifecycle

| When | Message |
|---|---|
| Startup, once MQTT is connected | `MMDVM` `{"message": "m17-gateway is starting"}` |
| The modem opened | `Modem` `{"open": true, "description": <ident>, "modes": ["M17"], "connection": "uart" or "spi", "port": ..., "speed": ...}`, plus `"protocol": 1 or 2` for MMDVM boards |
| Ready, idle | `MMDVM` `{"mode": "idle"}` |
| Clean shutdown | `MMDVM` `{"message": "m17-gateway is stopping"}`, then `Modem` `{"open": false}` |
| The modem failed or went away | `MMDVM` `{"mode": "error"}` |

- `modes` and `connection: "spi"` (for the SX1255) need schema additions: MMDVMHost's `modem_mode` list lacks `M17`, and its `modem_connection` list lacks `spi`.
- **Each time MQTT (re)connects**, the gateway also republishes the current `Modem` and `MMDVM` mode messages. Then a consumer that starts after the gateway, or a broker restart, still learns the current state without retained messages.

### Mode

MMDVMHost switches its mode to `M17` while there's M17 activity, and back to `idle` after a hang time. m17-gateway does the same:
- `M17` on the first stream or packet;
- `idle` after 10 s with no RF activity, or 3 s after network-only activity. These are MMDVMHost's default `RFModeHang` and `NetModeHang`, fixed for now.

### Voice streams

| Gateway event (today's `dashboard.log`) | MQTT |
|---|---|
| RF voice start | `M17` `{src_callsign, dst_callsign, source: "rf", action: "start", traffic_type, can}` |
| RF voice joined mid-stream (LSF rebuilt from the LICH) | the same with `action: "late_entry"` |
| RF voice end (EOT or last-frame flag) | `M17` `{source: "rf", action: "end", duration, ber}` |
| RF voice timed out, with no last frame | `M17` `{source: "rf", action: "lost", duration, ber}` |
| Network voice start | `M17` `{src_callsign, dst_callsign, source: "network", action: "start", traffic_type, can}` |
| Network voice joined mid-stream | the same with `action: "late_entry"` |
| Network voice end | `M17` `{source: "network", action: "end", duration}` |
| Network voice timed out (the gateway's 1 s backstop) | `M17` `{source: "network", action: "lost", duration}` |

- `traffic_type` comes from the LSF: `audio`, `audio_data` or `data`.
- `duration` is frames ÷ 25, from the stream's frame counter.
- `ber` is the gateway's bit error rate before error correction, as a percentage. That's the same measure as dashboard.log's `mer` and MMDVMHost's `ber`.
- `can` is **new** (optional, 0–15). dashboard.log has it, and it matters for M17.
- RSSI: no modem reports it to the gateway today, so it's left out (it's optional in the schema).
- Like MMDVMHost, the end message carries no callsigns. That keeps the format identical. If a consumer needs to pair start and end, it uses the order of messages, as MMDVMHost consumers already do.

### Packets

| Gateway event | MQTT |
|---|---|
| RF packet | `M17Packet` `{source: "rf", src_callsign, dst_callsign, protocol, length, text or data, can}` |
| Network packet | the same with `source: "network"` |

`can` is **new** here too. `text` is the SMS text without its terminating NUL. For other protocols, `data` is the payload in upper-case hex, as MMDVMHost does.

### GNSS (**new**)

dashboard.log reports positions decoded from the LSF META field (at most one per 15 s). MMDVMHost has no equivalent, so this needs a **new** object:

```json
{"M17GNSS": {"timestamp": "...", "source": "rf", "src_callsign": "N1ADJ",
  "data_source": 1, "station_type": 2, "latitude": 43.878929, "longitude": -69.986374,
  "altitude": 33.0, "speed": 62.0, "bearing": 35, "radius": 4}}
```

`altitude`, `speed`/`bearing` and `radius` are present only when the LSF marks them valid, as in dashboard.log. The field names use the schema's snake_case.

### Reflector links (**new**)

MMDVMHost's M17 support goes through a separate gateway program, so it has no link messages. A **new** object:

```json
{"M17Reflector": {"timestamp": "...", "action": "linked", "reflector": "M17-M17", "module": "C"}}
```

`action` is `linked` or `unlinked`, from the gateway's existing connect and disconnect events. Running with no reflector publishes nothing.

### What isn't published

Same as dashboard.log today:
- what the gateway transmits itself (echo playback, PARROT replies, link announcements);
- what a duplex hotspot repeats.

MMDVMHost doesn't publish these either. They can be added later if a dashboard wants them.

`BER`, `RSSI` and `Text` (running values during a stream) are left out of the first version. META text isn't decoded by the gateway today.

## Log lines

`<Name>/log` gets the gateway's log lines at or above `[MQTT] LogLevel`, in MMDVMHost's format: `I: 2026-11-20 01:23:45.678 text`, in UTC, with level letters `D`, `I`, `E` (and `W` for the `[WARN]` lines). The gateway's existing log output is unchanged.

## Implementation

- **Package:** a new package, `mqttstatus`, beside `dashboard`. The gateway calls one small interface at the places where it writes dashboard events today. The dashboard logger and the MQTT publisher both sit behind it, so there's no second set of hooks.
- **Client:** `github.com/eclipse/paho.mqtt.golang`. It's pure Go, so the static `CGO_ENABLED=0` build stays as it is. It speaks MQTT 3.1.1 and reconnects on its own.
- **Never blocks the radio path:** messages go into a buffered channel, about 256 messages, and one goroutine publishes them. When the buffer is full, or the broker is unreachable, new messages are dropped and counted, and the count is logged once a minute. A slow or dead broker can't delay audio.
- **Startup doesn't wait for the broker:** a broker that's down at startup is retried in the background, like MMDVMHost.
- **QoS 2 and no retain**, as MMDVMHost.
- **Timestamps:** UTC with milliseconds, formatted like MMDVMHost.

## Schema changes to propose to M17-Project/MMDVMHost

Opened as a PR against `schema.json` before the gateway's MQTT support is released:

1. `m17_callsign`: `minLength` 1, `maxLength` 9. Today's exact-9 rule doesn't match what MMDVMHost sends.
2. `modem_mode`: add `M17`. The fork also leaves M17 out of the `Modem` message's `modes`, which is a small bug to fix in the same PR.
3. `modem_connection`: add `spi`.
4. `M17`, `M17Packet`: optional `can`, an integer 0–15.
5. New `M17GNSS` and `M17Reflector` objects, as above.

## Testing

- **Unit tests:**
  - A fake publisher records messages.
  - The gateway's existing tests drive RF and network streams, packets and reflector events through it.
  - Each message is checked against the schema's required fields and types.
- **Shared fixtures:** sample messages, one per type, kept in this repository under `testdata/mqtt/`. They're offered to MMDVMHost along with the schema PR, so both projects test against the same examples.
- **Integration test:** a local Mosquitto in Docker with `mosquitto_sub -v -t 'm17-gateway/#'`, while the gateway's test inputs play.
- **Hardware:** the Dual Hat and the SX1255, pointed at a broker on the Mac or on the Pi, through a voice QSO, an SMS, a reflector link and unlink, and a broker restart (to check the state republished on reconnect).

## Rollout

1. This design, reviewed.
2. The implementation on the `feature/mqtt` branch, not released until it's been tried on hardware.
3. The schema PR to M17-Project/MMDVMHost.
4. Released in an m17-gateway version. Then the hotspot image adds Mosquitto (localhost plus WebSocket) and turns `[MQTT] Enable` on, and m17-admin subscribes and keeps history: steps 2 and 3 of the plan.

## Open questions

1. **Retained messages:** republishing `Modem` and mode on reconnect covers consumers that start late, but only while the gateway is connected. A retained `<Name>/state` topic would also cover a consumer that starts while the gateway is down. MMDVMHost doesn't do this. Worth adding as an extension, or keep strictly to MMDVMHost's behaviour?
2. **A will message:** MMDVMHost has none, so a crashed gateway just goes quiet. A will message (`MMDVM` `{"mode": "error"}`) would make a crash visible, but it can't carry a correct timestamp. Add it?
3. **Mode hang times:** fixed at MMDVMHost's defaults (10 s and 3 s), or settings?
