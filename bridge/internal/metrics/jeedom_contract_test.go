package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/prometheus/client_golang/prometheus"
)

func TestJeedomPatchDiscoveryTelemetryContract(t *testing.T) {
	// Read the shipped templates so renamed commands and subtype/unit changes
	// exercise the same parser and store mapping as MQTTManager discovery.
	// Values are Jeedom's published values, after its configured calculations.
	type reading struct {
		logicalID, metric, kind string
		value                   any
		want                    float64
		state                   string
	}
	fixtures := []struct {
		model    string
		readings []reading
	}{
		{"FireProtectPlus", []reading{
			{"state", "state", "state", "ACTIF", 1, "active"},
			{"smokeAlarmDetected", "smoke_alarm", "boolean", true, 1, ""},
			{"temperatureAlarmDetected", "heat_alarm", "boolean", false, 0, ""},
			{"highTemperatureDiffDetected", "rapid_temperature_rise_alarm", "boolean", true, 1, ""},
			{"coAlarmDetected", "carbon_monoxide_alarm", "boolean", false, 0, ""},
		}},
		{"SuperiorRexG3", []reading{
			{"state", "state", "state", "RADIO_CHANNEL_TEST", 1, "radio_channel_test"},
			{"radioConnectionOk", "radio_connection", "boolean", true, 1, ""},
			{"dataChannelOk", "photo_channel_connection", "boolean", false, 0, ""},
			{"dataChannelSignalQuality", "photo_channel_signal", "state", "MEDIUM", 1, "medium"},
			{"networkDetails::ethernet::enabled", "ethernet_enabled", "boolean", true, 1, ""},
			{"networkDetails::ethernet::connectionOk", "ethernet_connected", "boolean", false, 0, ""},
			{"jewellerAntennaStatus", "jeweller_antenna_status", "state", "CONNECTED", 1, "connected"},
			{"wingsAntennaStatus", "wings_antenna_status", "state", "ANTENNA_DAMAGED", 1, "damaged"},
		}},
		{"MultiTransmitter", []reading{
			{"state", "state", "state", "PASSIVE", 1, "passive"},
			{"externallyPowered", "external_power", "boolean", true, 1, ""},
			{"charging", "battery_charging", "boolean", false, 0, ""},
			{"batteryMalfunction", "battery_fault", "boolean", false, 0, ""},
			{"externalDevicePowerFailure", "detector_power_fault", "boolean", true, 1, ""},
			{"externalFireAlarmPowerFailure", "fire_detector_power_fault", "boolean", false, 0, ""},
		}},
		{"LifeQualityLite", []reading{
			{"actualTemperature", "temperature_c", "value", -5, -5, ""},
			{"actualHumidity", "humidity_percent", "value", 45, 45, ""},
			{"issuesCount", "issue_count", "value", 0, 0, ""},
		}},
		{"WaterStop", []reading{
			{"valveState", "valve_position", "state", "INTERMEDIATE_STATE", 1, "intermediate"},
		}},
		{"Button", []reading{
			{"buttonMode", "operating_mode", "state", "SMART_BUTTON", 1, "smart_button"},
			{"batteryPingStatus", "battery_check_status", "state", "NOT_OK", 1, "not_ok"},
			{"lastUpdateTimeSeconds", "device_last_update", "timestamp_seconds", 1720000000, 1720000000, ""},
			{"firmwareVersion", "firmware_version", "firmware_info", "5.54.1.0", 1, "5.54.1.0"},
		}},
		{"WallSwitch", []reading{
			{"powerWtH", "energy_wh", "value", 7250, 7250, ""},
			{"currentMA", "current_ma", "value", 1250, 1250, ""},
		}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.model, func(t *testing.T) {
			var template struct {
				Commands []map[string]any `json:"commands"`
			}
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "Jeedom", "files", "core", "config", "devices", fixture.model+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &template); err != nil {
				t.Fatal(err)
			}
			commands := map[string]map[string]any{}
			for i, reading := range fixture.readings {
				id := fmt.Sprint(1000 + i)
				for _, command := range template.Commands {
					if command["logicalId"] != reading.logicalID || command["type"] != "info" {
						continue
					}
					commands[id] = map[string]any{
						"id": id, "name": command["name"], "logicalId": command["logicalId"],
						"type": "info", "subType": command["subtype"], "currentValue": reading.value,
					}
					for _, field := range []string{"unite", "generic_type"} {
						if value, ok := command[field]; ok {
							commands[id][field] = value
						}
					}
				}
				if commands[id] == nil {
					t.Fatalf("template lacks info command %s", reading.logicalID)
				}
			}
			payload := map[string]any{"id": "90", "name": fixture.model, "eqType_name": "ajaxSystem", "configuration": map[string]any{"device": fixture.model}, "cmds": commands}
			store := jeedom.NewStore("unknown")
			registry := prometheus.NewPedanticRegistry()
			New(registry).SetJeedomStore(store)
			applyDiscovery := func(at int64) {
				t.Helper()
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				discovery, err := jeedom.ParseDiscoveryMessage("jeedom/discovery/eqLogic/90", body, time.Unix(at, 0))
				if err != nil {
					t.Fatal(err)
				}
				store.ApplyDiscovery(discovery)
			}
			applyDiscovery(600)
			for i, reading := range fixture.readings {
				id := fmt.Sprint(1000 + i)
				labels := map[string]string{"command_id": id, "metric": reading.metric}
				if reading.kind == "state" {
					labels["state"] = reading.state
				} else if reading.kind == "firmware_info" {
					labels["version"] = reading.state
				}
				assertJeedomGauge(t, registry, "ajax_jeedom_command_"+reading.kind, labels, reading.want)
				assertJeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"command_id": id}, 600)
				delete(commands[id], "currentValue")
			}
			if fixture.model == "WallSwitch" {
				assertJeedomGauge(t, registry, "ajax_jeedom_device_current_amperes", nil, 1.25)
				assertJeedomAbsent(t, registry, "ajax_jeedom_device_power_watts", nil)
			}
			// Metadata-only discovery advances receipt time, never observation time.
			applyDiscovery(660)
			for i, reading := range fixture.readings {
				id := fmt.Sprint(1000 + i)
				labels := map[string]string{"command_id": id}
				assertJeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", labels, 600)
				assertJeedomGauge(t, registry, "ajax_jeedom_last_received_timestamp_seconds", labels, 660)
				eventBody, _ := json.Marshal(map[string]any{"value": nil, "humanName": fmt.Sprintf("[Fixture][%s][%s]", fixture.model, commands[id]["name"])})
				event, err := jeedom.ParseMessage("jeedom/cmd/event/"+id, eventBody, time.Unix(720, 0))
				if err != nil {
					t.Fatal(err)
				}
				store.Apply(event)
				assertJeedomAbsent(t, registry, "ajax_jeedom_command_"+reading.kind, labels)
				assertJeedomAbsent(t, registry, "ajax_jeedom_last_update_timestamp_seconds", labels)
				assertJeedomAbsent(t, registry, "ajax_jeedom_value_age_seconds", labels)
				assertJeedomGauge(t, registry, "ajax_jeedom_last_received_timestamp_seconds", labels, 720)
			}
		})
	}
}

func TestJeedomBoundedEnumsAndFirmwareChanges(t *testing.T) {
	store := jeedom.NewStore("unknown")
	registry := prometheus.NewPedanticRegistry()
	New(registry).SetJeedomStore(store)
	for i, test := range []struct{ name, value, metric, state string }{
		{"Mode", "PANIC_BUTTON", "operating_mode", "panic_button"},
		{"Mode", "INTERCONNECT_DELAY", "operating_mode", "interconnect_delay"},
		{"Operating state", "DEVICE_IS_INSTALLING_FIRMWARE", "operating_state", "device_is_installing_firmware"},
		{"Etat", "WAIT_RADIO_CONNECTION_TEST_END", "state", "wait_radio_connection_test_end"},
		{"Antenne Jeweller", "ANTENNA_DISCONNECTED", "jeweller_antenna_status", "disconnected"},
		{"Antenne Wings", "damaged", "wings_antenna_status", "damaged"},
		{"Antenne Wings", "unknown arbitrary message", "wings_antenna_status", "unknown"},
		{"Signal canal photo", "VERY_LOW", "photo_channel_signal", "very_low"},
		{"Signal canal photo", "ABSENT", "photo_channel_signal", "absent"},
		{"Data channel signal", "HIGH", "data_channel_signal", "high"},
	} {
		value, _ := json.Marshal(test.value)
		id := fmt.Sprint(100 + i)
		store.Apply(jeedom.Event{CommandID: id, DeviceName: "Fixture", CommandName: test.name, Subtype: "string", Value: value, ReceivedAt: time.Unix(600, 0)})
		assertJeedomGauge(t, registry, "ajax_jeedom_command_state", map[string]string{"command_id": id, "metric": test.metric, "state": test.state}, 1)
		assertJeedomAbsent(t, registry, "ajax_jeedom_command_state", map[string]string{"state": "unknown_arbitrary_message"})
	}
	for _, version := range []string{"5.54.1.0", "6.0", "123456.123456.123456.123456.123456.123456"} {
		value, _ := json.Marshal(version)
		store.Apply(jeedom.Event{CommandID: "2", DeviceName: "Fixture", CommandName: "Firmware version", Value: value, ReceivedAt: time.Unix(700, 0)})
		assertJeedomGauge(t, registry, "ajax_jeedom_command_firmware_info", map[string]string{"version": version}, 1)
		if version != "5.54.1.0" {
			assertJeedomAbsent(t, registry, "ajax_jeedom_command_firmware_info", map[string]string{"version": "5.54.1.0"})
		}
	}
	for _, value := range []any{nil, true, "123", "5.5 release note", "5.5-beta", "1.2.3.4.5.6.7", "1234567.1", strings.Repeat("5.5", 100)} {
		raw, _ := json.Marshal(value)
		store.Apply(jeedom.Event{CommandID: "2", DeviceName: "Fixture", CommandName: "Firmware version", Value: raw, ReceivedAt: time.Unix(800, 0)})
		assertJeedomAbsent(t, registry, "ajax_jeedom_command_firmware_info", map[string]string{"command_id": "2"})
		assertJeedomAbsent(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "2"})
		assertJeedomAbsent(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"command_id": "2"})
	}
}

func TestJeedomUnverifiedEnergyStaysRawInPrometheus(t *testing.T) {
	store := jeedom.NewStore("unknown")
	registry := prometheus.NewPedanticRegistry()
	New(registry).SetJeedomStore(store)
	discovery, err := jeedom.ParseDiscoveryMessage("jeedom/discovery/eqLogic/90", []byte(`{"id":90,"name":"Energy fixture","cmds":{"1000":{"id":1000,"name":"Energy","type":"info","subType":"numeric","unite":"","currentValue":18.5}}}`), time.Unix(600, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	assertJeedomGauge(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "1000", "metric": "energy_raw"}, 18.5)
	assertJeedomAbsent(t, registry, "ajax_jeedom_command_value", map[string]string{"metric": "energy_kwh"})
	assertJeedomAbsent(t, registry, "ajax_jeedom_device_power_watts", nil)
	assertJeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"command_id": "1000"}, 600)
}
