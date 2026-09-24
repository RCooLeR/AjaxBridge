# AjaxBridge patch for the Jeedom Ajax Systems plugin

Bundle: 2026.09.24.1. Modified/packaged: 2026-09-24.
Upstream plugin author: Jeedom SAS (from plugin_info/info.json).
Local modifications: AjaxBridge contributors.

This is a source overlay for an existing official Ajax Systems installation,
not a complete plugin or a grant of Jeedom Market/cloud access. Purchase and
install the official plugin through your Jeedom Market account before following
this bundle's installation procedure. This prerequisite does not add restrictions
to rights granted by the applicable free-software license.

## License evidence retained from the source

- The original LICENSE contains GNU GPL version 2: LICENSE.GPL-2.0.
- PHP source headers explicitly state GNU GPL version 3 or any later version:
  LICENSE.GPL-3.0 is included without removing those headers.
- plugin_info/info.json declares AGPL with no version: the original metadata is
  preserved as UPSTREAM-info.json; LICENSE.AGPL-3.0 is supplied for reference.

These declarations are inconsistent. Including all three license texts does not
resolve that inconsistency, change upstream licensing, or give a free choice of
license for the entire plugin. The AjaxBridge repository's MIT license does not
relicense these third-party plugin files. Modifications to the plugin are supplied
under the applicable upstream copyleft terms. Confirm the intended scope/version
with Jeedom before relying on a single license designation for redistribution.
Preserve original notices and provide corresponding source under the applicable
license; AGPL section 13 may also require a source offer to remote users of a
modified program. See the bundle's LICENSING.md for evidence and details.

## Changes in this overlay

- core/class/ajaxSystem.class.php: shared snapshot/callback parsing, battery
  aliases, nested fields and string fault lists, missing-info-command migration,
  WallSwitch/Relay switch-state normalization; no new polling or API endpoint.
- core/php/jeeAjaxSystem.php: routes data updates through the shared parser.
- plugin_info/install.php: adds missing info commands during plugin update.
- core/config/devices/*.json: added/extended telemetry templates for Button,
  SpaceControl, WaterStop, ReX, MultiTransmitter, LifeQualityLite, FireProtect
  families and WallSwitch state. Each supplied file identifies this patch.
- Transmitter.json carries forward temperature already in the local baseline;
  it is included deliberately although absent from the subsequent Git diff.
- Relay.json and Socket templates are not replaced. Relay uses the new shared
  parser; no Socket feature change is included.

Local baseline: 0ee0a700a0f1d5fa578e0e51a762a15e98daeb69.
Telemetry commit: 92fee87fdc1762fd6143de6b4fd6c2ee3cd8ae2a.
State commit: af5a742706f7185a4b018e94671d1b7ed8b0e731.
These identify a local source tracker, not official Jeedom release tags.
The package also normalizes text line endings to LF and adds modification
notices; these packaging changes do not alter command definitions or PHP logic.

The complete list of runtime files and source/base/package hashes is provided in
the bundle's manifest.json. METRICS.md documents model-specific fields and known
cloud/firmware limitations. This unofficial patch is not endorsed by Jeedom or
Ajax Systems. Product names remain the property of their respective owners.
