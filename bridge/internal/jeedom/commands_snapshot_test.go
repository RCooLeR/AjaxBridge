package jeedom

import (
	"maps"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCommandsListsSyntheticObservationsBeforeAndAfterCacheRestore(t *testing.T) {
	store := NewStore("keep_last")
	at := time.Unix(1700000000, 0).UTC()
	for _, test := range []struct{ id, name string }{{"100", "Gate"}, {"200", "Power"}, {"300", "Wicket"}} {
		store.ApplyDiscovery(Discovery{
			EqLogicID: test.id, Name: test.name, DeviceType: "Transmitter", ReceivedAt: at,
			InfoCommands: map[string]DiscoveryCommand{
				test.id: {CommandID: test.id, Name: "Event code", Type: "info", Subtype: "string", Value: []byte(`"M_11_40"`)},
			},
		})
	}
	check := func(t *testing.T, store *Store) {
		t.Helper()
		indexBefore := maps.Clone(store.commands)
		commands := store.Commands()
		if len(commands) != 6 {
			t.Fatalf("command listing has %d entries, want 3 physical and 3 synthetic: %#v", len(commands), commands)
		}
		physical := make(map[string]string)
		synthetic := make(map[string]Command)
		for _, command := range commands {
			if command.CommandID == "" {
				if command.Metric != "grid_power" || command.Value != true || !command.LastValueAt.Equal(at) {
					t.Fatalf("synthetic observation changed: %#v", command)
				}
				synthetic[command.DeviceSlug] = command
			} else {
				if _, exists := physical[command.CommandID]; exists {
					t.Fatalf("duplicate physical command ID %s", command.CommandID)
				}
				physical[command.CommandID] = command.DeviceSlug
			}
		}
		for _, slug := range []string{"gate", "power", "wicket"} {
			if _, ok := synthetic[slug]; !ok {
				t.Errorf("synthetic grid_power is missing for %s", slug)
			}
		}
		if !maps.Equal(physical, map[string]string{"100": "gate", "200": "power", "300": "wicket"}) {
			t.Fatalf("physical command ownership changed: %#v", physical)
		}
		if !maps.Equal(indexBefore, store.commands) {
			t.Fatal("listing modified the command lookup index")
		}
		for range 10 {
			if !reflect.DeepEqual(commands, store.Commands()) {
				t.Fatal("listing order is not deterministic")
			}
		}
	}
	check(t, store)
	path := filepath.Join(t.TempDir(), "jeedom.json")
	store.SetPath(path)
	if err := store.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadStore(t.Context(), path, "keep_last", nil)
	if err != nil {
		t.Fatal(err)
	}
	check(t, restored)
}

func TestCommandsKeepsIndexedPhysicalOwnerWhenIncludingSyntheticObservations(t *testing.T) {
	store := NewStore("keep_last")
	for _, slug := range []string{"old", "current"} {
		store.devices[slug] = &Device{
			DeviceSlug: slug,
			RawCommands: map[string]Command{
				"55":         {CommandID: "55", DeviceSlug: slug, Metric: "event_code", Value: "M_11_40"},
				"grid_power": {DeviceSlug: slug, Metric: "grid_power", Value: true},
			},
		}
	}
	store.commands["55"] = "current"
	store.commands["grid_power"] = "old"
	commands := store.Commands()
	if len(commands) != 3 {
		t.Fatalf("listing = %#v, want one physical owner and two synthetic observations", commands)
	}
	if commands[2].CommandID != "55" || commands[2].DeviceSlug != "current" {
		t.Fatalf("physical command owner = %#v, want current", commands[2])
	}
	if commands[0].DeviceSlug != "current" || commands[1].DeviceSlug != "old" || commands[0].CommandID != "" || commands[1].CommandID != "" {
		t.Fatalf("synthetic observations = %#v", commands[:2])
	}
}
