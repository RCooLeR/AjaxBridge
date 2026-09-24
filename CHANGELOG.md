# Changelog

## Unreleased

- Package the optional Jeedom Ajax Systems telemetry patch in `Jeedom/`, with copy/replace source files, Ukrainian installation and rollback instructions, required official plugin purchase guidance, upstream licensing notices, checksums and offline validation.
- Normalize the expanded Ajax Jeedom plugin contract for Button, SpaceControl, WaterStop, ReX, MultiTransmitter, LifeQuality, and FireProtect families, including diagnostic metadata, CO₂, timestamp conversion, valve position, and separate smoke/heat/CO alarms.
- Prefer stable Jeedom logical/generic metadata and existing command-ID contracts over localized labels, migrate cached mappings on restart, and reconcile removed commands per owning eqLogic with persistent Home Assistant discovery cleanup retries.
- Expand the admin matching view with canonical command details, linked/unlinked summaries, one-click numeric command-ID merging, and notification metrics discovered from the live Jeedom model.
- Preserve action-only WallSwitch state across AjaxBridge restarts by publishing retained bridge state, persisting successful ON/OFF controls, and representing genuinely unknown state as unknown instead of OFF.
- Seed an active WallSwitch from positive power/current telemetry when Jeedom discovery has no `Etat`/`realState` command, without treating zero load as proof that the relay is off.
- Upgrade the bridge to Go 1.27, urfave/cli v3, Chi 5.3.2, Prometheus client 1.24.1, and current transitive modules; pin `govulncheck` through the Go tool directive and adopt context-aware CLI handlers, `WaitGroup.Go`, and modern slice iteration.
- Publish selected Go scheduler runtime metrics and OpenMetrics unit metadata for duration, timestamp, power, current, voltage, temperature, and battery metric families.
- Upgrade the Lovelace UI to React 19.2.8, TypeScript 7, Vite 8.2 with native Rolldown configuration, and type-aware Oxlint; adopt React Effect Events, stricter project references, safer state resets, and generated third-party license metadata.
- Modernize the container and release pipeline with Go 1.27 and Alpine 3.24.1 images, BuildKit cache mounts, Compose hardening, current pinned GitHub Actions, automated dependency updates, release checksums, SBOMs, provenance, keyless Cosign signatures, and GitHub attestations.

## 2.0.1 - 2026-08-05

- Split Home Assistant JSON attributes from dynamic Ajax/Jeedom state topics. Entities now keep compact stable device metadata while full MQTT state, HTTP APIs, entity identities, controls, and dedicated timestamp sensors remain compatible.
- Clear a zone's latched SIA alarm when the device is bypassed/deactivated or turned off, while preserving last-event and measurement telemetry.

## 2.0.0 - 2026-05-06

### Added

- Home Assistant Lovelace dashboard card for AjaxBridge rooms, devices, cameras, safety, grid power, and controls.
- Home Assistant MQTT discovery for Ajax devices, sensors, actions, valves, switches, hub modes, and Jeedom-derived entities.
- Jeedom integration support, including device catalog resolution, value seeding, event parsing, and retained MQTT topic cleanup.
- Dahua camera support in the card, including room camera selection, direct IPC model detection, native HA camera stream handling, and SMD/IVS summaries.
- Device image catalog based on Jeedom and Dahua device assets.
- Push notification support and admin UI improvements.

### Changed

- Renamed and documented the project as AjaxBridge while keeping compatibility-oriented release images.
- Improved Ajax event/state detection for wall switches, WaterStop valves, transmitters, grid power, hub arming actions, fire mute actions, and duplicate sensor cleanup.
- Reworked the card device layout to use real device images, Material Design icons, meaningful device controls, and room-level summaries.
- Wall switch power shown in the card is now calculated from voltage and current instead of using total power.

### Fixed

- Cleaned legacy Jeedom and Ajax2Prometheus retained MQTT discovery topics for unlinked or renamed devices.
- Hid Ajax app devices from the card device list.
- Hid room smoke/CO status when the room has no FireProtect, LifeQuality, or equivalent measuring device.
- Improved nested Home Assistant camera player mute and sizing behavior after the native video element is created.

## 1.0.1 - 2026-04-25

- Migrated Ajax2Prometheus release naming and metadata to AjaxBridge.

## 1.0.0 - 2026-04-25

- Initial Docker release.
