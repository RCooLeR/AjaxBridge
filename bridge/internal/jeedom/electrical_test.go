package jeedom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestElectricalMappingRequiresExplicitCompatibleUnits(t *testing.T) {
	for _, test := range []struct{ name, logicalID, unit, metric, class string }{
		{"Courant", "currentMA", "", "current_raw", ""},
		{"Puissance", "powerWtH", "", "power_raw", ""},
		{"Current", "", "A", "current_a", "current"},
		{"Renamed", "currentMA", "mA", "current_ma", "current"},
		{"Power", "", "W", "power_w", "power"},
		{"Renamed", "powerWtH", "kW", "power_kw", "power"},
	} {
		mapping := MappingFor(Event{CommandName: test.name, LogicalID: test.logicalID, Unit: test.unit})
		if mapping.Metric != test.metric || mapping.Unit != test.unit || mapping.DeviceClass != test.class || !mapping.Numeric {
			t.Fatalf("mapping(%#v) = %#v", test, mapping)
		}
		if test.unit == "" && (mapping.EntityCategory != "diagnostic" || mapping.StateClass != "") {
			t.Fatalf("raw reading is not diagnostic: %#v", mapping)
		}
	}
}

func TestElectricalExplicitBlankOverridesContractButOmittedUnitPreservesIt(t *testing.T) {
	for _, test := range []struct{ name, unit, metric, rawMetric string }{
		{"Courant", "A", "current_a", "current_raw"},
		{"Courant", "mA", "current_ma", "current_raw"},
		{"Puissance", "W", "power_w", "power_raw"},
	} {
		t.Run(test.unit, func(t *testing.T) {
			store := NewStore("keep_last")
			apply := func(unitField string, value string) ApplyResult {
				evt, err := ParseMessage("jeedom/cmd/event/205", []byte(fmt.Sprintf(`{"value":%s,"humanName":"[None][Server][%s]","type":"info","subtype":"numeric"%s}`, value, test.name, unitField)), time.Now())
				if err != nil {
					t.Fatal(err)
				}
				return store.Apply(evt)
			}
			first := apply(fmt.Sprintf(`,"unite":%q`, test.unit), "960")
			if first.Command.Metric != test.metric || !first.Command.SourceUnitKnown || first.Command.SourceUnit != test.unit {
				t.Fatalf("first = %#v", first.Command)
			}
			omitted := apply("", "970")
			if omitted.Command.Metric != test.metric || omitted.Command.Unit != test.unit || omitted.Command.Value != float64(970) {
				t.Fatalf("omitted = %#v", omitted.Command)
			}
			blank := apply(`,"unite":""`, "950")
			if blank.Command.Metric != test.rawMetric || blank.Command.Unit != "" || blank.Command.DeviceClass != "" || blank.Command.Value != float64(950) || !blank.Command.SourceUnitKnown || blank.Command.SourceUnit != "" {
				t.Fatalf("blank = %#v", blank.Command)
			}
			if _, remains := blank.Device.Values[test.metric]; remains {
				t.Fatalf("old physical metric remains: %#v", blank.Device.Values)
			}
			next := apply("", "980")
			if next.Command.Metric != test.rawMetric || next.Command.Value != float64(980) {
				t.Fatalf("value-only resurrected units: %#v", next.Command)
			}
		})
	}
}

func TestElectricalDiscoveryProvenanceAndUnitChanges(t *testing.T) {
	store := NewStore("keep_last")
	apply := func(unit, value string) ApplyDiscoveryResult {
		discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/20", []byte(fmt.Sprintf(`{"id":20,"name":"Server","configuration":{"device":"WallSwitch"},"cmds":{"205":{"id":205,"logicalId":"currentMA","name":"Courant","type":"info","subType":"numeric","unite":%q,"currentValue":%s}}}`, unit, value)), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return store.ApplyDiscovery(discovery)
	}
	raw := apply("", "960")
	if raw.Device.Values["current_raw"] != float64(960) || raw.Device.Values["state"] != nil {
		t.Fatalf("unverified load inferred units/state: %#v", raw.Device.Values)
	}
	verified := apply("mA", "1000")
	if verified.Device.Values["state"] != true {
		t.Fatalf("verified positive mA load did not retain WallSwitch inference: %#v", verified.Device.Values)
	}
	if verified.Device.Values["current_ma"] != float64(1000) || verified.Device.RawCommands["205"].SourceUnit != "mA" {
		t.Fatalf("verified = %#v", verified.Device)
	}
	changed := apply("A", "null")
	command := changed.Device.RawCommands["205"]
	if command.Value != nil || !command.LastValueAt.IsZero() {
		t.Fatalf("old mA sample relabeled as A: %#v", command)
	}
	evt, _ := ParseMessage("jeedom/cmd/event/205", []byte(`{"value":1,"humanName":"[None][Server][Courant]","type":"info","subtype":"numeric"}`), time.Now())
	current := store.Apply(evt)
	if current.Command.Value != float64(1) || current.Command.Metric != "current_a" || current.Command.Unit != "A" {
		t.Fatalf("verified value-only = %#v", current.Command)
	}
	blank := apply("", "null")
	if blank.Device.Values["current_raw"] != float64(1) || blank.Device.RawCommands["205"].DeviceClass != "" {
		t.Fatalf("blank discovery did not demote: %#v", blank.Device)
	}
}

func TestElectricalLegacyCacheDemotionAndVerifiedUnitPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	legacy := `[{"device_slug":"server","device":"Server","raw_commands":{"205":{"command_id":"205","device_slug":"server","name":"Current","raw_name":"Courant","logical_id":"currentMA","metric":"current_a","component":"sensor","subtype":"numeric","unit":"A","device_class":"current","value":960,"last_value_at":"2026-09-24T10:00:00Z"},"204":{"command_id":"204","device_slug":"server","name":"Custom power","metric":"power_w","component":"sensor","subtype":"numeric","unit":"W","device_class":"power","value":78484,"last_value_at":"2026-09-24T10:00:00Z"}}}]`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	for cycle := range 3 {
		store, err := LoadStore(t.Context(), path, "keep_last", nil)
		if err != nil {
			t.Fatal(err)
		}
		device, _ := store.Device("server")
		if device.Values["current_raw"] != float64(960) || device.Values["power_raw"] != float64(78484) {
			t.Fatalf("cycle%d numeric values changed: %#v", cycle, device.Values)
		}
		for _, command := range device.RawCommands {
			if command.Unit != "" || command.DeviceClass != "" || command.SourceUnitKnown {
				t.Fatalf("legacy unit trusted: %#v", command)
			}
		}
		if err := store.Save(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	store, _ := LoadStore(t.Context(), path, "keep_last", nil)
	store.Apply(Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", Unit: "mA", Value: []byte(`1000`), ReceivedAt: time.Now()})
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		store, err := LoadStore(t.Context(), path, "keep_last", nil)
		if err != nil {
			t.Fatal(err)
		}
		device, _ := store.Device("server")
		command := device.RawCommands["205"]
		if command.Metric != "current_ma" || command.Unit != "mA" || command.Value != float64(1000) || !command.SourceUnitKnown || command.SourceUnit != "mA" {
			t.Fatalf("verified unit changed across restart: %#v", command)
		}
		if err := store.Save(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestElectricalBlankUnitReplacesDiscoveryWithoutChangingEntityIdentity(t *testing.T) {
	store := NewStore("keep_last")
	first := store.Apply(Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", Unit: "A", Value: []byte(`1`), ReceivedAt: time.Now()})
	publisher := NewPublisher(PublisherConfig{StateTopicPrefix: "ajaxbridge/jeedom"}, nil)
	_, oldPayload, err := publisher.BuildDiscovery(first.Command, first.Device)
	if err != nil {
		t.Fatal(err)
	}
	blank := store.Apply(Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", UnitProvided: true, Value: []byte(`960`), ReceivedAt: time.Now()})
	_, newPayload, err := publisher.BuildDiscovery(blank.Command, blank.Device)
	if err != nil {
		t.Fatal(err)
	}
	var oldConfig, newConfig map[string]any
	if err := json.Unmarshal(oldPayload, &oldConfig); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(newPayload, &newConfig); err != nil {
		t.Fatal(err)
	}
	if oldConfig["unique_id"] != newConfig["unique_id"] {
		t.Fatal("unit correction changed entity identity")
	}
	for _, field := range []string{"unit_of_measurement", "device_class", "state_class"} {
		if _, exists := newConfig[field]; exists {
			t.Fatalf("raw discovery retains %s: %s", field, newPayload)
		}
	}
	if newConfig["entity_category"] != "diagnostic" {
		t.Fatalf("raw discovery is not diagnostic: %s", newPayload)
	}
}

func TestElectricalDiscoveryDoesNotRelabelFallbackValues(t *testing.T) {
	for _, test := range []struct {
		value string
		want  any
	}{{"null", nil}, {"1", float64(1)}} {
		t.Run(test.value, func(t *testing.T) {
			store := NewStore("keep_last")
			store.Apply(Event{CommandID: "205", DeviceName: "Server", CommandName: "Custom current reading", Type: "info", Subtype: "numeric", Value: []byte(`960`), ReceivedAt: time.Now()})
			discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/20", []byte(fmt.Sprintf(`{"id":20,"name":"Server","configuration":{"device":"WallSwitch"},"cmds":{"205":{"id":205,"logicalId":"currentMA","name":"Custom current reading","type":"info","subType":"numeric","unite":"A","currentValue":%s}}}`, test.value)), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			result := store.ApplyDiscovery(discovery)
			command := result.Device.RawCommands["205"]
			if command.Metric != "current_a" || command.Value != test.want || result.Device.Values["current_a"] != test.want {
				t.Fatalf("old fallback number replaced fresh/unknown current: command=%#v values=%#v", command, result.Device.Values)
			}
			if test.want == nil && (result.Device.Values["state"] != nil || !command.LastValueAt.IsZero()) {
				t.Fatalf("unverified fallback inferred physical state/freshness: %#v", result.Device)
			}
			if test.want != nil && result.Device.Values["state"] != true {
				t.Fatalf("fresh verified load not observed: %#v", result.Device)
			}
		})
	}
}

func TestElectricalLegacyLoadStateNeedsFeedbackOrSuccessfulControlEvidence(t *testing.T) {
	for _, test := range []struct {
		name string
		want any
	}{
		{"unproven_load", nil}, {"failed_request", nil}, {"successful_control", true}, {"observed_state", false}, {"observed_event", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			store := NewStore("keep_last")
			device := &Device{DeviceSlug: "server", Device: "Server", JeedomDeviceType: "WallSwitch", Values: map[string]any{"state": true, "current_a": float64(960)}, RawCommands: map[string]Command{
				"205": {CommandID: "205", DeviceSlug: "server", Name: "Current", RawName: "Courant", Metric: "current_a", Component: ComponentSensor, Subtype: "numeric", Unit: "A", DeviceClass: "current", Value: float64(960), LastUpdate: at, LastValueAt: at},
			}, Actions: map[string]Action{"on": {Action: "on", CommandID: "901", DeviceSlug: "server"}, "off": {Action: "off", CommandID: "902", DeviceSlug: "server"}}}
			store.devices["server"] = device
			switch test.name {
			case "failed_request":
				store.RecordControl(device.Actions["on"], "test", "test", fmt.Errorf("not published"))
			case "successful_control":
				if _, updated := store.RecordOptimisticControlState(device.Actions["on"], at.Add(time.Minute)); !updated {
					t.Fatal("control state not recorded")
				}
			case "observed_state":
				device.RawCommands["206"] = Command{CommandID: "206", DeviceSlug: "server", Name: "State", Metric: "state", Component: ComponentBinarySensor, Subtype: "binary", Value: false, LastValueAt: at}
			case "observed_event":
				device.RawCommands["206"] = Command{CommandID: "206", DeviceSlug: "server", Name: "Event code", Metric: "event_code", Component: ComponentSensor, Subtype: "string", Value: "M_1F_46", LastValueAt: at}
			}
			path := filepath.Join(t.TempDir(), "jeedom.json")
			store.SetPath(path)
			if err := store.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				restored, err := LoadStore(t.Context(), path, "keep_last", nil)
				if err != nil {
					t.Fatal(err)
				}
				current, _ := restored.Device("server")
				if current.Values["state"] != test.want {
					t.Fatalf("state=%#v, want %#v; values=%#v", current.Values["state"], test.want, current.Values)
				}
				if current.Values["current_raw"] != float64(960) {
					t.Fatalf("numeric value changed: %#v", current.Values)
				}
				if err := restored.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
