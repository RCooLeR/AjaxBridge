package jeedom

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

type electricalDiscoveryMQTT struct {
	recordingMQTT
	canonicalTopic string
	tombstones     int
}

func (m *electricalDiscoveryMQTT) PublishDiscoveryMessage(ctx context.Context, key, topic string, payload []byte, retain bool) error {
	if topic == m.canonicalTopic && len(payload) == 0 {
		m.tombstones++
	}
	return m.recordingMQTT.PublishDiscoveryMessage(ctx, key, topic, payload, retain)
}

func TestPublishVerifiedElectricalUnknownKeepsIdentityAcrossRemapAndRestart(t *testing.T) {
	for _, tc := range []struct{ id, logical, unit, metric, class string }{
		{"189", "currentMA", "mA", "current_ma", "current"},
		{"197", "currentMA", "A", "current_a", "current"},
		{"205", "currentMA", "μA", "current_ua", "current"},
		{"204", "power", "W", "power_w", "power"},
		{"346", "power", "kW", "power_kw", "power"},
		{"188", "powerWtH", "Wh", "energy_wh", "energy"},
		{"221", "powerWtH", "kWh", "energy_kwh", "energy"},
	} {
		t.Run(tc.metric, func(t *testing.T) {
			store := NewStore("keep_last")
			store.SetPath(filepath.Join(t.TempDir(), "jeedom.json"))
			topic := "homeassistant/sensor/ajaxbridge/jeedom_cmd_" + tc.id + "/config"
			mqtt := &electricalDiscoveryMQTT{canonicalTopic: topic}
			publisher := NewPublisher(PublisherConfig{Discovery: true, RetainDiscovery: true, RetainState: true}, mqtt)
			apply := func(unit, value string, at int64) Device {
				generic := "POWER"
				if tc.logical == "currentMA" {
					generic = "CURRENT"
				}
				payload := fmt.Sprintf(`{"id":20,"name":"Fixture","configuration":{"device":"WallSwitch"},"cmds":{%q:{"id":%q,"logicalId":%q,"generic_type":%q,"name":"Custom electrical reading","type":"info","subType":"numeric","unite":%q,"currentValue":%s}}}`, tc.id, tc.id, tc.logical, generic, unit, value)
				discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/20", []byte(payload), time.Unix(at, 0))
				if err != nil {
					t.Fatal(err)
				}
				return store.ApplyDiscovery(discovery).Device
			}
			publish := func(device Device) DiscoveryConfig {
				t.Helper()
				if err := publisher.PublishDevice(t.Context(), device); err != nil {
					t.Fatal(err)
				}
				var cfg DiscoveryConfig
				if err := json.Unmarshal([]byte(mqtt.discovery[topic]), &cfg); err != nil {
					t.Fatalf("missing canonical discovery: %v", err)
				}
				if cfg.UniqueID != "ajaxbridge_jeedom_cmd_"+tc.id || !mqtt.discoveryRetain[topic] || mqtt.tombstones != 0 {
					t.Fatalf("entity identity removed or replaced: config=%#v tombstones=%d", cfg, mqtt.tombstones)
				}
				return cfg
			}

			// Unitless unknown values stay diagnostic, without invented units/classes.
			before := publish(apply("", "null", 100))
			if before.UnitOfMeasurement != "" || before.DeviceClass != "" || before.StateClass != "" {
				t.Fatalf("raw unknown acquired a physical contract: %#v", before)
			}
			device := apply(tc.unit, "null", 200)
			for cycle := range 3 {
				cfg := publish(device)
				command := device.RawCommands[tc.id]
				if cfg.UniqueID != before.UniqueID || cfg.StateTopic != before.StateTopic || cfg.UnitOfMeasurement != tc.unit || cfg.DeviceClass != tc.class {
					t.Fatalf("cycle %d changed identity/contract: %#v", cycle, cfg)
				}
				if command.Metric != tc.metric || command.Value != nil || !command.LastValueAt.IsZero() || !command.SourceUnitKnown {
					t.Fatalf("cycle %d invented a sample: %#v", cycle, command)
				}
				var state map[string]any
				if err := json.Unmarshal([]byte(mqtt.state[cfg.StateTopic]), &state); err != nil {
					t.Fatal(err)
				}
				if value, present := state[tc.metric]; !present || value != nil {
					t.Fatalf("cycle %d expected explicit unknown, got %#v", cycle, state)
				}
				if err := store.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
				restored, err := LoadStore(t.Context(), store.path, "keep_last", nil)
				if err != nil {
					t.Fatal(err)
				}
				store = restored
				device, _ = store.Device(device.DeviceSlug)
				publisher = NewPublisher(PublisherConfig{Discovery: true, RetainDiscovery: true, RetainState: true}, mqtt)
			}
			fresh := apply(tc.unit, "0", 300)
			publish(fresh)
			if fresh.RawCommands[tc.id].Value != float64(0) || !fresh.RawCommands[tc.id].LastValueAt.Equal(time.Unix(300, 0)) {
				t.Fatalf("fresh numeric zero was not accepted: %#v", fresh.RawCommands[tc.id])
			}
		})
	}
}

func TestPublishUnknownElectricalRequiresMatchingVerifiedSourceContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command Command
	}{
		{"missing provenance", Command{Metric: "current_ma", Unit: "mA", DeviceClass: "current", StateClass: "measurement"}},
		{"explicit blank source", Command{Metric: "current_ma", Unit: "mA", SourceUnitKnown: true, DeviceClass: "current", StateClass: "measurement"}},
		{"mismatched source unit", Command{Metric: "current_ma", Unit: "mA", SourceUnit: "A", SourceUnitKnown: true, DeviceClass: "current", StateClass: "measurement"}},
		{"mismatched energy metric", Command{Metric: "power_w", Unit: "Wh", SourceUnit: "Wh", SourceUnitKnown: true, DeviceClass: "energy", StateClass: "total_increasing"}},
		{"unrelated temperature", Command{Metric: "temperature_c", Unit: "°C", SourceUnit: "°C", SourceUnitKnown: true, DeviceClass: "temperature", StateClass: "measurement"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := tc.command
			command.CommandID, command.Component = "189", ComponentSensor
			device := Device{DeviceSlug: "fixture", Values: map[string]any{}, RawCommands: map[string]Command{"189": command}}
			topic := "homeassistant/sensor/ajaxbridge/jeedom_cmd_189/config"
			mqtt := &electricalDiscoveryMQTT{canonicalTopic: topic}
			publisher := NewPublisher(PublisherConfig{Discovery: true, RetainDiscovery: true}, mqtt)
			if err := publisher.PublishDevice(t.Context(), device); err != nil {
				t.Fatal(err)
			}
			if mqtt.tombstones != 1 || mqtt.discovery[topic] != "" || !mqtt.discoveryRetain[topic] {
				t.Fatalf("unverified/unsupported measurement bypassed existing cleanup: %#v", mqtt)
			}
		})
	}
}
