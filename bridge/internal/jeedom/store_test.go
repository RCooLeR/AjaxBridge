package jeedom

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
)

func TestStoreEmptyValueKeepsLastNumericValue(t *testing.T) {
	store := NewStore("keep_last")
	now := time.Unix(100, 0)

	first := Event{
		Topic:       "jeedom/cmd/event/56",
		CommandID:   "56",
		ObjectName:  "None",
		DeviceName:  "Серверна",
		CommandName: "Puissance",
		Unit:        "W",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`123.4`),
		ReceivedAt:  now,
	}
	store.Apply(first)

	empty := first
	empty.Value = json.RawMessage(`""`)
	empty.ReceivedAt = now.Add(time.Second)
	result := store.Apply(empty)

	if !result.EmptyValue {
		t.Fatal("expected empty value result")
	}
	device, ok := store.Device("serverna")
	if !ok {
		t.Fatal("missing device state")
	}
	if got := device.Values["power_w"]; got != 123.4 {
		t.Fatalf("power_w = %#v, want 123.4", got)
	}
}

func TestStoreUnknownValuePreservesLastValidHistory(t *testing.T) {
	store := NewStore("unknown")
	now := time.Unix(100, 0)
	event := Event{
		Topic:       "jeedom/cmd/event/56",
		CommandID:   "56",
		DeviceName:  "Server",
		CommandName: "Puissance",
		Unit:        "W",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`12.5`),
		ReceivedAt:  now,
	}
	store.Apply(event)

	event.Value = json.RawMessage(`""`)
	event.ReceivedAt = now.Add(time.Second)
	result := store.Apply(event)
	command := result.Device.RawCommands["56"]

	if !result.EmptyValue || !result.UpdatedValue || !command.EmptyValue {
		t.Fatalf("empty result = %#v, command = %#v", result, command)
	}
	if !command.LastValueAt.Equal(now) {
		t.Fatalf("LastValueAt = %s, want %s", command.LastValueAt, now)
	}
	value, present := result.Device.Values["power_w"]
	if !present || value != nil {
		t.Fatalf("power_w = %#v, present=%v; want explicit nil", value, present)
	}
	if !commandDiscoverable(command, result.Device) {
		t.Fatal("temporarily missing previously valid measurement became undiscoverable")
	}
}

func TestStorePreservesZeroAndFalseAsUsableValues(t *testing.T) {
	store := NewStore("keep_last")
	now := time.Unix(100, 0)
	power := store.Apply(Event{
		Topic:       "jeedom/cmd/event/56",
		CommandID:   "56",
		DeviceName:  "Server",
		CommandName: "Puissance",
		Unit:        "W",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`0`),
		ReceivedAt:  now,
	})
	state := store.Apply(Event{
		Topic:       "jeedom/cmd/event/57",
		CommandID:   "57",
		DeviceName:  "Server",
		CommandName: "Etat",
		Type:        "info",
		Subtype:     "binary",
		Value:       json.RawMessage(`false`),
		ReceivedAt:  now.Add(time.Second),
	})

	if !power.UpdatedValue || !power.HasNumeric || power.NumericValue != 0 {
		t.Fatalf("zero power result = %#v", power)
	}
	if power.Command.LastValueAt.IsZero() || power.Command.EmptyValue {
		t.Fatalf("zero power command = %#v", power.Command)
	}
	if !state.UpdatedValue || state.Command.LastValueAt.IsZero() || state.Command.EmptyValue {
		t.Fatalf("false state result = %#v", state)
	}
	if value, ok := state.Device.Values["state"]; !ok || value != false {
		t.Fatalf("state = %#v, present=%v; want false", value, ok)
	}
	payload := StatePayload(state.Device)
	if value, ok := payload["power_w"]; !ok || value != float64(0) {
		t.Fatalf("state payload power_w = %#v, present=%v; want 0", value, ok)
	}
	if value, ok := payload["state"]; !ok || value != false {
		t.Fatalf("state payload state = %#v, present=%v; want false", value, ok)
	}
	if _, ok := payload["energy_kwh"]; ok {
		t.Fatalf("state payload synthesized energy_kwh: %#v", payload)
	}
}

func TestStoreDoesNotSynthesizeZeroForMalformedMeasurement(t *testing.T) {
	result := NewStore("keep_last").Apply(Event{
		Topic:       "jeedom/cmd/event/56",
		CommandID:   "56",
		DeviceName:  "Server",
		CommandName: "Puissance",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`"not-a-number"`),
		ReceivedAt:  time.Unix(100, 0),
	})

	if result.UpdatedValue || result.HasNumeric {
		t.Fatalf("malformed measurement result = %#v", result)
	}
	if !result.Command.LastValueAt.IsZero() || result.Command.Value != nil {
		t.Fatalf("malformed measurement command = %#v", result.Command)
	}
	if _, ok := result.Device.Values["power_w"]; ok {
		t.Fatalf("malformed measurement synthesized power_w: %#v", result.Device.Values)
	}
}

func TestStoreRejectsNonFiniteMeasurement(t *testing.T) {
	for _, raw := range []string{`"NaN"`, `"Inf"`, `"-Infinity"`} {
		t.Run(raw, func(t *testing.T) {
			result := NewStore("keep_last").Apply(Event{
				Topic:       "jeedom/cmd/event/56",
				CommandID:   "56",
				DeviceName:  "Server",
				CommandName: "Puissance",
				Type:        "info",
				Subtype:     "numeric",
				Value:       json.RawMessage(raw),
				ReceivedAt:  time.Unix(100, 0),
			})
			if result.UpdatedValue || result.HasNumeric || !result.Command.LastValueAt.IsZero() {
				t.Fatalf("non-finite measurement was accepted: %#v", result)
			}
			if _, ok := result.Device.Values["power_w"]; ok {
				t.Fatalf("non-finite measurement reached state: %#v", result.Device.Values)
			}
		})
	}
}

func TestStoreKeepsEnglishCommandNameAndRawFrenchName(t *testing.T) {
	store := NewStore("keep_last")
	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/12",
		CommandID:   "12",
		DeviceName:  "Hub",
		CommandName: "Alimentation secteur",
		Subtype:     "binary",
		Value:       json.RawMessage(`1`),
		ReceivedAt:  time.Unix(100, 0),
	})

	if result.Command.Name != "External power" {
		t.Fatalf("command name = %q, want External power", result.Command.Name)
	}
	if result.Command.RawName != "Alimentation secteur" {
		t.Fatalf("raw name = %q, want original French", result.Command.RawName)
	}
}

func TestStoreDisambiguatesDuplicateDeviceNamesByCommandGroup(t *testing.T) {
	store := NewStore("keep_last")
	now := time.Unix(100, 0)

	events := []Event{
		{Topic: "jeedom/cmd/event/60", CommandID: "60", DeviceName: "Дим", CommandName: "Etat", Value: json.RawMessage(`"PASSIVE"`), ReceivedAt: now},
		{Topic: "jeedom/cmd/event/64", CommandID: "64", DeviceName: "Дим", CommandName: "Température", Value: json.RawMessage(`15`), ReceivedAt: now},
		{Topic: "jeedom/cmd/event/96", CommandID: "96", DeviceName: "Дим", CommandName: "Etat", Value: json.RawMessage(`"PASSIVE"`), ReceivedAt: now},
		{Topic: "jeedom/cmd/event/100", CommandID: "100", DeviceName: "Дим", CommandName: "Température", Value: json.RawMessage(`24`), ReceivedAt: now},
	}
	for _, evt := range events {
		store.Apply(evt)
	}

	devices := store.Devices()
	if len(devices) != 2 {
		t.Fatalf("devices length = %d, want 2: %#v", len(devices), devices)
	}
	if _, ok := store.Device("dym"); !ok {
		t.Fatal("missing first duplicate group slug dym")
	}
	if _, ok := store.Device("dym_96"); !ok {
		t.Fatal("missing second duplicate group slug dym_96")
	}
}

func TestStoreTracksLegacyUnlinkedSlugWhenCatalogLinksDevice(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "8",
		Name:             "Server power",
		Kind:             "WallSwitch",
		JeedomNames:      []string{"Serverna"},
		JeedomCommandIDs: []string{"56"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/56",
		CommandID:   "56",
		DeviceName:  "Serverna",
		CommandName: "Puissance",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`100`),
		ReceivedAt:  time.Unix(100, 0),
	})

	if result.Device.DeviceSlug != "sia_a0f80d_zone_8" {
		t.Fatalf("DeviceSlug = %q, want linked SIA slug", result.Device.DeviceSlug)
	}
	if !containsString(result.Device.LegacyDeviceSlugs, "serverna") {
		t.Fatalf("LegacyDeviceSlugs = %#v, want serverna", result.Device.LegacyDeviceSlugs)
	}
}

func TestStoreFindsLegacyActionsForLinkedSIADevice(t *testing.T) {
	store := NewStoreWithResolver("keep_last", legacyActionResolver{})
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/14", []byte(`{
	  "id":14,
	  "name":"Battery",
	  "configuration":{"device":"Socket"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "216":{"id":216,"logicalId":"realState","name":"Etat","type":"info","subType":"binary","isVisible":1},
	    "220":{"id":220,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","isVisible":1,"value":"216"},
	    "221":{"id":221,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","isVisible":1,"value":"216"}
	  }
	}`), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	store.Apply(Event{
		Topic:       "jeedom/cmd/event/216",
		CommandID:   "216",
		DeviceName:  "Battery",
		CommandName: "Etat",
		Type:        "info",
		Subtype:     "binary",
		Value:       json.RawMessage(`1`),
		ReceivedAt:  time.Unix(101, 0),
	})

	action, ok := store.Action("sia_a0f80d_zone_14", "OFF")
	if !ok {
		t.Fatal("missing linked SIA off action")
	}
	if action.CommandID != "221" || action.DeviceSlug != "sia_a0f80d_zone_14" || action.DeviceType != "Socket" {
		t.Fatalf("action = %#v", action)
	}
	byCommandID, ok := store.ActionByCommandID("221")
	if !ok || byCommandID.DeviceSlug != "sia_a0f80d_zone_14" {
		t.Fatalf("ActionByCommandID selected legacy mirror: %#v, found=%v", byCommandID, ok)
	}
	device, ok := store.Device("sia_a0f80d_zone_14")
	if !ok {
		t.Fatal("missing linked SIA device")
	}
	if _, ok := device.Actions["off"]; !ok {
		t.Fatalf("linked SIA device did not inherit actions: %#v", device.Actions)
	}
}

func TestStorePropagatesLegacyDiscoveryActionsToExistingLinkedDevice(t *testing.T) {
	store := NewStoreWithResolver("keep_last", legacyActionResolver{})
	store.Apply(Event{
		Topic:       "jeedom/cmd/event/216",
		CommandID:   "216",
		DeviceName:  "Battery",
		CommandName: "Etat",
		Type:        "info",
		Subtype:     "binary",
		Value:       json.RawMessage(`0`),
		ReceivedAt:  time.Unix(100, 0),
	})

	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/14", []byte(`{
	  "id":14,
	  "name":"Battery",
	  "configuration":{"device":"Socket"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "220":{"id":220,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","isVisible":1},
	    "221":{"id":221,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","isVisible":1}
	  }
	}`), time.Unix(101, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := store.ApplyDiscovery(discovery)

	if result.Device.DeviceSlug != "sia_a0f80d_zone_14" {
		t.Fatalf("discovery result device = %q, want linked SIA slug", result.Device.DeviceSlug)
	}
	if result.Device.Actions["on"].CommandID != "220" || result.Device.Actions["off"].CommandID != "221" {
		t.Fatalf("linked actions = %#v", result.Device.Actions)
	}

	delete(discovery.Actions, "221")
	discovery.ReceivedAt = time.Unix(102, 0)
	refreshed := store.ApplyDiscovery(discovery)
	if _, ok := refreshed.Device.Actions["off"]; ok {
		t.Fatalf("removed legacy off action survived on linked target: %#v", refreshed.Device.Actions)
	}
	if refreshed.Device.Actions["on"].CommandID != "220" {
		t.Fatalf("remaining linked action = %#v", refreshed.Device.Actions)
	}
	if len(refreshed.Device.PendingActionDiscoveryCleanups) != 1 || refreshed.Device.PendingActionDiscoveryCleanups[0].CommandID != "221" || refreshed.Device.PendingActionDiscoveryCleanups[0].DeviceSlug != "sia_a0f80d_zone_14" {
		t.Fatalf("linked action cleanup queue = %#v", refreshed.Device.PendingActionDiscoveryCleanups)
	}
	legacyCleanupFound := false
	for _, previous := range refreshed.PreviousDevices {
		for _, cleanup := range previous.PendingActionDiscoveryCleanups {
			if cleanup.CommandID == "221" && cleanup.DeviceSlug == "battery" {
				legacyCleanupFound = true
			}
		}
	}
	if !legacyCleanupFound {
		t.Fatalf("previous devices = %#v, missing legacy off/221 cleanup", refreshed.PreviousDevices)
	}
}

func TestStoreApplyKeepsDiscoveredCommandContractByID(t *testing.T) {
	store := NewStore("keep_last")
	store.ApplyDiscovery(Discovery{
		EqLogicID: "50",
		Name:      "Life quality",
		InfoCommands: map[string]DiscoveryCommand{
			"500": {
				CommandID:   "500",
				EqLogicID:   "50",
				LogicalID:   "actualCO2",
				GenericType: "CO2",
				Name:        "CO2",
				Type:        "info",
				Subtype:     "numeric",
				Unit:        "ppm",
				Value:       json.RawMessage(`650`),
			},
			"501": {
				CommandID: "501",
				EqLogicID: "50",
				Name:      "Version du firmware",
				Type:      "info",
				Subtype:   "string",
				Value:     json.RawMessage(`"1.2.3"`),
			},
		},
	})

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/500",
		CommandID:   "500",
		DeviceName:  "Life quality",
		CommandName: "Power renamed by user",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`700`),
		ReceivedAt:  time.Unix(101, 0),
	})

	if result.Mapping.Metric != "co2_ppm" || result.Command.Metric != "co2_ppm" {
		t.Fatalf("renamed event remapped discovered contract: result=%#v command=%#v", result.Mapping, result.Command)
	}
	if got := result.Device.Values["co2_ppm"]; got != float64(700) {
		t.Fatalf("co2_ppm = %#v, want 700", got)
	}
	if _, exists := result.Device.Values["power_renamed_by_user_value"]; exists {
		t.Fatalf("fallback metric leaked into state: %#v", result.Device.Values)
	}

	firmware := store.Apply(Event{
		Topic:       "jeedom/cmd/event/501",
		CommandID:   "501",
		DeviceName:  "Life quality",
		CommandName: "Power",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"1.2.4"`),
		ReceivedAt:  time.Unix(102, 0),
	})
	if firmware.Mapping.Metric != "firmware_version" || firmware.Mapping.Numeric {
		t.Fatalf("renamed string contract = %#v", firmware.Mapping)
	}
	if got := firmware.Device.Values["firmware_version"]; got != "1.2.4" {
		t.Fatalf("firmware_version = %#v, want 1.2.4", got)
	}
}

func TestStatePayloadKeepsBridgeAndDeviceUpdateTimestampsSeparate(t *testing.T) {
	device := Device{
		Device:     "Button",
		DeviceSlug: "button",
		LastUpdate: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
		Values: map[string]any{
			"device_last_update": "2026-09-24T00:11:31Z",
		},
	}
	payload := StatePayload(device)
	if got := payload["last_update"]; got != "2026-09-24T12:00:00Z" {
		t.Fatalf("bridge last_update = %#v", got)
	}
	if got := payload["device_last_update"]; got != "2026-09-24T00:11:31Z" {
		t.Fatalf("device_last_update = %#v", got)
	}
}

func TestStoreNormalizesRelayVoltageFromCatalog(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "6",
		Name:             "Garage gate relay",
		Kind:             "Relay",
		JeedomCommandIDs: []string{"230"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/230",
		CommandID:   "230",
		DeviceName:  "Garage gate",
		CommandName: "Voltage",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`289.02`),
		ReceivedAt:  time.Unix(100, 0),
	})

	got, ok := result.Device.Values["voltage_v"].(float64)
	if !ok {
		t.Fatalf("voltage_v = %#v, want float64", result.Device.Values["voltage_v"])
	}
	if math.Abs(got-28.902) > 1e-9 {
		t.Fatalf("voltage_v = %#v, want 28.902", got)
	}
	if math.Abs(result.NumericValue-28.902) > 1e-9 {
		t.Fatalf("NumericValue = %#v, want 28.902", result.NumericValue)
	}
}

func TestStoreKeepsWallSwitchVoltageUnscaled(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "8",
		Name:             "Server power",
		Kind:             "WallSwitch",
		JeedomCommandIDs: []string{"203"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/203",
		CommandID:   "203",
		DeviceName:  "Server power",
		CommandName: "Voltage",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`238`),
		ReceivedAt:  time.Unix(100, 0),
	})

	if got := result.Device.Values["voltage_v"]; got != 238.0 {
		t.Fatalf("voltage_v = %#v, want 238", got)
	}
}

func TestSeedStoredRelayVoltageUsesPreviousMetric(t *testing.T) {
	for _, previous := range []struct {
		name      string
		metric    string
		component string
		value     float64
	}{
		{name: "same_contract", metric: "voltage_v", component: ComponentSensor, value: 28.902},
		{name: "component_change", metric: "voltage_v", component: ComponentBinarySensor, value: 28.902},
		{name: "legacy_fallback", metric: "legacy_voltage_value", component: ComponentSensor, value: 289.02},
	} {
		t.Run(previous.name, func(t *testing.T) {
			device := Device{DeviceSlug: "relay", JeedomDeviceType: "Relay", Values: map[string]any{}}
			existing := Command{CommandID: "230", Metric: previous.metric, Component: previous.component, Value: previous.value}
			mapping := MappingFor(Event{CommandName: "Voltage", Subtype: "numeric"})
			want := previous.value
			if previous.metric != "voltage_v" {
				want /= 10
			}
			for cycle := 0; cycle < 3; cycle++ {
				command := existing
				command.Metric = mapping.Metric
				command.Component = mapping.Component
				seedDeviceValueFromCommand(&device, &command, existing, mapping)
				if command.Value != want || device.Values["voltage_v"] != want {
					t.Fatalf("cycle %d stored voltage = %#v, want %v", cycle, command.Value, want)
				}
				existing = command
			}
		})
	}
}

func TestStoreDerivesWallSwitchStateFromEventCode(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "8",
		Name:             "Server power",
		Kind:             "WallSwitch",
		JeedomCommandIDs: []string{"204"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	on := store.Apply(Event{
		Topic:       "jeedom/cmd/event/204",
		CommandID:   "204",
		DeviceName:  "Server power",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_1F_37"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if got := on.Device.Values["state"]; got != true {
		t.Fatalf("state after M_1F_37 = %#v, want true", got)
	}
	if got := on.Device.Values["event_code"]; got != "M_1F_37" {
		t.Fatalf("event_code = %#v, want M_1F_37", got)
	}

	off := store.Apply(Event{
		Topic:       "jeedom/cmd/event/204",
		CommandID:   "204",
		DeviceName:  "Server power",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_1F_46"`),
		ReceivedAt:  time.Unix(101, 0),
	})
	if got := off.Device.Values["state"]; got != false {
		t.Fatalf("state after M_1F_46 = %#v, want false", got)
	}
}

func TestStoreDoesNotTurnWallSwitchOffFromZeroLoad(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "8",
		Name:             "Server power",
		Kind:             "WallSwitch",
		JeedomCommandIDs: []string{"203", "204"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	store.Apply(Event{
		Topic:       "jeedom/cmd/event/204",
		CommandID:   "204",
		DeviceName:  "Server power",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_1F_37"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/203",
		CommandID:   "203",
		DeviceName:  "Server power",
		CommandName: "Courant",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`0`),
		ReceivedAt:  time.Unix(101, 0),
	})

	if got := result.Device.Values["state"]; got != true {
		t.Fatalf("state after zero load = %#v, want retained true", got)
	}
}

func TestStoreDoesNotDeriveRelayStateFromWallSwitchEventCode(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "6",
		Name:             "Garage relay",
		Kind:             "Relay",
		JeedomCommandIDs: []string{"205"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/205",
		CommandID:   "205",
		DeviceName:  "Garage relay",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_1F_37"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if _, ok := result.Device.Values["state"]; ok {
		t.Fatalf("relay state = %#v, want no derived state", result.Device.Values["state"])
	}
}

func TestStoreDerivesWaterStopStateFromEventCode(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "12",
		Name:             "Water valve",
		Kind:             "WaterStop",
		JeedomCommandIDs: []string{"206"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	open := store.Apply(Event{
		Topic:       "jeedom/cmd/event/206",
		CommandID:   "206",
		DeviceName:  "Water valve",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_48_37"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if got := open.Device.Values["state"]; got != true {
		t.Fatalf("state after M_48_37 = %#v, want true", got)
	}
	if got := open.Device.Values["event_code"]; got != "M_48_37" {
		t.Fatalf("event_code = %#v, want M_48_37", got)
	}

	closed := store.Apply(Event{
		Topic:       "jeedom/cmd/event/206",
		CommandID:   "206",
		DeviceName:  "Water valve",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_48_46"`),
		ReceivedAt:  time.Unix(101, 0),
	})
	if got := closed.Device.Values["state"]; got != false {
		t.Fatalf("state after M_48_46 = %#v, want false", got)
	}
}

func TestStoreDoesNotDeriveWaterStopStateFromCommonEvent(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "12",
		Name:             "Water valve",
		Kind:             "WaterStop",
		JeedomCommandIDs: []string{"207"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/207",
		CommandID:   "207",
		DeviceName:  "Water valve",
		CommandName: "Evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"COMMON"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if _, ok := result.Device.Values["state"]; ok {
		t.Fatalf("state after COMMON event = %#v, want no derived state", result.Device.Values["state"])
	}
}

func TestStoreEqualTimeStateFeedbackBeatsWallSwitchInference(t *testing.T) {
	at := time.Unix(1_700_008_000, 0).UTC()
	device := &Device{
		Device:           "Fence light",
		DeviceSlug:       "fence_light",
		JeedomDeviceType: "WallSwitch",
		Values:           make(map[string]any),
		RawCommands: map[string]Command{
			"178": {CommandID: "178", Metric: "event_code", Component: ComponentSensor, Value: "M_1F_37", LastUpdate: at, LastValueAt: at},
			"180": {CommandID: "180", Metric: "power_w", Component: ComponentSensor, Value: float64(12), LastUpdate: at, LastValueAt: at},
			"420": {CommandID: "420", Metric: "state", Component: ComponentBinarySensor, Value: false, LastUpdate: at, LastValueAt: at},
		},
	}

	rebuildAllMetricValues(device)
	if state, exists := device.Values["state"]; !exists || state != false {
		t.Fatalf("equal-time WallSwitch state = %#v present=%v, want raw feedback false", state, exists)
	}
}

func TestStoreEqualTimeValvePositionBeatsWaterStopEvent(t *testing.T) {
	at := time.Unix(1_700_008_100, 0).UTC()
	device := &Device{
		Device:           "Water valve",
		DeviceSlug:       "water_valve",
		JeedomDeviceType: "WaterStop",
		Values:           make(map[string]any),
		RawCommands: map[string]Command{
			"310": {CommandID: "310", Metric: "event_code", Component: ComponentSensor, Value: "M_48_37", LastUpdate: at, LastValueAt: at},
			"414": {CommandID: "414", Metric: "valve_position", Component: ComponentSensor, Value: "INTERMEDIATE", LastUpdate: at, LastValueAt: at},
		},
	}

	rebuildAllMetricValues(device)
	if state, exists := device.Values["state"]; !exists || state != nil {
		t.Fatalf("equal-time WaterStop state = %#v present=%v, want intermediate/unknown", state, exists)
	}
}

func TestStoreNewerWallSwitchInferenceBeatsOlderRawState(t *testing.T) {
	feedbackAt := time.Unix(1_700_008_150, 0).UTC()
	eventAt := feedbackAt.Add(time.Second)
	device := &Device{
		Device:           "Fence light",
		DeviceSlug:       "fence_light",
		JeedomDeviceType: "WallSwitch",
		Values:           make(map[string]any),
		RawCommands: map[string]Command{
			"178": {CommandID: "178", Metric: "event_code", Component: ComponentSensor, Value: "M_1F_37", LastUpdate: eventAt, LastValueAt: eventAt},
			"420": {CommandID: "420", Metric: "state", Component: ComponentBinarySensor, Value: false, LastUpdate: feedbackAt, LastValueAt: feedbackAt},
		},
	}

	rebuildAllMetricValues(device)
	if state, exists := device.Values["state"]; !exists || state != true {
		t.Fatalf("newer WallSwitch event state = %#v present=%v, want true", state, exists)
	}
}

func TestStoreDoesNotOptimisticallyOverrideObservedToggleFeedback(t *testing.T) {
	at := time.Unix(1_700_008_200, 0).UTC()
	tests := []struct {
		name   string
		device Device
	}{
		{
			name: "WallSwitch raw state",
			device: Device{
				Device:           "Fence light",
				DeviceSlug:       "fence_light",
				JeedomDeviceType: "WallSwitch",
				Values:           map[string]any{"state": false},
				RawCommands: map[string]Command{
					"420": {CommandID: "420", Metric: "state", Value: false, LastUpdate: at, LastValueAt: at},
				},
				Actions: map[string]Action{"on": {Action: "on", CommandID: "182", DeviceSlug: "fence_light"}},
			},
		},
		{
			name: "WaterStop valve position",
			device: Device{
				Device:           "Water valve",
				DeviceSlug:       "water_valve",
				JeedomDeviceType: "WaterStop",
				Values:           map[string]any{"state": false, "valve_position": "CLOSED"},
				RawCommands: map[string]Command{
					"414": {CommandID: "414", Metric: "valve_position", Value: "CLOSED", LastUpdate: at, LastValueAt: at},
				},
				Actions: map[string]Action{"on": {Action: "on", CommandID: "312", DeviceSlug: "water_valve"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStore("keep_last")
			device := tt.device
			store.devices[device.DeviceSlug] = &device
			if snapshot, updated := store.RecordOptimisticControlState(device.Actions["on"], at.Add(time.Second)); updated {
				t.Fatalf("optimistic update = %#v, want authoritative feedback to remain unchanged", snapshot)
			}
			stored, ok := store.Device(device.DeviceSlug)
			if !ok || stored.Values["state"] != false {
				t.Fatalf("stored device = %#v, want state=false from feedback", stored)
			}
		})
	}
}

func TestReconcilePersistedWaterStopUsesObservedPositionOverOptimisticState(t *testing.T) {
	at := time.Unix(1_700_008_300, 0).UTC()
	device := &Device{
		Device:           "Water valve",
		DeviceSlug:       "water_valve",
		JeedomDeviceType: "WaterStop",
		Values:           map[string]any{"state": true, "valve_position": "CLOSED"},
		RawCommands: map[string]Command{
			"414": {
				CommandID:   "414",
				Metric:      "valve_position",
				Component:   ComponentSensor,
				Value:       "CLOSED",
				LastUpdate:  at,
				LastValueAt: at,
			},
		},
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "312", DeviceSlug: "water_valve"},
			"off": {Action: "off", CommandID: "313", DeviceSlug: "water_valve"},
		},
	}

	reconcilePersistedMappings(device)
	if state, exists := device.Values["state"]; !exists || state != false {
		t.Fatalf("reconciled WaterStop state = %#v present=%v, want CLOSED feedback false", state, exists)
	}
}

func TestStoreDerivesStickyTransmitterInputAlarmFromEventCode(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "14",
		Name:             "Physical input",
		Kind:             "Transmitter",
		JeedomCommandIDs: []string{"208"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	apply := func(at int64, code string) Device {
		t.Helper()
		result := store.Apply(Event{
			Topic:       "jeedom/cmd/event/208",
			CommandID:   "208",
			DeviceName:  "Physical input",
			CommandName: "Code evenement",
			Type:        "info",
			Subtype:     "string",
			Value:       json.RawMessage(strconv.Quote(code)),
			ReceivedAt:  time.Unix(at, 0),
		})
		return result.Device
	}

	alarmed := apply(100, "M_11_3F")
	if got := alarmed.Values["input_alarm"]; got != true {
		t.Fatalf("input_alarm after M_11_3F = %#v, want true", got)
	}
	if _, ok := alarmed.Values["state"]; ok {
		t.Fatalf("state = %#v, want no switch state", alarmed.Values["state"])
	}
	command := alarmed.RawCommands["input_alarm"]
	if command.CommandID != "" || command.Name != "Input alarm" || command.RawName != "Input alarm" ||
		command.Metric != "input_alarm" || command.Component != ComponentBinarySensor ||
		command.Type != "info" || command.Subtype != "binary" || command.DeviceClass != "safety" {
		t.Fatalf("synthetic input alarm command = %#v", command)
	}
	if command.Value != true || !command.LastValueAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("synthetic input alarm observation = %#v, want true at event time", command)
	}

	unknown := apply(101, "M_11_FF")
	if got := unknown.Values["input_alarm"]; got != true {
		t.Fatalf("input_alarm after unrelated event = %#v, want sticky true", got)
	}
	if got := unknown.RawCommands["input_alarm"].LastValueAt; !got.Equal(time.Unix(100, 0)) {
		t.Fatalf("input_alarm timestamp after unrelated event = %v, want original observation", got)
	}

	path := filepath.Join(t.TempDir(), "jeedom.json")
	store.SetPath(path)
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadStore(t.Context(), path, "keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	devices := restored.Devices()
	if len(devices) != 1 || devices[0].Values["input_alarm"] != true {
		t.Fatalf("restored input alarm = %#v, want one device with sticky input_alarm true", devices)
	}
	if command := devices[0].RawCommands["input_alarm"]; command.Value != true || !command.LastValueAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("restored synthetic input alarm command = %#v, want original true observation", command)
	}

	cleared := restored.Apply(Event{
		Topic:       "jeedom/cmd/event/208",
		CommandID:   "208",
		DeviceName:  "Physical input",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_11_40"`),
		ReceivedAt:  time.Unix(110, 0),
	})
	if got := cleared.Device.Values["input_alarm"]; got != false {
		t.Fatalf("input_alarm after M_11_40 = %#v, want false", got)
	}
}

func TestPersistedTransmitterMigratesGridPowerToRawInputAlarm(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	path := filepath.Join(t.TempDir(), "jeedom.json")
	store := NewStore("keep_last")
	store.SetPath(path)
	store.replaceDevices([]Device{{
		Device:           "Gate contact",
		DeviceSlug:       "sia_a0f80d_zone_24",
		HAModel:          "Transmitter",
		JeedomDeviceType: "Transmitter",
		Values:           map[string]any{"event_code": "M_11_40", "grid_power": true},
		RawCommands: map[string]Command{
			"208": {
				CommandID: "208", DeviceSlug: "sia_a0f80d_zone_24", Metric: "event_code",
				Component: ComponentSensor, Value: "M_11_40", LastUpdate: at, LastValueAt: at,
			},
			"grid_power": {
				DeviceSlug: "sia_a0f80d_zone_24", Metric: "grid_power", Component: ComponentBinarySensor,
				DeviceClass: "power", Value: true, LastUpdate: at, LastValueAt: at,
			},
		},
	}})

	device, ok := store.Device("sia_a0f80d_zone_24")
	if !ok {
		t.Fatal("missing restored ordinary Transmitter")
	}
	if _, exists := device.Values["grid_power"]; exists {
		t.Fatalf("stale grid_power value survived migration: %#v", device.Values)
	}
	if _, exists := device.RawCommands["grid_power"]; exists {
		t.Fatalf("stale synthetic command survived migration: %#v", device.RawCommands)
	}
	if got := device.Values["input_alarm"]; got != false {
		t.Fatalf("migrated input_alarm = %#v, want false from M_11_40", got)
	}
	if command := device.RawCommands["input_alarm"]; command.Value != false || command.DeviceClass != "safety" {
		t.Fatalf("migrated input alarm command = %#v, want safe raw input", command)
	}
	if len(device.PendingDiscoveryCleanups) != 1 || commandCleanupKey(device.PendingDiscoveryCleanups[0]) != "derived:binary_sensor:sia_a0f80d_zone_24:grid_power" {
		t.Fatalf("synthetic cleanup queue = %#v", device.PendingDiscoveryCleanups)
	}

	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	restoredDevice, _ := restored.Device("sia_a0f80d_zone_24")
	if len(restoredDevice.PendingDiscoveryCleanups) != 1 {
		t.Fatalf("persisted synthetic cleanup queue = %#v", restoredDevice.PendingDiscoveryCleanups)
	}
	if restoredDevice.Values["input_alarm"] != false || restoredDevice.RawCommands["input_alarm"].Value != false {
		t.Fatalf("restored input alarm migration = %#v", restoredDevice)
	}
	if !restored.AcknowledgeCommandCleanups(restoredDevice.PendingDiscoveryCleanups) {
		t.Fatal("synthetic cleanup was not acknowledged")
	}
	acknowledged, _ := restored.Device("sia_a0f80d_zone_24")
	if len(acknowledged.PendingDiscoveryCleanups) != 0 {
		t.Fatalf("acknowledged synthetic cleanup still queued: %#v", acknowledged.PendingDiscoveryCleanups)
	}
}

func TestLegacyGridPowerMigrationArbitratesStickyInputByObservationTime(t *testing.T) {
	for _, test := range []struct {
		name      string
		eventCode string
		eventAt   int64
		gridAt    int64
		want      bool
		wantAt    int64
	}{
		{
			name:      "unrelated newer event preserves legacy alarm",
			eventCode: "M_11_FF",
			eventAt:   200,
			gridAt:    100,
			want:      true,
			wantAt:    100,
		},
		{
			name:      "newer physical clear wins",
			eventCode: "M_11_40",
			eventAt:   200,
			gridAt:    100,
			want:      false,
			wantAt:    200,
		},
		{
			name:      "newer legacy alarm wins",
			eventCode: "M_11_40",
			eventAt:   100,
			gridAt:    200,
			want:      true,
			wantAt:    200,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewStore("keep_last")
			store.replaceDevices([]Device{{
				Device:           "Physical input",
				DeviceSlug:       "sia_a0f80d_zone_10",
				HAModel:          "Transmitter",
				JeedomDeviceType: "Transmitter",
				Values: map[string]any{
					"event_code": test.eventCode,
					"grid_power": false,
				},
				RawCommands: map[string]Command{
					"208": {
						CommandID: "208", Device: "Physical input", DeviceSlug: "sia_a0f80d_zone_10",
						Name: "Event code", RawName: "Code evenement", Metric: "event_code", Component: ComponentSensor,
						Type: "info", Subtype: "string", Value: test.eventCode,
						LastUpdate: time.Unix(test.eventAt, 0), LastValueAt: time.Unix(test.eventAt, 0),
					},
					"grid_power": {
						Device: "Physical input", DeviceSlug: "sia_a0f80d_zone_10",
						Name: "Grid power", RawName: "Grid power", Metric: "grid_power",
						Component: ComponentBinarySensor, DeviceClass: "power", Type: "info", Subtype: "binary",
						Value: false, LastUpdate: time.Unix(test.gridAt, 0), LastValueAt: time.Unix(test.gridAt, 0),
					},
				},
			}})

			device, ok := store.Device("sia_a0f80d_zone_10")
			if !ok {
				t.Fatal("migrated Transmitter is missing")
			}
			if got := device.Values["input_alarm"]; got != test.want {
				t.Fatalf("input_alarm = %#v, want %t", got, test.want)
			}
			command := device.RawCommands["input_alarm"]
			if command.Value != test.want || !command.LastValueAt.Equal(time.Unix(test.wantAt, 0)) {
				t.Fatalf("migrated input alarm = %#v, want %t at %d", command, test.want, test.wantAt)
			}
			if _, exists := device.RawCommands["grid_power"]; exists {
				t.Fatalf("legacy grid power survived: %#v", device.RawCommands)
			}
			if !hasCommandCleanupKey(device.PendingDiscoveryCleanups, "derived:binary_sensor:sia_a0f80d_zone_10:grid_power") {
				t.Fatalf("legacy discovery cleanup = %#v", device.PendingDiscoveryCleanups)
			}
		})
	}
}

func TestRemovingLastPhysicalEventCodeRemovesSyntheticInputAlarm(t *testing.T) {
	store := NewStore("keep_last")
	created := store.ApplyDiscovery(Discovery{
		EqLogicID:  "501",
		Name:       "Physical input",
		DeviceType: "Transmitter",
		ReceivedAt: time.Unix(100, 0),
		InfoCommands: map[string]DiscoveryCommand{
			"208": {
				CommandID: "208",
				Name:      "Code evenement",
				Type:      "info",
				Subtype:   "string",
				Value:     json.RawMessage(`"M_11_3F"`),
			},
		},
	})
	if created.Device.Values["input_alarm"] != true {
		t.Fatalf("initial input alarm = %#v", created.Device)
	}

	removed := store.ApplyDiscovery(Discovery{
		EqLogicID:    "501",
		Name:         "Physical input",
		DeviceType:   "Transmitter",
		ReceivedAt:   time.Unix(200, 0),
		InfoCommands: map[string]DiscoveryCommand{},
	})
	if _, exists := removed.Device.Values["input_alarm"]; exists {
		t.Fatalf("input_alarm survived source removal: %#v", removed.Device.Values)
	}
	if _, exists := removed.Device.RawCommands["input_alarm"]; exists {
		t.Fatalf("synthetic command survived source removal: %#v", removed.Device.RawCommands)
	}
	wantCleanup := "derived:binary_sensor:" + removed.Device.DeviceSlug + ":input_alarm"
	if !hasCommandCleanupKey(removed.Device.PendingDiscoveryCleanups, wantCleanup) {
		t.Fatalf("input alarm cleanup after source removal = %#v", removed.Device.PendingDiscoveryCleanups)
	}
}

func TestTransferringLastPhysicalEventCodeCleansSourceInputAlarm(t *testing.T) {
	store := NewStoreWithResolver("keep_last", transmitterNameResolver{})
	first := store.Apply(Event{
		Topic:       "jeedom/cmd/event/208",
		CommandID:   "208",
		DeviceName:  "Old input",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_11_3F"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if first.Device.Values["input_alarm"] != true {
		t.Fatalf("initial input alarm = %#v", first.Device)
	}

	moved := store.Apply(Event{
		Topic:       "jeedom/cmd/event/208",
		CommandID:   "208",
		DeviceName:  "New input",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_11_3F"`),
		ReceivedAt:  time.Unix(200, 0),
	})
	if moved.Device.DeviceSlug != "new_input" || moved.Device.Values["input_alarm"] != true {
		t.Fatalf("target input after transfer = %#v", moved.Device)
	}
	var source Device
	found := false
	for _, previous := range moved.PreviousDevices {
		if previous.DeviceSlug == "old_input" {
			source = previous
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("source snapshot missing after transfer: %#v", moved.PreviousDevices)
	}
	if _, exists := source.Values["input_alarm"]; exists {
		t.Fatalf("source input alarm survived command transfer: %#v", source.Values)
	}
	if _, exists := source.RawCommands["input_alarm"]; exists {
		t.Fatalf("source synthetic command survived transfer: %#v", source.RawCommands)
	}
	if !hasCommandCleanupKey(source.PendingDiscoveryCleanups, "derived:binary_sensor:old_input:input_alarm") {
		t.Fatalf("source input alarm cleanup after transfer = %#v", source.PendingDiscoveryCleanups)
	}
}

func TestWholeDeviceIdentityMoveCleansOldSyntheticInputAlarmDiscovery(t *testing.T) {
	store := NewStore("keep_last")
	created := store.ApplyDiscovery(Discovery{
		EqLogicID:  "501",
		Name:       "Legacy physical input",
		DeviceType: "Transmitter",
		ReceivedAt: time.Unix(100, 0),
		InfoCommands: map[string]DiscoveryCommand{
			"208": {
				CommandID: "208",
				Name:      "Code evenement",
				Type:      "info",
				Subtype:   "string",
				Value:     json.RawMessage(`"M_11_3F"`),
			},
		},
	})
	oldSlug := created.Device.DeviceSlug
	if oldSlug == "" || created.Device.Values["input_alarm"] != true {
		t.Fatalf("initial local Transmitter = %#v", created.Device)
	}

	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "10",
		Name:             "Physical input",
		Kind:             "Transmitter",
		JeedomCommandIDs: []string{"208"},
	})
	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))
	moved, ok := store.Device("sia_a0f80d_zone_10")
	if !ok || moved.Values["input_alarm"] != true {
		t.Fatalf("canonical Transmitter after identity move = %#v present=%v", moved, ok)
	}
	if _, oldExists := store.Device(oldSlug); oldExists {
		t.Fatalf("old identity %q survived whole-device move", oldSlug)
	}
	command := moved.RawCommands["input_alarm"]
	if command.DeviceSlug != moved.DeviceSlug {
		t.Fatalf("moved input alarm command = %#v", command)
	}
	wantCleanup := "derived:binary_sensor:" + oldSlug + ":input_alarm"
	if !hasCommandCleanupKey(moved.PendingDiscoveryCleanups, wantCleanup) {
		t.Fatalf("old input alarm discovery cleanup = %#v, want %s", moved.PendingDiscoveryCleanups, wantCleanup)
	}
}

func TestReconcileResolverKeepsRawInputAlarmAndCleansLegacyGridPower(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	catalog := testCatalog(t, devicecatalog.Device{
		Account: "A0F80D", Zone: "10", Name: "Grid detector", Kind: "Transmitter",
		JeedomCommandIDs: []string{"208"},
	})
	store := NewStore("keep_last")
	store.devices["sia_a0f80d_zone_10"] = &Device{
		Device:           "Grid detector",
		DeviceSlug:       "sia_a0f80d_zone_10",
		HAModel:          "Transmitter",
		JeedomDeviceType: "Transmitter",
		Values:           map[string]any{"event_code": "M_11_3F", "grid_power": false},
		RawCommands: map[string]Command{
			"208": {
				CommandID: "208", Device: "Grid detector", DeviceSlug: "sia_a0f80d_zone_10",
				Name: "Event code", RawName: "Code evenement", Metric: "event_code", Component: ComponentSensor,
				Type: "info", Subtype: "string", Value: "M_11_3F", LastUpdate: at, LastValueAt: at,
			},
			"grid_power": {
				Device: "Grid detector", DeviceSlug: "sia_a0f80d_zone_10", Name: "Grid power",
				Metric: "grid_power", Component: ComponentBinarySensor, DeviceClass: "power",
				Type: "info", Subtype: "binary", Value: false, LastUpdate: at, LastValueAt: at,
			},
		},
		Actions: map[string]Action{},
	}

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))
	device, ok := store.Device("sia_a0f80d_zone_10")
	if !ok || device.Values["input_alarm"] != true {
		t.Fatalf("reconciled detector = %#v, want raw input_alarm true", device)
	}
	if _, exists := device.Values["grid_power"]; exists {
		t.Fatalf("grid_power survived reconciliation: %#v", device.Values)
	}
	if _, exists := device.RawCommands["grid_power"]; exists {
		t.Fatalf("synthetic grid power command survived reconciliation: %#v", device.RawCommands)
	}
	if len(device.PendingDiscoveryCleanups) != 1 || commandCleanupKey(device.PendingDiscoveryCleanups[0]) != "derived:binary_sensor:sia_a0f80d_zone_10:grid_power" {
		t.Fatalf("reconciliation cleanup queue = %#v", device.PendingDiscoveryCleanups)
	}
}

func TestStoreDoesNotDeriveInputAlarmForMultiTransmitterVariants(t *testing.T) {
	for index, kind := range []string{"MultiTransmitter", "MultiTransmitterFibra", "SuperiorMultiTransmitterG3"} {
		t.Run(kind, func(t *testing.T) {
			commandID := strconv.Itoa(209 + index)
			catalog := testCatalog(t, devicecatalog.Device{
				Account:          "A0F80D",
				Zone:             strconv.Itoa(15 + index),
				Name:             "Multi input",
				Kind:             kind,
				JeedomCommandIDs: []string{commandID},
			})
			store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

			result := store.Apply(Event{
				Topic:       "jeedom/cmd/event/" + commandID,
				CommandID:   commandID,
				DeviceName:  "Multi input",
				CommandName: "Code evenement",
				Type:        "info",
				Subtype:     "string",
				Value:       json.RawMessage(`"M_11_3F"`),
				ReceivedAt:  time.Unix(100, 0),
			})
			if _, ok := result.Device.Values["input_alarm"]; ok {
				t.Fatalf("%s input_alarm = %#v, want no derived value", kind, result.Device.Values["input_alarm"])
			}
			if _, ok := result.Device.RawCommands["input_alarm"]; ok {
				t.Fatalf("%s synthetic command = %#v, want none", kind, result.Device.RawCommands["input_alarm"])
			}
		})
	}
}

func TestStorePublishRevisionOrdersLegacyDependencies(t *testing.T) {
	store := NewStore("keep_last")
	store.replaceDevices([]Device{
		{Device: "Canonical A", DeviceSlug: "a", LegacyDeviceSlugs: []string{"b"}},
		{Device: "Legacy B", DeviceSlug: "b", LegacyDeviceSlugs: []string{"c"}},
		{Device: "Legacy C", DeviceSlug: "c"},
	})
	a, _ := store.Device("a")
	b, _ := store.Device("b")
	c, _ := store.Device("c")
	if !(c.publishRevision < b.publishRevision && b.publishRevision < a.publishRevision) {
		t.Fatalf("legacy-chain revisions c=%d b=%d a=%d, want c < b < a", c.publishRevision, b.publishRevision, a.publishRevision)
	}
	if !containsString(a.LegacyDeviceSlugs, "b") || !containsString(a.LegacyDeviceSlugs, "c") {
		t.Fatalf("canonical legacy closure = %#v, want b and c", a.LegacyDeviceSlugs)
	}
}

func TestStoreRecordControlAdvancesPublishRevision(t *testing.T) {
	store := NewStore("keep_last")
	store.replaceDevices([]Device{{
		Device:     "Relay",
		DeviceSlug: "relay",
		Actions: map[string]Action{
			"on": {Action: "on", CommandID: "85", Device: "Relay", DeviceSlug: "relay"},
		},
	}})
	before, _ := store.Device("relay")
	action := before.Actions["on"]
	store.RecordControl(action, "http:127.0.0.1", "jeedom/cmd/set/85", nil)
	after, _ := store.Device("relay")
	if after.publishRevision <= before.publishRevision {
		t.Fatalf("publish revision = %d, want greater than %d", after.publishRevision, before.publishRevision)
	}
	if after.Actions["on"].LastRequestedAt.IsZero() {
		t.Fatal("control request metadata was not recorded")
	}
}

func hasCommandCleanupKey(commands []Command, wanted string) bool {
	for _, command := range commands {
		if commandCleanupKey(command) == wanted {
			return true
		}
	}
	return false
}

type transmitterNameResolver struct{}

func (transmitterNameResolver) Resolve(evt Event, _ Mapping) DeviceIdentity {
	slug := Slug(evt.DeviceName)
	return DeviceIdentity{
		DeviceSlug:     slug,
		DeviceName:     evt.DeviceName,
		BaseSlug:       slug,
		HAIdentifiers:  []string{"ajaxbridge_jeedom_" + slug},
		HAManufacturer: "Ajax Systems",
		HAModel:        "Transmitter",
	}
}

type legacyActionResolver struct{}

func (legacyActionResolver) Resolve(evt Event, _ Mapping) DeviceIdentity {
	if evt.CommandID != "216" {
		return DeviceIdentity{}
	}
	return DeviceIdentity{
		DeviceSlug:        "sia_a0f80d_zone_14",
		DeviceName:        "Battery heater",
		BaseSlug:          "sia_a0f80d_zone_14",
		HAIdentifiers:     []string{"ajaxbridge_A0F80D_zone_14"},
		HAManufacturer:    "Ajax Systems",
		HAModel:           "Socket",
		LegacyDeviceSlugs: []string{"battery"},
		LinkedSource:      "sia",
		LinkedAccount:     "A0F80D",
		LinkedZone:        "14",
	}
}

func (legacyActionResolver) ResolveDiscovery(Discovery) DeviceIdentity {
	return DeviceIdentity{DiscoveryDisabled: true}
}
