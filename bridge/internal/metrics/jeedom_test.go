package metrics

import (
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
)

func TestJeedomSnapshotRestoresValuesBeforeFirstMQTTEvent(t *testing.T) {
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	store := jeedom.NewStore("keep_last")
	store.ApplyDiscovery(jeedom.Discovery{
		EqLogicID: "20", Name: "Server", ReceivedAt: at,
		InfoCommands: map[string]jeedom.DiscoveryCommand{
			"55": {CommandID: "55", Name: "Temperature", Type: "info", Subtype: "numeric", Value: []byte(`23.5`)},
			"56": {CommandID: "56", Name: "Battery", Type: "info", Subtype: "numeric", Value: []byte(`0`)},
		},
	})
	path := filepath.Join(t.TempDir(), "cache.json")
	store.SetPath(path)
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := jeedom.LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	restored.ReconcileResolver(nil)
	registry := prometheus.NewPedanticRegistry()
	m := New(registry)
	m.SetJeedomStore(restored)
	assertJeedomGauge(t, registry, "ajax_jeedom_device_temperature_celsius", map[string]string{"device": "server"}, 23.5)
	assertJeedomGauge(t, registry, "ajax_jeedom_device_battery_percent", map[string]string{"device": "server"}, 0)
	assertJeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"command_id": "55"}, float64(at.Unix()))
	age := jeedomGauge(t, registry, "ajax_jeedom_value_age_seconds", map[string]string{"command_id": "55"}).GetGauge().GetValue()
	if age < 3600 || age > 3610 {
		t.Fatalf("restored value age = %v, want about 3600 seconds", age)
	}
	families, _ := registry.Gather()
	for _, family := range families {
		if family.GetName() == "ajax_jeedom_device_temperature_celsius" && family.GetUnit() != "celsius" {
			t.Fatalf("temperature unit = %q", family.GetUnit())
		}
	}
}

func TestJeedomExplicitEnergyNeverAppearsAsPower(t *testing.T) {
	for _, test := range []struct{ unit, metric string }{
		{"Wh", "energy_wh"}, {"kWh", "energy_kwh"}, {"MWh", "energy_mwh"},
	} {
		t.Run(test.unit, func(t *testing.T) {
			store := jeedom.NewStore("keep_last")
			registry := prometheus.NewPedanticRegistry()
			New(registry).SetJeedomStore(store)
			discovery := jeedom.Discovery{
				EqLogicID: "27", Name: "Socket", DeviceType: "Socket", ReceivedAt: time.Unix(500, 0),
				InfoCommands: map[string]jeedom.DiscoveryCommand{
					"221": {CommandID: "221", LogicalID: "powerWtH", GenericType: "POWER", Name: "Consommation", Type: "info", Subtype: "numeric", Unit: test.unit, Value: []byte(`7.5`)},
				},
			}
			store.ApplyDiscovery(discovery)
			assertJeedomGauge(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "221", "metric": test.metric}, 7.5)
			assertJeedomAbsent(t, registry, "ajax_jeedom_device_power_watts", nil)
			assertJeedomAbsent(t, registry, "ajax_jeedom_command_value", map[string]string{"metric": "power_raw"})

			// Changing only unit metadata cannot reuse the old number under a
			// different energy scale, even with keep_last enabled.
			command := discovery.InfoCommands["221"]
			command.Unit, command.Value = "J", nil
			discovery.InfoCommands["221"] = command
			discovery.ReceivedAt = time.Unix(600, 0)
			store.ApplyDiscovery(discovery)
			assertJeedomAbsent(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "221"})
			assertJeedomAbsent(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"command_id": "221"})
			assertJeedomAbsent(t, registry, "ajax_jeedom_device_power_watts", nil)
			assertJeedomGauge(t, registry, "ajax_jeedom_last_received_timestamp_seconds", map[string]string{"command_id": "221", "metric": "energy_j"}, 600)
		})
	}
}

func TestJeedomSnapshotTracksDiscoveryRemovalAndOwnerRemapping(t *testing.T) {
	store := jeedom.NewStore("keep_last")
	registry := prometheus.NewPedanticRegistry()
	m := New(registry)
	m.SetJeedomStore(store)
	service := jeedom.NewService(jeedom.ServiceConfig{}, store, nil, nil, m, nil, zerolog.Nop())
	service.HandleMessage(t.Context(), "jeedom/discovery/eqLogic/20", []byte(`{"id":20,"name":"Server","cmds":{"55":{"id":55,"name":"Temperature","type":"info","subType":"numeric","currentValue":23.5},"56":{"id":56,"name":"Battery","type":"info","subType":"numeric","currentValue":80}}}`))
	assertJeedomGauge(t, registry, "ajax_jeedom_device_temperature_celsius", map[string]string{"device": "server"}, 23.5)
	assertJeedomGauge(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "56"}, 80)

	store.ReconcileResolver(metricsOwnerResolver{slug: "sia_a0f80d_zone_20"})
	assertJeedomGauge(t, registry, "ajax_jeedom_device_temperature_celsius", map[string]string{"device": "sia_a0f80d_zone_20"}, 23.5)
	assertJeedomAbsent(t, registry, "ajax_jeedom_device_temperature_celsius", map[string]string{"device": "server"})
	assertJeedomAbsent(t, registry, "ajax_jeedom_command_value", map[string]string{"device": "server"})
	assertJeedomAbsent(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"device": "server"})

	service.HandleMessage(t.Context(), "jeedom/discovery/eqLogic/20", []byte(`{"id":20,"name":"Server","cmds":{}}`))
	for _, name := range []string{"ajax_jeedom_command_value", "ajax_jeedom_device_temperature_celsius", "ajax_jeedom_device_battery_percent", "ajax_jeedom_last_update_timestamp_seconds", "ajax_jeedom_value_age_seconds", "ajax_jeedom_last_received_timestamp_seconds"} {
		assertJeedomAbsent(t, registry, name, nil)
	}
}

func TestJeedomSnapshotUnknownAndKeepLastFreshness(t *testing.T) {
	for _, policy := range []string{"unknown", "keep_last"} {
		t.Run(policy, func(t *testing.T) {
			store := jeedom.NewStore(policy)
			registry := prometheus.NewRegistry()
			New(registry).SetJeedomStore(store)
			at := time.Now().Add(-time.Hour).Truncate(time.Second)
			store.Apply(jeedom.Event{CommandID: "55", DeviceName: "Server", CommandName: "Temperature", Subtype: "numeric", Value: []byte(`24`), ReceivedAt: at})
			store.Apply(jeedom.Event{CommandID: "55", DeviceName: "Server", CommandName: "Temperature", Subtype: "numeric", Value: []byte(`null`), ReceivedAt: at.Add(time.Minute)})
			assertJeedomGauge(t, registry, "ajax_jeedom_last_received_timestamp_seconds", map[string]string{"command_id": "55"}, float64(at.Add(time.Minute).Unix()))
			if policy == "unknown" {
				for _, name := range []string{"ajax_jeedom_command_value", "ajax_jeedom_device_temperature_celsius", "ajax_jeedom_last_update_timestamp_seconds", "ajax_jeedom_value_age_seconds"} {
					assertJeedomAbsent(t, registry, name, nil)
				}
			} else {
				assertJeedomGauge(t, registry, "ajax_jeedom_device_temperature_celsius", nil, 24)
				assertJeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", nil, float64(at.Unix()))
			}
		})
	}
}

func TestJeedomMetadataReceiptDoesNotSubstituteForValueTime(t *testing.T) {
	store := jeedom.NewStore("keep_last")
	discovery := jeedom.Discovery{
		EqLogicID: "20", Name: "Server", ReceivedAt: time.Unix(500, 0),
		InfoCommands: map[string]jeedom.DiscoveryCommand{
			"55": {CommandID: "55", Name: "Temperature", Type: "info", Subtype: "numeric", Value: []byte(`24`)},
		},
	}
	store.ApplyDiscovery(discovery)
	discovery.ReceivedAt = time.Unix(1000, 0)
	store.ApplyDiscovery(discovery)
	registry := prometheus.NewRegistry()
	m := New(registry)
	m.SetJeedomStore(store)
	assertJeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", nil, 500)
	assertJeedomGauge(t, registry, "ajax_jeedom_last_received_timestamp_seconds", nil, 1000)

	path := filepath.Join(t.TempDir(), "legacy-cache.json")
	if err := os.WriteFile(path, []byte(`[{"device_slug":"server","device":"Server","raw_commands":{"55":{"command_id":"55","device_slug":"server","device":"Server","name":"Temperature","metric":"temperature_c","component":"sensor","type":"info","subtype":"numeric","value":24,"last_update":"2026-09-24T10:00:00Z"}}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := jeedom.LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetJeedomStore(legacy)
	assertJeedomGauge(t, registry, "ajax_jeedom_command_value", nil, 24)
	assertJeedomAbsent(t, registry, "ajax_jeedom_last_update_timestamp_seconds", nil)
	assertJeedomAbsent(t, registry, "ajax_jeedom_value_age_seconds", nil)
}

func TestJeedomOpenMetricsUnitMetadata(t *testing.T) {
	store := jeedom.NewStore("keep_last")
	store.Apply(jeedom.Event{CommandID: "55", DeviceName: "Server", CommandName: "Temperature", Subtype: "numeric", Value: []byte(`23.5`), ReceivedAt: time.Unix(500, 0)})
	registry := prometheus.NewRegistry()
	New(registry).SetJeedomStore(store)
	request := httptest.NewRequest("GET", "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text; version=1.0.0; charset=utf-8")
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "application/openmetrics-text") {
		t.Fatalf("OpenMetrics negotiation: status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"# UNIT ajax_jeedom_device_temperature_celsius celsius",
		"# UNIT ajax_jeedom_last_update_timestamp_seconds seconds",
		"# UNIT ajax_jeedom_value_age_seconds seconds",
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("OpenMetrics response lacks %q", want)
		}
	}
}

func TestJeedomPhysicalElectricalMetricsRequireVerifiedUnits(t *testing.T) {
	store := jeedom.NewStore("keep_last")
	registry := prometheus.NewRegistry()
	New(registry).SetJeedomStore(store)
	at := time.Now()
	store.Apply(jeedom.Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", Unit: "mA", Value: []byte(`1000`), ReceivedAt: at})
	store.Apply(jeedom.Event{CommandID: "204", DeviceName: "Server", CommandName: "Puissance", Unit: "kW", Value: []byte(`0.25`), ReceivedAt: at})
	assertJeedomGauge(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "205", "metric": "current_ma"}, 1000)
	assertJeedomGauge(t, registry, "ajax_jeedom_device_current_amperes", nil, 1)
	assertJeedomGauge(t, registry, "ajax_jeedom_device_power_watts", nil, 250)
	store.Apply(jeedom.Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", UnitProvided: true, Value: []byte(`960`), ReceivedAt: at})
	store.Apply(jeedom.Event{CommandID: "204", DeviceName: "Server", CommandName: "Puissance", UnitProvided: true, Value: []byte(`78484`), ReceivedAt: at})
	assertJeedomGauge(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "205", "metric": "current_raw"}, 960)
	assertJeedomGauge(t, registry, "ajax_jeedom_command_value", map[string]string{"command_id": "204", "metric": "power_raw"}, 78484)
	assertJeedomAbsent(t, registry, "ajax_jeedom_device_current_amperes", nil)
	assertJeedomAbsent(t, registry, "ajax_jeedom_device_power_watts", nil)
}

func TestJeedomPhysicalElectricalMetricsUseValidAlternativeAfterUnknown(t *testing.T) {
	store := jeedom.NewStore("unknown")
	registry := prometheus.NewRegistry()
	New(registry).SetJeedomStore(store)
	store.Apply(jeedom.Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", Unit: "A", Value: []byte(`1`), ReceivedAt: time.Now()})
	store.Apply(jeedom.Event{CommandID: "205", DeviceName: "Server", CommandName: "Courant", Value: []byte(`null`), ReceivedAt: time.Now()})
	store.Apply(jeedom.Event{CommandID: "206", DeviceName: "Server", CommandName: "Courant", Unit: "mA", Value: []byte(`500`), ReceivedAt: time.Now()})
	assertJeedomGauge(t, registry, "ajax_jeedom_device_current_amperes", nil, 0.5)
	store.Apply(jeedom.Event{CommandID: "206", DeviceName: "Server", CommandName: "Courant", Value: []byte(`null`), ReceivedAt: time.Now()})
	store.Apply(jeedom.Event{CommandID: "207", DeviceName: "Server", CommandName: "Courant", Unit: "µA", Value: []byte(`500000`), ReceivedAt: time.Now()})
	assertJeedomGauge(t, registry, "ajax_jeedom_device_current_amperes", nil, 0.5)
	store.Apply(jeedom.Event{CommandID: "207", DeviceName: "Server", CommandName: "Courant", Value: []byte(`null`), ReceivedAt: time.Now()})
	assertJeedomAbsent(t, registry, "ajax_jeedom_device_current_amperes", nil)
}

func TestJeedomBooleanAndEnumSemantics(t *testing.T) {
	store := jeedom.NewStore("unknown")
	registry := prometheus.NewPedanticRegistry()
	m := New(registry)
	m.SetJeedomStore(store)
	service := jeedom.NewService(jeedom.ServiceConfig{}, store, nil, nil, m, nil, zerolog.Nop())
	for _, test := range []struct {
		id, command, value string
		want               float64
	}{
		{"1", "Online", `"OFFLINE"`, 0},
		{"2", "Battery fault", `"OK"`, 0},
		{"3", "Radio connection", `"CONNECTED"`, 1},
		{"4", "Smoke alarm", `"DETECTED"`, 1},
	} {
		service.HandleMessage(t.Context(), "jeedom/cmd/event/"+test.id, []byte(fmt.Sprintf(`{"value":%s,"humanName":"[None][Server][%s]","type":"info","subtype":"binary"}`, test.value, test.command)))
		assertJeedomGauge(t, registry, "ajax_jeedom_command_boolean", map[string]string{"command_id": test.id}, test.want)
		if jeedomGauge(t, registry, "ajax_jeedom_last_update_timestamp_seconds", map[string]string{"command_id": test.id}) == nil {
			t.Fatal("boolean observation timestamp missing")
		}
	}
	service.HandleMessage(t.Context(), "jeedom/cmd/event/1", []byte(`{"value":null,"humanName":"[None][Server][Online]","type":"info","subtype":"binary"}`))
	assertJeedomAbsent(t, registry, "ajax_jeedom_command_boolean", map[string]string{"command_id": "1"})

	for _, test := range []struct{ id, command, value, state string }{
		{"5", "Battery check status", "OK", "ok"},
		{"6", "Battery state", "CHARGED", "charged"},
		{"7", "Valve position", "INTERMEDIATE", "intermediate"},
		{"8", "Signal", "STRONG", "strong"},
		{"9", "Battery check status", "unrecognized arbitrary payload", "unknown"},
	} {
		service.HandleMessage(t.Context(), "jeedom/cmd/event/"+test.id, []byte(fmt.Sprintf(`{"value":%q,"humanName":"[None][Server][%s]","type":"info","subtype":"string"}`, test.value, test.command)))
		assertJeedomGauge(t, registry, "ajax_jeedom_command_state", map[string]string{"command_id": test.id, "state": test.state}, 1)
		if test.state != "unknown" {
			assertJeedomGauge(t, registry, "ajax_jeedom_command_state", map[string]string{"command_id": test.id, "state": "unknown"}, 0)
		}
	}
	assertJeedomAbsent(t, registry, "ajax_jeedom_command_value", nil)
	service.HandleMessage(t.Context(), "jeedom/cmd/event/7", []byte(`{"value":"CLOSED","humanName":"[None][Server][Valve position]","type":"info","subtype":"string"}`))
	assertJeedomGauge(t, registry, "ajax_jeedom_command_state", map[string]string{"command_id": "7", "state": "closed"}, 1)
	assertJeedomGauge(t, registry, "ajax_jeedom_command_state", map[string]string{"command_id": "7", "state": "intermediate"}, 0)
	service.HandleMessage(t.Context(), "jeedom/cmd/event/7", []byte(`{"value":null,"humanName":"[None][Server][Valve position]","type":"info","subtype":"string"}`))
	assertJeedomAbsent(t, registry, "ajax_jeedom_command_state", map[string]string{"command_id": "7"})
}

func TestJeedomCollectorRejectsInvalidNumbersAndUnboundedStringLabels(t *testing.T) {
	collector := newJeedomCollector()
	for _, value := range []any{nil, math.NaN(), math.Inf(1), math.Inf(-1), "NaN", "123", "firmware unique version"} {
		ch := make(chan prometheus.Metric, 10)
		if collector.collectValue(ch, jeedom.Command{Metric: "firmware_version", Value: value}, []string{"server", "Firmware", "1", "firmware_version"}) || len(ch) != 0 {
			t.Fatalf("invalid/unbounded value %#v was exported", value)
		}
	}
	ch := make(chan prometheus.Metric, 10)
	if !collector.collectValue(ch, jeedom.Command{Metric: "device_last_update", DeviceClass: "timestamp", Value: "2026-09-24T00:11:31Z"}, []string{"server", "Last update", "2", "device_last_update"}) {
		t.Fatal("reported timestamp was not exported")
	}
	metric := &dto.Metric{}
	if err := (<-ch).Write(metric); err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(time.RFC3339, "2026-09-24T00:11:31Z")
	if metric.GetGauge().GetValue() != float64(at.Unix()) {
		t.Fatalf("reported timestamp = %v", metric.GetGauge().GetValue())
	}
}

func TestJeedomConcurrentScrapesUseOneConsistentSnapshot(t *testing.T) {
	store := jeedom.NewStore("keep_last")
	registry := prometheus.NewPedanticRegistry()
	New(registry).SetJeedomStore(store)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			store.Apply(jeedom.Event{CommandID: "55", DeviceName: "Server", CommandName: "Temperature", Subtype: "numeric", Value: []byte(fmt.Sprint(i)), ReceivedAt: time.Now()})
		}
	})
	for range 4 {
		wg.Go(func() {
			for range 25 {
				families, err := registry.Gather()
				if err != nil {
					t.Error(err)
					return
				}
				var command, device *float64
				for _, family := range families {
					switch family.GetName() {
					case "ajax_jeedom_command_value":
						command = family.Metric[0].Gauge.Value
					case "ajax_jeedom_device_temperature_celsius":
						device = family.Metric[0].Gauge.Value
					}
				}
				if (command == nil) != (device == nil) || (command != nil && *command != *device) {
					t.Error("command and device metrics came from different snapshots")
				}
			}
		})
	}
	wg.Wait()
}

type metricsOwnerResolver struct{ slug string }

func (r metricsOwnerResolver) Resolve(jeedom.Event, jeedom.Mapping) jeedom.DeviceIdentity {
	return jeedom.DeviceIdentity{DeviceSlug: r.slug, DeviceName: "Server", BaseSlug: r.slug}
}

func jeedomGauge(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			matches := true
			for key, value := range labels {
				found := false
				for _, label := range metric.Label {
					if label.GetName() == key && label.GetValue() == value {
						found = true
					}
				}
				matches = matches && found
			}
			if matches {
				return metric
			}
		}
	}
	return nil
}

func assertJeedomGauge(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string, want float64) {
	t.Helper()
	metric := jeedomGauge(t, registry, name, labels)
	if metric == nil || metric.GetGauge().GetValue() != want {
		t.Fatalf("%s %v = %v, want %v", name, labels, metric, want)
	}
}

func assertJeedomAbsent(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) {
	t.Helper()
	if metric := jeedomGauge(t, registry, name, labels); metric != nil {
		t.Fatalf("unexpected %s %v: %s", name, labels, strings.TrimSpace(metric.String()))
	}
}
