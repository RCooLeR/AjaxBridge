# AjaxBridge

AjaxBridge connects Ajax security hubs to Home Assistant and Prometheus. It receives SIA DC-09 events as the authoritative security input and optionally reads Jeedom MQTT for device measurements, diagnostics, and allowlisted controls. It exposes HTTP JSON, Prometheus metrics, retained MQTT state, and Home Assistant MQTT Discovery.

<p style="text-align: center">
<img src="./bridge/ajax-bridge.png" alt="AjaxBridge" width="70%">
</p>

The repository includes the Go bridge, Home Assistant Lovelace cards, and an optional telemetry patch for the official Jeedom Ajax Systems plugin. The bridge keeps current state and an optional Jeedom cache; historical storage belongs to Prometheus or Home Assistant Recorder.

## Version 2.1.0

This release adds complete Jeedom snapshot metrics, broader device diagnostics, stable command ownership, observed-state controls, and explicit electrical unit handling. Cached values are available to Prometheus immediately after startup. Missing readings stay unknown, and unit corrections preserve Home Assistant command identities without rewriting old statistics.

Read the [2.1.0 release and upgrade notes](./bridge/docs/releases/2.1.0.md) before upgrading from 2.0.1, especially if you already record current, power, or energy history. The release includes a separate `AjaxBridge_2.1.0_jeedom_patch.zip`; extract it and follow the included `Jeedom/README.md` to update an existing plugin installation.

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Documentation

- [Bridge documentation](./bridge/docs/index.md)
- [2.1.0 release and upgrade notes](./bridge/docs/releases/2.1.0.md)
- [Prometheus metrics and history](./bridge/docs/prometheus.md)
- [Jeedom Ajax Systems patch: files, installation and licensing (Українською)](./Jeedom/README.md)
- [Notifications](./bridge/docs/notifications.md)
- [Admin panel](./bridge/docs/admin.md)
- [Bridge package](./bridge/README.md)
- [Home Assistant cards](./ha-cards/README.md)

## License

AjaxBridge code is MIT licensed; see [LICENSE](./LICENSE) and [NOTICE](./NOTICE). The third-party Jeedom plugin files in [`Jeedom/files/`](./Jeedom/files/) retain their upstream copyleft terms and are not relicensed under MIT; see [Jeedom licensing](./Jeedom/LICENSING.md). Purchase and install the official Ajax Systems plugin from Jeedom Market before following the patch installation procedure.
