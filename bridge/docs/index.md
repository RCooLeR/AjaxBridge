# AjaxBridge Documentation

## Overview

- [Overview](./overview.md): architecture, install/run basics, data flow, HTTP endpoints, and configuration model.

## Releases and Upgrades

- [2.1.0](./releases/2.1.0.md): changes since 2.0.1, electrical unit and history migration, unknown states and controls, tooling, Jeedom patch packaging, upgrade checks, and rollback.
- [Project changelog](../../CHANGELOG.md): release history for the bridge, cards, and bundled integrations.

## Disclaimer

- [Disclaimer](./disclaimer.md): unofficial DIY project, trademark, and affiliation notice.

## SIA

- [SIA integration](./sia.md): SIA DC-09 details, source priority, state engine behavior, Ajax app setup, and bridge configuration.

## Jeedom

- [Jeedom integration](./jeedom.md): MQTT setup, source-unit provenance, cache migration, command ownership, sample capture, translation, and controls.
- [Jeedom plugin patch (Українською)](../../Jeedom/README.md): copy/replace files, required official plugin purchase, licensing, supported metrics, compatibility checks, backup, installation and rollback.
- [Jeedom electrical command audit](../../Jeedom/tools/README.md): read-only inspection of command units and existing conversion formulas before correcting electrical telemetry.

## Prometheus

- [Prometheus metrics](./prometheus.md): `/metrics`, snapshot-based Jeedom values, booleans and enums, observation freshness, OpenMetrics, scrape configuration, and history queries independent of Recorder.

## Notifications

- [Notifications](./notifications.md): configurable threshold, state-change, and control notifications for Jeedom-backed values.

## Admin Panel

- [Admin panel](./admin.md): Bootstrap UI for editing device names, rooms, SIA/Jeedom matching, and notification rules.

## Home Assistant

- [Home Assistant integration and technical guide](./home-assistant.md): MQTT Discovery setup, created devices, naming rules, sensors, commands, topics, payloads, dashboard/card guidance, and troubleshooting.

## Runtime Contracts

- [Runtime contracts](./contracts.md): stable HTTP, MQTT, and Home Assistant discovery surfaces that dashboards and automations depend on.
