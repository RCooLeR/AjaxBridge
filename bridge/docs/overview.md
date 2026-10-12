# Overview

AjaxBridge is a small Go service for exposing Ajax security hub data to monitoring and home automation tools. It receives Ajax SIA DC-09 TCP events, maintains current account and zone state in memory, and publishes that state through HTTP JSON, Prometheus, MQTT, and Home Assistant MQTT Discovery.

The bridge has two input sources:

| Source | Required | Role |
| --- | --- | --- |
| SIA DC-09 | Yes | Authoritative security state from the Ajax hub. |
| Jeedom MQTT | No | Additional metrics, allowlisted toggles, and relay impulse controls from Jeedom Ajax equipment. |

SIA always has priority. If SIA and Jeedom report different security state for the same physical device, dashboards and integrations must use SIA. Jeedom is used for values SIA does not provide well, such as power, current, voltage, energy, battery, signal diagnostics, and safe outlet/relay/switch controls.

## Data Flow

1. Ajax sends SIA DC-09 frames to the bridge on TCP port `8099`.
2. AjaxBridge validates, decrypts if configured, parses, normalizes, and ACKs or NAKs the frame.
3. The in-memory state engine updates account and zone state.
4. HTTP JSON endpoints, Prometheus metrics, MQTT state, and Home Assistant discovery are updated.
5. If Jeedom input is enabled, Jeedom MQTT Manager events are parsed into a separate Jeedom mirror store.
6. When Jeedom devices are linked to SIA catalog entries, their Home Assistant entities are attached to the same HA device as the SIA zone.
7. If Jeedom exposes a linked security/status command that SIA already owns, AjaxBridge cleans the Jeedom HA discovery entry and keeps the SIA entity authoritative.

## Runtime Ports

| Port | Protocol | Purpose |
| --- | --- | --- |
| `8099` | TCP | SIA DC-09 receiver for Ajax. |
| `8080` | HTTP | Health, JSON state, debug endpoints, and Prometheus metrics. |

## Install And Run

From `bridge/`:

```bash
docker compose up -d
```

For production, keep host-specific values in an untracked override or private compose file. The tracked compose file is the safe baseline; production data belongs in `./data` so it is not committed but still survives restarts:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml up -d
```

Check the service:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/state
curl http://localhost:8080/metrics
```

Build locally:

```bash
go test ./...
go build ./cmd/ajaxbridge
./ajaxbridge --sia-addr :8099 --http-addr :8080 --account 0001
```

## Core Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `AJAXBRIDGE_SOURCE_ID` | empty | Stable installation namespace, for example `apartment`. Empty preserves legacy identities. See [multiple installations](./multiple-installations.md). |
| `AJAXBRIDGE_SIA_ADDR` | `:8099` | SIA TCP listen address. |
| `AJAXBRIDGE_HTTP_ADDR` | `:8080` | HTTP listen address. |
| `AJAXBRIDGE_ACCOUNT` | empty | Expected Ajax account/object number. Empty accepts all accounts. |
| `AJAXBRIDGE_ENCRYPTION_KEY` | empty | Optional SIA AES key. |
| `AJAXBRIDGE_STRICT_CRC` | `true` | Reject frames with invalid CRC. |
| `AJAXBRIDGE_DEVICES_PATH` | `data/devices.json` | Device catalog file base; a configured source ID inserts a source directory before the filename. |
| `AJAXBRIDGE_MQTT_BROKER` | empty | MQTT broker URL. Enables MQTT when set. |
| `AJAXBRIDGE_MQTT_CLIENT_ID` | `ajaxbridge` | MQTT client ID base; a configured source ID appends `-{source_id}`. |
| `AJAXBRIDGE_MQTT_TOPIC_PREFIX` | `ajaxbridge` | MQTT state prefix base; a configured source ID appends `/{source_id}`. |
| `AJAXBRIDGE_MQTT_DISCOVERY` | `true` | Publish Home Assistant discovery when MQTT is enabled. |
| `AJAXBRIDGE_JEEDOM_ENABLED` | `false` | Enable Jeedom MQTT input. |
| `AJAXBRIDGE_JEEDOM_STORE_PATH` | `data/jeedom.json` | Jeedom cache file base; a configured source ID inserts a source directory before the filename. Preserves discovered commands/values for republishing after restarts. |
| `AJAXBRIDGE_FORWARD_ADDR` | empty | Optional comma-separated raw SIA forward targets. |
| `AJAXBRIDGE_NOTIFICATIONS_PATH` | `data/notifications.json` | Notification config file base; a configured source ID inserts a source directory before the filename. |

Legacy `AJAX2PROM_*` variables are still accepted as compatibility aliases.

Configure a source ID before the first MQTT discovery for a new installation. One bridge consumes one Jeedom source. Multiple bridges require separate data directories and Jeedom MQTT Manager roots; separate Ajax account numbers alone do not isolate Jeedom command IDs. Explicit Jeedom input/control topics override their source-derived defaults. Outgoing MQTT client/state settings are bases and still receive the configured source suffix.

Data file paths are scoped after environment/CLI parsing too: `/data/devices.json` becomes `/data/apartment/devices.json` for source `apartment`. Jeedom cache and notification files follow the same rule; an enabled sample directory appends `/apartment`. Empty source preserves all legacy paths. The namespaced admin panel displays its source, and HTTP responses identify it through `X-AjaxBridge-Source-ID`.

## HTTP Endpoints

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Process health. |
| `GET /readyz` | Store readiness. |
| `GET /state` | Current SIA-derived account and zone state. |
| `GET /events?limit=100` | Latest in-memory SIA events. |
| `GET /devices` | Device catalog. |
| `GET /metrics` | Prometheus metrics. |
| `GET /admin` | Bootstrap admin panel. |
| `GET /api/admin/bootstrap` | Admin data for catalog, Jeedom, notifications, and current state. |
| `PUT /api/admin/devices` | Replace and persist the device catalog. |
| `PUT /api/admin/notifications` | Replace and persist notification rules/channels. |
| `GET /jeedom/devices` | Jeedom mirror devices, when Jeedom is enabled. |
| `GET /jeedom/devices/{slug}` | One Jeedom mirror device. |
| `GET /jeedom/commands` | Jeedom command metadata. |
| `GET /jeedom/actions` | Jeedom action metadata. |
| `GET /jeedom/control-audit?limit=100` | Recent Jeedom control attempts. |
| `POST /jeedom/devices/{slug}/control` | Jeedom toggle or relay impulse control, when enabled. |

## Data Files

| Path | Purpose |
| --- | --- |
| `data/devices.json` | Optional catalog with stable names, rooms, kinds, SIA zones, Jeedom aliases, and Jeedom command ids. |
| `data/jeedom.json` | Optional Jeedom command/value cache used to restore MQTT discovery and retained state after bridge restarts. |
| `data/notifications.json` | Notification channels and rules. |
| `tmp-jeedom/*.json` | Optional raw Jeedom MQTT sample envelopes when `AJAXBRIDGE_JEEDOM_SAMPLE_DIR` is set. Disabled by default for production. |

The table shows empty-source paths. Namespaced installations insert their source directory before each configured data filename and append it to an enabled debug sample directory. Use the effective paths shown in `/admin` when editing or backing up a namespaced installation.

## CI

Every pushed branch and pull request runs the `ci` GitHub Actions workflow:

- `bridge`: sets up Go from `bridge/go.mod`, then runs `go vet ./...` and `go test ./...` from the bridge module.
- `ha-cards`: installs with `npm ci`, then runs `npm run check` and `npm run build`.

Release packaging remains in the tag-only `release` workflow.

## Recommended Setup Order

For a second installation, first choose its source ID, private data directory, and Jeedom MQTT Manager root using the [multiple-installations guide](./multiple-installations.md).

1. Run AjaxBridge with SIA only and confirm `/state` receives Ajax events.
2. Edit `data/devices.json` with stable device names, rooms, kinds, and expected signals.
3. Enable MQTT and Home Assistant discovery.
4. Enable Jeedom only after SIA devices are stable.
5. Add `jeedom_names`, `jeedom_command_ids`, and `AJAXBRIDGE_JEEDOM_ACCOUNT_NAMES` to prevent duplicate HA devices.
6. Enable Jeedom controls only after `/jeedom/actions` shows the expected allowlisted actions.
7. Open `/admin` to edit catalog links and configure notifications.

## External References

- Ajax direct SIA DC-09 setup: https://support.ajax.systems/en/how-to-use-sia-for-cms-connection/
- Jeedom installation: https://doc.jeedom.com/en_US/installation/index.html
- Jeedom MQTT Manager: https://doc.jeedom.com/en_US/plugins/programming/mqtt2/
- Jeedom Ajax System plugin: https://doc.jeedom.com/en_US/plugins/security/ajaxSystem/
- Home Assistant MQTT integration: https://www.home-assistant.io/integrations/mqtt
