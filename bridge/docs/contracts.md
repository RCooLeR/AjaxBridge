# Runtime Contracts

This project keeps SIA as the authoritative security source and uses Jeedom as an optional source for extra metrics and safe controls. The surfaces below are treated as compatibility contracts: change them only with tests and migration notes.

The identity/topic examples below describe the legacy contract with empty `AJAXBRIDGE_SOURCE_ID`. A nonempty source ID creates a separate installation namespace for MQTT, HA discovery/entity/device identities, and a `source_id` Prometheus label. Outgoing client/topic environment values are bases to which the source suffix is appended; explicitly configured external Jeedom input/control topics remain unchanged. Local Ajax accounts/zones and raw Jeedom IDs are never renumbered. See [multiple installations](./multiple-installations.md) before introducing or changing a source ID on a populated installation.

## HTTP JSON

- `GET /state` returns the current SIA-derived snapshot with `accounts` and `zones`.
- `GET /devices` returns the configured device catalog.
- `GET /jeedom/devices`, `GET /jeedom/commands`, and `GET /jeedom/actions` expose the Jeedom mirror when Jeedom is enabled.
- `GET /api/admin/bootstrap` combines current state, catalog, Jeedom mirror data, notifications, and file paths for the admin UI.

JSON field names are snake_case and should remain stable because the admin UI, Home Assistant cards, scripts, and troubleshooting workflows depend on them.

A namespaced bridge identifies HTTP responses with `X-AjaxBridge-Source-ID`; `/api/admin/bootstrap` also includes `source_id`, and `/admin` displays a source badge. Effective catalog/cache/notification paths insert the source directory before the filename; debug sample directories append the source. Resolution follows environment/CLI parsing, including explicit base overrides. Empty source ID preserves the legacy response and path behavior.

## MQTT State

Default state topics use the `ajaxbridge` prefix:

- `ajaxbridge/accounts/{account}/state`
- `ajaxbridge/accounts/{account}/attributes`
- `ajaxbridge/accounts/{account}/zones/{zone}/state`
- `ajaxbridge/accounts/{account}/zones/{zone}/attributes`
- `ajaxbridge/jeedom/devices/{device_slug}/state`
- `ajaxbridge/jeedom/devices/{device_slug}/attributes`
- `ajaxbridge/status`

State messages are retained when MQTT retain is enabled. Attribute messages are always retained because they contain only stable device metadata used by Home Assistant entities. Dynamic state payloads keep their existing fields and topics for direct MQTT consumers, but Home Assistant discovery does not import those payloads wholesale as entity attributes.

On MQTT reconnect, AjaxBridge resets its publish caches and republishes retained SIA state plus cached Jeedom discovery/state and metadata so Home Assistant can recover without resubmitting Jeedom discovery.

## Home Assistant Discovery

Default discovery topics use:

- `homeassistant/{component}/ajaxbridge/{object_id}/config`
- `homeassistant/{component}/ajaxbridge/jeedom_cmd_{command_id}/config`
- `homeassistant/{component}/ajaxbridge/jeedom_control_{device_slug}/config`

SIA entity unique IDs are based on the discovery node plus the SIA object id. Jeedom measurement unique IDs are based on the Jeedom command id. These IDs must not be renamed casually because Home Assistant uses them for entity identity.

Jeedom control switches keep `ajaxbridge_jeedom_control_{device_slug}` as their unique ID and `ajaxbridge/jeedom/devices/{device_slug}/set` as their command topic. Their state comes from the retained device state topic even when Jeedom exposes only ON/OFF actions. An absent state is published to Home Assistant as `unknown`, not `OFF`.

When a Jeedom device is linked to SIA, Jeedom metrics may attach to the SIA Home Assistant device. Jeedom security/status entities that duplicate SIA-owned state are published as empty retained discovery payloads to remove stale duplicates.

## Frontend Card Inputs

The Home Assistant cards consume Home Assistant device/entity registries and state objects. Ajax devices are recognized primarily by these stable identifiers:

- SIA account devices: `ajaxbridge_account_{account}`
- SIA zone devices: `ajaxbridge_{account}_zone_{zone}`
- Jeedom devices: `ajaxbridge_jeedom_{device_slug}` unless linked to a SIA identifier

Card-facing models live in `ha-cards/src/models/dashboard.ts`; keep those TypeScript shapes in sync with any bridge output or Home Assistant discovery changes.

Home Assistant cards should use the dedicated entity states for dynamic values. Compact entity attributes retain stable identity and classification fields such as `account`, `zone`, `kind`, `device_slug`, and `jeedom_device_type`. Full Jeedom command/action details remain available through HTTP JSON and the unchanged MQTT `/state` payload.

Namespaced entity metadata includes `source_id`. Cards with a nonempty source filter select only matching Ajax devices and exclude missing/foreign source metadata. Omitting or leaving the filter empty selects legacy Ajax devices only. Account/area filters can further narrow the installation. Ancillary data in a namespaced card requires matching source metadata or explicitly selected areas; legacy ancillary behavior is preserved unless narrowed by areas.
