package jeedom

import (
	"math"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
)

const relayDiscoveryPayload = `{
  "id": 10,
  "name": "Garage gate",
  "logicalId": "3092B925",
  "eqType_name": "ajaxSystem",
  "isVisible": 1,
  "isEnable": 1,
  "configuration": {"device":"Relay","applyDevice":"Relay","hub_id":"002BAD2F"},
  "cmds": {
    "81": {"id":81,"logicalId":"realState","name":"Etat","type":"info","subType":"binary","unite":"","eqLogic_id":10,"isVisible":1,"value":null},
    "85": {"id":85,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","eqLogic_id":10,"isVisible":1,"value":"81"},
    "86": {"id":86,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","eqLogic_id":10,"isVisible":1,"value":"81"}
  }
}`

const wallSwitchDiscoveryPayload = `{
  "id": 26,
  "name": "Grid load",
  "logicalId": "WALLSWITCH26",
  "eqType_name": "ajaxSystem",
  "isVisible": 1,
  "isEnable": 1,
  "configuration": {"device":"WallSwitch","applyDevice":"WallSwitch"},
  "cmds": {
    "342": {"id":342,"logicalId":"sourceObjectName","name":"Source evenement","type":"info","subType":"other","eqLogic_id":26,"isVisible":1},
    "343": {"id":343,"logicalId":"event","name":"Evenement","type":"info","subType":"other","eqLogic_id":26,"isVisible":1},
    "344": {"id":344,"logicalId":"eventCode","name":"Code evenement","type":"info","subType":"other","eqLogic_id":26,"isVisible":1},
    "345": {"id":345,"logicalId":"voltage","name":"Voltage","type":"info","subType":"numeric","eqLogic_id":26,"isVisible":1},
    "346": {"id":346,"logicalId":"powerWTh","name":"Puissance","type":"info","subType":"numeric","eqLogic_id":26,"isVisible":1},
    "347": {"id":347,"logicalId":"currentMA","name":"Courant","type":"info","subType":"numeric","eqLogic_id":26,"isVisible":1},
    "348": {"id":348,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","eqLogic_id":26,"isVisible":1},
    "349": {"id":349,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","eqLogic_id":26,"isVisible":1}
  }
}`

func TestParseDiscoveryMessageExtractsActions(t *testing.T) {
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if discovery.EqLogicID != "10" {
		t.Fatalf("EqLogicID = %q, want 10", discovery.EqLogicID)
	}
	if discovery.DeviceType != "Relay" {
		t.Fatalf("DeviceType = %q, want Relay", discovery.DeviceType)
	}
	if discovery.InfoCommands["81"].Subtype != "binary" {
		t.Fatalf("state command = %#v", discovery.InfoCommands["81"])
	}
	if discovery.Actions["85"].StateCommandID != "81" {
		t.Fatalf("on state command = %q, want 81", discovery.Actions["85"].StateCommandID)
	}
}

func TestStoreApplyDiscoveryRegistersSafeActions(t *testing.T) {
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore("keep_last")
	result := store.ApplyDiscovery(discovery)

	if result.Device.DeviceSlug != "garage_gate" {
		t.Fatalf("DeviceSlug = %q, want garage_gate", result.Device.DeviceSlug)
	}
	state := result.Device.RawCommands["81"]
	if state.Metric != "state" || state.Component != ComponentBinarySensor {
		t.Fatalf("state command mapping = %#v", state)
	}
	on := result.Device.Actions["on"]
	if on.CommandID != "85" || !on.Allowed || on.StateCommandID != "81" {
		t.Fatalf("on action = %#v", on)
	}
	if on.Name != "On" || on.RawName != "On" {
		t.Fatalf("translated action name = %#v", on)
	}
}

func TestStoreApplyDiscoveryRegistersRelayImpulseAction(t *testing.T) {
	payload := []byte(`{
	  "id":11,
	  "name":"Garage pulse",
	  "configuration":{"device":"Relay","applyDevice":"Relay"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "90":{"id":90,"logicalId":"IMPULSE","name":"Impulsion","type":"action","subType":"other","isVisible":1}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/11", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)
	impulse := result.Device.Actions["impulse"]
	if impulse.CommandID != "90" || !impulse.Allowed {
		t.Fatalf("impulse action = %#v", impulse)
	}
	if impulse.Name != "Impulse" {
		t.Fatalf("impulse name = %q, want Impulse", impulse.Name)
	}
}

func TestStoreApplyDiscoverySeedsCurrentInfoValues(t *testing.T) {
	payload := []byte(`{
	  "id":8,
	  "name":"Server power",
	  "configuration":{"device":"WallSwitch"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "56":{"id":56,"name":"Puissance","type":"info","subType":"numeric","unite":"W","isVisible":1,"currentValue":"123,4"},
	    "57":{"id":57,"name":"Temp\u00e9rature","type":"info","subType":"numeric","unite":"\u00b0C","isVisible":1,"value":18.6}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/8", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)

	if got := result.Device.Values["power_w"]; got != 123.4 {
		t.Fatalf("power_w = %#v, want 123.4", got)
	}
	if got := result.Device.Values["temperature_c"]; got != 18.6 {
		t.Fatalf("temperature_c = %#v, want 18.6", got)
	}
	if result.Device.RawCommands["56"].LastValueAt.IsZero() {
		t.Fatalf("power command LastValueAt was not seeded")
	}
}

func TestStoreApplyDiscoveryDoesNotSeedEmptyMeasurements(t *testing.T) {
	payload := []byte(`{
	  "id":9,
	  "name":"Fence power",
	  "configuration":{"device":"WallSwitch"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "180":{"id":180,"name":"Puissance","type":"info","subType":"numeric","unite":"W","isVisible":1,"currentValue":""},
	    "181":{"id":181,"name":"Consommation","type":"info","subType":"numeric","unite":"kWh","isVisible":1,"currentValue":null}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/9", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)

	for _, tc := range []struct {
		commandID string
		metric    string
	}{
		{commandID: "180", metric: "power_w"},
		{commandID: "181", metric: "energy_kwh"},
	} {
		command, ok := result.Device.RawCommands[tc.commandID]
		if !ok {
			t.Fatalf("command %s was not registered", tc.commandID)
		}
		if !command.LastValueAt.IsZero() {
			t.Fatalf("command %s LastValueAt = %s, want zero", tc.commandID, command.LastValueAt)
		}
		if _, ok := result.Device.Values[tc.metric]; ok {
			t.Fatalf("empty discovery synthesized %s: %#v", tc.metric, result.Device.Values)
		}
	}
}

func TestStoreApplyDiscoverySeedsWallSwitchOnFromPositiveLoad(t *testing.T) {
	payload := []byte(`{
	  "id":26,
	  "name":"Grid load",
	  "configuration":{"device":"WallSwitch"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "346":{"id":346,"logicalId":"powerWTh","name":"Puissance","type":"info","subType":"numeric","unite":"W","currentValue":15329},
	    "347":{"id":347,"logicalId":"currentMA","name":"Courant","type":"info","subType":"numeric","unite":"A","currentValue":1940},
	    "348":{"id":348,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other"},
	    "349":{"id":349,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other"}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/26", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)

	if got := result.Device.Values["state"]; got != true {
		t.Fatalf("state = %#v, want true from positive WallSwitch load", got)
	}
	if result.Device.Actions["on"].StateCommandID != "" {
		t.Fatalf("WallSwitch unexpectedly acquired a state command: %#v", result.Device.Actions["on"])
	}
}

func TestStoreApplyDiscoveryNormalizesRelayVoltageSeed(t *testing.T) {
	payload := []byte(`{
	  "id":6,
	  "name":"Garage gate",
	  "configuration":{"device":"Relay"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "230":{"id":230,"name":"Voltage","type":"info","subType":"numeric","unite":"V","isVisible":1,"currentValue":"289.02"}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/6", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)

	got, ok := result.Device.Values["voltage_v"].(float64)
	if !ok {
		t.Fatalf("voltage_v = %#v, want float64", result.Device.Values["voltage_v"])
	}
	if math.Abs(got-28.902) > 1e-9 {
		t.Fatalf("voltage_v = %#v, want 28.902", got)
	}
}

func TestStoreApplyDiscoveryAllowsWaterStopToggleActions(t *testing.T) {
	payload := []byte(`{
	  "id":3,
	  "name":"Valve",
	  "configuration":{"device":"WaterStop"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "26":{"id":26,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","isVisible":1},
	    "27":{"id":27,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","isVisible":1}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/3", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)
	on := result.Device.Actions["on"]
	off := result.Device.Actions["off"]
	if !on.Allowed || !off.Allowed {
		t.Fatalf("WaterStop actions should be allowed: on=%#v off=%#v", on, off)
	}
}

func TestStoreApplyDiscoveryAllowsCatalogWaterStopToggleWithoutJeedomDeviceType(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "12",
		Name:             "Water valve",
		Kind:             "WaterStop",
		JeedomCommandIDs: []string{"174", "175"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	payload := []byte(`{
	  "id":30,
	  "name":"Water valve",
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "174":{"id":174,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","isVisible":1},
	    "175":{"id":175,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","isVisible":1}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/30", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := store.ApplyDiscovery(discovery)

	if result.Device.JeedomDeviceType != "WaterStop" {
		t.Fatalf("JeedomDeviceType = %q, want WaterStop", result.Device.JeedomDeviceType)
	}
	on := result.Device.Actions["on"]
	off := result.Device.Actions["off"]
	if on.CommandID != "174" || !on.Allowed {
		t.Fatalf("catalog WaterStop on action = %#v", on)
	}
	if off.CommandID != "175" || !off.Allowed {
		t.Fatalf("catalog WaterStop off action = %#v", off)
	}
}

func TestStoreApplyDiscoveryAllowsHubSecurityActions(t *testing.T) {
	payload := []byte(`{
	  "id":40,
	  "name":"Security hub",
	  "configuration":{"device":"Hub"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "163":{"id":163,"logicalId":"ARM","name":"Armement","type":"action","subType":"other","isVisible":1},
	    "164":{"id":164,"logicalId":"NIGHT_MODE","name":"Mode nuit","type":"action","subType":"other","isVisible":1},
	    "165":{"id":165,"logicalId":"DISARM","name":"Desarmement","type":"action","subType":"other","isVisible":1},
	    "167":{"id":167,"logicalId":"muteFireDetectors","name":"Arret detection incendie","type":"action","subType":"other","isVisible":1}
	  }
	}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/40", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	result := NewStore("keep_last").ApplyDiscovery(discovery)

	want := map[string]string{
		"arm":                 "163",
		"night_mode":          "164",
		"disarm":              "165",
		"mute_fire_detectors": "167",
	}
	for actionName, commandID := range want {
		action := result.Device.Actions[actionName]
		if action.CommandID != commandID || !action.Allowed {
			t.Fatalf("%s action = %#v, want command %s allowed", actionName, action, commandID)
		}
	}
}

func TestStoreApplyDiscoveryReconcilesOnlyCommandsOwnedByEqLogic(t *testing.T) {
	store := NewStore("keep_last")
	initial := Discovery{
		EqLogicID:  "20",
		Name:       "Remote",
		ObjectName: "House",
		DeviceType: "SpaceControl",
		InfoCommands: map[string]DiscoveryCommand{
			"369": {CommandID: "369", EqLogicID: "20", Name: "Nombre de defauts", Type: "info", Subtype: "numeric", Value: []byte(`2`)},
			"370": {CommandID: "370", EqLogicID: "20", Name: "Version du firmware", Type: "info", Subtype: "string", Value: []byte(`"1.0"`)},
		},
		Actions: map[string]DiscoveryCommand{
			"900": {CommandID: "900", EqLogicID: "20", LogicalID: "ARM", Name: "Arm", Type: "action"},
		},
		ReceivedAt: time.Unix(100, 0),
	}
	first := store.ApplyDiscovery(initial)
	device := store.devices[first.Device.DeviceSlug]
	legacyIssue := device.RawCommands["369"]
	legacyIssue.EqLogicID = "" // simulate a cache written before owner provenance existed
	device.RawCommands["369"] = legacyIssue
	device.RawCommands["777"] = Command{
		CommandID:  "777",
		EqLogicID:  "21",
		ObjectName: "House",
		RawName:    "Temperature",
		Metric:     "temperature_c",
		Component:  ComponentSensor,
	}
	device.RawCommands["synthetic"] = Command{
		RawName:   "Synthetic state",
		Metric:    "state",
		Component: ComponentBinarySensor,
	}
	device.Actions["other"] = Action{Action: "other", CommandID: "901", EqLogicID: "21"}
	store.commands["777"] = device.DeviceSlug

	refreshed := initial
	refreshed.InfoCommands = map[string]DiscoveryCommand{
		"374": {CommandID: "374", EqLogicID: "20", Name: "Batterie", Type: "info", Subtype: "numeric", Unit: "%", Historized: true, Value: []byte(`95`)},
		"375": {CommandID: "375", EqLogicID: "20", Name: "Nombre de defauts", Type: "info", Subtype: "numeric", Value: []byte(`0`)},
		"376": {CommandID: "376", EqLogicID: "20", Name: "Version du firmware", Type: "info", Subtype: "string", Value: []byte(`"1.1"`)},
	}
	refreshed.Actions = map[string]DiscoveryCommand{}
	refreshed.ReceivedAt = time.Unix(200, 0)
	result := store.ApplyDiscovery(refreshed)

	if _, ok := result.Device.RawCommands["369"]; ok {
		t.Fatal("legacy command replaced under a new id was not removed")
	}
	if _, ok := result.Device.RawCommands["370"]; ok {
		t.Fatal("owned stale command was not removed")
	}
	if _, ok := result.Device.RawCommands["777"]; !ok {
		t.Fatal("command owned by another eqLogic was removed")
	}
	if _, ok := result.Device.RawCommands["synthetic"]; !ok {
		t.Fatal("synthetic command was removed")
	}
	if got := result.Device.RawCommands["374"]; got.EqLogicID != "20" || !got.Historized {
		t.Fatalf("new command provenance = %#v", got)
	}
	if len(result.RemovedCommands) != 2 || result.RemovedCommands[0].CommandID != "369" || result.RemovedCommands[1].CommandID != "370" {
		t.Fatalf("RemovedCommands = %#v, want 369 and 370", result.RemovedCommands)
	}
	if got := len(result.Device.PendingDiscoveryCleanups); got != 2 {
		t.Fatalf("pending cleanups = %d, want 2", got)
	}
	if _, ok := result.Device.Actions["arm"]; ok {
		t.Fatal("stale action owned by refreshed eqLogic was not removed")
	}
	if _, ok := result.Device.Actions["other"]; !ok {
		t.Fatal("action owned by another eqLogic was removed")
	}
	if len(result.RemovedActions) != 1 || result.RemovedActions[0].CommandID != "900" {
		t.Fatalf("RemovedActions = %#v, want 900", result.RemovedActions)
	}
	if len(result.Device.PendingActionDiscoveryCleanups) != 1 || result.Device.PendingActionDiscoveryCleanups[0].CommandID != "900" {
		t.Fatalf("pending action cleanups = %#v, want 900", result.Device.PendingActionDiscoveryCleanups)
	}
	if !store.AcknowledgeCommandCleanups(result.RemovedCommands) {
		t.Fatal("cleanup acknowledgement did not update the store")
	}
	if !store.AcknowledgeActionCleanups(result.RemovedActions) {
		t.Fatal("action cleanup acknowledgement did not update the store")
	}
	acknowledged, _ := store.Device(result.Device.DeviceSlug)
	if len(acknowledged.PendingDiscoveryCleanups) != 0 {
		t.Fatalf("acknowledged cleanups still queued: %#v", acknowledged.PendingDiscoveryCleanups)
	}
	if len(acknowledged.PendingActionDiscoveryCleanups) != 0 {
		t.Fatalf("acknowledged action cleanups still queued: %#v", acknowledged.PendingActionDiscoveryCleanups)
	}
}
