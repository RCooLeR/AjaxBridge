package metrics

import (
	"math"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/prometheus/client_golang/prometheus"
)

// jeedomCollector reads one atomic Store snapshot per scrape. It does not retain
// samples: ownership changes, command removal and unknown values take effect on
// the next scrape, including changes made by discovery or cache reconciliation.
type jeedomCollector struct {
	store           atomic.Pointer[jeedom.Store]
	commandValue    *prometheus.Desc
	commandBoolean  *prometheus.Desc
	commandState    *prometheus.Desc
	commandFirmware *prometheus.Desc
	commandTime     *prometheus.Desc
	lastValue       *prometheus.Desc
	lastReceived    *prometheus.Desc
	valueAge        *prometheus.Desc
	deviceValues    map[string]*prometheus.Desc
}

// SetJeedomStore binds the canonical observation source, including an already
// restored cache. No MQTT event is required to populate the first scrape.
func (m *Metrics) SetJeedomStore(store *jeedom.Store) {
	m.jeedomSnapshot.store.Store(store)
}

func newJeedomCollector() *jeedomCollector {
	labels := []string{"device", "command", "command_id", "metric"}
	desc := func(name, help, unit string, labels []string) *prometheus.Desc {
		return prometheus.V2.NewDesc(name, help, prometheus.UnconstrainedLabels(labels), nil, prometheus.WithUnit(unit))
	}
	return &jeedomCollector{
		commandValue:    desc("ajax_jeedom_command_value", "Last numeric value reported by a Jeedom command.", "", labels),
		commandBoolean:  desc("ajax_jeedom_command_boolean", "Last boolean Jeedom command value: 1 for true, 0 for false; polarity follows the named metric.", "", labels),
		commandState:    desc("ajax_jeedom_command_state", "One-hot state of an allowlisted Jeedom enum command; unknown includes unrecognized states.", "", append(append([]string(nil), labels...), "state")),
		commandFirmware: desc("ajax_jeedom_command_firmware_info", "Reported Jeedom firmware version; only bounded dotted numeric versions are exported.", "", append(append([]string(nil), labels...), "version")),
		commandTime:     desc("ajax_jeedom_command_timestamp_seconds", "Unix timestamp reported as the value of a Jeedom timestamp command.", "seconds", labels),
		lastValue:       desc("ajax_jeedom_last_update_timestamp_seconds", "Unix timestamp for the last Jeedom command update.", "seconds", labels),
		lastReceived:    desc("ajax_jeedom_last_received_timestamp_seconds", "Unix timestamp for the last received Jeedom command value or metadata update.", "seconds", labels),
		valueAge:        desc("ajax_jeedom_value_age_seconds", "Seconds since the current Jeedom command value was observed by the bridge.", "seconds", labels),
		deviceValues: map[string]*prometheus.Desc{
			"power_w":         desc("ajax_jeedom_device_power_watts", "Last Jeedom power value per device.", "watts", []string{"device"}),
			"current_a":       desc("ajax_jeedom_device_current_amperes", "Last Jeedom current value per device.", "amperes", []string{"device"}),
			"voltage_v":       desc("ajax_jeedom_device_voltage_volts", "Last Jeedom voltage value per device.", "volts", []string{"device"}),
			"temperature_c":   desc("ajax_jeedom_device_temperature_celsius", "Last Jeedom temperature value per device.", "celsius", []string{"device"}),
			"battery_percent": desc("ajax_jeedom_device_battery_percent", "Last Jeedom battery percent value per device.", "percent", []string{"device"}),
		},
	}
}

func (c *jeedomCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.commandValue, c.commandBoolean, c.commandState, c.commandFirmware, c.commandTime, c.lastValue, c.lastReceived, c.valueAge} {
		ch <- desc
	}
	for _, desc := range c.deviceValues {
		ch <- desc
	}
}

func (c *jeedomCollector) Collect(ch chan<- prometheus.Metric) {
	store := c.store.Load()
	if store == nil {
		return
	}
	devices := store.Devices()
	now := time.Now()
	for _, device := range devices {
		for metric, desc := range c.deviceValues {
			if number, ok := jeedomDeviceNumber(device.Values, metric); ok {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, number, device.DeviceSlug)
			}
		}
		for _, command := range device.RawCommands {
			labels := []string{device.DeviceSlug, command.Name, command.CommandID, command.Metric}
			if !command.LastUpdate.IsZero() {
				ch <- prometheus.MustNewConstMetric(c.lastReceived, prometheus.GaugeValue, timestamp(command.LastUpdate), labels...)
			}
			if !c.collectValue(ch, command, labels) {
				continue
			}
			// LastUpdate also changes on discovery/empty messages. It must not
			// make a retained value look fresh, or substitute for an unknown
			// observation time in an old cache.
			if !command.LastValueAt.IsZero() {
				ch <- prometheus.MustNewConstMetric(c.lastValue, prometheus.GaugeValue, timestamp(command.LastValueAt), labels...)
				ch <- prometheus.MustNewConstMetric(c.valueAge, prometheus.GaugeValue, math.Max(0, now.Sub(command.LastValueAt).Seconds()), labels...)
			}
		}
	}
}

// Canonical prefixed units have explicit source provenance. Convert only these
// declared units for the existing base-unit device series, never raw readings.
func jeedomDeviceNumber(values map[string]any, metric string) (float64, bool) {
	if value, exists := values[metric]; exists {
		if number, ok := finiteJeedomNumber(value); ok {
			return number, true
		}
	}
	var alternatives []struct {
		metric string
		scale  float64
	}
	switch metric {
	case "current_a":
		alternatives = []struct {
			metric string
			scale  float64
		}{{"current_ma", 0.001}, {"current_ua", 0.000001}}
	case "power_w":
		alternatives = []struct {
			metric string
			scale  float64
		}{{"power_kw", 1000}, {"power_mw", 0.001}}
	}
	for _, alternative := range alternatives {
		if value, exists := values[alternative.metric]; exists {
			if number, ok := finiteJeedomNumber(value); ok {
				if converted, finite := finiteJeedomNumber(number * alternative.scale); finite {
					return converted, true
				}
			}
		}
	}
	return 0, false
}

func (c *jeedomCollector) collectValue(ch chan<- prometheus.Metric, command jeedom.Command, labels []string) bool {
	if command.Metric == "firmware_version" {
		version, ok := command.Value.(string)
		if !ok || len(version) > 41 || !jeedomFirmwareVersion.MatchString(version) {
			return false
		}
		ch <- prometheus.MustNewConstMetric(c.commandFirmware, prometheus.GaugeValue, 1, append(labels, version)...)
		return true
	}
	if number, ok := finiteJeedomNumber(command.Value); ok {
		ch <- prometheus.MustNewConstMetric(c.commandValue, prometheus.GaugeValue, number, labels...)
		return true
	}
	if value, ok := command.Value.(bool); ok {
		ch <- prometheus.MustNewConstMetric(c.commandBoolean, prometheus.GaugeValue, boolFloat(value), labels...)
		return true
	}
	value, ok := command.Value.(string)
	if !ok || strings.TrimSpace(value) == "" {
		return false
	}
	if command.DeviceClass == "timestamp" {
		if at, err := time.Parse(time.RFC3339Nano, value); err == nil && !at.IsZero() {
			ch <- prometheus.MustNewConstMetric(c.commandTime, prometheus.GaugeValue, float64(at.Unix())+float64(at.Nanosecond())/1e9, labels...)
			return true
		}
		return false
	}
	states := jeedomEnumStates[command.Metric]
	if len(states) == 0 {
		return false
	}
	value = strings.Join(strings.Fields(strings.ReplaceAll(strings.ToLower(value), "-", "_")), "_")
	// The plugin keeps these official API spellings. Reuse the existing valve
	// label and common antenna states without growing labels from raw payloads.
	if command.Metric == "valve_position" && value == "intermediate_state" {
		value = "intermediate"
	}
	if command.Metric == "jeweller_antenna_status" || command.Metric == "wings_antenna_status" {
		value = strings.TrimPrefix(value, "antenna_")
	}
	active := "unknown"
	for _, state := range states {
		if value == state {
			active = state
			break
		}
	}
	for _, state := range states {
		ch <- prometheus.MustNewConstMetric(c.commandState, prometheus.GaugeValue, boolFloat(state == active), append(labels, state)...)
	}
	return true
}

// Firmware is an info metric, never a numeric or health reading. Deliberately
// accept only 2–6 numeric components of at most six digits (41 bytes total).
// Unsupported firmware formats and free-form text cannot become version labels.
var jeedomFirmwareVersion = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,6}){1,5}$`)

// Keep metric names and state labels bounded. Official Ajax API1.152 definitions:
// https://api.ajax.systems/api/swagger/history/1.152.0/swagger.yaml
// DeviceState (6234), ButtonBase.buttonMode/batteryPingStatus (11456/11512),
// AntennaStatus (5188), SuperiorRexG3.*AntennaStatus (14973).
// Photo/data quality uses several distinct API enums (7200, 14887, 15871);
// retain their tokens without inventing equivalence between quality scales.
// The patch's logicalId=state/label=Etat retains the canonical metric "state".
var jeedomDeviceStates = []string{
	"passive", "active", "detection_area_test", "radio_connection_test",
	"wait_radio_connection_test_start", "wait_radio_connection_test_end",
	"wait_detection_area_test_start", "wait_detection_area_test_end",
	"wait_registration", "wait_radio_channel_test_start", "radio_channel_test",
	"wait_radio_channel_test_end", "calibration_in_progress",
	"maximum_bus_power_consumption_test_in_progress",
	"device_is_in_file_receiving_mode_wings", "device_is_installing_firmware",
	"self_test_in_progress", "unknown",
}

var jeedomEnumStates = map[string][]string{
	"battery_state":           {"charged", "charging", "discharged", "low", "empty", "unknown"},
	"battery_check_status":    {"ok", "not_ok", "failed", "in_progress", "not_performed", "unknown"},
	"valve_position":          {"open", "closed", "intermediate", "opening", "closing", "unknown"},
	"signal_level":            {"no_signal", "weak", "normal", "strong", "unknown"},
	"photo_channel_signal":    {"no_signal", "weak", "normal", "strong", "absent", "very_low", "low", "medium", "high", "unknown"},
	"data_channel_signal":     {"no_signal", "weak", "normal", "strong", "absent", "very_low", "low", "medium", "high", "unknown"},
	"operating_mode":          {"panic_button", "smart_button", "interconnect_delay", "unknown"},
	"operating_state":         jeedomDeviceStates,
	"state":                   jeedomDeviceStates,
	"jeweller_antenna_status": {"connected", "disconnected", "damaged", "unknown"},
	"wings_antenna_status":    {"connected", "disconnected", "damaged", "unknown"},
}

func finiteJeedomNumber(value any) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case float32:
		number = float64(value)
	case int:
		number = float64(value)
	case int64:
		number = float64(value)
	case int32:
		number = float64(value)
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}
