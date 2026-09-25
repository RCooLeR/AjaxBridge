package jeedom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestServicePersistsJeedomDiscoveryForRestartRepublish(t *testing.T) {
	path := t.TempDir() + "/jeedom.json"
	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceConfig{EventTopic: "jeedom/cmd/event/#"}, store, nil, nil, nil, nil, zerolog.Nop())

	service.HandleMessage(t.Context(), "jeedom/discovery/eqLogic/8", []byte(`{
	  "id":8,
	  "name":"Server power",
	  "configuration":{"device":"WallSwitch"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "56":{"id":56,"name":"Puissance","type":"info","subType":"numeric","unite":"W","isVisible":1,"currentValue":"123,4"},
	    "57":{"id":57,"name":"Temp\u00e9rature","type":"info","subType":"numeric","unite":"\u00b0C","isVisible":1,"value":18.6}
	  }
	}`))

	restarted, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	device, ok := restarted.Device("server_power")
	if !ok {
		t.Fatal("missing persisted Jeedom device after restart")
	}
	if got := device.Values["temperature_c"]; got != 18.6 {
		t.Fatalf("temperature_c = %#v, want 18.6", got)
	}

	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainState:      true,
		RetainDiscovery:  true,
	}, mqtt)
	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_57/config"]; got == "" {
		t.Fatalf("temperature discovery missing after restart: %#v", mqtt.discovery)
	}
	if got := mqtt.state["ajaxbridge/jeedom/devices/server_power/state"]; !strings.Contains(got, `"temperature_c":18.6`) {
		t.Fatalf("persisted Jeedom state = %q, want temperature_c", got)
	}
}

func TestActionOnlyWallSwitchSurvivesBridgeThenHARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/26", []byte(wallSwitchDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	controller := NewController(ControllerConfig{
		Enabled:              true,
		StateTopicPrefix:     "ajaxbridge/jeedom",
		JeedomSetTopicPrefix: "jeedom/cmd/set",
	}, store, &fakeCommandPublisher{}, zerolog.Nop())
	if _, err := controller.Execute(t.Context(), "grid_load", "ON", "test"); err != nil {
		t.Fatal(err)
	}

	// Bridge restart: the only source of truth is the persisted Jeedom cache.
	restarted, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	device, ok := restarted.Device("grid_load")
	if !ok {
		t.Fatal("missing WallSwitch after bridge restart")
	}
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainState:      true,
		RetainDiscovery:  true,
		Controls:         true,
	}, mqtt)
	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}

	// HA restart: discovery and state must both be available as retained MQTT.
	stateTopic := "ajaxbridge/jeedom/devices/grid_load/state"
	if !mqtt.stateRetain[stateTopic] {
		t.Fatalf("state topic %q was not retained", stateTopic)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(mqtt.state[stateTopic]), &state); err != nil {
		t.Fatal(err)
	}
	if state["state"] != true {
		t.Fatalf("republished state = %#v, want true", state["state"])
	}
	discoveryTopic := "homeassistant/switch/ajaxbridge/jeedom_control_grid_load/config"
	if !mqtt.discoveryRetain[discoveryTopic] {
		t.Fatalf("discovery topic %q was not retained", discoveryTopic)
	}
	var config DiscoveryConfig
	if err := json.Unmarshal([]byte(mqtt.discovery[discoveryTopic]), &config); err != nil {
		t.Fatal(err)
	}
	if config.StateTopic != stateTopic || config.Optimistic == nil || *config.Optimistic {
		t.Fatalf("restart-safe switch discovery = %#v", config)
	}
}

func TestLoadStoreAcceptsMissingFileAsEmptyCache(t *testing.T) {
	store, err := LoadStore(t.Context(), t.TempDir()+"/missing.json", "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.Devices()); got != 0 {
		t.Fatalf("devices = %d, want empty cache", got)
	}
}

func TestLoadStoreAcceptsLegacyDeviceArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	if err := os.WriteFile(path, []byte(`[{
	  "source":"jeedom",
	  "device":"Server power",
	  "device_slug":"server_power",
	  "values":{"temperature_c":18.6},
	  "raw_commands":{
	    "57":{"command_id":"57","device":"Server power","device_slug":"server_power","name":"Temperature","metric":"temperature_c","component":"sensor","type":"info","subtype":"numeric","unit":"°C"}
	  }
	}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	device, ok := store.Device("server_power")
	if !ok {
		t.Fatal("missing device loaded from legacy array")
	}
	if got := device.RawCommands["57"].Metric; got != "temperature_c" {
		t.Fatalf("metric = %q, want temperature_c", got)
	}
}

func TestLoadStorePreservesLegacyCommandContractAfterCustomRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	if err := os.WriteFile(path, []byte(`[{
	  "source":"jeedom",
	  "device":"Server power",
	  "device_slug":"server_power",
	  "values":{"temperature_c":18.6},
	  "raw_commands":{
	    "57":{"command_id":"57","device":"Server power","device_slug":"server_power","name":"Rack inlet","raw_name":"Rack inlet","metric":"temperature_c","component":"sensor","type":"info","subtype":"numeric","unit":"°C","device_class":"temperature","state_class":"measurement","value":18.6}
	  }
	}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	device, ok := store.Device("server_power")
	if !ok {
		t.Fatal("missing device loaded from legacy cache")
	}
	command := device.RawCommands["57"]
	if command.Metric != "temperature_c" || command.Component != ComponentSensor || command.DeviceClass != "temperature" {
		t.Fatalf("preserved command contract = %#v, want temperature sensor", command)
	}
	if got := device.Values["temperature_c"]; got != 18.6 {
		t.Fatalf("temperature_c = %#v, want 18.6", got)
	}
	if _, exists := device.Values["rack_inlet_value"]; exists {
		t.Fatalf("custom-name fallback survived restart migration: %#v", device.Values)
	}
}

func TestSaveStoreRestoresActionMetadata(t *testing.T) {
	path := t.TempDir() + "/jeedom.json"
	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}

	restarted, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	action, ok := restarted.Action("garage_gate", "on")
	if !ok {
		t.Fatal("missing persisted on action")
	}
	if action.CommandID != "85" || action.StateCommandID != "81" {
		t.Fatalf("action = %#v, want command/state ids", action)
	}
}

func TestLoadStoreMigratesExpandedJeedomCommandMappings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	legacy := `[{
	  "source":"jeedom",
	  "device":"Fire detector",
	  "device_slug":"fire_detector",
	  "jeedom_id":"13",
	  "jeedom_device_type":"FireProtect2HscAc",
	  "values":{
	    "alarme_fumee":"SMOKE_ALARM_NOT_DETECTED",
	    "nombre_de_defauts_value":2,
	    "derniere_mise_a_jour_value":1790208691
	  },
	  "raw_commands":{
	    "379":{"command_id":"379","object":"House","device":"Fire detector","device_slug":"fire_detector","raw_name":"Alarme fumée","name":"Alarme fumée","metric":"alarme_fumee","component":"sensor","type":"info","subtype":"string","value":"SMOKE_ALARM_NOT_DETECTED"},
	    "398":{"command_id":"398","object":"House","device":"Fire detector","device_slug":"fire_detector","raw_name":"Nombre de défauts","name":"Nombre de défauts","metric":"nombre_de_defauts_value","component":"sensor","type":"info","subtype":"numeric","value":2},
	    "402":{"command_id":"402","object":"House","device":"Fire detector","device_slug":"fire_detector","raw_name":"Dernière mise à jour","name":"Dernière mise à jour","metric":"derniere_mise_a_jour_value","component":"sensor","type":"info","subtype":"numeric","unit":"s","value":1790208691}
	  }
	},{
	  "source":"jeedom",
	  "device":"Water valve",
	  "device_slug":"water_valve",
	  "jeedom_id":"21",
	  "jeedom_device_type":"WaterStop",
	  "values":{"etat_de_la_vanne":"INTERMEDIATE"},
	  "raw_commands":{
	    "414":{"command_id":"414","object":"Utility","device":"Water valve","device_slug":"water_valve","raw_name":"Etat de la vanne","name":"Etat de la vanne","metric":"etat_de_la_vanne","component":"sensor","type":"info","subtype":"string","value":"INTERMEDIATE"}
	  }
	}]`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	fire, ok := store.Device("fire_detector")
	if !ok {
		t.Fatal("missing migrated fire detector")
	}
	if got := fire.Values["smoke_alarm"]; got != false {
		t.Fatalf("smoke_alarm = %#v, want false", got)
	}
	if got := fire.Values["issue_count"]; got != float64(2) {
		t.Fatalf("issue_count = %#v, want 2", got)
	}
	if got := fire.Values["device_last_update"]; got != "2026-09-24T00:11:31Z" {
		t.Fatalf("device_last_update = %#v, want RFC3339 timestamp", got)
	}
	for _, obsolete := range []string{"alarme_fumee", "nombre_de_defauts_value", "derniere_mise_a_jour_value"} {
		if _, exists := fire.Values[obsolete]; exists {
			t.Fatalf("obsolete metric %q survived migration: %#v", obsolete, fire.Values)
		}
	}
	if command := fire.RawCommands["379"]; command.Metric != "smoke_alarm" || command.Component != ComponentBinarySensor {
		t.Fatalf("migrated smoke command = %#v", command)
	}
	if command := fire.RawCommands["402"]; command.DeviceClass != "timestamp" || command.Unit != "" {
		t.Fatalf("migrated timestamp command = %#v", command)
	}

	valve, ok := store.Device("water_valve")
	if !ok {
		t.Fatal("missing migrated WaterStop")
	}
	if got := valve.Values["valve_position"]; got != "INTERMEDIATE" {
		t.Fatalf("valve_position = %#v, want INTERMEDIATE", got)
	}
	if got, exists := valve.Values["state"]; !exists || got != nil {
		t.Fatalf("derived intermediate state = %#v present=%v, want nil", got, exists)
	}
	if _, exists := valve.Values["etat_de_la_vanne"]; exists {
		t.Fatalf("obsolete valve metric survived migration: %#v", valve.Values)
	}
}

func TestPendingDiscoveryCleanupSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(Discovery{
		EqLogicID: "20",
		Name:      "Remote",
		InfoCommands: map[string]DiscoveryCommand{
			"369": {CommandID: "369", EqLogicID: "20", Name: "Nombre de défauts", Type: "info", Subtype: "numeric", Value: []byte(`1`)},
		},
		Actions: map[string]DiscoveryCommand{
			"900": {CommandID: "900", EqLogicID: "20", LogicalID: "PANIC", Name: "Panic", Type: "action"},
		},
	})
	store.ApplyDiscovery(Discovery{
		EqLogicID: "20",
		Name:      "Remote",
		InfoCommands: map[string]DiscoveryCommand{
			"375": {CommandID: "375", EqLogicID: "20", Name: "Nombre de défauts", Type: "info", Subtype: "numeric", Value: []byte(`0`)},
		},
	})
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}

	restarted, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	device, ok := restarted.Device("remote")
	if !ok {
		t.Fatal("missing restarted remote")
	}
	if len(device.PendingDiscoveryCleanups) != 1 || device.PendingDiscoveryCleanups[0].CommandID != "369" {
		t.Fatalf("pending cleanup after restart = %#v, want command 369", device.PendingDiscoveryCleanups)
	}
	if len(device.PendingActionDiscoveryCleanups) != 1 || device.PendingActionDiscoveryCleanups[0].CommandID != "900" {
		t.Fatalf("pending action cleanup after restart = %#v, want action 900", device.PendingActionDiscoveryCleanups)
	}
}

func TestVoltageRemainsCanonicalAcrossReconcileAndRestart(t *testing.T) {
	for _, deviceType := range []string{"Relay", "WallSwitch", "Socket"} {
		t.Run(deviceType, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jeedom.json")
			store, err := LoadStore(t.Context(), path, "keep_last", nil)
			if err != nil {
				t.Fatal(err)
			}
			at := time.Unix(100, 0).UTC()
			result := store.ApplyDiscovery(Discovery{
				EqLogicID: "6", Name: "Supply", DeviceType: deviceType, ReceivedAt: at,
				InfoCommands: map[string]DiscoveryCommand{
					"230": {CommandID: "230", Name: "Voltage", LogicalID: "voltage", Type: "info", Subtype: "numeric", Value: json.RawMessage(`289.02`)},
				},
			})
			want := 289.02
			if deviceType == "Relay" {
				want /= 10
			}
			if got := result.Device.Values["voltage_v"]; got != want {
				t.Fatalf("discovery voltage = %#v, want %v", got, want)
			}
			live := store.Apply(Event{CommandID: "230", DeviceName: "Supply", CommandName: "Voltage", Subtype: "numeric", Value: json.RawMessage(`289.02`), ReceivedAt: at})
			if live.Command.Value != want {
				t.Fatalf("live voltage = %#v, want %v", live.Command.Value, want)
			}
			for cycle := 0; cycle < 3; cycle++ {
				if err := store.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
				store, err = LoadStore(t.Context(), path, "keep_last", nil)
				if err != nil {
					t.Fatal(err)
				}
				device, ok := store.Device(result.Device.DeviceSlug)
				if !ok {
					t.Fatal("missing device after restart")
				}
				reconcilePersistedMappings(&device)
				if got := device.Values["voltage_v"]; got != want || device.RawCommands["230"].Value != want {
					t.Fatalf("cycle %d voltage = %#v, command = %#v, want exactly %v", cycle, got, device.RawCommands["230"].Value, want)
				}
				if !device.RawCommands["230"].LastValueAt.Equal(at) {
					t.Fatal("cache migration changed observation time")
				}
			}
		})
	}
}

func TestLegacyRelayVoltageMappingMigratesOnce(t *testing.T) {
	for _, bareArray := range []bool{false, true} {
		t.Run(map[bool]string{false: "versioned", true: "legacy_array"}[bareArray], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jeedom.json")
			legacy := Device{
				Device: "Supply", DeviceSlug: "supply", JeedomDeviceType: "Relay",
				Values: map[string]any{"legacy_voltage_value": 289.02},
				RawCommands: map[string]Command{
					"230": {CommandID: "230", RawName: "Voltage", Metric: "legacy_voltage_value", Component: ComponentSensor, Subtype: "numeric", Value: 289.02},
				},
			}
			var body []byte
			var err error
			if bareArray {
				body, err = json.Marshal([]Device{legacy})
			} else {
				body, err = json.Marshal(persistedStore{Version: persistedStoreVersion, Devices: []Device{legacy}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			want := 289.02
			want /= 10
			for cycle := 0; cycle < 3; cycle++ {
				store, err := LoadStore(t.Context(), path, "keep_last", nil)
				if err != nil {
					t.Fatal(err)
				}
				device, _ := store.Device("supply")
				if device.Values["voltage_v"] != want || device.RawCommands["230"].Value != want {
					t.Fatalf("cycle %d migrated values = %#v", cycle, device)
				}
				if _, exists := device.Values["legacy_voltage_value"]; exists {
					t.Fatal("obsolete voltage metric survived migration")
				}
				if err := store.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRelayCacheDoesNotGuessRepairsForCanonicalVoltage(t *testing.T) {
	device := Device{
		DeviceSlug: "relay", JeedomDeviceType: "Relay", Values: map[string]any{},
		RawCommands: map[string]Command{
			"230": {CommandID: "230", RawName: "Voltage", Metric: "voltage_v", Component: ComponentSensor, Subtype: "numeric", Value: 2.8902},
		},
	}
	reconcilePersistedMappings(&device)
	if got := device.Values["voltage_v"]; got != 2.8902 {
		t.Fatalf("canonical voltage = %#v, want unchanged 2.8902", got)
	}
}
