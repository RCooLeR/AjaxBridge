package jeedom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrUnsupportedDiscoveryTopic = errors.New("unsupported Jeedom discovery topic")

type Discovery struct {
	Topic        string                      `json:"topic"`
	EqLogicID    string                      `json:"eq_logic_id"`
	Name         string                      `json:"name"`
	LogicalID    string                      `json:"logical_id"`
	ObjectName   string                      `json:"object_name,omitempty"`
	EqType       string                      `json:"eq_type,omitempty"`
	DeviceType   string                      `json:"device_type,omitempty"`
	ApplyDevice  string                      `json:"apply_device,omitempty"`
	HubID        string                      `json:"hub_id,omitempty"`
	Enabled      bool                        `json:"enabled"`
	Visible      bool                        `json:"visible"`
	InfoCommands map[string]DiscoveryCommand `json:"info_commands"`
	Actions      map[string]DiscoveryCommand `json:"actions"`
	ReceivedAt   time.Time                   `json:"received_at"`
	RawPayload   json.RawMessage             `json:"raw_payload"`
}

type DiscoveryCommand struct {
	CommandID      string          `json:"command_id"`
	EqLogicID      string          `json:"eq_logic_id"`
	LogicalID      string          `json:"logical_id,omitempty"`
	GenericType    string          `json:"generic_type,omitempty"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Subtype        string          `json:"subtype"`
	Unit           string          `json:"unit,omitempty"`
	UnitProvided   bool            `json:"unit_provided,omitempty"`
	Visible        bool            `json:"visible"`
	Historized     bool            `json:"historized,omitempty"`
	StateCommandID string          `json:"state_command_id,omitempty"`
	Value          json.RawMessage `json:"value,omitempty"`
}

type discoveryPayload struct {
	ID            json.RawMessage                    `json:"id"`
	Name          string                             `json:"name"`
	LogicalID     string                             `json:"logicalId"`
	EqType        string                             `json:"eqType_name"`
	ObjectName    string                             `json:"object_name"`
	IsVisible     json.RawMessage                    `json:"isVisible"`
	IsEnable      json.RawMessage                    `json:"isEnable"`
	Configuration map[string]any                     `json:"configuration"`
	Cmds          map[string]discoveryCommandPayload `json:"cmds"`
}

type discoveryCommandPayload struct {
	ID           json.RawMessage `json:"id"`
	EqLogicID    json.RawMessage `json:"eqLogic_id"`
	LogicalID    string          `json:"logicalId"`
	GenericType  string          `json:"generic_type"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Subtype      string          `json:"subType"`
	Unit         *string         `json:"unite"`
	IsVisible    json.RawMessage `json:"isVisible"`
	IsHistorized json.RawMessage `json:"isHistorized"`
	Value        json.RawMessage `json:"value"`
	CurrentValue json.RawMessage `json:"currentValue"`
	State        json.RawMessage `json:"state"`
}

func ParseDiscoveryMessage(topic string, body []byte, receivedAt time.Time) (Discovery, error) {
	eqLogicID, err := EqLogicIDFromDiscoveryTopic(topic)
	if err != nil {
		return Discovery{Topic: topic, ReceivedAt: receivedAt, RawPayload: append([]byte(nil), body...)}, err
	}

	var raw discoveryPayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return Discovery{Topic: topic, EqLogicID: eqLogicID, ReceivedAt: receivedAt, RawPayload: append([]byte(nil), body...)}, err
	}
	if id := rawIDString(raw.ID); id != "" {
		eqLogicID = id
	}

	discovery := Discovery{
		Topic:        topic,
		EqLogicID:    eqLogicID,
		Name:         RepairText(raw.Name),
		LogicalID:    strings.TrimSpace(raw.LogicalID),
		ObjectName:   RepairText(raw.ObjectName),
		EqType:       strings.TrimSpace(raw.EqType),
		DeviceType:   configString(raw.Configuration, "device"),
		ApplyDevice:  configString(raw.Configuration, "applyDevice"),
		HubID:        configString(raw.Configuration, "hub_id"),
		Enabled:      rawBool(raw.IsEnable, true),
		Visible:      rawBool(raw.IsVisible, true),
		InfoCommands: make(map[string]DiscoveryCommand),
		Actions:      make(map[string]DiscoveryCommand),
		ReceivedAt:   receivedAt,
		RawPayload:   append([]byte(nil), body...),
	}
	if discovery.DeviceType == "" {
		discovery.DeviceType = discovery.ApplyDevice
	}
	if discovery.ApplyDevice == "" {
		discovery.ApplyDevice = discovery.DeviceType
	}

	for key, cmd := range raw.Cmds {
		command := discoveryCommand(key, eqLogicID, cmd)
		switch strings.ToLower(command.Type) {
		case "action":
			discovery.Actions[command.CommandID] = command
		default:
			discovery.InfoCommands[command.CommandID] = command
		}
	}
	return discovery, nil
}

func EqLogicIDFromDiscoveryTopic(topic string) (string, error) {
	topic = strings.Trim(strings.TrimSpace(topic), "/")
	parts := strings.Split(topic, "/")
	if len(parts) < 4 || parts[len(parts)-3] != "discovery" || parts[len(parts)-2] != "eqLogic" {
		return "", ErrUnsupportedDiscoveryTopic
	}
	id := strings.TrimSpace(parts[len(parts)-1])
	if id == "" || id == "#" || id == "+" {
		return "", ErrUnsupportedDiscoveryTopic
	}
	return id, nil
}

func IsDiscoveryEqLogicTopic(topic string) bool {
	_, err := EqLogicIDFromDiscoveryTopic(topic)
	return err == nil
}

func discoveryCommand(mapKey, eqLogicID string, raw discoveryCommandPayload) DiscoveryCommand {
	commandID := firstNonEmpty(rawIDString(raw.ID), mapKey)
	return DiscoveryCommand{
		CommandID:      commandID,
		EqLogicID:      firstNonEmpty(rawIDString(raw.EqLogicID), eqLogicID),
		LogicalID:      strings.TrimSpace(raw.LogicalID),
		GenericType:    strings.TrimSpace(raw.GenericType),
		Name:           RepairText(raw.Name),
		Type:           strings.ToLower(strings.TrimSpace(raw.Type)),
		Subtype:        strings.ToLower(strings.TrimSpace(raw.Subtype)),
		Unit:           RepairText(sourceUnitValue(raw.Unit)),
		UnitProvided:   raw.Unit != nil,
		Visible:        rawBool(raw.IsVisible, false),
		Historized:     rawBool(raw.IsHistorized, false),
		StateCommandID: rawString(raw.Value),
		Value:          copyRawMessage(firstNonEmptyRaw(raw.CurrentValue, raw.Value, raw.State)),
	}
}

func firstNonEmptyRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if !EmptyRawValue(value) {
			return value
		}
	}
	return nil
}

func copyRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func configString(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func rawIDString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.String()
	}
	return ""
}

func rawString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	return rawIDString(raw)
}

func rawBool(raw json.RawMessage, fallback bool) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return fallback
	}
	var boolean bool
	if err := json.Unmarshal(raw, &boolean); err == nil {
		return boolean
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number != 0
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	default:
		if number, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64); err == nil {
			return number != 0
		}
		return fallback
	}
}
