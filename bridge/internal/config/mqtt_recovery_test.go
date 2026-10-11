package config

import (
	"os"
	"testing"
)

func TestHomeAssistantRecoveryDefaultsAndSourceIsolation(t *testing.T) {
	t.Setenv("AJAXBRIDGE_MQTT_HA_STATUS_TOPIC", "")
	if err := os.Unsetenv("AJAXBRIDGE_MQTT_HA_STATUS_TOPIC"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AJAXBRIDGE_MQTT_HA_ONLINE_PAYLOAD", "")
	for _, source := range []string{"", "appt"} {
		t.Setenv("AJAXBRIDGE_SOURCE_ID", source)
		cfg := FromEnv().WithSourceNamespace()
		if cfg.MQTTHomeAssistantStatusTopic != "homeassistant/status" || cfg.MQTTHomeAssistantOnlinePayload != "online" {
			t.Fatalf("source %q changed shared HA birth defaults", source)
		}
	}
	t.Setenv("AJAXBRIDGE_MQTT_HA_STATUS_TOPIC", " custom/ha/status ")
	t.Setenv("AJAXBRIDGE_MQTT_HA_ONLINE_PAYLOAD", "ready")
	cfg := FromEnv().WithSourceNamespace()
	if cfg.MQTTHomeAssistantStatusTopic != "custom/ha/status" || cfg.MQTTHomeAssistantOnlinePayload != "ready" {
		t.Fatal("custom HA birth configuration ignored or namespaced")
	}
	t.Setenv("AJAXBRIDGE_MQTT_HA_STATUS_TOPIC", "")
	if FromEnv().MQTTHomeAssistantStatusTopic != "" {
		t.Fatal("explicit empty topic did not disable recovery")
	}
}

func TestHomeAssistantRecoveryValidation(t *testing.T) {
	t.Setenv("AJAXBRIDGE_MQTT_BROKER", "tcp://fixture:1883")
	t.Setenv("AJAXBRIDGE_MQTT_HA_ONLINE_PAYLOAD", "online")
	for _, topic := range []string{"homeassistant/#", "+/status", "bad\x00topic"} {
		cfg := FromEnv()
		cfg.MQTTHomeAssistantStatusTopic = topic
		if cfg.Validate() == nil {
			t.Fatalf("invalid status topic accepted: %q", topic)
		}
	}
	cfg := FromEnv()
	cfg.MQTTHomeAssistantStatusTopic = "ha/status"
	cfg.MQTTHomeAssistantOnlinePayload = ""
	if cfg.Validate() == nil {
		t.Fatal("missing online payload accepted")
	}
}
