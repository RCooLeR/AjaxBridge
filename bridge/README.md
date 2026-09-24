# AjaxBridge

AjaxBridge receives Ajax security hub SIA DC-09 events, keeps the current alarm state in memory, and exposes the data through HTTP JSON, Prometheus, MQTT, and Home Assistant MQTT Discovery.

<p style="text-align: center">
<img src="./ajax-bridge.png" alt="AjaxBridge" width="70%">
</p>

The bridge is built around two inputs:

- SIA DC-09 from Ajax as the authoritative security source.
- Optional Jeedom MQTT for device metrics, allowlisted toggles, and relay impulse controls.

No database is required. Runtime SIA state is kept in memory, the optional device catalog lives in `data/devices.json`, and the optional Jeedom mirror cache lives in `data/jeedom.json`.

Use [docker-compose.yml](./docker-compose.yml) as the tracked baseline and keep site-specific production overrides private, for example in an untracked `docker-compose.production.yml` or `docker-compose.override.yml`. Production data should stay under `./data` so the device catalog, notifications, and Jeedom cache survive container restarts.

CI runs on every pushed branch and pull request. It verifies the Go bridge with vet, normal and race-enabled tests, and `govulncheck`; verifies the Home Assistant cards with type-aware linting, TypeScript 7, and a Vite production build; and validates, smoke-tests, and scans the hardened amd64/arm64 container image.

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Documentation

- [Documentation index](./docs/index.md)
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
