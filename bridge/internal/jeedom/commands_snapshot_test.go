package jeedom

import (
	"maps"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
)

func TestCommandsListsSyntheticObservationsBeforeAndAfterCacheRestore(t *testing.T) {
	catalog := testCatalog(t,
		devicecatalog.Device{Account: "A0F80D", Zone: "1", Name: "Gate", Kind: "Transmitter", JeedomCommandIDs: []string{"100"}},
		devicecatalog.Device{Account: "A0F80D", Zone: "2", Name: "Power", Kind: "Transmitter", Roles: []string{gridPowerDetectorRole}, JeedomCommandIDs: []string{"200"}},
		devicecatalog.Device{Account: "A0F80D", Zone: "3", Name: "Wicket", Kind: "Transmitter", JeedomCommandIDs: []string{"300"}},
	)
	resolver := NewCatalogResolver(catalog, CatalogResolverConfig{})
	store := NewStoreWithResolver("keep_last", resolver)
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
		if len(commands) != 4 {
			t.Fatalf("command listing has %d entries, want 3 physical and 1 explicitly configured synthetic: %#v", len(commands), commands)
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
		if _, ok := synthetic["sia_a0f80d_zone_2"]; !ok {
			t.Errorf("synthetic grid_power is missing for explicitly configured detector: %#v", synthetic)
		}
		for _, slug := range []string{"sia_a0f80d_zone_1", "sia_a0f80d_zone_3"} {
			if _, ok := synthetic[slug]; ok {
				t.Errorf("ordinary Transmitter %s exposed synthetic grid_power", slug)
			}
		}
		if !maps.Equal(physical, map[string]string{
			"100": "sia_a0f80d_zone_1",
			"200": "sia_a0f80d_zone_2",
			"300": "sia_a0f80d_zone_3",
		}) {
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
	restored, err := LoadStore(t.Context(), path, "keep_last", resolver)
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
