package app

import (
	"testing"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/event"
	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/state"
)

func TestMetricSourceLabelCoversEveryCollectorAndPreservesLegacy(t *testing.T) {
	for _, sourceID := range []string{"", "apartment", "house-2"} {
		t.Run(sourceID, func(t *testing.T) {
			registry, metrics := newMetricRegistry(sourceID)
			metrics.ObserveEvent(event.Normalized{Account: "ABC", EventCode: "BA", ParseStatus: event.ParseStatusOK})
			metrics.SetSnapshot(state.Snapshot{Accounts: []state.Account{{Account: "ABC", Online: true}}, Zones: []state.Zone{{Account: "ABC", Zone: "2"}}})
			store := jeedom.NewStore("keep_last")
			store.Apply(jeedom.Event{CommandID: "55", DeviceName: "Detector", CommandName: "Temperature", Subtype: "numeric", Value: []byte(`22`), ReceivedAt: time.Now()})
			metrics.SetJeedomStore(store)
			families, err := registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			seenJeedom, seenSIA, seenRuntime := false, false, false
			for _, family := range families {
				seenJeedom = seenJeedom || family.GetName() == "ajax_jeedom_command_value"
				seenSIA = seenSIA || family.GetName() == "ajax_account_online"
				seenRuntime = seenRuntime || family.GetName() == "go_goroutines"
				for _, sample := range family.Metric {
					got, found := "", false
					for _, label := range sample.Label {
						if label.GetName() == "source_id" {
							got, found = label.GetValue(), true
						}
					}
					if got != sourceID || found != (sourceID != "") {
						t.Fatalf("%s source label = %q (present %t), want %q", family.GetName(), got, found, sourceID)
					}
				}
			}
			if !seenJeedom || !seenSIA || !seenRuntime {
				t.Fatalf("missing collectors: Jeedom=%t SIA=%t runtime=%t", seenJeedom, seenSIA, seenRuntime)
			}
		})
	}
}
