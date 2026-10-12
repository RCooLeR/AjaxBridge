package hamqtt

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	paho "github.com/eclipse/paho.mqtt.golang"
)

// Opt in only against a disposable local broker, never the user's NAS broker.
func TestHomeAssistantRecoveryBrokerIntegration(t *testing.T) {
	broker := os.Getenv("AJAXBRIDGE_TEST_MQTT_BROKER")
	if broker == "" {
		t.Skip("set AJAXBRIDGE_TEST_MQTT_BROKER to a disposable local Mosquitto broker")
	}
	parsed, err := url.Parse(broker)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "mqtt-recovery-fixture") {
		t.Fatal("integration test only allows loopback or the isolated mqtt-recovery-fixture container")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var mu sync.Mutex
	counts := make(map[string]int)
	commands := 0
	options := paho.NewClientOptions().AddBroker(broker).SetClientID("recovery-test-observer")
	observer := paho.NewClient(options)
	waitToken := func(token paho.Token) {
		t.Helper()
		if !token.WaitTimeout(5 * time.Second) {
			t.Fatal("fixture MQTT operation timed out")
		}
		if err := token.Error(); err != nil {
			t.Fatal(err)
		}
	}
	waitToken(observer.Connect())
	defer observer.Disconnect(250)
	waitToken(observer.Subscribe("#", 1, func(_ paho.Client, message paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		counts[message.Topic()]++
		if strings.Contains(message.Topic(), "/set") {
			commands++
		}
	}))
	readCount := func(topic string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[topic]
	}
	waitUntil := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !check() {
			if time.Now().After(deadline) {
				t.Fatal("fixture observation timed out")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	var stateTopics, discoveryTopics []string
	for _, source := range []string{"", "appt"} {
		prefix, jeedomPrefix, label := "ajaxbridge", "ajaxbridge/jeedom", "house"
		if source != "" {
			prefix += "/" + source
			jeedomPrefix += "/" + source
			label = source
		}
		publisher := New(Config{
			Broker: broker, ClientID: "recovery-test-" + label, SourceID: source,
			TopicPrefix: prefix, Discovery: true, Retain: true,
			HomeAssistantStatusTopic: "homeassistant/status", HomeAssistantOnlinePayload: "online",
		}, zerologNop())
		if err := publisher.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		defer publisher.Close()
		jeedomPublisher := jeedom.NewPublisher(jeedom.PublisherConfig{
			SourceID: source, StateTopicPrefix: jeedomPrefix, DiscoveryNode: prefix,
			Discovery: true, RetainState: true, RetainDiscovery: true,
		}, publisher)
		device := jeedom.Device{Device: "Fixture", DeviceSlug: "fixture", Values: map[string]any{"temperature": 21.0}, RawCommands: map[string]jeedom.Command{
			"101": {CommandID: "101", DeviceSlug: "fixture", Name: "Temperature", Metric: "temperature", Component: "sensor", Value: 21.0},
		}}
		snapshot := state.Snapshot{Revision: 1, Accounts: []state.Account{{Account: "0001", Online: true}}}
		publish := func(ctx context.Context) {
			if err := publisher.PublishSnapshot(ctx, snapshot); err != nil {
				t.Error(err)
			}
			if err := jeedomPublisher.PublishDevice(ctx, device); err != nil {
				t.Error(err)
			}
		}
		publish(ctx)
		if err := publisher.StartHomeAssistantRecovery(ctx, func(ctx context.Context) {
			if err := publisher.ReplayRetained(ctx, publish); err != nil {
				t.Error(err)
			}
		}); err != nil {
			t.Fatal(err)
		}
		stateTopics = append(stateTopics, prefix+"/accounts/0001/state", jeedomPrefix+"/devices/fixture/state")
		node, object := "ajaxbridge", "jeedom_cmd_101"
		if source != "" {
			node = "ajaxbridge_appt"
			object = "appt_jeedom_cmd_101"
		}
		discoveryTopics = append(discoveryTopics, "homeassistant/sensor/"+node+"/"+object+"/config")
	}
	expected := append(stateTopics, discoveryTopics...)
	waitUntil(func() bool {
		for _, topic := range expected {
			if readCount(topic) < 1 {
				return false
			}
		}
		return true
	})
	baseline := make(map[string]int)
	for _, topic := range expected {
		baseline[topic] = readCount(topic)
	}
	waitToken(observer.Publish("homeassistant/status", 1, false, "offline"))
	time.Sleep(6 * time.Second)
	for _, topic := range expected {
		if readCount(topic) != baseline[topic] {
			t.Fatalf("offline triggered replay: %s", topic)
		}
	}
	for range 10 {
		waitToken(observer.Publish("homeassistant/status", 1, false, "online"))
	}
	waitUntil(func() bool {
		for _, topic := range expected {
			if readCount(topic) < baseline[topic]+1 {
				return false
			}
		}
		return true
	})
	time.Sleep(6 * time.Second)
	for _, topic := range expected {
		if readCount(topic) != baseline[topic]+1 {
			t.Fatalf("birth burst caused extra replay: %s count=%d", topic, readCount(topic))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if commands != 0 {
		t.Fatalf("recovery published %d actuator commands", commands)
	}
	t.Logf("Local broker: two isolated namespaces recovered %d state/discovery topics; offline ignored, online burst coalesced, zero commands", len(expected))
}
