# AjaxBridge

AjaxBridge receives Ajax security hub SIA DC-09 events, keeps the current alarm state in memory, and exposes HTTP JSON, Prometheus metrics, MQTT state, and Home Assistant MQTT Discovery.

<p style="text-align: center">
<img src="./bridge/ajax-bridge.png" alt="AjaxBridge" width="70%">
</p>

## Disclaimer

AjaxBridge is an unofficial DIY open-source project for compatibility and integration. It is not affiliated with, endorsed by, or sponsored by Ajax Systems.

## Documentation

- [Bridge documentation](./bridge/docs/index.md)
- [Jeedom Ajax Systems patch: files, installation and licensing (Українською)](./Jeedom/README.md)
- [Notifications](./bridge/docs/notifications.md)
- [Admin panel](./bridge/docs/admin.md)
- [Bridge package](./bridge/README.md)
- [Home Assistant cards](./ha-cards/README.md)

## License

AjaxBridge code is MIT licensed; see [LICENSE](./LICENSE) and [NOTICE](./NOTICE). The third-party Jeedom plugin files in [`Jeedom/files/`](./Jeedom/files/) retain their upstream copyleft terms and are not relicensed under MIT; see [Jeedom licensing](./Jeedom/LICENSING.md). Purchase and install the official Ajax Systems plugin from Jeedom Market before following the patch installation procedure.
