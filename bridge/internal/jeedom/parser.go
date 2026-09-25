package jeedom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	ErrMalformedHumanName = errors.New("malformed Jeedom humanName")
	ErrMissingCommandID   = errors.New("missing Jeedom command id in MQTT topic")
)

type Event struct {
	Topic        string          `json:"topic"`
	CommandID    string          `json:"command_id"`
	ObjectName   string          `json:"object"`
	DeviceName   string          `json:"device"`
	CommandName  string          `json:"command"`
	Name         string          `json:"name"`
	LogicalID    string          `json:"logical_id"`
	GenericType  string          `json:"generic_type"`
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype"`
	Unit         string          `json:"unit"`
	UnitProvided bool            `json:"unit_provided,omitempty"`
	Value        json.RawMessage `json:"value"`
	ReceivedAt   time.Time       `json:"received_at"`
	RawPayload   json.RawMessage `json:"raw_payload"`
}

type payload struct {
	Value          json.RawMessage `json:"value"`
	HumanName      string          `json:"humanName"`
	Unite          *string         `json:"unite"`
	Name           string          `json:"name"`
	LogicalID      string          `json:"logicalId"`
	LogicalIDAlt   string          `json:"logical_id"`
	GenericType    string          `json:"generic_type"`
	GenericTypeAlt string          `json:"genericType"`
	Type           string          `json:"type"`
	Subtype        string          `json:"subtype"`
}

func ParseMessage(topic string, body []byte, receivedAt time.Time) (Event, error) {
	commandID, err := CommandIDFromTopic(topic)
	if err != nil {
		return Event{Topic: topic, ReceivedAt: receivedAt, RawPayload: append([]byte(nil), body...)}, err
	}

	var raw payload
	if err := json.Unmarshal(body, &raw); err != nil {
		return Event{Topic: topic, CommandID: commandID, ReceivedAt: receivedAt, RawPayload: append([]byte(nil), body...)}, err
	}

	objectName, deviceName, commandName, err := ParseHumanName(raw.HumanName)
	if err != nil {
		return Event{Topic: topic, CommandID: commandID, ReceivedAt: receivedAt, RawPayload: append([]byte(nil), body...)}, err
	}
	if commandName == "" {
		commandName = raw.Name
	}

	return Event{
		Topic:        topic,
		CommandID:    commandID,
		ObjectName:   RepairText(objectName),
		DeviceName:   RepairText(deviceName),
		CommandName:  RepairText(commandName),
		Name:         RepairText(raw.Name),
		LogicalID:    RepairText(firstNonEmpty(raw.LogicalID, raw.LogicalIDAlt)),
		GenericType:  RepairText(firstNonEmpty(raw.GenericType, raw.GenericTypeAlt)),
		Type:         strings.TrimSpace(raw.Type),
		Subtype:      strings.TrimSpace(raw.Subtype),
		Unit:         RepairText(sourceUnitValue(raw.Unite)),
		UnitProvided: raw.Unite != nil,
		Value:        append(json.RawMessage(nil), raw.Value...),
		ReceivedAt:   receivedAt,
		RawPayload:   append([]byte(nil), body...),
	}, nil
}

func CommandIDFromTopic(topic string) (string, error) {
	topic = strings.Trim(strings.TrimSpace(topic), "/")
	if topic == "" {
		return "", ErrMissingCommandID
	}
	parts := strings.Split(topic, "/")
	id := strings.TrimSpace(parts[len(parts)-1])
	if id == "" || id == "#" || id == "+" {
		return "", ErrMissingCommandID
	}
	return id, nil
}

func IsCommandEventTopic(topic string) bool {
	topic = strings.Trim(strings.TrimSpace(topic), "/")
	if topic == "" {
		return false
	}
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		return false
	}
	if parts[len(parts)-3] != "cmd" || parts[len(parts)-2] != "event" {
		return false
	}
	id := strings.TrimSpace(parts[len(parts)-1])
	return id != "" && id != "#" && id != "+"
}

func ParseHumanName(value string) (objectName, deviceName, commandName string, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", ErrMalformedHumanName
	}

	var parts []string
	rest := value
	for rest != "" {
		if !strings.HasPrefix(rest, "[") {
			return "", "", "", fmt.Errorf("%w: %q", ErrMalformedHumanName, value)
		}
		end := strings.Index(rest, "]")
		if end < 0 {
			return "", "", "", fmt.Errorf("%w: %q", ErrMalformedHumanName, value)
		}
		parts = append(parts, rest[1:end])
		rest = rest[end+1:]
	}
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("%w: %q", ErrMalformedHumanName, value)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), nil
}

func (e Event) EmptyValue() bool {
	return EmptyRawValue(e.Value)
}

func EmptyRawValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil && strings.TrimSpace(text) == "" {
		return true
	}
	return false
}

func NumericRawValue(value json.RawMessage) (float64, bool) {
	if EmptyRawValue(value) {
		return 0, false
	}
	var number float64
	if err := json.Unmarshal(value, &number); err == nil {
		return number, true
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return 0, false
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, ",", "."))
	if text == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func BoolRawValue(value json.RawMessage) (bool, bool) {
	if EmptyRawValue(value) {
		return false, false
	}
	var boolean bool
	if err := json.Unmarshal(value, &boolean); err == nil {
		return boolean, true
	}
	var number float64
	if err := json.Unmarshal(value, &number); err == nil {
		return number != 0, true
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return false, false
	}
	token := boolEnumToken(text)
	switch token {
	case "1", "TRUE", "ON", "OPEN", "OPENED", "OUVERT", "OUVERTE", "OUI", "YES", "ACTIVE", "ACTIF",
		"CONNECTED", "CONNECTE", "CONNECTEE", "ENABLED", "CHARGING", "EN_CHARGE":
		return true, true
	case "0", "FALSE", "OFF", "CLOSED", "CLOSE", "FERME", "FERMEE", "NON", "NO", "INACTIVE", "INACTIF",
		"DISCONNECTED", "DECONNECTED", "DECONNECTE", "DECONNECTEE", "NOT_CONNECTED", "DISABLED", "DESACTIVE", "DESACTIVEE", "NOT_ENABLED",
		"NOT_CHARGING", "PAS_EN_CHARGE", "DISCHARGING", "UNDETECTED":
		return false, true
	}
	if token == "NOT_DETECTED" || strings.HasSuffix(token, "_NOT_DETECTED") || strings.HasSuffix(token, "_UNDETECTED") {
		return false, true
	}
	if token == "DETECTED" || strings.HasSuffix(token, "_DETECTED") {
		return true, true
	}
	return false, false
}

// BoolRawValueForMapping resolves enum values whose polarity depends on what
// the entity represents. In particular, OK is false for a problem sensor but
// true for a connectivity sensor; treating it globally would invert one of the
// two. Callers should use this helper when a Mapping is available.
func BoolRawValueForMapping(value json.RawMessage, mapping Mapping) (bool, bool) {
	if result, ok := BoolRawValue(value); ok {
		return result, true
	}
	if !mapping.Binary || EmptyRawValue(value) {
		return false, false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return false, false
	}
	token := boolEnumToken(text)
	switch mapping.DeviceClass {
	case "problem":
		switch token {
		case "FAULT", "FAILED", "FAILURE", "ERROR", "DEFAUT", "ECHEC":
			return true, true
		case "OK", "NORMAL", "HEALTHY", "NO_FAULT", "NOT_FAILED":
			return false, true
		}
	case "connectivity":
		switch token {
		case "OK", "ONLINE", "AVAILABLE":
			return true, true
		case "FAULT", "FAILED", "OFFLINE", "UNAVAILABLE":
			return false, true
		}
	}
	return false, false
}

func boolEnumToken(value string) string {
	value = strings.TrimSpace(RepairText(value))
	var b strings.Builder
	separator := false
	for _, r := range value {
		r = unicode.ToUpper(foldLatin(r))
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
			separator = false
			continue
		}
		separator = true
	}
	return b.String()
}

func StringRawValue(value json.RawMessage) (string, bool) {
	if EmptyRawValue(value) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return EnglishValue(text), true
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.String(), true
	}
	var boolean bool
	if err := json.Unmarshal(value, &boolean); err == nil {
		if boolean {
			return "true", true
		}
		return "false", true
	}
	return string(value), true
}
