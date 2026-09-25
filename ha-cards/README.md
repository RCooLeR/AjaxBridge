# AjaxBridge Lovelace UI

`ha-cards` builds the Home Assistant Lovelace cards for AjaxBridge.

It ships two custom cards:

- `custom:ajaxbridge-detailed-card`
- `custom:ajaxbridge-chips-card`

When loaded inside Home Assistant, the cards use live Home Assistant data from the area, device, and entity registries plus current entity state. The standalone Vite preview still works for local UI development.

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Development

Use Node.js 24.21.0 LTS and npm 12.1.0. The checked-in `.node-version` and `packageManager` fields record those versions.

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
```

Use `dahua_base` when browser-side DahuaBridge calls need to go through a Home Assistant proxy path instead of the bridge URL published on camera attributes.

Compact chips card:

```yaml
type: custom:ajaxbridge-chips-card
max_chips: 7
```

Ready-to-paste examples live in [`examples/`](./examples/):

- [`ajaxbridge-detailed-card.yaml`](./examples/ajaxbridge-detailed-card.yaml)
- [`ajaxbridge-chips-card.yaml`](./examples/ajaxbridge-chips-card.yaml)

Reference notes for DahuaBridge camera discovery, live playback, and SMD/IVS room counters live in [`docs/`](./docs/).

## Card behavior

- Rooms come from Home Assistant areas.
- Room hero backgrounds prefer area pictures and fall back to linked image or camera entities.
- AjaxBridge devices come from the Home Assistant device/entity registries plus MQTT entities published by AjaxBridge.
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
