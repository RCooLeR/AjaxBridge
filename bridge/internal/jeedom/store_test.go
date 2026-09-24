package jeedom

import (
	"encoding/json"
	"math"
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

func TestStoreDerivesTransmitterGridPowerFromEventCode(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "14",
		Name:             "Grid detector",
		Kind:             "Transmitter",
		JeedomCommandIDs: []string{"208"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/208",
		CommandID:   "208",
		DeviceName:  "Grid detector",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_11_40"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if got := result.Device.Values["grid_power"]; got != true {
		t.Fatalf("grid_power after M_11_40 = %#v, want true", got)
	}
	if _, ok := result.Device.Values["state"]; ok {
		t.Fatalf("state after M_11_40 = %#v, want no switch state", result.Device.Values["state"])
	}
	command := result.Device.RawCommands["grid_power"]
	if command.Metric != "grid_power" || command.Component != ComponentBinarySensor || command.DeviceClass != "power" {
		t.Fatalf("synthetic grid power command = %#v", command)
	}
	if command.Value != true {
		t.Fatalf("synthetic grid power command value = %#v, want true", command.Value)
	}
}

func TestStoreDoesNotDeriveMultiTransmitterGridPowerFromTransmitterEventCode(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "15",
		Name:             "Multi input",
		Kind:             "MultiTransmitter",
		JeedomCommandIDs: []string{"209"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/209",
		CommandID:   "209",
		DeviceName:  "Multi input",
		CommandName: "Code evenement",
		Type:        "info",
		Subtype:     "string",
		Value:       json.RawMessage(`"M_11_40"`),
		ReceivedAt:  time.Unix(100, 0),
	})
	if _, ok := result.Device.Values["grid_power"]; ok {
		t.Fatalf("multitransmitter grid_power = %#v, want no derived value", result.Device.Values["grid_power"])
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
