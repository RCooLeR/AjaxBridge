# AjaxBridge Lovelace UI

`ha-cards` builds the Home Assistant Lovelace cards for AjaxBridge.

It ships two custom cards:

- `custom:ajaxbridge-detailed-card`
- `custom:ajaxbridge-chips-card`

When loaded inside Home Assistant, the cards use live Home Assistant data from the area, device, and entity registries plus current entity state. The standalone Vite preview still works for local UI development.

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Development

Use Node.js 24.21.0 LTS and npm 12.2.0. The checked-in `.node-version` and `packageManager` fields record those versions.

```bash
npm ci
npm run dev
```

Useful commands:

- `npm run dev:lan`: expose the development server on the local network
- `npm run check`: TypeScript 7 project build check
- `npm run lint`: type-aware Oxlint checks, including React hooks and promise handling
- `npm run build`: production build into `dist/`
- `npm run verify`: run regression tests, lint, type checking, and the production build
- `npm run preview`: serve the built `dist/` output locally

## Build output

`npm run build` writes:

- `dist/ajaxbridge-lovelace.js`: the Home Assistant module that registers both cards
- `dist/assets/*`: JS chunks, CSS, icons, and room assets used by the module
- `dist/index.html`: standalone browser preview
- `dist/.vite/manifest.json`: Vite's entry, chunk, and asset manifest
- `dist/.vite/license.md`: generated third-party dependency license metadata

Copy the full `dist/` contents into Home Assistant, not only `ajaxbridge-lovelace.js`.

## Home Assistant install

1. Build and verify the UI:

```bash
npm run verify
```

2. Stage the complete build as an immutable release under Home Assistant `www`. Run from `ha-cards`, replacing the output path with the mounted or local HA configuration directory:

```bash
node scripts/dashboard-release.mjs prepare --dist dist --out <ha-config>/www/ajax/releases
```

The helper uses only Node built-ins. It copies every build file into `releases/<sha256>/` and writes `release.json` with each file's path, byte length, and SHA-256. The release ID depends on the sorted paths and exact file bytes, including chunks, CSS, images, and Vite metadata. Repeating preparation reuses an identical release; changed or missing files in an existing release cause an error. Symlinks and unsafe paths are rejected. Save the printed `manifestPath` for verification. If staging locally first, copy the entire release directory, including hidden `.vite` files, to the same `www/ajax/releases/<sha256>/` path on HA.

3. Verify the files actually served by Home Assistant. Use the `manifestPath` printed above and the browser-facing HA URL:

```bash
node scripts/dashboard-release.mjs verify --manifest <release-dir>/release.json --base-url https://ha.example/local/ajax/releases
```

Verification fetches every manifest file from `<base-url>/<sha256>/`, requires HTTP 200 without redirects, compares exact byte lengths and hashes, and checks JavaScript MIME types. HTML responses masquerading as JavaScript are rejected. A failure exits with a nonzero status and prints no resource URL. No HA resource is activated or changed by either command.

4. **Only after verification succeeds**, use its printed `resourceURL` to replace the previous AjaxBridge module resource in Home Assistant. Keep the old resource URL and release directory for rollback. For YAML-managed resources:

```yaml
resources:
  - url: https://ha.example/local/ajax/releases/<verified-sha256>/ajaxbridge-lovelace.js
    type: module
```

Use the same HA origin as the dashboard. Replace the existing resource instead of keeping two AjaxBridge module versions active. Reload the dashboard after activation. To roll back, restore the previous verified resource URL and reload; do not overwrite files inside an existing release directory.

5. Add one of the cards.

Detailed card:

```yaml
title: AjaxBridge
path: ajaxbridge
panel: true
cards:
  - type: custom:ajaxbridge-detailed-card
    dahua_base: https://ha.example.test/dahua-bridge
    grid_power_alarm_entities:
      - "ajaxbridge_jeedom_sia_<account>_zone_<zone>_input_alarm"
```

Use `dahua_base` when browser-side DahuaBridge calls need to go through a Home Assistant proxy path instead of the bridge URL published on camera attributes.

Compact chips card:

```yaml
type: custom:ajaxbridge-chips-card
max_chips: 7
dahua_base: https://ha.example.test/dahua-bridge
grid_power_alarm_entities:
  - "ajaxbridge_jeedom_sia_<account>_zone_<zone>_input_alarm"
```

`grid_power_alarm_entities` is an explicit Home Assistant-side mapping. Each item may be the entity registry's stable `unique_id` (recommended) or its current `entity_id` for backward compatibility. Using `unique_id` keeps the mapping valid when Home Assistant renames or prefixes an entity id. For each listed Transmitter input, `on` means the physical input is in alarm (utility power is unavailable) and `off` means the input is restored (utility power is available). Unlisted `input_alarm` entities keep their normal safety-alarm meaning; AjaxBridge does not decide which physical input monitors the mains. When the option is omitted or empty, the Grid power chip is not rendered.

## Several installations in one Home Assistant

Both cards accept `source_id` and `area_ids`. Set `source_id` to the new bridge's `AJAXBRIDGE_SOURCE_ID`; matching is exact. Omitted or empty `source_id` selects the existing legacy installation whose Ajax entities have no source namespace. It does not include a newly prefixed installation, even when both hubs reuse the same account, zone number, device names, or Jeedom command IDs. Keep the current house bridge and card without a source ID to preserve their identities.

For a new apartment bridge configured with `AJAXBRIDGE_SOURCE_ID=apartment`:

```yaml
type: custom:ajaxbridge-detailed-card
source_id: apartment
account: "<apartment SIA account>"
area_ids:
  - apartment_kitchen
  - apartment_hall
dahua_base: https://ha.example.test/apartment-dahua-bridge
```

Use the same `source_id` and `area_ids` on `custom:ajaxbridge-chips-card`. `account` remains an optional hub filter within the selected source. Source selection excludes unknown or foreign Ajax entities. `area_ids` uses Home Assistant's stable area IDs, not display names, and restricts rooms, device controls, events, climate, hero media, summary details, and camera analytics. An explicit empty list selects no areas.

Non-Ajax entities without source metadata, such as existing Dahua cameras and independent climate sensors, need explicit `area_ids` on a source-scoped card. A source ID alone includes only ancillary entities carrying that exact `source_id`. Add house area IDs to the legacy house cards as well when both properties share Home Assistant; unscoped legacy ancillary discovery retains its existing behavior. Use separate HA areas for the two properties. `dahua_base` applies only to camera channels that survive the card's source and area selection.

Ready-to-paste examples live in [`examples/`](./examples/):

- [`ajaxbridge-detailed-card.yaml`](./examples/ajaxbridge-detailed-card.yaml)
- [`ajaxbridge-chips-card.yaml`](./examples/ajaxbridge-chips-card.yaml)

Reference notes for DahuaBridge camera discovery, live playback, and SMD/IVS room counters live in [`docs/`](./docs/).

## Card behavior

- Rooms come from Home Assistant areas.
- Room hero backgrounds prefer area pictures and fall back to linked image or camera entities.
- AjaxBridge devices come from the Home Assistant device/entity registries plus MQTT entities published by AjaxBridge.
- An omitted `source_id` selects legacy Ajax entities; a configured source selects only its exact namespace. `area_ids` bounds all room data and ancillary integrations to the chosen Home Assistant areas.
- Grid-power status is derived only from the `input_alarm` entity or unique ids explicitly listed in `grid_power_alarm_entities`.
- Summary chips are rendered only when their backing mapping or discovered HA source exists. Security mode requires a mode or active-alarm source; SMD/IVS requires a configured Dahua analytics channel; switch counts require discovered matching devices. A configured source with a valid zero value remains visible (including `Alerts: 0`); an absent source does not produce a `0/0` placeholder.
- Summary chips are interactive: select one to see the sources behind its total, including the meaning of `x/y` device counts. Mapped grid outages are shown as informational status and are excluded from Alerts.
- Dahua and Roller devices are grouped by Home Assistant device and rendered inside their assigned room.
- Dahua camera event rows are limited to SMD/IVS detection state and camera online/offline state. Unavailable SMD/IVS sensors, stream, codec, ONVIF/H.264, profile, capability, and other diagnostic entities are ignored for camera events and alerts.
- Dahua SMD/IVS 24 hour counters are shown only for rooms that contain a Dahua camera device. They are loaded from the DahuaBridge NVR `/events/summary` endpoint and can use `dahua_base` for browser-reachable proxy URLs.
- Event rows are synthesized from current or latest Home Assistant entity state. The UI does not query AjaxBridge `/events` history directly.

## Project structure

- `src/ha/`: Home Assistant card registration and types
- `src/data/liveDashboardData.ts`: live HA registry and state adapter
- `src/cards/` and `src/components/`: presentational UI building blocks
- `src/models/dashboard.ts`: shared typed dashboard model
- `src/styles/`: theme and layout CSS
- `public/assets/`: icons and room assets bundled by Vite

## Notes

- The module resolves icons and room assets relative to the module URL, so `/local/ajaxbridge-lovelace/` and similar install paths both work.
- `dist/index.html` is useful for local visual review, but Home Assistant custom-card usage is the main target.

## License

MIT License. See [../LICENSE](../LICENSE). See [../NOTICE](../NOTICE) for trademark and affiliation notice.
