# SIA

SIA DC-09 is the primary AjaxBridge input. Ajax sends monitoring-station events directly to AjaxBridge over TCP. AjaxBridge parses those events into account and zone state and treats that state as authoritative.

Ajax Systems SIA setup reference: https://support.ajax.systems/en/how-to-use-sia-for-cms-connection/

## Details And How It Works

AjaxBridge implements a SIA DC-09 receiver:

1. Listens on `AJAXBRIDGE_SIA_ADDR`, default `:8099`.
2. Reads one SIA frame from each TCP connection.
3. Validates frame shape, length, and CRC when `AJAXBRIDGE_STRICT_CRC=true`.
4. Decrypts encrypted content when `AJAXBRIDGE_ENCRYPTION_KEY` is set.
5. Checks the account/object number when `AJAXBRIDGE_ACCOUNT` is set.
6. Normalizes Contact ID and SIA-DCS event data into a stable event class, action, name, and signal.
7. Applies the event to the in-memory state engine.
8. Publishes updated HTTP, Prometheus, MQTT, and Home Assistant state.
9. Returns SIA `ACK` for accepted frames or `NAK` for rejected frames.

SIA event state is the source of truth for:

- account online/offline
- armed, disarmed, night mode, and partially armed state
- intrusion, fire, leak, panic, duress, and medical alarm signals
- tamper state
- trouble state such as battery, connectivity, power, hardware, firmware, bypass, and interference
- event history and last event metadata

Jeedom data never overwrites those fields. If Jeedom reports a different state for the same physical device, use SIA for the security dashboard state and show Jeedom as secondary diagnostics.

## State Engine Behavior

Accounts are keyed by SIA account/object number. Zones are keyed by `account + zone`. Device catalog metadata can refine the name, room, kind, expected signals, and Jeedom linking for a zone.

Account mode is derived as:

| Condition | `mode` |
| --- | --- |
| `night_mode=true` | `night` |
| `armed=true` | `armed` |
| otherwise | `disarmed` |

Zone alarms and troubles are sticky until SIA sends a restore event. For example, a burglary alarm sets `alarm_active=true` and `signal_active.burglary=true`; a burglary restore clears that specific alarm signal. A battery trouble remains active until a battery restore arrives. A device bypass/deactivation (`QB`) or device turned-off event (`ZZ`) clears the active alarm for that zone because the device is no longer participating in alarm state; last-event metadata remains available as history.

The account `online` field is recalculated from the latest ping/event and `AJAXBRIDGE_OFFLINE_GRACE`.

## Configure In The Ajax App

The Ajax app UI can move between releases. The current official flow is documented by Ajax in the SIA DC-09 article linked above. In general:

1. Open the Ajax app with an admin or PRO account that can configure system settings.
2. Select the space or hub.
3. Open the space or hub settings.
4. Open the monitoring station or central monitoring station settings.
5. Select protocol `SIA DC-09` or `SIA DC-09 (SIA-DCS)`.
6. Set the primary receiver host/IP to the machine running AjaxBridge.
7. Set the primary receiver port to `8099`, unless `AJAXBRIDGE_SIA_ADDR` uses a different port.
8. Set the account/object number and use the same value in `AJAXBRIDGE_ACCOUNT`.
9. If encryption is enabled in Ajax, set the same AES key in `AJAXBRIDGE_ENCRYPTION_KEY`.
10. Enable the network channels you want Ajax to use for monitoring delivery.

Network requirements:

- The Ajax hub must reach AjaxBridge on TCP port `8099`.
- If AjaxBridge is in Docker, publish `8099:8099`.
- If the bridge is behind NAT, forward the TCP port from the hub network to the bridge host.

## Configure AjaxBridge

Minimal Docker Compose environment:

```yaml
services:
  ajaxbridge:
    image: rcooler/ajax-bridge:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "8099:8099"
    environment:
      AJAXBRIDGE_HTTP_ADDR: ":8080"
      AJAXBRIDGE_SIA_ADDR: ":8099"
      AJAXBRIDGE_ACCOUNT: "0001"
      AJAXBRIDGE_DEVICES_PATH: "/data/devices.json"
      AJAXBRIDGE_STRICT_CRC: "true"
    volumes:
      - ./data:/data
```

With SIA encryption:

```yaml
AJAXBRIDGE_ENCRYPTION_KEY: "REPLACE_WITH_AES_KEY"
```

Bridge SIA variables:

| Variable | Default | Description |
| --- | --- | --- |
| `AJAXBRIDGE_SIA_ADDR` | `:8099` | TCP listen address for SIA DC-09. |
| `AJAXBRIDGE_ACCOUNT` | empty | Expected Ajax account/object number. Empty accepts all accounts. |
| `AJAXBRIDGE_ENCRYPTION_KEY` | empty | AES key as 32/48/64 hex chars or raw 16/24/32 bytes. Ajax direct SIA normally uses 128-bit encryption. |
| `AJAXBRIDGE_STRICT_CRC` | `true` | Reject invalid CRC frames. |
| `AJAXBRIDGE_PING_INTERVAL` | `60s` | Expected Ajax monitoring ping interval. |
| `AJAXBRIDGE_OFFLINE_GRACE` | `180s` | Grace period before an account is marked offline. |
| `AJAXBRIDGE_READ_TIMEOUT` | `120s` | Per-connection SIA read timeout. |

## Device Catalog

The catalog is optional but strongly recommended. It gives stable names, rooms, kinds, expected signals, and Jeedom links.

```json
[
  {
    "account": "A0F80D",
    "zone": "8",
    "name": "Server power",
    "room": "Boiler room",
    "kind": "WallSwitch",
    "events": ["power", "connectivity", "hardware", "firmware"],
    "jeedom_names": ["Serverna"],
    "jeedom_command_ids": ["52", "53", "54", "55", "56", "57", "58", "59"]
  }
]
```

Catalog fields used by SIA:

| Field | Purpose |
| --- | --- |
| `account` | SIA account/object number. |
| `zone` | SIA zone number. |
| `partition` | Optional SIA partition/area. |
| `group` | Optional SIA group. |
| `device` | Optional Ajax device id when available. |
| `name` | Human device name. |
| `room` | Home Assistant suggested area and dashboard room. |
| `kind` | Device model/category. |
| `events` | Expected signal names used to pre-create HA binary sensors. |
| `jeedom_names` | Jeedom aliases for duplicate prevention. |
| `jeedom_command_ids` | Jeedom command ids for duplicate prevention. |

If a SIA zone is missing from the catalog, AjaxBridge creates an auto-discovered catalog entry from the first event. You should review and rename those entries.

## Raw SIA Forwarding

AjaxBridge can forward valid raw SIA frames to other receivers, such as a CMS or another SIA integration:

```yaml
AJAXBRIDGE_FORWARD_ADDR: "home-assistant.example:12345,cms.example:7700"
AJAXBRIDGE_FORWARD_TIMEOUT: "5s"
AJAXBRIDGE_FORWARD_REQUIRE_ACK: "false"
```

By default, forwarding failures are logged and exported as metrics, but AjaxBridge still ACKs Ajax. Set `AJAXBRIDGE_FORWARD_REQUIRE_ACK=true` only when Ajax should receive `NAK` if any upstream receiver fails.

## Verify SIA

Check logs:

```bash
docker compose logs -f ajaxbridge
```

Check current state:

```bash
curl http://localhost:8080/state
curl http://localhost:8080/events?limit=20
curl http://localhost:8080/devices
```

Expected successful startup logs include:

```text
SIA listener started
HTTP server started
```

## Troubleshooting

No SIA events:

- Confirm the Ajax receiver host and port point to AjaxBridge.
- Confirm TCP `8099` is reachable from the Ajax hub network.
- Confirm `AJAXBRIDGE_ACCOUNT` matches the Ajax object/account number.
- If encryption is enabled, confirm the same key is configured on both sides.
- Check firewall and Docker port publishing.

Frames rejected:

- Check `/events?limit=20` and logs for parse status.
- `crc_invalid` usually means the payload was damaged or the source is not sending valid SIA DC-09.
- `account_invalid` means `AJAXBRIDGE_ACCOUNT` does not match the frame.
- `decrypt_invalid` means encryption settings do not match.

Wrong device names:

- Edit `data/devices.json`.
- Keep `account` and `zone` stable.
- Restart AjaxBridge so MQTT discovery is republished with the corrected metadata.
