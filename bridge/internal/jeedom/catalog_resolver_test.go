package jeedom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
)

func TestCatalogResolverLinksByCommandID(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "8",
		Name:             "Живлення сервера",
		Room:             "Котельна",
		Kind:             "WallSwitch",
		JeedomCommandIDs: []string{"55", "56", "57"},
	})

	resolver := NewCatalogResolver(catalog, CatalogResolverConfig{})
	identity := resolver.Resolve(Event{CommandID: "56", DeviceName: "Серверна", CommandName: "Puissance"}, MappingFor(Event{CommandName: "Puissance"}))

	if identity.LinkedZone != "8" {
		t.Fatalf("LinkedZone = %q, want 8", identity.LinkedZone)
	}
	if len(identity.HAIdentifiers) != 1 || identity.HAIdentifiers[0] != "ajaxbridge_A0F80D_zone_8" {
		t.Fatalf("HAIdentifiers = %#v", identity.HAIdentifiers)
	}
	if identity.DeviceSlug != "sia_a0f80d_zone_8" {
		t.Fatalf("DeviceSlug = %q", identity.DeviceSlug)
	}
}

func TestCatalogResolverDisablesUnlinkedDiscoveryByDefault(t *testing.T) {
	resolver := NewCatalogResolver(devicecatalog.Empty(), CatalogResolverConfig{})
	identity := resolver.Resolve(Event{CommandID: "999", DeviceName: "Unknown", CommandName: "Voltage"}, MappingFor(Event{CommandName: "Voltage"}))

	if !identity.DiscoveryDisabled {
		t.Fatal("expected unlinked discovery disabled")
	}
}

func TestCatalogResolverRejectsAmbiguousAliasesAndKeepsCommandOwners(t *testing.T) {
	devices := []devicecatalog.Device{
		{Account: "A0F80D", Zone: "2", Name: "Diana", Kind: "SpaceControl", JeedomNames: []string{"Будинок Діана", "Shared remote"}, JeedomCommandIDs: []string{"102"}},
		{Account: "A0F80D", Zone: "20", Name: "Diana", Kind: "SpaceControl", JeedomNames: []string{"Будинок Діана", "Shared-remote"}, JeedomCommandIDs: []string{"120"}},
		{Account: "A0F80D", Zone: "21", Name: "Diana", Kind: "SpaceControl", JeedomNames: []string{"Будинок Діана"}, JeedomCommandIDs: []string{"121"}},
		{Account: "A0F80D", Zone: "501", Name: "Diana", Kind: "App", JeedomNames: []string{"Будинок Діана"}, JeedomCommandIDs: []string{"501"}},
		{Account: "A0F80D", Zone: "502", Name: "Diana", Kind: "App", JeedomNames: []string{"Будинок Діана"}, JeedomCommandIDs: []string{"502"}},
	}
	for _, reverse := range []bool{false, true} {
		ordered := append([]devicecatalog.Device(nil), devices...)
		if reverse {
			for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
		for _, discoverUnlinked := range []bool{false, true} {
			resolver := NewCatalogResolver(testCatalog(t, ordered...), CatalogResolverConfig{
				AccountNames: []string{"Diana"}, DiscoverUnlinked: discoverUnlinked,
			})
			for _, name := range []string{"Diana", "Будинок Діана", "Shared remote", "Shared-remote"} {
				identities := []DeviceIdentity{
					resolver.Resolve(Event{CommandID: "unknown", DeviceName: name, CommandName: "Etat"}, Mapping{}),
					resolver.ResolveDiscovery(Discovery{Name: name, DeviceType: "Hub"}),
				}
				for _, identity := range identities {
					if identity.DeviceSlug != "" || identity.LinkedZone != "" || identity.DiscoveryDisabled == discoverUnlinked {
						t.Fatalf("ambiguous alias %q reverse=%v discoverUnlinked=%v resolved to %#v", name, reverse, discoverUnlinked, identity)
					}
				}
			}
			for _, device := range devices {
				commandID := device.JeedomCommandIDs[0]
				identities := []DeviceIdentity{
					resolver.Resolve(Event{CommandID: commandID, DeviceName: "Diana"}, Mapping{}),
					resolver.ResolveDiscovery(Discovery{Name: "Diana", InfoCommands: map[string]DiscoveryCommand{
						commandID: {CommandID: commandID},
					}}),
					resolver.ResolveDiscovery(Discovery{Name: "Diana", Actions: map[string]DiscoveryCommand{
						commandID: {CommandID: commandID},
					}}),
				}
				for _, identity := range identities {
					if identity.LinkedZone != device.Zone || identity.DeviceSlug != "sia_a0f80d_zone_"+device.Zone || len(identity.HAIdentifiers) != 1 || identity.HAIdentifiers[0] != "ajaxbridge_A0F80D_zone_"+device.Zone {
						t.Fatalf("command %q reverse=%v lost canonical owner: %#v", commandID, reverse, identity)
					}
				}
			}
		}
	}
}

func TestCatalogResolverKeepsUniqueAliasesAndExplicitCommandPriority(t *testing.T) {
	resolver := NewCatalogResolver(testCatalog(t,
		devicecatalog.Device{Account: "A0F80D", Zone: "2", Name: "Remote one", JeedomNames: []string{"Unique remote", "Unique-remote", "Remote one"}, JeedomCommandIDs: []string{"102"}},
		devicecatalog.Device{Account: "A0F80D", Zone: "20", Name: "Remote two", JeedomCommandIDs: []string{"120"}},
	), CatalogResolverConfig{})
	for _, name := range []string{"Remote one", "Unique remote", "Unique-remote"} {
		if identity := resolver.Resolve(Event{DeviceName: name}, Mapping{}); identity.LinkedZone != "2" {
			t.Fatalf("unique alias %q lost owner: %#v", name, identity)
		}
		if identity := resolver.ResolveDiscovery(Discovery{Name: name}); identity.LinkedZone != "2" {
			t.Fatalf("unique discovery alias %q lost owner: %#v", name, identity)
		}
		if identity := resolver.Resolve(Event{CommandID: "120", DeviceName: name}, Mapping{}); identity.LinkedZone != "20" {
			t.Fatalf("name %q overrode explicit command owner: %#v", name, identity)
		}
	}
}

func TestCatalogResolverPreservesSameNameZonesAcrossRestart(t *testing.T) {
	var devices []devicecatalog.Device
	for _, zone := range []string{"2", "20", "21", "501", "502"} {
		kind := "SpaceControl"
		if zone == "501" || zone == "502" {
			kind = "App"
		}
		devices = append(devices, devicecatalog.Device{Account: "A0F80D", Zone: zone, Name: "Diana", Kind: kind, JeedomCommandIDs: []string{"command_" + zone}})
	}
	resolver := NewCatalogResolver(testCatalog(t, devices...), CatalogResolverConfig{})
	path := filepath.Join(t.TempDir(), "jeedom.json")
	store, err := LoadStore(t.Context(), path, "keep_last", resolver)
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		for _, expected := range devices {
			commandID := expected.JeedomCommandIDs[0]
			store.ApplyDiscovery(Discovery{
				EqLogicID: expected.Zone, Name: "Diana", DeviceType: expected.Kind,
				InfoCommands: map[string]DiscoveryCommand{
					commandID: {CommandID: commandID, Name: "Firmware version", LogicalID: "firmwareVersion", Type: "info", Subtype: "string", Value: json.RawMessage(`"1.0"`)},
				},
			})
		}
		if err := store.Save(t.Context()); err != nil {
			t.Fatal(err)
		}
		store, err = LoadStore(t.Context(), path, "keep_last", resolver)
		if err != nil {
			t.Fatal(err)
		}
		store.ReconcileResolver(resolver)
		if got := len(store.Devices()); got != len(devices) {
			t.Fatalf("cycle %d device count = %d, want %d", cycle, got, len(devices))
		}
		for _, expected := range devices {
			device, ok := store.Device("sia_a0f80d_zone_" + expected.Zone)
			if !ok || device.LinkedZone != expected.Zone || len(device.RawCommands) != 1 || len(device.HAIdentifiers) != 1 || device.HAIdentifiers[0] != "ajaxbridge_A0F80D_zone_"+expected.Zone {
				t.Fatalf("cycle %d zone %s merged: %#v", cycle, expected.Zone, device)
			}
			if command, ok := device.RawCommands[expected.JeedomCommandIDs[0]]; !ok || command.DeviceSlug != device.DeviceSlug {
				t.Fatalf("cycle %d wrong command owner in zone %s: %#v", cycle, expected.Zone, command)
			}
		}
	}
}

func TestCatalogResolverLinksConfiguredAccountName(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{Account: "A0F80D", Zone: "1", Name: "Relay"})
	resolver := NewCatalogResolver(catalog, CatalogResolverConfig{AccountNames: []string{"Будинок"}})
	identity := resolver.Resolve(Event{CommandID: "1", DeviceName: "Будинок", CommandName: "Etat"}, MappingFor(Event{CommandName: "Etat"}))

	if identity.LinkedAccount != "A0F80D" || identity.LinkedZone != "" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(identity.HAIdentifiers) != 1 || identity.HAIdentifiers[0] != "ajaxbridge_account_A0F80D" {
		t.Fatalf("HAIdentifiers = %#v", identity.HAIdentifiers)
	}
}

func TestCatalogResolverLinksAccountCatalogRowByCommandID(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Name:             "Ajax hub",
		Kind:             "Hub",
		JeedomCommandIDs: []string{"163", "164", "165", "167"},
	})
	resolver := NewCatalogResolver(catalog, CatalogResolverConfig{})

	identity := resolver.Resolve(Event{CommandID: "163", DeviceName: "Ajax hub", CommandName: "Armement"}, MappingFor(Event{CommandName: "Armement"}))

	if identity.LinkedAccount != "A0F80D" || identity.LinkedZone != "" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.DeviceSlug != "account_a0f80d" {
		t.Fatalf("DeviceSlug = %q, want account_a0f80d", identity.DeviceSlug)
	}
	if len(identity.HAIdentifiers) != 1 || identity.HAIdentifiers[0] != "ajaxbridge_account_A0F80D" {
		t.Fatalf("HAIdentifiers = %#v", identity.HAIdentifiers)
	}
}

func TestCatalogResolverLinksHubDiscoveryToSingleAccount(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{Account: "A0F80D", Zone: "1", Name: "Relay"})
	resolver := NewCatalogResolver(catalog, CatalogResolverConfig{})

	identity := resolver.ResolveDiscovery(Discovery{
		Name:       "Ajax hub",
		DeviceType: "HUB_2_PLUS",
	})

	if identity.LinkedAccount != "A0F80D" || identity.LinkedZone != "" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.DeviceSlug != "account_a0f80d" {
		t.Fatalf("DeviceSlug = %q, want account_a0f80d", identity.DeviceSlug)
	}
	if len(identity.HAIdentifiers) != 1 || identity.HAIdentifiers[0] != "ajaxbridge_account_A0F80D" {
		t.Fatalf("HAIdentifiers = %#v", identity.HAIdentifiers)
	}
}

func TestStoreReconcileResolverRelinksExistingDiscoveryDevices(t *testing.T) {
	store := NewStore("keep_last")
	discovery, err := ParseDiscoveryMessage("jeedom/discovery/eqLogic/42", []byte(`{
	  "id":42,
	  "name":"Хатинка Двер.",
	  "configuration":{"device":"DoorProtectPlus"},
	  "isVisible":1,
	  "isEnable":1,
	  "cmds":{
	    "322":{"id":322,"name":"Etat","type":"info","subType":"string","isVisible":1},
	    "328":{"id":328,"name":"Température","type":"info","subType":"numeric","unite":"°C","isVisible":1,"currentValue":21},
	    "331":{"id":331,"logicalId":"SWITCH_ON","name":"On","type":"action","subType":"other","isVisible":1}
	  }
	}`), time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	initial := store.ApplyDiscovery(discovery)
	if initial.Device.DeviceSlug != "khatynka_dver" {
		t.Fatalf("initial slug = %q, want khatynka_dver", initial.Device.DeviceSlug)
	}

	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "18",
		Name:             "Хатинка Двері",
		Room:             "Хатинка",
		Kind:             "DoorProtectPlus",
		JeedomCommandIDs: []string{"322", "328", "331"},
	})
	devices := store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	if _, ok := store.Device("khatynka_dver"); ok {
		t.Fatal("legacy local Jeedom device still exists after reconciliation")
	}
	device, ok := store.Device("sia_a0f80d_zone_18")
	if !ok {
		t.Fatal("missing relinked SIA device")
	}
	if device.LinkedZone != "18" || device.HAModel != "DoorProtectPlus" {
		t.Fatalf("relinked device = %#v", device)
	}
	if command := device.RawCommands["328"]; command.DeviceSlug != "sia_a0f80d_zone_18" {
		t.Fatalf("command was not moved to SIA device: %#v", command)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %#v, want exactly one relinked device", devices)
	}
}

func testCatalog(t *testing.T, devices ...devicecatalog.Device) *devicecatalog.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devices.json")
	body, err := json.Marshal(devices)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := devicecatalog.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
