package jeedom

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiscoveryPayloadUsesStableCommandIDAndDeviceIdentifier(t *testing.T) {
	device := Device{
		Source:     Source,
		Device:     "Серверна",
		DeviceSlug: "serverna",
		RawCommands: map[string]Command{
			"56": {
				CommandID:   "56",
				Device:      "Серверна",
				DeviceSlug:  "serverna",
				Name:        "Puissance",
				Metric:      "power_w",
				Component:   ComponentSensor,
				DeviceClass: "power",
				StateClass:  "measurement",
				Unit:        "W",
				LastUpdate:  time.Unix(100, 0),
			},
		},
	}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
	}, fakeMQTT{})

	topic, body, err := publisher.BuildDiscovery(device.RawCommands["56"], device)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/sensor/ajaxbridge/jeedom_cmd_56/config" {
		t.Fatalf("discovery topic = %q", topic)
	}

	var payload DiscoveryConfig
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.UniqueID != "ajaxbridge_jeedom_cmd_56" {
		t.Fatalf("UniqueID = %q", payload.UniqueID)
	}
	if len(payload.Device.Identifiers) != 1 || payload.Device.Identifiers[0] != "ajaxbridge_jeedom_serverna" {
		t.Fatalf("identifiers = %#v", payload.Device.Identifiers)
	}
	if payload.StateTopic != "ajaxbridge/jeedom/devices/serverna/state" {
		t.Fatalf("state_topic = %q", payload.StateTopic)
	}
	if payload.JSONAttributesTopic != "ajaxbridge/jeedom/devices/serverna/attributes" {
		t.Fatalf("json_attributes_topic = %q", payload.JSONAttributesTopic)
	}
	if payload.ValueTemplate != "{{ value_json.get('power_w') }}" {
		t.Fatalf("value_template = %q, want missing-key-safe lookup", payload.ValueTemplate)
	}
}

func TestPublishDeviceCleansNeverValuedMeasurements(t *testing.T) {
	for _, tc := range []struct {
		name       string
		commandID  string
		metric     string
		unit       string
		class      string
		stateClass string
	}{
		{name: "power", commandID: "180", metric: "power_w", unit: "W", class: "power", stateClass: "measurement"},
		{name: "energy", commandID: "221", metric: "energy_kwh", unit: "kWh", class: "energy", stateClass: "total_increasing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mqtt := &recordingMQTT{}
			publisher := NewPublisher(PublisherConfig{
				StateTopicPrefix: "ajaxbridge/jeedom",
				Discovery:        true,
				DiscoveryPrefix:  "homeassistant",
				DiscoveryNode:    "ajaxbridge",
				RetainState:      true,
				RetainDiscovery:  true,
			}, mqtt)
			device := Device{
				Device:     "Never valued",
				DeviceSlug: "never_valued_" + tc.commandID,
				Values:     map[string]any{},
				RawCommands: map[string]Command{
					tc.commandID: {
						CommandID:   tc.commandID,
						DeviceSlug:  "never_valued_" + tc.commandID,
						Metric:      tc.metric,
						Component:   ComponentSensor,
						Unit:        tc.unit,
						DeviceClass: tc.class,
						StateClass:  tc.stateClass,
					},
				},
			}

			if err := publisher.PublishDevice(t.Context(), device); err != nil {
				t.Fatal(err)
			}
			topic := "homeassistant/sensor/ajaxbridge/jeedom_cmd_" + tc.commandID + "/config"
			payload, ok := mqtt.discovery[topic]
			if !ok {
				t.Fatalf("missing exact retained cleanup for %s", topic)
			}
			if payload != "" || !mqtt.discoveryRetain[topic] {
				t.Fatalf("cleanup for %s = %q retain=%v, want empty retained", topic, payload, mqtt.discoveryRetain[topic])
			}
			var state map[string]any
			stateTopic := "ajaxbridge/jeedom/devices/" + device.DeviceSlug + "/state"
			if err := json.Unmarshal([]byte(mqtt.state[stateTopic]), &state); err != nil {
				t.Fatal(err)
			}
			if _, ok := state[tc.metric]; ok {
				t.Fatalf("unknown measurement %s was synthesized: %#v", tc.metric, state)
			}
		})
	}
}

func TestCleanupCommandsRemovesBothPossibleRetainedComponents(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		Discovery:       true,
		DiscoveryPrefix: "homeassistant",
		DiscoveryNode:   "ajaxbridge",
	}, mqtt)
	command := Command{
		CommandID: "369",
		Metric:    "issue_count",
		Component: ComponentSensor,
	}

	if err := publisher.CleanupCommands(t.Context(), []Command{command}); err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{ComponentSensor, ComponentBinarySensor} {
		topic := "homeassistant/" + component + "/ajaxbridge/jeedom_cmd_369/config"
		if payload, ok := mqtt.discovery[topic]; !ok || payload != "" || !mqtt.discoveryRetain[topic] {
			t.Fatalf("cleanup %s = %q present=%v retain=%v", topic, payload, ok, mqtt.discoveryRetain[topic])
		}
	}
}

func TestPublishDeviceRetriesPersistedDiscoveryCleanups(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
	}, mqtt)
	device := Device{
		Device:     "Remote",
		DeviceSlug: "remote",
		Values:     map[string]any{"issue_count": float64(0)},
		RawCommands: map[string]Command{
			"375": {
				CommandID:      "375",
				Device:         "Remote",
				DeviceSlug:     "remote",
				Name:           "Issue count",
				Metric:         "issue_count",
				Component:      ComponentSensor,
				StateClass:     "measurement",
				EntityCategory: "diagnostic",
				Value:          float64(0),
				LastValueAt:    time.Unix(100, 0),
			},
		},
		PendingDiscoveryCleanups: []Command{{
			CommandID: "369",
			Metric:    "issue_count",
			Component: ComponentSensor,
		}},
	}

	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{ComponentSensor, ComponentBinarySensor} {
		topic := "homeassistant/" + component + "/ajaxbridge/jeedom_cmd_369/config"
		if payload, ok := mqtt.discovery[topic]; !ok || payload != "" || !mqtt.discoveryRetain[topic] {
			t.Fatalf("retry cleanup %s = %q present=%v retain=%v", topic, payload, ok, mqtt.discoveryRetain[topic])
		}
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_375/config"]; got == "" {
		t.Fatal("current command discovery was not published after cleanup retry")
	}
}

func TestPublishDeviceCleansOneRemovedButtonWhileKeepingOthers(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
		Controls:         true,
	}, mqtt)
	device := Device{
		Device:           "Hub",
		DeviceSlug:       "hub",
		JeedomDeviceType: "Hub",
		Values:           map[string]any{},
		RawCommands:      map[string]Command{},
		Actions: map[string]Action{
			"arm": {Action: "arm", CommandID: "163", Device: "Hub", DeviceSlug: "hub", Name: "Arm", Allowed: true},
		},
		PendingActionDiscoveryCleanups: []Action{{
			Action:     "panic",
			CommandID:  "166",
			Device:     "Hub",
			DeviceSlug: "hub",
		}},
	}

	published, err := publisher.PublishDeviceWithResult(t.Context(), device)
	if err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("current hub snapshot was unexpectedly skipped")
	}
	if payload, ok := mqtt.discovery["homeassistant/button/ajaxbridge/jeedom_control_hub_panic/config"]; !ok || payload != "" || !mqtt.discoveryRetain["homeassistant/button/ajaxbridge/jeedom_control_hub_panic/config"] {
		t.Fatalf("panic cleanup = %q present=%v", payload, ok)
	}
	if got := mqtt.discovery["homeassistant/button/ajaxbridge/jeedom_control_hub_arm/config"]; got == "" {
		t.Fatal("remaining arm button discovery was not published")
	}
}

func TestFirstValidZeroMeasurementCausesRediscovery(t *testing.T) {
	store := NewStore("keep_last")
	discovered := store.ApplyDiscovery(Discovery{
		EqLogicID:  "9",
		Name:       "Fence power",
		DeviceType: "WallSwitch",
		InfoCommands: map[string]DiscoveryCommand{
			"180": {CommandID: "180", Name: "Puissance", Type: "info", Subtype: "numeric", Unit: "W"},
		},
		ReceivedAt: time.Unix(100, 0),
	})
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainState:      true,
		RetainDiscovery:  true,
	}, mqtt)
	topic := "homeassistant/sensor/ajaxbridge/jeedom_cmd_180/config"

	if err := publisher.PublishDevice(t.Context(), discovered.Device); err != nil {
		t.Fatal(err)
	}
	if payload, ok := mqtt.discovery[topic]; !ok || payload != "" {
		t.Fatalf("initial discovery = %q present=%v, want retained cleanup", payload, ok)
	}

	updated := store.Apply(Event{
		Topic:       "jeedom/cmd/event/180",
		CommandID:   "180",
		DeviceName:  "Fence power",
		CommandName: "Puissance",
		Type:        "info",
		Subtype:     "numeric",
		Unit:        "W",
		Value:       json.RawMessage(`0`),
		ReceivedAt:  time.Unix(101, 0),
	})
	if err := publisher.PublishDevice(t.Context(), updated.Device); err != nil {
		t.Fatal(err)
	}
	payload := mqtt.discovery[topic]
	if payload == "" {
		t.Fatal("first valid zero did not republish discovery")
	}
	var config DiscoveryConfig
	if err := json.Unmarshal([]byte(payload), &config); err != nil {
		t.Fatal(err)
	}
	if config.UniqueID != "ajaxbridge_jeedom_cmd_180" || config.ValueTemplate != "{{ value_json.get('power_w') }}" {
		t.Fatalf("rediscovery config = %#v", config)
	}
	stateTopic := "ajaxbridge/jeedom/devices/fence_power/state"
	var state map[string]any
	if err := json.Unmarshal([]byte(mqtt.state[stateTopic]), &state); err != nil {
		t.Fatal(err)
	}
	if value, ok := state["power_w"]; !ok || value != float64(0) {
		t.Fatalf("rediscovered zero state = %#v present=%v", value, ok)
	}
}

func TestDiscoveryTemplatesAreSafeForPartialJSONAndFalse(t *testing.T) {
	publisher := NewPublisher(PublisherConfig{}, fakeMQTT{})
	device := Device{Device: "Device", DeviceSlug: "device"}
	for _, tc := range []struct {
		name    string
		command Command
		want    string
	}{
		{
			name:    "sensor",
			command: Command{CommandID: "1", Metric: "power_w", Component: ComponentSensor},
			want:    "{{ value_json.get('power_w') }}",
		},
		{
			name:    "binary false",
			command: Command{CommandID: "2", Metric: "state", Component: ComponentBinarySensor},
			want:    "{{ 'None' if value_json.get('state') is none else ('ON' if value_json.get('state') else 'OFF') }}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, body, err := publisher.BuildDiscovery(tc.command, device)
			if err != nil {
				t.Fatal(err)
			}
			var config DiscoveryConfig
			if err := json.Unmarshal(body, &config); err != nil {
				t.Fatal(err)
			}
			if config.ValueTemplate != tc.want || strings.Contains(config.ValueTemplate, "value_json."+tc.command.Metric) {
				t.Fatalf("value_template = %q, want %q", config.ValueTemplate, tc.want)
			}
		})
	}
	if strings.Contains(switchStateValueTemplate, "value_json.state") || !strings.Contains(switchStateValueTemplate, "get('state')") {
		t.Fatalf("switch value template is not partial-JSON-safe: %q", switchStateValueTemplate)
	}
}

func TestNeverValuedDuplicateMetricCommandIsNotAuthorizedBySiblingValue(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
	}, mqtt)
	device := Device{
		Device:     "Duplicate metrics",
		DeviceSlug: "duplicate_metrics",
		Values:     map[string]any{"power_w": 5.0},
		RawCommands: map[string]Command{
			"1": {
				CommandID:   "1",
				Metric:      "power_w",
				Component:   ComponentSensor,
				StateClass:  "measurement",
				Value:       5.0,
				LastValueAt: time.Unix(100, 0),
			},
			"2": {
				CommandID:  "2",
				Metric:     "power_w",
				Component:  ComponentSensor,
				StateClass: "measurement",
			},
		},
	}

	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_1/config"]; got == "" {
		t.Fatal("valued sibling discovery missing")
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_2/config"]; got != "" {
		t.Fatalf("never-valued duplicate metric discovery = %q, want cleanup", got)
	}
}

func TestSwitchDiscoveryUsesBridgeCommandTopic(t *testing.T) {
	device := Device{
		Source:           Source,
		Device:           "Server outlet",
		DeviceSlug:       "server_outlet",
		JeedomDeviceType: "Outlet",
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "85", DeviceSlug: "server_outlet", StateCommandID: "81", Allowed: true},
			"off": {Action: "off", CommandID: "86", DeviceSlug: "server_outlet", StateCommandID: "81", Allowed: true},
		},
	}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		Controls:         true,
	}, fakeMQTT{})

	topic, body, err := publisher.BuildSwitchDiscovery(device.Actions["on"], device)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/switch/ajaxbridge/jeedom_control_server_outlet/config" {
		t.Fatalf("discovery topic = %q", topic)
	}
	var payload DiscoveryConfig
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CommandTopic != "ajaxbridge/jeedom/devices/server_outlet/set" {
		t.Fatalf("command_topic = %q", payload.CommandTopic)
	}
	if payload.StateTopic != "ajaxbridge/jeedom/devices/server_outlet/state" {
		t.Fatalf("state_topic = %q", payload.StateTopic)
	}
	if payload.JSONAttributesTopic != "ajaxbridge/jeedom/devices/server_outlet/attributes" {
		t.Fatalf("json_attributes_topic = %q", payload.JSONAttributesTopic)
	}
	if payload.Optimistic == nil || *payload.Optimistic {
		t.Fatalf("optimistic = %#v, want false", payload.Optimistic)
	}
}

func TestWallSwitchDiscoveryUsesRetainedBridgeStateWithoutJeedomStateCommand(t *testing.T) {
	device := Device{
		Source:           Source,
		Device:           "Grid load",
		DeviceSlug:       "grid_load",
		JeedomDeviceType: "WallSwitch",
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "348", DeviceSlug: "grid_load", Allowed: true},
			"off": {Action: "off", CommandID: "349", DeviceSlug: "grid_load", Allowed: true},
		},
	}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		Controls:         true,
	}, fakeMQTT{})

	topic, body, err := publisher.BuildSwitchDiscovery(device.Actions["on"], device)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/switch/ajaxbridge/jeedom_control_grid_load/config" {
		t.Fatalf("discovery topic = %q", topic)
	}
	var payload DiscoveryConfig
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.UniqueID != "ajaxbridge_jeedom_control_grid_load" {
		t.Fatalf("unique_id = %q", payload.UniqueID)
	}
	if payload.CommandTopic != "ajaxbridge/jeedom/devices/grid_load/set" {
		t.Fatalf("command_topic = %q", payload.CommandTopic)
	}
	if payload.StateTopic != "ajaxbridge/jeedom/devices/grid_load/state" {
		t.Fatalf("state_topic = %q", payload.StateTopic)
	}
	if payload.Optimistic == nil || *payload.Optimistic {
		t.Fatalf("optimistic = %#v, want false", payload.Optimistic)
	}
	if payload.ValueTemplate != switchStateValueTemplate || !strings.Contains(payload.ValueTemplate, "None") {
		t.Fatalf("value_template = %q, want unknown-safe bridge state template", payload.ValueTemplate)
	}
}

func TestButtonDiscoveryUsesImpulsePayload(t *testing.T) {
	device := Device{
		Source:           Source,
		Device:           "Garage gate",
		DeviceSlug:       "garage_gate",
		JeedomDeviceType: "Relay",
		Actions: map[string]Action{
			"on": {Action: "on", CommandID: "85", DeviceSlug: "garage_gate", StateCommandID: "81", Allowed: true},
		},
	}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		Controls:         true,
	}, fakeMQTT{})

	topic, body, err := publisher.BuildButtonDiscovery(device.Actions["on"], device)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/button/ajaxbridge/jeedom_control_garage_gate_impulse/config" {
		t.Fatalf("discovery topic = %q", topic)
	}
	var payload DiscoveryConfig
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "Impulse" {
		t.Fatalf("name = %q, want Impulse", payload.Name)
	}
	if payload.PayloadPress != "ON" {
		t.Fatalf("payload_press = %q, want ON", payload.PayloadPress)
	}
	if payload.CommandTopic != "ajaxbridge/jeedom/devices/garage_gate/set" {
		t.Fatalf("command_topic = %q", payload.CommandTopic)
	}
	if payload.StateTopic != "" {
		t.Fatalf("button state_topic = %q, want empty", payload.StateTopic)
	}
}

func TestPublishDevicePublishesToggleForOutletAndImpulseForRelay(t *testing.T) {
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
		Controls:         true,
	}, nil)

	outletMQTT := &recordingMQTT{}
	publisher.mqtt = outletMQTT
	outlet := Device{
		Source:           Source,
		Device:           "Server outlet",
		DeviceSlug:       "server_outlet",
		JeedomDeviceType: "Outlet",
		Values:           map[string]any{"state": false},
		RawCommands:      map[string]Command{},
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "85", DeviceSlug: "server_outlet", StateCommandID: "81", Allowed: true},
			"off": {Action: "off", CommandID: "86", DeviceSlug: "server_outlet", StateCommandID: "81", Allowed: true},
		},
	}
	if err := publisher.PublishDevice(context.Background(), outlet); err != nil {
		t.Fatal(err)
	}
	if got := outletMQTT.discovery["homeassistant/switch/ajaxbridge/jeedom_control_server_outlet/config"]; got == "" {
		t.Fatalf("outlet toggle switch discovery missing")
	}
	if got := outletMQTT.discovery["homeassistant/button/ajaxbridge/jeedom_control_server_outlet_impulse/config"]; got != "" {
		t.Fatalf("outlet impulse button discovery = %q, want cleanup/empty", got)
	}

	relayMQTT := &recordingMQTT{}
	publisher.mqtt = relayMQTT
	relay := Device{
		Source:           Source,
		Device:           "Garage gate",
		DeviceSlug:       "garage_gate",
		JeedomDeviceType: "Relay",
		Values:           map[string]any{"state": false},
		RawCommands:      map[string]Command{},
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "85", DeviceSlug: "garage_gate", StateCommandID: "81", Allowed: true},
			"off": {Action: "off", CommandID: "86", DeviceSlug: "garage_gate", StateCommandID: "81", Allowed: true},
		},
	}
	if err := publisher.PublishDevice(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	if got := relayMQTT.discovery["homeassistant/switch/ajaxbridge/jeedom_control_garage_gate/config"]; got != "" {
		t.Fatalf("relay toggle switch discovery = %q, want cleanup/empty", got)
	}
	if got := relayMQTT.discovery["homeassistant/button/ajaxbridge/jeedom_control_garage_gate_impulse/config"]; got == "" {
		t.Fatalf("relay impulse button discovery missing")
	}

	waterStopMQTT := &recordingMQTT{}
	publisher.mqtt = waterStopMQTT
	waterStop := Device{
		Source:           Source,
		Device:           "Water valve",
		DeviceSlug:       "water_valve",
		JeedomDeviceType: "WaterStop",
		Values:           map[string]any{"state": false},
		RawCommands:      map[string]Command{},
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "26", DeviceSlug: "water_valve", StateCommandID: "23", Allowed: true},
			"off": {Action: "off", CommandID: "27", DeviceSlug: "water_valve", StateCommandID: "23", Allowed: true},
		},
	}
	if err := publisher.PublishDevice(context.Background(), waterStop); err != nil {
		t.Fatal(err)
	}
	if got := waterStopMQTT.discovery["homeassistant/switch/ajaxbridge/jeedom_control_water_valve/config"]; got == "" {
		t.Fatalf("WaterStop toggle switch discovery missing")
	}
}

func TestPublishDevicePublishesHubSecurityButtons(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
		Controls:         true,
	}, mqtt)

	hub := Device{
		Source:           Source,
		Device:           "Security hub",
		DeviceSlug:       "security_hub",
		JeedomDeviceType: "Hub",
		Values:           map[string]any{},
		RawCommands:      map[string]Command{},
		Actions: map[string]Action{
			"arm":                 {Action: "arm", CommandID: "163", DeviceSlug: "security_hub", Name: "Arm", Allowed: true},
			"night_mode":          {Action: "night_mode", CommandID: "164", DeviceSlug: "security_hub", Name: "Night mode", Allowed: true},
			"disarm":              {Action: "disarm", CommandID: "165", DeviceSlug: "security_hub", Name: "Disarm", Allowed: true},
			"mute_fire_detectors": {Action: "mute_fire_detectors", CommandID: "167", DeviceSlug: "security_hub", Name: "Mute fire detectors", Allowed: true},
		},
	}
	if err := publisher.PublishDevice(context.Background(), hub); err != nil {
		t.Fatal(err)
	}

	for _, actionSlug := range []string{"arm", "night_mode", "disarm", "mute_fire_detectors"} {
		topic := "homeassistant/button/ajaxbridge/jeedom_control_security_hub_" + actionSlug + "/config"
		if got := mqtt.discovery[topic]; got == "" {
			t.Fatalf("hub %s button discovery missing at %s", actionSlug, topic)
		}
	}
	if got := mqtt.discovery["homeassistant/switch/ajaxbridge/jeedom_control_security_hub/config"]; got != "" {
		t.Fatalf("hub switch discovery = %q, want cleanup/empty", got)
	}
}

func TestPublishDeviceClearsLegacyUnlinkedSwitchAndState(t *testing.T) {
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

	device := Device{
		Source:            Source,
		Device:            "Server power",
		DeviceSlug:        "sia_a0f80d_zone_8",
		LegacyDeviceSlugs: []string{"serverna"},
		Values:            map[string]any{},
		RawCommands:       map[string]Command{},
		Actions:           map[string]Action{},
	}
	if err := publisher.PublishDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	if got := mqtt.discovery["homeassistant/switch/ajaxbridge/jeedom_control_serverna/config"]; got != "" {
		t.Fatalf("legacy switch cleanup payload = %q, want empty", got)
	}
	if got := mqtt.state["ajaxbridge/jeedom/devices/serverna/state"]; got != "" {
		t.Fatalf("legacy state cleanup payload = %q, want empty", got)
	}
	if got := mqtt.state["ajaxbridge/jeedom/devices/serverna/attributes"]; got != "" {
		t.Fatalf("legacy attributes cleanup payload = %q, want empty", got)
	}
}

func TestPublishDeviceCleansSIAMergedDuplicateCommands(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
	}, mqtt)

	device := Device{
		Source:        Source,
		Device:        "Garage fire detector",
		DeviceSlug:    "sia_a0f80d_zone_4",
		LinkedSource:  "sia",
		LinkedAccount: "A0F80D",
		LinkedZone:    "4",
		HAIdentifiers: []string{"ajaxbridge_A0F80D_zone_4"},
		Values: map[string]any{
			"temperature_c":                  21.0,
			"smoke_alarm":                    false,
			"heat_alarm":                     false,
			"critical_smoke_alarm":           false,
			"rapid_temperature_rise_alarm":   false,
			"carbon_monoxide_alarm":          false,
			"critical_carbon_monoxide_alarm": false,
		},
		RawCommands: map[string]Command{
			"134": {
				CommandID: "134",
				Name:      "Bypassed",
				Metric:    "bypass",
				Component: ComponentBinarySensor,
			},
			"136": {
				CommandID:   "136",
				Name:        "Temperature",
				Metric:      "temperature_c",
				Component:   ComponentSensor,
				Unit:        "\u00b0C",
				DeviceClass: "temperature",
				StateClass:  "measurement",
				Value:       21.0,
				LastValueAt: time.Unix(100, 0),
			},
			"379": {CommandID: "379", Metric: "smoke_alarm", Component: ComponentBinarySensor, DeviceClass: "smoke", Value: false},
			"380": {CommandID: "380", Metric: "heat_alarm", Component: ComponentBinarySensor, DeviceClass: "heat", Value: false},
			"381": {CommandID: "381", Metric: "rapid_temperature_rise_alarm", Component: ComponentBinarySensor, DeviceClass: "heat", Value: false},
			"382": {CommandID: "382", Metric: "carbon_monoxide_alarm", Component: ComponentBinarySensor, DeviceClass: "carbon_monoxide", Value: false},
			"383": {CommandID: "383", Metric: "critical_carbon_monoxide_alarm", Component: ComponentBinarySensor, DeviceClass: "carbon_monoxide", Value: false},
			"394": {CommandID: "394", Metric: "critical_smoke_alarm", Component: ComponentBinarySensor, DeviceClass: "smoke", Value: false},
		},
	}

	if err := publisher.PublishDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	if got := mqtt.discovery["homeassistant/binary_sensor/ajaxbridge/jeedom_cmd_134/config"]; got != "" {
		t.Fatalf("SIA-owned bypass command discovery = %q, want retained cleanup", got)
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_134/config"]; got != "" {
		t.Fatalf("SIA-owned bypass sensor migration cleanup = %q, want empty", got)
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_136/config"]; got == "" {
		t.Fatalf("Jeedom temperature measurement discovery missing")
	}
	if got := mqtt.discovery["homeassistant/binary_sensor/ajaxbridge/jeedom_cmd_136/config"]; got != "" {
		t.Fatalf("stale Jeedom temperature binary discovery = %q, want retained cleanup", got)
	}
	for _, commandID := range []string{"379", "380", "382"} {
		topic := "homeassistant/binary_sensor/ajaxbridge/jeedom_cmd_" + commandID + "/config"
		if got := mqtt.discovery[topic]; got != "" {
			t.Fatalf("base SIA-owned fire alarm %s discovery = %q, want retained cleanup", commandID, got)
		}
	}
	for _, commandID := range []string{"381", "383", "394"} {
		topic := "homeassistant/binary_sensor/ajaxbridge/jeedom_cmd_" + commandID + "/config"
		if got := mqtt.discovery[topic]; got == "" {
			t.Fatalf("granular Jeedom fire alarm %s discovery missing", commandID)
		}
	}
}

func TestPublishDeviceCleansLegacyNameBasedDiscoveryForAllSensorTypes(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
	}, mqtt)

	device := Device{
		Source:            Source,
		Device:            "Ajax account A0F80D",
		DeviceSlug:        "account_a0f80d",
		BaseSlug:          "budinok",
		LegacyDeviceSlugs: []string{"budinok"},
		LinkedSource:      "sia",
		LinkedAccount:     "A0F80D",
		HAIdentifiers:     []string{"ajaxbridge_account_A0F80D"},
		Values:            map[string]any{},
		RawCommands: map[string]Command{
			"10": {
				CommandID:  "10",
				ObjectName: "Zahidna 20",
				Device:     "Будинок",
				DeviceSlug: "account_a0f80d",
				Name:       "Battery",
				RawName:    "Batterie",
				Metric:     "battery_percent",
				Component:  ComponentSensor,
			},
			"11": {
				CommandID:  "11",
				ObjectName: "Zahidna 20",
				Device:     "Будинок",
				DeviceSlug: "account_a0f80d",
				Name:       "External power",
				RawName:    "Alimentation secteur",
				Metric:     "external_power",
				Component:  ComponentBinarySensor,
			},
		},
	}

	if err := publisher.PublishDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	for _, topic := range []string{
		"homeassistant/sensor/ajaxbridge/budinok_battery/config",
		"homeassistant/binary_sensor/ajaxbridge/budinok_battery/config",
		"homeassistant/binary_sensor/ajaxbridge/budinok_external_power/config",
		"homeassistant/sensor/ajaxbridge/budinok_external_power/config",
	} {
		if got := mqtt.discovery[topic]; got != "" {
			t.Fatalf("legacy topic %s cleanup payload = %q, want empty", topic, got)
		}
		if _, ok := mqtt.discovery[topic]; !ok {
			t.Fatalf("missing legacy cleanup for %s: %#v", topic, mqtt.discovery)
		}
	}
	if got := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_10/config"]; got == "" {
		t.Fatalf("current battery discovery missing")
	}
	if got := mqtt.discovery["homeassistant/binary_sensor/ajaxbridge/jeedom_cmd_11/config"]; got != "" {
		t.Fatalf("SIA-owned external power command should be cleaned by command id, got %q", got)
	}
}

func TestPublishDeviceKeepsControlStateForLinkedSIADevice(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainDiscovery:  true,
	}, mqtt)

	device := Device{
		Source:        Source,
		Device:        "Server power",
		DeviceSlug:    "sia_a0f80d_zone_8",
		LinkedSource:  "sia",
		LinkedAccount: "A0F80D",
		LinkedZone:    "8",
		HAIdentifiers: []string{"ajaxbridge_A0F80D_zone_8"},
		Values:        map[string]any{},
		RawCommands: map[string]Command{
			"52": {
				CommandID: "52",
				Name:      "State",
				Metric:    "state",
				Component: ComponentBinarySensor,
			},
		},
	}

	if err := publisher.PublishDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	if got := mqtt.discovery["homeassistant/binary_sensor/ajaxbridge/jeedom_cmd_52/config"]; got == "" {
		t.Fatalf("Jeedom control state discovery should stay for linked SIA device")
	}
}

func TestPublishDeviceSeparatesStableAttributesFromFullState(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	device := Device{
		Source:           Source,
		ObjectName:       "House",
		Device:           "Server power",
		DeviceSlug:       "sia_a0f80d_zone_8",
		JeedomID:         "7",
		JeedomLogicalID:  "30E81A2B",
		JeedomDeviceType: "WallSwitch",
		LastUpdate:       time.Unix(100, 0),
		Values:           map[string]any{"power_w": 120.5, "state": true},
		RawCommands: map[string]Command{
			"56": {
				CommandID:  "56",
				Metric:     "power_w",
				Value:      120.5,
				LastUpdate: time.Unix(100, 0),
			},
		},
		Actions: map[string]Action{
			"on": {Action: "on", CommandID: "58", Allowed: true},
		},
	}

	if err := publisher.PublishDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	var statePayload map[string]any
	if err := json.Unmarshal([]byte(mqtt.state["ajaxbridge/jeedom/devices/sia_a0f80d_zone_8/state"]), &statePayload); err != nil {
		t.Fatal(err)
	}
	if _, ok := statePayload["raw_commands"]; !ok {
		t.Fatalf("existing MQTT state contract lost raw_commands: %#v", statePayload)
	}
	if _, ok := statePayload["last_update"]; !ok {
		t.Fatalf("existing MQTT state contract lost last_update: %#v", statePayload)
	}

	var attributes map[string]any
	if err := json.Unmarshal([]byte(mqtt.state["ajaxbridge/jeedom/devices/sia_a0f80d_zone_8/attributes"]), &attributes); err != nil {
		t.Fatal(err)
	}
	if got := attributes["jeedom_device_type"]; got != "WallSwitch" {
		t.Fatalf("jeedom_device_type = %#v, want WallSwitch", got)
	}
	for _, volatile := range []string{"raw_commands", "actions", "last_update", "last_value_at", "power_w", "state"} {
		if _, ok := attributes[volatile]; ok {
			t.Fatalf("volatile field %q leaked into stable attributes: %#v", volatile, attributes)
		}
	}
}

func TestPublisherRejectsOlderDeviceSnapshot(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	newer := Device{
		Device:          "Server",
		DeviceSlug:      "server",
		Values:          map[string]any{"power_w": 2.0},
		publishRevision: 2,
	}
	older := newer
	older.Values = map[string]any{"power_w": 1.0}
	older.publishRevision = 1

	if err := publisher.PublishDevice(t.Context(), newer); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDevice(t.Context(), older); err != nil {
		t.Fatal(err)
	}
	assertRecordedPower(t, mqtt.state["ajaxbridge/jeedom/devices/server/state"], 2)
}

func TestPublishDeviceWithResultReportsSkippedStaleCleanup(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
	}, mqtt)
	newer := Device{Device: "Remote", DeviceSlug: "remote", Values: map[string]any{}, publishRevision: 2}
	older := Device{
		Device:          "Remote",
		DeviceSlug:      "remote",
		Values:          map[string]any{},
		publishRevision: 1,
		PendingDiscoveryCleanups: []Command{{
			CommandID: "369",
			Metric:    "issue_count",
			Component: ComponentSensor,
		}},
	}
	if published, err := publisher.PublishDeviceWithResult(t.Context(), newer); err != nil || !published {
		t.Fatalf("newer publish = %v, %v", published, err)
	}
	published, err := publisher.PublishDeviceWithResult(t.Context(), older)
	if err != nil {
		t.Fatal(err)
	}
	if published {
		t.Fatal("stale snapshot reported a successful publish")
	}
	if _, ok := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_369/config"]; ok {
		t.Fatal("stale snapshot published its cleanup")
	}
}

func TestPublisherRejectsUnversionedSnapshotAfterVersionedSnapshot(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	versioned := Device{
		Device:          "Server",
		DeviceSlug:      "server",
		Values:          map[string]any{"power_w": 2.0},
		publishRevision: 2,
	}
	unversioned := versioned
	unversioned.Values = map[string]any{"power_w": 0.0}
	unversioned.publishRevision = 0

	if err := publisher.PublishDevice(t.Context(), versioned); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDevice(t.Context(), unversioned); err != nil {
		t.Fatal(err)
	}
	assertRecordedPower(t, mqtt.state["ajaxbridge/jeedom/devices/server/state"], 2)
}

func TestPublisherAllowsEqualRevisionReconnectReplay(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	device := Device{
		Device:          "Server",
		DeviceSlug:      "server",
		Values:          map[string]any{"power_w": 2.0},
		publishRevision: 2,
	}

	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	if got := mqtt.stateCalls["ajaxbridge/jeedom/devices/server/state"]; got != 2 {
		t.Fatalf("equal-revision state publishes = %d, want 2 for reconnect replay", got)
	}
}

func TestPublisherPreventsStaleLegacySnapshotFromUndoingCleanup(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainState:      true,
		RetainDiscovery:  true,
	}, mqtt)
	canonical := Device{
		Device:            "Linked SIA device",
		DeviceSlug:        "sia_a0f80d_zone_8",
		LegacyDeviceSlugs: []string{"server"},
		Values:            map[string]any{"power_w": 2.0},
		publishRevision:   2,
	}
	staleLegacy := Device{
		Device:          "Old Jeedom device",
		DeviceSlug:      "server",
		Values:          map[string]any{"power_w": 1.0},
		publishRevision: 1,
	}

	if err := publisher.PublishDevice(t.Context(), canonical); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDevice(t.Context(), staleLegacy); err != nil {
		t.Fatal(err)
	}
	if got := mqtt.state["ajaxbridge/jeedom/devices/server/state"]; got != "" {
		t.Fatalf("stale legacy snapshot repopulated cleaned retained state: %q", got)
	}
}

func TestPublisherSharedLegacyAliasDoesNotSuppressCanonicalDevices(t *testing.T) {
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	zone2 := Device{
		Device:            "Diana zone 2",
		DeviceSlug:        "sia_a0f80d_zone_2",
		LegacyDeviceSlugs: []string{"diana"},
		Values:            map[string]any{"power_w": 2.0},
		publishRevision:   2,
	}
	zone20 := Device{
		Device:            "Diana zone 20",
		DeviceSlug:        "sia_a0f80d_zone_20",
		LegacyDeviceSlugs: []string{"diana"},
		Values:            map[string]any{"power_w": 20.0},
		publishRevision:   1,
	}
	staleLegacy := Device{
		Device:          "Old Diana",
		DeviceSlug:      "diana",
		Values:          map[string]any{"power_w": 1.0},
		publishRevision: 1,
	}

	if err := publisher.PublishDevice(t.Context(), zone2); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishDevice(t.Context(), zone20); err != nil {
		t.Fatal(err)
	}
	assertRecordedPower(t, mqtt.state["ajaxbridge/jeedom/devices/sia_a0f80d_zone_20/state"], 20)
	if err := publisher.PublishDevice(t.Context(), staleLegacy); err != nil {
		t.Fatal(err)
	}
	if got := mqtt.state["ajaxbridge/jeedom/devices/diana/state"]; got != "" {
		t.Fatalf("stale shared legacy snapshot repopulated cleanup: %q", got)
	}
}

func TestPublisherCleansTransitiveLegacyAliasChain(t *testing.T) {
	store := NewStore("keep_last")
	store.replaceDevices([]Device{
		{Device: "Canonical A", DeviceSlug: "a", LegacyDeviceSlugs: []string{"b"}, Values: map[string]any{"power_w": 3.0}},
		{Device: "Legacy B", DeviceSlug: "b", LegacyDeviceSlugs: []string{"c"}, Values: map[string]any{"power_w": 2.0}},
		{Device: "Legacy C", DeviceSlug: "c", Values: map[string]any{"power_w": 1.0}},
	})
	mqtt := &recordingMQTT{state: map[string]string{
		"ajaxbridge/jeedom/devices/b/state": `{"power_w":2}`,
		"ajaxbridge/jeedom/devices/c/state": `{"power_w":1}`,
	}}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
		RetainState:      true,
	}, mqtt)

	for _, device := range store.Devices() {
		if err := publisher.PublishDevice(t.Context(), device); err != nil {
			t.Fatal(err)
		}
	}
	assertRecordedPower(t, mqtt.state["ajaxbridge/jeedom/devices/a/state"], 3)
	for _, slug := range []string{"b", "c"} {
		if got := mqtt.state["ajaxbridge/jeedom/devices/"+slug+"/state"]; got != "" {
			t.Fatalf("transitive legacy %s retained state = %q, want cleanup", slug, got)
		}
	}
}

func TestPublisherSerializesSameDeviceThroughMQTTPublish(t *testing.T) {
	topic := "ajaxbridge/jeedom/devices/server/state"
	mqtt := newOrderedStateMQTT(topic)
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	older := Device{Device: "Server", DeviceSlug: "server", Values: map[string]any{"power_w": 1.0}, publishRevision: 1}
	newer := Device{Device: "Server", DeviceSlug: "server", Values: map[string]any{"power_w": 2.0}, publishRevision: 2}

	errA := make(chan error, 1)
	go func() {
		errA <- publisher.PublishDevice(t.Context(), older)
	}()
	select {
	case <-mqtt.firstEntered:
	case <-time.After(time.Second):
		t.Fatal("older snapshot never reached MQTT")
	}

	errB := make(chan error, 1)
	go func() {
		errB <- publisher.PublishDevice(t.Context(), newer)
	}()
	select {
	case <-mqtt.secondEntered:
		t.Fatal("newer same-device snapshot entered MQTT before older publish completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(mqtt.releaseFirst)
	if err := <-errA; err != nil {
		t.Fatal(err)
	}
	if err := <-errB; err != nil {
		t.Fatal(err)
	}
	assertRecordedPower(t, mqtt.statePayload(topic), 2)
}

func TestPublisherDoesNotSerializeDifferentDevices(t *testing.T) {
	mqtt := newBlockingStateMQTT("ajaxbridge/jeedom/devices/device_a/state")
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	deviceA := Device{Device: "Device A", DeviceSlug: "device_a", Values: map[string]any{"power_w": 1.0}, publishRevision: 1}
	deviceB := Device{Device: "Device B", DeviceSlug: "device_b", Values: map[string]any{"power_w": 2.0}, publishRevision: 2}

	errA := make(chan error, 1)
	go func() {
		errA <- publisher.PublishDevice(t.Context(), deviceA)
	}()
	select {
	case <-mqtt.entered:
	case <-time.After(time.Second):
		t.Fatal("device A never reached the blocking MQTT publish")
	}

	errB := make(chan error, 1)
	go func() {
		errB <- publisher.PublishDevice(t.Context(), deviceB)
	}()
	select {
	case err := <-errB:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(mqtt.release)
		<-errA
		t.Fatal("device B was unnecessarily serialized behind device A")
	}
	close(mqtt.release)
	if err := <-errA; err != nil {
		t.Fatal(err)
	}
}

func assertRecordedPower(t *testing.T, payload string, want float64) {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		t.Fatal(err)
	}
	if got := state["power_w"]; got != want {
		t.Fatalf("retained power_w = %#v, want %v (payload %s)", got, want, payload)
	}
}

type fakeMQTT struct{}

func (fakeMQTT) PublishStateMessage(context.Context, string, []byte, bool) error {
	return nil
}

func (fakeMQTT) PublishDiscoveryMessage(context.Context, string, string, []byte, bool) error {
	return nil
}

func (fakeMQTT) AvailabilityTopic() string {
	return "ajaxbridge/status"
}

type recordingMQTT struct {
	state           map[string]string
	stateRetain     map[string]bool
	stateCalls      map[string]int
	discovery       map[string]string
	discoveryRetain map[string]bool
}

func (m *recordingMQTT) PublishStateMessage(_ context.Context, topic string, payload []byte, retain bool) error {
	if m.state == nil {
		m.state = make(map[string]string)
	}
	if m.stateRetain == nil {
		m.stateRetain = make(map[string]bool)
	}
	if m.stateCalls == nil {
		m.stateCalls = make(map[string]int)
	}
	m.state[topic] = string(payload)
	m.stateRetain[topic] = retain
	m.stateCalls[topic]++
	return nil
}

func (m *recordingMQTT) PublishDiscoveryMessage(_ context.Context, _ string, topic string, payload []byte, retain bool) error {
	if m.discovery == nil {
		m.discovery = make(map[string]string)
	}
	if m.discoveryRetain == nil {
		m.discoveryRetain = make(map[string]bool)
	}
	m.discovery[topic] = string(payload)
	m.discoveryRetain[topic] = retain
	return nil
}

func (m *recordingMQTT) AvailabilityTopic() string {
	return "ajaxbridge/status"
}

type blockingStateMQTT struct {
	blockTopic string
	entered    chan struct{}
	release    chan struct{}
	once       sync.Once
	mu         sync.Mutex
	state      map[string]string
}

type orderedStateMQTT struct {
	blockTopic    string
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseFirst  chan struct{}
	mu            sync.Mutex
	calls         int
	state         map[string]string
}

func newOrderedStateMQTT(blockTopic string) *orderedStateMQTT {
	return &orderedStateMQTT{
		blockTopic:    blockTopic,
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		state:         make(map[string]string),
	}
}

func (m *orderedStateMQTT) PublishStateMessage(_ context.Context, topic string, payload []byte, _ bool) error {
	if topic == m.blockTopic {
		m.mu.Lock()
		m.calls++
		call := m.calls
		m.mu.Unlock()
		switch call {
		case 1:
			close(m.firstEntered)
			<-m.releaseFirst
		case 2:
			close(m.secondEntered)
		}
	}
	m.mu.Lock()
	m.state[topic] = string(payload)
	m.mu.Unlock()
	return nil
}

func (*orderedStateMQTT) PublishDiscoveryMessage(context.Context, string, string, []byte, bool) error {
	return nil
}

func (*orderedStateMQTT) AvailabilityTopic() string {
	return "ajaxbridge/status"
}

func (m *orderedStateMQTT) statePayload(topic string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state[topic]
}

func newBlockingStateMQTT(blockTopic string) *blockingStateMQTT {
	return &blockingStateMQTT{
		blockTopic: blockTopic,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
		state:      make(map[string]string),
	}
}

func (m *blockingStateMQTT) PublishStateMessage(_ context.Context, topic string, payload []byte, _ bool) error {
	if topic == m.blockTopic {
		m.once.Do(func() {
			close(m.entered)
			<-m.release
		})
	}
	m.mu.Lock()
	m.state[topic] = string(payload)
	m.mu.Unlock()
	return nil
}

func (*blockingStateMQTT) PublishDiscoveryMessage(context.Context, string, string, []byte, bool) error {
	return nil
}

func (*blockingStateMQTT) AvailabilityTopic() string {
	return "ajaxbridge/status"
}
