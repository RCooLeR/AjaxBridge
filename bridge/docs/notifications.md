# Notifications

AjaxBridge can send configurable notifications for Jeedom values and Jeedom controls that Ajax itself may not push reliably.

Notifications are driven by Jeedom MQTT data. SIA still remains the security source of truth, but SIA does not normally provide power, current, voltage, temperature, or outlet/switch state telemetry.

## What Can Be Notified

Supported metric rules:

| Use case | Jeedom metric | Rule condition |
| --- | --- | --- |
| Temperature below value | `temperature_c` | `below` |
| Temperature above value | `temperature_c` | `above` |
| Voltage below value | `voltage_v` | `below` |
| Voltage above value | `voltage_v` | `above` |
| WallSwitch/Outlet power above value | `power_w` | `above` |
| WallSwitch/Outlet current above value | `current_a` | `above` |
| WallSwitch/Outlet turned on | `state` | `changed_to_on` |
| WallSwitch/Outlet turned off | `state` | `changed_to_off` |

Supported control rules:

| Use case | Rule condition |
| --- | --- |
| Any AjaxBridge-issued Jeedom control | `control` |
| AjaxBridge-issued turn on | `control_on` |
| AjaxBridge-issued turn off | `control_off` |

Relay impulse detection depends on where the trigger comes from:

- If the relay/outlet/switch is triggered through AjaxBridge HTTP, a Home Assistant MQTT switch, or a Home Assistant relay impulse button, AjaxBridge sees the control request and can notify with `control`, `control_on`, or `control_off`.
- If another tool publishes to the Jeedom MQTT Manager command topic `jeedom/cmd/set/<command_id>`, AjaxBridge subscribes to that topic and can notify when the command id maps to a discovered Jeedom action.
- If the device is triggered physically, from Ajax, or directly from Jeedom, AjaxBridge can notify only when Jeedom publishes a changed `state` info command.
- If Jeedom does not publish a state command for that equipment, AjaxBridge cannot reliably know the physical on/off state.

## Configuration File

Default path:

```text
data/notifications.json
```

Docker default:

```text
/data/notifications.json
```

Environment variable:

```yaml
AJAXBRIDGE_NOTIFICATIONS_PATH: "/data/notifications.json"
```

This is a file base. With `AJAXBRIDGE_SOURCE_ID=apartment`, the effective path is `/data/apartment/notifications.json`, including when set through an explicit environment or CLI override. Empty source preserves the original path. Each installation has its own notification rules and channel configuration; see [multiple installations](./multiple-installations.md).

An example file is available at:

```text
notifications.example.json
```

## Channels

Channels define where notifications are sent.

| Type | Behavior |
| --- | --- |
| `log` | Writes the notification to AjaxBridge logs. Useful for testing rules. |
| `webhook` | Sends a JSON `POST` payload to `url`. |
| `ntfy` | Sends a plain-text notification to an ntfy topic URL. |

Webhook payload:

```json
{
  "title": "AjaxBridge: Server power high",
  "message": "Server power power_w is 2200.00, above 2000.00",
  "rule": {},
  "event": {},
  "created_at": "2026-05-05T13:30:00Z"
}
```

Example channels:

```json
[
  {
    "id": "log",
    "type": "log"
  },
  {
    "id": "phone",
    "type": "ntfy",
    "url": "https://ntfy.sh/replace-with-private-topic",
    "headers": {
      "Priority": "high"
    }
  },
  {
    "id": "automation",
    "type": "webhook",
    "url": "https://example.local/ajaxbridge-alert",
    "headers": {
      "Authorization": "Bearer replace-me"
    }
  }
]
```

## Rules

Rule fields:

| Field | Purpose |
| --- | --- |
| `id` | Stable rule id. Auto-filled by the admin UI if empty. |
| `name` | Human readable alert name. |
| `enabled` | Enables or disables the rule. |
| `device_slug` | Jeedom device slug. Linked devices usually look like `sia_<account>_zone_<zone>`. |
| `account` | Optional SIA account filter. Usually not needed for linked devices. |
| `zone` | Optional SIA zone filter. Usually not needed for linked devices. |
| `metric` | Jeedom metric, for example `temperature_c`, `voltage_v`, `power_w`, `current_a`, or `state`. |
| `condition` | `above`, `below`, `changed`, `changed_to_on`, `changed_to_off`, `control`, `control_on`, or `control_off`. |
| `threshold` | Numeric threshold for `above` and `below`. |
| `arm_modes` | Allowed SIA modes: `any`, `disarmed`, `armed`, `night`. |
| `channels` | Channel ids. Empty means all configured channels. |
| `cooldown` | Go duration string, for example `0s`, `5m`, `30m`, `1h`. |

Example:

```json
{
  "id": "server_power_high",
  "name": "Server power high",
  "enabled": true,
  "device_slug": "sia_a0f80d_zone_8",
  "metric": "power_w",
  "condition": "above",
  "threshold": 2000,
  "arm_modes": ["any"],
  "channels": ["phone"],
  "cooldown": "30m"
}
```

## Arm State Filtering

Rules can be limited by SIA account mode:

| Mode | Meaning |
| --- | --- |
| `any` | Rule runs in every mode. |
| `disarmed` | Rule runs only when the SIA account is disarmed. |
| `armed` | Rule runs when mode is `armed` or `night`. |
| `night` | Rule runs only in night mode. |

For linked Jeedom devices, AjaxBridge uses `linked_account` from the SIA catalog match. For unlinked Jeedom devices, arm mode is `unknown` unless there is exactly one SIA account in memory.

## Admin Panel

Open:

```text
http://localhost:8080/admin
```

The Notifications tab can edit:

- global enable/disable
- channels
- rules
- device slug matching
- metric/condition/value
- arm modes
- cooldowns
- recent delivery history

Use the `log` channel first to validate rules before sending phone pushes.

## Debug

Useful endpoints:

```text
GET /api/admin/bootstrap
PUT /api/admin/notifications
GET /api/admin/notifications/history?limit=100
GET /jeedom/devices
GET /jeedom/commands
GET /jeedom/actions
```

If a state-change rule does not fire, check whether `/jeedom/commands` contains a binary `state` command for that device and whether `/jeedom/devices/<slug>` changes when the device is toggled.

If a control rule does not fire for an external automation, check whether the automation publishes to `jeedom/cmd/set/<command_id>`. Commands executed only inside Jeedom may not appear on that MQTT topic; in that case the state-change rule is the reliable path.
