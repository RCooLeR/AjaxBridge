package hamqtt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/state"
)

func TestSourceIsolationWithRepeatedAccountAndZone(t *testing.T) {
	zone := state.Zone{Account: "0001", Zone: "2", DeviceName: "Kitchen", DeviceEvents: []string{"fire", "power"}}
	account := state.Account{Account: "0001", Online: true}
	seenTopics := make(map[string]bool)
	seenEntities := make(map[string]bool)
	seenDevices := make(map[string]bool)
	for _, sourceID := range []string{"", "home-a", "home_a", "ajaxbridge", "home_ajaxbridge_lab"} {
		publisher := New(Config{SourceID: sourceID, TopicPrefix: "ajaxbridge", DiscoveryPrefix: "homeassistant"}, zerologNop())
		zonePlan, err := publisher.zonePlanFor(zone)
		if err != nil {
			t.Fatal(err)
		}
		accountPlan, err := publisher.accountPlanFor(account)
		if err != nil {
			t.Fatal(err)
		}
		for _, plan := range [][]discoveryMessage{accountPlan.discovery, zonePlan.discovery} {
			var deviceID string
			for _, message := range plan {
				var config discoveryConfig
				if err := json.Unmarshal(message.payload, &config); err != nil {
					t.Fatal(err)
				}
				if seenTopics[message.topic] || seenEntities[config.UniqueID] {
					t.Fatalf("source %q collides at %s / %s", sourceID, message.topic, config.UniqueID)
				}
				seenTopics[message.topic] = true
				seenEntities[config.UniqueID] = true
				deviceID = config.Device.Identifiers[0]
				if sourceID != "" && (!strings.HasPrefix(config.UniqueID, sourceID+":") || !strings.Contains(message.topic, "/"+sourceID+"_")) {
					t.Fatalf("source was normalized or omitted: %s / %s", message.topic, config.UniqueID)
				}
			}
			if seenDevices[deviceID] {
				t.Fatalf("source %q shares device %s", sourceID, deviceID)
			}
			seenDevices[deviceID] = true
		}
		for _, message := range append(accountPlan.cleanup, zonePlan.cleanup...) {
			if sourceID != "" && (strings.Contains(message.topic, "/ajax2prometheus/") || !strings.Contains(message.topic, "/"+sourceID+"_")) {
				t.Fatalf("source %q cleans discovery outside its namespace: %s", sourceID, message.topic)
			}
		}
	}
}

func TestSIAStateAndAttributesCarryOptionalSourceID(t *testing.T) {
	for _, sourceID := range []string{"", "apartment"} {
		t.Run(sourceID, func(t *testing.T) {
			client := &stubClient{open: true}
			publisher := New(Config{SourceID: sourceID, Broker: "tcp://mqtt:1883", TopicPrefix: "ajaxbridge", Timeout: time.Second}, zerologNop())
			publisher.client = client
			if err := publisher.PublishSnapshot(t.Context(), state.Snapshot{
				Accounts: []state.Account{{Account: "0001", Online: true}},
				Zones:    []state.Zone{{Account: "0001", Zone: "2"}},
			}); err != nil {
				t.Fatal(err)
			}
			if len(client.publishes) != 4 {
				t.Fatalf("published %d messages, want account/zone state and attributes", len(client.publishes))
			}
			for _, message := range client.publishes {
				var payload map[string]any
				if err := json.Unmarshal([]byte(stringValue(message.payload)), &payload); err != nil {
					t.Fatal(err)
				}
				actual, exists := payload["source_id"]
				if sourceID == "" && exists || sourceID != "" && actual != sourceID {
					t.Fatalf("source %q metadata at %s = %#v", sourceID, message.topic, payload)
				}
			}
		})
	}
}

func TestSourceSIAIdentityIsIndependentOfTopicRouting(t *testing.T) {
	zone := state.Zone{Account: "0001", Zone: "2"}
	for _, topicPrefix := range []string{"ajaxbridge/apartment", "custom/apartment"} {
		publisher := New(Config{SourceID: "apartment", TopicPrefix: topicPrefix}, zerologNop())
		plan, err := publisher.zonePlanFor(zone)
		if err != nil {
			t.Fatal(err)
		}
		var config discoveryConfig
		if err := json.Unmarshal(plan.discovery[0].payload, &config); err != nil {
			t.Fatal(err)
		}
		if config.UniqueID != "apartment:ajaxbridge_zone_0001_2_alarm_active" {
			t.Fatalf("routing %q changed stable source identity to %s", topicPrefix, config.UniqueID)
		}
	}
	legacy := New(Config{TopicPrefix: "custom"}, zerologNop())
	if actual := legacy.uniqueID("zone_0001_2_alarm_active"); actual != "custom_zone_0001_2_alarm_active" {
		t.Fatalf("legacy custom namespace changed: %s", actual)
	}
}
