package jeedom

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
)

func TestStoreReconcileResolverSplitsMixedCachedCommandOwners(t *testing.T) {
	zone2IssueAt := time.Unix(1_700_000_100, 0).UTC()
	zone2FirmwareAt := time.Unix(1_700_000_200, 0).UTC()
	store := NewStore("keep_last")
	store.devices["sia_a0f80d_zone_20"] = &Device{
		Source:           Source,
		Device:           "Діана",
		DeviceSlug:       "sia_a0f80d_zone_20",
		JeedomDeviceType: "SpaceControl",
		LinkedSource:     "sia",
		LinkedAccount:    "A0F80D",
		LinkedZone:       "20",
		Values: map[string]any{
			"state":            false,
			"battery_percent":  float64(95),
			"issue_count":      float64(2),
			"firmware_version": "1.0",
		},
		RawCommands: map[string]Command{
			"276": cachedOwnershipCommand("276", "20", "state", false, time.Unix(1_700_000_000, 0).UTC()),
			"369": cachedOwnershipCommand("369", "20", "issue_count", float64(2), zone2IssueAt),
			"370": cachedOwnershipCommand("370", "20", "firmware_version", "1.0", zone2FirmwareAt),
			"374": cachedOwnershipCommand("374", "20", "battery_percent", float64(95), time.Unix(1_700_000_300, 0).UTC()),
			"375": cachedOwnershipCommand("375", "20", "issue_count", float64(0), time.Unix(1_700_000_400, 0).UTC()),
			"376": cachedOwnershipCommand("376", "20", "firmware_version", "1.1", time.Unix(1_700_000_500, 0).UTC()),
		},
		Actions:    make(map[string]Action),
		LastUpdate: time.Unix(1_700_000_500, 0).UTC(),
	}
	store.rebuildIndexesLocked()

	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"168", "169", "170", "368", "369", "370"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276", "277", "278", "374", "375", "376"},
		},
	)
	reconciledDevices := store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))
	zone20PublishIndex, zone2PublishIndex := -1, -1
	for index, device := range reconciledDevices {
		switch device.DeviceSlug {
		case "sia_a0f80d_zone_20":
			zone20PublishIndex = index
		case "sia_a0f80d_zone_2":
			zone2PublishIndex = index
		}
	}
	if zone20PublishIndex == -1 || zone2PublishIndex == -1 || zone20PublishIndex >= zone2PublishIndex {
		t.Fatalf("reconcile publish order = %#v, want old zone 20 owner before new zone 2 owner", reconciledDevices)
	}

	wantOwner := map[string]string{
		"276": "sia_a0f80d_zone_20",
		"369": "sia_a0f80d_zone_2",
		"370": "sia_a0f80d_zone_2",
		"374": "sia_a0f80d_zone_20",
		"375": "sia_a0f80d_zone_20",
		"376": "sia_a0f80d_zone_20",
	}
	assertUniqueRawCommandOwners(t, store.Devices(), wantOwner)

	indexed := make(map[string]Command)
	for _, command := range store.Commands() {
		if _, duplicate := indexed[command.CommandID]; duplicate {
			t.Fatalf("Commands index returned command %s more than once", command.CommandID)
		}
		indexed[command.CommandID] = command
	}
	if len(indexed) != len(wantOwner) {
		t.Fatalf("Commands index length = %d, want %d: %#v", len(indexed), len(wantOwner), indexed)
	}
	for commandID, owner := range wantOwner {
		command, ok := indexed[commandID]
		if !ok {
			t.Fatalf("Commands index is missing command %s", commandID)
		}
		if command.DeviceSlug != owner {
			t.Fatalf("Commands index owner for %s = %q, want %q", commandID, command.DeviceSlug, owner)
		}
	}

	zone2, ok := store.Device("sia_a0f80d_zone_2")
	if !ok {
		t.Fatal("missing zone 2 device after per-command reconciliation")
	}
	if zone2.Device != "Діана" || zone2.LinkedZone != "2" || zone2.JeedomDeviceType != "SpaceControl" {
		t.Fatalf("zone 2 identity = %#v, want the catalog SpaceControl identity", zone2)
	}
	issue := zone2.RawCommands["369"]
	if issue.Value != float64(2) || !issue.LastValueAt.Equal(zone2IssueAt) {
		t.Fatalf("transferred issue command lost its value history: %#v", issue)
	}
	firmware := zone2.RawCommands["370"]
	if firmware.Value != "1.0" || !firmware.LastValueAt.Equal(zone2FirmwareAt) {
		t.Fatalf("transferred firmware command lost its value history: %#v", firmware)
	}
	if got := zone2.Values["issue_count"]; got != float64(2) {
		t.Fatalf("zone 2 issue_count = %#v, want 2", got)
	}
	if got := zone2.Values["firmware_version"]; got != "1.0" {
		t.Fatalf("zone 2 firmware_version = %#v, want 1.0", got)
	}
	if !zone2.LastUpdate.Equal(zone2FirmwareAt) {
		t.Fatalf("zone 2 LastUpdate = %s, want latest moved command time %s", zone2.LastUpdate, zone2FirmwareAt)
	}
	zone20, ok := store.Device("sia_a0f80d_zone_20")
	if !ok {
		t.Fatal("missing zone 20 after per-command reconciliation")
	}
	if got := zone20.Values["issue_count"]; got != float64(0) {
		t.Fatalf("zone 20 issue_count = %#v, want remaining command 375 value 0", got)
	}
	if got := zone20.Values["firmware_version"]; got != "1.1" {
		t.Fatalf("zone 20 firmware_version = %#v, want remaining command 376 value 1.1", got)
	}
	if want := time.Unix(1_700_000_500, 0).UTC(); !zone20.LastUpdate.Equal(want) {
		t.Fatalf("zone 20 LastUpdate = %s, want %s", zone20.LastUpdate, want)
	}
}

func TestStoreReconcileResolverAdvancesMetadataOnlyCatalogRevision(t *testing.T) {
	const slug = "sia_a0f80d_zone_2"
	store := NewStore("keep_last")
	store.devices[slug] = &Device{
		Source:           Source,
		Device:           "Old remote name",
		DeviceSlug:       slug,
		JeedomDeviceType: "SpaceControl",
		LinkedSource:     "sia",
		LinkedAccount:    "A0F80D",
		LinkedZone:       "2",
		Values:           map[string]any{"issue_count": float64(0)},
		RawCommands: map[string]Command{
			"369": cachedOwnershipCommand("369", "2", "issue_count", float64(0), time.Unix(1_700_000_100, 0).UTC()),
		},
		Actions:         make(map[string]Action),
		publishRevision: 41,
	}
	store.rebuildIndexesLocked()
	stale, ok := store.Device(slug)
	if !ok {
		t.Fatal("missing pre-reconciliation device")
	}

	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "2",
		Name:             "Updated remote name",
		Kind:             "SpaceControl",
		JeedomCommandIDs: []string{"369"},
	})
	reconciled := store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))
	if len(reconciled) != 1 {
		t.Fatalf("reconciled devices = %d, want 1: %#v", len(reconciled), reconciled)
	}
	if reconciled[0].Device != "Updated remote name" {
		t.Fatalf("reconciled device name = %q, want updated catalog name", reconciled[0].Device)
	}
	if reconciled[0].publishRevision <= 41 {
		t.Fatalf("reconciled publish revision = %d, want newer than the prior catalog generation", reconciled[0].publishRevision)
	}
	publisher := NewPublisher(PublisherConfig{}, fakeMQTT{})
	if published, err := publisher.PublishDeviceWithResult(context.Background(), reconciled[0]); err != nil || !published {
		t.Fatalf("fresh reconciled publish = (%v, %v), want (true, nil)", published, err)
	}
	if published, err := publisher.PublishDeviceWithResult(context.Background(), stale); err != nil || published {
		t.Fatalf("stale pre-catalog publish = (%v, %v), want (false, nil)", published, err)
	}
}

func TestStoreReconcileDoesNotKeepRecreatedCanonicalDeviceAsLegacyAlias(t *testing.T) {
	store := NewStore("keep_last")
	store.devices["sia_a0f80d_zone_2"] = &Device{
		Source:        Source,
		Device:        "Діана",
		DeviceSlug:    "sia_a0f80d_zone_2",
		LinkedSource:  "sia",
		LinkedAccount: "A0F80D",
		LinkedZone:    "2",
		Values: map[string]any{
			"state":       false,
			"issue_count": float64(2),
		},
		RawCommands: map[string]Command{
			"276": cachedOwnershipCommand("276", "20", "state", false, time.Unix(1_700_000_600, 0).UTC()),
			"369": cachedOwnershipCommand("369", "20", "issue_count", float64(2), time.Unix(1_700_000_700, 0).UTC()),
		},
		Actions: make(map[string]Action),
	}
	store.rebuildIndexesLocked()
	catalog := testCatalog(t,
		devicecatalog.Device{Account: "A0F80D", Zone: "2", Name: "Діана", Kind: "SpaceControl", JeedomCommandIDs: []string{"369"}},
		devicecatalog.Device{Account: "A0F80D", Zone: "20", Name: "Діана", Kind: "SpaceControl", JeedomCommandIDs: []string{"276"}},
	)

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	zone20, ok := store.Device("sia_a0f80d_zone_20")
	if !ok {
		t.Fatal("missing zone 20 after whole-device reconciliation")
	}
	if containsString(zone20.LegacyDeviceSlugs, "sia_a0f80d_zone_2") {
		t.Fatalf("zone 20 retained recreated canonical zone 2 as legacy alias: %#v", zone20.LegacyDeviceSlugs)
	}
	assertUniqueRawCommandOwners(t, store.Devices(), map[string]string{
		"276": "sia_a0f80d_zone_20",
		"369": "sia_a0f80d_zone_2",
	})
}

func TestStoreApplyDiscoveryTransfersCommandWithoutCleanup(t *testing.T) {
	valueAt := time.Unix(1_700_001_000, 0).UTC()
	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"168", "169", "170", "368", "369", "370"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276", "277", "278", "374", "375", "376"},
		},
	)
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	stale := cachedOwnershipCommand("369", "20", "issue_count", float64(4), valueAt)
	store.devices["sia_a0f80d_zone_20"] = &Device{
		Source:        Source,
		Device:        "Діана",
		DeviceSlug:    "sia_a0f80d_zone_20",
		LinkedSource:  "sia",
		LinkedAccount: "A0F80D",
		LinkedZone:    "20",
		JeedomID:      "20",
		Values:        map[string]any{"issue_count": float64(4)},
		RawCommands:   map[string]Command{"369": stale},
		Actions:       make(map[string]Action),
		PendingDiscoveryCleanups: []Command{
			stale,
		},
	}
	store.rebuildIndexesLocked()

	result := store.ApplyDiscovery(Discovery{
		EqLogicID:  "2",
		Name:       "Діана",
		DeviceType: "SpaceControl",
		Enabled:    true,
		Visible:    true,
		InfoCommands: map[string]DiscoveryCommand{
			"369": {
				CommandID: "369",
				EqLogicID: "2",
				Name:      "Nombre de defauts",
				Type:      "info",
				Subtype:   "numeric",
			},
		},
		Actions:    make(map[string]DiscoveryCommand),
		ReceivedAt: time.Unix(1_700_002_000, 0).UTC(),
	})

	if result.Device.DeviceSlug != "sia_a0f80d_zone_2" {
		t.Fatalf("discovery owner = %q, want zone 2", result.Device.DeviceSlug)
	}
	command := result.Device.RawCommands["369"]
	if command.Value != float64(4) || !command.LastValueAt.Equal(valueAt) {
		t.Fatalf("transferred command lost its value history: %#v", command)
	}
	if len(result.PreviousDevices) != 1 {
		t.Fatalf("PreviousDevices = %#v, want one previous owner", result.PreviousDevices)
	}
	previous := result.PreviousDevices[0]
	if previous.DeviceSlug != "sia_a0f80d_zone_20" {
		t.Fatalf("previous owner = %q, want zone 20", previous.DeviceSlug)
	}
	if _, ok := previous.RawCommands["369"]; ok {
		t.Fatalf("previous owner still contains transferred command: %#v", previous.RawCommands)
	}
	if len(previous.PendingDiscoveryCleanups) != 0 {
		t.Fatalf("previous owner retained a cleanup for the active command: %#v", previous.PendingDiscoveryCleanups)
	}
	if len(result.RemovedCommands) != 0 {
		t.Fatalf("ownership transfer emitted discovery cleanup removals: %#v", result.RemovedCommands)
	}
	for _, device := range store.Devices() {
		for _, pending := range device.PendingDiscoveryCleanups {
			if pending.CommandID == "369" {
				t.Fatalf("ownership transfer queued cleanup for active command 369 on %s", device.DeviceSlug)
			}
		}
	}
	assertUniqueRawCommandOwners(t, store.Devices(), map[string]string{"369": "sia_a0f80d_zone_2"})
}

func TestStoreApplyDiscoveryKeepsMixedEqLogicCommandsSplitAndRemovesGlobally(t *testing.T) {
	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"369", "370"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276", "374", "375", "376"},
		},
	)
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	discovery := Discovery{
		EqLogicID:  "35",
		Name:       "Діана",
		DeviceType: "SpaceControl",
		Enabled:    true,
		Visible:    true,
		InfoCommands: map[string]DiscoveryCommand{
			"276": {CommandID: "276", EqLogicID: "35", LogicalID: "eventCode", Name: "Code evenement", Type: "info", Subtype: "string", Value: []byte(`"M_11_40"`)},
			"369": {CommandID: "369", EqLogicID: "35", LogicalID: "issuesCount", Name: "Nombre de defauts", Type: "info", Subtype: "numeric", Value: []byte(`2`)},
			"370": {CommandID: "370", EqLogicID: "35", LogicalID: "firmwareVersion", Name: "Version du firmware", Type: "info", Subtype: "string", Value: []byte(`"1.0"`)},
			"374": {CommandID: "374", EqLogicID: "35", LogicalID: "batteryChargeLevelPercentage", Name: "Batterie", Type: "info", Subtype: "numeric", Unit: "%", Value: []byte(`95`)},
			"375": {CommandID: "375", EqLogicID: "35", LogicalID: "issuesCount", Name: "Nombre de defauts", Type: "info", Subtype: "numeric", Value: []byte(`0`)},
			"376": {CommandID: "376", EqLogicID: "35", LogicalID: "firmwareVersion", Name: "Version du firmware", Type: "info", Subtype: "string", Value: []byte(`"1.1"`)},
		},
		Actions:    make(map[string]DiscoveryCommand),
		ReceivedAt: time.Unix(1_700_002_500, 0).UTC(),
	}

	result := store.ApplyDiscovery(discovery)
	assertUniqueRawCommandOwners(t, store.Devices(), map[string]string{
		"276": "sia_a0f80d_zone_20",
		"369": "sia_a0f80d_zone_2",
		"370": "sia_a0f80d_zone_2",
		"374": "sia_a0f80d_zone_20",
		"375": "sia_a0f80d_zone_20",
		"376": "sia_a0f80d_zone_20",
	})
	publishedSlugs := map[string]struct{}{result.Device.DeviceSlug: {}}
	for _, previous := range result.PreviousDevices {
		publishedSlugs[previous.DeviceSlug] = struct{}{}
	}
	for _, slug := range []string{"sia_a0f80d_zone_2", "sia_a0f80d_zone_20"} {
		if _, ok := publishedSlugs[slug]; !ok {
			t.Fatalf("ApplyDiscovery publish set = %#v, missing %q", publishedSlugs, slug)
		}
	}

	delete(discovery.InfoCommands, "369")
	discovery.ReceivedAt = discovery.ReceivedAt.Add(time.Minute)
	refreshed := store.ApplyDiscovery(discovery)
	zone2, ok := store.Device("sia_a0f80d_zone_2")
	if !ok {
		t.Fatal("missing zone 2 after mixed discovery refresh")
	}
	if _, exists := zone2.RawCommands["369"]; exists {
		t.Fatalf("globally omitted command 369 survived on split owner: %#v", zone2.RawCommands)
	}
	removed369 := false
	for _, command := range refreshed.RemovedCommands {
		removed369 = removed369 || command.CommandID == "369"
	}
	if !removed369 {
		t.Fatalf("removed commands = %#v, want 369", refreshed.RemovedCommands)
	}
	cleanupFound := false
	for _, current := range append([]Device{refreshed.Device}, refreshed.PreviousDevices...) {
		for _, cleanup := range current.PendingDiscoveryCleanups {
			cleanupFound = cleanupFound || cleanup.CommandID == "369"
		}
	}
	if !cleanupFound {
		t.Fatalf("removed command 369 cleanup missing from publish set: %#v", refreshed)
	}
}

func TestStoreApplyEmptyTransferSeedsRetainedValue(t *testing.T) {
	valueAt := time.Unix(1_700_003_000, 0).UTC()
	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"369"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276"},
		},
	)
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	stale := cachedOwnershipCommand("369", "20", "issue_count", float64(7), valueAt)
	store.devices["sia_a0f80d_zone_20"] = &Device{
		Source:           Source,
		Device:           "Діана",
		DeviceSlug:       "sia_a0f80d_zone_20",
		JeedomDeviceType: "SpaceControl",
		Values:           map[string]any{"issue_count": float64(7)},
		RawCommands:      map[string]Command{"369": stale},
		Actions:          make(map[string]Action),
	}
	store.rebuildIndexesLocked()

	result := store.Apply(Event{
		Topic:       "jeedom/cmd/event/369",
		CommandID:   "369",
		DeviceName:  "Діана",
		CommandName: "Nombre de defauts",
		Type:        "info",
		Subtype:     "numeric",
		Value:       json.RawMessage(`""`),
		ReceivedAt:  time.Unix(1_700_004_000, 0).UTC(),
	})

	if !result.EmptyValue || result.UpdatedValue {
		t.Fatalf("empty keep-last result = %#v", result)
	}
	if !result.StoreChanged {
		t.Fatal("ownership transfer was not marked for persistence")
	}
	if got := result.Device.Values["issue_count"]; got != float64(7) {
		t.Fatalf("transferred top-level value = %#v, want 7", got)
	}
	command := result.Device.RawCommands["369"]
	if command.Value != float64(7) || !command.LastValueAt.Equal(valueAt) {
		t.Fatalf("transferred command history = %#v", command)
	}
}

func TestStoreReconcileMergeKeepsNewestCommandAndTopLevelValue(t *testing.T) {
	newerAt := time.Unix(1_700_005_000, 0).UTC()
	olderAt := newerAt.Add(-time.Hour)
	store := NewStore("keep_last")
	store.devices["a_newer"] = ownershipTestDevice("a_newer", "369", float64(4), newerAt)
	store.devices["z_older"] = ownershipTestDevice("z_older", "369", float64(2), olderAt)
	store.rebuildIndexesLocked()
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "2",
		Name:             "Діана",
		Kind:             "SpaceControl",
		JeedomCommandIDs: []string{"369"},
	})

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	device, ok := store.Device("sia_a0f80d_zone_2")
	if !ok {
		t.Fatal("missing reconciled zone 2 device")
	}
	command := device.RawCommands["369"]
	if command.Value != float64(4) || !command.LastValueAt.Equal(newerAt) {
		t.Fatalf("merged command = %#v, want newest value 4", command)
	}
	if got := device.Values["issue_count"]; got != float64(4) {
		t.Fatalf("merged top-level issue_count = %#v, want 4", got)
	}
}

func TestStoreReconcileUsesCatalogDeviceTypeForRelinkedDevice(t *testing.T) {
	store := NewStore("keep_last")
	device := ownershipTestDevice("legacy_mixed", "369", float64(1), time.Unix(1_700_005_500, 0).UTC())
	device.JeedomDeviceType = "SpaceControl"
	store.devices[device.DeviceSlug] = device
	store.rebuildIndexesLocked()
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "2",
		Name:             "Power detector",
		Kind:             "Button",
		JeedomCommandIDs: []string{"369"},
	})

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	relinked, ok := store.Device("sia_a0f80d_zone_2")
	if !ok || relinked.JeedomDeviceType != "Button" || relinked.HAModel != "Button" {
		t.Fatalf("relinked device type = %#v, found=%v", relinked, ok)
	}
}

func TestStoreReconcileDeduplicatesUnresolvedCommandIDs(t *testing.T) {
	newerAt := time.Unix(1_700_006_000, 0).UTC()
	store := NewStore("keep_last")
	store.devices["a_old"] = ownershipTestDevice("a_old", "999", float64(1), newerAt.Add(-time.Hour))
	store.devices["b_new"] = ownershipTestDevice("b_new", "999", float64(2), newerAt)
	store.rebuildIndexesLocked()

	store.ReconcileResolver(NewCatalogResolver(devicecatalog.Empty(), CatalogResolverConfig{}))

	owners := make(map[string]string)
	for _, device := range store.Devices() {
		if command, ok := device.RawCommands["999"]; ok {
			owners[device.DeviceSlug] = command.CommandID
		}
	}
	if len(owners) != 1 || owners["b_new"] != "999" {
		t.Fatalf("unresolved command owners = %#v, want only b_new", owners)
	}
	commands := store.Commands()
	if len(commands) != 1 || commands[0].CommandID != "999" || commands[0].DeviceSlug != "b_new" {
		t.Fatalf("command index = %#v, want one b_new owner", commands)
	}
}

func TestCommandWithNewestValueKeepsUntimestampedConcreteLegacyValue(t *testing.T) {
	concrete := Command{Value: float64(12), LastUpdate: time.Unix(100, 0)}
	newerMetadata := Command{Value: nil, LastUpdate: time.Unix(200, 0)}
	merged := commandWithNewestValue(concrete, newerMetadata)
	if merged.Value != float64(12) {
		t.Fatalf("merged value = %#v, want legacy concrete value 12", merged.Value)
	}
}

func TestCommandWithNewestValuePreservesNewerExplicitUnknown(t *testing.T) {
	concreteAt := time.Unix(300, 0).UTC()
	concrete := Command{Value: float64(42), LastUpdate: concreteAt, LastValueAt: concreteAt}
	unknown := Command{
		Value:       nil,
		EmptyValue:  true,
		LastValueAt: time.Unix(200, 0).UTC(),
		LastUpdate:  time.Unix(400, 0).UTC(),
	}
	merged := commandWithNewestValue(concrete, unknown)
	if merged.Value != nil || !merged.EmptyValue {
		t.Fatalf("merged explicit unknown = %#v, want nil/empty", merged)
	}
}

func TestTakeCommandFromMultipleOwnersPreservesNewerExplicitUnknown(t *testing.T) {
	concreteAt := time.Unix(300, 0).UTC()
	unknownAt := time.Unix(400, 0).UTC()
	unknown := cachedOwnershipCommand("369", "20", "issue_count", nil, unknownAt)
	unknown.EmptyValue = true
	unknown.LastValueAt = time.Unix(200, 0).UTC()
	concrete := cachedOwnershipCommand("369", "21", "issue_count", float64(42), concreteAt)

	store := NewStore("keep_last")
	store.devices["a_unknown"] = &Device{
		Device:      "Unknown owner",
		DeviceSlug:  "a_unknown",
		Values:      map[string]any{"issue_count": nil},
		RawCommands: map[string]Command{"369": unknown},
		Actions:     make(map[string]Action),
	}
	store.devices["b_concrete"] = &Device{
		Device:      "Concrete owner",
		DeviceSlug:  "b_concrete",
		Values:      map[string]any{"issue_count": float64(42)},
		RawCommands: map[string]Command{"369": concrete},
		Actions:     make(map[string]Action),
	}
	store.rebuildIndexesLocked()

	transferred, previousSlugs, found, changed := store.takeCommandFromOtherOwnersLocked("369", "target")
	if !found || !changed {
		t.Fatalf("transfer found=%v changed=%v, want both true", found, changed)
	}
	if transferred.Value != nil || !transferred.EmptyValue || !transferred.LastUpdate.Equal(unknownAt) {
		t.Fatalf("transferred command = %#v, want newer explicit unknown", transferred)
	}
	if len(previousSlugs) != 2 {
		t.Fatalf("previous owners = %#v, want both owners", previousSlugs)
	}
	for _, slug := range []string{"a_unknown", "b_concrete"} {
		if _, exists := store.devices[slug].RawCommands["369"]; exists {
			t.Fatalf("old owner %q retained transferred command", slug)
		}
	}
}

func TestRebuildMetricValueUsesUnknownObservationTimeForDerivedState(t *testing.T) {
	device := &Device{
		Device:           "Wall switch",
		DeviceSlug:       "wall_switch",
		JeedomDeviceType: "WallSwitch",
		Values:           map[string]any{"state": true},
		RawCommands: map[string]Command{
			"100": {
				CommandID:   "100",
				Metric:      "state",
				Component:   ComponentBinarySensor,
				Value:       true,
				LastUpdate:  time.Unix(300, 0).UTC(),
				LastValueAt: time.Unix(300, 0).UTC(),
			},
			"101": {
				CommandID:   "101",
				Metric:      "state",
				Component:   ComponentBinarySensor,
				Value:       nil,
				EmptyValue:  true,
				LastUpdate:  time.Unix(400, 0).UTC(),
				LastValueAt: time.Unix(200, 0).UTC(),
			},
		},
		Actions: make(map[string]Action),
	}

	rebuildMetricValue(device, "state")
	if state, exists := device.Values["state"]; !exists || state != nil {
		t.Fatalf("rebuilt state = %#v present=%v, want newer explicit unknown", state, exists)
	}
}

func TestApplyDiscoveryDoesNotRefreshExplicitUnknownObservationTime(t *testing.T) {
	unknownAt := time.Unix(100, 0).UTC()
	concreteAt := time.Unix(200, 0).UTC()
	store := NewStore("unknown")
	store.devices["device"] = &Device{
		Source:           Source,
		Device:           "Device",
		DeviceSlug:       "device",
		JeedomID:         "50",
		JeedomDeviceType: "Button",
		Values:           map[string]any{"issue_count": float64(42)},
		RawCommands: map[string]Command{
			"500": {
				CommandID:  "500",
				EqLogicID:  "50",
				Device:     "Device",
				DeviceSlug: "device",
				Metric:     "issue_count",
				LogicalID:  "issuesCount",
				Type:       "info",
				Subtype:    "numeric",
				EmptyValue: true,
				LastUpdate: unknownAt,
			},
			"501": {
				CommandID:   "501",
				EqLogicID:   "50",
				Device:      "Device",
				DeviceSlug:  "device",
				Metric:      "issue_count",
				LogicalID:   "issuesCount",
				Type:        "info",
				Subtype:     "numeric",
				Value:       float64(42),
				LastUpdate:  concreteAt,
				LastValueAt: concreteAt,
			},
		},
		Actions: make(map[string]Action),
	}
	store.rebuildIndexesLocked()

	result := store.ApplyDiscovery(Discovery{
		EqLogicID:  "50",
		Name:       "Device",
		DeviceType: "Button",
		InfoCommands: map[string]DiscoveryCommand{
			"500": {CommandID: "500", EqLogicID: "50", LogicalID: "issuesCount", Name: "Nombre de defauts", Type: "info", Subtype: "numeric"},
			"501": {CommandID: "501", EqLogicID: "50", LogicalID: "issuesCount", Name: "Nombre de defauts", Type: "info", Subtype: "numeric"},
		},
		Actions:    make(map[string]DiscoveryCommand),
		ReceivedAt: time.Unix(300, 0).UTC(),
	})

	unknown := result.Device.RawCommands["500"]
	if !unknown.LastUpdate.Equal(unknownAt) {
		t.Fatalf("unknown LastUpdate = %s, want preserved observation time %s", unknown.LastUpdate, unknownAt)
	}
	if got := result.Device.Values["issue_count"]; got != float64(42) {
		t.Fatalf("issue_count = %#v, want newer concrete value 42", got)
	}
}

func TestStoreReconcileDoesNotOverwriteNewerSharedMetricOnTarget(t *testing.T) {
	newerAt := time.Unix(1_700_006_500, 0).UTC()
	olderAt := newerAt.Add(-time.Hour)
	store := NewStore("keep_last")
	target := ownershipTestDevice("sia_a0f80d_zone_2", "368", float64(9), newerAt)
	target.LinkedAccount = "A0F80D"
	target.LinkedZone = "2"
	store.devices[target.DeviceSlug] = target
	source := ownershipTestDevice("stale_owner", "369", float64(4), olderAt)
	store.devices[source.DeviceSlug] = source
	store.rebuildIndexesLocked()
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "2",
		Name:             "Діана",
		Kind:             "SpaceControl",
		JeedomCommandIDs: []string{"368", "369"},
	})

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	reconciled, _ := store.Device("sia_a0f80d_zone_2")
	if got := reconciled.Values["issue_count"]; got != float64(9) {
		t.Fatalf("shared metric = %#v, want newer target value 9", got)
	}
}

func TestStoreReconcilePreservesOptimisticStateWhenRelinkingActionOnlyWallSwitch(t *testing.T) {
	loadAt := time.Unix(1_700_006_700, 0).UTC()
	controlAt := loadAt.Add(time.Minute)
	store := NewStore("keep_last")
	store.devices["legacy_wall_switch"] = &Device{
		Source:           Source,
		Device:           "Grid load",
		DeviceSlug:       "legacy_wall_switch",
		JeedomDeviceType: "WallSwitch",
		LastUpdate:       controlAt,
		Values:           map[string]any{"power_w": float64(10), "state": false},
		RawCommands: map[string]Command{
			"346": {
				CommandID:   "346",
				EqLogicID:   "26",
				Device:      "Grid load",
				DeviceSlug:  "legacy_wall_switch",
				Metric:      "power_w",
				LogicalID:   "powerWTh",
				Type:        "info",
				Subtype:     "numeric",
				Value:       float64(10),
				LastUpdate:  loadAt,
				LastValueAt: loadAt,
			},
		},
		Actions: map[string]Action{
			"on":  {Action: "on", CommandID: "348", DeviceSlug: "legacy_wall_switch", DeviceType: "WallSwitch"},
			"off": {Action: "off", CommandID: "349", DeviceSlug: "legacy_wall_switch", DeviceType: "WallSwitch", LastRequestedAt: controlAt},
		},
	}
	store.rebuildIndexesLocked()
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "26",
		Name:             "Grid load",
		Kind:             "WallSwitch",
		JeedomCommandIDs: []string{"346", "348", "349"},
	})

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	target, ok := store.Device("sia_a0f80d_zone_26")
	if !ok || target.Values["state"] != false {
		t.Fatalf("relinked WallSwitch = %#v, want newer optimistic state=false", target)
	}
}

func TestStoreReconcileRehomesCatalogActionWithoutBreakingLegacyMirrors(t *testing.T) {
	store := NewStore("keep_last")
	state := cachedOwnershipCommand("276", "20", "state", false, time.Unix(1_700_007_000, 0).UTC())
	store.devices["sia_a0f80d_zone_20"] = &Device{
		Source:           Source,
		Device:           "Діана",
		DeviceSlug:       "sia_a0f80d_zone_20",
		JeedomDeviceType: "SpaceControl",
		Values:           map[string]any{"state": false},
		RawCommands:      map[string]Command{"276": state},
		Actions: map[string]Action{
			"on": {
				Action:     "on",
				CommandID:  "900",
				Device:     "Діана",
				DeviceSlug: "sia_a0f80d_zone_20",
				DeviceType: "SpaceControl",
				Allowed:    false,
			},
		},
	}
	store.rebuildIndexesLocked()
	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Relay",
			Kind:             "Relay",
			JeedomCommandIDs: []string{"900"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276"},
		},
	)

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	oldOwner, _ := store.Device("sia_a0f80d_zone_20")
	if _, exists := oldOwner.Actions["on"]; exists {
		t.Fatalf("old owner retained catalog-resolved action: %#v", oldOwner.Actions)
	}
	if len(oldOwner.PendingActionDiscoveryCleanups) != 1 || oldOwner.PendingActionDiscoveryCleanups[0].CommandID != "900" {
		t.Fatalf("old owner cleanup queue = %#v", oldOwner.PendingActionDiscoveryCleanups)
	}
	action, ok := store.ActionByCommandID("900")
	if !ok || action.DeviceSlug != "sia_a0f80d_zone_2" || action.DeviceType != "Relay" || !action.Allowed {
		t.Fatalf("re-homed action = %#v, found=%v", action, ok)
	}
	target, _ := store.Device("sia_a0f80d_zone_2")
	if target.JeedomDeviceType != "Relay" {
		t.Fatalf("action target inherited source type: %#v", target)
	}
}

func TestStoreReconcileKeepsSameNameActionsWhileRehomingCollision(t *testing.T) {
	zone20State := cachedOwnershipCommand("276", "20", "state", false, time.Unix(1_700_007_100, 0).UTC())
	zone20State.DeviceSlug = "sia_a0f80d_zone_20"
	store := NewStore("keep_last")
	store.devices["sia_a0f80d_zone_20"] = &Device{
		Source:           Source,
		Device:           "Діана",
		DeviceSlug:       "sia_a0f80d_zone_20",
		JeedomDeviceType: "SpaceControl",
		Values:           map[string]any{"state": false},
		RawCommands:      map[string]Command{"276": zone20State},
		Actions: map[string]Action{
			"on": {
				Action:          "on",
				CommandID:       "900",
				Device:          "Діана",
				DeviceSlug:      "sia_a0f80d_zone_20",
				DeviceType:      "SpaceControl",
				LastRequestedAt: time.Unix(1_700_007_200, 0).UTC(),
			},
		},
	}
	mixedState := zone20State
	mixedState.DeviceSlug = "mixed_owner"
	store.devices["mixed_owner"] = &Device{
		Source:           Source,
		Device:           "Діана",
		DeviceSlug:       "mixed_owner",
		JeedomDeviceType: "SpaceControl",
		Values:           map[string]any{"state": false},
		RawCommands:      map[string]Command{"276": mixedState},
		Actions: map[string]Action{
			"on": {
				Action:            "on",
				CommandID:         "901",
				Device:            "Relay",
				DeviceSlug:        "mixed_owner",
				DeviceType:        "Relay",
				LastRequestedAt:   time.Unix(1_700_007_300, 0).UTC(),
				LastRequestSource: "test",
			},
		},
	}
	store.rebuildIndexesLocked()
	catalog := testCatalog(t,
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "2",
			Name:             "Relay",
			Kind:             "Relay",
			JeedomCommandIDs: []string{"901"},
		},
		devicecatalog.Device{
			Account:          "A0F80D",
			Zone:             "20",
			Name:             "Діана",
			Kind:             "SpaceControl",
			JeedomCommandIDs: []string{"276", "900"},
		},
	)

	store.ReconcileResolver(NewCatalogResolver(catalog, CatalogResolverConfig{}))

	zone20Action, ok := store.Action("sia_a0f80d_zone_20", "on")
	if !ok || zone20Action.CommandID != "900" {
		t.Fatalf("zone 20 action = %#v, found=%v, want command 900", zone20Action, ok)
	}
	zone2Action, ok := store.Action("sia_a0f80d_zone_2", "on")
	if !ok || zone2Action.CommandID != "901" || !zone2Action.Allowed || zone2Action.LastRequestSource != "test" {
		t.Fatalf("zone 2 action = %#v, found=%v, want rehomed command 901 with audit", zone2Action, ok)
	}
	if byID, ok := store.ActionByCommandID("900"); !ok || byID.DeviceSlug != "sia_a0f80d_zone_20" {
		t.Fatalf("command 900 lookup = %#v, found=%v", byID, ok)
	}
	if byID, ok := store.ActionByCommandID("901"); !ok || byID.DeviceSlug != "sia_a0f80d_zone_2" {
		t.Fatalf("command 901 lookup = %#v, found=%v", byID, ok)
	}
}

func TestActionCleanupQueueAndAcknowledgementUseFullDiscoveryIdentity(t *testing.T) {
	actions := []Action{
		{Action: "on", CommandID: "900", DeviceSlug: "old_a"},
		{Action: "on", CommandID: "900", DeviceSlug: "old_b"},
		{Action: "off", CommandID: "900", DeviceSlug: "old_a"},
	}
	device := &Device{PendingActionDiscoveryCleanups: []Action{}}
	queueActionCleanups(device, actions)
	if len(device.PendingActionDiscoveryCleanups) != 3 {
		t.Fatalf("cleanup queue = %#v, want three distinct action topics", device.PendingActionDiscoveryCleanups)
	}
	store := NewStore("keep_last")
	device.DeviceSlug = "holder"
	device.Values = make(map[string]any)
	device.RawCommands = make(map[string]Command)
	device.Actions = make(map[string]Action)
	store.devices["holder"] = device
	if !store.AcknowledgeActionCleanups(actions[:1]) {
		t.Fatal("expected exact cleanup acknowledgement")
	}
	remaining, _ := store.Device("holder")
	if len(remaining.PendingActionDiscoveryCleanups) != 2 {
		t.Fatalf("remaining cleanups = %#v, want two", remaining.PendingActionDiscoveryCleanups)
	}
}

func TestStoreApplyDiscoveryCancelsActiveActionCleanupFromAnotherHolder(t *testing.T) {
	catalog := testCatalog(t, devicecatalog.Device{
		Account:          "A0F80D",
		Zone:             "2",
		Name:             "Relay",
		Kind:             "Relay",
		JeedomCommandIDs: []string{"900"},
	})
	store := NewStoreWithResolver("keep_last", NewCatalogResolver(catalog, CatalogResolverConfig{}))
	store.devices["cleanup_holder"] = &Device{
		Source:      Source,
		Device:      "Cleanup holder",
		DeviceSlug:  "cleanup_holder",
		Values:      make(map[string]any),
		RawCommands: make(map[string]Command),
		Actions:     make(map[string]Action),
		PendingActionDiscoveryCleanups: []Action{
			{Action: "on", CommandID: "900", DeviceSlug: "sia_a0f80d_zone_2"},
			{Action: "on", CommandID: "900", DeviceSlug: "old_source"},
		},
	}
	store.rebuildIndexesLocked()

	result := store.ApplyDiscovery(Discovery{
		EqLogicID:  "2",
		Name:       "Relay",
		DeviceType: "Relay",
		Enabled:    true,
		Visible:    true,
		Actions: map[string]DiscoveryCommand{
			"900": {
				CommandID: "900",
				EqLogicID: "2",
				LogicalID: "SWITCH_ON",
				Name:      "On",
				Type:      "action",
			},
		},
		ReceivedAt: time.Unix(1_700_008_000, 0).UTC(),
	})

	holder, ok := store.Device("cleanup_holder")
	if !ok {
		t.Fatal("cleanup holder disappeared")
	}
	if len(holder.PendingActionDiscoveryCleanups) != 1 || holder.PendingActionDiscoveryCleanups[0].DeviceSlug != "old_source" {
		t.Fatalf("cleanup holder = %#v, want only old-source cleanup", holder.PendingActionDiscoveryCleanups)
	}
	if len(result.PreviousDevices) != 1 || result.PreviousDevices[0].DeviceSlug != "cleanup_holder" {
		t.Fatalf("PreviousDevices = %#v, want changed cleanup holder", result.PreviousDevices)
	}
	action, ok := store.ActionByCommandID("900")
	if !ok || action.DeviceSlug != "sia_a0f80d_zone_2" || action.Action != "on" {
		t.Fatalf("active action = %#v, found=%v", action, ok)
	}
}

func ownershipTestDevice(slug, commandID string, value float64, valueAt time.Time) *Device {
	command := cachedOwnershipCommand(commandID, slug, "issue_count", value, valueAt)
	command.DeviceSlug = slug
	return &Device{
		Source:           Source,
		Device:           "Діана",
		DeviceSlug:       slug,
		JeedomDeviceType: "SpaceControl",
		Values:           map[string]any{"issue_count": value},
		RawCommands:      map[string]Command{commandID: command},
		Actions:          make(map[string]Action),
	}
}

func cachedOwnershipCommand(commandID, eqLogicID, metric string, value any, valueAt time.Time) Command {
	return Command{
		CommandID:   commandID,
		EqLogicID:   eqLogicID,
		Device:      "Діана",
		DeviceSlug:  "sia_a0f80d_zone_20",
		Name:        metric,
		RawName:     metric,
		Metric:      metric,
		Component:   ComponentSensor,
		Type:        "info",
		Subtype:     "string",
		Value:       value,
		LastUpdate:  valueAt,
		LastValueAt: valueAt,
	}
}

func assertUniqueRawCommandOwners(t *testing.T, devices []Device, wantOwner map[string]string) {
	t.Helper()
	owners := make(map[string][]string)
	for _, device := range devices {
		for commandID, command := range device.RawCommands {
			owners[commandID] = append(owners[commandID], device.DeviceSlug)
			if command.DeviceSlug != device.DeviceSlug {
				t.Fatalf("raw command %s embeds owner %q but is stored on %q", commandID, command.DeviceSlug, device.DeviceSlug)
			}
		}
	}
	for commandID, got := range owners {
		if len(got) != 1 {
			t.Fatalf("raw command %s has multiple owners: %#v", commandID, got)
		}
	}
	for commandID, want := range wantOwner {
		got := owners[commandID]
		if len(got) != 1 || got[0] != want {
			t.Fatalf("raw command %s owners = %#v, want only %q", commandID, got, want)
		}
	}
}
