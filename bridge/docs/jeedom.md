# Jeedom

Jeedom support is optional. It adds a second input source through Jeedom MQTT Manager so AjaxBridge can capture richer Ajax device data and safe device controls that are not available through SIA.

External references:

- Jeedom installation: https://doc.jeedom.com/en_US/installation/index.html
- Jeedom command-line install: https://doc.jeedom.com/en_US/installation/cli
- MQTT Manager plugin: https://doc.jeedom.com/en_US/plugins/programming/mqtt2/
- Ajax System plugin: https://doc.jeedom.com/en_US/plugins/security/ajaxSystem/

## Details: What Jeedom Is In This Bridge

Jeedom is used as a secondary Ajax data mirror:

- MQTT Manager publishes Jeedom command events and eqLogic discovery.
- AjaxBridge reads those MQTT messages.
- AjaxBridge normalizes French/English Jeedom labels into stable English names and metrics.
- AjaxBridge publishes one normalized state topic per Jeedom device.
- Linked Jeedom entities attach to the same Home Assistant device as the SIA zone.
- Allowlisted Jeedom actions can be exposed as Home Assistant switches and HTTP controls.

SIA remains authoritative. Jeedom is not used to decide armed/disarmed, alarm, tamper, trouble, or security history when SIA has a value for the same physical device.

## Install Jeedom

Use the official Jeedom installation guide for your hardware. Jeedom recommends official Jeedom boxes for full compatibility, and also supports Debian-based installations.

For an advanced Debian server install, the official command-line guide uses this basic flow:

```bash
wget https://raw.githubusercontent.com/jeedom/core/master/install/install.sh
chmod +x install.sh
sudo ./install.sh
```

After Jeedom is installed:

1. Open the Jeedom web UI.
2. Finish first-run setup.
3. Update Jeedom core.
4. Install plugins from Jeedom Market.
5. Keep Jeedom reachable from Ajax and from the MQTT broker network.

## Install Jeedom Plugins

Install and activate these Jeedom plugins:

| Plugin | Purpose |
| --- | --- |
| MQTT Manager | Provides the MQTT broker connection, publishes Jeedom events, publishes eqLogic discovery, and accepts `jeedom/cmd/set/<cmd_id>` action topics. |
| Ajax System | Connects Jeedom to the Ajax account and creates Ajax equipment inside Jeedom. |

### MQTT Manager Plugin

In MQTT Manager:

1. Choose broker mode.
   - Local broker installs/configures Mosquitto on the Jeedom host.
   - Remote broker connects Jeedom to an existing broker, for example `mqtt://192.168.1.10:1883`.
2. Configure authentication.
3. Keep or set the Jeedom root topic. AjaxBridge defaults to `jeedom`.
4. Enable transmitting Jeedom command events over MQTT.
5. Configure the publication template as JSON so command events are parseable by AjaxBridge:

```json
{"value":"#value#","humanName":"#humanName#","unite":"#unit#","name":"#name#","type":"#type#","subtype":"#subtype#"}
```

6. Enable or run eqLogic discovery so AjaxBridge receives `jeedom/discovery/eqLogic/#`.

The bridge expects these MQTT Manager topic patterns:

```text
jeedom/cmd/event/#          # Jeedom info command events
jeedom/discovery/eqLogic/#  # Jeedom equipment and command discovery
jeedom/cmd/set/<cmd_id>     # Action execution topic used by controls
```

Do not subscribe AjaxBridge to broad topics like `jeedom/#` unless you know all payloads are JSON command events. Broad subscriptions can include status strings such as `online`, which produce JSON parse warnings.

### Ajax System Plugin

In the Ajax System plugin:

1. Connect using a normal Ajax user account. The official plugin docs say not to use a professional account for this plugin link.
2. For real-time updates, make Jeedom externally reachable over HTTPS with a valid certificate, as required by the plugin docs.
3. In the Ajax app, add the event-reporting user required by the Jeedom plugin docs: `ajax@jeedom.com`. The invitation can remain pending; the plugin docs describe that as normal.
4. Synchronize equipment in Jeedom.
5. Confirm Ajax devices appear as Jeedom equipment with info commands and, for controllable devices, action commands.
6. Send or refresh MQTT discovery from MQTT Manager after devices are visible.

## Configure AjaxBridge

Jeedom input requires MQTT. Minimal bridge environment:

```yaml
environment:
  AJAXBRIDGE_MQTT_BROKER: "tcp://homeassistant.local:1883"
  AJAXBRIDGE_MQTT_USERNAME: "mqtt-user"
  AJAXBRIDGE_MQTT_PASSWORD: "mqtt-password"
  AJAXBRIDGE_MQTT_DISCOVERY: "true"

  AJAXBRIDGE_JEEDOM_ENABLED: "true"
  AJAXBRIDGE_JEEDOM_EVENT_TOPIC: "jeedom/cmd/event/#"
  AJAXBRIDGE_JEEDOM_DISCOVERY_TOPIC: "jeedom/discovery/eqLogic/#"
  AJAXBRIDGE_JEEDOM_STATE_TOPIC_PREFIX: "ajaxbridge/jeedom"
  AJAXBRIDGE_JEEDOM_DISCOVERY: "true"
  AJAXBRIDGE_JEEDOM_EMPTY_VALUE_POLICY: "keep_last"
  AJAXBRIDGE_JEEDOM_STORE_PATH: "/data/jeedom.json"
  # Optional debugging only. Leave unset in production.
  # AJAXBRIDGE_JEEDOM_SAMPLE_DIR: "/data/tmp-jeedom"
  AJAXBRIDGE_JEEDOM_DISCOVER_UNLINKED: "false"
  AJAXBRIDGE_JEEDOM_ACCOUNT_NAMES: "House"
  AJAXBRIDGE_JEEDOM_CONTROLS_ENABLED: "false"
  AJAXBRIDGE_JEEDOM_SET_TOPIC_PREFIX: "jeedom/cmd/set"
  AJAXBRIDGE_JEEDOM_CONTROL_PAYLOAD: "1"
```

Variables:

| Variable | Default | Description |
| --- | --- | --- |
| `AJAXBRIDGE_JEEDOM_ENABLED` | `false` | Enables Jeedom MQTT input. |
| `AJAXBRIDGE_JEEDOM_EVENT_TOPIC` | `jeedom/cmd/event/#` | Jeedom event subscription. |
| `AJAXBRIDGE_JEEDOM_DISCOVERY_TOPIC` | `jeedom/discovery/eqLogic/#` | Jeedom eqLogic discovery subscription. |
| `AJAXBRIDGE_JEEDOM_STATE_TOPIC_PREFIX` | `ajaxbridge/jeedom` | Normalized Jeedom state topic prefix. |
| `AJAXBRIDGE_JEEDOM_DISCOVERY` | same as MQTT discovery | Publishes HA discovery for Jeedom values. |
| `AJAXBRIDGE_JEEDOM_EMPTY_VALUE_POLICY` | `keep_last` | `keep_last` or `unknown`. |
| `AJAXBRIDGE_JEEDOM_RETAIN_STATE` | `true` | Retain normalized Jeedom state messages. |
| `AJAXBRIDGE_JEEDOM_RETAIN_DISCOVERY` | `true` | Retain Jeedom HA discovery configs. |
| `AJAXBRIDGE_JEEDOM_STORE_PATH` | `data/jeedom.json` | Persists discovered Jeedom commands and last values so AjaxBridge can republish HA discovery/state after restart without forcing Jeedom MQTT discovery again. Empty keeps Jeedom data in memory only. |
| `AJAXBRIDGE_JEEDOM_SAMPLE_DIR` | empty | Raw Jeedom MQTT sample capture directory. Empty disables capture. Leave empty in production. |
| `AJAXBRIDGE_JEEDOM_DISCOVER_UNLINKED` | `false` | Publish HA discovery for Jeedom devices not linked to SIA catalog devices. |
| `AJAXBRIDGE_JEEDOM_ACCOUNT_NAMES` | empty | Jeedom names that represent the SIA account/hub device. |
| `AJAXBRIDGE_JEEDOM_CONTROLS_ENABLED` | `false` | Enable allowlisted toggles and relay impulse controls. |
| `AJAXBRIDGE_JEEDOM_SET_TOPIC_PREFIX` | `jeedom/cmd/set` | Jeedom action topic prefix. |
| `AJAXBRIDGE_JEEDOM_CONTROL_PAYLOAD` | `1` | Payload sent to Jeedom action topics. |

## Restart Behavior

AjaxBridge keeps the normalized Jeedom mirror in memory while it runs and, by default, persists that mirror to `data/jeedom.json`. The file contains discovered Jeedom devices, command metadata, action metadata, and the latest normalized values.

On startup, AjaxBridge loads this cache before subscribing to Jeedom MQTT topics. On every MQTT broker connect or reconnect, it republishes:

- retained Ajax/SIA account and zone discovery/state
- retained Jeedom Home Assistant discovery configs
- retained Jeedom state payloads, including measurements such as temperature, power, current, voltage, battery, humidity, and energy
- the last known state of action-only WallSwitch controls, even though those devices do not expose an `Etat`/`realState` command

That means a normal bridge restart should not require forcing Jeedom MQTT Manager to resend eqLogic discovery. Force Jeedom discovery only when `data/jeedom.json` is missing, empty, stale, or you have changed Jeedom equipment/commands and want the bridge to learn the new command list immediately.

An action-only WallSwitch starts as `unknown` when the cache has never learned its physical state. AjaxBridge learns and persists state from successful bridge/MQTT ON/OFF controls and recognized Ajax event codes. A positive power/current measurement can safely seed `ON`; a zero measurement does not prove `OFF` because an enabled relay may have no active load.

## Expected Jeedom Event Payload

MQTT Manager event topic:

```text
jeedom/cmd/event/56
```

Payload:

```json
{
  "value": "123.4",
  "humanName": "[None][Server power][Puissance]",
  "unite": "W",
  "name": "Puissance",
  "type": "info",
  "subtype": "numeric"
}
```

`humanName` is parsed as:

```text
[object][device][command]
```

The command id is taken from the topic suffix.

## Normalized Jeedom State

AjaxBridge publishes one retained JSON payload per Jeedom device:

```text
ajaxbridge/jeedom/devices/<device_slug>/state
```

Example:

```json
{
  "source": "jeedom",
  "object": "None",
  "device": "Server power",
  "device_slug": "sia_a0f80d_zone_8",
  "jeedom_id": "7",
  "jeedom_device_type": "WallSwitch",
  "last_update": "2026-05-05T13:11:21+03:00",
  "power_w": 120.5,
  "current_a": 0.53,
  "voltage_v": 228,
  "online": true,
  "signal_level": "STRONG",
  "raw_commands": {
    "56": {
      "command_id": "56",
      "name": "Power",
      "raw_name": "Puissance",
      "metric": "power_w",
      "component": "sensor",
      "topic": "jeedom/cmd/event/56",
      "type": "info",
      "subtype": "numeric",
      "unit": "W",
      "device_class": "power",
      "state_class": "measurement",
      "value": 120.5
    }
  },
  "actions": {
    "on": {
      "action": "on",
      "command_id": "58",
      "name": "On",
      "raw_name": "On",
      "allowed": true
    },
    "off": {
      "action": "off",
      "command_id": "59",
      "name": "Off",
      "raw_name": "Off",
      "allowed": true
    }
  }
}
```

The full `/state` payload is kept unchanged for MQTT consumers and debugging. Home Assistant entities no longer copy it wholesale into every entity. Their `json_attributes_topic` points to a compact retained metadata payload:

```text
ajaxbridge/jeedom/devices/<device_slug>/attributes
```

```json
{
  "source": "jeedom",
  "object": "None",
  "device": "Server power",
  "device_slug": "sia_a0f80d_zone_8",
  "jeedom_id": "7",
  "jeedom_device_type": "WallSwitch"
}
```

This preserves card and automation fields used for stable device identity and classification while excluding volatile `raw_commands`, `actions`, values, and timestamps from Home Assistant entity attributes. Full command/action metadata remains available from the HTTP endpoints and `/state`.

## French To English Translation

Known Jeedom command labels are translated to English before being exposed to Home Assistant discovery, Prometheus labels, and debug JSON. The original label remains in `raw_name`.

| Jeedom label | English name | Metric |
| --- | --- | --- |
| `Puissance` | `Power` | `power_w` |
| `Consommation` | `Energy` | `energy_kwh` |
| `Courant` | `Current` | `current_a` |
| `Tension`, `Voltage` | `Voltage` | `voltage_v` |
| `Temperature`, `Temp` | `Temperature` | `temperature_c` |
| `Batterie` | `Battery` | `battery_percent` |
| `Etat de la batterie` | `Battery state` | `battery_state` |
| numeric `Signal`, `RSSI` | `Signal` | `signal_dbm` |
| string `Signal` | `Signal` | `signal_level` |
| `Humidite` | `Humidity` | `humidity_percent` |
| `CO2`, `Carbon dioxide` | `Carbon dioxide` | `co2_ppm` |
| `Etat` | `State` | `state` |
| `En ligne` | `Online` | `online` |
| `Trafique`, `Tamper`, `Sabotage` | `Tamper` | `tamper` |
| `Alimentation secteur` | `External power` | `external_power` |
| `Donnees cellulaires actives` | `Cellular data active` | `cellular_data_active` |
| `Type reseau GSM` | `GSM network type` | `gsm_network_type` |
| `Ouverture` | `Opening` | `opening` |
| `Porte` | `Door` | `door` |
| `Fuite` | `Leak` | `leak` |
| `Source evenement` | `Event source` | `event_source` |
| `Evenement` | `Event` | `event` |
| `Code evenement` | `Event code` | `event_code` |
| `Nombre de defauts` | `Issue count` | `issue_count` |
| `Version du firmware` | `Firmware version` | `firmware_version` |
| `Mode`, `Operating mode` | `Operating mode` | `operating_mode` |
| `Operating state`, `Etat de fonctionnement` | `Operating state` | `operating_state` |
| `Etat du controle de batterie` | `Battery check status` | `battery_check_status` |
| `Derniere mise a jour` | `Last update` | `device_last_update` |
| `Etat de la vanne` | `Valve position` | `valve_position` |
| `Alarme fumee` | `Smoke alarm` | `smoke_alarm` |
| `Alarme fumee critique` | `Critical smoke alarm` | `critical_smoke_alarm` |
| `Alarme temperature` | `Heat alarm` | `heat_alarm` |
| `Alarme hausse rapide de temperature` | `Rapid temperature rise alarm` | `rapid_temperature_rise_alarm` |
| `Alarme CO` | `Carbon monoxide alarm` | `carbon_monoxide_alarm` |
| `Alarme CO critique` | `Critical carbon monoxide alarm` | `critical_carbon_monoxide_alarm` |

ReX, ReX 2, Superior ReX, MultiTransmitter, MultiTransmitter Fibra, and Superior MultiTransmitter diagnostics are also normalized into stable radio/photo/Ethernet connectivity, antenna, charging, power-fault, undervoltage, Fibra-test, data-channel, detector-supply, and charger-fault metrics. Numeric photo/data-channel signal values use Home Assistant `signal_strength`/`measurement` metadata; textual quality values remain diagnostic strings.

When the Jeedom plugin supplies stable `logicalId` or `generic_type` metadata, AjaxBridge gives it precedence over localized display labels. `TEMPERATURE`, `HUMIDITY`, `CO2`, and `BATTERY` contracts therefore remain stable even if a command is renamed. Existing command IDs keep their discovered canonical mapping on later value-only events.

`device_last_update` accepts Unix seconds, milliseconds, microseconds, or nanoseconds and is exposed as a UTC RFC3339 Home Assistant timestamp. It is intentionally distinct from the state envelope's bridge-generated `last_update`. Fire enum values ending in `_DETECTED`/`_NOT_DETECTED` become booleans. WaterStop keeps the exact `valve_position` text and derives the existing control `state` only for unambiguous open/closed values.

Known French string values are also normalized:

| Raw value | Normalized value |
| --- | --- |
| `ARME` | `ARMED` |
| `DESARME` | `DISARMED` |
| `MODE_NUIT` | `NIGHT_MODE` |
| `FORT` | `STRONG` |
| `FAIBLE` | `WEAK` |
| `MOYEN` | `MEDIUM` |
| `CHARGE` | `CHARGED` |
| `DECHARGE` | `DISCHARGED` |

Relay voltage values from the Jeedom Ajax plugin are divided by 10 before publishing, because the plugin reports those relay supply voltages in tenths of volts.

## Duplicate Prevention With SIA

Jeedom and SIA often describe the same physical Ajax device. To avoid duplicate Home Assistant devices, link Jeedom to SIA in `data/devices.json`:

```json
{
  "account": "A0F80D",
  "zone": "8",
  "name": "Server power",
  "room": "Boiler room",
  "kind": "WallSwitch",
  "events": ["power", "connectivity", "hardware", "firmware"],
  "jeedom_names": ["Server power", "Serverna"],
  "jeedom_command_ids": ["52", "53", "54", "55", "56", "57", "58", "59"]
}
```

Linking rules:

- `jeedom_command_ids` are the strongest match.
- `jeedom_names` match repaired/transliterated Jeedom device names.
- `AJAXBRIDGE_JEEDOM_ACCOUNT_NAMES` links hub/system Jeedom equipment to the SIA account device.
- Linked Jeedom devices reuse the SIA HA identifier `ajaxbridge_<account>_zone_<zone>`.
- Unlinked Jeedom devices do not publish Home Assistant discovery by default because `AJAXBRIDGE_JEEDOM_DISCOVER_UNLINKED=false`.

## Raw Sample Capture

When `AJAXBRIDGE_JEEDOM_SAMPLE_DIR` is set, every received Jeedom MQTT message is written as a JSON envelope. Leave it unset in production so the bridge does not write an unbounded stream of sample files.

Example file shape:

```json
{
  "topic": "jeedom/cmd/event/56",
  "command_id": "56",
  "received_at": "2026-05-05T13:11:21+03:00",
  "payload": {
    "value": "123.4",
    "humanName": "[None][Server power][Puissance]",
    "unite": "W",
    "name": "Puissance",
    "type": "info",
    "subtype": "numeric"
  }
}
```

Use this while refining catalog links and adding support for new Jeedom command labels.

## Controls

Controls are disabled by default:

```yaml
AJAXBRIDGE_JEEDOM_CONTROLS_ENABLED: "false"
```

When enabled, AjaxBridge reads Jeedom eqLogic discovery, registers action command ids, and exposes only allowlisted controls.

Allowed control types:

- `Socket`, `WallSwitch`, `LightSwitch`, `Outlet`, and `WaterStop`: Home Assistant switch toggles when both `on` and `off` actions exist.
- `Relay`: Home Assistant button impulse. AjaxBridge uses a discovered `impulse` action when available, otherwise it uses the relay `on` action as the pulse.

Blocked by default:

- hub/security actions such as arm, disarm, night mode, panic, and fire-detector mute
- any device type not on the allowlist

Home Assistant switch/button command topic:

```text
ajaxbridge/jeedom/devices/<device_slug>/set
```

Payloads:

```text
ON
OFF
IMPULSE
```

AjaxBridge resolves the Jeedom action command id and publishes:

```text
jeedom/cmd/set/<command_id>
```

The Jeedom command payload is `1` by default. Override it with `AJAXBRIDGE_JEEDOM_CONTROL_PAYLOAD` if your MQTT Manager action commands expect a different payload.

AjaxBridge also subscribes to this same `jeedom/cmd/set/#` prefix. That gives best-effort visibility into commands issued by other MQTT clients when the command id is known from eqLogic discovery. Commands issued directly inside Jeedom may not be visible as command topics, so physical state-change detection still depends on Jeedom publishing related state or event information.

For WallSwitch equipment without a state info command, successful ON/OFF commands observed on this path update the persisted bridge state. Physical/direct Jeedom changes are learned from recognized Ajax event-code updates. If neither signal has ever been observed, the state remains `unknown` rather than being assumed `OFF`.

HTTP control:

```bash
curl -X POST http://localhost:8080/jeedom/devices/server_power/control \
  -H "Content-Type: application/json" \
  -d '{"action":"on"}'
```

Every attempt is stored in:

```text
GET /jeedom/control-audit?limit=100
```

## State And Trigger Notifications

AjaxBridge can notify about Jeedom metrics and controls through [Notifications](./notifications.md).

For WallSwitch/Outlet on/off:

- AjaxBridge can always notify when the command is issued through AjaxBridge or Home Assistant using `control`, `control_on`, or `control_off` rules.
- AjaxBridge can learn physical/external WallSwitch changes from recognized Ajax event-code updates even when the equipment has no binary `state` info command.
- Other equipment still depends on Jeedom publishing a binary `state` info command for authoritative physical state changes.
- Use `state` plus `changed_to_on` or `changed_to_off` rules for those state changes.

For relay trigger notifications:

- Use `control` rules for bridge-issued relay impulse commands.
- Use `state` change rules for physical or Jeedom-originated relay changes, if Jeedom publishes the state.

## Debug Endpoints

```text
GET /jeedom/devices
GET /jeedom/devices/<device_slug>
GET /jeedom/commands
GET /jeedom/actions
GET /jeedom/control-audit?limit=100
POST /jeedom/devices/<device_slug>/control
```

`/state` remains the SIA truth. `/jeedom/*` is the Jeedom mirror/control layer.

## Troubleshooting

Parse warnings for `jeedom/state` or `invalid character 'o'`:

- Do not subscribe the bridge to a broad status topic.
- Use `AJAXBRIDGE_JEEDOM_EVENT_TOPIC=jeedom/cmd/event/#`.
- Use `AJAXBRIDGE_JEEDOM_DISCOVERY_TOPIC=jeedom/discovery/eqLogic/#`.

Jeedom devices do not appear in Home Assistant:

- Confirm SIA catalog links exist in `jeedom_names` or `jeedom_command_ids`.
- Keep `AJAXBRIDGE_JEEDOM_DISCOVER_UNLINKED=false` unless you intentionally want Jeedom-only devices.
- Check `/jeedom/devices` and `/jeedom/commands`.
- Confirm MQTT discovery is enabled.

Controls do not work:

- Set `AJAXBRIDGE_JEEDOM_CONTROLS_ENABLED=true`.
- Confirm `/jeedom/actions` shows `allowed: true` for the target device.
- Confirm the device type is allowlisted.
- Check `/jeedom/control-audit?limit=100`.
- Confirm MQTT Manager accepts `jeedom/cmd/set/<command_id>` with payload `1`, or set `AJAXBRIDGE_JEEDOM_CONTROL_PAYLOAD` to the payload your Jeedom action expects.
