package jeedom

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const Source = "jeedom"

type EmptyValuePolicy string

const (
	EmptyValueKeepLast EmptyValuePolicy = "keep_last"
	EmptyValueUnknown  EmptyValuePolicy = "unknown"
)

type Store struct {
	mu                  sync.RWMutex
	saveMu              sync.Mutex
	policy              EmptyValuePolicy
	resolver            IdentityResolver
	path                string
	devices             map[string]*Device
	commands            map[string]string
	eqLogics            map[string]string
	baseGroups          map[string][]string
	audits              []ControlAudit
	nextPublishRevision uint64
}

type Device struct {
	Source                         string             `json:"source"`
	ObjectName                     string             `json:"object"`
	Device                         string             `json:"device"`
	DeviceSlug                     string             `json:"device_slug"`
	BaseSlug                       string             `json:"base_slug,omitempty"`
	LastUpdate                     time.Time          `json:"last_update"`
	Values                         map[string]any     `json:"values"`
	RawCommands                    map[string]Command `json:"raw_commands"`
	HAIdentifiers                  []string           `json:"ha_identifiers,omitempty"`
	HAManufacturer                 string             `json:"ha_manufacturer,omitempty"`
	HAModel                        string             `json:"ha_model,omitempty"`
	SuggestedArea                  string             `json:"suggested_area,omitempty"`
	LegacyDeviceSlugs              []string           `json:"legacy_device_slugs,omitempty"`
	LinkedSource                   string             `json:"linked_source,omitempty"`
	LinkedAccount                  string             `json:"linked_account,omitempty"`
	LinkedZone                     string             `json:"linked_zone,omitempty"`
	DiscoveryDisabled              bool               `json:"discovery_disabled,omitempty"`
	JeedomID                       string             `json:"jeedom_id,omitempty"`
	JeedomLogicalID                string             `json:"jeedom_logical_id,omitempty"`
	JeedomDeviceType               string             `json:"jeedom_device_type,omitempty"`
	JeedomEnabled                  bool               `json:"jeedom_enabled,omitempty"`
	JeedomVisible                  bool               `json:"jeedom_visible,omitempty"`
	Actions                        map[string]Action  `json:"actions,omitempty"`
	PendingDiscoveryCleanups       []Command          `json:"pending_discovery_cleanups,omitempty"`
	PendingActionDiscoveryCleanups []Action           `json:"pending_action_discovery_cleanups,omitempty"`
	publishRevision                uint64
}

type Command struct {
	CommandID      string    `json:"command_id"`
	EqLogicID      string    `json:"eq_logic_id,omitempty"`
	ObjectName     string    `json:"object"`
	Device         string    `json:"device"`
	DeviceSlug     string    `json:"device_slug"`
	Name           string    `json:"name"`
	RawName        string    `json:"raw_name,omitempty"`
	Metric         string    `json:"metric"`
	Component      string    `json:"component"`
	Topic          string    `json:"topic"`
	Type           string    `json:"type"`
	Subtype        string    `json:"subtype"`
	Unit           string    `json:"unit,omitempty"`
	DeviceClass    string    `json:"device_class,omitempty"`
	StateClass     string    `json:"state_class,omitempty"`
	EntityCategory string    `json:"entity_category,omitempty"`
	LogicalID      string    `json:"logical_id,omitempty"`
	GenericType    string    `json:"generic_type,omitempty"`
	Visible        bool      `json:"visible,omitempty"`
	Historized     bool      `json:"historized,omitempty"`
	Value          any       `json:"value,omitempty"`
	LastUpdate     time.Time `json:"last_update"`
	LastValueAt    time.Time `json:"last_value_at,omitempty"`
	EmptyValue     bool      `json:"empty_value,omitempty"`
}

type Action struct {
	Action            string    `json:"action"`
	CommandID         string    `json:"command_id"`
	Device            string    `json:"device"`
	DeviceSlug        string    `json:"device_slug"`
	DeviceType        string    `json:"device_type,omitempty"`
	EqLogicID         string    `json:"eq_logic_id,omitempty"`
	Name              string    `json:"name"`
	RawName           string    `json:"raw_name,omitempty"`
	LogicalID         string    `json:"logical_id,omitempty"`
	Subtype           string    `json:"subtype,omitempty"`
	StateCommandID    string    `json:"state_command_id,omitempty"`
	Allowed           bool      `json:"allowed"`
	DenyReason        string    `json:"deny_reason,omitempty"`
	LastRequestedAt   time.Time `json:"last_requested_at,omitempty"`
	LastRequestSource string    `json:"last_request_source,omitempty"`
}

type ControlAudit struct {
	Time       time.Time `json:"time"`
	Source     string    `json:"source"`
	Device     string    `json:"device"`
	DeviceSlug string    `json:"device_slug"`
	Action     string    `json:"action"`
	CommandID  string    `json:"command_id"`
	Topic      string    `json:"topic"`
	Result     string    `json:"result"`
	Error      string    `json:"error,omitempty"`
}

type ApplyResult struct {
	Device       Device
	Command      Command
	Mapping      Mapping
	EmptyValue   bool
	UpdatedValue bool
	NumericValue float64
	HasNumeric   bool
}

type ApplyDiscoveryResult struct {
	Device          Device
	Actions         []Action
	RemovedCommands []Command
	RemovedActions  []Action
}

type IdentityResolver interface {
	Resolve(evt Event, mapping Mapping) DeviceIdentity
}

type DiscoveryIdentityResolver interface {
	ResolveDiscovery(discovery Discovery) DeviceIdentity
}

type DeviceIdentity struct {
	DeviceSlug        string
	DeviceName        string
	BaseSlug          string
	HAIdentifiers     []string
	HAManufacturer    string
	HAModel           string
	SuggestedArea     string
	LegacyDeviceSlugs []string
	LinkedSource      string
	LinkedAccount     string
	LinkedZone        string
	DiscoveryDisabled bool
}

func NewStore(policy string) *Store {
	return NewStoreWithResolver(policy, nil)
}

func NewStoreWithResolver(policy string, resolver IdentityResolver) *Store {
	return &Store{
		policy:     NormalizeEmptyValuePolicy(policy),
		resolver:   resolver,
		devices:    make(map[string]*Device),
		commands:   make(map[string]string),
		eqLogics:   make(map[string]string),
		baseGroups: make(map[string][]string),
	}
}

func NormalizeEmptyValuePolicy(policy string) EmptyValuePolicy {
	switch EmptyValuePolicy(strings.TrimSpace(policy)) {
	case EmptyValueUnknown:
		return EmptyValueUnknown
	default:
		return EmptyValueKeepLast
	}
}

func (s *Store) Apply(evt Event) ApplyResult {
	mapping := MappingFor(evt)
	now := evt.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	knownDeviceSlug := s.commands[evt.CommandID]
	if knownDevice := s.devices[knownDeviceSlug]; knownDevice != nil {
		if knownCommand, ok := knownDevice.RawCommands[evt.CommandID]; ok {
			mapping = mappingFromCommandContract(knownCommand, mapping)
		}
	}
	identity := s.identityFor(evt, mapping)
	deviceSlug := identity.DeviceSlug
	device := s.devices[deviceSlug]
	if device == nil {
		device = &Device{
			Source:      Source,
			DeviceSlug:  deviceSlug,
			BaseSlug:    identity.BaseSlug,
			Values:      make(map[string]any),
			RawCommands: make(map[string]Command),
			Actions:     make(map[string]Action),
		}
		s.devices[deviceSlug] = device
		if identity.BaseSlug != "" && !containsString(s.baseGroups[identity.BaseSlug], deviceSlug) {
			s.baseGroups[identity.BaseSlug] = append(s.baseGroups[identity.BaseSlug], deviceSlug)
		}
	}
	device.Source = Source
	device.ObjectName = evt.ObjectName
	device.Device = firstNonEmpty(identity.DeviceName, evt.DeviceName)
	device.BaseSlug = identity.BaseSlug
	device.LastUpdate = now
	device.HAIdentifiers = append([]string(nil), identity.HAIdentifiers...)
	device.HAManufacturer = identity.HAManufacturer
	device.HAModel = identity.HAModel
	device.SuggestedArea = identity.SuggestedArea
	device.LegacyDeviceSlugs = mergeStringLists(device.LegacyDeviceSlugs, identity.LegacyDeviceSlugs)
	device.LinkedSource = identity.LinkedSource
	device.LinkedAccount = identity.LinkedAccount
	device.LinkedZone = identity.LinkedZone
	device.DiscoveryDisabled = identity.DiscoveryDisabled
	s.mergeLegacyActionsLocked(device)

	command := Command{
		CommandID:      evt.CommandID,
		ObjectName:     evt.ObjectName,
		Device:         evt.DeviceName,
		DeviceSlug:     deviceSlug,
		Name:           EnglishCommandName(firstNonEmpty(evt.CommandName, evt.Name), mapping, evt.CommandID),
		RawName:        firstNonEmpty(evt.CommandName, evt.Name),
		Metric:         mapping.Metric,
		Component:      mapping.Component,
		Topic:          evt.Topic,
		Type:           evt.Type,
		Subtype:        evt.Subtype,
		Unit:           mapping.Unit,
		DeviceClass:    mapping.DeviceClass,
		StateClass:     mapping.StateClass,
		EntityCategory: mapping.EntityCategory,
		LastUpdate:     now,
	}
	existing, hasExisting := device.RawCommands[evt.CommandID]
	if hasExisting {
		command.LogicalID = existing.LogicalID
		command.GenericType = existing.GenericType
		command.Visible = existing.Visible
		command.Historized = existing.Historized
		command.EqLogicID = existing.EqLogicID
		command.Value = existing.Value
		command.LastValueAt = existing.LastValueAt
		command.EmptyValue = existing.EmptyValue
	}

	result := ApplyResult{
		Mapping:    mapping,
		EmptyValue: evt.EmptyValue(),
	}

	if result.EmptyValue {
		command.EmptyValue = true
		if s.policy == EmptyValueUnknown {
			device.Values[mapping.Metric] = nil
			command.Value = nil
			result.UpdatedValue = true
		}
	} else {
		value, ok := mappedValue(evt, mapping, deviceTypeForNormalization(*device))
		if ok {
			device.Values[mapping.Metric] = value
			command.Value = value
			command.LastValueAt = now
			command.EmptyValue = false
			result.UpdatedValue = true
			applyDerivedValuesFromEventCode(mapping, device, value, now)
			applyDerivedWallSwitchStateFromLoad(mapping, device, value)
			applyDerivedWaterStopState(mapping, device, value)
			if number, numeric := numericValue(value); numeric {
				result.NumericValue = number
				result.HasNumeric = true
			}
		}
	}

	device.RawCommands[evt.CommandID] = command
	if hasExisting && existing.Metric != "" && existing.Metric != command.Metric {
		deleteUnreferencedMetric(device, existing.Metric)
	}
	s.commands[evt.CommandID] = deviceSlug
	s.bumpDevicePublishRevisionLocked(device)
	result.Device = copyDevice(*device)
	result.Command = command
	return result
}

func (s *Store) ApplyDiscovery(discovery Discovery) ApplyDiscoveryResult {
	now := discovery.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	identity := s.identityForDiscovery(discovery)
	deviceSlug := identity.DeviceSlug
	device := s.devices[deviceSlug]
	if device == nil {
		device = &Device{
			Source:      Source,
			DeviceSlug:  deviceSlug,
			BaseSlug:    identity.BaseSlug,
			Values:      make(map[string]any),
			RawCommands: make(map[string]Command),
			Actions:     make(map[string]Action),
		}
		s.devices[deviceSlug] = device
		if identity.BaseSlug != "" && !containsString(s.baseGroups[identity.BaseSlug], deviceSlug) {
			s.baseGroups[identity.BaseSlug] = append(s.baseGroups[identity.BaseSlug], deviceSlug)
		}
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

	previousJeedomID := device.JeedomID
	device.Source = Source
	device.ObjectName = discovery.ObjectName
	device.Device = firstNonEmpty(identity.DeviceName, discovery.Name)
	device.BaseSlug = identity.BaseSlug
	device.LastUpdate = now
	device.HAIdentifiers = append([]string(nil), identity.HAIdentifiers...)
	device.HAManufacturer = identity.HAManufacturer
	device.HAModel = identity.HAModel
	device.SuggestedArea = identity.SuggestedArea
	device.LegacyDeviceSlugs = mergeStringLists(device.LegacyDeviceSlugs, identity.LegacyDeviceSlugs)
	device.LinkedSource = identity.LinkedSource
	device.LinkedAccount = identity.LinkedAccount
	device.LinkedZone = identity.LinkedZone
	device.DiscoveryDisabled = identity.DiscoveryDisabled
	device.JeedomID = discovery.EqLogicID
	device.JeedomLogicalID = discovery.LogicalID
	device.JeedomDeviceType = firstNonEmpty(discovery.DeviceType, discovery.ApplyDevice, identity.HAModel)
	device.JeedomEnabled = discovery.Enabled
	device.JeedomVisible = discovery.Visible
	if discovery.EqLogicID != "" {
		s.eqLogics[discovery.EqLogicID] = deviceSlug
	}
	s.mergeLegacyActionsLocked(device)

	currentCommandIDs := make(map[string]struct{}, len(discovery.InfoCommands))
	for _, info := range discovery.InfoCommands {
		currentCommandIDs[info.CommandID] = struct{}{}
	}
	removedCommands := make([]Command, 0)
	for commandID, command := range device.RawCommands {
		if command.CommandID == "" {
			continue
		}
		if _, current := currentCommandIDs[commandID]; current {
			continue
		}
		ownedByDiscovery := command.EqLogicID == discovery.EqLogicID
		legacyReplacement := command.EqLogicID == "" &&
			previousJeedomID == discovery.EqLogicID &&
			discoveryReplacesCommand(discovery, command)
		if !ownedByDiscovery && !legacyReplacement {
			continue
		}
		removedCommands = append(removedCommands, command)
		delete(device.RawCommands, commandID)
		delete(s.commands, commandID)
	}
	sort.Slice(removedCommands, func(i, j int) bool {
		return removedCommands[i].CommandID < removedCommands[j].CommandID
	})
	queueCommandCleanups(device, removedCommands)

	for _, info := range discovery.InfoCommands {
		mapping := MappingFor(Event{
			Topic:       "jeedom/cmd/event/" + info.CommandID,
			CommandID:   info.CommandID,
			LogicalID:   info.LogicalID,
			GenericType: info.GenericType,
			ObjectName:  discovery.ObjectName,
			DeviceName:  discovery.Name,
			CommandName: info.Name,
			Name:        info.Name,
			Type:        info.Type,
			Subtype:     info.Subtype,
			Unit:        info.Unit,
			ReceivedAt:  now,
		})
		command := Command{
			CommandID:      info.CommandID,
			EqLogicID:      firstNonEmpty(info.EqLogicID, discovery.EqLogicID),
			ObjectName:     discovery.ObjectName,
			Device:         discovery.Name,
			DeviceSlug:     deviceSlug,
			Name:           EnglishCommandName(info.Name, mapping, info.CommandID),
			RawName:        info.Name,
			Metric:         mapping.Metric,
			Component:      mapping.Component,
			Topic:          "jeedom/cmd/event/" + info.CommandID,
			Type:           info.Type,
			Subtype:        info.Subtype,
			Unit:           mapping.Unit,
			DeviceClass:    mapping.DeviceClass,
			StateClass:     mapping.StateClass,
			EntityCategory: mapping.EntityCategory,
			LogicalID:      info.LogicalID,
			GenericType:    info.GenericType,
			Visible:        info.Visible,
			Historized:     info.Historized,
			LastUpdate:     now,
		}
		existing, hasExisting := device.RawCommands[info.CommandID]
		if hasExisting {
			command.Value = existing.Value
			command.LastValueAt = existing.LastValueAt
			command.EmptyValue = existing.EmptyValue
			if existing.LastUpdate.After(command.LastUpdate) {
				command.LastUpdate = existing.LastUpdate
			}
			if existing.Value != nil {
				if value, ok := mappedAnyValue(existing.Value, mapping, deviceTypeForNormalization(*device)); ok {
					command.Value = value
					device.Values[mapping.Metric] = value
					applyDerivedWaterStopState(mapping, device, value)
				}
			}
		}
		if !EmptyRawValue(info.Value) && (!hasExisting || existing.LastValueAt.IsZero()) {
			if value, ok := mappedValue(Event{Value: info.Value}, mapping, deviceTypeForNormalization(*device)); ok {
				device.Values[mapping.Metric] = value
				command.Value = value
				command.LastValueAt = now
				command.EmptyValue = false
				applyDerivedValuesFromEventCode(mapping, device, value, now)
				applyDerivedWallSwitchStateFromLoad(mapping, device, value)
				applyDerivedWaterStopState(mapping, device, value)
			}
		}
		device.RawCommands[info.CommandID] = command
		if hasExisting && existing.Metric != "" && existing.Metric != command.Metric {
			deleteUnreferencedMetric(device, existing.Metric)
		}
		s.commands[info.CommandID] = deviceSlug
	}
	for _, command := range removedCommands {
		deleteUnreferencedMetric(device, command.Metric)
	}

	actions := make([]Action, 0, len(discovery.Actions))
	currentActionIDs := make(map[string]struct{}, len(discovery.Actions))
	for _, actionCommand := range discovery.Actions {
		currentActionIDs[actionCommand.CommandID] = struct{}{}
	}
	removedActions := make([]Action, 0)
	for actionName, action := range device.Actions {
		if action.EqLogicID != discovery.EqLogicID {
			continue
		}
		if _, current := currentActionIDs[action.CommandID]; current {
			continue
		}
		removedActions = append(removedActions, action)
		delete(device.Actions, actionName)
	}
	sort.Slice(removedActions, func(i, j int) bool {
		return removedActions[i].CommandID < removedActions[j].CommandID
	})
	queueActionCleanups(device, removedActions)
	for _, actionCommand := range discovery.Actions {
		actionName := normalizeDiscoveryAction(actionCommand)
		if actionName == "" {
			actionName = "cmd_" + actionCommand.CommandID
		}
		allowed, denyReason := controlAllowed(device.JeedomDeviceType, actionName)
		action := Action{
			Action:         actionName,
			CommandID:      actionCommand.CommandID,
			Device:         discovery.Name,
			DeviceSlug:     deviceSlug,
			DeviceType:     device.JeedomDeviceType,
			EqLogicID:      discovery.EqLogicID,
			Name:           EnglishActionName(actionName, actionCommand.Name),
			RawName:        actionCommand.Name,
			LogicalID:      actionCommand.LogicalID,
			Subtype:        actionCommand.Subtype,
			StateCommandID: actionCommand.StateCommandID,
			Allowed:        allowed,
			DenyReason:     denyReason,
		}
		if existing, ok := device.Actions[actionName]; ok {
			action.LastRequestedAt = existing.LastRequestedAt
			action.LastRequestSource = existing.LastRequestSource
		}
		device.Actions[actionName] = action
		actions = append(actions, action)
	}

	s.bumpDevicePublishRevisionLocked(device)
	if target := s.linkedTargetForLegacyLocked(deviceSlug); target != nil {
		for _, removed := range removedActions {
			for actionName, targetAction := range target.Actions {
				if targetAction.CommandID == removed.CommandID {
					delete(target.Actions, actionName)
				}
			}
		}
		s.copyActionsLocked(target, device)
		s.bumpDevicePublishRevisionLocked(target)
		resultDevice := copyDevice(*target)
		queueCommandCleanups(&resultDevice, removedCommands)
		queueActionCleanups(&resultDevice, removedActions)
		return ApplyDiscoveryResult{
			Device:          resultDevice,
			Actions:         actionsForDevice(target),
			RemovedCommands: removedCommands,
			RemovedActions:  removedActions,
		}
	}

	return ApplyDiscoveryResult{
		Device:          copyDevice(*device),
		Actions:         actions,
		RemovedCommands: removedCommands,
		RemovedActions:  removedActions,
	}
}

func (s *Store) ReconcileResolver(resolver IdentityResolver) []Device {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resolver = resolver
	if resolver != nil {
		slugs := make([]string, 0, len(s.devices))
		for slug := range s.devices {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			device := s.devices[slug]
			if device == nil {
				continue
			}
			identity := s.reconciledIdentityLocked(*device, resolver)
			if identity.DeviceSlug == "" {
				continue
			}
			s.mergeDeviceIntoIdentityLocked(slug, identity)
		}
		s.rebuildIndexesLocked()
	}
	s.bumpAllDevicePublishRevisionsLocked()
	return s.devicesLocked()
}

func (s *Store) identityFor(evt Event, mapping Mapping) DeviceIdentity {
	baseSlug := Slug(evt.DeviceName)
	identity := DeviceIdentity{
		DeviceName:     evt.DeviceName,
		DeviceSlug:     s.localDeviceSlug(evt, mapping, baseSlug),
		BaseSlug:       baseSlug,
		HAManufacturer: "Ajax via Jeedom",
		HAModel:        "Jeedom MQTT Bridge",
	}
	identity.HAIdentifiers = []string{"ajaxbridge_jeedom_" + identity.DeviceSlug}
	if s.resolver == nil {
		return identity
	}

	resolved := s.resolver.Resolve(evt, mapping)
	if resolved.DeviceSlug == "" {
		resolved.DeviceSlug = identity.DeviceSlug
	}
	if resolved.DeviceSlug != identity.DeviceSlug {
		resolved.LegacyDeviceSlugs = mergeStringLists(resolved.LegacyDeviceSlugs, []string{identity.DeviceSlug})
	}
	if resolved.DeviceName == "" {
		resolved.DeviceName = identity.DeviceName
	}
	if resolved.BaseSlug == "" {
		resolved.BaseSlug = identity.BaseSlug
	}
	if len(resolved.HAIdentifiers) == 0 {
		resolved.HAIdentifiers = identity.HAIdentifiers
	}
	if resolved.HAManufacturer == "" {
		resolved.HAManufacturer = identity.HAManufacturer
	}
	if resolved.HAModel == "" {
		resolved.HAModel = identity.HAModel
	}
	return resolved
}

func (s *Store) reconciledIdentityLocked(device Device, resolver IdentityResolver) DeviceIdentity {
	fallback := DeviceIdentity{
		DeviceSlug:        device.DeviceSlug,
		DeviceName:        device.Device,
		BaseSlug:          device.BaseSlug,
		HAIdentifiers:     append([]string(nil), device.HAIdentifiers...),
		HAManufacturer:    device.HAManufacturer,
		HAModel:           firstNonEmpty(device.JeedomDeviceType, device.HAModel),
		SuggestedArea:     device.SuggestedArea,
		LegacyDeviceSlugs: append([]string(nil), device.LegacyDeviceSlugs...),
		LinkedSource:      device.LinkedSource,
		LinkedAccount:     device.LinkedAccount,
		LinkedZone:        device.LinkedZone,
		DiscoveryDisabled: device.DiscoveryDisabled,
	}
	if discoveryResolver, ok := resolver.(DiscoveryIdentityResolver); ok {
		discovery := Discovery{
			EqLogicID:    device.JeedomID,
			Name:         device.Device,
			LogicalID:    device.JeedomLogicalID,
			ObjectName:   device.ObjectName,
			DeviceType:   firstNonEmpty(device.JeedomDeviceType, device.HAModel),
			ApplyDevice:  firstNonEmpty(device.JeedomDeviceType, device.HAModel),
			InfoCommands: make(map[string]DiscoveryCommand, len(device.RawCommands)),
			Actions:      make(map[string]DiscoveryCommand, len(device.Actions)),
		}
		for commandID, command := range device.RawCommands {
			discovery.InfoCommands[commandID] = DiscoveryCommand{
				CommandID:   commandID,
				EqLogicID:   command.EqLogicID,
				LogicalID:   command.LogicalID,
				GenericType: command.GenericType,
				Name:        firstNonEmpty(command.RawName, command.Name),
				Type:        command.Type,
				Subtype:     command.Subtype,
				Unit:        command.Unit,
				Historized:  command.Historized,
			}
		}
		for actionName, action := range device.Actions {
			discovery.Actions[action.CommandID] = DiscoveryCommand{
				CommandID: action.CommandID,
				Name:      firstNonEmpty(action.RawName, action.Name, actionName),
				Type:      "action",
				Subtype:   action.Subtype,
				LogicalID: action.LogicalID,
			}
		}
		return mergeIdentity(fallback, discoveryResolver.ResolveDiscovery(discovery))
	}

	commandIDs := make([]string, 0, len(device.RawCommands)+len(device.Actions))
	for commandID := range device.RawCommands {
		commandIDs = append(commandIDs, commandID)
	}
	for _, action := range device.Actions {
		if action.CommandID != "" {
			commandIDs = append(commandIDs, action.CommandID)
		}
	}
	sort.Strings(commandIDs)
	for _, commandID := range commandIDs {
		command := device.RawCommands[commandID]
		resolved := resolver.Resolve(Event{
			CommandID:   commandID,
			ObjectName:  device.ObjectName,
			DeviceName:  device.Device,
			CommandName: firstNonEmpty(command.RawName, command.Name),
			Type:        command.Type,
			Subtype:     command.Subtype,
			Unit:        command.Unit,
		}, Mapping{})
		if resolved.DeviceSlug != "" {
			return mergeIdentity(fallback, resolved)
		}
	}
	return fallback
}

func (s *Store) mergeDeviceIntoIdentityLocked(sourceSlug string, identity DeviceIdentity) {
	source := s.devices[sourceSlug]
	if source == nil || identity.DeviceSlug == "" {
		return
	}
	target := s.devices[identity.DeviceSlug]
	if target == nil {
		target = &Device{
			Source:      Source,
			DeviceSlug:  identity.DeviceSlug,
			Values:      make(map[string]any),
			RawCommands: make(map[string]Command),
			Actions:     make(map[string]Action),
		}
		s.devices[identity.DeviceSlug] = target
	}
	if target.Values == nil {
		target.Values = make(map[string]any)
	}
	if target.RawCommands == nil {
		target.RawCommands = make(map[string]Command)
	}
	if target.Actions == nil {
		target.Actions = make(map[string]Action)
	}

	for key, value := range source.Values {
		target.Values[key] = value
	}
	for commandID, command := range source.RawCommands {
		command.DeviceSlug = identity.DeviceSlug
		command.Device = firstNonEmpty(identity.DeviceName, source.Device, command.Device)
		target.RawCommands[commandID] = command
	}
	for actionName, action := range source.Actions {
		action.DeviceSlug = identity.DeviceSlug
		action.Device = firstNonEmpty(identity.DeviceName, source.Device, action.Device)
		action.DeviceType = firstNonEmpty(identity.HAModel, source.JeedomDeviceType, source.HAModel, action.DeviceType)
		target.Actions[actionName] = action
	}
	queueCommandCleanups(target, source.PendingDiscoveryCleanups)
	queueActionCleanups(target, source.PendingActionDiscoveryCleanups)

	target.Source = Source
	target.DeviceSlug = identity.DeviceSlug
	target.Device = firstNonEmpty(identity.DeviceName, target.Device, source.Device)
	target.BaseSlug = firstNonEmpty(identity.BaseSlug, target.BaseSlug, source.BaseSlug)
	target.ObjectName = firstNonEmpty(source.ObjectName, target.ObjectName)
	if source.LastUpdate.After(target.LastUpdate) {
		target.LastUpdate = source.LastUpdate
	}
	target.HAIdentifiers = append([]string(nil), identity.HAIdentifiers...)
	target.HAManufacturer = firstNonEmpty(identity.HAManufacturer, target.HAManufacturer, source.HAManufacturer)
	target.HAModel = firstNonEmpty(identity.HAModel, target.HAModel, source.HAModel)
	target.SuggestedArea = firstNonEmpty(identity.SuggestedArea, target.SuggestedArea, source.SuggestedArea)
	target.LegacyDeviceSlugs = mergeStringLists(target.LegacyDeviceSlugs, source.LegacyDeviceSlugs)
	if sourceSlug != identity.DeviceSlug {
		target.LegacyDeviceSlugs = mergeStringLists(target.LegacyDeviceSlugs, []string{sourceSlug})
	}
	target.LegacyDeviceSlugs = mergeStringLists(target.LegacyDeviceSlugs, identity.LegacyDeviceSlugs)
	target.LinkedSource = identity.LinkedSource
	target.LinkedAccount = identity.LinkedAccount
	target.LinkedZone = identity.LinkedZone
	target.DiscoveryDisabled = identity.DiscoveryDisabled
	target.JeedomID = firstNonEmpty(source.JeedomID, target.JeedomID)
	target.JeedomLogicalID = firstNonEmpty(source.JeedomLogicalID, target.JeedomLogicalID)
	target.JeedomDeviceType = firstNonEmpty(source.JeedomDeviceType, identity.HAModel, target.JeedomDeviceType)
	target.JeedomEnabled = source.JeedomEnabled || target.JeedomEnabled
	target.JeedomVisible = source.JeedomVisible || target.JeedomVisible

	if sourceSlug != identity.DeviceSlug {
		delete(s.devices, sourceSlug)
	}
}

func (s *Store) rebuildIndexesLocked() {
	s.commands = make(map[string]string)
	s.eqLogics = make(map[string]string)
	s.baseGroups = make(map[string][]string)
	for slug, device := range s.devices {
		if device == nil {
			continue
		}
		device.DeviceSlug = slug
		if device.BaseSlug != "" && !containsString(s.baseGroups[device.BaseSlug], slug) {
			s.baseGroups[device.BaseSlug] = append(s.baseGroups[device.BaseSlug], slug)
		}
		if device.JeedomID != "" {
			s.eqLogics[device.JeedomID] = slug
		}
		for commandID, command := range device.RawCommands {
			command.DeviceSlug = slug
			command.Device = firstNonEmpty(device.Device, command.Device)
			device.RawCommands[commandID] = command
			s.commands[commandID] = slug
		}
		for actionName, action := range device.Actions {
			device.Actions[actionName] = actionForDevice(action, device)
		}
		sort.Strings(s.baseGroups[device.BaseSlug])
	}
	s.ensureDevicePublishRevisionsLocked()
}

func (s *Store) identityForDiscovery(discovery Discovery) DeviceIdentity {
	baseSlug := Slug(discovery.Name)
	identity := DeviceIdentity{
		DeviceName:     discovery.Name,
		BaseSlug:       baseSlug,
		HAManufacturer: "Ajax via Jeedom",
		HAModel:        firstNonEmpty(discovery.DeviceType, discovery.ApplyDevice, "Jeedom MQTT Bridge"),
	}
	if current, ok := s.eqLogics[discovery.EqLogicID]; ok && current != "" {
		identity.DeviceSlug = current
	} else {
		identity.DeviceSlug = s.localDiscoveryDeviceSlug(discovery, baseSlug)
	}
	identity.HAIdentifiers = []string{"ajaxbridge_jeedom_" + identity.DeviceSlug}
	if s.resolver == nil {
		return identity
	}
	if resolver, ok := s.resolver.(DiscoveryIdentityResolver); ok {
		resolved := resolver.ResolveDiscovery(discovery)
		return mergeIdentity(identity, resolved)
	}
	return mergeIdentity(identity, s.resolver.Resolve(Event{
		Topic:       discovery.Topic,
		CommandID:   firstDiscoveryCommandID(discovery),
		ObjectName:  discovery.ObjectName,
		DeviceName:  discovery.Name,
		CommandName: firstDiscoveryCommandName(discovery),
		ReceivedAt:  discovery.ReceivedAt,
	}, Mapping{}))
}

func (s *Store) localDeviceSlug(evt Event, mapping Mapping, baseSlug string) string {
	if current, ok := s.commands[evt.CommandID]; ok {
		return current
	}
	groups := s.baseGroups[baseSlug]
	if len(groups) == 0 {
		return baseSlug
	}
	for _, group := range slices.Backward(groups) {
		device := s.devices[group]
		if device == nil || !deviceHasMetric(device, mapping.Metric) {
			return group
		}
	}
	suffix := Slug(evt.CommandID)
	if suffix == "" || suffix == "unknown" {
		suffix = shortHash(evt.Topic)
	}
	return baseSlug + "_" + suffix
}

func deviceHasMetric(device *Device, metric string) bool {
	if device == nil || metric == "" {
		return false
	}
	for _, command := range device.RawCommands {
		if command.Metric == metric {
			return true
		}
	}
	return false
}

func mappedValue(evt Event, mapping Mapping, deviceType string) (any, bool) {
	switch {
	case mapping.Timestamp:
		return timestampRawValue(evt.Value)
	case mapping.Numeric:
		value, ok := NumericRawValue(evt.Value)
		if !ok || !finiteNumericValue(value) {
			return 0, false
		}
		return normalizeNumericValue(mapping, deviceType, value), true
	case mapping.Binary:
		return BoolRawValueForMapping(evt.Value, mapping)
	default:
		return StringRawValue(evt.Value)
	}
}

func mappingFromCommandContract(command Command, fallback Mapping) Mapping {
	if command.Metric == "" || command.Component == "" {
		return fallback
	}
	mapping := Mapping{
		Metric:         command.Metric,
		Component:      command.Component,
		EntityName:     firstNonEmpty(command.Name, fallback.EntityName),
		Unit:           command.Unit,
		DeviceClass:    command.DeviceClass,
		StateClass:     command.StateClass,
		EntityCategory: command.EntityCategory,
	}
	mapping.Timestamp = command.Component == ComponentSensor && strings.EqualFold(command.DeviceClass, "timestamp")
	mapping.Binary = command.Component == ComponentBinarySensor
	if !mapping.Timestamp && !mapping.Binary {
		switch strings.ToLower(strings.TrimSpace(command.Subtype)) {
		case "numeric":
			mapping.Numeric = true
		case "string", "other":
			mapping.Numeric = false
		default:
			mapping.Numeric = fallback.Numeric
		}
	}
	return mapping
}

func mappedAnyValue(value any, mapping Mapping, deviceType string) (any, bool) {
	if value == nil {
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return mappedValue(Event{Value: raw}, mapping, deviceType)
}

func timestampRawValue(value json.RawMessage) (string, bool) {
	if number, ok := NumericRawValue(value); ok && finiteNumericValue(number) {
		seconds, nanoseconds := unixTimestampParts(number)
		if seconds <= 0 {
			return "", false
		}
		return time.Unix(seconds, nanoseconds).UTC().Format(time.RFC3339Nano), true
	}
	text, ok := StringRawValue(value)
	if !ok {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339Nano), true
}

func unixTimestampParts(value float64) (int64, int64) {
	abs := math.Abs(value)
	scale := float64(1)
	switch {
	case abs >= 1e17:
		scale = 1e9
	case abs >= 1e14:
		scale = 1e6
	case abs >= 1e11:
		scale = 1e3
	}
	secondsFloat := value / scale
	seconds := int64(secondsFloat)
	nanoseconds := int64((secondsFloat - float64(seconds)) * float64(time.Second))
	return seconds, nanoseconds
}

func deleteUnreferencedMetric(device *Device, metric string) {
	if device == nil || strings.TrimSpace(metric) == "" {
		return
	}
	for _, command := range device.RawCommands {
		if command.Metric == metric {
			return
		}
	}
	delete(device.Values, metric)
}

func queueCommandCleanups(device *Device, commands []Command) {
	if device == nil || len(commands) == 0 {
		return
	}
	byID := make(map[string]Command, len(device.PendingDiscoveryCleanups)+len(commands))
	for _, command := range device.PendingDiscoveryCleanups {
		if command.CommandID != "" {
			byID[command.CommandID] = command
		}
	}
	for _, command := range commands {
		if command.CommandID != "" {
			byID[command.CommandID] = command
		}
	}
	ids := make([]string, 0, len(byID))
	for commandID := range byID {
		ids = append(ids, commandID)
	}
	sort.Strings(ids)
	device.PendingDiscoveryCleanups = make([]Command, 0, len(ids))
	for _, commandID := range ids {
		device.PendingDiscoveryCleanups = append(device.PendingDiscoveryCleanups, byID[commandID])
	}
}

func queueActionCleanups(device *Device, actions []Action) {
	if device == nil || len(actions) == 0 {
		return
	}
	byID := make(map[string]Action, len(device.PendingActionDiscoveryCleanups)+len(actions))
	for _, action := range device.PendingActionDiscoveryCleanups {
		if action.CommandID != "" {
			byID[action.CommandID] = action
		}
	}
	for _, action := range actions {
		if action.CommandID != "" {
			byID[action.CommandID] = action
		}
	}
	ids := make([]string, 0, len(byID))
	for commandID := range byID {
		ids = append(ids, commandID)
	}
	sort.Strings(ids)
	device.PendingActionDiscoveryCleanups = make([]Action, 0, len(ids))
	for _, commandID := range ids {
		device.PendingActionDiscoveryCleanups = append(device.PendingActionDiscoveryCleanups, byID[commandID])
	}
}

// discoveryReplacesCommand identifies pre-provenance cache entries that have
// been recreated by Jeedom under a new command id. It intentionally requires
// the same source object and command signature: a canonical Ajax device can
// contain commands merged from more than one Jeedom eqLogic, and those must
// never be removed just because they are absent from this discovery payload.
func discoveryReplacesCommand(discovery Discovery, command Command) bool {
	if command.CommandID == "" {
		return false
	}
	if command.ObjectName != "" && discovery.ObjectName != "" &&
		commandKey(command.ObjectName) != commandKey(discovery.ObjectName) {
		return false
	}
	if command.Device != "" && discovery.Name != "" &&
		commandKey(command.Device) != commandKey(discovery.Name) {
		return false
	}
	commandName := commandKey(firstNonEmpty(command.RawName, command.Name))
	for _, candidate := range discovery.InfoCommands {
		if candidate.CommandID == "" || candidate.CommandID == command.CommandID {
			continue
		}
		if command.LogicalID != "" && candidate.LogicalID != "" &&
			command.LogicalID == candidate.LogicalID {
			return true
		}
		if commandName == "" || commandName != commandKey(candidate.Name) {
			continue
		}
		if command.Type != "" && candidate.Type != "" &&
			!strings.EqualFold(command.Type, candidate.Type) {
			continue
		}
		if command.Subtype != "" && candidate.Subtype != "" &&
			!strings.EqualFold(command.Subtype, candidate.Subtype) {
			continue
		}
		return true
	}
	return false
}

func applyDerivedValuesFromEventCode(mapping Mapping, device *Device, value any, now time.Time) {
	if device == nil {
		return
	}
	if state, ok := derivedStateFromEventCode(mapping, *device, value); ok {
		device.Values["state"] = state
	}
	if gridPower, ok := derivedGridPowerFromEventCode(mapping, *device, value); ok {
		device.Values["grid_power"] = gridPower
		ensureSyntheticGridPowerCommand(device, gridPower, now)
	}
}

// WallSwitch does not expose an Etat/realState command in the Jeedom Ajax
// plugin. Positive load is nevertheless conclusive evidence that its relay is
// on and lets a fresh discovery seed the state without physically toggling it.
// Zero load is deliberately not treated as off because an enabled relay may
// have no active consumer.
func applyDerivedWallSwitchStateFromLoad(mapping Mapping, device *Device, value any) {
	if device == nil || !isWallSwitchDevice(*device) {
		return
	}
	switch mapping.Metric {
	case "current_a", "power_w":
	default:
		return
	}
	load, ok := numericValue(value)
	if ok && load > 0 {
		device.Values["state"] = true
	}
}

func applyDerivedWaterStopState(mapping Mapping, device *Device, value any) {
	if device == nil || mapping.Metric != "valve_position" || !isWaterStopDevice(*device) {
		return
	}
	switch strings.ToUpper(strings.TrimSpace(fmt.Sprint(value))) {
	case "OPEN", "OPENED":
		device.Values["state"] = true
	case "CLOSED", "CLOSE":
		device.Values["state"] = false
	case "INTERMEDIATE", "OPENING", "CLOSING", "MOVING":
		device.Values["state"] = nil
	}
}

func derivedStateFromEventCode(mapping Mapping, device Device, value any) (bool, bool) {
	if mapping.Metric != "event_code" {
		return false, false
	}

	eventCode := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	if isWallSwitchDevice(device) {
		switch eventCode {
		case "M_1F_37":
			return true, true
		case "M_1F_46":
			return false, true
		}
	}
	if isWaterStopDevice(device) {
		switch eventCode {
		case "M_48_37":
			return true, true
		case "M_48_46":
			return false, true
		}
	}
	return false, false
}

func derivedGridPowerFromEventCode(mapping Mapping, device Device, value any) (bool, bool) {
	if mapping.Metric != "event_code" || !isTransmitterDevice(device) {
		return false, false
	}
	switch strings.ToUpper(strings.TrimSpace(fmt.Sprint(value))) {
	case "M_11_40":
		return true, true
	default:
		return false, false
	}
}

func ensureSyntheticGridPowerCommand(device *Device, value bool, now time.Time) {
	const metric = "grid_power"
	command := device.RawCommands[metric]
	command.Device = device.Device
	command.DeviceSlug = device.DeviceSlug
	command.Name = "Grid power"
	command.RawName = "Grid power"
	command.Metric = metric
	command.Component = ComponentBinarySensor
	command.Type = "info"
	command.Subtype = "binary"
	command.DeviceClass = "power"
	command.LastUpdate = now
	command.LastValueAt = now
	command.Value = value
	command.EmptyValue = false
	device.RawCommands[metric] = command
}

func isWallSwitchDevice(device Device) bool {
	switch commandKey(firstNonEmpty(device.JeedomDeviceType, device.HAModel)) {
	case "wallswitch":
		return true
	default:
		return false
	}
}

func isTransmitterDevice(device Device) bool {
	deviceType := commandKey(firstNonEmpty(device.JeedomDeviceType, device.HAModel))
	return strings.Contains(deviceType, "transmitter") && !strings.Contains(deviceType, "multitransmitter")
}

func isWaterStopDevice(device Device) bool {
	switch commandKey(firstNonEmpty(device.JeedomDeviceType, device.HAModel)) {
	case "waterstop", "valve":
		return true
	default:
		return false
	}
}

func normalizeNumericValue(mapping Mapping, deviceType string, value float64) float64 {
	if mapping.Metric == "voltage_v" && commandKey(deviceType) == "relay" {
		return value / 10
	}
	return value
}

func deviceTypeForNormalization(device Device) string {
	return firstNonEmpty(device.JeedomDeviceType, device.HAModel)
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, finiteNumericValue(typed)
	case float32:
		number := float64(typed)
		return number, finiteNumericValue(number)
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	default:
		return 0, false
	}
}

func finiteNumericValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *Store) bumpDevicePublishRevisionLocked(device *Device) {
	if s == nil || device == nil {
		return
	}
	s.flattenDeviceLegacySlugsLocked(device)
	s.nextPublishRevision++
	if s.nextPublishRevision == 0 {
		s.nextPublishRevision = 1
	}
	device.publishRevision = s.nextPublishRevision
}

func (s *Store) flattenDeviceLegacySlugsLocked(device *Device) {
	if s == nil || device == nil || len(device.LegacyDeviceSlugs) == 0 {
		return
	}
	root := Slug(device.DeviceSlug)
	seen := map[string]bool{root: true}
	flattened := make([]string, 0, len(device.LegacyDeviceSlugs))
	var visit func(string)
	visit = func(slug string) {
		slug = Slug(slug)
		if slug == "" || slug == "unknown" || seen[slug] {
			return
		}
		seen[slug] = true
		flattened = append(flattened, slug)
		if legacy := s.devices[slug]; legacy != nil {
			for _, dependency := range legacy.LegacyDeviceSlugs {
				visit(dependency)
			}
		}
	}
	for _, slug := range device.LegacyDeviceSlugs {
		visit(slug)
	}
	device.LegacyDeviceSlugs = compactUniqueStrings(flattened)
	sort.Strings(device.LegacyDeviceSlugs)
}

func (s *Store) ensureDevicePublishRevisionsLocked() {
	if s == nil {
		return
	}
	for _, device := range s.devices {
		if device != nil && device.publishRevision > s.nextPublishRevision {
			s.nextPublishRevision = device.publishRevision
		}
	}
	for _, slug := range s.devicePublishRevisionOrderLocked() {
		device := s.devices[slug]
		if device != nil && device.publishRevision == 0 {
			s.bumpDevicePublishRevisionLocked(device)
		}
	}
}

func (s *Store) bumpAllDevicePublishRevisionsLocked() {
	if s == nil {
		return
	}
	for _, slug := range s.devicePublishRevisionOrderLocked() {
		s.bumpDevicePublishRevisionLocked(s.devices[slug])
	}
}

func (s *Store) devicePublishRevisionOrderLocked() []string {
	slugs := make([]string, 0, len(s.devices))
	for slug, device := range s.devices {
		if device != nil {
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)

	// A canonical device must receive a later revision than any persisted
	// device slug it owns and cleans. DFS gives that dependency order even for
	// legacy chains (C, then B->C, then A->B).
	ordered := make([]string, 0, len(slugs))
	visiting := make(map[string]bool, len(slugs))
	visited := make(map[string]bool, len(slugs))
	var visit func(string)
	visit = func(slug string) {
		if visited[slug] || visiting[slug] {
			return
		}
		device := s.devices[slug]
		if device == nil {
			return
		}
		visiting[slug] = true
		legacy := compactUniqueStrings(device.LegacyDeviceSlugs)
		sort.Strings(legacy)
		for _, dependency := range legacy {
			if dependency != slug {
				visit(dependency)
			}
		}
		visiting[slug] = false
		visited[slug] = true
		ordered = append(ordered, slug)
	}
	for _, slug := range slugs {
		visit(slug)
	}
	return ordered
}

func (s *Store) Devices() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.devicesLocked()
}

// AcknowledgeCommandCleanups removes persisted retained-discovery tombstones
// after the MQTT broker accepted them. Keeping this queue in the cache makes a
// bridge restart or transient broker failure retry cleanup instead of leaving
// orphaned Home Assistant entities forever.
func (s *Store) AcknowledgeCommandCleanups(commands []Command) bool {
	if s == nil || len(commands) == 0 {
		return false
	}
	acknowledged := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		if command.CommandID != "" {
			acknowledged[command.CommandID] = struct{}{}
		}
	}
	if len(acknowledged) == 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, device := range s.devices {
		if device == nil || len(device.PendingDiscoveryCleanups) == 0 {
			continue
		}
		kept := device.PendingDiscoveryCleanups[:0]
		for _, pending := range device.PendingDiscoveryCleanups {
			if _, ok := acknowledged[pending.CommandID]; ok {
				changed = true
				continue
			}
			kept = append(kept, pending)
		}
		device.PendingDiscoveryCleanups = kept
	}
	return changed
}

func (s *Store) AcknowledgeActionCleanups(actions []Action) bool {
	if s == nil || len(actions) == 0 {
		return false
	}
	acknowledged := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		if action.CommandID != "" {
			acknowledged[action.CommandID] = struct{}{}
		}
	}
	if len(acknowledged) == 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, device := range s.devices {
		if device == nil || len(device.PendingActionDiscoveryCleanups) == 0 {
			continue
		}
		kept := device.PendingActionDiscoveryCleanups[:0]
		for _, pending := range device.PendingActionDiscoveryCleanups {
			if _, ok := acknowledged[pending.CommandID]; ok {
				changed = true
				continue
			}
			kept = append(kept, pending)
		}
		device.PendingActionDiscoveryCleanups = kept
	}
	return changed
}

func (s *Store) devicesLocked() []Device {
	devices := make([]Device, 0, len(s.devices))
	for _, device := range s.devices {
		devices = append(devices, copyDevice(*device))
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].DeviceSlug < devices[j].DeviceSlug
	})
	return devices
}

func (s *Store) Device(slug string) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	device, ok := s.devices[slug]
	if !ok {
		return Device{}, false
	}
	return copyDevice(*device), true
}

func (s *Store) Commands() []Command {
	s.mu.RLock()
	defer s.mu.RUnlock()

	commands := make([]Command, 0, len(s.commands))
	for commandID, deviceSlug := range s.commands {
		device := s.devices[deviceSlug]
		if device == nil {
			continue
		}
		command, ok := device.RawCommands[commandID]
		if ok {
			commands = append(commands, command)
		}
	}
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].CommandID < commands[j].CommandID
	})
	return commands
}

func (s *Store) Actions() []Action {
	s.mu.RLock()
	defer s.mu.RUnlock()

	actions := make([]Action, 0)
	for _, device := range s.devices {
		if device == nil {
			continue
		}
		for _, action := range device.Actions {
			actions = append(actions, action)
		}
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].DeviceSlug == actions[j].DeviceSlug {
			return actions[i].Action < actions[j].Action
		}
		return actions[i].DeviceSlug < actions[j].DeviceSlug
	})
	return actions
}

func (s *Store) Action(deviceSlug, actionName string) (Action, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deviceSlug = Slug(deviceSlug)
	device := s.devices[deviceSlug]
	if device == nil {
		return Action{}, false
	}
	actionName = NormalizeControlAction(actionName)
	if action, ok := device.Actions[actionName]; ok {
		return action, true
	}
	for _, legacySlug := range device.LegacyDeviceSlugs {
		legacy := s.devices[Slug(legacySlug)]
		if legacy == nil {
			continue
		}
		if action, ok := legacy.Actions[actionName]; ok {
			return actionForDevice(action, device), true
		}
	}
	return Action{}, false
}

func (s *Store) ActionByCommandID(commandID string) (Action, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return Action{}, false
	}
	for _, device := range s.devices {
		if device == nil {
			continue
		}
		for _, action := range device.Actions {
			if action.CommandID == commandID {
				return action, true
			}
		}
	}
	return Action{}, false
}

func (s *Store) RecordControl(action Action, source, topic string, err error) {
	if s == nil {
		return
	}
	now := time.Now().UTC()
	result := "published"
	errText := ""
	if err != nil {
		result = "failed"
		errText = err.Error()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if device := s.devices[action.DeviceSlug]; device != nil && device.Actions != nil {
		if current, ok := device.Actions[action.Action]; ok {
			current.LastRequestedAt = now
			current.LastRequestSource = source
			device.Actions[action.Action] = current
			s.bumpDevicePublishRevisionLocked(device)
		}
	}
	s.audits = append(s.audits, ControlAudit{
		Time:       now,
		Source:     source,
		Device:     action.Device,
		DeviceSlug: action.DeviceSlug,
		Action:     action.Action,
		CommandID:  action.CommandID,
		Topic:      topic,
		Result:     result,
		Error:      errText,
	})
	if len(s.audits) > 200 {
		s.audits = append([]ControlAudit(nil), s.audits[len(s.audits)-200:]...)
	}
}

// RecordOptimisticControlState records the requested state only for toggle
// devices that have no Jeedom state command. Devices with real feedback remain
// authoritative and are updated by their info command instead.
func (s *Store) RecordOptimisticControlState(action Action, at time.Time) (Device, bool) {
	if s == nil || strings.TrimSpace(action.StateCommandID) != "" {
		return Device{}, false
	}
	var stateValue bool
	switch NormalizeControlAction(action.Action) {
	case "on":
		stateValue = true
	case "off":
		stateValue = false
	default:
		return Device{}, false
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	device := s.devices[Slug(action.DeviceSlug)]
	if device == nil || !toggleCapableDevice(*device) {
		return Device{}, false
	}
	if device.Values == nil {
		device.Values = make(map[string]any)
	}
	device.Values["state"] = stateValue
	device.LastUpdate = at
	s.bumpDevicePublishRevisionLocked(device)
	return copyDevice(*device), true
}

func (s *Store) HasRecentBridgeControl(commandID string, window time.Duration) bool {
	if s == nil {
		return false
	}
	commandID = strings.TrimSpace(commandID)
	if commandID == "" || window <= 0 {
		return false
	}
	cutoff := time.Now().UTC().Add(-window)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, audit := range slices.Backward(s.audits) {
		if audit.Time.Before(cutoff) {
			return false
		}
		if audit.CommandID != commandID {
			continue
		}
		if strings.HasPrefix(audit.Source, "http:") || strings.HasPrefix(audit.Source, "mqtt:") {
			return true
		}
	}
	return false
}

func (s *Store) ControlAudit(limit int) []ControlAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.audits) {
		limit = len(s.audits)
	}
	out := make([]ControlAudit, 0, limit)
	for i := len(s.audits) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.audits[i])
	}
	return out
}

func StatePayload(device Device) map[string]any {
	payload := map[string]any{
		"source":       Source,
		"object":       device.ObjectName,
		"device":       device.Device,
		"device_slug":  device.DeviceSlug,
		"last_update":  mqttTime(device.LastUpdate),
		"raw_commands": device.RawCommands,
	}
	if device.JeedomID != "" {
		payload["jeedom_id"] = device.JeedomID
	}
	if device.JeedomLogicalID != "" {
		payload["jeedom_logical_id"] = device.JeedomLogicalID
	}
	if device.JeedomDeviceType != "" {
		payload["jeedom_device_type"] = device.JeedomDeviceType
	}
	if len(device.Actions) > 0 {
		payload["actions"] = device.Actions
	}
	for metric, value := range device.Values {
		payload[metric] = value
	}
	return payload
}

func AttributesPayload(device Device) map[string]any {
	payload := map[string]any{
		"source":      Source,
		"object":      device.ObjectName,
		"device":      device.Device,
		"device_slug": device.DeviceSlug,
	}
	if device.JeedomID != "" {
		payload["jeedom_id"] = device.JeedomID
	}
	if device.JeedomLogicalID != "" {
		payload["jeedom_logical_id"] = device.JeedomLogicalID
	}
	if device.JeedomDeviceType != "" {
		payload["jeedom_device_type"] = device.JeedomDeviceType
	}
	return payload
}

func copyDevice(device Device) Device {
	device.Values = copyAnyMap(device.Values)
	device.RawCommands = copyCommands(device.RawCommands)
	device.HAIdentifiers = append([]string(nil), device.HAIdentifiers...)
	device.LegacyDeviceSlugs = append([]string(nil), device.LegacyDeviceSlugs...)
	device.Actions = copyActions(device.Actions)
	device.PendingDiscoveryCleanups = append([]Command(nil), device.PendingDiscoveryCleanups...)
	device.PendingActionDiscoveryCleanups = append([]Action(nil), device.PendingActionDiscoveryCleanups...)
	return device
}

func copyAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyCommands(commands map[string]Command) map[string]Command {
	if len(commands) == 0 {
		return map[string]Command{}
	}
	out := make(map[string]Command, len(commands))
	for key, value := range commands {
		out[key] = value
	}
	return out
}

func copyActions(actions map[string]Action) map[string]Action {
	if len(actions) == 0 {
		return map[string]Action{}
	}
	out := make(map[string]Action, len(actions))
	for key, value := range actions {
		out[key] = value
	}
	return out
}

func actionsForDevice(device *Device) []Action {
	if device == nil || len(device.Actions) == 0 {
		return nil
	}
	actions := make([]Action, 0, len(device.Actions))
	for _, action := range device.Actions {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Action < actions[j].Action
	})
	return actions
}

func (s *Store) mergeLegacyActionsLocked(device *Device) {
	if device == nil || len(device.LegacyDeviceSlugs) == 0 {
		return
	}
	for _, legacySlug := range device.LegacyDeviceSlugs {
		legacy := s.devices[Slug(legacySlug)]
		if legacy == nil || legacy == device {
			continue
		}
		s.copyActionsLocked(device, legacy)
	}
}

func (s *Store) linkedTargetForLegacyLocked(legacySlug string) *Device {
	legacySlug = Slug(legacySlug)
	if legacySlug == "" || legacySlug == "unknown" {
		return nil
	}
	for _, device := range s.devices {
		if device == nil || device.DeviceSlug == legacySlug {
			continue
		}
		if containsString(device.LegacyDeviceSlugs, legacySlug) {
			return device
		}
	}
	return nil
}

func (s *Store) copyActionsLocked(target, source *Device) {
	if target == nil || source == nil || len(source.Actions) == 0 {
		return
	}
	if target.Actions == nil {
		target.Actions = make(map[string]Action)
	}
	for actionName, action := range source.Actions {
		if _, exists := target.Actions[actionName]; exists {
			continue
		}
		target.Actions[actionName] = actionForDevice(action, target)
	}
}

func actionForDevice(action Action, device *Device) Action {
	if device == nil {
		return action
	}
	action.DeviceSlug = device.DeviceSlug
	action.Device = firstNonEmpty(device.Device, action.Device)
	action.DeviceType = firstNonEmpty(device.JeedomDeviceType, device.HAModel, action.DeviceType)
	return action
}

func mqttTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func containsString(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func mergeIdentity(fallback, resolved DeviceIdentity) DeviceIdentity {
	linkedToDifferentSlug := resolved.DeviceSlug != "" && resolved.DeviceSlug != fallback.DeviceSlug
	if resolved.DeviceSlug == "" {
		resolved.DeviceSlug = fallback.DeviceSlug
	}
	if resolved.DeviceName == "" {
		resolved.DeviceName = fallback.DeviceName
	}
	if resolved.BaseSlug == "" {
		resolved.BaseSlug = fallback.BaseSlug
	}
	if len(resolved.HAIdentifiers) == 0 {
		resolved.HAIdentifiers = fallback.HAIdentifiers
	}
	if resolved.HAManufacturer == "" {
		resolved.HAManufacturer = fallback.HAManufacturer
	}
	if resolved.HAModel == "" {
		resolved.HAModel = fallback.HAModel
	}
	if resolved.SuggestedArea == "" {
		resolved.SuggestedArea = fallback.SuggestedArea
	}
	if linkedToDifferentSlug {
		resolved.LegacyDeviceSlugs = mergeStringLists(resolved.LegacyDeviceSlugs, []string{fallback.DeviceSlug})
	}
	return resolved
}

func mergeStringLists(current, extra []string) []string {
	out := make([]string, 0, len(current)+len(extra))
	seen := make(map[string]struct{}, len(current)+len(extra))
	for _, values := range [][]string{current, extra} {
		for _, value := range values {
			value = Slug(value)
			if value == "" || value == "unknown" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func (s *Store) localDiscoveryDeviceSlug(discovery Discovery, baseSlug string) string {
	if baseSlug == "" {
		baseSlug = "dev_" + shortHash(discovery.Name+discovery.EqLogicID)
	}
	groups := s.baseGroups[baseSlug]
	if len(groups) == 0 {
		return baseSlug
	}
	for _, slug := range groups {
		device := s.devices[slug]
		if device == nil {
			continue
		}
		if device.JeedomID != "" && discovery.EqLogicID != "" && device.JeedomID == discovery.EqLogicID {
			return slug
		}
		if device.JeedomLogicalID != "" && discovery.LogicalID != "" && device.JeedomLogicalID == discovery.LogicalID {
			return slug
		}
	}
	suffix := Slug(discovery.EqLogicID)
	if suffix == "" || suffix == "unknown" {
		suffix = shortHash(discovery.Topic)
	}
	return baseSlug + "_" + suffix
}

func firstDiscoveryCommandID(discovery Discovery) string {
	for _, command := range discovery.InfoCommands {
		if command.CommandID != "" {
			return command.CommandID
		}
	}
	for _, command := range discovery.Actions {
		if command.CommandID != "" {
			return command.CommandID
		}
	}
	return discovery.EqLogicID
}

func firstDiscoveryCommandName(discovery Discovery) string {
	for _, command := range discovery.InfoCommands {
		if command.Name != "" {
			return command.Name
		}
	}
	for _, command := range discovery.Actions {
		if command.Name != "" {
			return command.Name
		}
	}
	return ""
}

func normalizeDiscoveryAction(command DiscoveryCommand) string {
	switch strings.ToUpper(strings.TrimSpace(command.LogicalID)) {
	case "SWITCH_ON", "ON":
		return "on"
	case "SWITCH_OFF", "OFF":
		return "off"
	case "IMPULSE", "PULSE", "TOGGLE", "SWITCH_TOGGLE", "RELAY_IMPULSE":
		return "impulse"
	case "ARM":
		return "arm"
	case "NIGHT_MODE":
		return "night_mode"
	case "DISARM":
		return "disarm"
	case "PANIC":
		return "panic"
	case "MUTEFIREDETECTORS", "MUTE_FIRE_DETECTORS":
		return "mute_fire_detectors"
	}
	switch commandKey(command.Name) {
	case "on":
		return "on"
	case "off":
		return "off"
	case "impulse", "impulsion", "pulse", "toggle":
		return "impulse"
	case "armement", "arm":
		return "arm"
	case "modenuit", "nightmode":
		return "night_mode"
	case "desarmement", "disarm":
		return "disarm"
	case "panic":
		return "panic"
	case "arretdetectionincendie", "mutefiredetectors":
		return "mute_fire_detectors"
	default:
		return ""
	}
}

func NormalizeControlAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "1", "true", "on", "open", "enable":
		return "on"
	case "0", "false", "off", "close", "disable":
		return "off"
	case "impulse", "impulsion", "pulse", "momentary", "toggle":
		return "impulse"
	default:
		return strings.ToLower(strings.TrimSpace(action))
	}
}

func controlAllowed(deviceType, action string) (bool, string) {
	deviceType = commandKey(deviceType)
	if action == "impulse" {
		if deviceType == "relay" {
			return true, ""
		}
		return false, "only relay impulse controls are allowlisted"
	}
	if action != "on" && action != "off" {
		if isHubControlDevice(deviceType) && isSecurityButtonAction(action) {
			return true, ""
		}
		return false, "only on/off, relay impulse, and hub security controls are allowlisted"
	}
	switch deviceType {
	case "relay", "socket", "wallswitch", "lightswitch", "outlet", "waterstop":
		return true, ""
	case "hub", "hub_2_plus", "hub2plus", "hub_plus", "hubplus", "hubhybrid", "hub_hybrid":
		return false, "hub/security controls are blocked by default"
	default:
		return false, "device type is not allowlisted for Jeedom control"
	}
}

func isSecurityButtonAction(action string) bool {
	for _, allowed := range securityButtonActionOrder {
		if action == allowed {
			return true
		}
	}
	return false
}
