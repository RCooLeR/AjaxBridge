package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestZoneTamperMetricIncludesSensorLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)
	m.SetSnapshot(state.Snapshot{
		Zones: []state.Zone{
			{
				Account:           "0001",
				Partition:         "1",
				Group:             "1",
				Zone:              "3",
				Device:            "ri0",
				DeviceName:        "Kitchen transmitter",
				Room:              "Kitchen",
				Kind:              "ajax_transmitter",
				DeviceEventsLabel: "alarm,tamper",
				TamperActive:      true,
				LastSignal:        "tamper",
				LastEventAt:       time.Unix(100, 0),
			},
		},
	})

	expected := `
# HELP ajax_zone_tamper_active Whether tamper is currently active for a zone/device.
# TYPE ajax_zone_tamper_active gauge
ajax_zone_tamper_active{account="0001",device="ri0",device_events="alarm,tamper",device_kind="ajax_transmitter",device_name="Kitchen transmitter",group="1",partition="1",room="Kitchen",zone="3"} 1
# HELP ajax_zone_tamper_last_event_timestamp_seconds Unix timestamp for the last tamper or tamper restore event per zone/device.
# TYPE ajax_zone_tamper_last_event_timestamp_seconds gauge
ajax_zone_tamper_last_event_timestamp_seconds{account="0001",device="ri0",device_events="alarm,tamper",device_kind="ajax_transmitter",device_name="Kitchen transmitter",group="1",partition="1",room="Kitchen",zone="3"} 100
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "ajax_zone_tamper_active", "ajax_zone_tamper_last_event_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestZoneAlarmMetricIncludesDeviceAndReasonLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)
	m.SetSnapshot(state.Snapshot{
		Zones: []state.Zone{
			{
				Account:           "0001",
				Partition:         "1",
				Group:             "1",
				Zone:              "8",
				Device:            "ri2",
				DeviceName:        "Hall smoke detector",
				Room:              "Hall",
				Kind:              "fireprotect",
				DeviceEventsLabel: "smoke,temperature,tamper",
				AlarmActive:       true,
				AlarmSignal:       "smoke",
				AlarmAction:       "smoke_alarm",
				AlarmStartedAt:    time.Unix(200, 0),
			},
		},
	})

	expected := `
# HELP ajax_zone_alarm_active Whether an alarm is currently active for a zone/device.
# TYPE ajax_zone_alarm_active gauge
ajax_zone_alarm_active{account="0001",alarm_action="smoke_alarm",alarm_signal="smoke",device="ri2",device_events="smoke,temperature,tamper",device_kind="fireprotect",device_name="Hall smoke detector",group="1",partition="1",room="Hall",zone="8"} 1
# HELP ajax_zone_alarm_last_event_timestamp_seconds Unix timestamp for the last alarm event per zone/device.
# TYPE ajax_zone_alarm_last_event_timestamp_seconds gauge
ajax_zone_alarm_last_event_timestamp_seconds{account="0001",alarm_action="smoke_alarm",alarm_signal="smoke",device="ri2",device_events="smoke,temperature,tamper",device_kind="fireprotect",device_name="Hall smoke detector",group="1",partition="1",room="Hall",zone="8"} 200
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "ajax_zone_alarm_active", "ajax_zone_alarm_last_event_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestInactiveZoneAlarmMetricUsesStableFallbackLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)
	m.SetSnapshot(state.Snapshot{
		Zones: []state.Zone{
			{
				Account:           "0001",
				Partition:         "1",
				Group:             "1",
				Zone:              "501",
				DeviceName:        "Roma",
				Room:              "House",
				Kind:              "App",
				DeviceEventsLabel: "night_mode,arming",
				AlarmActive:       false,
			},
		},
	})

	expected := `
# HELP ajax_zone_alarm_active Whether an alarm is currently active for a zone/device.
# TYPE ajax_zone_alarm_active gauge
ajax_zone_alarm_active{account="0001",alarm_action="none",alarm_signal="none",device="501",device_events="night_mode,arming",device_kind="App",device_name="Roma",group="1",partition="1",room="House",zone="501"} 0
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "ajax_zone_alarm_active"); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotMetricsDropStaleAlarmLabelSeries(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)
	activeZone := state.Zone{
		Account:           "0001",
		Partition:         "1",
		Group:             "1",
		Zone:              "21",
		DeviceName:        "Roma",
		Room:              "House",
		Kind:              "SpaceControl",
		DeviceEventsLabel: "night_mode,arming,panic",
		AlarmActive:       true,
		AlarmSignal:       "panic",
		AlarmAction:       "panic_alarm",
		AlarmStartedAt:    time.Unix(200, 0),
	}
	m.SetSnapshot(state.Snapshot{Zones: []state.Zone{activeZone}})

	clearedZone := activeZone
	clearedZone.AlarmActive = false
	clearedZone.AlarmSignal = ""
	clearedZone.AlarmAction = ""
	clearedZone.AlarmStartedAt = time.Time{}
	m.SetSnapshot(state.Snapshot{Zones: []state.Zone{clearedZone}})

	expected := `
# HELP ajax_zone_alarm_active Whether an alarm is currently active for a zone/device.
# TYPE ajax_zone_alarm_active gauge
ajax_zone_alarm_active{account="0001",alarm_action="none",alarm_signal="none",device="21",device_events="night_mode,arming,panic",device_kind="SpaceControl",device_name="Roma",group="1",partition="1",room="House",zone="21"} 0
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "ajax_zone_alarm_active", "ajax_zone_alarm_last_event_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestZoneTroubleAndLastEventMetricsIncludeDeviceLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)
	m.SetSnapshot(state.Snapshot{
		Zones: []state.Zone{
			{
				Account:           "0001",
				Partition:         "1",
				Group:             "garage",
				Zone:              "12",
				Device:            "ri1",
				DeviceName:        "Garage door",
				Room:              "Garage",
				Kind:              "doorprotect",
				DeviceEventsLabel: "burglary,tamper,battery,connectivity",
				TroubleActive:     true,
				LastEventAt:       time.Unix(300, 0),
			},
		},
	})

	expected := `
# HELP ajax_zone_last_event_timestamp_seconds Unix timestamp for the last received event per zone/device.
# TYPE ajax_zone_last_event_timestamp_seconds gauge
ajax_zone_last_event_timestamp_seconds{account="0001",device="ri1",device_events="burglary,tamper,battery,connectivity",device_kind="doorprotect",device_name="Garage door",group="garage",partition="1",room="Garage",zone="12"} 300
# HELP ajax_zone_trouble_active Whether trouble is currently active for a zone/device.
# TYPE ajax_zone_trouble_active gauge
ajax_zone_trouble_active{account="0001",device="ri1",device_events="burglary,tamper,battery,connectivity",device_kind="doorprotect",device_name="Garage door",group="garage",partition="1",room="Garage",zone="12"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "ajax_zone_trouble_active", "ajax_zone_last_event_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestJeedomNumericMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)
	store := jeedom.NewStore("keep_last")
	store.Apply(jeedom.Event{CommandID: "56", DeviceName: "serverna", CommandName: "Puissance", Subtype: "numeric", Unit: "W", Value: []byte(`123.4`), ReceivedAt: time.Unix(500, 0)})
	m.SetJeedomStore(store)

	expected := `
# HELP ajax_jeedom_command_value Last numeric value reported by a Jeedom command.
# TYPE ajax_jeedom_command_value gauge
ajax_jeedom_command_value{command="Power",command_id="56",device="serverna",metric="power_w"} 123.4
# HELP ajax_jeedom_device_power_watts Last Jeedom power value per device.
# TYPE ajax_jeedom_device_power_watts gauge
ajax_jeedom_device_power_watts{device="serverna"} 123.4
# HELP ajax_jeedom_last_update_timestamp_seconds Unix timestamp for the last Jeedom command update.
# TYPE ajax_jeedom_last_update_timestamp_seconds gauge
ajax_jeedom_last_update_timestamp_seconds{command="Power",command_id="56",device="serverna",metric="power_w"} 500
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "ajax_jeedom_command_value", "ajax_jeedom_device_power_watts", "ajax_jeedom_last_update_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestGoSchedulerMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	New(registry)

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool, len(families))
	for _, family := range families {
		found[family.GetName()] = true
	}
	for _, name := range []string{
		"go_sched_goroutines_created_goroutines_total",
		"go_sched_goroutines_not_in_go_goroutines",
		"go_sched_goroutines_runnable_goroutines",
		"go_sched_goroutines_running_goroutines",
		"go_sched_goroutines_waiting_goroutines",
		"go_sched_threads_total_threads",
	} {
		if !found[name] {
			t.Errorf("runtime metric %q was not registered", name)
		}
	}
}
