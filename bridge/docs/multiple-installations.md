# Multiple Installations

Run one AjaxBridge per Jeedom installation. Each bridge has a stable source ID, a separate Jeedom MQTT Manager root, and its own catalog, cache, and notification files. The bridges can share one MQTT broker, Home Assistant instance, and Prometheus server.

`AJAXBRIDGE_SOURCE_ID=apartment` namespaces the new installation's MQTT discovery and Home Assistant identities, includes `source_id` in entity metadata, and adds a constant `source_id="apartment"` label to its exported Prometheus metrics. Ajax account/zone numbers, device slugs, and raw Jeedom command IDs remain local values; they can overlap between installations.

The source ID must contain 1–64 characters matching `[a-z0-9][a-z0-9_-]*`, such as `apartment` or `house_2`. Choose it before the first discovery and keep it stable. Empty preserves the existing legacy MQTT/HA identities and metric labels. Adding or changing the source ID of an existing installation creates different Home Assistant identities and topics; it requires a separate migration of retained discovery, automations, cards, and history references.

## Namespace Defaults

With `AJAXBRIDGE_SOURCE_ID=apartment`, the following defaults apply:

| Setting | Empty source ID | `apartment` source ID |
| --- | --- | --- |
| `AJAXBRIDGE_MQTT_CLIENT_ID` | `ajaxbridge` | `ajaxbridge-apartment` |
| `AJAXBRIDGE_MQTT_TOPIC_PREFIX` | `ajaxbridge` | `ajaxbridge/apartment` |
| `AJAXBRIDGE_JEEDOM_STATE_TOPIC_PREFIX` | `ajaxbridge/jeedom` | `ajaxbridge/jeedom/apartment` |
| `AJAXBRIDGE_JEEDOM_EVENT_TOPIC` | `jeedom/cmd/event/#` | `jeedom_apartment/cmd/event/#` |
| `AJAXBRIDGE_JEEDOM_DISCOVERY_TOPIC` | `jeedom/discovery/eqLogic/#` | `jeedom_apartment/discovery/eqLogic/#` |
| `AJAXBRIDGE_JEEDOM_SET_TOPIC_PREFIX` | `jeedom/cmd/set` | `jeedom_apartment/cmd/set` |
| `AJAXBRIDGE_DEVICES_PATH` | `data/devices.json` | `data/apartment/devices.json` |
| `AJAXBRIDGE_JEEDOM_STORE_PATH` | `data/jeedom.json` | `data/apartment/jeedom.json` |
| `AJAXBRIDGE_NOTIFICATIONS_PATH` | `data/notifications.json` | `data/apartment/notifications.json` |

Outgoing MQTT client/state settings are base values: a configured source ID is appended even to explicit bases, so copying `AJAXBRIDGE_MQTT_TOPIC_PREFIX=ajaxbridge` still produces `ajaxbridge/apartment`. The Jeedom state base receives the same `/{source_id}` suffix, producing `ajaxbridge/jeedom/apartment` by default. Do not include the source suffix in these base values yourself. Jeedom event/discovery/set topics are external inputs and destinations: explicit overrides remain exactly as configured and must match that Jeedom's MQTT Manager root.

Catalog, cache, and notification file settings are also bases. The bridge inserts the source directory before each filename: `/data/devices.json` becomes `/data/apartment/devices.json`, and `/private/custom-cache.json` becomes `/private/apartment/custom-cache.json`. An enabled Jeedom sample directory receives the source suffix, for example `/data/tmp-jeedom/apartment`. Empty paths keep their existing disabled/in-memory behavior. Namespace resolution happens after environment and CLI parsing, so explicit flags receive the same isolation. Keep base paths unscoped in your configuration.

The Home Assistant discovery prefix can remain `homeassistant`; source-specific discovery nodes, entity unique IDs, and device identifiers provide isolation within it. MQTT availability and bridge control topics are also separate. Check copied Jeedom input/control overrides so they do not subscribe to or execute actions on the existing house's Jeedom.

Set the apartment Jeedom MQTT Manager plugin's `root_topic` to `jeedom_apartment`, then publish its command events and eqLogic discovery. A bridge subscription cannot rename the Jeedom topics: the producing Jeedom instance must use the same root. Give the Jeedom MQTT clients distinct client IDs too. Keep subscriptions narrow and scoped to one source.

Two Jeedom installations must not publish their default `jeedom/cmd/event/<id>` or discovery topics into the same root on a shared broker. Command IDs and equipment IDs are unique only within one Jeedom database. A single bridge consuming multiple Jeedom installations is not supported: its store and controls use raw local command IDs. Setting the second Jeedom's starting ID to `10000` is optional administration, not isolation; it does not separate availability, controls, discovery, names, or future collisions.

## Existing House Plus New Apartment

Keep the existing house's source ID and data path unchanged. Configure the new apartment with its namespace before connecting it to Home Assistant. This private Compose example shows the arrangement; substitute account numbers, broker settings, and your chosen host ports:

```yaml
services:
  ajaxbridge-house:
    image: ${AJAXBRIDGE_IMAGE:-rcooler/ajax-bridge:latest}
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "8099:8099"
    environment:
      AJAXBRIDGE_SOURCE_ID: ""
      AJAXBRIDGE_ACCOUNT: "0001"
      AJAXBRIDGE_MQTT_BROKER: "${MQTT_BROKER:?Set MQTT_BROKER}"
      AJAXBRIDGE_MQTT_USERNAME: "${MQTT_USERNAME:-}"
      AJAXBRIDGE_MQTT_PASSWORD: "${MQTT_PASSWORD:-}"
      AJAXBRIDGE_JEEDOM_ENABLED: "true"
      AJAXBRIDGE_DEVICES_PATH: "/data/devices.json"
      AJAXBRIDGE_JEEDOM_STORE_PATH: "/data/jeedom.json"
      AJAXBRIDGE_NOTIFICATIONS_PATH: "/data/notifications.json"
    volumes:
      - ./data:/data

  ajaxbridge-apartment:
    image: ${AJAXBRIDGE_IMAGE:-rcooler/ajax-bridge:latest}
    restart: unless-stopped
    ports:
      - "8081:8080"
      - "8100:8099"
    environment:
      AJAXBRIDGE_SOURCE_ID: "apartment"
      AJAXBRIDGE_ACCOUNT: "0001"
      AJAXBRIDGE_MQTT_BROKER: "${MQTT_BROKER:?Set MQTT_BROKER}"
      AJAXBRIDGE_MQTT_USERNAME: "${MQTT_USERNAME:-}"
      AJAXBRIDGE_MQTT_PASSWORD: "${MQTT_PASSWORD:-}"
      AJAXBRIDGE_JEEDOM_ENABLED: "true"
      AJAXBRIDGE_DEVICES_PATH: "/data/devices.json"
      AJAXBRIDGE_JEEDOM_STORE_PATH: "/data/jeedom.json"
      AJAXBRIDGE_NOTIFICATIONS_PATH: "/data/notifications.json"
    volumes:
      - ./data-apartment:/data
```

Both examples intentionally use account `0001`; the namespace separates the installations even when account, zone, names, and Jeedom IDs overlap. The house reads `./data/devices.json` and `./data/jeedom.json`; the apartment reads `./data-apartment/apartment/devices.json` and `./data-apartment/apartment/jeedom.json`. Notifications use the same per-source directory. Separate volume mounts are recommended even though the source directory also prevents accidentally sharing the same base files.

Configure each Ajax hub to send SIA to its bridge's host port. Each `/admin` edits that bridge's own catalog and links only its Jeedom commands. A namespaced admin panel shows its source badge, `/api/admin/bootstrap` includes `source_id`, and HTTP responses include `X-AjaxBridge-Source-ID`. Preserve the hardening and networking from the [baseline Compose file](../docker-compose.yml) in your production configuration; the example focuses on instance separation.

For two new installations, assign `house` and `apartment` before either publishes discovery, and set MQTT Manager roots to `jeedom_house` and `jeedom_apartment`. Each needs its own data directory. Do not share or copy the existing house's populated `devices.json` or `jeedom.json` into the apartment: catalogs contain local zones and command links, and caches contain observations and command owners.

## Home Assistant Cards

Set the apartment card's `source_id` to select its installation. Area filters can further limit rooms within that installation:

```yaml
type: custom:ajaxbridge-detailed-card
source_id: apartment
area_ids:
  - apartment_hall
  - apartment_kitchen
```

Use the actual Home Assistant area IDs. `area_ids` is optional and complements source filtering; area names and room labels do not establish source identity. Apply `source_id: apartment` to `custom:ajaxbridge-chips-card` too. A nonempty source filter excludes Ajax devices with foreign or missing source metadata. Omitting or leaving `source_id` empty selects only legacy Ajax devices, so adding apartment discovery does not add apartment Ajax devices to the existing house card.

For ancillary cameras, climate, and other non-Ajax data without source metadata, configure `area_ids` to scope the installation. Legacy cards preserve their existing ancillary behavior when no areas are configured. A namespaced card includes ancillary data only when it has matching source metadata or belongs to explicitly selected areas. Keep house grid inputs, controls, and other explicitly configured entity lists pointed at house entities, and apartment lists pointed at apartment entities. There is no implicit combined-installation view.

## Prometheus

The default `job` and `instance` labels distinguish scrape endpoints. A stable `source_id` lets queries distinguish installations independently of endpoint names or overlapping command IDs. A namespaced bridge exports it directly. For a legacy house, add `source_id: house` to its scrape target without changing the house's MQTT/HA configuration:

```yaml
scrape_configs:
  - job_name: ajaxbridge
    honor_labels: true
    static_configs:
      - targets: ['ajaxbridge-house:8080']
        labels:
          source_id: house
      - targets: ['ajaxbridge-apartment:8080']
        labels:
          source_id: apartment
```

Use addresses reachable from Prometheus. The apartment target label must agree with the bridge's configured source ID. `honor_labels: true` preserves the exporter label; target labels also mark generated `up` and scrape series. Keep the default endpoint-specific `instance` and use source filters in dashboards and alerts. [Prometheus documents its automatic job and instance labels here](https://prometheus.io/docs/concepts/jobs_instances/).

Examples scoped to the apartment:

```promql
ajax_jeedom_command_value{job="ajaxbridge",source_id="apartment",command_id="162"}

ajax_zone_alarm_active{job="ajaxbridge",source_id="apartment",account="0001"} == 1

up{job="ajaxbridge",source_id="apartment"} == 0
```

When comparing installations, retain the source label in aggregations:

```promql
sum by (source_id) (ajax_jeedom_device_power_watts{job="ajaxbridge"})

histogram_quantile(
  0.95,
  sum by (source_id, instance, le, target) (
    rate(ajax_sia_forward_duration_seconds_bucket{job="ajaxbridge"}[5m])
  )
)
```

The total-power example sums only the exported device readings; exclude overlapping meters where needed to avoid counting the same load twice. Preserve `instance` as well when multiple bridges represent the same source. Historical samples retain their original labels: adding a target label now does not relabel existing history. Query older house history using its original `job`/`instance` selectors until it ages out or a separate reviewed historical migration is performed.

## Verification Before Use

Verify both `/admin` pages show their own catalog and Jeedom command lists, the apartment source badge is visible, and its effective paths contain the `apartment` directory. Inspect each `/metrics` endpoint. Apartment metrics must include `source_id="apartment"`; the legacy house keeps its original exporter labels. In Home Assistant, confirm same-number commands and zones produce distinct device/entity identities and that each card shows only its configured source. Changing apartment state or availability must affect only the apartment entities and series. Verify a control's MQTT destination before enabling controls for a new installation.
