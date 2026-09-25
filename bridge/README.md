# AjaxBridge

AjaxBridge receives Ajax security hub SIA DC-09 events, keeps the current alarm state in memory, and exposes the data through HTTP JSON, Prometheus, MQTT, and Home Assistant MQTT Discovery.

<p style="text-align: center">
<img src="./ajax-bridge.png" alt="AjaxBridge" width="70%">
</p>

The bridge is built around two inputs:

- SIA DC-09 from Ajax as the authoritative security source.
- Optional Jeedom MQTT for device metrics, allowlisted toggles, and relay impulse controls.

No database is required by the bridge. Runtime SIA state is kept in memory, the optional device catalog lives in `data/devices.json`, and the optional Jeedom mirror cache lives in `data/jeedom.json`. These files preserve configuration and current observations, not a time-series history. Configure Prometheus retention or Home Assistant Recorder separately when history is needed.

Use [docker-compose.yml](./docker-compose.yml) as the tracked baseline and keep site-specific production overrides private, for example in an untracked `docker-compose.production.yml` or `docker-compose.override.yml`. Production data should stay under `./data` so the device catalog, notifications, and Jeedom cache survive container restarts.

## Upgrading to 2.1.0

Read the [release and upgrade notes](./docs/releases/2.1.0.md) for the complete changes since 2.0.1. Back up the bridge data directory, existing Jeedom command settings, and any Home Assistant statistics before changing electrical units.

Jeedom metrics now come from one store snapshot on every scrape, including restored observations, boolean diagnostics, supported enum states, and observation age. A restart or metadata refresh does not make an old measurement fresh. Removed commands and old owners stop producing current series.

Current, power, and energy use verified source units. Unitless current/power readings remain raw diagnostics; legacy ambiguous A/W labels are removed without changing the raw numbers. A unit change can leave a reading unknown until a new observation arrives. Verified electrical entities keep their MQTT identity even while unknown. The dashboard blocks controls that depend on unknown state, and calculated voltage times current is labelled apparent power in VA.

Home Assistant Recorder exclusions do not stop Prometheus scraping. Neither the bridge nor this upgrade automatically changes Recorder configuration, converts historical samples, or repairs old statistics. The optional [Jeedom patch](../Jeedom/README.md) supplies expanded telemetry and explicit mA/Wh defaults for new WallSwitch commands; existing commands require a separate unit/formula audit.

## Build and verification

The bridge uses Go 1.27.1. From `bridge/`:

```sh
go test ./...
go vet ./...
go test -race ./...
go tool govulncheck ./...
```

The race detector requires a supported platform and C toolchain. Home Assistant cards use Node.js 24.21.0 and npm 12.1.0; see their [development guide](../ha-cards/README.md).

CI is configured to run on pushed branches and pull requests. It checks the Go bridge with vet, normal and race-enabled tests, and the pinned `govulncheck`; checks the cards with regression tests, type-aware linting, TypeScript 7, and a Vite production build; and validates, smoke-tests, and scans the hardened amd64/arm64 container image. The release pipeline produces checksums, SBOMs, provenance and signed container images.

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Documentation

- [Documentation index](./docs/index.md)
- [2.1.0 release and upgrade notes](./docs/releases/2.1.0.md)
- [Overview](./docs/overview.md)
- [SIA integration](./docs/sia.md)
- [Jeedom integration](./docs/jeedom.md)
- [Jeedom plugin patch files and installation guide (Українською)](../Jeedom/README.md)
- [Prometheus metrics](./docs/prometheus.md)
- [Notifications](./docs/notifications.md)
- [Admin panel](./docs/admin.md)
- [Home Assistant integration and technical guide](./docs/home-assistant.md)
- [Runtime contracts](./docs/contracts.md)
- [Home Assistant card package](../ha-cards/README.md)
- [Disclaimer and trademark notice](../NOTICE)

## License

MIT License. See [../LICENSE](../LICENSE). See [../NOTICE](../NOTICE) for trademark and affiliation notice.
