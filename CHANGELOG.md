# Changelog

## Unreleased

- Update Go to 1.27.3, Chi to 5.5.0, and compatible Prometheus and Go networking/tooling dependencies. Refresh digest-pinned Go and Alpine 3.24.3 images for both amd64 and arm64 builds.
- Update the card toolchain to Node.js 24.22.1 LTS and npm 12.2.1, React to 19.3.1, Vite to 8.3.5, and compatible linting and bundler dependencies.
- Refresh the Jeedom 4.6 Bookworm image (bundled Jeedom 4.6.2 and PHP 8.2.35), preserving existing mounted HTML and external-database configuration. Update pinned release and scanning tools.

## 2.1.1 - 2026-10-12

Upgrade and rollback guidance: [2.1.1 release notes](bridge/docs/releases/2.1.1.md).

- Update Go to 1.27.2, `golang.org/x/net` to 0.60.0, and Vite to 8.3.3, including the security fixes needed for PR #10. Keep the digest-pinned Go builder aligned with the module toolchain.
- Republish unchanged MQTT discovery and cached SIA/Jeedom state after Home Assistant announces it is online. Coalesce repeated startup messages, stagger bridge replies, and never replay actuator commands. Internal SIA publication revisions prevent an older replay from overwriting newer live state.
- Isolate additional bridge installations with an opt-in source ID, separate Jeedom input roots, scoped data paths, MQTT/HA identities, Prometheus labels, and dashboard filters. An empty source ID preserves the existing installation's defaults.
- Add an optional Jeedom Docker runtime with bind-mounted HTML and an existing external MySQL 8.4 server, explicit bootstrap/migration, private configuration, guarded compatibility patches, and preserved command/configuration data.
- Fix MySQL 8.4 listener maintenance by quoting reserved identifiers; preserve original core files and ownership. Keep service PID files readable by Jeedom without relaxing private credential-file permissions.
- Include Jeedom bundle 2026.10.09.1 with reviewed upstream compatibility, a guarded offline installer, Hub 2 Plus communication telemetry, and invalid/nonfinite Socket telemetry handling that preserves valid zero values and existing IDs/formulas.
- Republish the complete Jeedom catalog after linking changes; expose Hub transport and radio diagnostics without treating an enabled interface as a connected uplink, and separate analytics categories from scrollable room breakdowns.
- Hide summary chips that have no configured or discovered backing source, avoid inferred security modes and analytics zeroes, preserve valid zero values (including `Alerts: 0`) for sources that do exist, and honor `dahua_base` on the compact chips card.
- Make Home Assistant grid-input mappings resilient to entity renames by accepting stable entity-registry `unique_id` values, while preserving existing `entity_id` configurations and reporting stale selectors explicitly; hide the grid-power chip when no mapping is configured.
- Publish a Transmitter's physical-input `M_11_3F` and `M_11_40` events as a generic `input_alarm` binary state, while cleaning up obsolete derived `grid_power` discovery entities during cache migration.
- Let Home Assistant card configuration decide which `input_alarm` entity represents utility-power availability; other Transmitters retain normal alarm semantics.
- Keep mapped utility outages informational and outside the Alerts total, and make dashboard summary chips open a per-device or per-category breakdown.
- Update the Home Assistant card toolchain to `@vitejs/plugin-react` 6.1.2 and Oxlint 1.87.0, including current compatible lockfile patches.

## 2.1.0 - 2026-09-25

Upgrade guidance and history-migration details: [2.1.0 release notes](bridge/docs/releases/2.1.0.md).

- Preserve canonical Relay voltage across cache reloads and metadata reconciliation; reject ambiguous display-name aliases while retaining explicit command ownership.
- Collect Jeedom Prometheus metrics from one store snapshot per scrape, including restored values, booleans, bounded enum states, timestamps and observation age; remove stale series when commands disappear and negotiate OpenMetrics on `/metrics`.
- Export the new device/button modes, antenna diagnostics and validated firmware versions; verify the exporter against the bundled Jeedom templates, including FireProtect, ReX, MultiTransmitter, WaterStop and LifeQuality telemetry.
- Resolve dashboard entities by stable Ajax ownership and metadata, recognize suffixed legacy alarm entities, distinguish operational warnings from security alarms, preserve the full power-device inventory, and keep physical Button/SpaceControl cards read-only.
- Block unknown/intermediate control states at rendering and dispatch, confirm security actions, and avoid automatically retrying service commands.
- Preserve electrical values with unknown source units as raw diagnostics instead of inventing amperes or watts; record unit provenance and label calculated voltage times current as apparent power in VA.
- Recognize explicitly declared energy units on legacy `powerWtH`/`POWER` commands, preserve command entity IDs and cached observations, and show energy despite a historic Power entity name. Document preservation of existing electrical statistics and archive graphs during quantity corrections.
- Keep unverified energy readings as `energy_raw` across discovery and cache reloads instead of inventing kWh; show raw energy and all supported verified energy units in the cards.
- Keep discovery and entity identity for verified current, power and energy commands while their value is unknown, including after unit corrections and cache reloads; publish unknown without inventing a zero or observation time.
- Declare raw WallSwitch current in mA and cumulative energy in Wh in Jeedom bundle 2026.09.25.1; preserve existing command settings and support verified upgrades from the prior bundle.
- Stage immutable dashboard releases with a SHA-256 manifest and verify every served asset before switching the Home Assistant resource.
- Package the optional Jeedom Ajax Systems telemetry patch in `Jeedom/`, with copy/replace source files, Ukrainian installation and rollback instructions, required official plugin purchase guidance, upstream licensing notices, checksums and offline validation.
- Ship the verified Jeedom patch as a separate reproducible ZIP with the release checksums, alongside the bridge binaries, Home Assistant cards, source archive and multi-platform Docker images.
- Normalize the expanded Ajax Jeedom plugin contract for Button, SpaceControl, WaterStop, ReX, MultiTransmitter, LifeQuality, and FireProtect families, including diagnostic metadata, CO₂, timestamp conversion, valve position, and separate smoke/heat/CO alarms.
- Prefer stable Jeedom logical/generic metadata and existing command-ID contracts over localized labels, migrate cached mappings on restart, and reconcile removed commands per owning eqLogic with persistent Home Assistant discovery cleanup retries.
- Expand the admin matching view with canonical command details, linked/unlinked summaries, one-click numeric command-ID merging, and notification metrics discovered from the live Jeedom model.
- Preserve action-only WallSwitch state across AjaxBridge restarts by publishing retained bridge state, persisting successful ON/OFF controls, and representing genuinely unknown state as unknown instead of OFF.
- Seed an active WallSwitch from positive power/current telemetry when Jeedom discovery has no `Etat`/`realState` command, without treating zero load as proof that the relay is off.
- Upgrade the bridge to Go 1.27.1, urfave/cli 3.13, Chi 5.3.2, Prometheus client 1.24.1, and current transitive modules; pin `govulncheck` through the Go tool directive and adopt context-aware CLI handlers, `WaitGroup.Go`, and modern slice iteration.
- Publish selected Go scheduler runtime metrics and OpenMetrics unit metadata for duration, timestamp, power, current, voltage, temperature, and battery metric families.
- Upgrade the Lovelace UI to React 19.3, TypeScript 7, Vite 8.3.1 with native Rolldown configuration, and type-aware Oxlint 1.85; use Node 24.21 LTS and npm 12.1, React Effect Events for live registry/poll callbacks, stricter project references, safer state resets, and generated build/license manifests.
- Modernize the container and release pipeline with Go 1.27.1 and Alpine 3.24.2 images, BuildKit cache mounts, Compose hardening, current pinned GitHub Actions, automated dependency updates, release checksums, SBOMs, provenance, keyless Cosign signatures, and GitHub attestations. Update admin Bootstrap to 5.3.8 with integrity-checked assets.

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
