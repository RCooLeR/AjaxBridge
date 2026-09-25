# Electrical command audit

Run this standalone helper **inside the existing Jeedom host or container**, using the same PHP user and installation root as Jeedom:

```sh
php /path/to/electrical-contract-audit.php --jeedom-root /var/www/html
```

It reads only `ajaxSystem` electrical **numeric info** commands on WallSwitch, Socket, SocketTypeG/Plus, SocketTypeB and SocketOutletTypeE/F. JSON contains equipment/command IDs, model, logical ID, unit, `calculValueOffset`, and the existing finite numeric cached value. It does not execute commands, evaluate formulas, refresh devices, synchronize, contact Ajax, or save configuration. The normal Jeedom bootstrap and local database/cache access are required. No API key is needed.

Use the actual command's unit **and formula** to determine whether mA → A or Wh → kWh conversion has already happened. MQTT Manager removes `calculValueOffset` from its discovery payload, so MQTT discovery alone cannot establish this. Cached readings may be stale; the helper does not infer physical units or recalculate them.

Device names, account identifiers, full configuration and nonnumeric cached strings are excluded. Only arithmetic formulas and standard math functions (`abs`, `round`, `floor`, `ceil`, `min`, `max`, `pow`) are printed; other expressions and unexpected unit text are withheld with an explicit status. Inspect any withheld formula locally rather than assuming it is blank.

The helper stays outside `files/`; it is not part of the plugin overlay or the runtime file list in `manifest.json`. The bundle's `SHA256SUMS` covers it and its offline validation. PHP 7.4+ is required. `--help` works without loading Jeedom. The helper and its validation use the included [MIT license](../LICENSE.MIT).

Offline validation: `php Jeedom/validation/electrical-contract-audit.php` uses a temporary stub installation and checks scope, unchanged cached numbers, redaction, errors and CLI validation. It never loads the real Jeedom installation.

Read-only API references: [Jeedom equipment lookup and command getters](https://github.com/jeedom/core/blob/aeaf504f9bcaa1fe62f9fac1f2763e508116ff03/core/class/eqLogic.class.php), [command metadata and cache getters](https://github.com/jeedom/core/blob/aeaf504f9bcaa1fe62f9fac1f2763e508116ff03/core/class/cmd.class.php), [MQTT discovery filtering](https://github.com/jeedom/plugin-mqtt2/blob/1d2cd9dcf11661642c36f2a5e896a34b048b6d4d/core/class/mqtt2.class.php).
