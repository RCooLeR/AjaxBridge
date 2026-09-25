package jeedom

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestServiceInvalidJSONIncrementsParseErrorAndDoesNotPanic(t *testing.T) {
	metrics := &fakeMetrics{}
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#"},
		NewStore("keep_last"),
		nil,
		nil,
		metrics,
		nil,
		zerolog.Nop(),
	)

	service.HandleMessage(t.Context(), "jeedom/cmd/event/56", []byte("{"))

	if metrics.messages != 1 {
		t.Fatalf("messages = %d, want 1", metrics.messages)
	}
	if metrics.parseErrors != 1 {
		t.Fatalf("parseErrors = %d, want 1", metrics.parseErrors)
	}
}

func TestServiceIgnoresNonEventTopicsAfterCapturing(t *testing.T) {
	metrics := &fakeMetrics{}
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/#"},
		NewStore("keep_last"),
		nil,
		nil,
		metrics,
		nil,
		zerolog.Nop(),
	)

	service.HandleMessage(t.Context(), "jeedom/state", []byte("online"))

	if metrics.messages != 1 {
		t.Fatalf("messages = %d, want 1", metrics.messages)
	}
	if metrics.parseErrors != 0 {
		t.Fatalf("parseErrors = %d, want 0", metrics.parseErrors)
	}
}

func TestServiceEmptyValueIncrementsCounter(t *testing.T) {
	metrics := &fakeMetrics{}
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#"},
		NewStore("keep_last"),
		nil,
		nil,
		metrics,
		nil,
		zerolog.Nop(),
	)

	service.HandleMessage(t.Context(), "jeedom/cmd/event/56", []byte(`{"value":"","humanName":"[None][Server][Puissance]","unite":"","name":"Puissance","type":"info","subtype":"numeric"}`))

	if metrics.emptyValues != 1 {
		t.Fatalf("emptyValues = %d, want 1", metrics.emptyValues)
	}
}

func TestServiceNumericValueUpdatesStoreForMetrics(t *testing.T) {
	metrics := &fakeMetrics{}
	store := NewStore("keep_last")
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#"},
		store,
		nil,
		nil,
		metrics,
		nil,
		zerolog.Nop(),
	)

	service.HandleMessage(t.Context(), "jeedom/cmd/event/56", []byte(`{"value":"123.4","humanName":"[None][Server][Puissance]","unite":"W","name":"Puissance","type":"info","subtype":"numeric"}`))

	if metrics.messages != 1 {
		t.Fatalf("messages = %d, want 1", metrics.messages)
	}
	if got := store.Devices()[0].Values["power_w"]; got != float64(123.4) {
		t.Fatalf("snapshot power_w = %v, want 123.4", got)
	}
}

func TestServiceRetainedStateCannotRegressFromOutOfOrderHandlers(t *testing.T) {
	store := NewStore("keep_last")
	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#"},
		store,
		nil,
		publisher,
		nil,
		nil,
		zerolog.Nop(),
	)
	observer := &blockingUpdateObserver{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service.SetObserver(observer)

	doneA := make(chan struct{})
	go func() {
		service.HandleMessage(t.Context(), "jeedom/cmd/event/56", []byte(`{"value":"1","humanName":"[None][Server][Puissance]","unite":"W","name":"Puissance","type":"info","subtype":"numeric"}`))
		close(doneA)
	}()
	select {
	case <-observer.entered:
	case <-time.After(time.Second):
		t.Fatal("older handler did not reach the post-Apply observer")
	}

	service.HandleMessage(t.Context(), "jeedom/cmd/event/56", []byte(`{"value":"2","humanName":"[None][Server][Puissance]","unite":"W","name":"Puissance","type":"info","subtype":"numeric"}`))
	close(observer.release)
	select {
	case <-doneA:
	case <-time.After(time.Second):
		t.Fatal("older handler did not finish")
	}

	device, ok := store.Device("server")
	if !ok || device.Values["power_w"] != float64(2) {
		t.Fatalf("store power_w = %#v, want 2", device.Values["power_w"])
	}
	assertRecordedPower(t, mqtt.state["ajaxbridge/jeedom/devices/server/state"], 2)
}

func TestServiceAcknowledgesCommandCleanupOnlyWhenDiscoveryIsPublished(t *testing.T) {
	for _, discoveryEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[discoveryEnabled], func(t *testing.T) {
			store := NewStore("keep_last")
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
			mqtt := &recordingMQTT{}
			publisher := NewPublisher(PublisherConfig{
				StateTopicPrefix: "ajaxbridge/jeedom",
				Discovery:        discoveryEnabled,
				DiscoveryPrefix:  "homeassistant",
				DiscoveryNode:    "ajaxbridge",
			}, mqtt)
			service := NewService(ServiceConfig{}, store, nil, publisher, nil, nil, zerolog.Nop())

			service.HandleMessage(t.Context(), "jeedom/discovery/eqLogic/20", []byte(`{
			  "id":20,
			  "name":"Remote",
			  "cmds":{
			    "375":{"id":375,"name":"Nombre de défauts","type":"info","subType":"numeric","currentValue":0}
			  }
			}`))

			device, ok := store.Device("remote")
			if !ok {
				t.Fatal("missing Remote after refreshed discovery")
			}
			if discoveryEnabled {
				if len(device.PendingDiscoveryCleanups) != 0 {
					t.Fatalf("published cleanup remains queued: %#v", device.PendingDiscoveryCleanups)
				}
				if len(device.PendingActionDiscoveryCleanups) != 0 {
					t.Fatalf("published action cleanup remains queued: %#v", device.PendingActionDiscoveryCleanups)
				}
				if payload, ok := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_369/config"]; !ok || payload != "" {
					t.Fatalf("cleanup publish = %q present=%v", payload, ok)
				}
				if payload, ok := mqtt.discovery["homeassistant/button/ajaxbridge/jeedom_control_remote_panic/config"]; !ok || payload != "" {
					t.Fatalf("action cleanup publish = %q present=%v", payload, ok)
				}
			} else {
				if len(device.PendingDiscoveryCleanups) != 1 || device.PendingDiscoveryCleanups[0].CommandID != "369" {
					t.Fatalf("disabled discovery lost pending cleanup: %#v", device.PendingDiscoveryCleanups)
				}
				if len(device.PendingActionDiscoveryCleanups) != 1 || device.PendingActionDiscoveryCleanups[0].CommandID != "900" {
					t.Fatalf("disabled discovery lost pending action cleanup: %#v", device.PendingActionDiscoveryCleanups)
				}
			}
		})
	}
}

func TestServiceAcknowledgesPreviouslyQueuedCleanupAfterValueEvent(t *testing.T) {
	store := NewStore("keep_last")
	store.ApplyDiscovery(Discovery{
		EqLogicID: "20",
		Name:      "Remote",
		InfoCommands: map[string]DiscoveryCommand{
			"369": {CommandID: "369", EqLogicID: "20", Name: "Nombre de défauts", Type: "info", Subtype: "numeric", Value: []byte(`1`)},
		},
	})
	store.ApplyDiscovery(Discovery{
		EqLogicID: "20",
		Name:      "Remote",
		InfoCommands: map[string]DiscoveryCommand{
			"375": {CommandID: "375", EqLogicID: "20", Name: "Nombre de défauts", Type: "info", Subtype: "numeric", Value: []byte(`0`)},
		},
	})
	queued, _ := store.Device("remote")
	if len(queued.PendingDiscoveryCleanups) != 1 {
		t.Fatalf("setup pending cleanups = %#v", queued.PendingDiscoveryCleanups)
	}

	mqtt := &recordingMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
	}, mqtt)
	service := NewService(ServiceConfig{}, store, nil, publisher, nil, nil, zerolog.Nop())
	service.HandleMessage(t.Context(), "jeedom/cmd/event/375", []byte(`{
	  "value":0,
	  "humanName":"[House][Remote][Nombre de défauts]",
	  "name":"Nombre de défauts",
	  "type":"info",
	  "subtype":"numeric"
	}`))

	device, _ := store.Device("remote")
	if len(device.PendingDiscoveryCleanups) != 0 {
		t.Fatalf("successful event publish did not acknowledge old cleanup: %#v", device.PendingDiscoveryCleanups)
	}
	if payload, ok := mqtt.discovery["homeassistant/sensor/ajaxbridge/jeedom_cmd_369/config"]; !ok || payload != "" {
		t.Fatalf("retried cleanup = %q present=%v", payload, ok)
	}
}

func TestServiceObservesExternalJeedomSetCommand(t *testing.T) {
	store := NewStore("keep_last")
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	observer := &fakeObserver{}
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#", SetTopicPrefix: "jeedom/cmd/set"},
		store,
		nil,
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)
	service.SetObserver(observer)

	service.HandleMessage(t.Context(), "jeedom/cmd/set/85", nil)

	if observer.controls != 1 {
		t.Fatalf("controls = %d, want 1", observer.controls)
	}
	if observer.last.Action != "on" || observer.last.DeviceSlug != "garage_gate" {
		t.Fatalf("last control = %#v", observer.last)
	}
}

func TestServiceRecordsExternalActionOnlyWallSwitchState(t *testing.T) {
	store := NewStore("keep_last")
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/26", []byte(wallSwitchDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	observer := &fakeObserver{}
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#", SetTopicPrefix: "jeedom/cmd/set"},
		store,
		nil,
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)
	service.SetObserver(observer)

	service.HandleMessage(t.Context(), "jeedom/cmd/set/348", nil)

	device, ok := store.Device("grid_load")
	if !ok || device.Values["state"] != true {
		t.Fatalf("WallSwitch after external ON = %#v, want state=true", device)
	}
	if observer.controls != 1 || !observer.last.StateUpdated {
		t.Fatalf("control observation = %#v, want state update", observer.last)
	}
}

func TestServiceSkipsJeedomSetCommandRecentlyIssuedByBridge(t *testing.T) {
	store := NewStore("keep_last")
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyDiscovery(discovery)
	action, ok := store.ActionByCommandID("85")
	if !ok {
		t.Fatal("missing action")
	}
	store.RecordControl(action, "http:127.0.0.1", "jeedom/cmd/set/85", nil)
	observer := &fakeObserver{}
	service := NewService(
		ServiceConfig{EventTopic: "jeedom/cmd/event/#", SetTopicPrefix: "jeedom/cmd/set"},
		store,
		nil,
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)
	service.SetObserver(observer)

	service.HandleMessage(t.Context(), "jeedom/cmd/set/85", nil)

	if observer.controls != 0 {
		t.Fatalf("controls = %d, want 0", observer.controls)
	}
}

type fakeMetrics struct {
	messages    int
	parseErrors int
	emptyValues int
}

func (m *fakeMetrics) ObserveJeedomMessage() {
	m.messages++
}

func (m *fakeMetrics) ObserveJeedomParseError() {
	m.parseErrors++
}

func (m *fakeMetrics) ObserveJeedomEmptyValue() {
	m.emptyValues++
}

type fakeObserver struct {
	updates  int
	controls int
	last     ControlResult
}

type blockingUpdateObserver struct {
	entered chan struct{}
	release chan struct{}
}

func (o *blockingUpdateObserver) ObserveJeedomUpdate(_ context.Context, result ApplyResult) {
	if result.HasNumeric && result.NumericValue == 1 {
		close(o.entered)
		<-o.release
	}
}

func (*blockingUpdateObserver) ObserveJeedomControl(context.Context, ControlResult, error) {}

func (o *fakeObserver) ObserveJeedomUpdate(context.Context, ApplyResult) {
	o.updates++
}

func (o *fakeObserver) ObserveJeedomControl(_ context.Context, result ControlResult, _ error) {
	o.controls++
	o.last = result
}
