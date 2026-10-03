package app

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/config"
	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/metrics"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

func TestHandleCatalogChangedQueuesJeedomPublish(t *testing.T) {
	store := jeedomStoreWithDevices("Alpha", "Bravo", "Charlie")
	mqtt := &contextRecordingJeedomMQTT{}
	publisher := jeedom.NewPublisher(jeedom.PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
		RetainState:      true,
	}, mqtt)
	application := &App{
		cfg: config.Config{
			JeedomDiscoverUnlinked: true,
			MQTTTimeout:            time.Hour,
		},
		log:         zerolog.Nop(),
		devices:     devicecatalog.Empty(),
		metrics:     metrics.New(prometheus.NewRegistry()),
		jeedom:      store,
		jeedomPub:   publisher,
		jeedomQueue: make(chan jeedomPublishBatch, 1),
	}

	application.handleCatalogChanged(state.Snapshot{})

	if got := len(mqtt.callsSnapshot()); got != 0 {
		t.Fatalf("catalog callback published %d MQTT messages synchronously", got)
	}
	select {
	case batch := <-application.jeedomQueue:
		if got, want := deviceSlugs(batch.devices), deviceSlugs(store.Devices()); !slices.Equal(got, want) {
			t.Fatalf("queued devices = %v, want %v", got, want)
		}
		if batch.message != "publish Jeedom device after catalog change" {
			t.Fatalf("queued message = %q", batch.message)
		}
	default:
		t.Fatal("catalog callback did not queue Jeedom publish")
	}
}

func TestEnqueueJeedomPublishCoalescesLatestBatch(t *testing.T) {
	application := &App{jeedomQueue: make(chan jeedomPublishBatch, 1)}
	application.enqueueJeedomPublish([]jeedom.Device{{DeviceSlug: "old"}}, "old batch")
	latest := []jeedom.Device{{DeviceSlug: "new"}}
	application.enqueueJeedomPublish(latest, "new batch")
	latest[0].DeviceSlug = "mutated by caller"

	select {
	case batch := <-application.jeedomQueue:
		if got := deviceSlugs(batch.devices); !slices.Equal(got, []string{"new"}) {
			t.Fatalf("queued devices = %v, want latest batch", got)
		}
		if batch.message != "new batch" {
			t.Fatalf("queued message = %q, want latest batch", batch.message)
		}
	default:
		t.Fatal("Jeedom publish queue is empty")
	}
}

func TestQueuedJeedomPublishUsesLifecycleContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mqtt := &contextRecordingJeedomMQTT{
		afterCall: func(count int) {
			if count == 4 {
				cancel()
			}
		},
	}
	publisher := jeedom.NewPublisher(jeedom.PublisherConfig{
		StateTopicPrefix: "ajaxbridge/jeedom",
	}, mqtt)
	application := &App{
		cfg:         config.Config{MQTTTimeout: time.Hour},
		log:         zerolog.Nop(),
		jeedomPub:   publisher,
		jeedomQueue: make(chan jeedomPublishBatch, 1),
	}
	devices := []jeedom.Device{
		{DeviceSlug: "first"},
		{DeviceSlug: "second"},
	}
	application.enqueueJeedomPublish(devices, "test publish")

	application.publishQueuedJeedomDevices(ctx)

	calls := mqtt.callsSnapshot()
	wantTopics := []string{
		publisher.StateTopic("first"), publisher.AttributesTopic("first"),
		publisher.StateTopic("second"), publisher.AttributesTopic("second"),
	}
	if got := callTopics(calls); !slices.Equal(got, wantTopics) {
		t.Fatalf("published topics = %v, want %v", got, wantTopics)
	}
	for i, call := range calls {
		if call.ctx != ctx {
			t.Fatalf("MQTT call %d replaced the queue lifecycle context", i)
		}
	}
}

func TestRetainedJeedomPublishKeepsCallerContext(t *testing.T) {
	mqtt := &contextRecordingJeedomMQTT{}
	application := &App{
		cfg: config.Config{MQTTTimeout: time.Nanosecond},
		log: zerolog.Nop(),
		jeedomPub: jeedom.NewPublisher(jeedom.PublisherConfig{
			StateTopicPrefix: "ajaxbridge/jeedom",
		}, mqtt),
	}
	ctx := context.WithValue(context.Background(), testContextKey{}, "retained")

	application.publishJeedomDevices(ctx, []jeedom.Device{{DeviceSlug: "first"}}, "retained publish")

	calls := mqtt.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("MQTT calls = %d, want state and attributes", len(calls))
	}
	for i, call := range calls {
		if call.ctx != ctx {
			t.Fatalf("MQTT call %d replaced reconnect caller context", i)
		}
	}
}

func TestQueuedJeedomPublisherStopsOnParentCancellation(t *testing.T) {
	mqtt := &contextRecordingJeedomMQTT{}
	application := &App{
		cfg:         config.Config{MQTTTimeout: time.Hour},
		log:         zerolog.Nop(),
		jeedomPub:   jeedom.NewPublisher(jeedom.PublisherConfig{StateTopicPrefix: "ajaxbridge/jeedom"}, mqtt),
		jeedomQueue: make(chan jeedomPublishBatch, 1),
	}
	application.enqueueJeedomPublish([]jeedom.Device{
		{DeviceSlug: "first"},
		{DeviceSlug: "second"},
	}, "test publish")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	application.publishQueuedJeedomDevices(ctx)

	if got := len(mqtt.callsSnapshot()); got != 0 {
		t.Fatalf("MQTT calls after parent cancellation = %d, want 0", got)
	}
}

type testContextKey struct{}

type jeedomMQTTCall struct {
	ctx   context.Context
	topic string
}

type contextRecordingJeedomMQTT struct {
	mu        sync.Mutex
	calls     []jeedomMQTTCall
	afterCall func(int)
}

func (m *contextRecordingJeedomMQTT) PublishStateMessage(ctx context.Context, topic string, _ []byte, _ bool) error {
	m.mu.Lock()
	m.calls = append(m.calls, jeedomMQTTCall{ctx: ctx, topic: topic})
	count := len(m.calls)
	afterCall := m.afterCall
	m.mu.Unlock()
	if afterCall != nil {
		afterCall(count)
	}
	return nil
}

func (*contextRecordingJeedomMQTT) PublishDiscoveryMessage(context.Context, string, string, []byte, bool) error {
	return nil
}

func (*contextRecordingJeedomMQTT) AvailabilityTopic() string {
	return ""
}

func (m *contextRecordingJeedomMQTT) callsSnapshot() []jeedomMQTTCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

func jeedomStoreWithDevices(names ...string) *jeedom.Store {
	store := jeedom.NewStore("keep_last")
	for i, name := range names {
		id := strconv.Itoa(i + 1)
		store.ApplyDiscovery(jeedom.Discovery{
			Topic:        "jeedom/discovery/eqLogic/" + id,
			EqLogicID:    id,
			Name:         name,
			DeviceType:   "CombiProtect",
			Enabled:      true,
			Visible:      true,
			InfoCommands: map[string]jeedom.DiscoveryCommand{},
			Actions:      map[string]jeedom.DiscoveryCommand{},
			ReceivedAt:   time.Unix(int64(i+1), 0),
		})
	}
	return store
}

func deviceSlugs(devices []jeedom.Device) []string {
	slugs := make([]string, 0, len(devices))
	for _, device := range devices {
		slugs = append(slugs, device.DeviceSlug)
	}
	return slugs
}

func callTopics(calls []jeedomMQTTCall) []string {
	topics := make([]string, 0, len(calls))
	for _, call := range calls {
		topics = append(topics, call.topic)
	}
	return topics
}
