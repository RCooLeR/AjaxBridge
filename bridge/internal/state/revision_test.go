package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/RCooLeR/AjaxBridge/internal/event"
)

func TestSnapshotRevisionOrdersEventsOnlineAndCatalogWithoutJSONChange(t *testing.T) {
	engine := NewEngine(time.Minute, devicecatalog.Empty())
	initial := engine.Snapshot()
	if initial.Revision == 0 || engine.Snapshot().Revision != initial.Revision {
		t.Fatal("initial revision must be nonzero and reads must not advance it")
	}
	now := time.Now().UTC()
	active := engine.Apply(event.Normalized{Account: "1", Zone: "2", ReceivedAt: now, EventClass: event.ClassAlarm, Signal: "fire"})
	offline := engine.RefreshOnline(now.Add(2 * time.Minute))
	reloaded := engine.ReloadCatalog()
	if !(initial.Revision < active.Revision && active.Revision < offline.Revision && offline.Revision < reloaded.Revision) {
		t.Fatalf("revisions are not ordered: %d %d %d %d", initial.Revision, active.Revision, offline.Revision, reloaded.Revision)
	}
	if !active.Accounts[0].Online || offline.Accounts[0].Online || !offline.Accounts[0].LastEventAt.Equal(active.Accounts[0].LastEventAt) {
		t.Fatal("fixture must change online status without an event timestamp change")
	}
	data, err := json.Marshal(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["accounts"] == nil || body["zones"] == nil {
		t.Fatalf("internal publication revision changed public JSON: %s", data)
	}
}
