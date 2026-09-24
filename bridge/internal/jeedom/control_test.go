package jeedom

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/rs/zerolog"
)

func TestControllerPublishesJeedomSetCommand(t *testing.T) {
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore("keep_last")
	store.ApplyDiscovery(discovery)
	mqtt := &fakeCommandPublisher{}
	controller := NewController(ControllerConfig{
		Enabled:              true,
		StateTopicPrefix:     "ajaxbridge/jeedom",
		JeedomSetTopicPrefix: "jeedom/cmd/set",
	}, store, mqtt, zerolog.Nop())

	result, err := controller.Execute(context.Background(), "garage_gate", "ON", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.CommandID != "85" {
		t.Fatalf("result = %#v", result)
	}
	if mqtt.topic != "jeedom/cmd/set/85" {
		t.Fatalf("published topic = %q, want jeedom/cmd/set/85", mqtt.topic)
	}
	if mqtt.payload != "1" {
		t.Fatalf("published payload = %q, want 1", mqtt.payload)
	}
	audits := store.ControlAudit(1)
	if len(audits) != 1 || audits[0].Result != "published" {
		t.Fatalf("audit = %#v", audits)
	}
}

func TestControllerUsesConfiguredJeedomCommandPayload(t *testing.T) {
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore("keep_last")
	store.ApplyDiscovery(discovery)
	mqtt := &fakeCommandPublisher{}
	controller := NewController(ControllerConfig{
		Enabled:              true,
		StateTopicPrefix:     "ajaxbridge/jeedom",
		JeedomSetTopicPrefix: "jeedom/cmd/set",
		CommandPayload:       "go",
	}, store, mqtt, zerolog.Nop())

	if _, err := controller.Execute(context.Background(), "garage_gate", "ON", "test"); err != nil {
		t.Fatal(err)
	}
	if mqtt.payload != "go" {
		t.Fatalf("published payload = %q, want go", mqtt.payload)
	}
}

func TestControllerPublishesRelayImpulseCommand(t *testing.T) {
	payload := []byte(`{"id":11,"name":"Garage pulse","configuration":{"device":"Relay","applyDevice":"Relay"},"isVisible":1,"isEnable":1,"cmds":{"90":{"id":90,"logicalId":"IMPULSE","name":"Impulsion","type":"action","subType":"other","isVisible":1}}}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/11", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore("keep_last")
	store.ApplyDiscovery(discovery)
	mqtt := &fakeCommandPublisher{}
	controller := NewController(ControllerConfig{
		Enabled:              true,
		StateTopicPrefix:     "ajaxbridge/jeedom",
		JeedomSetTopicPrefix: "jeedom/cmd/set",
	}, store, mqtt, zerolog.Nop())

	result, err := controller.Execute(context.Background(), "garage_pulse", "IMPULSE", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.CommandID != "90" {
		t.Fatalf("result = %#v", result)
	}
	if mqtt.topic != "jeedom/cmd/set/90" {
		t.Fatalf("published topic = %q, want jeedom/cmd/set/90", mqtt.topic)
	}
	if mqtt.payload != "1" {
		t.Fatalf("published payload = %q, want 1", mqtt.payload)
	}
}

func TestControllerPublishesWaterStopCommand(t *testing.T) {
	payload := []byte(`{"id":3,"name":"Valve","configuration":{"device":"WaterStop"},"isVisible":1,"isEnable":1,"cmds":{"26":{"id":26,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","isVisible":1}}}`)
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/3", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore("keep_last")
	store.ApplyDiscovery(discovery)
	mqtt := &fakeCommandPublisher{}
	controller := NewController(ControllerConfig{Enabled: true}, store, mqtt, zerolog.Nop())

	result, err := controller.Execute(context.Background(), "valve", "on", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.CommandID != "26" {
		t.Fatalf("result = %#v", result)
	}
	if mqtt.topic != "jeedom/cmd/set/26" {
		t.Fatalf("published topic = %q, want jeedom/cmd/set/26", mqtt.topic)
	}
}

func TestControllerPersistsActionOnlyWallSwitchStateAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom.json")
	store, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/26", []byte(wallSwitchDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	device := store.ApplyDiscovery(discovery).Device
	if _, exists := device.Values["state"]; exists {
		t.Fatalf("fresh WallSwitch state = %#v, want unknown", device.Values["state"])
	}
	mqtt := &fakeCommandPublisher{}
	controller := NewController(ControllerConfig{
		Enabled:              true,
		StateTopicPrefix:     "ajaxbridge/jeedom",
		JeedomSetTopicPrefix: "jeedom/cmd/set",
	}, store, mqtt, zerolog.Nop())

	result, err := controller.Execute(t.Context(), "grid_load", "ON", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.StateUpdated {
		t.Fatalf("result = %#v, want persisted optimistic state", result)
	}
	restarted, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The application always reconciles the loaded cache against a non-nil
	// catalog resolver before publishing restored state.
	restarted.ReconcileResolver(NewCatalogResolver(devicecatalog.Empty(), CatalogResolverConfig{}))
	restored, ok := restarted.Device("grid_load")
	if !ok || restored.Values["state"] != true {
		t.Fatalf("restored WallSwitch = %#v, want state=true", restored)
	}

	restartedController := NewController(ControllerConfig{Enabled: true}, restarted, mqtt, zerolog.Nop())
	if _, err := restartedController.Execute(t.Context(), "grid_load", "OFF", "test"); err != nil {
		t.Fatal(err)
	}
	restartedAgain, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok = restartedAgain.Device("grid_load")
	if !ok || restored.Values["state"] != false {
		t.Fatalf("restored WallSwitch = %#v, want state=false", restored)
	}
}

func TestControllerRestoresNewerOptimisticStateOverOlderLoadSample(t *testing.T) {
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
	loadResult := store.Apply(Event{
		CommandID:   "346",
		DeviceName:  "Grid load",
		CommandName: "Puissance",
		LogicalID:   "powerWTh",
		Type:        "info",
		Subtype:     "numeric",
		Unit:        "W",
		Value:       json.RawMessage(`10`),
		ReceivedAt:  time.Unix(200, 0),
	})
	if loadResult.Device.Values["state"] != true {
		t.Fatalf("load-derived state = %#v, want true", loadResult.Device.Values["state"])
	}

	controller := NewController(ControllerConfig{Enabled: true}, store, &fakeCommandPublisher{}, zerolog.Nop())
	if _, err := controller.Execute(t.Context(), "grid_load", "OFF", "test"); err != nil {
		t.Fatal(err)
	}
	restarted, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := restarted.Device("grid_load")
	if !ok || loaded.Values["state"] != false {
		t.Fatalf("loaded WallSwitch = %#v, want persisted optimistic state=false before reconciliation", loaded)
	}
	restarted.ReconcileResolver(NewCatalogResolver(devicecatalog.Empty(), CatalogResolverConfig{}))
	restored, ok := restarted.Device("grid_load")
	if !ok || restored.Values["state"] != false {
		t.Fatalf("restored WallSwitch = %#v, want newer optimistic state=false", restored)
	}
	discovery.ReceivedAt = time.Unix(300, 0)
	refreshed := restarted.ApplyDiscovery(discovery).Device
	if refreshed.Values["state"] != false {
		t.Fatalf("state after retained discovery = %#v, want newer optimistic state=false", refreshed.Values["state"])
	}
}

func TestControllerDoesNotUpdateWallSwitchStateWhenCommandPublishFails(t *testing.T) {
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/26", []byte(wallSwitchDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore("keep_last")
	store.ApplyDiscovery(discovery)
	controller := NewController(ControllerConfig{Enabled: true}, store, &fakeCommandPublisher{err: errors.New("publish failed")}, zerolog.Nop())

	result, err := controller.Execute(t.Context(), "grid_load", "ON", "test")
	if err == nil {
		t.Fatal("expected publish error")
	}
	if result.StateUpdated {
		t.Fatalf("result = %#v, state must not update after failed command", result)
	}
	device, ok := store.Device("grid_load")
	if !ok {
		t.Fatal("missing WallSwitch")
	}
	if _, exists := device.Values["state"]; exists {
		t.Fatalf("state = %#v, want unknown after failed command", device.Values["state"])
	}
}

type fakeCommandPublisher struct {
	topic   string
	payload string
	err     error
}

func (f *fakeCommandPublisher) PublishCommandMessage(_ context.Context, topic string, payload []byte) error {
	f.topic = topic
	f.payload = string(payload)
	return f.err
}
