# AjaxBridge

AjaxBridge connects Ajax security hubs to Home Assistant and Prometheus. It receives SIA DC-09 events as the authoritative security input and optionally reads Jeedom MQTT for device measurements, diagnostics, and allowlisted controls. It exposes HTTP JSON, Prometheus metrics, retained MQTT state, and Home Assistant MQTT Discovery.

<p style="text-align: center">
<img src="./bridge/ajax-bridge.png" alt="AjaxBridge" width="70%">
</p>

The repository includes the Go bridge, Home Assistant Lovelace cards, and an optional telemetry patch for the official Jeedom Ajax Systems plugin. The bridge keeps current state and an optional Jeedom cache; historical storage belongs to Prometheus or Home Assistant Recorder.

## Version 2.1.1

This patch release includes Go security fixes and a Vite dependency update, restores MQTT discovery after Home Assistant restarts, fixes Jeedom compatibility with MySQL 8.4, and separates multiple bridge installations. It also improves configured dashboard chips, informational grid outages, and Hub communication details. Keep existing source IDs unchanged and review the documented migration from legacy grid-power entities to raw input alarms.

Read the [2.1.1 release and upgrade notes](./bridge/docs/releases/2.1.1.md). The release includes `AjaxBridge_2.1.1_jeedom_patch.zip` for an existing official plugin installation; the custom external-database Jeedom runtime is built separately from the source archive. Upgrading from 2.0.1 or earlier also requires the [2.1.0 electrical unit and history guidance](./bridge/docs/releases/2.1.0.md).

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Documentation

- [Bridge documentation](./bridge/docs/index.md)
- [2.1.1 release and upgrade notes](./bridge/docs/releases/2.1.1.md)
- [Prometheus metrics and history](./bridge/docs/prometheus.md)
- [Multiple installations: house and apartment](./bridge/docs/multiple-installations.md)
- [Jeedom Docker with bind-mounted HTML and external MySQL 8.4](./deploy/jeedom-external/README.md)
- [Jeedom Ajax Systems patch: files, installation and licensing (Українською)](./Jeedom/README.md)
- [Notifications](./bridge/docs/notifications.md)
- [Admin panel](./bridge/docs/admin.md)
- [Bridge package](./bridge/README.md)
- [Home Assistant cards](./ha-cards/README.md)

## License

AjaxBridge code is MIT licensed; see [LICENSE](./LICENSE) and [NOTICE](./NOTICE). The third-party Jeedom plugin files in [`Jeedom/files/`](./Jeedom/files/) retain their upstream copyleft terms and are not relicensed under MIT; see [Jeedom licensing](./Jeedom/LICENSING.md). Purchase and install the official Ajax Systems plugin from Jeedom Market before following the patch installation procedure.
