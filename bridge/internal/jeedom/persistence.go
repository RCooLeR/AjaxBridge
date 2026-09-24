package jeedom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const persistedStoreVersion = 1

type persistedStore struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
}

func LoadStore(ctx context.Context, path, policy string, resolver IdentityResolver) (*Store, error) {
	store := NewStoreWithResolver(policy, resolver)
	store.SetPath(path)
	path = strings.TrimSpace(path)
	if path == "" {
		return store, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}

	devices, err := decodePersistedDevices(data)
	if err != nil {
		return nil, err
	}
	store.replaceDevices(devices)
	return store, nil
}

func (s *Store) SetPath(path string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = strings.TrimSpace(path)
}

func (s *Store) Save(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.RLock()
	path := s.path
	devices := s.devicesLocked()
	s.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return writePersistedDevices(ctx, path, devices)
}

func (s *Store) replaceDevices(devices []Device) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices = make(map[string]*Device, len(devices))
	for _, device := range devices {
		device = normalizePersistedDevice(device)
		if device.DeviceSlug == "" || device.DeviceSlug == "unknown" {
			continue
		}
		current := device
		s.devices[current.DeviceSlug] = &current
	}
	s.rebuildIndexesLocked()
}

func decodePersistedDevices(data []byte) ([]Device, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	// Accept a bare device array as a forwards-compatible escape hatch for
	// hand-edited caches and early development builds.
	if len(data) > 0 && data[0] == '[' {
		var devices []Device
		if err := json.Unmarshal(data, &devices); err != nil {
			return nil, err
		}
		return devices, nil
	}
	var persisted persistedStore
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, err
	}
	return persisted.Devices, nil
}

func writePersistedDevices(ctx context.Context, path string, devices []Device) error {
	body, err := json.MarshalIndent(persistedStore{
		Version: persistedStoreVersion,
		Devices: devices,
	}, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func normalizePersistedDevice(device Device) Device {
	device.Source = firstNonEmpty(device.Source, Source)
	device.DeviceSlug = Slug(device.DeviceSlug)
	if device.DeviceSlug == "" || device.DeviceSlug == "unknown" {
		device.DeviceSlug = Slug(device.Device)
	}
	if device.Values == nil {
		device.Values = make(map[string]any)
	}
	if device.RawCommands == nil {
		device.RawCommands = make(map[string]Command)
	}
	if device.Actions == nil {
		device.Actions = make(map[string]Action)
	}
	if device.HAManufacturer == "" {
		device.HAManufacturer = "Ajax via Jeedom"
	}
	if device.HAModel == "" {
		device.HAModel = firstNonEmpty(device.JeedomDeviceType, "Jeedom MQTT Bridge")
	}
	reconcilePersistedMappings(&device)
	return device
}

// reconcilePersistedMappings upgrades cached commands whenever canonical
// Jeedom mappings evolve. Without this pass, a bridge restart would keep old
// fallback names and Home Assistant components until every quiet command
// emitted a fresh event or Jeedom discovery was forced manually.
func reconcilePersistedMappings(device *Device) {
	if device == nil {
		return
	}
	commandIDs := make([]string, 0, len(device.RawCommands))
	for commandID := range device.RawCommands {
		commandIDs = append(commandIDs, commandID)
	}
	sort.Strings(commandIDs)

	oldMetrics := make(map[string]struct{})
	for _, commandID := range commandIDs {
		command := device.RawCommands[commandID]
		if command.CommandID == "" {
			continue
		}
		event := Event{
			CommandID:   command.CommandID,
			LogicalID:   command.LogicalID,
			GenericType: command.GenericType,
			ObjectName:  command.ObjectName,
			DeviceName:  firstNonEmpty(command.Device, device.Device),
			CommandName: firstNonEmpty(command.RawName, command.Name),
			Name:        firstNonEmpty(command.RawName, command.Name),
			Type:        command.Type,
			Subtype:     command.Subtype,
			Unit:        command.Unit,
		}
		mapping := MappingFor(event)
		if mapping.fallback {
			// Legacy cache entries do not have stable logical or generic IDs. If
			// their display name was customized, keep the command-ID contract
			// already persisted instead of replacing it with a name-based metric.
			mapping = mappingFromCommandContract(command, mapping)
		}
		oldMetric := command.Metric
		if oldMetric != "" && oldMetric != mapping.Metric {
			oldMetrics[oldMetric] = struct{}{}
		}
		command.Name = EnglishCommandName(firstNonEmpty(command.RawName, command.Name), mapping, command.CommandID)
		command.Metric = mapping.Metric
		command.Component = mapping.Component
		command.Unit = mapping.Unit
		command.DeviceClass = mapping.DeviceClass
		command.StateClass = mapping.StateClass
		command.EntityCategory = mapping.EntityCategory
		if command.Value != nil {
			if value, ok := mappedAnyValue(command.Value, mapping, deviceTypeForNormalization(*device)); ok {
				command.Value = value
				device.Values[mapping.Metric] = value
				applyDerivedValuesFromEventCode(mapping, device, value, command.LastValueAt)
				applyDerivedWallSwitchStateFromLoad(mapping, device, value)
				applyDerivedWaterStopState(mapping, device, value)
			}
		}
		device.RawCommands[commandID] = command
	}
	for metric := range oldMetrics {
		deleteUnreferencedMetric(device, metric)
	}
}
