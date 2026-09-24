package jeedom

import (
	"errors"
	"testing"
	"time"
)

func TestParseMessageHumanName(t *testing.T) {
	body := []byte(`{"value":"123.4","humanName":"[None][РЎРµСЂРІРµСЂРЅР°][Puissance]","unite":"","name":"Puissance","type":"info","subtype":"numeric"}`)

	evt, err := ParseMessage("jeedom/cmd/event/56", body, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if evt.ObjectName != "None" {
		t.Fatalf("ObjectName = %q, want None", evt.ObjectName)
	}
	if evt.DeviceName != "Серверна" {
		t.Fatalf("DeviceName = %q, want repaired Cyrillic name", evt.DeviceName)
	}
	if evt.CommandName != "Puissance" {
		t.Fatalf("CommandName = %q, want Puissance", evt.CommandName)
	}
	if got := Slug(evt.DeviceName); got != "serverna" {
		t.Fatalf("Slug(DeviceName) = %q, want serverna", got)
	}
}

func TestParseMessageContractMetadata(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		logicalID   string
		genericType string
	}{
		{
			name:        "Jeedom field names",
			body:        `{"value":500,"humanName":"[None][Air][CO2]","logicalId":"actualCO2","generic_type":"CO2","type":"info","subtype":"numeric"}`,
			logicalID:   "actualCO2",
			genericType: "CO2",
		},
		{
			name:        "alternate API field names",
			body:        `{"value":50,"humanName":"[None][Sensor][Battery]","logical_id":"battery::chargeLevelPercentage","genericType":"BATTERY","type":"info","subtype":"numeric"}`,
			logicalID:   "battery::chargeLevelPercentage",
			genericType: "BATTERY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt, err := ParseMessage("jeedom/cmd/event/56", []byte(tt.body), time.Unix(100, 0))
			if err != nil {
				t.Fatal(err)
			}
			if evt.LogicalID != tt.logicalID || evt.GenericType != tt.genericType {
				t.Fatalf("contract metadata = (%q, %q), want (%q, %q)", evt.LogicalID, evt.GenericType, tt.logicalID, tt.genericType)
			}
		})
	}
}

func TestParseHumanNameMalformed(t *testing.T) {
	_, _, _, err := ParseHumanName("[None][OnlyDevice]")
	if !errors.Is(err, ErrMalformedHumanName) {
		t.Fatalf("error = %v, want ErrMalformedHumanName", err)
	}
}

func TestCommandIDFromTopic(t *testing.T) {
	got, err := CommandIDFromTopic("jeedom/cmd/event/56")
	if err != nil {
		t.Fatal(err)
	}
	if got != "56" {
		t.Fatalf("CommandIDFromTopic = %q, want 56", got)
	}
}

func TestIsCommandEventTopic(t *testing.T) {
	tests := []struct {
		topic string
		want  bool
	}{
		{topic: "jeedom/cmd/event/56", want: true},
		{topic: "prefix/jeedom/cmd/event/56", want: true},
		{topic: "jeedom/state", want: false},
		{topic: "jeedom/cmd/action/56", want: false},
		{topic: "jeedom/cmd/event/#", want: false},
	}
	for _, tt := range tests {
		if got := IsCommandEventTopic(tt.topic); got != tt.want {
			t.Fatalf("IsCommandEventTopic(%q) = %t, want %t", tt.topic, got, tt.want)
		}
	}
}

func TestMappingPowerAndCurrent(t *testing.T) {
	power := MappingFor(Event{CommandName: "Puissance", Type: "info", Subtype: "numeric"})
	if power.Metric != "power_w" || power.Unit != "W" || power.DeviceClass != "power" || power.StateClass != "measurement" {
		t.Fatalf("power mapping = %#v", power)
	}

	current := MappingFor(Event{CommandName: "Courant", Type: "info", Subtype: "numeric"})
	if current.Metric != "current_a" || current.Unit != "A" || current.DeviceClass != "current" || current.StateClass != "measurement" {
		t.Fatalf("current mapping = %#v", current)
	}
}

func TestMappingProductionJeedomCommands(t *testing.T) {
	tests := []struct {
		name      string
		subtype   string
		metric    string
		component string
		numeric   bool
		binary    bool
	}{
		{name: "Consommation", subtype: "numeric", metric: "energy_kwh", component: ComponentSensor, numeric: true},
		{name: "Etat", subtype: "binary", metric: "state", component: ComponentBinarySensor, binary: true},
		{name: "En ligne", subtype: "binary", metric: "online", component: ComponentBinarySensor, binary: true},
		{name: "Signal", subtype: "string", metric: "signal_level", component: ComponentSensor},
		{name: "Alimentation secteur", subtype: "binary", metric: "external_power", component: ComponentBinarySensor, binary: true},
	}
	for _, tt := range tests {
		mapping := MappingFor(Event{CommandName: tt.name, Subtype: tt.subtype})
		if mapping.Metric != tt.metric || mapping.Component != tt.component || mapping.Numeric != tt.numeric || mapping.Binary != tt.binary {
			t.Fatalf("MappingFor(%q/%q) = %#v", tt.name, tt.subtype, mapping)
		}
	}
}

func TestEnglishCommandNameTranslatesFrenchMetadata(t *testing.T) {
	mapping := MappingFor(Event{CommandName: "Alimentation secteur", Subtype: "binary"})
	if got := EnglishCommandName("Alimentation secteur", mapping, "12"); got != "External power" {
		t.Fatalf("EnglishCommandName = %q, want External power", got)
	}
	if got := EnglishActionName("night_mode", "Mode nuit"); got != "Night mode" {
		t.Fatalf("EnglishActionName = %q, want Night mode", got)
	}
}

func TestMappingExpandedCanonicalCommands(t *testing.T) {
	tests := []struct {
		command        string
		subtype        string
		metric         string
		component      string
		name           string
		unit           string
		deviceClass    string
		stateClass     string
		entityCategory string
		numeric        bool
		binary         bool
		timestamp      bool
	}{
		{command: "Nombre de défauts", subtype: "numeric", metric: "issue_count", component: ComponentSensor, name: "Issue count", stateClass: "measurement", entityCategory: "diagnostic", numeric: true},
		{command: "Number of issues", subtype: "numeric", metric: "issue_count", component: ComponentSensor, name: "Issue count", stateClass: "measurement", entityCategory: "diagnostic", numeric: true},
		{command: "CO2", subtype: "numeric", metric: "co2_ppm", component: ComponentSensor, name: "Carbon dioxide", unit: "ppm", deviceClass: "carbon_dioxide", stateClass: "measurement", numeric: true},
		{command: "Version du firmware", subtype: "string", metric: "firmware_version", component: ComponentSensor, name: "Firmware version", entityCategory: "diagnostic"},
		{command: "Mode", subtype: "string", metric: "operating_mode", component: ComponentSensor, name: "Operating mode", entityCategory: "diagnostic"},
		{command: "Operating state", subtype: "string", metric: "operating_state", component: ComponentSensor, name: "Operating state", entityCategory: "diagnostic"},
		{command: "Etat du contrôle de batterie", subtype: "string", metric: "battery_check_status", component: ComponentSensor, name: "Battery check status", entityCategory: "diagnostic"},
		{command: "Dernière mise à jour", subtype: "string", metric: "device_last_update", component: ComponentSensor, name: "Last update", deviceClass: "timestamp", entityCategory: "diagnostic", timestamp: true},
		{command: "Etat de la vanne", subtype: "string", metric: "valve_position", component: ComponentSensor, name: "Valve position"},
		{command: "Alarme fumée", subtype: "binary", metric: "smoke_alarm", component: ComponentBinarySensor, name: "Smoke alarm", deviceClass: "smoke", binary: true},
		{command: "Alarme fumée critique", subtype: "binary", metric: "critical_smoke_alarm", component: ComponentBinarySensor, name: "Critical smoke alarm", deviceClass: "smoke", binary: true},
		{command: "Alarme température", subtype: "binary", metric: "heat_alarm", component: ComponentBinarySensor, name: "Heat alarm", deviceClass: "heat", binary: true},
		{command: "Alarme hausse rapide de température", subtype: "binary", metric: "rapid_temperature_rise_alarm", component: ComponentBinarySensor, name: "Rapid temperature rise alarm", deviceClass: "heat", binary: true},
		{command: "Alarme CO", subtype: "binary", metric: "carbon_monoxide_alarm", component: ComponentBinarySensor, name: "Carbon monoxide alarm", deviceClass: "carbon_monoxide", binary: true},
		{command: "Alarme CO critique", subtype: "binary", metric: "critical_carbon_monoxide_alarm", component: ComponentBinarySensor, name: "Critical carbon monoxide alarm", deviceClass: "carbon_monoxide", binary: true},
		{command: "Critical carbon monoxide alarm", subtype: "binary", metric: "critical_carbon_monoxide_alarm", component: ComponentBinarySensor, name: "Critical carbon monoxide alarm", deviceClass: "carbon_monoxide", binary: true},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			mapping := MappingFor(Event{CommandName: tt.command, Subtype: tt.subtype})
			if mapping.Metric != tt.metric || mapping.Component != tt.component || mapping.EntityName != tt.name || mapping.Unit != tt.unit ||
				mapping.DeviceClass != tt.deviceClass || mapping.StateClass != tt.stateClass || mapping.EntityCategory != tt.entityCategory ||
				mapping.Numeric != tt.numeric || mapping.Binary != tt.binary || mapping.Timestamp != tt.timestamp {
				t.Fatalf("MappingFor(%q) = %#v", tt.command, mapping)
			}
		})
	}
}

func TestMappingContractMetadataPrecedesDisplayAliases(t *testing.T) {
	tests := []struct {
		name        string
		event       Event
		metric      string
		unit        string
		deviceClass string
	}{
		{name: "logical temperature", event: Event{CommandName: "Humidity", LogicalID: "temperature", GenericType: "HUMIDITY"}, metric: "temperature_c", unit: "°C", deviceClass: "temperature"},
		{name: "logical actual humidity", event: Event{CommandName: "Power", LogicalID: "actualHumidity"}, metric: "humidity_percent", unit: "%", deviceClass: "humidity"},
		{name: "logical actual CO2", event: Event{CommandName: "Power", LogicalID: "actualCO2"}, metric: "co2_ppm", unit: "ppm", deviceClass: "carbon_dioxide"},
		{name: "logical battery", event: Event{CommandName: "Power", LogicalID: "battery::chargeLevelPercentage"}, metric: "battery_percent", unit: "%", deviceClass: "battery"},
		{name: "generic temperature", event: Event{CommandName: "Power", GenericType: "TEMPERATURE"}, metric: "temperature_c", unit: "°C", deviceClass: "temperature"},
		{name: "generic humidity", event: Event{CommandName: "Power", GenericType: "HUMIDITY"}, metric: "humidity_percent", unit: "%", deviceClass: "humidity"},
		{name: "generic CO2", event: Event{CommandName: "Power", GenericType: "CO2"}, metric: "co2_ppm", unit: "ppm", deviceClass: "carbon_dioxide"},
		{name: "generic battery", event: Event{CommandName: "Power", GenericType: "BATTERY"}, metric: "battery_percent", unit: "%", deviceClass: "battery"},
		{name: "unknown contract falls through", event: Event{CommandName: "Power", LogicalID: "temperatureAlarm", GenericType: "CO2_STATUS"}, metric: "power_w", unit: "W", deviceClass: "power"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapping := MappingFor(tt.event)
			if mapping.Metric != tt.metric || mapping.Unit != tt.unit || mapping.DeviceClass != tt.deviceClass || !mapping.Numeric || mapping.StateClass != "measurement" {
				t.Fatalf("MappingFor(%#v) = %#v", tt.event, mapping)
			}
		})
	}
}

func TestMappingExpandedDeviceFields(t *testing.T) {
	tests := []struct {
		command        string
		metric         string
		component      string
		name           string
		deviceClass    string
		entityCategory string
		binary         bool
	}{
		{command: "Etat de la connexion radio", metric: "radio_connection", component: ComponentBinarySensor, name: "Radio connection", deviceClass: "connectivity", entityCategory: "diagnostic", binary: true},
		{command: "Photo channel connection status", metric: "photo_channel_connection", component: ComponentBinarySensor, name: "Photo channel connection", deviceClass: "connectivity", entityCategory: "diagnostic", binary: true},
		{command: "Niveau du signal du canal photo", metric: "photo_channel_signal", component: ComponentSensor, name: "Photo channel signal", entityCategory: "diagnostic"},
		{command: "Ethernet", metric: "ethernet_enabled", component: ComponentBinarySensor, name: "Ethernet enabled", deviceClass: "connectivity", entityCategory: "diagnostic", binary: true},
		{command: "Connexion Ethernet", metric: "ethernet_connected", component: ComponentBinarySensor, name: "Ethernet connected", deviceClass: "connectivity", entityCategory: "diagnostic", binary: true},
		{command: "Statut des antennes Jeweller", metric: "jeweller_antenna_status", component: ComponentSensor, name: "Jeweller antenna status", entityCategory: "diagnostic"},
		{command: "Etat des antennes Wings", metric: "wings_antenna_status", component: ComponentSensor, name: "Wings antenna status", entityCategory: "diagnostic"},
		{command: "Batterie en charge", metric: "battery_charging", component: ComponentBinarySensor, name: "Battery charging", deviceClass: "battery_charging", entityCategory: "diagnostic", binary: true},
		{command: "Défaut de la batterie", metric: "battery_fault", component: ComponentBinarySensor, name: "Battery fault", deviceClass: "problem", entityCategory: "diagnostic", binary: true},
		{command: "Detector power supply fault", metric: "detector_power_fault", component: ComponentBinarySensor, name: "Detector power fault", deviceClass: "problem", entityCategory: "diagnostic", binary: true},
		{command: "Défaut d'alimentation du détecteur incendie", metric: "fire_detector_power_fault", component: ComponentBinarySensor, name: "Fire detector power fault", deviceClass: "problem", entityCategory: "diagnostic", binary: true},
		{command: "Sous-tension du détecteur", metric: "detector_power_undervoltage", component: ComponentBinarySensor, name: "Detector power undervoltage", deviceClass: "problem", entityCategory: "diagnostic", binary: true},
		{command: "Fire detector supply undervoltage", metric: "fire_detector_power_undervoltage", component: ComponentBinarySensor, name: "Fire detector power undervoltage", deviceClass: "problem", entityCategory: "diagnostic", binary: true},
		{command: "Résultat du test d'alimentation Fibra", metric: "fibra_power_test", component: ComponentSensor, name: "Fibra power test", entityCategory: "diagnostic"},
		{command: "Défaut du chargeur", metric: "charger_fault", component: ComponentBinarySensor, name: "Charger fault", deviceClass: "problem", entityCategory: "diagnostic", binary: true},
		{command: "Liste des défauts du chargeur", metric: "charger_faults", component: ComponentSensor, name: "Charger faults", entityCategory: "diagnostic"},
		{command: "Liste des défauts de la batterie et du chargeur", metric: "battery_charger_faults", component: ComponentSensor, name: "Battery and charger faults", entityCategory: "diagnostic"},
		{command: "Etat du canal de données", metric: "data_channel_status", component: ComponentSensor, name: "Data channel status", entityCategory: "diagnostic"},
		{command: "Data channel signal level", metric: "data_channel_signal", component: ComponentSensor, name: "Data channel signal", entityCategory: "diagnostic"},
		{command: "Etat de l'alimentation du détecteur", metric: "detector_supply_state", component: ComponentSensor, name: "Detector supply state", entityCategory: "diagnostic"},
		{command: "Fire detector power supply status", metric: "fire_detector_supply_state", component: ComponentSensor, name: "Fire detector supply state", entityCategory: "diagnostic"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			mapping := MappingFor(Event{CommandName: tt.command, Subtype: "string"})
			if mapping.Metric != tt.metric || mapping.Component != tt.component || mapping.EntityName != tt.name ||
				mapping.DeviceClass != tt.deviceClass || mapping.EntityCategory != tt.entityCategory || mapping.Binary != tt.binary {
				t.Fatalf("MappingFor(%q) = %#v", tt.command, mapping)
			}
		})
	}
}

func TestMappingNumericChannelSignalUsesSignalStrengthMetadata(t *testing.T) {
	for _, command := range []string{"Niveau du signal du canal photo", "Data channel signal level"} {
		mapping := MappingFor(Event{CommandName: command, Subtype: "numeric", Unit: "dBm"})
		if !mapping.Numeric || mapping.Unit != "dBm" || mapping.DeviceClass != "signal_strength" || mapping.StateClass != "measurement" || mapping.EntityCategory != "diagnostic" {
			t.Fatalf("MappingFor(%q numeric) = %#v", command, mapping)
		}
	}
}

func TestBoolRawValueExpandedEnums(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
		ok   bool
	}{
		{name: "detected", raw: `"SMOKE_DETECTED"`, want: true, ok: true},
		{name: "not detected", raw: `"SMOKE_NOT_DETECTED"`, want: false, ok: true},
		{name: "connected", raw: `"CONNECTED"`, want: true, ok: true},
		{name: "disconnected", raw: `"DISCONNECTED"`, want: false, ok: true},
		{name: "enabled", raw: `"ENABLED"`, want: true, ok: true},
		{name: "disabled", raw: `"DISABLED"`, want: false, ok: true},
		{name: "charging", raw: `"CHARGING"`, want: true, ok: true},
		{name: "not charging", raw: `"NOT_CHARGING"`, want: false, ok: true},
		{name: "ok needs mapping context", raw: `"OK"`, want: false, ok: false},
		{name: "fault needs mapping context", raw: `"FAULT"`, want: false, ok: false},
		{name: "failed needs mapping context", raw: `"FAILED"`, want: false, ok: false},
		{name: "French connected", raw: `"CONNECTÉ"`, want: true, ok: true},
		{name: "unknown remains unknown", raw: `"PARTIALLY_AVAILABLE"`, want: false, ok: false},
		{name: "detected-looking suffix is not enough", raw: `"UNDETECTEDNESS"`, want: false, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BoolRawValue([]byte(tt.raw))
			if got != tt.want || ok != tt.ok {
				t.Fatalf("BoolRawValue(%s) = (%t, %t), want (%t, %t)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBoolRawValueForMappingPolarity(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		mapping Mapping
		want    bool
		ok      bool
	}{
		{name: "problem OK", raw: `"OK"`, mapping: binaryMapping("fault", "Fault", "problem"), want: false, ok: true},
		{name: "problem fault", raw: `"FAULT"`, mapping: binaryMapping("fault", "Fault", "problem"), want: true, ok: true},
		{name: "problem failed", raw: `"FAILED"`, mapping: binaryMapping("fault", "Fault", "problem"), want: true, ok: true},
		{name: "connectivity OK", raw: `"OK"`, mapping: binaryMapping("connection", "Connection", "connectivity"), want: true, ok: true},
		{name: "connectivity fault", raw: `"FAULT"`, mapping: binaryMapping("connection", "Connection", "connectivity"), want: false, ok: true},
		{name: "alarm OK is not assumed", raw: `"OK"`, mapping: binaryMapping("smoke", "Smoke", "smoke"), want: false, ok: false},
		{name: "non-binary fault is not assumed", raw: `"FAULT"`, mapping: sensorMapping("status", "Status", "", "", "", false), want: false, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BoolRawValueForMapping([]byte(tt.raw), tt.mapping)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("BoolRawValueForMapping(%s, %#v) = (%t, %t), want (%t, %t)", tt.raw, tt.mapping, got, ok, tt.want, tt.ok)
			}
		})
	}
}
