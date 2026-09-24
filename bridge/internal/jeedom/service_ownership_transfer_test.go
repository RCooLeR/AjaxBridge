package jeedom

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/rs/zerolog"
)

func TestServicePublishesPreviousCommandOwnerBeforeNewOwnerWithoutDiscoveryTombstone(t *testing.T) {
	tests := []struct {
		name  string
		store *Store
		act   func(*testing.T, *Service)
	}{
		{
			name:  "event",
			store: NewStoreWithResolver("keep_last", eventOwnershipTransferResolver{}),
			act: func(t *testing.T, service *Service) {
				t.Helper()
				service.HandleMessage(t.Context(), "jeedom/cmd/event/369", []byte(`{
				  "value":22,
				  "humanName":"[House][New Owner][Temperature]",
				  "name":"Temperature",
				  "type":"info",
				  "subtype":"numeric",
				  "unite":"°C"
				}`))
			},
		},
		{
			name:  "discovery",
			store: NewStore("keep_last"),
			act: func(t *testing.T, service *Service) {
				t.Helper()
				service.HandleMessage(t.Context(), "jeedom/discovery/eqLogic/2", []byte(`{
				  "id":2,
				  "name":"New Owner",
				  "cmds":{
				    "369":{
				      "id":369,
				      "name":"Temperature",
				      "type":"info",
				      "subType":"numeric",
				      "unite":"°C",
				      "currentValue":22
				    }
				  }
				}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedCommandOwner(tt.store)

			mqtt := &ownershipTransferMQTT{}
			publisher := NewPublisher(PublisherConfig{
				StateTopicPrefix: "ajaxbridge/jeedom",
				Discovery:        true,
				DiscoveryPrefix:  "homeassistant",
				DiscoveryNode:    "ajaxbridge",
			}, mqtt)
			service := NewService(ServiceConfig{}, tt.store, nil, publisher, nil, nil, zerolog.Nop())

			tt.act(t, service)

			assertOwnerStatePublishOrder(t, mqtt.statePublishes, "old_owner", "new_owner")
			assertNoRemovedCommandTombstone(t, mqtt.discoveryPublishes, "369")

			oldOwner, ok := tt.store.Device("old_owner")
			if !ok {
				t.Fatal("old command owner disappeared after transfer")
			}
			if _, exists := oldOwner.RawCommands["369"]; exists {
				t.Fatal("old command owner still contains command 369")
			}
			if len(oldOwner.PendingDiscoveryCleanups) != 0 {
				t.Fatalf("ownership transfer queued discovery cleanup: %#v", oldOwner.PendingDiscoveryCleanups)
			}
			newOwner, ok := tt.store.Device("new_owner")
			if !ok {
				t.Fatal("new command owner was not created")
			}
			if _, exists := newOwner.RawCommands["369"]; !exists {
				t.Fatal("new command owner does not contain command 369")
			}
		})
	}
}

func TestServicePersistsCancelledTargetCleanupOnEmptyEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jeedom-cache.json")
	store := NewStoreWithResolver("keep_last", eventOwnershipTransferResolver{})
	store.SetPath(path)
	store.devices["new_owner"] = &Device{
		Source:      Source,
		Device:      "New Owner",
		DeviceSlug:  "new_owner",
		Values:      make(map[string]any),
		RawCommands: make(map[string]Command),
		Actions:     make(map[string]Action),
		PendingDiscoveryCleanups: []Command{
			{CommandID: "369", Metric: "temperature_c", Component: ComponentSensor},
		},
	}
	store.rebuildIndexesLocked()
	service := NewService(ServiceConfig{}, store, nil, nil, nil, nil, zerolog.Nop())

	service.HandleMessage(t.Context(), "jeedom/cmd/event/369", []byte(`{
	  "value":"",
	  "humanName":"[House][New Owner][Temperature]",
	  "name":"Temperature",
	  "type":"info",
	  "subtype":"numeric",
	  "unite":"°C"
	}`))

	loaded, err := LoadStore(t.Context(), path, "keep_last", eventOwnershipTransferResolver{})
	if err != nil {
		t.Fatal(err)
	}
	device, ok := loaded.Device("new_owner")
	if !ok {
		t.Fatal("persisted cache is missing target device")
	}
	if len(device.PendingDiscoveryCleanups) != 0 {
		t.Fatalf("cancelled cleanup survived in persisted cache: %#v", device.PendingDiscoveryCleanups)
	}
}

func TestServicePublishesMixedDiscoverySourceBeforeResolvedDestination(t *testing.T) {
	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"369", "370"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276", "374", "375", "376"},
		},
	)
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	mqtt := &ownershipTransferMQTT{}
	publisher := NewPublisher(PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		Discovery:        true,
		DiscoveryPrefix:  "homeassistant",
		DiscoveryNode:    "ajaxbridge",
	}, mqtt)
	service := NewService(ServiceConfig{}, store, nil, publisher, nil, nil, zerolog.Nop())

	service.HandleMessage(t.Context(), "jeedom/discovery/eqLogic/35", []byte(`{
	  "id":35,
	  "name":"Діана",
	  "configuration":{"device":"SpaceControl"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "276":{"id":276,"logicalId":"eventCode","name":"Code evenement","type":"info","subType":"string","currentValue":"M_11_40"},
	    "369":{"id":369,"logicalId":"issuesCount","name":"Nombre de defauts","type":"info","subType":"numeric","currentValue":2},
	    "370":{"id":370,"logicalId":"firmwareVersion","name":"Version du firmware","type":"info","subType":"string","currentValue":"1.0"},
	    "374":{"id":374,"logicalId":"batteryChargeLevelPercentage","name":"Batterie","type":"info","subType":"numeric","unite":"%","currentValue":95},
	    "375":{"id":375,"logicalId":"issuesCount","name":"Nombre de defauts","type":"info","subType":"numeric","currentValue":0},
	    "376":{"id":376,"logicalId":"firmwareVersion","name":"Version du firmware","type":"info","subType":"string","currentValue":"1.1"}
	  }
	}`))

	assertOwnerStatePublishOrder(t, mqtt.statePublishes, "sia_a0f80d_zone_20", "sia_a0f80d_zone_2")
	assertNoRemovedCommandTombstone(t, mqtt.discoveryPublishes, "369")
	assertUniqueRawCommandOwners(t, store.Devices(), map[string]string{
		"276": "sia_a0f80d_zone_20",
		"369": "sia_a0f80d_zone_2",
		"370": "sia_a0f80d_zone_2",
		"374": "sia_a0f80d_zone_20",
		"375": "sia_a0f80d_zone_20",
		"376": "sia_a0f80d_zone_20",
	})
}

func seedCommandOwner(store *Store) {
	store.ApplyDiscovery(Discovery{
		EqLogicID: "20",
		Name:      "Old Owner",
		InfoCommands: map[string]DiscoveryCommand{
			"369": {
				CommandID: "369",
				EqLogicID: "20",
				Name:      "Temperature",
				Type:      "info",
				Subtype:   "numeric",
				Unit:      "°C",
				Value:     []byte(`21`),
			},
		},
	})
}

type eventOwnershipTransferResolver struct{}

func (eventOwnershipTransferResolver) Resolve(evt Event, _ Mapping) DeviceIdentity {
	if evt.DeviceName != "New Owner" {
		return DeviceIdentity{}
	}
	return DeviceIdentity{
		DeviceSlug:    "new_owner",
		DeviceName:    "New Owner",
		BaseSlug:      "new_owner",
		HAIdentifiers: []string{"ajaxbridge_jeedom_new_owner"},
	}
}

type ownershipStatePublish struct {
	topic   string
	payload string
}

type ownershipDiscoveryPublish struct {
	key     string
	topic   string
	payload string
}

type ownershipTransferMQTT struct {
	statePublishes     []ownershipStatePublish
	discoveryPublishes []ownershipDiscoveryPublish
}

func (m *ownershipTransferMQTT) PublishStateMessage(_ context.Context, topic string, payload []byte, _ bool) error {
	m.statePublishes = append(m.statePublishes, ownershipStatePublish{topic: topic, payload: string(payload)})
	return nil
}

func (m *ownershipTransferMQTT) PublishDiscoveryMessage(_ context.Context, key, topic string, payload []byte, _ bool) error {
	m.discoveryPublishes = append(m.discoveryPublishes, ownershipDiscoveryPublish{
		key:     key,
		topic:   topic,
		payload: string(payload),
	})
	return nil
}

func (*ownershipTransferMQTT) AvailabilityTopic() string {
	return "ajaxbridge/status"
}

func assertOwnerStatePublishOrder(t *testing.T, publishes []ownershipStatePublish, oldSlug, newSlug string) {
	t.Helper()
	oldTopic := "ajaxbridge/jeedom/devices/" + oldSlug + "/state"
	newTopic := "ajaxbridge/jeedom/devices/" + newSlug + "/state"
	oldIndex, newIndex := -1, -1
	for index, publish := range publishes {
		if publish.payload == "" {
			continue
		}
		switch publish.topic {
		case oldTopic:
			if oldIndex == -1 {
				oldIndex = index
			}
		case newTopic:
			if newIndex == -1 {
				newIndex = index
			}
		}
	}
	if oldIndex == -1 || newIndex == -1 {
		t.Fatalf("state publishes = %#v, want non-empty state for %s and %s", publishes, oldTopic, newTopic)
	}
	if oldIndex >= newIndex {
		t.Fatalf("state publish order = %#v, want old owner before new owner", publishes)
	}
}

func assertNoRemovedCommandTombstone(t *testing.T, publishes []ownershipDiscoveryPublish, commandID string) {
	t.Helper()
	prefix := "jeedom_removed_command_cleanup:"
	stableTopic := "homeassistant/sensor/ajaxbridge/jeedom_cmd_" + commandID + "/config"
	stableConfigPublished := false
	for _, publish := range publishes {
		if strings.HasPrefix(publish.key, prefix) {
			t.Fatalf("ownership transfer emitted removed-command cleanup: %#v", publish)
		}
		if publish.topic == stableTopic {
			if publish.payload == "" {
				t.Fatalf("ownership transfer tombstoned stable command discovery: %#v", publish)
			}
			stableConfigPublished = true
		}
	}
	if !stableConfigPublished {
		t.Fatalf("stable discovery config for command %s was not published: %#v", commandID, publishes)
	}
}
