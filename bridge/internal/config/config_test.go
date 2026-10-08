package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func clearSourceEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"AJAXBRIDGE_SOURCE_ID", "AJAXBRIDGE_MQTT_CLIENT_ID", "AJAX2PROM_MQTT_CLIENT_ID",
		"AJAXBRIDGE_MQTT_TOPIC_PREFIX", "AJAX2PROM_MQTT_TOPIC_PREFIX",
		"AJAXBRIDGE_JEEDOM_STATE_TOPIC_PREFIX", "AJAXBRIDGE_JEEDOM_EVENT_TOPIC",
		"AJAXBRIDGE_JEEDOM_DISCOVERY_TOPIC", "AJAXBRIDGE_JEEDOM_SET_TOPIC_PREFIX",
	} {
		t.Setenv(name, "")
	}
}

func TestSourceDefaultsPreserveLegacyAndIsolateNextInstallation(t *testing.T) {
	clearSourceEnv(t)
	legacy := FromEnv().WithSourceNamespace()
	if legacy.SourceID != "" || legacy.MQTTClientID != "ajaxbridge" || legacy.MQTTTopicPrefix != "ajaxbridge" || legacy.JeedomStateTopicPrefix != "ajaxbridge/jeedom" {
		t.Fatalf("legacy defaults changed: %+v", legacy)
	}
	t.Setenv("AJAXBRIDGE_SOURCE_ID", "apartment")
	scoped := FromEnv().WithSourceNamespace()
	if scoped.MQTTClientID != "ajaxbridge-apartment" || scoped.MQTTTopicPrefix != "ajaxbridge/apartment" || scoped.JeedomStateTopicPrefix != "ajaxbridge/jeedom/apartment" {
		t.Fatalf("outgoing topics not isolated: %+v", scoped)
	}
	if scoped.JeedomEventTopic != "jeedom_apartment/cmd/event/#" || scoped.JeedomDiscoveryTopic != "jeedom_apartment/discovery/eqLogic/#" || scoped.JeedomSetTopicPrefix != "jeedom_apartment/cmd/set" {
		t.Fatalf("MQTT Manager defaults not isolated: %+v", scoped)
	}
}

func TestSourceNamespacesExplicitOutgoingBasesButPreservesInputOverrides(t *testing.T) {
	clearSourceEnv(t)
	t.Setenv("AJAXBRIDGE_SOURCE_ID", "apartment")
	t.Setenv("AJAXBRIDGE_MQTT_CLIENT_ID", "custom-client")
	t.Setenv("AJAXBRIDGE_MQTT_TOPIC_PREFIX", "custom/state/")
	t.Setenv("AJAXBRIDGE_JEEDOM_STATE_TOPIC_PREFIX", "custom/jeedom/")
	t.Setenv("AJAXBRIDGE_JEEDOM_EVENT_TOPIC", "flat/cmd/event/#")
	t.Setenv("AJAXBRIDGE_JEEDOM_DISCOVERY_TOPIC", "flat/discovery/eqLogic/#")
	t.Setenv("AJAXBRIDGE_JEEDOM_SET_TOPIC_PREFIX", "flat/cmd/set")
	cfg := FromEnv().WithSourceNamespace()
	if cfg.MQTTClientID != "custom-client-apartment" || cfg.MQTTTopicPrefix != "custom/state/apartment" || cfg.JeedomStateTopicPrefix != "custom/jeedom/apartment" {
		t.Fatalf("explicit bridge bases not scoped: %+v", cfg)
	}
	if cfg.JeedomEventTopic != "flat/cmd/event/#" || cfg.JeedomDiscoveryTopic != "flat/discovery/eqLogic/#" || cfg.JeedomSetTopicPrefix != "flat/cmd/set" {
		t.Fatalf("external MQTT Manager configuration changed: %+v", cfg)
	}
}

func TestSourceNamespacesFilesAfterCLIOverridesAndKeepsDisabledPaths(t *testing.T) {
	clearSourceEnv(t)
	cfg := FromEnv()
	cfg.SourceID = "apartment"
	cfg.DevicesPath = filepath.Join(t.TempDir(), "devices.json")
	cfg.JeedomStorePath = ""
	cfg.NotificationsPath = ""
	cfg.JeedomSampleDir = filepath.Join(t.TempDir(), "samples")
	cfg.MQTTTopicPrefix = "cli/prefix"
	scoped := cfg.WithSourceNamespace()
	if scoped.DevicesPath != filepath.Join(filepath.Dir(cfg.DevicesPath), "apartment", "devices.json") || scoped.JeedomStorePath != "" || scoped.NotificationsPath != "" {
		t.Fatalf("data paths not isolated or disabled path restored: %+v", scoped)
	}
	if scoped.JeedomSampleDir != filepath.Join(cfg.JeedomSampleDir, "apartment") || scoped.MQTTTopicPrefix != "cli/prefix/apartment" {
		t.Fatalf("CLI bases not isolated: %+v", scoped)
	}
}

func TestValidateSourceID(t *testing.T) {
	clearSourceEnv(t)
	for _, id := range []string{"", "apartment", "home-2", "home_2", strings.Repeat("a", 64)} {
		cfg := FromEnv()
		cfg.SourceID = id
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid source %q rejected: %v", id, err)
		}
	}
	for _, id := range []string{"Apartment", "../home", "home/2", "home+", "home#", " home", "_home", strings.Repeat("a", 65)} {
		cfg := FromEnv()
		cfg.SourceID = id
		if err := cfg.Validate(); err == nil {
			t.Errorf("invalid source %q accepted", id)
		}
	}
}

func TestFromEnvPrefersAjaxBridgeVariables(t *testing.T) {
	t.Setenv("AJAXBRIDGE_MQTT_TOPIC_PREFIX", "ajaxbridge-new")
	t.Setenv("AJAX2PROM_MQTT_TOPIC_PREFIX", "ajax2prom-old")

	if got := FromEnv().MQTTTopicPrefix; got != "ajaxbridge-new" {
		t.Fatalf("MQTTTopicPrefix = %q, want %q", got, "ajaxbridge-new")
	}
}

func TestFromEnvSupportsLegacyAjax2PromVariables(t *testing.T) {
	t.Setenv("AJAX2PROM_MQTT_CLIENT_ID", "legacy-client")

	if got := FromEnv().MQTTClientID; got != "legacy-client" {
		t.Fatalf("MQTTClientID = %q, want %q", got, "legacy-client")
	}
}

func TestFromEnvUsesStandaloneDevicesPathDefault(t *testing.T) {
	if got := FromEnv().DevicesPath; got != "data/devices.json" {
		t.Fatalf("DevicesPath = %q, want %q", got, "data/devices.json")
	}
}

func TestFromEnvUsesJeedomDefaults(t *testing.T) {
	cfg := FromEnv()
	if cfg.JeedomEnabled {
		t.Fatal("JeedomEnabled default = true, want false")
	}
	if cfg.JeedomEventTopic != "jeedom/cmd/event/#" {
		t.Fatalf("JeedomEventTopic = %q, want default", cfg.JeedomEventTopic)
	}
	if cfg.JeedomDiscoveryTopic != "jeedom/discovery/eqLogic/#" {
		t.Fatalf("JeedomDiscoveryTopic = %q, want default", cfg.JeedomDiscoveryTopic)
	}
	if cfg.JeedomStateTopicPrefix != "ajaxbridge/jeedom" {
		t.Fatalf("JeedomStateTopicPrefix = %q, want default", cfg.JeedomStateTopicPrefix)
	}
	if cfg.JeedomEmptyValuePolicy != "keep_last" {
		t.Fatalf("JeedomEmptyValuePolicy = %q, want keep_last", cfg.JeedomEmptyValuePolicy)
	}
	if cfg.JeedomStorePath != "data/jeedom.json" {
		t.Fatalf("JeedomStorePath = %q, want default", cfg.JeedomStorePath)
	}
	if cfg.JeedomSampleDir != "" {
		t.Fatalf("JeedomSampleDir = %q, want empty production default", cfg.JeedomSampleDir)
	}
	if cfg.JeedomDiscoverUnlinked {
		t.Fatal("JeedomDiscoverUnlinked default = true, want false")
	}
	if len(cfg.JeedomAccountNames) != 0 {
		t.Fatalf("JeedomAccountNames = %#v, want empty", cfg.JeedomAccountNames)
	}
	if cfg.JeedomControlsEnabled {
		t.Fatal("JeedomControlsEnabled default = true, want false")
	}
	if cfg.JeedomSetTopicPrefix != "jeedom/cmd/set" {
		t.Fatalf("JeedomSetTopicPrefix = %q, want default", cfg.JeedomSetTopicPrefix)
	}
	if cfg.JeedomControlPayload != "1" {
		t.Fatalf("JeedomControlPayload = %q, want default 1", cfg.JeedomControlPayload)
	}
}

func TestValidateRequiresMQTTBrokerWhenJeedomEnabled(t *testing.T) {
	cfg := FromEnv()
	cfg.JeedomEnabled = true
	cfg.MQTTBroker = ""

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestForwardAddressesSupportsSlicesAndCSV(t *testing.T) {
	cfg := Config{SIAForwardAddrs: []string{"127.0.0.1:1,127.0.0.1:2", " 127.0.0.1:3 "}}
	got := cfg.ForwardAddresses()
	want := []string{"127.0.0.1:1", "127.0.0.1:2", "127.0.0.1:3"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
