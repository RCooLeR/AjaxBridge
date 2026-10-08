package jeedom

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/rs/zerolog"
)

func TestJeedomDiscoveryIsolatesRepeatedLocalIDs(t *testing.T) {
	device := Device{Device: "Kitchen", DeviceSlug: "sia_0001_zone_2", JeedomDeviceType: "Socket", HAIdentifiers: []string{"ajaxbridge_0001_zone_2"}}
	command := Command{CommandID: "56", DeviceSlug: device.DeviceSlug, Metric: "temperature_c", Component: ComponentSensor}
	synthetic := Command{DeviceSlug: device.DeviceSlug, Metric: "input_alarm", Component: ComponentBinarySensor}
	action := Action{CommandID: "57", DeviceSlug: device.DeviceSlug, Action: "on"}
	seenTopics := make(map[string]bool)
	seenIDs := make(map[string]bool)
	seenDevices := make(map[string]bool)
	for _, sourceID := range []string{"", "home-a", "home_a", "ajaxbridge", "home_ajaxbridge_lab"} {
		publisher := NewPublisher(PublisherConfig{SourceID: sourceID, DiscoveryNode: "ajaxbridge"}, fakeMQTT{})
		builders := []func() (string, []byte, error){
			func() (string, []byte, error) { return publisher.BuildDiscovery(command, device) },
			func() (string, []byte, error) { return publisher.BuildDiscovery(synthetic, device) },
			func() (string, []byte, error) { return publisher.BuildSwitchDiscovery(action, device) },
			func() (string, []byte, error) { return publisher.BuildButtonDiscovery(action, device) },
		}
		var deviceID string
		for _, build := range builders {
			topic, body, err := build()
			if err != nil {
				t.Fatal(err)
			}
			var config DiscoveryConfig
			if err := json.Unmarshal(body, &config); err != nil {
				t.Fatal(err)
			}
			if seenTopics[topic] || seenIDs[config.UniqueID] {
				t.Fatalf("source %q discovery collides: %s / %s", sourceID, topic, config.UniqueID)
			}
			seenTopics[topic] = true
			seenIDs[config.UniqueID] = true
			deviceID = config.Device.Identifiers[0]
			if sourceID != "" && (!strings.HasPrefix(config.UniqueID, sourceID+":") || !strings.Contains(topic, "/"+sourceID+"_")) {
				t.Fatalf("source was normalized or omitted: %s / %s", topic, config.UniqueID)
			}
		}
		if seenDevices[deviceID] {
			t.Fatalf("source %q shares device %s", sourceID, deviceID)
		}
		seenDevices[deviceID] = true
	}
	if command.CommandID != "56" || action.CommandID != "57" {
		t.Fatal("source namespacing changed real Jeedom command IDs")
	}
}

func TestSourceCatalogIdentityMatchesSIADiscoveryAndIsScopedOnce(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{Account: "0001", Zone: "2", Name: "Kitchen", JeedomCommandIDs: []string{"56"}})
	resolver := NewCatalogResolver(catalog, CatalogResolverConfig{SourceID: "apartment", Account: "0001"})
	identity := resolver.Resolve(Event{CommandID: "56"}, Mapping{})
	if len(identity.HAIdentifiers) != 1 || identity.HAIdentifiers[0] != "apartment:ajaxbridge_0001_zone_2" {
		t.Fatalf("catalog identifiers = %#v", identity.HAIdentifiers)
	}
	publisher := NewPublisher(PublisherConfig{SourceID: "apartment"}, fakeMQTT{})
	device := Device{DeviceSlug: identity.DeviceSlug, HAIdentifiers: identity.HAIdentifiers}
	_, body, err := publisher.BuildDiscovery(Command{CommandID: "56", Metric: "temperature_c", Component: ComponentSensor}, device)
	if err != nil {
		t.Fatal(err)
	}
	var config DiscoveryConfig
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	if config.Device.Identifiers[0] != identity.HAIdentifiers[0] {
		t.Fatalf("publisher double-scoped catalog identifiers: %#v", config.Device.Identifiers)
	}
	hub := resolver.ResolveDiscovery(Discovery{DeviceType: "Hub"})
	if hub.HAIdentifiers[0] != "apartment:ajaxbridge_account_0001" {
		t.Fatalf("hub identifier = %#v", hub.HAIdentifiers)
	}
	if !deviceLinkedToSIA(device) {
		t.Fatal("namespaced SIA identifier was not recognized")
	}
}

func TestSourceIdentityDelimiterPreservesAjaxBridgeTokensInNames(t *testing.T) {
	legacyID := "ajaxbridge_jeedom_heater_ajaxbridge_monitor"
	for _, sourceID := range []string{"", "ajaxbridge", "home_ajaxbridge_lab", "home-a", "home_a"} {
		publisher := NewPublisher(PublisherConfig{SourceID: sourceID}, fakeMQTT{})
		device := Device{Device: "Heater", DeviceSlug: "heater_ajaxbridge_monitor"}
		identifiers := publisher.discoveryIdentifiers(device)
		wanted := legacyID
		if sourceID != "" {
			wanted = sourceID + ":" + legacyID
		}
		if len(identifiers) != 1 || identifiers[0] != wanted {
			t.Fatalf("source %q identifiers = %#v, want %s", sourceID, identifiers, wanted)
		}
		// A reconciled cache/resolver identity must retain its namespace once.
		device.HAIdentifiers = identifiers
		republished := publisher.discoveryIdentifiers(device)
		if len(republished) != 1 || republished[0] != wanted {
			t.Fatalf("source %q double-scoped %s: %#v", sourceID, wanted, republished)
		}
		if sourceID != "" {
			parsedSource, parsedIdentity, found := strings.Cut(identifiers[0], ":")
			if !found || parsedSource != sourceID || parsedIdentity != legacyID {
				t.Fatalf("ambiguous source identity %s", identifiers[0])
			}
		}
	}
}

func TestJeedomSourceMetadataAndCleanupStayScoped(t *testing.T) {
	for _, sourceID := range []string{"", "apartment"} {
		t.Run(sourceID, func(t *testing.T) {
			mqtt := &recordingMQTT{}
			publisher := NewPublisher(PublisherConfig{SourceID: sourceID, Discovery: true, DiscoveryNode: "ajaxbridge", Controls: true}, mqtt)
			device := Device{
				Device: "Kitchen", DeviceSlug: "sia_0001_zone_2", LegacyDeviceSlugs: []string{"kitchen"},
				Values:      map[string]any{"temperature_c": float64(20)},
				RawCommands: map[string]Command{"56": {CommandID: "56", DeviceSlug: "sia_0001_zone_2", Metric: "temperature_c", Component: ComponentSensor, Value: float64(20), LastValueAt: time.Unix(100, 0)}},
			}
			if err := publisher.PublishDevice(t.Context(), device); err != nil {
				t.Fatal(err)
			}
			for topic := range mqtt.discovery {
				if sourceID != "" && !strings.Contains(topic, "/"+sourceID+"_") {
					t.Fatalf("source %q published/cleaned foreign discovery: %s", sourceID, topic)
				}
			}
			for _, topic := range []string{publisher.StateTopic(device.DeviceSlug), publisher.AttributesTopic(device.DeviceSlug)} {
				var payload map[string]any
				if err := json.Unmarshal([]byte(mqtt.state[topic]), &payload); err != nil {
					t.Fatal(err)
				}
				actual, exists := payload["source_id"]
				if sourceID == "" && exists || sourceID != "" && actual != sourceID {
					t.Fatalf("source %q metadata at %s = %#v", sourceID, topic, payload)
				}
			}
		})
	}
}

func TestSourceControlRoutesKeepActualJeedomIDs(t *testing.T) {
	for _, sourceID := range []string{"house", "apartment"} {
		t.Run(sourceID, func(t *testing.T) {
			jeedomRoot := "jeedom_" + sourceID
			discovery, err := ParseDiscoveryMessage(jeedomRoot+"/discovery/eqLogic/10", []byte(relayDiscoveryPayload), time.Unix(100, 0))
			if err != nil {
				t.Fatal(err)
			}
			store := NewStore("keep_last")
			device := store.ApplyDiscovery(discovery).Device
			mqtt := &fakeCommandPublisher{}
			statePrefix := "ajaxbridge/" + sourceID + "/jeedom"
			controller := NewController(ControllerConfig{Enabled: true, StateTopicPrefix: statePrefix, JeedomSetTopicPrefix: jeedomRoot + "/cmd/set"}, store, mqtt, zerolog.Nop())
			publisher := NewPublisher(PublisherConfig{SourceID: sourceID, StateTopicPrefix: statePrefix}, fakeMQTT{})
			_, body, err := publisher.BuildButtonDiscovery(device.Actions["on"], device)
			if err != nil {
				t.Fatal(err)
			}
			var config DiscoveryConfig
			if err := json.Unmarshal(body, &config); err != nil {
				t.Fatal(err)
			}
			if !controller.IsCommandTopic(config.CommandTopic) || config.CommandTopic != controller.CommandTopic(device.DeviceSlug) {
				t.Fatalf("publisher/controller disagree: %s / %s", config.CommandTopic, controller.CommandTopic(device.DeviceSlug))
			}
			result, err := controller.HandleMQTTCommand(t.Context(), config.CommandTopic, []byte(config.PayloadPress))
			if err != nil {
				t.Fatal(err)
			}
			if result.CommandID != "85" || mqtt.topic != jeedomRoot+"/cmd/set/85" || mqtt.payload != "1" {
				t.Fatalf("source control renumbered or misrouted: %#v / %s / %s", result, mqtt.topic, mqtt.payload)
			}
			foreignTopic := "ajaxbridge/other/jeedom/devices/" + device.DeviceSlug + "/set"
			if controller.IsCommandTopic(foreignTopic) {
				t.Fatalf("source %q accepts another installation's control topic", sourceID)
			}
		})
	}
}
