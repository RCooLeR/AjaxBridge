package devicecatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/event"
)

func TestLoadAndLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	data := []byte(`[
  {
    "account": "0001",
    "zone": "3",
    "device": "ri0",
    "name": "Kitchen transmitter",
    "room": "Kitchen",
    "kind": "ajax_transmitter",
    "events": ["alarm", "tamper"]
  }
]`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	device, ok := catalog.Lookup("0001", "3", "ri0")
	if !ok {
		t.Fatal("expected exact device match")
	}
	if device.Name != "Kitchen transmitter" || device.Room != "Kitchen" || device.Kind != "ajax_transmitter" {
		t.Fatalf("unexpected device: %#v", device)
	}
	device, ok = catalog.Lookup("0001", "3", "other")
	if !ok {
		t.Fatal("expected zone fallback match")
	}
	if device.Name != "Kitchen transmitter" {
		t.Fatalf("fallback device = %#v", device)
	}
}

func TestReplaceDropsLegacyRolesField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte(`[{"account":"0001","zone":"3","name":"Power input","room":"Utility","kind":"Transmitter","events":[],"roles":["grid_power_detector"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Replace(context.Background(), catalog.Devices()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"roles"`)) {
		t.Fatalf("legacy roles field survived catalog save: %s", data)
	}
}

func TestUpsertFromEventCreatesMissingFileAndDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	catalog, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := catalog.UpsertFromEvent(context.Background(), event.Normalized{
		Account:     "0001",
		Zone:        "7",
		EventCode:   "TA",
		EventClass:  event.ClassTamper,
		EventName:   "Tamper alarm",
		Signal:      "tamper",
		Source:      "sensor",
		ReceivedAt:  time.Unix(100, 0).UTC(),
		ParseStatus: event.ParseStatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatalf("expected created result, got %#v", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var devices []Device
	if err := json.Unmarshal(data, &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices length = %d", len(devices))
	}
	device := devices[0]
	if device.Account != "0001" || device.Zone != "7" || device.Device != "" {
		t.Fatalf("unexpected device identity: %#v", device)
	}
	if device.Name != "Zone 7" || device.Room != "unknown" || device.Kind != "ajax_sensor" {
		t.Fatalf("unexpected default labels: %#v", device)
	}
	if len(device.Events) != 1 || device.Events[0] != "tamper" {
		t.Fatalf("events = %#v", device.Events)
	}
	if !device.AutoDiscovered {
		t.Fatal("expected auto_discovered=true")
	}
}

func TestUpsertFromEventUpdatesExistingDeviceEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte(`[{"account":"0001","zone":"7","device":"ri1","name":"Kitchen transmitter","room":"Kitchen","kind":"ajax_transmitter","events":["tamper"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := catalog.UpsertFromEvent(context.Background(), event.Normalized{
		Account:     "0001",
		Zone:        "7",
		Device:      "ri1",
		EventCode:   "BA",
		EventClass:  event.ClassAlarm,
		EventName:   "Burglary alarm",
		Signal:      "burglary",
		Source:      "sensor",
		ReceivedAt:  time.Unix(200, 0).UTC(),
		ParseStatus: event.ParseStatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatalf("expected updated result, got %#v", result)
	}

	device, ok := catalog.Lookup("0001", "7", "ri1")
	if !ok {
		t.Fatal("expected device lookup")
	}
	if !contains(device.Events, "tamper") || !contains(device.Events, "burglary") {
		t.Fatalf("events = %#v", device.Events)
	}
	if device.Name != "Kitchen transmitter" || device.Room != "Kitchen" || device.Kind != "ajax_transmitter" {
		t.Fatalf("manual labels should be preserved: %#v", device)
	}
}

func TestUpsertFromEventPersistsLastSeenAndLastEventFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte(`[{"account":"0001","zone":"7","name":"Kitchen transmitter","room":"Kitchen","kind":"ajax_transmitter","events":["tamper"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	receivedAt := time.Date(2026, 4, 21, 10, 11, 12, 0, time.UTC)
	result, err := catalog.UpsertFromEvent(context.Background(), event.Normalized{
		Account:     "0001",
		Zone:        "7",
		EventCode:   "TU",
		EventClass:  event.ClassRestore,
		EventName:   "Tamper bypass restored or reactivated",
		Signal:      "tamper_bypass",
		Source:      "sensor",
		ReceivedAt:  receivedAt,
		ParseStatus: event.ParseStatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatalf("expected updated result, got %#v", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var devices []Device
	if err := json.Unmarshal(data, &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices length = %d", len(devices))
	}
	device := devices[0]
	if !device.LastSeenAt.Equal(receivedAt) {
		t.Fatalf("last_seen_at = %s, want %s", device.LastSeenAt, receivedAt)
	}
	if device.LastEventCode != "TU" || device.LastEventName != "Tamper bypass restored or reactivated" || device.LastSignal != "tamper_bypass" {
		t.Fatalf("unexpected persisted last event fields: %#v", device)
	}
}
