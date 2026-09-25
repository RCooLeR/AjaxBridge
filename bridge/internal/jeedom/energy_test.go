package jeedom

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExplicitEnergyUnitsOverrideLegacyPowerContract(t *testing.T) {
	metrics := make(map[string]string)
	for _, unit := range []string{"mWh", "Wh", "kWh", "MWh", "GWh", "TWh", "J", "kJ", "MJ", "GJ", "cal", "kcal", "Mcal", "Gcal"} {
		for _, event := range []Event{
			{LogicalID: "powerWtH", GenericType: "POWER", CommandName: "Consommation", Unit: unit},
			{LogicalID: "powerWTh", CommandName: "Renamed counter", Unit: unit},
			{CommandName: "Consommation", Unit: unit},
		} {
			mapping := MappingFor(event)
			if mapping.DeviceClass != "energy" || mapping.StateClass != "total_increasing" || mapping.Unit != unit || !mapping.Numeric || !strings.HasPrefix(mapping.Metric, "energy_") {
				t.Fatalf("energy mapping for %#v = %#v", event, mapping)
			}
			if previous, exists := metrics[mapping.Metric]; exists && previous != unit {
				t.Fatalf("units %s and %s share metric %s", previous, unit, mapping.Metric)
			}
			metrics[mapping.Metric] = unit
		}
	}
	blank := MappingFor(Event{LogicalID: "powerWtH", CommandName: "Puissance"})
	if blank.Metric != "power_raw" || blank.Unit != "" || blank.DeviceClass != "" {
		t.Fatalf("blank-unit behavior changed: %#v", blank)
	}
}

func TestSocketEnergyContractSurvivesRestartAndValueOnlyEvents(t *testing.T) {
	store := NewStore("keep_last")
	store.SetPath(filepath.Join(t.TempDir(), "jeedom.json"))
	at := time.Unix(1700000000, 0).UTC()
	result := applySocketEnergyDiscovery(t, store, "kWh", "78.41", at)
	identity := energyDiscoveryIdentity(t, result.Device)
	for cycle := range 3 {
		if err := store.Save(t.Context()); err != nil {
			t.Fatal(err)
		}
		restored, err := LoadStore(t.Context(), store.path, "keep_last", nil)
		if err != nil {
			t.Fatal(err)
		}
		device, _ := restored.Device(result.Device.DeviceSlug)
		command := device.RawCommands["221"]
		if command.Metric != "energy_kwh" || command.DeviceClass != "energy" || command.Unit != "kWh" || command.Value != float64(78.41) || !command.LastValueAt.Equal(at) || !command.SourceUnitKnown || command.SourceUnit != "kWh" {
			t.Fatalf("restart %d changed energy contract/value: %#v", cycle, command)
		}
		if device.Values["energy_kwh"] != float64(78.41) || energyDiscoveryIdentity(t, device) != identity {
			t.Fatalf("restart %d changed published energy identity/value: %#v", cycle, device)
		}
		store = restored
	}
	event, err := ParseMessage("jeedom/cmd/event/221", []byte(`{"value":78.42,"humanName":"[Home][Socket][Consommation]","type":"info","subtype":"numeric"}`), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	updated := store.Apply(event)
	if updated.Command.Metric != "energy_kwh" || updated.Command.Value != float64(78.42) || updated.Command.Unit != "kWh" || energyDiscoveryIdentity(t, updated.Device) != identity {
		t.Fatalf("value-only event lost energy contract: %#v", updated.Command)
	}
}

func TestEnergyUnitOnlyChangeRequiresFreshValue(t *testing.T) {
	for _, transport := range []string{"event", "discovery"} {
		t.Run(transport, func(t *testing.T) {
			store := NewStore("keep_last")
			at := time.Unix(1700000000, 0).UTC()
			initial := applySocketEnergyDiscovery(t, store, "Wh", "78410", at)
			identity := energyDiscoveryIdentity(t, initial.Device)
			var device Device
			if transport == "discovery" {
				device = applySocketEnergyDiscovery(t, store, "kWh", "null", at.Add(time.Minute)).Device
			} else {
				event, err := ParseMessage("jeedom/cmd/event/221", []byte(`{"value":null,"unite":"kWh","humanName":"[Home][Socket][Consommation]","type":"info","subtype":"numeric"}`), at.Add(time.Minute))
				if err != nil {
					t.Fatal(err)
				}
				device = store.Apply(event).Device
			}
			command := device.RawCommands["221"]
			if command.Metric != "energy_kwh" || command.Value != nil || !command.LastValueAt.IsZero() || device.Values["energy_kwh"] != nil {
				t.Fatalf("old Wh value relabeled kWh: %#v / %#v", command, device.Values)
			}
			if _, exists := device.Values["energy_wh"]; exists {
				t.Fatalf("old unit metric remains: %#v", device.Values)
			}
			if energyDiscoveryIdentity(t, device) != identity {
				t.Fatal("unit-only change replaced the entity")
			}
			fresh := applySocketEnergyDiscovery(t, store, "kWh", "78.411", at.Add(2*time.Minute)).Device
			if fresh.Values["energy_kwh"] != float64(78.411) || !fresh.RawCommands["221"].LastValueAt.Equal(at.Add(2*time.Minute)) {
				t.Fatalf("fresh energy sample not used verbatim: %#v", fresh)
			}
		})
	}
}

func TestPersistedVerifiedEnergyRepairsOldPowerMappingWithoutRescaling(t *testing.T) {
	store := NewStore("keep_last")
	store.SetPath(filepath.Join(t.TempDir(), "jeedom.json"))
	at := time.Unix(1700000000, 0).UTC()
	result := applySocketEnergyDiscovery(t, store, "kWh", "78.41", at)
	device := store.devices[result.Device.DeviceSlug]
	command := device.RawCommands["221"]
	command.Metric, command.Name, command.DeviceClass, command.StateClass = "power_raw", "Power", "", ""
	device.RawCommands["221"] = command
	device.Values = map[string]any{"power_raw": float64(78.41)}
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadStore(t.Context(), store.path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := restored.Device(device.DeviceSlug)
	if current.Values["energy_kwh"] != float64(78.41) || current.RawCommands["221"].DeviceClass != "energy" || !current.RawCommands["221"].LastValueAt.Equal(at) {
		t.Fatalf("verified cached energy not repaired: %#v", current)
	}
	if _, exists := current.Values["power_raw"]; exists {
		t.Fatalf("old power quantity remains: %#v", current.Values)
	}
}

func applySocketEnergyDiscovery(t *testing.T, store *Store, unit, value string, at time.Time) ApplyDiscoveryResult {
	t.Helper()
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/27", []byte(fmt.Sprintf(`{"id":27,"name":"Socket","object_name":"Home","configuration":{"device":"Socket"},"cmds":{"221":{"id":221,"logicalId":"powerWtH","generic_type":"POWER","name":"Consommation","type":"info","subType":"numeric","unite":%q,"currentValue":%s}}}`, unit, value)), at)
	if err != nil {
		t.Fatal(err)
	}
	return store.ApplyDiscovery(discovery)
}

func energyDiscoveryIdentity(t *testing.T, device Device) string {
	t.Helper()
	publisher := NewPublisher(PublisherConfig{StateTopicPrefix: "ajaxbridge/jeedom"}, nil)
	_, payload, err := publisher.BuildDiscovery(device.RawCommands["221"], device)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		UniqueID    string `json:"unique_id"`
		DeviceClass string `json:"device_class"`
		StateClass  string `json:"state_class"`
		Unit        string `json:"unit_of_measurement"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	if config.DeviceClass != "energy" || config.StateClass != "total_increasing" || config.Unit != device.RawCommands["221"].Unit || config.UniqueID != "ajaxbridge_jeedom_cmd_221" {
		t.Fatalf("invalid energy discovery: %s", payload)
	}
	return config.UniqueID
}

func TestUnverifiedEnergyNamesRemainRawThroughPublishAndRestart(t *testing.T) {
	for _, name := range []string{"Consommation", "Energy", "Consumption"} {
		for _, transport := range []string{"event", "discovery"} {
			for _, source := range []struct {
				field, unit string
				known       bool
			}{
				{}, {field: `,"unite":""`, known: true}, {field: `,"unite":"widgets"`, unit: "widgets", known: true},
			} {
				t.Run(name+"/"+transport+"/"+source.field, func(t *testing.T) {
					store := NewStore("keep_last")
					store.SetPath(filepath.Join(t.TempDir(), "jeedom.json"))
					at := time.Unix(1700000000, 0).UTC()
					device := applyNamedEnergy(t, store, transport, name, source.field, "42", at)
					for cycle := range 2 {
						command := device.RawCommands["221"]
						if command.Metric != "energy_raw" || command.Unit != source.unit || command.DeviceClass != "" || command.StateClass != "" || command.EntityCategory != "diagnostic" || command.Value != float64(42) || !command.LastValueAt.Equal(at) || command.SourceUnit != source.unit || command.SourceUnitKnown != source.known {
							t.Fatalf("cycle %d invented an energy contract or changed the sample: %#v", cycle, command)
						}
						cfg := publishedNamedEnergy(t, device)
						if cfg.UniqueID != "ajaxbridge_jeedom_cmd_221" || cfg.UnitOfMeasurement != source.unit || cfg.DeviceClass != "" || cfg.StateClass != "" || cfg.EntityCategory != "diagnostic" {
							t.Fatalf("raw discovery has physical metadata: %#v", cfg)
						}
						if _, exists := device.Values["energy_kwh"]; exists {
							t.Fatalf("invented kWh projection remains: %#v", device.Values)
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
					}
				})
			}
		}
	}
}

func TestEnergyLegacyMappedUnitIsNotSourceEvidence(t *testing.T) {
	for _, name := range []string{"Consommation", "Custom meter reading", "Temperature"} {
		t.Run(name, func(t *testing.T) {
			store := NewStore("keep_last")
			store.SetPath(filepath.Join(t.TempDir(), "jeedom.json"))
			at := time.Unix(1700000000, 0).UTC()
			original := applyNamedEnergy(t, store, "event", "Consommation", `,"unite":"kWh"`, "42", at)
			device := store.devices[original.DeviceSlug]
			command := device.RawCommands["221"]
			command.RawName, command.Name = name, name
			command.SourceUnit, command.SourceUnitKnown = "", false
			device.RawCommands["221"] = command
			for cycle := range 3 {
				if err := store.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
				restored, err := LoadStore(t.Context(), store.path, "keep_last", nil)
				if err != nil {
					t.Fatal(err)
				}
				store = restored
				current, _ := store.Device(original.DeviceSlug)
				command = current.RawCommands["221"]
				if command.Metric != "energy_raw" || command.Unit != "" || command.DeviceClass != "" || command.StateClass != "" || command.SourceUnitKnown || command.Value != float64(42) || !command.LastValueAt.Equal(at) {
					t.Fatalf("cycle %d trusted old mapped kWh or changed raw data: %#v", cycle, command)
				}
				cfg := publishedNamedEnergy(t, current)
				if cfg.UniqueID != "ajaxbridge_jeedom_cmd_221" || cfg.UnitOfMeasurement != "" || cfg.DeviceClass != "" || cfg.StateClass != "" {
					t.Fatalf("legacy discovery retained invented units: %#v", cfg)
				}
			}
			updated := applyNamedEnergy(t, store, "event", name, "", "43", at.Add(time.Minute))
			if updated.RawCommands["221"].Metric != "energy_raw" || updated.Values["energy_raw"] != float64(43) {
				t.Fatalf("value-only event resurrected the old physical contract: %#v", updated)
			}
		})
	}
}

func TestEnergyRawUnitUpgradeWaitsForFreshValueAndKeepsIdentity(t *testing.T) {
	for _, transport := range []string{"event", "discovery"} {
		t.Run(transport, func(t *testing.T) {
			store := NewStore("keep_last")
			store.SetPath(filepath.Join(t.TempDir(), "jeedom.json"))
			at := time.Unix(1700000000, 0).UTC()
			raw := applyNamedEnergy(t, store, transport, "Consommation", `,"unite":""`, "42000", at)
			identity := publishedNamedEnergy(t, raw)
			upgraded := applyNamedEnergy(t, store, transport, "Consommation", `,"unite":"kWh"`, "null", at.Add(time.Minute))
			for cycle := range 2 {
				command := upgraded.RawCommands["221"]
				cfg := publishedNamedEnergy(t, upgraded)
				if command.Metric != "energy_kwh" || command.Value != nil || !command.LastValueAt.IsZero() || cfg.UniqueID != identity.UniqueID || cfg.StateTopic != identity.StateTopic || cfg.UnitOfMeasurement != "kWh" || cfg.DeviceClass != "energy" {
					t.Fatalf("cycle %d relabeled an old sample or replaced entity: %#v / %#v", cycle, command, cfg)
				}
				if _, exists := upgraded.Values["energy_raw"]; exists {
					t.Fatal("raw projection remains after upgrade")
				}
				if err := store.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
				restored, err := LoadStore(t.Context(), store.path, "keep_last", nil)
				if err != nil {
					t.Fatal(err)
				}
				store = restored
				upgraded, _ = store.Device(upgraded.DeviceSlug)
			}
			fresh := applyNamedEnergy(t, store, "event", "Renamed meter", "", "0", at.Add(2*time.Minute))
			if fresh.Values["energy_kwh"] != float64(0) || !fresh.RawCommands["221"].LastValueAt.Equal(at.Add(2*time.Minute)) {
				t.Fatalf("fresh zero lost: %#v", fresh)
			}
			demoted := applyNamedEnergy(t, store, "event", "Renamed meter", `,"unite":""`, "null", at.Add(3*time.Minute))
			command := demoted.RawCommands["221"]
			if command.Metric != "energy_raw" || command.Unit != "" || command.DeviceClass != "" || command.Value != float64(0) || !command.LastValueAt.Equal(at.Add(2*time.Minute)) {
				t.Fatalf("explicit blank failed to clear units or changed raw value/freshness: %#v", command)
			}
			if publishedNamedEnergy(t, demoted).UniqueID != identity.UniqueID {
				t.Fatal("demotion replaced entity")
			}
		})
	}
}

func applyNamedEnergy(t *testing.T, store *Store, transport, name, unitField, value string, at time.Time) Device {
	t.Helper()
	if transport == "event" {
		body := fmt.Sprintf(`{"humanName":%q,"type":"info","subtype":"numeric","value":%s%s}`, "[Test][Meter]["+name+"]", value, unitField)
		event, err := ParseMessage("jeedom/cmd/event/221", []byte(body), at)
		if err != nil {
			t.Fatal(err)
		}
		return store.Apply(event).Device
	}
	body := fmt.Sprintf(`{"id":27,"name":"Meter","object_name":"Test","cmds":{"221":{"id":221,"name":%q,"type":"info","subType":"numeric","currentValue":%s%s}}}`, name, value, unitField)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/27", []byte(body), at)
	if err != nil {
		t.Fatal(err)
	}
	return store.ApplyDiscovery(discovery).Device
}

func publishedNamedEnergy(t *testing.T, device Device) DiscoveryConfig {
	t.Helper()
	topic := "homeassistant/sensor/ajaxbridge/jeedom_cmd_221/config"
	mqtt := &electricalDiscoveryMQTT{canonicalTopic: topic}
	publisher := NewPublisher(PublisherConfig{Discovery: true, RetainDiscovery: true}, mqtt)
	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	var config DiscoveryConfig
	if err := json.Unmarshal([]byte(mqtt.discovery[topic]), &config); err != nil {
		t.Fatalf("missing canonical discovery: %v", err)
	}
	if mqtt.tombstones != 0 {
		t.Fatalf("canonical discovery was deleted %d times", mqtt.tombstones)
	}
	return config
}
