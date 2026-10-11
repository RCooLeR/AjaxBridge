package hamqtt

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	paho "github.com/eclipse/paho.mqtt.golang"
)

func TestHomeAssistantBirthReplaysUnchangedSIAAndJeedomWithoutCommands(t *testing.T) {
	for _, source := range []string{"", "appt"} {
		t.Run(source, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				prefix, jeedomPrefix := "ajaxbridge", "ajaxbridge/jeedom"
				if source != "" {
					prefix += "/" + source
					jeedomPrefix += "/" + source
				}
				client := &recoveryRecordingClient{stubClient: stubClient{open: true}}
				publisher := New(Config{
					Broker: "tcp://fixture:1883", SourceID: source, TopicPrefix: prefix,
					Discovery: true, Retain: true,
					HomeAssistantStatusTopic: "homeassistant/status", HomeAssistantOnlinePayload: "online",
				}, zerologNop())
				publisher.client = client
				jeedomPublisher := jeedom.NewPublisher(jeedom.PublisherConfig{
					SourceID: source, StateTopicPrefix: jeedomPrefix, DiscoveryNode: prefix,
					Discovery: true, Controls: true, RetainState: true, RetainDiscovery: true,
				}, publisher)
				discovery, err := jeedom.ParseDiscoveryMessage("jeedom/discovery/eqLogic/10", []byte(`{
					"id":10,"name":"Relay","configuration":{"device":"Relay"},"isEnable":1,"isVisible":1,
					"cmds":{
						"81":{"id":81,"logicalId":"realState","name":"State","type":"info","subType":"binary","currentValue":1},
						"85":{"id":85,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","value":"81"},
						"86":{"id":86,"logicalId":"SWITCH_OFF","name":"Off","type":"action","subType":"other","value":"81"}
					}}`), time.Now())
				if err != nil {
					t.Fatal(err)
				}
				device := jeedom.NewStore("keep_last").ApplyDiscovery(discovery).Device
				snapshot := state.Snapshot{
					Accounts: []state.Account{{Account: "0001", Online: true}},
					Zones:    []state.Zone{{Account: "0001", Zone: "3", DeviceEvents: []string{"fire"}}},
				}
				publish := func(ctx context.Context) {
					if err := publisher.PublishSnapshot(ctx, snapshot); err != nil {
						t.Error(err)
					}
					if err := jeedomPublisher.PublishDevice(ctx, device); err != nil {
						t.Error(err)
					}
				}
				publish(ctx)
				first := client.snapshot()
				if !client.hasTopicContaining("jeedom_control_") {
					t.Fatal("fixture must include Jeedom switch discovery")
				}
				publish(ctx)
				if len(client.snapshot()) != len(first) {
					t.Fatal("fixture's unchanged snapshots were not deduplicated")
				}
				if err := publisher.StartHomeAssistantRecovery(ctx, func(ctx context.Context) {
					if err := publisher.ReplayRetained(ctx, publish); err != nil {
						t.Error(err)
					}
				}); err != nil {
					t.Fatal(err)
				}
				handler := publisher.subscriptions["homeassistant/status"]
				if handler == nil {
					t.Fatal("missing HA birth subscription")
				}
				for _, payload := range []string{"offline", "Online", "online\n", ""} {
					handler("homeassistant/status", []byte(payload))
				}
				handler("other/status", []byte("online"))
				synctest.Wait()
				if len(client.snapshot()) != len(first) {
					t.Fatal("non-birth payload caused replay")
				}
				for range 20 {
					handler("homeassistant/status", []byte("online"))
				}
				synctest.Wait()
				if len(client.snapshot()) != len(first) {
					t.Fatal("MQTT callback replayed synchronously or skipped startup delay")
				}
				time.Sleep(6 * time.Second)
				synctest.Wait()
				replayed := client.snapshot()[len(first):]
				if len(replayed) != len(first)+1 || replayed[0].topic != prefix+"/status" || stringValue(replayed[0].payload) != "online" {
					t.Fatalf("expected one availability plus identical snapshot: first=%d replay=%d", len(first), len(replayed))
				}
				if !reflect.DeepEqual(replayed[1:], first) {
					t.Fatal("replay changed state, discovery identities, topics, or retain flags")
				}
				for _, message := range replayed {
					if strings.Contains(message.topic, "/set") {
						t.Fatalf("replay sent actuator command: %s", message.topic)
					}
				}
				cancel()
				handler("homeassistant/status", []byte("online"))
				time.Sleep(6 * time.Second)
				synctest.Wait()
				if len(client.snapshot()) != 2*len(first)+1 {
					t.Fatal("replayed after shutdown")
				}
			})
		})
	}
}

func TestHomeAssistantRecoveryKeepsOneBirthDuringReplay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		publisher := New(Config{}, zerologNop())
		requests := make(chan struct{}, 1)
		release := make(chan struct{})
		calls := 0
		go publisher.runHomeAssistantRecovery(ctx, requests, func(context.Context) {
			calls++
			if calls == 1 {
				<-release
			}
		}, func() time.Duration { return time.Second })
		requests <- struct{}{}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if calls != 1 {
			t.Fatalf("calls = %d, want first replay", calls)
		}
		for range 50 {
			select {
			case requests <- struct{}{}:
			default:
			}
		}
		close(release)
		synctest.Wait()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if calls != 2 {
			t.Fatalf("calls = %d, want one coalesced follow-up", calls)
		}
	})
}

func TestHomeAssistantRecoveryCancellationDuringDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		publisher := New(Config{}, zerologNop())
		requests := make(chan struct{}, 1)
		go publisher.runHomeAssistantRecovery(ctx, requests, func(context.Context) {
			t.Error("replayed after cancellation")
		}, func() time.Duration { return time.Second })
		requests <- struct{}{}
		synctest.Wait()
		cancel()
		time.Sleep(2 * time.Second)
		synctest.Wait()
	})
}

func TestHomeAssistantRecoveryConfigurationAndCancellation(t *testing.T) {
	for _, topic := range []string{"", "custom/status", "ajaxbridge/status"} {
		ctx, cancel := context.WithCancel(t.Context())
		publisher := New(Config{Broker: "tcp://fixture:1883", HomeAssistantStatusTopic: topic, HomeAssistantOnlinePayload: "ready"}, zerologNop())
		publisher.client = &stubClient{open: true}
		err := publisher.StartHomeAssistantRecovery(ctx, func(context.Context) {})
		cancel()
		if topic == "ajaxbridge/status" {
			if err == nil {
				t.Fatal("accepted feedback loop through own availability topic")
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if got := len(publisher.subscriptions); (topic == "" && got != 0) || (topic == "custom/status" && got != 1) {
			t.Fatalf("topic=%q subscriptions=%d", topic, got)
		}
	}
	publisher := New(Config{Broker: "tcp://fixture:1883"}, zerologNop())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := publisher.ReplayRetained(ctx, func(context.Context) { t.Error("cancelled replay ran") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replay error=%v", err)
	}
}

func TestReplayCannotOverwriteNewerSIAZoneWhilePublishingAnotherEntity(t *testing.T) {
	client := &blockedStateClient{
		stubClient: stubClient{open: true},
		blockTopic: "ajaxbridge/accounts/0001/state",
		entered:    make(chan struct{}), release: make(chan struct{}),
	}
	publisher := New(Config{Broker: "tcp://fixture:1883", Retain: true}, zerologNop())
	publisher.client = client
	old := state.Snapshot{
		Revision: 1,
		Accounts: []state.Account{{Account: "0001", Online: true}},
		Zones:    []state.Zone{{Account: "0001", Zone: "3", AlarmActive: false}},
	}
	done := make(chan error, 1)
	go func() {
		done <- publisher.ReplayRetained(t.Context(), func(ctx context.Context) {
			if err := publisher.PublishSnapshot(ctx, old); err != nil {
				t.Error(err)
			}
		})
	}()
	<-client.entered
	// The live zone must remain publishable while replay is blocked on another
	// entity, and its alarm must survive replay's subsequently stale zone.
	if err := publisher.PublishUpdate(t.Context(), Update{
		Revision: 2, Zones: []state.Zone{{Account: "0001", Zone: "3", AlarmActive: true}},
	}); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var seen int
	for _, message := range client.publishes {
		if message.topic == "ajaxbridge/accounts/0001/zones/3/state" {
			seen++
			var body zonePayload
			if err := json.Unmarshal([]byte(stringValue(message.payload)), &body); err != nil || !body.AlarmActive {
				t.Fatalf("stale replay rolled back live zone: %s (%v)", stringValue(message.payload), err)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("zone publishes=%d, want only newer state", seen)
	}
}

func TestSIARevisionGatesSurviveCacheResetAndAllowEqualReplay(t *testing.T) {
	client := &stubClient{open: true}
	publisher := New(Config{Broker: "tcp://fixture:1883", Retain: true}, zerologNop())
	publisher.client = client
	current := state.Snapshot{Revision: 5, Accounts: []state.Account{{Account: "1", Online: false}}, Zones: []state.Zone{{Account: "1", Zone: "2", DeviceName: "New name"}}}
	if err := publisher.PublishSnapshot(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	count := len(client.publishes)
	publisher.resetCaches()
	for _, revision := range []uint64{4, 0} {
		stale := current
		stale.Revision = revision
		if err := publisher.PublishSnapshot(t.Context(), stale); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.publishes) != count {
		t.Fatal("stale revision bypassed gates after cache reset")
	}
	if err := publisher.PublishSnapshot(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if len(client.publishes) != 2*count {
		t.Fatal("equal revision did not replay after cache reset")
	}
}

type blockedStateClient struct {
	stubClient
	mu         sync.Mutex
	once       sync.Once
	blockTopic string
	entered    chan struct{}
	release    chan struct{}
}

type recoveryRecordingClient struct {
	stubClient
	mu sync.Mutex
}

func (c *recoveryRecordingClient) Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stubClient.Publish(topic, qos, retained, payload)
}

func (c *recoveryRecordingClient) snapshot() []stubPublish {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]stubPublish(nil), c.publishes...)
}

func (c *blockedStateClient) Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token {
	if topic == c.blockTopic {
		c.once.Do(func() { close(c.entered); <-c.release })
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stubClient.Publish(topic, qos, retained, payload)
}
