package state

import (
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/RCooLeR/AjaxBridge/internal/event"
)

type Account struct {
	Account        string    `json:"account"`
	Online         bool      `json:"online"`
	Mode           string    `json:"mode"`
	Armed          bool      `json:"armed"`
	NightMode      bool      `json:"night_mode"`
	PartiallyArmed bool      `json:"partially_armed"`
	AlarmActive    bool      `json:"alarm_active"`
	TamperActive   bool      `json:"tamper_active"`
	TroubleActive  bool      `json:"trouble_active"`
	LastEventCode  string    `json:"last_event_code"`
	LastEventName  string    `json:"last_event_name"`
	LastSignal     string    `json:"last_signal"`
	LastEventAt    time.Time `json:"last_event_at"`
	LastPingAt     time.Time `json:"last_ping_at"`
}

type Zone struct {
	Account           string          `json:"account"`
	Partition         string          `json:"partition"`
	Group             string          `json:"group"`
	Zone              string          `json:"zone"`
	Device            string          `json:"device"`
	DeviceName        string          `json:"device_name"`
	Room              string          `json:"room"`
	Kind              string          `json:"kind"`
	DeviceEvents      []string        `json:"device_events"`
	DeviceEventsLabel string          `json:"device_events_label"`
	AlarmActive       bool            `json:"alarm_active"`
	AlarmSignal       string          `json:"alarm_signal"`
	AlarmAction       string          `json:"alarm_action"`
	AlarmEventCode    string          `json:"alarm_event_code"`
	AlarmEventName    string          `json:"alarm_event_name"`
	AlarmStartedAt    time.Time       `json:"alarm_started_at"`
	TamperActive      bool            `json:"tamper_active"`
	TroubleActive     bool            `json:"trouble_active"`
	SignalActive      map[string]bool `json:"signal_active"`
	LastEventCode     string          `json:"last_event_code"`
	LastEventName     string          `json:"last_event_name"`
	LastSignal        string          `json:"last_signal"`
	LastEventAt       time.Time       `json:"last_event_at"`
}

type Snapshot struct {
	Accounts []Account `json:"accounts"`
	Zones    []Zone    `json:"zones"`
	// Revision orders in-process publications without changing the JSON API.
	Revision uint64 `json:"-"`
}

type Engine struct {
	mu           sync.RWMutex
	offlineGrace time.Duration
	devices      *devicecatalog.Catalog
	accounts     map[string]*Account
	zones        map[string]*Zone
	directAlarms map[string]bool
	revision     uint64
}

func NewEngine(offlineGrace time.Duration, devices *devicecatalog.Catalog) *Engine {
	engine := &Engine{
		offlineGrace: offlineGrace,
		devices:      devices,
		accounts:     make(map[string]*Account),
		zones:        make(map[string]*Zone),
		directAlarms: make(map[string]bool),
		revision:     1,
	}
	engine.seedCatalogDevices()
	return engine
}

func (e *Engine) Apply(evt event.Normalized) Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revision++

	account := e.account(evt.Account)
	account.LastEventAt = maxTime(account.LastEventAt, evt.ReceivedAt)
	account.LastEventCode = evt.EventCode
	account.LastEventName = evt.EventName
	account.LastSignal = evt.Signal
	account.Online = true

	if evt.EventClass == event.ClassTest {
		account.LastPingAt = maxTime(account.LastPingAt, evt.ReceivedAt)
	}

	switch evt.EventClass {
	case event.ClassAlarm:
		if evt.Zone == "" {
			e.directAlarms[account.Account] = true
			account.AlarmActive = true
		}
	case event.ClassRestore:
		applyAccountRestore(account, evt)
		if evt.Zone == "" {
			e.directAlarms[account.Account] = false
		}
	case event.ClassArm:
		account.Armed = true
		account.NightMode = false
		account.PartiallyArmed = evt.EventCode == "CG"
	case event.ClassNight:
		account.Armed = true
		account.NightMode = true
		account.PartiallyArmed = true
	case event.ClassDisarm:
		if evt.Signal == "night_mode" {
			account.NightMode = false
			account.PartiallyArmed = false
		} else {
			account.Armed = false
			account.NightMode = false
			account.PartiallyArmed = false
			e.directAlarms[account.Account] = false
		}
	case event.ClassTamper:
		account.TamperActive = true
	case event.ClassTrouble:
		account.TroubleActive = true
	}

	if evt.Zone != "" {
		zone := e.zone(evt.Account, evt.Zone)
		e.updateZoneMetadata(zone, evt)
		zone.LastEventAt = maxTime(zone.LastEventAt, evt.ReceivedAt)
		zone.LastEventCode = evt.EventCode
		zone.LastEventName = evt.EventName
		zone.LastSignal = evt.Signal
		switch evt.EventClass {
		case event.ClassAlarm:
			zone.AlarmActive = true
			zone.AlarmSignal = evt.Signal
			zone.AlarmAction = evt.EventAction
			zone.AlarmEventCode = evt.EventCode
			zone.AlarmEventName = evt.EventName
			zone.AlarmStartedAt = evt.ReceivedAt
			zone.setSignal(evt.Signal, true)
		case event.ClassRestore:
			applyZoneRestore(zone, evt)
		case event.ClassTamper:
			zone.TamperActive = true
			zone.setSignal(evt.Signal, true)
		case event.ClassTrouble:
			zone.TroubleActive = true
			zone.setSignal(evt.Signal, true)
			if evt.EventAction == "device_bypassed" {
				clearZoneAlarm(zone)
			}
		case event.ClassCommon:
			if evt.EventAction == "turned_off" {
				clearZoneAlarm(zone)
			}
		case event.ClassArm:
			zone.setSignal("arming", true)
			zone.setSignal("night_mode", false)
		case event.ClassNight:
			zone.setSignal("arming", true)
			zone.setSignal("night_mode", true)
		case event.ClassDisarm:
			if evt.Signal == "night_mode" {
				zone.setSignal("night_mode", false)
			} else {
				zone.setSignal("arming", false)
				zone.setSignal("night_mode", false)
				zone.setSignal("duress", false)
			}
		}
	}
	e.refreshAccountAlarmLocked(evt.Account)

	e.refreshOnlineLocked(time.Now().UTC())
	return e.snapshotLocked()
}

func (e *Engine) RefreshOnline(now time.Time) Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revision++
	e.refreshOnlineLocked(now)
	return e.snapshotLocked()
}

func (e *Engine) ReloadCatalog() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revision++
	e.seedCatalogDevices()
	return e.snapshotLocked()
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snapshotLocked()
}

func (e *Engine) account(accountID string) *Account {
	if accountID == "" {
		accountID = "unknown"
	}
	if current, ok := e.accounts[accountID]; ok {
		return current
	}
	current := &Account{Account: accountID, Mode: "unknown"}
	e.accounts[accountID] = current
	return current
}

func (e *Engine) zone(accountID, zoneID string) *Zone {
	key := accountID + "/" + zoneID
	if current, ok := e.zones[key]; ok {
		return current
	}
	current := &Zone{Account: accountID, Zone: zoneID, SignalActive: make(map[string]bool)}
	e.zones[key] = current
	return current
}

func (e *Engine) seedCatalogDevices() {
	if e.devices == nil {
		return
	}
	for _, device := range e.devices.Devices() {
		if device.Account == "" || device.Zone == "" {
			continue
		}
		zone := e.zone(device.Account, device.Zone)
		applyDeviceMetadata(zone, device)
		applyCatalogState(zone, device)
	}
}

func (e *Engine) refreshOnlineLocked(now time.Time) {
	for _, account := range e.accounts {
		reference := account.LastPingAt
		if reference.IsZero() {
			reference = account.LastEventAt
		}
		account.Online = !reference.IsZero() && now.Sub(reference) <= e.offlineGrace
		switch {
		case account.NightMode:
			account.Mode = "night"
		case account.Armed:
			account.Mode = "armed"
		default:
			account.Mode = "disarmed"
		}
	}
}

func (e *Engine) refreshAccountAlarmLocked(accountID string) {
	account := e.accounts[accountID]
	if account == nil {
		return
	}
	active := e.directAlarms[accountID]
	if !active {
		for _, zone := range e.zones {
			if zone.Account == accountID && zone.AlarmActive {
				active = true
				break
			}
		}
	}
	account.AlarmActive = active
}

func (e *Engine) snapshotLocked() Snapshot {
	snapshot := Snapshot{
		Accounts: make([]Account, 0, len(e.accounts)),
		Zones:    make([]Zone, 0, len(e.zones)),
		Revision: e.revision,
	}
	for _, account := range e.accounts {
		snapshot.Accounts = append(snapshot.Accounts, *account)
	}
	for _, zone := range e.zones {
		copyZone := *zone
		copyZone.DeviceEvents = append([]string(nil), zone.DeviceEvents...)
		copyZone.SignalActive = cloneSignalActive(zone.SignalActive)
		snapshot.Zones = append(snapshot.Zones, copyZone)
	}
	return snapshot
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (e *Engine) updateZoneMetadata(zone *Zone, evt event.Normalized) {
	if evt.Partition != "" {
		zone.Partition = evt.Partition
	}
	if evt.Group != "" {
		zone.Group = evt.Group
	}
	if evt.Device != "" {
		zone.Device = evt.Device
	}
	if e.devices == nil {
		return
	}
	if device, ok := e.devices.Lookup(evt.Account, evt.Zone, evt.Device); ok {
		applyDeviceMetadata(zone, device)
	}
}

func applyDeviceMetadata(zone *Zone, device devicecatalog.Device) {
	if device.Partition != "" {
		zone.Partition = device.Partition
	}
	if device.Group != "" {
		zone.Group = device.Group
	}
	if device.Device != "" {
		zone.Device = device.Device
	}
	zone.DeviceName = device.Name
	zone.Room = device.Room
	zone.Kind = device.Kind
	zone.DeviceEvents = append([]string(nil), device.Events...)
	zone.DeviceEventsLabel = strings.Join(device.Events, ",")
	zone.ensureSignalMap()
	for _, signal := range device.Events {
		if signal != "" {
			if _, ok := zone.SignalActive[signal]; !ok {
				zone.SignalActive[signal] = false
			}
		}
	}
}

func applyCatalogState(zone *Zone, device devicecatalog.Device) {
	if zone == nil {
		return
	}
	if !device.LastSeenAt.IsZero() && device.LastSeenAt.After(zone.LastEventAt) {
		zone.LastEventAt = device.LastSeenAt
		zone.LastEventCode = device.LastEventCode
		zone.LastEventName = device.LastEventName
		zone.LastSignal = device.LastSignal
		return
	}
	if zone.LastEventCode == "" {
		zone.LastEventCode = device.LastEventCode
	}
	if zone.LastEventName == "" {
		zone.LastEventName = device.LastEventName
	}
	if zone.LastSignal == "" {
		zone.LastSignal = device.LastSignal
	}
}

func applyAccountRestore(account *Account, evt event.Normalized) {
	switch evt.Signal {
	case "tamper":
		account.TamperActive = false
	case "battery", "connectivity", "power", "interference", "fire_detector", "hardware", "accelerometer", "bypass", "tamper_bypass":
		account.TroubleActive = false
	default:
		account.AlarmActive = false
	}
}

func applyZoneRestore(zone *Zone, evt event.Normalized) {
	switch evt.Signal {
	case "tamper":
		zone.TamperActive = false
		zone.setSignal(evt.Signal, false)
	case "battery", "connectivity", "power", "interference", "fire_detector", "hardware", "accelerometer", "bypass", "tamper_bypass":
		zone.TroubleActive = false
		zone.setSignal(evt.Signal, false)
	default:
		clearZoneAlarm(zone)
		zone.setSignal(evt.Signal, false)
	}
}

func clearZoneAlarm(zone *Zone) {
	if zone == nil {
		return
	}
	zone.setSignal(zone.AlarmSignal, false)
	zone.AlarmActive = false
	zone.AlarmSignal = ""
	zone.AlarmAction = ""
	zone.AlarmEventCode = ""
	zone.AlarmEventName = ""
	zone.AlarmStartedAt = time.Time{}
}

func (z *Zone) ensureSignalMap() {
	if z.SignalActive == nil {
		z.SignalActive = make(map[string]bool)
	}
}

func (z *Zone) setSignal(signal string, active bool) {
	if signal == "" || signal == "unknown" {
		return
	}
	z.ensureSignalMap()
	z.SignalActive[signal] = active
}

func cloneSignalActive(values map[string]bool) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]bool, len(values))
	for signal, active := range values {
		out[signal] = active
	}
	return out
}
