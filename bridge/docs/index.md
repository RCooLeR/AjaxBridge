# AjaxBridge Documentation

## Overview

- [Overview](./overview.md): architecture, install/run basics, data flow, HTTP endpoints, and configuration model.

## Disclaimer

- [Disclaimer](./disclaimer.md): unofficial DIY project, trademark, and affiliation notice.

## SIA

- [SIA integration](./sia.md): SIA DC-09 details, source priority, state engine behavior, Ajax app setup, and bridge configuration.

## Jeedom

- [Jeedom integration](./jeedom.md): what Jeedom adds, how to install Jeedom and required plugins, MQTT Manager setup, bridge setup, sample capture, translation, duplicate prevention, and controls.
- [Jeedom plugin patch (Українською)](../../Jeedom/README.md): copy/replace files, required official plugin purchase, licensing, supported metrics, compatibility checks, backup, installation and rollback.

## Prometheus

- [Prometheus metrics](./prometheus.md): `/metrics` endpoint, scrape config, metric names, labels, values, and query examples.

## Notifications

- [Notifications](./notifications.md): configurable threshold, state-change, and control notifications for Jeedom-backed values.

## Admin Panel

- [Admin panel](./admin.md): Bootstrap UI for editing device names, rooms, SIA/Jeedom matching, and notification rules.

## Home Assistant

- [Home Assistant integration and technical guide](./home-assistant.md): MQTT Discovery setup, created devices, naming rules, sensors, commands, topics, payloads, dashboard/card guidance, and troubleshooting.

## Runtime Contracts

- [Runtime contracts](./contracts.md): stable HTTP, MQTT, and Home Assistant discovery surfaces that dashboards and automations depend on.
