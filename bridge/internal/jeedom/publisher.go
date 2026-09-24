package jeedom

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

const (
	payloadOn                = "ON"
	payloadOff               = "OFF"
	switchStateValueTemplate = "{{ 'None' if value_json.get('state') is none else ('ON' if value_json.get('state') else 'OFF') }}"
)

type MQTTClient interface {
	PublishStateMessage(ctx context.Context, topic string, payload []byte, retain bool) error
	PublishDiscoveryMessage(ctx context.Context, key, topic string, payload []byte, retain bool) error
	AvailabilityTopic() string
}

type PublisherConfig struct {
	StateTopicPrefix string
	Discovery        bool
	DiscoveryPrefix  string
	DiscoveryNode    string
	RetainState      bool
	RetainDiscovery  bool
	Controls         bool
}

type Publisher struct {
	cfg     PublisherConfig
	mqtt    MQTTClient
	gatesMu sync.Mutex
	gates   map[string]*devicePublishGate
}

type devicePublishGate struct {
	mu             sync.Mutex
	latestRevision uint64
}

type DiscoveryDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
}

type DiscoveryConfig struct {
	Name                string          `json:"name"`
	UniqueID            string          `json:"unique_id"`
	StateTopic          string          `json:"state_topic,omitempty"`
	CommandTopic        string          `json:"command_topic,omitempty"`
	ValueTemplate       string          `json:"value_template,omitempty"`
	UnitOfMeasurement   string          `json:"unit_of_measurement,omitempty"`
	DeviceClass         string          `json:"device_class,omitempty"`
	StateClass          string          `json:"state_class,omitempty"`
	EntityCategory      string          `json:"entity_category,omitempty"`
	PayloadOn           string          `json:"payload_on,omitempty"`
	PayloadOff          string          `json:"payload_off,omitempty"`
	PayloadPress        string          `json:"payload_press,omitempty"`
	Optimistic          *bool           `json:"optimistic,omitempty"`
	AvailabilityTopic   string          `json:"availability_topic,omitempty"`
	PayloadAvailable    string          `json:"payload_available,omitempty"`
	PayloadNotAvailable string          `json:"payload_not_available,omitempty"`
	JSONAttributesTopic string          `json:"json_attributes_topic"`
	Device              DiscoveryDevice `json:"device"`
}

func NewPublisher(cfg PublisherConfig, mqtt MQTTClient) *Publisher {
	cfg.StateTopicPrefix = trimTopic(firstNonEmpty(cfg.StateTopicPrefix, "ajaxbridge/jeedom"))
	cfg.DiscoveryPrefix = trimTopic(firstNonEmpty(cfg.DiscoveryPrefix, "homeassistant"))
	cfg.DiscoveryNode = Slug(firstNonEmpty(cfg.DiscoveryNode, "ajaxbridge"))
	return &Publisher{cfg: cfg, mqtt: mqtt, gates: make(map[string]*devicePublishGate)}
}

func (p *Publisher) DiscoveryEnabled() bool {
	return p != nil && p.mqtt != nil && p.cfg.Discovery
}

func (p *Publisher) PublishDevice(ctx context.Context, device Device) error {
	_, err := p.PublishDeviceWithResult(ctx, device)
	return err
}

// PublishDeviceWithResult reports whether this snapshot actually passed the
// revision gate. Callers that acknowledge persisted discovery tombstones must
// only do so when published is true and err is nil.
func (p *Publisher) PublishDeviceWithResult(ctx context.Context, device Device) (published bool, err error) {
	if p == nil || p.mqtt == nil {
		return false, nil
	}
	release, current := p.beginDevicePublish(device)
	if !current {
		return false, nil
	}
	defer release()
	if err := p.CleanupCommands(ctx, device.PendingDiscoveryCleanups); err != nil {
		return false, err
	}
	if err := p.CleanupActions(ctx, device.PendingActionDiscoveryCleanups, device); err != nil {
		return false, err
	}
	if err := p.publishDevice(ctx, device); err != nil {
		return false, err
	}
	return true, nil
}

// CleanupCommands removes retained Home Assistant discovery for commands that
// disappeared from an authoritative Jeedom eqLogic discovery payload. State
// payloads are republished separately by PublishDevice without those metrics.
func (p *Publisher) CleanupCommands(ctx context.Context, commands []Command) error {
	if !p.DiscoveryEnabled() {
		return nil
	}
	commands = append([]Command(nil), commands...)
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].CommandID < commands[j].CommandID
	})
	for _, command := range commands {
		if err := p.publishCommandDiscoveryCleanup(ctx, command, "jeedom_removed_command_cleanup"); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) CleanupActions(ctx context.Context, actions []Action, device Device) error {
	if !p.DiscoveryEnabled() || len(actions) == 0 {
		return nil
	}
	actions = append([]Action(nil), actions...)
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].CommandID < actions[j].CommandID
	})
	for _, action := range actions {
		actionSlug := Slug(NormalizeControlAction(action.Action))
		if actionSlug == "" {
			continue
		}
		deviceSlugs := compactUniqueStrings([]string{device.DeviceSlug, action.DeviceSlug})
		for _, deviceSlug := range deviceSlugs {
			if actionSlug == "on" || actionSlug == "off" {
				if err := p.publishSwitchDiscoveryCleanup(ctx, deviceSlug, "jeedom_removed_action_cleanup"); err != nil {
					return err
				}
			}
			if err := p.publishButtonDiscoveryCleanup(ctx, deviceSlug, actionSlug, "jeedom_removed_action_cleanup"); err != nil {
				return err
			}
			if actionSlug == "on" && impulseCapableDevice(device) {
				if err := p.publishButtonDiscoveryCleanup(ctx, deviceSlug, "impulse", "jeedom_removed_action_cleanup"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (p *Publisher) beginDevicePublish(device Device) (func(), bool) {
	canonicalKey := Slug(device.DeviceSlug)
	keys := make([]string, 0, 1+len(device.LegacyDeviceSlugs))
	keys = append(keys, device.DeviceSlug)
	keys = append(keys, device.LegacyDeviceSlugs...)
	keys = compactUniqueStrings(keys)
	if len(keys) == 0 {
		return func() {}, true
	}
	sort.Strings(keys)

	p.gatesMu.Lock()
	if p.gates == nil {
		p.gates = make(map[string]*devicePublishGate)
	}
	gates := make([]*devicePublishGate, 0, len(keys))
	var canonicalGate *devicePublishGate
	for _, key := range keys {
		gate := p.gates[key]
		if gate == nil {
			gate = &devicePublishGate{}
			p.gates[key] = gate
		}
		gates = append(gates, gate)
		if key == canonicalKey {
			canonicalGate = gate
		}
	}
	p.gatesMu.Unlock()

	for _, gate := range gates {
		gate.mu.Lock()
	}
	release := func() {
		for _, gate := range slices.Backward(gates) {
			gate.mu.Unlock()
		}
	}
	if canonicalGate == nil {
		canonicalGate = gates[0]
	}

	// Hand-built Device values used by callers and tests predate revisions, but
	// they must not overwrite a slug after versioned Store traffic has begun.
	if device.publishRevision == 0 {
		if canonicalGate.latestRevision != 0 {
			release()
			return nil, false
		}
		return release, true
	}
	if device.publishRevision < canonicalGate.latestRevision {
		release()
		return nil, false
	}
	for _, gate := range gates {
		if device.publishRevision > gate.latestRevision {
			gate.latestRevision = device.publishRevision
		}
	}
	return release, true
}

func (p *Publisher) publishDevice(ctx context.Context, device Device) error {
	stateTopic := p.StateTopic(device.DeviceSlug)
	if p.cfg.Discovery {
		if err := p.publishLegacyCleanup(ctx, device); err != nil {
			return err
		}
		commands := sortedCommands(device.RawCommands)
		if device.DiscoveryDisabled {
			for _, command := range commands {
				if err := p.publishCommandLegacyNameDiscoveryCleanup(ctx, command, device); err != nil {
					return err
				}
				if err := p.publishCommandDiscoveryCleanup(ctx, command, "jeedom_discovery_cleanup"); err != nil {
					return err
				}
			}
			if p.cfg.Controls {
				if err := p.publishSwitchDiscoveryCleanup(ctx, device.DeviceSlug, "jeedom_switch_discovery_cleanup"); err != nil {
					return err
				}
				if err := p.publishKnownButtonDiscoveryCleanups(ctx, device.DeviceSlug, "jeedom_button_discovery_cleanup"); err != nil {
					return err
				}
			}
		} else {
			for _, command := range commands {
				if err := p.publishCommandLegacyNameDiscoveryCleanup(ctx, command, device); err != nil {
					return err
				}
				if !commandDiscoverable(command, device) {
					if err := p.publishCommandDiscoveryCleanup(ctx, command, "jeedom_merged_sia_cleanup"); err != nil {
						return err
					}
					continue
				}
				if err := p.publishCommandAlternateDiscoveryCleanup(ctx, command); err != nil {
					return err
				}
				topic, payload, err := p.BuildDiscovery(command, device)
				if err != nil {
					return err
				}
				key := "jeedom_discovery:" + command.Component + "/" + discoveryObjectID(command)
				if err := p.mqtt.PublishDiscoveryMessage(ctx, key, topic, payload, p.cfg.RetainDiscovery); err != nil {
					return err
				}
			}
			if p.cfg.Controls {
				switchActions := sortedSwitchActions(device)
				if len(switchActions) == 0 {
					if err := p.publishSwitchDiscoveryCleanup(ctx, device.DeviceSlug, "jeedom_switch_discovery_cleanup"); err != nil {
						return err
					}
				}
				for _, action := range switchActions {
					topic, payload, err := p.BuildSwitchDiscovery(action, device)
					if err != nil {
						return err
					}
					key := "jeedom_switch_discovery:" + action.DeviceSlug
					if err := p.mqtt.PublishDiscoveryMessage(ctx, key, topic, payload, p.cfg.RetainDiscovery); err != nil {
						return err
					}
				}

				buttonActions := sortedButtonActions(device)
				if len(buttonActions) == 0 {
					if err := p.publishKnownButtonDiscoveryCleanups(ctx, device.DeviceSlug, "jeedom_button_discovery_cleanup"); err != nil {
						return err
					}
				}
				for _, action := range buttonActions {
					topic, payload, err := p.BuildButtonDiscovery(action, device)
					if err != nil {
						return err
					}
					key := "jeedom_button_discovery:" + buttonObjectID(action, device)
					if err := p.mqtt.PublishDiscoveryMessage(ctx, key, topic, payload, p.cfg.RetainDiscovery); err != nil {
						return err
					}
				}
			}
		}
	}

	payload, err := json.Marshal(StatePayload(device))
	if err != nil {
		return err
	}
	if err := p.mqtt.PublishStateMessage(ctx, stateTopic, payload, p.cfg.RetainState); err != nil {
		return err
	}
	attributes, err := json.Marshal(AttributesPayload(device))
	if err != nil {
		return err
	}
	return p.mqtt.PublishStateMessage(ctx, p.AttributesTopic(device.DeviceSlug), attributes, true)
}

func (p *Publisher) publishCommandDiscoveryCleanup(ctx context.Context, command Command, prefix string) error {
	if command.Component == "" || command.Metric == "" {
		return nil
	}
	key := prefix + ":" + command.Component + "/" + discoveryObjectID(command)
	if err := p.mqtt.PublishDiscoveryMessage(ctx, key, p.discoveryTopic(command), []byte{}, true); err != nil {
		return err
	}
	for _, component := range alternateDiscoveryComponents(command.Component) {
		key := prefix + ":" + component + "/" + discoveryObjectID(command)
		if err := p.mqtt.PublishDiscoveryMessage(ctx, key, p.discoveryTopicFor(component, discoveryObjectID(command)), []byte{}, true); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) publishCommandAlternateDiscoveryCleanup(ctx context.Context, command Command) error {
	if command.Component == "" || command.Metric == "" {
		return nil
	}
	for _, component := range alternateDiscoveryComponents(command.Component) {
		key := "jeedom_component_migration_cleanup:" + component + "/" + discoveryObjectID(command)
		if err := p.mqtt.PublishDiscoveryMessage(ctx, key, p.discoveryTopicFor(component, discoveryObjectID(command)), []byte{}, true); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) publishCommandLegacyNameDiscoveryCleanup(ctx context.Context, command Command, device Device) error {
	if command.Metric == "" {
		return nil
	}
	for _, objectID := range legacyCommandObjectIDs(command, device) {
		for _, component := range commandCleanupComponents(command.Component) {
			if component == "" {
				continue
			}
			if component == command.Component && objectID == discoveryObjectID(command) {
				continue
			}
			key := "jeedom_legacy_name_cleanup:" + component + "/" + objectID
			if err := p.mqtt.PublishDiscoveryMessage(ctx, key, p.discoveryTopicFor(component, objectID), []byte{}, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Publisher) publishLegacyCleanup(ctx context.Context, device Device) error {
	if len(device.LegacyDeviceSlugs) == 0 {
		return nil
	}
	for _, legacySlug := range device.LegacyDeviceSlugs {
		legacySlug = Slug(legacySlug)
		if legacySlug == "" || legacySlug == device.DeviceSlug {
			continue
		}
		if p.cfg.Controls {
			if err := p.publishSwitchDiscoveryCleanup(ctx, legacySlug, "jeedom_legacy_switch_cleanup"); err != nil {
				return err
			}
			if err := p.publishKnownButtonDiscoveryCleanups(ctx, legacySlug, "jeedom_legacy_button_cleanup"); err != nil {
				return err
			}
		}
		if err := p.mqtt.PublishStateMessage(ctx, p.StateTopic(legacySlug), []byte{}, true); err != nil {
			return err
		}
		if err := p.mqtt.PublishStateMessage(ctx, p.AttributesTopic(legacySlug), []byte{}, true); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) StateTopic(deviceSlug string) string {
	return p.cfg.StateTopicPrefix + "/devices/" + Slug(deviceSlug) + "/state"
}

func (p *Publisher) AttributesTopic(deviceSlug string) string {
	return p.cfg.StateTopicPrefix + "/devices/" + Slug(deviceSlug) + "/attributes"
}

func (p *Publisher) CommandTopic(deviceSlug string) string {
	return p.cfg.StateTopicPrefix + "/devices/" + Slug(deviceSlug) + "/set"
}

func (p *Publisher) BuildDiscovery(command Command, device Device) (string, []byte, error) {
	if command.Component == "" {
		return "", nil, fmt.Errorf("command %q has no Home Assistant component", command.CommandID)
	}

	stateTopic := p.StateTopic(device.DeviceSlug)
	attributesTopic := p.AttributesTopic(device.DeviceSlug)
	cfg := DiscoveryConfig{
		Name:                firstNonEmpty(commandName(command), titleName(command.Metric)),
		UniqueID:            discoveryUniqueID(command),
		StateTopic:          stateTopic,
		ValueTemplate:       valueTemplate(command),
		UnitOfMeasurement:   command.Unit,
		DeviceClass:         command.DeviceClass,
		StateClass:          command.StateClass,
		EntityCategory:      command.EntityCategory,
		JSONAttributesTopic: attributesTopic,
		Device: DiscoveryDevice{
			Identifiers:  discoveryIdentifiers(device),
			Name:         device.Device,
			Manufacturer: firstNonEmpty(device.HAManufacturer, "Ajax via Jeedom"),
			Model:        firstNonEmpty(device.HAModel, "Jeedom MQTT Bridge"),
		},
	}
	if command.Component == ComponentBinarySensor {
		cfg.PayloadOn = payloadOn
		cfg.PayloadOff = payloadOff
	}
	if p.mqtt != nil && p.mqtt.AvailabilityTopic() != "" {
		cfg.AvailabilityTopic = p.mqtt.AvailabilityTopic()
		cfg.PayloadAvailable = "online"
		cfg.PayloadNotAvailable = "offline"
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return "", nil, err
	}
	return p.discoveryTopic(command), payload, nil
}

func (p *Publisher) BuildSwitchDiscovery(action Action, device Device) (string, []byte, error) {
	stateTopic := p.StateTopic(device.DeviceSlug)
	attributesTopic := p.AttributesTopic(device.DeviceSlug)
	optimistic := false
	cfg := DiscoveryConfig{
		Name:                "Control",
		UniqueID:            "ajaxbridge_jeedom_control_" + Slug(device.DeviceSlug),
		StateTopic:          stateTopic,
		CommandTopic:        p.CommandTopic(device.DeviceSlug),
		ValueTemplate:       switchStateValueTemplate,
		PayloadOn:           payloadOn,
		PayloadOff:          payloadOff,
		Optimistic:          &optimistic,
		JSONAttributesTopic: attributesTopic,
		Device: DiscoveryDevice{
			Identifiers:  discoveryIdentifiers(device),
			Name:         device.Device,
			Manufacturer: firstNonEmpty(device.HAManufacturer, "Ajax via Jeedom"),
			Model:        firstNonEmpty(device.HAModel, "Jeedom MQTT Bridge"),
		},
	}
	if p.mqtt != nil && p.mqtt.AvailabilityTopic() != "" {
		cfg.AvailabilityTopic = p.mqtt.AvailabilityTopic()
		cfg.PayloadAvailable = "online"
		cfg.PayloadNotAvailable = "offline"
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return "", nil, err
	}
	topic := p.discoveryTopicFor(ComponentSwitch, switchObjectID(device.DeviceSlug))
	return topic, payload, nil
}

func (p *Publisher) BuildButtonDiscovery(action Action, device Device) (string, []byte, error) {
	cfg := DiscoveryConfig{
		Name:                buttonControlName(action, device),
		UniqueID:            "ajaxbridge_jeedom_control_" + Slug(device.DeviceSlug) + "_" + buttonActionSlug(action, device),
		CommandTopic:        p.CommandTopic(device.DeviceSlug),
		PayloadPress:        controlPayload(action.Action),
		JSONAttributesTopic: p.AttributesTopic(device.DeviceSlug),
		Device: DiscoveryDevice{
			Identifiers:  discoveryIdentifiers(device),
			Name:         device.Device,
			Manufacturer: firstNonEmpty(device.HAManufacturer, "Ajax via Jeedom"),
			Model:        firstNonEmpty(device.HAModel, "Jeedom MQTT Bridge"),
		},
	}
	if p.mqtt != nil && p.mqtt.AvailabilityTopic() != "" {
		cfg.AvailabilityTopic = p.mqtt.AvailabilityTopic()
		cfg.PayloadAvailable = "online"
		cfg.PayloadNotAvailable = "offline"
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return "", nil, err
	}
	return p.discoveryTopicFor(ComponentButton, buttonObjectID(action, device)), payload, nil
}

func (p *Publisher) discoveryTopic(command Command) string {
	return p.discoveryTopicFor(command.Component, discoveryObjectID(command))
}

func (p *Publisher) discoveryTopicFor(component, objectID string) string {
	return strings.Join([]string{p.cfg.DiscoveryPrefix, component, p.cfg.DiscoveryNode, objectID, "config"}, "/")
}

func alternateDiscoveryComponents(component string) []string {
	switch component {
	case ComponentSensor:
		return []string{ComponentBinarySensor}
	case ComponentBinarySensor:
		return []string{ComponentSensor}
	default:
		return nil
	}
}

func commandCleanupComponents(component string) []string {
	components := []string{component}
	components = append(components, alternateDiscoveryComponents(component)...)
	return compactUniqueStrings(components)
}

func discoveryIdentifiers(device Device) []string {
	if len(device.HAIdentifiers) == 0 {
		return []string{"ajaxbridge_jeedom_" + device.DeviceSlug}
	}
	return append([]string(nil), device.HAIdentifiers...)
}

func sortedSwitchActions(device Device) []Action {
	if !toggleCapableDevice(device) {
		return nil
	}

	on, hasOn := device.Actions["on"]
	off, hasOff := device.Actions["off"]
	if !hasOn || !hasOff || !on.Allowed || !off.Allowed {
		return nil
	}
	on.StateCommandID = firstNonEmpty(on.StateCommandID, off.StateCommandID)
	return []Action{on}
}

func sortedButtonActions(device Device) []Action {
	if impulseCapableDevice(device) {
		if impulse, ok := device.Actions["impulse"]; ok && impulse.Allowed {
			return []Action{impulse}
		}
		if on, ok := device.Actions["on"]; ok && on.Allowed {
			return []Action{on}
		}
		return nil
	}
	if !securityButtonCapableDevice(device) {
		return nil
	}

	actions := make([]Action, 0, len(securityButtonActionOrder))
	for _, actionName := range securityButtonActionOrder {
		if action, ok := device.Actions[actionName]; ok && action.Allowed {
			actions = append(actions, action)
		}
	}
	return actions
}

func toggleCapableDevice(device Device) bool {
	switch commandKey(firstNonEmpty(device.JeedomDeviceType, device.HAModel)) {
	case "socket", "wallswitch", "lightswitch", "outlet", "waterstop":
		return true
	default:
		return false
	}
}

func impulseCapableDevice(device Device) bool {
	return commandKey(firstNonEmpty(device.JeedomDeviceType, device.HAModel)) == "relay"
}

func securityButtonCapableDevice(device Device) bool {
	return isHubControlDevice(firstNonEmpty(device.JeedomDeviceType, device.HAModel))
}

func isHubControlDevice(deviceType string) bool {
	switch commandKey(deviceType) {
	case "hub", "hub2", "hub2plus", "hub_2_plus", "hubplus", "hub_plus", "hubhybrid", "hub_hybrid":
		return true
	default:
		return false
	}
}

var securityButtonActionOrder = []string{
	"arm",
	"night_mode",
	"disarm",
	"panic",
	"mute_fire_detectors",
}

func switchObjectID(deviceSlug string) string {
	return "jeedom_control_" + Slug(deviceSlug)
}

func buttonObjectID(action Action, device Device) string {
	return "jeedom_control_" + Slug(device.DeviceSlug) + "_" + buttonActionSlug(action, device)
}

func buttonActionSlug(action Action, device Device) string {
	if impulseCapableDevice(device) && NormalizeControlAction(action.Action) == "on" {
		return "impulse"
	}
	return Slug(NormalizeControlAction(action.Action))
}

func buttonControlName(action Action, device Device) string {
	if impulseCapableDevice(device) && (NormalizeControlAction(action.Action) == "on" || NormalizeControlAction(action.Action) == "impulse") {
		return "Impulse"
	}
	return firstNonEmpty(action.Name, EnglishActionName(action.Action, action.RawName), "Action")
}

func controlPayload(action string) string {
	switch NormalizeControlAction(action) {
	case "on":
		return payloadOn
	case "off":
		return payloadOff
	default:
		return strings.ToUpper(NormalizeControlAction(action))
	}
}

func sortedCommands(commands map[string]Command) []Command {
	out := make([]Command, 0, len(commands))
	for _, command := range commands {
		if command.Component != "" && command.Metric != "" {
			out = append(out, command)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CommandID < out[j].CommandID
	})
	return out
}

func commandDiscoverable(command Command, device Device) bool {
	switch command.Metric {
	case "event_source", "event", "event_code":
		return false
	}
	if command.Component == ComponentSensor && measurementStateClass(command.StateClass) && !measurementCommandHasUsableValue(command, device) {
		return false
	}
	return !deviceLinkedToSIA(device) || !siaOwnedJeedomMetric(command.Metric)
}

func measurementCommandHasUsableValue(command Command, device Device) bool {
	if !command.LastValueAt.IsZero() {
		return true
	}
	if _, usable := numericValue(command.Value); usable {
		return true
	}
	if !soleCommandForMetric(command, device) {
		return false
	}
	_, usable := numericValue(device.Values[command.Metric])
	return usable
}

func measurementStateClass(stateClass string) bool {
	switch strings.ToLower(strings.TrimSpace(stateClass)) {
	case "measurement", "total", "total_increasing":
		return true
	default:
		return false
	}
}

func soleCommandForMetric(command Command, device Device) bool {
	count := 0
	for _, candidate := range device.RawCommands {
		if candidate.Metric == command.Metric {
			count++
		}
	}
	return count == 1
}

func deviceLinkedToSIA(device Device) bool {
	return strings.EqualFold(strings.TrimSpace(device.LinkedSource), "sia") ||
		(strings.TrimSpace(device.LinkedAccount) != "" && strings.TrimSpace(device.LinkedZone) != "") ||
		hasSIAIdentifier(device.HAIdentifiers)
}

func hasSIAIdentifier(identifiers []string) bool {
	for _, identifier := range identifiers {
		identifier = strings.ToLower(strings.TrimSpace(identifier))
		if strings.HasPrefix(identifier, "ajaxbridge_") && strings.Contains(identifier, "_zone_") {
			return true
		}
	}
	return false
}

func siaOwnedJeedomMetric(metric string) bool {
	switch strings.ToLower(strings.TrimSpace(metric)) {
	case "alarm", "alarm_active",
		"smoke_alarm", "heat_alarm", "carbon_monoxide_alarm",
		"tamper",
		"trouble", "trouble_active",
		"external_power",
		"bypass", "tamper_bypass",
		"battery_low",
		"connectivity", "hardware", "firmware", "fire_detector",
		"fire", "smoke", "co", "gas", "gas_or_co",
		"water_leak",
		"interference", "accelerometer":
		return true
	default:
		return false
	}
}

func discoveryObjectID(command Command) string {
	if command.CommandID != "" {
		return "jeedom_cmd_" + Slug(command.CommandID)
	}
	return "jeedom_" + command.DeviceSlug + "_" + Slug(command.Metric)
}

func discoveryUniqueID(command Command) string {
	if command.CommandID != "" {
		return "ajaxbridge_jeedom_cmd_" + Slug(command.CommandID)
	}
	return "ajaxbridge_jeedom_" + command.DeviceSlug + "_" + Slug(command.Metric)
}

func legacyCommandObjectIDs(command Command, device Device) []string {
	slugs := legacyCommandDeviceSlugs(command, device)
	suffixes := legacyCommandSuffixes(command)
	objectIDs := make([]string, 0, len(slugs)*len(suffixes))
	for _, slug := range slugs {
		for _, suffix := range suffixes {
			objectIDs = append(objectIDs, slug+"_"+suffix)
		}
	}
	return compactUniqueStrings(objectIDs)
}

func legacyCommandDeviceSlugs(command Command, device Device) []string {
	return compactUniqueStrings(append(
		[]string{
			device.BaseSlug,
			device.DeviceSlug,
			Slug(device.Device),
			Slug(command.Device),
			Slug(command.ObjectName),
		},
		device.LegacyDeviceSlugs...,
	))
}

func legacyCommandSuffixes(command Command) []string {
	suffixes := []string{
		Slug(command.Metric),
		Slug(command.Name),
		Slug(command.RawName),
	}
	suffixes = append(suffixes, legacyMetricAliases(command.Metric)...)
	return compactUniqueStrings(suffixes)
}

func legacyMetricAliases(metric string) []string {
	switch strings.ToLower(strings.TrimSpace(metric)) {
	case "battery_percent":
		return []string{"battery", "batterie"}
	case "battery_state":
		return []string{"battery_state", "etat_de_la_batterie", "etatdelabatterie"}
	case "temperature_c":
		return []string{"temperature"}
	case "power_w":
		return []string{"power", "puissance"}
	case "current_a":
		return []string{"current", "courant"}
	case "voltage_v":
		return []string{"voltage", "tension"}
	case "energy_kwh":
		return []string{"energy", "energie"}
	case "external_power":
		return []string{"external_power", "mains", "alimentation_secteur"}
	case "signal_level", "signal_dbm":
		return []string{"signal"}
	case "humidity_percent":
		return []string{"humidity", "humidite"}
	case "grid_power":
		return []string{"grid_power", "power", "mains"}
	default:
		return nil
	}
}

func valueTemplate(command Command) string {
	if command.Component == ComponentBinarySensor {
		return "{{ 'None' if value_json.get('" + command.Metric + "') is none else ('" + payloadOn + "' if value_json.get('" + command.Metric + "') else '" + payloadOff + "') }}"
	}
	return "{{ value_json.get('" + command.Metric + "') }}"
}

func commandName(command Command) string {
	switch command.Metric {
	case "power_w":
		return "Power"
	case "current_a":
		return "Current"
	case "voltage_v":
		return "Voltage"
	case "energy_kwh":
		return "Energy"
	case "temperature_c":
		return "Temperature"
	case "battery_percent":
		return "Battery"
	case "battery_state":
		return "Battery state"
	case "signal_dbm":
		return "Signal"
	case "signal_level":
		return "Signal"
	case "humidity_percent":
		return "Humidity"
	case "state":
		return "State"
	case "event_source":
		return "Event source"
	case "event":
		return "Event"
	case "event_code":
		return "Event code"
	case "online":
		return "Online"
	case "external_power":
		return "External power"
	case "gsm_network_type":
		return "GSM network type"
	case "cellular_data_active":
		return "Cellular data active"
	case "opening":
		return "Opening"
	case "door":
		return "Door"
	case "leak":
		return "Leak"
	case "tamper":
		return "Tamper"
	default:
		return titleName(command.Name)
	}
}

func trimTopic(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func compactUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
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
	return out
}

func (p *Publisher) publishSwitchDiscoveryCleanup(ctx context.Context, deviceSlug, keyPrefix string) error {
	return p.mqtt.PublishDiscoveryMessage(
		ctx,
		keyPrefix+":"+Slug(deviceSlug),
		p.discoveryTopicFor(ComponentSwitch, switchObjectID(deviceSlug)),
		[]byte{},
		true,
	)
}

func (p *Publisher) publishButtonDiscoveryCleanup(ctx context.Context, deviceSlug, actionSlug, keyPrefix string) error {
	objectID := "jeedom_control_" + Slug(deviceSlug) + "_" + Slug(actionSlug)
	return p.mqtt.PublishDiscoveryMessage(
		ctx,
		keyPrefix+":"+Slug(deviceSlug)+":"+Slug(actionSlug),
		p.discoveryTopicFor(ComponentButton, objectID),
		[]byte{},
		true,
	)
}

func (p *Publisher) publishKnownButtonDiscoveryCleanups(ctx context.Context, deviceSlug, keyPrefix string) error {
	for _, actionSlug := range append([]string{"impulse", "on"}, securityButtonActionOrder...) {
		if err := p.publishButtonDiscoveryCleanup(ctx, deviceSlug, actionSlug, keyPrefix); err != nil {
			return err
		}
	}
	return nil
}
