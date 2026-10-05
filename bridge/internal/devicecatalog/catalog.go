package devicecatalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/event"
)

type Catalog struct {
	mu      sync.RWMutex
	path    string
	devices []Device
	byExact map[string]Device
	byZone  map[string]Device
}

type Device struct {
	Account          string    `json:"account"`
	Zone             string    `json:"zone"`
	Partition        string    `json:"partition,omitempty"`
	Group            string    `json:"group,omitempty"`
	Device           string    `json:"device,omitempty"`
	Name             string    `json:"name"`
	Room             string    `json:"room"`
	Kind             string    `json:"kind"`
	Events           []string  `json:"events"`
	Description      string    `json:"description,omitempty"`
	JeedomNames      []string  `json:"jeedom_names,omitempty"`
	JeedomCommandIDs []string  `json:"jeedom_command_ids,omitempty"`
	AutoDiscovered   bool      `json:"auto_discovered,omitempty"`
	FirstSeenAt      time.Time `json:"first_seen_at,omitempty"`
	LastSeenAt       time.Time `json:"last_seen_at,omitempty"`
	LastEventCode    string    `json:"last_event_code,omitempty"`
	LastEventName    string    `json:"last_event_name,omitempty"`
	LastSignal       string    `json:"last_signal,omitempty"`
}

type UpsertResult struct {
	Device  Device
	Created bool
	Updated bool
}

func Empty() *Catalog {
	return &Catalog{
		devices: make([]Device, 0),
		byExact: make(map[string]Device),
		byZone:  make(map[string]Device),
	}
}

func Load(ctx context.Context, path string) (*Catalog, error) {
	if path == "" {
		return Empty(), nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			catalog := Empty()
			catalog.path = path
			return catalog, nil
		}
		return nil, err
	}

	var devices []Device
	if err := json.Unmarshal(data, &devices); err != nil {
		return nil, err
	}

	catalog := Empty()
	catalog.path = path
	catalog.devices = devices
	catalog.rebuildIndexes()
	return catalog, nil
}

func (c *Catalog) Path() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

func (c *Catalog) Devices() []Device {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Device, len(c.devices))
	copy(out, c.devices)
	return out
}

func (c *Catalog) Replace(ctx context.Context, devices []Device) ([]Device, error) {
	if c == nil {
		return nil, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.devices = make([]Device, len(devices))
	copy(c.devices, devices)
	c.rebuildIndexes()
	if c.path != "" {
		if err := c.saveLocked(); err != nil {
			return nil, err
		}
	}
	out := make([]Device, len(c.devices))
	copy(out, c.devices)
	return out, nil
}

func (c *Catalog) Lookup(account, zone, device string) (Device, bool) {
	if c == nil {
		return Device{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	if found, ok := c.byExact[key(account, zone, device)]; ok {
		return found, true
	}
	if found, ok := c.byZone[key(account, zone, "")]; ok {
		return found, true
	}
	return Device{}, false
}

func (c *Catalog) UpsertFromEvent(ctx context.Context, evt event.Normalized) (UpsertResult, error) {
	if c == nil || c.path == "" || evt.Account == "" || evt.Zone == "" {
		return UpsertResult{}, nil
	}
	select {
	case <-ctx.Done():
		return UpsertResult{}, ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := evt.ReceivedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	index, ok := c.findDeviceIndex(evt.Account, evt.Zone, evt.Device)
	if !ok {
		device := Device{
			Account:        evt.Account,
			Zone:           evt.Zone,
			Partition:      evt.Partition,
			Group:          evt.Group,
			Device:         evt.Device,
			Name:           defaultName(evt),
			Room:           "unknown",
			Kind:           defaultKind(evt),
			Events:         []string{eventFamily(evt)},
			AutoDiscovered: true,
			FirstSeenAt:    now,
			LastSeenAt:     now,
			LastEventCode:  evt.EventCode,
			LastEventName:  evt.EventName,
			LastSignal:     evt.Signal,
		}
		c.devices = append(c.devices, device)
		c.rebuildIndexes()
		return UpsertResult{Device: device, Created: true}, c.saveLocked()
	}

	device := c.devices[index]
	changed := false
	if device.Partition == "" && evt.Partition != "" {
		device.Partition = evt.Partition
		changed = true
	}
	if device.Group == "" && evt.Group != "" {
		device.Group = evt.Group
		changed = true
	}
	if device.Device == "" && evt.Device != "" {
		device.Device = evt.Device
		changed = true
	}
	family := eventFamily(evt)
	if family != "" && !contains(device.Events, family) {
		device.Events = append(device.Events, family)
		changed = true
	}
	if device.LastSeenAt.IsZero() || now.After(device.LastSeenAt) {
		device.LastSeenAt = now
		changed = true
	}
	if device.LastEventCode != evt.EventCode {
		device.LastEventCode = evt.EventCode
		changed = true
	}
	if device.LastEventName != evt.EventName {
		device.LastEventName = evt.EventName
		changed = true
	}
	if device.LastSignal != evt.Signal {
		device.LastSignal = evt.Signal
		changed = true
	}
	if !changed {
		return UpsertResult{Device: device}, nil
	}
	c.devices[index] = device
	c.rebuildIndexes()
	return UpsertResult{Device: device, Updated: true}, c.saveLocked()
}

func (c *Catalog) rebuildIndexes() {
	c.byExact = make(map[string]Device, len(c.devices))
	c.byZone = make(map[string]Device, len(c.devices))
	for _, device := range c.devices {
		if device.Account == "" || device.Zone == "" {
			continue
		}
		if device.Device != "" {
			c.byExact[key(device.Account, device.Zone, device.Device)] = device
		}
		if _, exists := c.byZone[key(device.Account, device.Zone, "")]; !exists {
			c.byZone[key(device.Account, device.Zone, "")] = device
		}
	}
}

func (c *Catalog) findDeviceIndex(account, zone, deviceID string) (int, bool) {
	fallback := -1
	for i, device := range c.devices {
		if device.Account != account || device.Zone != zone {
			continue
		}
		if deviceID != "" && device.Device == deviceID {
			return i, true
		}
		if device.Device == "" && fallback < 0 {
			fallback = i
		}
	}
	if fallback >= 0 {
		return fallback, true
	}
	return -1, false
}

func (c *Catalog) saveLocked() error {
	data, err := json.MarshalIndent(c.devices, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(c.path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(c.path)
	return os.Rename(tmp, c.path)
}

func key(account, zone, device string) string {
	return account + "\x00" + zone + "\x00" + device
}

func defaultName(evt event.Normalized) string {
	if evt.Device != "" {
		return "Zone " + evt.Zone + " " + evt.Device
	}
	return "Zone " + evt.Zone
}

func defaultKind(evt event.Normalized) string {
	switch evt.Source {
	case "button_or_user", "button_or_transmitter":
		return "button"
	case "keypad", "user_or_keypad":
		return "keypad"
	case "user":
		return "user"
	case "hub":
		return "hub"
	case "sensor":
		switch evt.Signal {
		case "smoke", "fire", "temperature", "fire_detector", "co":
			return "fireprotect"
		case "water_leak":
			return "leakprotect"
		case "burglary":
			return "ajax_sensor"
		default:
			return "ajax_sensor"
		}
	case "device", "hub_or_device":
		return "ajax_device"
	default:
		return "unknown"
	}
}

func eventFamily(evt event.Normalized) string {
	switch {
	case evt.Signal == "tamper":
		return "tamper"
	case evt.Signal == "night_mode":
		return "night_mode"
	case evt.Signal != "" && evt.Signal != "unknown":
		return evt.Signal
	case evt.EventClass == event.ClassAlarm:
		return "alarm"
	case evt.EventClass == event.ClassRestore:
		return "restore"
	case evt.EventClass == event.ClassArm:
		return "arm"
	case evt.EventClass == event.ClassDisarm:
		return "disarm"
	case evt.EventClass == event.ClassTrouble:
		return "trouble"
	default:
		return string(evt.EventClass)
	}
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
