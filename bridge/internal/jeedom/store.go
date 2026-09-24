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
	Device          Device
	PreviousDevices []Device
	Command         Command
	Mapping         Mapping
	EmptyValue      bool
	UpdatedValue    bool
	StoreChanged    bool
	NumericValue    float64
	HasNumeric      bool
}

type ApplyDiscoveryResult struct {
	Device          Device
	PreviousDevices []Device
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
	transferred, previousSlugs, transferredCommand, ownershipChanged := s.takeCommandFromOtherOwnersLocked(evt.CommandID, deviceSlug)

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
	if transferredCommand {
		if hasExisting {
			existing = commandWithNewestValue(existing, transferred)
		} else {
			existing = transferred
			hasExisting = true
		}
	}
	if hasExisting {
		command.LogicalID = existing.LogicalID
		command.GenericType = existing.GenericType
		command.Visible = existing.Visible
		command.Historized = existing.Historized
		command.EqLogicID = existing.EqLogicID
		command.Value = existing.Value
		command.LastValueAt = existing.LastValueAt
		command.EmptyValue = existing.EmptyValue
		seedDeviceValueFromCommand(device, &command, existing, mapping)
	}

	result := ApplyResult{
		Mapping:      mapping,
		EmptyValue:   evt.EmptyValue(),
		StoreChanged: ownershipChanged,
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
		rebuildMetricValue(device, existing.Metric)
	}
	rebuildMetricValue(device, command.Metric)
	s.commands[evt.CommandID] = deviceSlug
	s.bumpDevicePublishRevisionLocked(device)
	result.Device = copyDevice(*device)
	result.PreviousDevices = s.deviceSnapshotsLocked(previousSlugs)
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

	// Retained Jeedom discovery may contain older load/current samples than a
	// successful optimistic toggle recorded by the bridge. Keep those states
	// across metadata refreshes unless discovery supplies an observed state
	// command, which is authoritative.
	optimisticStates := make(map[string]any)
	optimisticStatesByAction := make(map[string]any)
	for slug, current := range s.devices {
		if current == nil {
			continue
		}
		state, exists := current.Values["state"]
		if exists && hasActionOnlyToggleControl(current) && !hasObservedAuthoritativeToggleFeedback(current) {
			optimisticStates[slug] = state
			for _, action := range current.Actions {
				switch NormalizeControlAction(action.Action) {
				case "on", "off":
					if action.CommandID != "" && strings.TrimSpace(action.StateCommandID) == "" {
						optimisticStatesByAction[action.CommandID] = state
					}
				}
			}
		}
	}

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

	previousSlugs := make(map[string]struct{})
	currentCommandIDs := make(map[string]struct{}, len(discovery.InfoCommands))
	infoCommandIDs := make([]string, 0, len(discovery.InfoCommands))
	for commandID := range discovery.InfoCommands {
		infoCommandIDs = append(infoCommandIDs, commandID)
	}
	sort.Strings(infoCommandIDs)
	for _, commandID := range infoCommandIDs {
		info := discovery.InfoCommands[commandID]
		currentCommandIDs[info.CommandID] = struct{}{}
	}
	removedCommands := make([]Command, 0)
	ownerSlugs := make([]string, 0, len(s.devices))
	for slug := range s.devices {
		ownerSlugs = append(ownerSlugs, slug)
	}
	sort.Strings(ownerSlugs)
	for _, ownerSlug := range ownerSlugs {
		owner := s.devices[ownerSlug]
		if owner == nil {
			continue
		}
		removedFromOwner := make([]Command, 0)
		for commandID, command := range owner.RawCommands {
			if command.CommandID == "" {
				continue
			}
			if _, current := currentCommandIDs[commandID]; current {
				continue
			}
			ownedByDiscovery := discovery.EqLogicID != "" && command.EqLogicID == discovery.EqLogicID
			legacyJeedomID := owner.JeedomID
			if ownerSlug == deviceSlug {
				legacyJeedomID = firstNonEmpty(legacyJeedomID, previousJeedomID)
			}
			legacyReplacement := command.EqLogicID == "" &&
				legacyJeedomID == discovery.EqLogicID &&
				discoveryReplacesCommand(discovery, command)
			if !ownedByDiscovery && !legacyReplacement {
				continue
			}
			removedCommands = append(removedCommands, command)
			removedFromOwner = append(removedFromOwner, command)
			delete(owner.RawCommands, commandID)
			delete(s.commands, commandID)
			rebuildMetricValue(owner, command.Metric)
		}
		if len(removedFromOwner) > 0 {
			queueCommandCleanups(owner, removedFromOwner)
			s.bumpDevicePublishRevisionLocked(owner)
			previousSlugs[ownerSlug] = struct{}{}
		}
	}
	sort.Slice(removedCommands, func(i, j int) bool {
		return removedCommands[i].CommandID < removedCommands[j].CommandID
	})
	for _, commandID := range infoCommandIDs {
		info := discovery.InfoCommands[commandID]
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
		transferred, movedSlugs, transferredCommand, _ := s.takeCommandFromOtherOwnersLocked(info.CommandID, deviceSlug)
		for slug := range movedSlugs {
			previousSlugs[slug] = struct{}{}
		}
		existing, hasExisting := device.RawCommands[info.CommandID]
		if transferredCommand {
			if hasExisting {
				existing = commandWithNewestValue(existing, transferred)
			} else {
				existing = transferred
				hasExisting = true
			}
		}
		if hasExisting {
			command.Value = existing.Value
			command.LastValueAt = existing.LastValueAt
			command.EmptyValue = existing.EmptyValue
			if existing.Value == nil && existing.EmptyValue {
				// Discovery refreshes command metadata, not the time at which an
				// explicit unknown value was observed.
				command.LastUpdate = existing.LastUpdate
			} else if existing.LastUpdate.After(command.LastUpdate) {
				command.LastUpdate = existing.LastUpdate
			}
			seedDeviceValueFromCommand(device, &command, existing, mapping)
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
			rebuildMetricValue(device, existing.Metric)
		}
		rebuildMetricValue(device, command.Metric)
		s.commands[info.CommandID] = deviceSlug
	}
	currentActionIDs := make(map[string]struct{}, len(discovery.Actions))
	actionCommandIDs := make([]string, 0, len(discovery.Actions))
	for commandID, actionCommand := range discovery.Actions {
		actionCommandIDs = append(actionCommandIDs, commandID)
		currentActionIDs[actionCommand.CommandID] = struct{}{}
	}
	sort.Strings(actionCommandIDs)
	removedActions := make([]Action, 0)
	for _, ownerSlug := range ownerSlugs {
		owner := s.devices[ownerSlug]
		if owner == nil {
			continue
		}
		removedFromOwner := make([]Action, 0)
		for actionName, action := range owner.Actions {
			if discovery.EqLogicID == "" || action.EqLogicID != discovery.EqLogicID {
				continue
			}
			if _, current := currentActionIDs[action.CommandID]; current {
				continue
			}
			removedActions = append(removedActions, action)
			removedFromOwner = append(removedFromOwner, action)
			delete(owner.Actions, actionName)
		}
		if len(removedFromOwner) > 0 {
			queueActionCleanups(owner, removedFromOwner)
			s.bumpDevicePublishRevisionLocked(owner)
			previousSlugs[ownerSlug] = struct{}{}
		}
	}
	sort.Slice(removedActions, func(i, j int) bool {
		return removedActions[i].CommandID < removedActions[j].CommandID
	})
	for _, commandID := range actionCommandIDs {
		actionCommand := discovery.Actions[commandID]
		actionName := normalizeDiscoveryAction(actionCommand)
		if actionName == "" {
			actionName = "cmd_" + actionCommand.CommandID
		}
		transferred, movedSlugs, transferredAction := s.takeActionFromOtherOwnersLocked(actionCommand.CommandID, deviceSlug, actionName)
		for slug := range movedSlugs {
			previousSlugs[slug] = struct{}{}
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
		storageKey := actionName
		if existing, ok := device.Actions[actionName]; ok && existing.CommandID == action.CommandID {
			action.LastRequestedAt = existing.LastRequestedAt
			action.LastRequestSource = existing.LastRequestSource
		} else if ok {
			storageKey = actionCollisionStorageKey(device.Actions, actionName, action.CommandID)
		}
		if transferredAction && transferred.LastRequestedAt.After(action.LastRequestedAt) {
			action.LastRequestedAt = transferred.LastRequestedAt
			action.LastRequestSource = transferred.LastRequestSource
		}
		device.Actions[storageKey] = action
	}

	s.bumpDevicePublishRevisionLocked(device)
	primary := device
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
		primary = target
	}

	if s.resolver != nil {
		for slug := range s.rehomeResolvedCommandsLocked(s.resolver) {
			previousSlugs[slug] = struct{}{}
		}
		for slug := range s.rehomeResolvedActionsLocked(s.resolver) {
			previousSlugs[slug] = struct{}{}
		}
		for slug := range s.resolveActionCollisionsLocked(s.resolver) {
			previousSlugs[slug] = struct{}{}
		}
		s.deduplicateCommandOwnersLocked()
		for slug := range s.removeCanonicalLegacyAliasesLocked() {
			previousSlugs[slug] = struct{}{}
		}
		s.rebuildIndexesLocked()
	}

	affectedSlugs := make(map[string]struct{}, len(previousSlugs)+1)
	for slug := range previousSlugs {
		affectedSlugs[slug] = struct{}{}
	}
	affectedSlugs[primary.DeviceSlug] = struct{}{}
	for slug := range affectedSlugs {
		current := s.devices[slug]
		if current == nil || !hasActionOnlyToggleControl(current) || hasObservedAuthoritativeToggleFeedback(current) {
			continue
		}
		state, restore := optimisticStates[slug]
		if !restore {
			for _, action := range current.Actions {
				if candidate, ok := optimisticStatesByAction[action.CommandID]; ok {
					state = candidate
					restore = true
					break
				}
			}
		}
		if !restore {
			continue
		}
		existingState, stateExists := current.Values["state"]
		sameState := stateExists && existingState == nil && state == nil
		if existingBool, ok := existingState.(bool); ok {
			if stateBool, stateOK := state.(bool); stateOK && existingBool == stateBool {
				sameState = true
			}
		}
		if sameState {
			continue
		}
		current.Values["state"] = state
		s.bumpDevicePublishRevisionLocked(current)
		previousSlugs[slug] = struct{}{}
	}

	affectedSlugs = make(map[string]struct{}, len(previousSlugs)+1)
	for slug := range previousSlugs {
		affectedSlugs[slug] = struct{}{}
	}
	affectedSlugs[primary.DeviceSlug] = struct{}{}
	orderedDevices := s.deviceSnapshotsInPublishOrderLocked(affectedSlugs)
	if len(orderedDevices) == 0 {
		orderedDevices = []Device{copyDevice(*primary)}
	}
	resultDevice := orderedDevices[len(orderedDevices)-1]
	previousDevices := orderedDevices[:len(orderedDevices)-1]
	resultOwner := s.devices[resultDevice.DeviceSlug]
	return ApplyDiscoveryResult{
		Device:          resultDevice,
		PreviousDevices: previousDevices,
		Actions:         actionsForDevice(resultOwner),
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
		// Reconciliation can change identity and Home Assistant metadata without
		// moving a command. Advance every returned snapshot past the prior
		// generation so a delayed pre-catalog publish cannot overwrite it. Command
		// and action rehoming below then advances source and destination revisions
		// again, preserving the required source-before-destination publish order.
		s.ensureDevicePublishRevisionsLocked()
		s.bumpAllDevicePublishRevisionsLocked()
		s.rehomeResolvedCommandsLocked(resolver)
		s.rehomeResolvedActionsLocked(resolver)
		s.resolveActionCollisionsLocked(resolver)
		s.deduplicateCommandOwnersLocked()
		s.removeCanonicalLegacyAliasesLocked()
		for _, device := range s.devices {
			rebuildAllMetricValuesPreservingOptimisticState(device)
		}
		s.rebuildIndexesLocked()
	}
	s.ensureDevicePublishRevisionsLocked()
	allSlugs := make(map[string]struct{}, len(s.devices))
	for slug := range s.devices {
		allSlugs[slug] = struct{}{}
	}
	return s.deviceSnapshotsInPublishOrderLocked(allSlugs)
}

// takeCommandFromOtherOwnersLocked enforces the global Jeedom command-ID
// ownership contract. Moving an active command must not queue discovery
// cleanup because the retained discovery topic and unique ID are unchanged.
func (s *Store) takeCommandFromOtherOwnersLocked(commandID, targetSlug string) (Command, map[string]struct{}, bool, bool) {
	previousSlugs := make(map[string]struct{})
	var transferred Command
	found := false
	changed := false
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
		cleanupRemoved := removePendingCommandCleanup(device, commandID)
		changed = changed || cleanupRemoved
		if slug == targetSlug {
			continue
		}
		command, ok := device.RawCommands[commandID]
		if !ok {
			if cleanupRemoved {
				s.bumpDevicePublishRevisionLocked(device)
				previousSlugs[slug] = struct{}{}
			}
			continue
		}
		if !found {
			transferred = command
			found = true
		} else {
			transferred = commandWithNewestValue(transferred, command)
		}
		delete(device.RawCommands, commandID)
		rebuildMetricValue(device, command.Metric)
		s.bumpDevicePublishRevisionLocked(device)
		previousSlugs[slug] = struct{}{}
		changed = true
	}
	if owner := s.commands[commandID]; owner != "" && owner != targetSlug {
		delete(s.commands, commandID)
	}
	return transferred, previousSlugs, found, changed
}

func removePendingCommandCleanup(device *Device, commandID string) bool {
	if device == nil || commandID == "" || len(device.PendingDiscoveryCleanups) == 0 {
		return false
	}
	kept := device.PendingDiscoveryCleanups[:0]
	removed := false
	for _, command := range device.PendingDiscoveryCleanups {
		if command.CommandID != commandID {
			kept = append(kept, command)
		} else {
			removed = true
		}
	}
	device.PendingDiscoveryCleanups = kept
	return removed
}

func (s *Store) takeActionFromOtherOwnersLocked(commandID, targetSlug, targetActionName string) (Action, map[string]struct{}, bool) {
	previousSlugs := make(map[string]struct{})
	var transferred Action
	found := false
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
		if removePendingActionCleanup(device, targetSlug, commandID, targetActionName) {
			s.bumpDevicePublishRevisionLocked(device)
			if slug != targetSlug {
				previousSlugs[slug] = struct{}{}
			}
		}
		actionNames := make([]string, 0, len(device.Actions))
		for actionName := range device.Actions {
			actionNames = append(actionNames, actionName)
		}
		sort.Strings(actionNames)
		for _, actionName := range actionNames {
			action := device.Actions[actionName]
			if action.CommandID != commandID || (slug == targetSlug && actionName == targetActionName) {
				continue
			}
			if slug != targetSlug && s.intentionalLegacyActionMirrorLocked(slug, targetSlug, commandID) {
				continue
			}
			if !found || action.LastRequestedAt.After(transferred.LastRequestedAt) {
				transferred = action
				found = true
			}
			delete(device.Actions, actionName)
			queueActionCleanups(device, []Action{action})
			if slug != targetSlug {
				previousSlugs[slug] = struct{}{}
				s.bumpDevicePublishRevisionLocked(device)
			}
		}
	}
	return transferred, previousSlugs, found
}

func (s *Store) intentionalLegacyActionMirrorLocked(sourceSlug, targetSlug, commandID string) bool {
	source, target := s.devices[sourceSlug], s.devices[targetSlug]
	if source == nil || target == nil {
		return false
	}
	if !containsString(target.LegacyDeviceSlugs, sourceSlug) && !containsString(source.LegacyDeviceSlugs, targetSlug) {
		return false
	}
	for _, action := range source.Actions {
		if action.CommandID == commandID {
			for _, targetAction := range target.Actions {
				if targetAction.CommandID == commandID {
					return true
				}
			}
		}
	}
	return false
}

func removePendingActionCleanup(device *Device, activeDeviceSlug, commandID, activeActionName string) bool {
	if device == nil || commandID == "" || len(device.PendingActionDiscoveryCleanups) == 0 {
		return false
	}
	activeDeviceSlug = Slug(activeDeviceSlug)
	activeActionName = NormalizeControlAction(activeActionName)
	kept := device.PendingActionDiscoveryCleanups[:0]
	removed := false
	for _, action := range device.PendingActionDiscoveryCleanups {
		cleanupDeviceSlug := Slug(action.DeviceSlug)
		if cleanupDeviceSlug == "" {
			cleanupDeviceSlug = Slug(device.DeviceSlug)
		}
		sameDevice := cleanupDeviceSlug == activeDeviceSlug
		if action.CommandID == commandID && sameDevice && NormalizeControlAction(action.Action) == activeActionName {
			removed = true
			continue
		}
		kept = append(kept, action)
	}
	device.PendingActionDiscoveryCleanups = kept
	return removed
}

func (s *Store) deviceSnapshotsLocked(slugs map[string]struct{}) []Device {
	if len(slugs) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(slugs))
	for slug := range slugs {
		ordered = append(ordered, slug)
	}
	sort.Strings(ordered)
	devices := make([]Device, 0, len(ordered))
	for _, slug := range ordered {
		if device := s.devices[slug]; device != nil {
			devices = append(devices, copyDevice(*device))
		}
	}
	return devices
}

func (s *Store) deviceSnapshotsInPublishOrderLocked(slugs map[string]struct{}) []Device {
	if len(slugs) == 0 {
		return nil
	}
	ordered := make([]*Device, 0, len(slugs))
	for slug := range slugs {
		if device := s.devices[slug]; device != nil {
			ordered = append(ordered, device)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].publishRevision != ordered[j].publishRevision {
			return ordered[i].publishRevision < ordered[j].publishRevision
		}
		return ordered[i].DeviceSlug < ordered[j].DeviceSlug
	})
	devices := make([]Device, 0, len(ordered))
	for _, device := range ordered {
		devices = append(devices, copyDevice(*device))
	}
	return devices
}

func (s *Store) rehomeResolvedCommandsLocked(resolver IdentityResolver) map[string]struct{} {
	changedSlugs := make(map[string]struct{})
	type placement struct {
		sourceSlug string
		commandID  string
		identity   DeviceIdentity
	}
	placements := make([]placement, 0)
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
		commandIDs := make([]string, 0, len(device.RawCommands))
		for commandID := range device.RawCommands {
			commandIDs = append(commandIDs, commandID)
		}
		sort.Strings(commandIDs)
		for _, commandID := range commandIDs {
			command := device.RawCommands[commandID]
			event := Event{
				CommandID:   commandID,
				LogicalID:   command.LogicalID,
				GenericType: command.GenericType,
				ObjectName:  firstNonEmpty(command.ObjectName, device.ObjectName),
				DeviceName:  firstNonEmpty(command.Device, device.Device),
				CommandName: firstNonEmpty(command.RawName, command.Name),
				Name:        firstNonEmpty(command.RawName, command.Name),
				Type:        command.Type,
				Subtype:     command.Subtype,
				Unit:        command.Unit,
			}
			mapping := mappingFromCommandContract(command, MappingFor(event))
			identity := resolver.Resolve(event, mapping)
			if identity.DeviceSlug != "" && identity.DeviceSlug != slug {
				placements = append(placements, placement{sourceSlug: slug, commandID: commandID, identity: identity})
			}
		}
	}

	for _, placement := range placements {
		source := s.devices[placement.sourceSlug]
		if source == nil {
			continue
		}
		if _, ok := source.RawCommands[placement.commandID]; !ok {
			continue
		}
		target := s.ensureIdentityDeviceLocked(placement.identity, *source)
		transferred, movedSlugs, ok, _ := s.takeCommandFromOtherOwnersLocked(placement.commandID, target.DeviceSlug)
		if !ok {
			continue
		}
		for slug := range movedSlugs {
			changedSlugs[slug] = struct{}{}
		}
		changedSlugs[placement.sourceSlug] = struct{}{}
		changedSlugs[target.DeviceSlug] = struct{}{}
		if existing, exists := target.RawCommands[placement.commandID]; exists {
			transferred = commandWithNewestValue(existing, transferred)
		}
		transferred.DeviceSlug = target.DeviceSlug
		transferred.Device = firstNonEmpty(placement.identity.DeviceName, transferred.Device, target.Device)
		target.RawCommands[placement.commandID] = transferred
		removePendingCommandCleanup(target, placement.commandID)
		rebuildMetricValue(target, transferred.Metric)
		if transferred.LastUpdate.After(target.LastUpdate) {
			target.LastUpdate = transferred.LastUpdate
		}
		if target.JeedomID == "" && transferred.EqLogicID != "" {
			target.JeedomID = transferred.EqLogicID
		}
		s.bumpDevicePublishRevisionLocked(target)
	}
	return changedSlugs
}

func (s *Store) rehomeResolvedActionsLocked(resolver IdentityResolver) map[string]struct{} {
	changedSlugs := make(map[string]struct{})
	type placement struct {
		sourceSlug string
		actionName string
		commandID  string
		identity   DeviceIdentity
	}
	placements := make([]placement, 0)
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
		actionNames := make([]string, 0, len(device.Actions))
		for actionName := range device.Actions {
			actionNames = append(actionNames, actionName)
		}
		sort.Strings(actionNames)
		for _, actionName := range actionNames {
			action := device.Actions[actionName]
			if strings.TrimSpace(action.CommandID) == "" {
				continue
			}
			identity := resolver.Resolve(Event{
				CommandID:   action.CommandID,
				ObjectName:  device.ObjectName,
				DeviceName:  firstNonEmpty(action.Device, device.Device),
				CommandName: firstNonEmpty(action.RawName, action.Name, actionName),
				Name:        firstNonEmpty(action.RawName, action.Name, actionName),
				Type:        "action",
				Subtype:     action.Subtype,
			}, Mapping{})
			if identity.DeviceSlug != "" && identity.DeviceSlug != slug {
				placements = append(placements, placement{
					sourceSlug: slug,
					actionName: actionName,
					commandID:  action.CommandID,
					identity:   identity,
				})
			}
		}
	}

	for _, placement := range placements {
		source := s.devices[placement.sourceSlug]
		if source == nil {
			continue
		}
		action, ok := source.Actions[placement.actionName]
		if !ok || action.CommandID != placement.commandID {
			continue
		}
		target := s.ensureIdentityDeviceLocked(placement.identity, *source)
		actionName := NormalizeControlAction(action.Action)
		if actionName == "" {
			actionName = placement.actionName
		}
		transferred, movedSlugs, found := s.takeActionFromOtherOwnersLocked(placement.commandID, target.DeviceSlug, actionName)
		if !found {
			continue
		}
		for slug := range movedSlugs {
			changedSlugs[slug] = struct{}{}
		}
		changedSlugs[placement.sourceSlug] = struct{}{}
		changedSlugs[target.DeviceSlug] = struct{}{}
		if existing, exists := target.Actions[actionName]; exists && existing.LastRequestedAt.After(transferred.LastRequestedAt) {
			transferred.LastRequestedAt = existing.LastRequestedAt
			transferred.LastRequestSource = existing.LastRequestSource
		}
		transferred.Action = actionName
		transferred.DeviceSlug = target.DeviceSlug
		transferred.Device = firstNonEmpty(placement.identity.DeviceName, target.Device, transferred.Device)
		transferred.DeviceType = firstNonEmpty(placement.identity.HAModel, target.JeedomDeviceType, transferred.DeviceType)
		transferred.Allowed, transferred.DenyReason = controlAllowed(transferred.DeviceType, actionName)
		target.Actions[actionName] = transferred
		s.bumpDevicePublishRevisionLocked(target)
	}
	return changedSlugs
}

func actionCollisionStorageKey(actions map[string]Action, actionName, commandID string) string {
	base := actionName + "__cmd_" + Slug(commandID)
	if _, exists := actions[base]; !exists {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s_%d", base, suffix)
		if _, exists := actions[candidate]; !exists {
			return candidate
		}
	}
}

func (s *Store) resolveActionCollisionsLocked(resolver IdentityResolver) map[string]struct{} {
	changedSlugs := make(map[string]struct{})
	type actionCandidate struct {
		mapKey   string
		action   Action
		resolved bool
	}
	for slug, device := range s.devices {
		if device == nil || len(device.Actions) < 2 {
			continue
		}
		groups := make(map[string][]actionCandidate)
		for mapKey, action := range device.Actions {
			actionName := NormalizeControlAction(action.Action)
			if actionName == "" {
				actionName = NormalizeControlAction(strings.Split(mapKey, "__cmd_")[0])
			}
			if actionName == "" {
				actionName = mapKey
			}
			identity := resolver.Resolve(Event{
				CommandID:   action.CommandID,
				ObjectName:  device.ObjectName,
				DeviceName:  firstNonEmpty(action.Device, device.Device),
				CommandName: firstNonEmpty(action.RawName, action.Name, actionName),
				Name:        firstNonEmpty(action.RawName, action.Name, actionName),
				Type:        "action",
				Subtype:     action.Subtype,
			}, Mapping{})
			groups[actionName] = append(groups[actionName], actionCandidate{
				mapKey:   mapKey,
				action:   action,
				resolved: identity.DeviceSlug == slug,
			})
		}
		changed := false
		for actionName, candidates := range groups {
			if len(candidates) < 2 {
				candidate := candidates[0]
				if candidate.mapKey != actionName {
					delete(device.Actions, candidate.mapKey)
					candidate.action.Action = actionName
					device.Actions[actionName] = actionForDevice(candidate.action, device)
					changed = true
				}
				continue
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				left, right := candidates[i], candidates[j]
				if left.resolved != right.resolved {
					return left.resolved
				}
				if !left.action.LastRequestedAt.Equal(right.action.LastRequestedAt) {
					return left.action.LastRequestedAt.After(right.action.LastRequestedAt)
				}
				leftProvenance := left.action.EqLogicID != "" && left.action.EqLogicID == device.JeedomID
				rightProvenance := right.action.EqLogicID != "" && right.action.EqLogicID == device.JeedomID
				if leftProvenance != rightProvenance {
					return leftProvenance
				}
				return left.action.CommandID < right.action.CommandID
			})
			winner := candidates[0].action
			for _, candidate := range candidates[1:] {
				if candidate.action.LastRequestedAt.After(winner.LastRequestedAt) {
					winner.LastRequestedAt = candidate.action.LastRequestedAt
					winner.LastRequestSource = candidate.action.LastRequestSource
				}
			}
			for _, candidate := range candidates {
				delete(device.Actions, candidate.mapKey)
			}
			winner.Action = actionName
			device.Actions[actionName] = actionForDevice(winner, device)
			changed = true
		}
		if changed {
			s.bumpDevicePublishRevisionLocked(device)
			changedSlugs[slug] = struct{}{}
		}
	}
	return changedSlugs
}

func (s *Store) removeCanonicalLegacyAliasesLocked() map[string]struct{} {
	changedSlugs := make(map[string]struct{})
	canonicalSlugs := make(map[string]struct{})
	for slug, device := range s.devices {
		if device == nil || strings.TrimSpace(device.LinkedSource) == "" {
			continue
		}
		canonicalSlugs[Slug(slug)] = struct{}{}
	}
	if len(canonicalSlugs) == 0 {
		return changedSlugs
	}
	for slug, device := range s.devices {
		if device == nil || len(device.LegacyDeviceSlugs) == 0 {
			continue
		}
		kept := device.LegacyDeviceSlugs[:0]
		changed := false
		for _, legacySlug := range device.LegacyDeviceSlugs {
			normalized := Slug(legacySlug)
			_, canonical := canonicalSlugs[normalized]
			if canonical && normalized != Slug(slug) {
				changed = true
				continue
			}
			kept = append(kept, legacySlug)
		}
		device.LegacyDeviceSlugs = kept
		if changed {
			s.bumpDevicePublishRevisionLocked(device)
			changedSlugs[slug] = struct{}{}
		}
	}
	return changedSlugs
}

func (s *Store) ensureIdentityDeviceLocked(identity DeviceIdentity, source Device) *Device {
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
	target.Source = Source
	target.DeviceSlug = identity.DeviceSlug
	target.Device = firstNonEmpty(identity.DeviceName, target.Device, source.Device)
	target.BaseSlug = firstNonEmpty(identity.BaseSlug, target.BaseSlug)
	target.ObjectName = firstNonEmpty(target.ObjectName, source.ObjectName)
	if len(identity.HAIdentifiers) > 0 {
		target.HAIdentifiers = append([]string(nil), identity.HAIdentifiers...)
	}
	target.HAManufacturer = firstNonEmpty(identity.HAManufacturer, target.HAManufacturer, source.HAManufacturer)
	target.HAModel = firstNonEmpty(identity.HAModel, target.HAModel, source.HAModel)
	target.SuggestedArea = firstNonEmpty(identity.SuggestedArea, target.SuggestedArea)
	target.LinkedSource = identity.LinkedSource
	target.LinkedAccount = identity.LinkedAccount
	target.LinkedZone = identity.LinkedZone
	target.DiscoveryDisabled = identity.DiscoveryDisabled
	target.JeedomDeviceType = firstNonEmpty(identity.HAModel, target.JeedomDeviceType, source.JeedomDeviceType)
	target.JeedomEnabled = target.JeedomEnabled || source.JeedomEnabled
	target.JeedomVisible = target.JeedomVisible || source.JeedomVisible
	return target
}

func commandWithNewestValue(primary, candidate Command) Command {
	result := primary
	if candidate.LastUpdate.After(result.LastUpdate) {
		result = candidate
	}
	primaryState := effectiveCommandState(primary)
	candidateState := effectiveCommandState(candidate)
	valueSource := primary
	selectedState := primaryState
	if newerCommandState(candidateState, candidate, primaryState, primary) {
		valueSource = candidate
		selectedState = candidateState
	}
	if selectedState.present {
		result.Value = selectedState.value
		result.EmptyValue = selectedState.empty
	} else {
		result.Value = valueSource.Value
		result.EmptyValue = valueSource.EmptyValue
	}
	result.LastValueAt = valueSource.LastValueAt
	return result
}

func seedDeviceValueFromCommand(device *Device, command *Command, existing Command, mapping Mapping) {
	if device == nil || command == nil || mapping.Metric == "" {
		return
	}
	if command.Value == nil {
		if command.EmptyValue {
			device.Values[mapping.Metric] = nil
		}
		return
	}
	value := command.Value
	if existing.Metric != mapping.Metric || existing.Component != mapping.Component {
		mapped, ok := mappedAnyValue(command.Value, mapping, deviceTypeForNormalization(*device))
		if !ok {
			return
		}
		value = mapped
	}
	command.Value = value
	device.Values[mapping.Metric] = value
	applyDerivedValuesFromEventCode(mapping, device, value, command.LastValueAt)
	applyDerivedWallSwitchStateFromLoad(mapping, device, value)
	applyDerivedWaterStopState(mapping, device, value)
}

func (s *Store) deduplicateCommandOwnersLocked() {
	type commandOwner struct {
		slug    string
		mapKey  string
		command Command
	}
	ownersByID := make(map[string][]commandOwner)
	for slug, device := range s.devices {
		if device == nil {
			continue
		}
		for mapKey, command := range device.RawCommands {
			commandID := strings.TrimSpace(command.CommandID)
			if commandID == "" {
				continue
			}
			ownersByID[commandID] = append(ownersByID[commandID], commandOwner{
				slug:    slug,
				mapKey:  mapKey,
				command: command,
			})
		}
	}

	commandIDs := make([]string, 0, len(ownersByID))
	for commandID, owners := range ownersByID {
		if len(owners) > 1 {
			commandIDs = append(commandIDs, commandID)
		}
	}
	sort.Strings(commandIDs)
	for _, commandID := range commandIDs {
		owners := ownersByID[commandID]
		sort.SliceStable(owners, func(i, j int) bool {
			left, right := owners[i], owners[j]
			leftDevice, rightDevice := s.devices[left.slug], s.devices[right.slug]
			leftProvenance := leftDevice != nil && left.command.EqLogicID != "" && left.command.EqLogicID == leftDevice.JeedomID
			rightProvenance := rightDevice != nil && right.command.EqLogicID != "" && right.command.EqLogicID == rightDevice.JeedomID
			if leftProvenance != rightProvenance {
				return leftProvenance
			}
			if !left.command.LastValueAt.Equal(right.command.LastValueAt) {
				return left.command.LastValueAt.After(right.command.LastValueAt)
			}
			if !left.command.LastUpdate.Equal(right.command.LastUpdate) {
				return left.command.LastUpdate.After(right.command.LastUpdate)
			}
			return left.slug < right.slug
		})

		winner := owners[0]
		merged := winner.command
		for _, owner := range owners[1:] {
			merged = commandWithNewestValue(merged, owner.command)
		}
		for slug, device := range s.devices {
			if device == nil {
				continue
			}
			cleanupRemoved := removePendingCommandCleanup(device, commandID)
			if cleanupRemoved && slug != winner.slug {
				s.bumpDevicePublishRevisionLocked(device)
			}
		}
		for _, owner := range owners[1:] {
			device := s.devices[owner.slug]
			if device == nil {
				continue
			}
			delete(device.RawCommands, owner.mapKey)
			rebuildMetricValue(device, owner.command.Metric)
			s.bumpDevicePublishRevisionLocked(device)
		}

		target := s.devices[winner.slug]
		if target == nil {
			continue
		}
		if winner.mapKey != commandID {
			delete(target.RawCommands, winner.mapKey)
		}
		merged.CommandID = commandID
		merged.DeviceSlug = winner.slug
		merged.Device = firstNonEmpty(target.Device, merged.Device)
		target.RawCommands[commandID] = merged
		rebuildMetricValue(target, merged.Metric)
		s.bumpDevicePublishRevisionLocked(target)
	}
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
		if existing, ok := target.RawCommands[commandID]; ok {
			command = commandWithNewestValue(existing, command)
		}
		command.DeviceSlug = identity.DeviceSlug
		command.Device = firstNonEmpty(identity.DeviceName, source.Device, command.Device)
		target.RawCommands[commandID] = command
	}
	for actionName, action := range source.Actions {
		action.DeviceSlug = identity.DeviceSlug
		action.Device = firstNonEmpty(identity.DeviceName, source.Device, action.Device)
		action.DeviceType = firstNonEmpty(identity.HAModel, source.JeedomDeviceType, source.HAModel, action.DeviceType)
		if existing, ok := target.Actions[actionName]; ok {
			if existing.CommandID == action.CommandID {
				if existing.LastRequestedAt.After(action.LastRequestedAt) {
					action.LastRequestedAt = existing.LastRequestedAt
					action.LastRequestSource = existing.LastRequestSource
				}
				target.Actions[actionName] = action
				continue
			}
			actionName = actionCollisionStorageKey(target.Actions, actionName, action.CommandID)
		}
		target.Actions[actionName] = action
	}
	rebuildAllMetricValuesPreservingOptimisticState(target)
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
	target.JeedomDeviceType = firstNonEmpty(identity.HAModel, source.JeedomDeviceType, target.JeedomDeviceType)
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

type commandState struct {
	value      any
	empty      bool
	observedAt time.Time
	present    bool
}

func effectiveCommandState(command Command) commandState {
	if command.Value == nil {
		if !command.EmptyValue {
			return commandState{}
		}
		observedAt := command.LastUpdate
		if observedAt.IsZero() {
			observedAt = command.LastValueAt
		}
		return commandState{value: nil, empty: true, observedAt: observedAt, present: true}
	}
	observedAt := command.LastValueAt
	if observedAt.IsZero() {
		observedAt = command.LastUpdate
	}
	return commandState{value: command.Value, empty: command.EmptyValue, observedAt: observedAt, present: true}
}

func newerCommandState(candidate commandState, candidateCommand Command, current commandState, currentCommand Command) bool {
	if !current.present {
		return candidate.present
	}
	if !candidate.present {
		return false
	}
	if !candidate.observedAt.Equal(current.observedAt) {
		return candidate.observedAt.After(current.observedAt)
	}
	if !candidateCommand.LastUpdate.Equal(currentCommand.LastUpdate) {
		return candidateCommand.LastUpdate.After(currentCommand.LastUpdate)
	}
	if candidate.empty != current.empty {
		return candidate.empty
	}
	return candidateCommand.CommandID < currentCommand.CommandID
}

func rebuildMetricValue(device *Device, metric string) {
	if device == nil || strings.TrimSpace(metric) == "" {
		return
	}
	if device.Values == nil {
		device.Values = make(map[string]any)
	}
	var selected commandState
	var selectedCommand Command
	for _, command := range device.RawCommands {
		if command.Metric != metric {
			continue
		}
		candidate := effectiveCommandState(command)
		if newerCommandState(candidate, command, selected, selectedCommand) {
			selected = candidate
			selectedCommand = command
		}
	}
	if selected.present {
		device.Values[metric] = selected.value
	} else {
		delete(device.Values, metric)
	}
	rebuildDerivedValuesForMetricChange(device, metric)
}

func rebuildAllMetricValues(device *Device) {
	if device == nil {
		return
	}
	metrics := make(map[string]struct{}, len(device.RawCommands))
	for _, command := range device.RawCommands {
		if command.Metric != "" {
			metrics[command.Metric] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(metrics))
	for metric := range metrics {
		ordered = append(ordered, metric)
	}
	sort.Strings(ordered)
	for _, metric := range ordered {
		rebuildMetricValue(device, metric)
	}
}

// rebuildAllMetricValuesPreservingOptimisticState refreshes values restored
// from command state without discarding the last successful control request
// for action-only toggle devices. Those devices intentionally have no info
// command from which their state can be reconstructed after a restart.
func rebuildAllMetricValuesPreservingOptimisticState(device *Device) {
	if device == nil {
		return
	}
	optimisticState, hadOptimisticState := device.Values["state"]
	preserveOptimisticState := hadOptimisticState && hasActionOnlyToggleControl(device) && !hasObservedAuthoritativeToggleFeedback(device)
	rebuildAllMetricValues(device)
	if preserveOptimisticState {
		device.Values["state"] = optimisticState
	}
}

func hasActionOnlyToggleControl(device *Device) bool {
	if device == nil || !toggleCapableDevice(*device) {
		return false
	}
	for _, action := range device.Actions {
		if strings.TrimSpace(action.StateCommandID) != "" {
			continue
		}
		switch NormalizeControlAction(action.Action) {
		case "on", "off":
			return true
		}
	}
	return false
}

func hasObservedStateCommand(device *Device) bool {
	if device == nil {
		return false
	}
	for _, command := range device.RawCommands {
		if command.Metric == "state" && effectiveCommandState(command).present {
			return true
		}
	}
	return false
}

func hasObservedAuthoritativeToggleFeedback(device *Device) bool {
	if hasObservedStateCommand(device) {
		return true
	}
	if device == nil || !isWaterStopDevice(*device) {
		return false
	}
	for _, command := range device.RawCommands {
		if command.Metric == "valve_position" && effectiveCommandState(command).present {
			return true
		}
	}
	return false
}

func rebuildDerivedValuesForMetricChange(device *Device, metric string) {
	if device == nil {
		return
	}
	rebuildState := metric == "state" ||
		(metric == "event_code" && (isWallSwitchDevice(*device) || isWaterStopDevice(*device))) ||
		((metric == "current_a" || metric == "power_w") && isWallSwitchDevice(*device)) ||
		(metric == "valve_position" && isWaterStopDevice(*device))
	rebuildGridPower := metric == "grid_power" ||
		(metric == "event_code" && isTransmitterDevice(*device))
	if !rebuildState && !rebuildGridPower {
		return
	}
	if device.Values == nil {
		device.Values = make(map[string]any)
	}
	if rebuildGridPower {
		for key, command := range device.RawCommands {
			if command.CommandID == "" && command.Metric == "grid_power" {
				delete(device.RawCommands, key)
			}
		}
	}

	commands := make([]Command, 0, len(device.RawCommands))
	for _, command := range device.RawCommands {
		commands = append(commands, command)
	}
	sort.SliceStable(commands, func(i, j int) bool {
		left, right := commands[i], commands[j]
		leftState, rightState := effectiveCommandState(left), effectiveCommandState(right)
		leftAt, rightAt := leftState.observedAt, rightState.observedAt
		if !leftState.present {
			leftAt = left.LastUpdate
		}
		if !rightState.present {
			rightAt = right.LastUpdate
		}
		if !leftAt.Equal(rightAt) {
			return leftAt.Before(rightAt)
		}
		if !left.LastUpdate.Equal(right.LastUpdate) {
			return left.LastUpdate.Before(right.LastUpdate)
		}
		if rebuildState {
			leftPriority := derivedStateSourcePriority(storedCommandMapping(left), *device)
			rightPriority := derivedStateSourcePriority(storedCommandMapping(right), *device)
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
		}
		if leftState.empty != rightState.empty {
			return !leftState.empty
		}
		// rebuildMetricValue selects the lexicographically smaller command ID
		// on an otherwise exact tie, so process it last here as well.
		return left.CommandID > right.CommandID
	})

	var state any
	stateFound := false
	gridPower := false
	gridPowerFound := false
	gridPowerAt := time.Time{}
	for _, command := range commands {
		mapping := storedCommandMapping(command)
		if rebuildState {
			if mapping.Metric == "state" && (command.Value != nil || command.EmptyValue) {
				state = command.Value
				stateFound = true
			}
			if derived, ok := derivedStateFromEventCode(mapping, *device, command.Value); ok {
				state = derived
				stateFound = true
			}
			if isWallSwitchDevice(*device) && (mapping.Metric == "current_a" || mapping.Metric == "power_w") {
				if load, ok := numericValue(command.Value); ok && load > 0 {
					state = true
					stateFound = true
				}
			}
			if isWaterStopDevice(*device) && mapping.Metric == "valve_position" {
				switch strings.ToUpper(strings.TrimSpace(fmt.Sprint(command.Value))) {
				case "OPEN", "OPENED":
					state = true
					stateFound = true
				case "CLOSED", "CLOSE":
					state = false
					stateFound = true
				case "INTERMEDIATE", "OPENING", "CLOSING", "MOVING":
					state = nil
					stateFound = true
				}
			}
		}
		if rebuildGridPower {
			if derived, ok := derivedGridPowerFromEventCode(mapping, *device, command.Value); ok {
				gridPower = derived
				gridPowerFound = true
				gridPowerAt = command.LastValueAt
				if gridPowerAt.IsZero() {
					gridPowerAt = command.LastUpdate
				}
			}
		}
	}
	if rebuildState {
		if stateFound {
			device.Values["state"] = state
		} else {
			delete(device.Values, "state")
		}
	}
	if rebuildGridPower {
		if gridPowerFound {
			device.Values["grid_power"] = gridPower
			ensureSyntheticGridPowerCommand(device, gridPower, gridPowerAt)
		} else {
			delete(device.Values, "grid_power")
		}
	}
}

func storedCommandMapping(command Command) Mapping {
	event := Event{
		CommandID:   command.CommandID,
		LogicalID:   command.LogicalID,
		GenericType: command.GenericType,
		ObjectName:  command.ObjectName,
		DeviceName:  command.Device,
		CommandName: firstNonEmpty(command.RawName, command.Name),
		Name:        firstNonEmpty(command.RawName, command.Name),
		Type:        command.Type,
		Subtype:     command.Subtype,
		Unit:        command.Unit,
	}
	return mappingFromCommandContract(command, MappingFor(event))
}

// derivedStateSourcePriority resolves equal-observation-time discovery seeds.
// Process weaker inference first so physical state/position feedback wins the
// final value. Observation time still takes precedence when feedback arrives
// later in normal operation.
func derivedStateSourcePriority(mapping Mapping, device Device) int {
	switch mapping.Metric {
	case "state":
		return 4
	case "valve_position":
		if isWaterStopDevice(device) {
			return 3
		}
	case "event_code":
		if isWallSwitchDevice(device) || isWaterStopDevice(device) {
			return 2
		}
	case "current_a", "power_w":
		if isWallSwitchDevice(device) {
			return 1
		}
	}
	return 0
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
		if key := actionCleanupKey(action); key != "" {
			byID[key] = action
		}
	}
	for _, action := range actions {
		if key := actionCleanupKey(action); key != "" {
			byID[key] = action
		}
	}
	ids := make([]string, 0, len(byID))
	for key := range byID {
		ids = append(ids, key)
	}
	sort.Strings(ids)
	device.PendingActionDiscoveryCleanups = make([]Action, 0, len(ids))
	for _, key := range ids {
		device.PendingActionDiscoveryCleanups = append(device.PendingActionDiscoveryCleanups, byID[key])
	}
}

func actionCleanupKey(action Action) string {
	commandID := strings.TrimSpace(action.CommandID)
	if commandID == "" {
		return ""
	}
	return commandID + "\x00" + Slug(action.DeviceSlug) + "\x00" + NormalizeControlAction(action.Action)
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
		if key := actionCleanupKey(action); key != "" {
			acknowledged[key] = struct{}{}
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
			if _, ok := acknowledged[actionCleanupKey(pending)]; ok {
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
	type candidate struct {
		device *Device
		action Action
	}
	candidates := make([]candidate, 0)
	for _, device := range s.devices {
		if device == nil {
			continue
		}
		for _, action := range device.Actions {
			if action.CommandID == commandID {
				candidates = append(candidates, candidate{device: device, action: actionForDevice(action, device)})
			}
		}
	}
	if len(candidates) == 0 {
		return Action{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		leftLegacy := s.isLegacyActionSourceLocked(left.device.DeviceSlug, commandID)
		rightLegacy := s.isLegacyActionSourceLocked(right.device.DeviceSlug, commandID)
		if leftLegacy != rightLegacy {
			return !leftLegacy
		}
		leftLinked := strings.EqualFold(left.device.LinkedSource, "sia")
		rightLinked := strings.EqualFold(right.device.LinkedSource, "sia")
		if leftLinked != rightLinked {
			return leftLinked
		}
		if left.device.DiscoveryDisabled != right.device.DiscoveryDisabled {
			return !left.device.DiscoveryDisabled
		}
		if left.device.publishRevision != right.device.publishRevision {
			return left.device.publishRevision > right.device.publishRevision
		}
		return left.device.DeviceSlug < right.device.DeviceSlug
	})
	return candidates[0].action, true
}

func (s *Store) isLegacyActionSourceLocked(deviceSlug, commandID string) bool {
	for _, device := range s.devices {
		if device == nil || !containsString(device.LegacyDeviceSlugs, deviceSlug) {
			continue
		}
		for _, action := range device.Actions {
			if action.CommandID == commandID {
				return true
			}
		}
	}
	return false
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
	if device == nil || !toggleCapableDevice(*device) || hasObservedAuthoritativeToggleFeedback(device) {
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
