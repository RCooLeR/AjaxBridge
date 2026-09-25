# Prometheus

AjaxBridge exposes Prometheus metrics on the HTTP server.

Default endpoint:

```text
http://localhost:8080/metrics
```

The endpoint is enabled whenever the HTTP server is enabled. Configure the HTTP listener with `AJAXBRIDGE_HTTP_ADDR`, default `:8080`.

## Scrape Config

Example Prometheus target:

```yaml
scrape_configs:
  - job_name: ajaxbridge
    static_configs:
      - targets:
          - ajaxbridge:8080
```

Use the hostname that Prometheus can reach. If Prometheus runs outside Docker, that may be `HOST_IP:8080`.

## SIA Ingestion Metrics

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `ajax_sia_events_total` | counter | `account`, `event_code`, `event_class`, `parse_status` | Total SIA events seen by the parser. |
| `ajax_sia_parse_errors_total` | counter | `reason` | Total rejected or degraded SIA frames by parse status. |

`parse_status` and `reason` values include:

- `ok`
- `crc_invalid`
- `length_invalid`
- `format_invalid`
- `account_invalid`
- `decrypt_invalid`

## SIA Forwarding Metrics

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `ajax_sia_forward_total` | counter | `target`, `status` | Forward attempts to an upstream SIA receiver. |
| `ajax_sia_forward_duration_seconds` | histogram | `target`, `status` | Duration of SIA forwarding attempts. |

Forwarding metrics are emitted only when `AJAXBRIDGE_FORWARD_ADDR` is set.

## Go Runtime Metrics

AjaxBridge exports a focused set of Go scheduler metrics in addition to its application metrics:

| Metric | Meaning |
| --- | --- |
| `go_sched_goroutines_created_goroutines_total` | Goroutines created since process start. |
| `go_sched_goroutines_not_in_go_goroutines` | Goroutines executing outside Go code. |
| `go_sched_goroutines_runnable_goroutines` | Goroutines waiting for a processor. |
| `go_sched_goroutines_running_goroutines` | Goroutines currently running Go code. |
| `go_sched_goroutines_waiting_goroutines` | Goroutines blocked on runtime resources. |
| `go_sched_threads_total_threads` | Threads created by the Go runtime. |

These series make scheduler pressure and goroutine growth visible without exporting the full Go runtime metric set. Metric families with physical units also expose OpenMetrics unit metadata.

## Account Metrics

All account gauges have label `account`.

| Metric | Value |
| --- | --- |
| `ajax_account_online` | `1` when the account is online, otherwise `0`. |
| `ajax_account_armed` | `1` when armed, otherwise `0`. |
| `ajax_account_night_mode` | `1` when night mode is active. |
| `ajax_account_partially_armed` | `1` when partially armed. |
| `ajax_account_alarm_active` | `1` when any account alarm is active. |
| `ajax_account_tamper_active` | `1` when any account tamper is active. |
| `ajax_account_trouble_active` | `1` when any account trouble is active. |
| `ajax_account_last_event_timestamp_seconds` | Unix timestamp of the last event. |
| `ajax_account_last_ping_timestamp_seconds` | Unix timestamp of the last ping/test event. |

## Zone Metrics

Zone gauges use these labels:

| Label | Meaning |
| --- | --- |
| `account` | SIA account/object number. |
| `partition` | SIA partition/area or `unknown`. |
| `group` | SIA group or `unknown`. |
| `zone` | SIA zone. |
| `device` | Ajax device id, or zone when missing. |
| `device_name` | Catalog device name or fallback. |
| `room` | Catalog room or `unknown`. |
| `device_kind` | Catalog kind or inferred kind. |
| `device_events` | Comma-separated catalog signal list. |
| `alarm_signal` | Alarm signal label for alarm metrics. |
| `alarm_action` | Alarm action label for alarm metrics. |

| Metric | Extra labels | Value |
| --- | --- | --- |
| `ajax_zone_alarm_active` | `alarm_signal`, `alarm_action` | `1` when the zone alarm is active. |
| `ajax_zone_alarm_last_event_timestamp_seconds` | `alarm_signal`, `alarm_action` | Unix timestamp of the active/last zone alarm event. |
| `ajax_zone_tamper_active` | none | `1` when zone tamper is active. |
| `ajax_zone_tamper_last_event_timestamp_seconds` | none | Unix timestamp of the last tamper event. |
| `ajax_zone_trouble_active` | none | `1` when zone trouble is active. |
| `ajax_zone_last_event_timestamp_seconds` | none | Unix timestamp of the last event for the zone. |

## Jeedom Metrics

When `AJAXBRIDGE_JEEDOM_ENABLED=true`, every scrape reads one consistent snapshot of the Jeedom store. Cached and discovery values are available on the first scrape without waiting for a live MQTT event. Removed commands and previous owners disappear on the next scrape; counters still describe messages received by the current process.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `ajax_jeedom_mqtt_messages_total` | counter | none | Total Jeedom MQTT messages received by the Jeedom service. |
| `ajax_jeedom_mqtt_parse_errors_total` | counter | none | Total Jeedom MQTT messages that could not be parsed. |
| `ajax_jeedom_empty_values_total` | counter | none | Jeedom messages with empty or null values. |
| `ajax_jeedom_last_update_timestamp_seconds` | gauge | `device`, `command`, `command_id`, `metric` | `LastValueAt`: when the bridge observed the currently exported value. |
| `ajax_jeedom_last_received_timestamp_seconds` | gauge | `device`, `command`, `command_id`, `metric` | Last command value or metadata receipt, including empty messages and discovery refreshes. |
| `ajax_jeedom_value_age_seconds` | gauge | `device`, `command`, `command_id`, `metric` | Age of the currently exported observation at scrape time. |
| `ajax_jeedom_command_value` | gauge | `device`, `command`, `command_id`, `metric` | Last numeric value for a Jeedom command. |
| `ajax_jeedom_command_boolean` | gauge | `device`, `command`, `command_id`, `metric` | Canonical boolean: `1` for true and `0` for false. Missing/unknown values are absent. |
| `ajax_jeedom_command_state` | gauge | `device`, `command`, `command_id`, `metric`, `state` | One-hot enum: `1` for the current state, `0` for the other supported states. |
| `ajax_jeedom_command_firmware_info` | gauge | `device`, `command`, `command_id`, `metric`, `version` | `1` for the current validated firmware version. Only bounded dotted numeric versions are exported. |
| `ajax_jeedom_command_timestamp_seconds` | gauge | `device`, `command`, `command_id`, `metric` | Timestamp reported as a command value, for example `device_last_update`. This is distinct from bridge receipt time. |
| `ajax_jeedom_device_power_watts` | gauge | `device` | Latest device power in watts. |
| `ajax_jeedom_device_current_amperes` | gauge | `device` | Latest device current in amperes. |
| `ajax_jeedom_device_voltage_volts` | gauge | `device` | Latest device voltage in volts. |
| `ajax_jeedom_device_temperature_celsius` | gauge | `device` | Latest device temperature in Celsius. |
| `ajax_jeedom_device_battery_percent` | gauge | `device` | Latest device battery percentage. |

Jeedom `command` labels are translated to English. The underlying debug JSON still keeps `raw_name`.

Numeric command values come from the canonical store and are not rescaled during export. Missing values and non-finite numbers are omitted, never replaced with zero. A measured zero is exported normally. With `keep_last`, an empty message preserves the last value and its original `LastValueAt`; with `unknown`, the value and its value-age series disappear. A metadata refresh or restart does not reset value freshness. Old caches without `LastValueAt` can expose a value without a value timestamp or age: metadata receipt time is not a reliable substitute.

Unitless/unverified electrical readings remain in `ajax_jeedom_command_value` as `current_raw`, `power_raw`, or `energy_raw`; they do not produce amperes/watts device series or a guessed energy scale. Legacy caches with ambiguous unit defaults are conservatively demoted to raw without changing their numbers until the source supplies explicit units. Verified `mA`/`µA` and `kW`/`mW` remain unchanged in generic command series; the existing device series convert those declared prefixes to amperes/watts when a base-unit observation is absent. Command names, logical IDs, and value magnitude never justify a physical unit or guessed divisor.

Value timestamps describe when the bridge received an observation, not necessarily when the device measured it. A retained MQTT replay can only provide the measurement time if the source explicitly reports one, such as a `device_last_update` command. Age is clamped to zero for future observation timestamps caused by clock changes; the original timestamp is preserved.

Boolean polarity follows the `metric` label. For example, `online`, `radio_connection`, and `external_power` are `1` when available/connected/powered; `battery_fault`, `tamper`, and `smoke_alarm` are `1` when the condition is active. An operational `state=1` is on, not an alarm. Boolean diagnostics are exported independently of the SIA security aggregate.

Only the following string enums become state labels. Tokens are case-insensitive; spaces and hyphens normalize to underscores. Any unrecognized nonempty token selects `unknown`, so arbitrary payloads cannot create new labels or appear healthy. Missing/null values have no state series. The new operating and antenna states follow the [official Ajax API 1.152 schema](https://api.ajax.systems/api/swagger/history/1.152.0/swagger.yaml): `DeviceState`, `ButtonBase.buttonMode`, `ButtonBase.batteryPingStatus`, `AntennaStatus`, and `SuperiorRexG3` antenna fields.

| `metric` | Supported `state` labels |
| --- | --- |
| `battery_state` | `charged`, `charging`, `discharged`, `low`, `empty`, `unknown` |
| `battery_check_status` | `ok`, `not_ok`, `failed`, `in_progress`, `not_performed`, `unknown` |
| `valve_position` | `open`, `closed`, `intermediate`, `opening`, `closing`, `unknown` |
| `signal_level` | `no_signal`, `weak`, `normal`, `strong`, `unknown` |
| `photo_channel_signal`, `data_channel_signal` | `no_signal`, `weak`, `normal`, `strong`, `absent`, `very_low`, `low`, `medium`, `high`, `unknown` |
| `operating_mode` | `panic_button`, `smart_button`, `interconnect_delay`, `unknown` |
| `jeweller_antenna_status`, `wings_antenna_status` | `connected`, `disconnected`, `damaged`, `unknown` |
| `state` (string), `operating_state` | The device states listed below. Binary `state` continues to use the boolean series. |

Supported device states are `passive`, `active`, `detection_area_test`, `radio_connection_test`, `wait_radio_connection_test_start`, `wait_radio_connection_test_end`, `wait_detection_area_test_start`, `wait_detection_area_test_end`, `wait_registration`, `wait_radio_channel_test_start`, `radio_channel_test`, `wait_radio_channel_test_end`, `calibration_in_progress`, `maximum_bus_power_consumption_test_in_progress`, `device_is_in_file_receiving_mode_wings`, `device_is_installing_firmware`, `self_test_in_progress`, and `unknown`. These are operational diagnostics, not security alarm states.

The source token `INTERMEDIATE_STATE` uses the existing valve label `intermediate`. Hub antenna spellings `ANTENNA_CONNECTED`, `ANTENNA_DISCONNECTED`, and `ANTENNA_DAMAGED` use the same labels as their ReX equivalents without the prefix. Photo/data-channel quality retains the tokens of the different Ajax API enums; it does not equate `very_low` with `no_signal` or invent a numeric signal strength.

Firmware is exported separately as `ajax_jeedom_command_firmware_info`, only for canonical `firmware_version` values with 2–6 numeric components separated by dots, at most 6 digits per component and 41 characters total (for example `5.54.1.0`). This is an export restriction, not a claim that Ajax guarantees this format. Unsupported versions and missing values produce no firmware series. A version change removes the old version from the next scrape; normal observation freshness applies. Free-form event text, JSON fault lists, and other unlisted strings are not exported as value labels.

## Query Examples

Accounts offline:

```promql
ajax_account_online == 0
```

Any active alarm:

```promql
ajax_account_alarm_active == 1
```

Active zone alarms with device names:

```promql
ajax_zone_alarm_active == 1
```

SIA parse errors in the last 5 minutes:

```promql
increase(ajax_sia_parse_errors_total[5m])
```

Jeedom parse errors in the last 5 minutes:

```promql
increase(ajax_jeedom_mqtt_parse_errors_total[5m])
```

Power by Jeedom device:

```promql
ajax_jeedom_device_power_watts
```

Energy meter readings in kWh for commands whose verified source unit is Wh. Select the desired time range in Prometheus or Grafana:

```promql
ajax_jeedom_command_value{job="ajaxbridge",metric="energy_wh"} / 1000
```

For a source already reporting kWh, select `metric="energy_kwh"` without dividing. These are cumulative meter readings exported as gauges, not instantaneous power or interval consumption. Do not apply `rate()` or `increase()` directly: those functions assume counters. Meter resets and source corrections need separate handling before calculating consumption.

Current in amperes by device, including sources with verified mA units:

```promql
ajax_jeedom_device_current_amperes{job="ajaxbridge"}
```

Use your configured job name and add a `command_id` or `device` selector from your own metrics when narrowing a query. Typed energy labels require verified source units. Unit corrections do not rewrite historical Prometheus samples: earlier raw or incorrectly labelled A/W samples must not be treated as corrected energy/current history. Unknown readings remain absent rather than zero.

Jeedom observations older than one hour:

```promql
ajax_jeedom_value_age_seconds > 3600
```

Reported offline devices and battery faults:

```promql
ajax_jeedom_command_boolean{metric="online"} == 0
```

```promql
ajax_jeedom_command_boolean{metric="battery_fault"} == 1
```

Battery checks with an unrecognized/unknown status:

```promql
ajax_jeedom_command_state{metric="battery_check_status",state="unknown"} == 1
```

Forward receiver latency:

```promql
histogram_quantile(
  0.95,
  sum(rate(ajax_sia_forward_duration_seconds_bucket[5m])) by (le, target)
)
```

## Dashboard Notes

- Use SIA metrics for alarm/security status.
- Use Jeedom metrics for power, voltage, current, temperature, battery, and signal diagnostics.
- Prefer `account`, `zone`, and `device_name` labels for panels and alerts.
- Timestamps are Unix seconds. SIA may use zero for unseen events; Jeedom omits unknown timestamps.
- Boolean gauges use `1` for true and `0` for false.
