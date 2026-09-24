package jeedom

import (
	"strings"
	"unicode"
)

const (
	ComponentSensor       = "sensor"
	ComponentBinarySensor = "binary_sensor"
	ComponentSwitch       = "switch"
	ComponentButton       = "button"
)

type Mapping struct {
	Metric         string
	Component      string
	EntityName     string
	Unit           string
	DeviceClass    string
	StateClass     string
	EntityCategory string
	Numeric        bool
	Binary         bool
	Timestamp      bool
	fallback       bool
}

func MappingFor(evt Event) Mapping {
	command := firstNonEmpty(evt.CommandName, evt.Name, evt.CommandID)
	unit := strings.TrimSpace(RepairText(evt.Unit))
	if mapping, ok := contractMappingFor(evt, unit); ok {
		return mapping
	}
	switch canonicalCommandKey(commandKey(command)) {
	case "puissance", "power":
		return sensorMapping("power_w", "Power", firstNonEmpty(unit, "W"), "power", "measurement", true)
	case "consommation", "energy", "consumption":
		return sensorMapping("energy_kwh", "Energy", firstNonEmpty(unit, "kWh"), "energy", "total_increasing", true)
	case "courant", "current":
		return sensorMapping("current_a", "Current", firstNonEmpty(unit, "A"), "current", "measurement", true)
	case "tension", "voltage":
		return sensorMapping("voltage_v", "Voltage", firstNonEmpty(unit, "V"), "voltage", "measurement", true)
	case "temperature", "temp":
		return sensorMapping("temperature_c", "Temperature", firstNonEmpty(unit, "\u00b0C"), "temperature", "measurement", true)
	case "batterie", "battery":
		return sensorMapping("battery_percent", "Battery", firstNonEmpty(unit, "%"), "battery", "measurement", true)
	case "etatdelabatterie", "batterystate":
		return diagnosticSensorMapping("battery_state", "Battery state", unit, "", "", false)
	case "signal", "rssi":
		if strings.EqualFold(strings.TrimSpace(evt.Subtype), "numeric") || strings.EqualFold(unit, "dBm") {
			return sensorMapping("signal_dbm", "Signal", firstNonEmpty(unit, "dBm"), "signal_strength", "measurement", true)
		}
		return diagnosticSensorMapping("signal_level", "Signal", unit, "", "", false)
	case "intensitedusignalcellulaire", "cellularsignalstrength":
		return sensorMapping("signal_dbm", "Signal", firstNonEmpty(unit, "dBm"), "signal_strength", "measurement", true)
	case "humidite", "humidity":
		return sensorMapping("humidity_percent", "Humidity", firstNonEmpty(unit, "%"), "humidity", "measurement", true)
	case "co2":
		return sensorMapping("co2_ppm", "Carbon dioxide", firstNonEmpty(unit, "ppm"), "carbon_dioxide", "measurement", true)
	case "etat", "state", "status":
		if strings.EqualFold(strings.TrimSpace(evt.Subtype), "binary") {
			return binaryMapping("state", "State", "")
		}
		return sensorMapping("state", "State", unit, "", "", false)
	case "sourceevenement", "eventsource":
		return diagnosticSensorMapping("event_source", "Event source", unit, "", "", false)
	case "evenement", "event":
		return diagnosticSensorMapping("event", "Event", unit, "", "", false)
	case "codeevenement", "eventcode":
		return diagnosticSensorMapping("event_code", "Event code", unit, "", "", false)
	case "enligne", "online":
		return diagnosticBinaryMapping("online", "Online", "connectivity")
	case "trafique", "tampered", "tamper", "sabotage":
		return binaryMapping("tamper", "Tamper", "tamper")
	case "alimentationsecteur", "externallypowered", "externalpower", "mainspower":
		return binaryMapping("external_power", "External power", "power")
	case "gsm", "cms", "ethernet":
		return diagnosticBinaryMapping(MetricName(command, "cmd_"+evt.CommandID), firstNonEmpty(translateCommandLabel(command), titleName(command)), "connectivity")
	case "typereseaugsm", "gsmnetworktype":
		return diagnosticSensorMapping("gsm_network_type", "GSM network type", unit, "", "", false)
	case "donneescellulairesactives", "cellulardataactive":
		return diagnosticBinaryMapping("cellular_data_active", "Cellular data active", "connectivity")
	case "ouverture", "opening":
		return binaryMapping("opening", "Opening", "opening")
	case "porte", "door":
		return binaryMapping("door", "Door", "door")
	case "fuite", "leak":
		return binaryMapping("leak", "Leak", "moisture")
	case "issuecount":
		return diagnosticSensorMapping("issue_count", "Issue count", unit, "", "measurement", true)
	case "firmwareversion":
		return diagnosticSensorMapping("firmware_version", "Firmware version", unit, "", "", false)
	case "operatingmode":
		return diagnosticSensorMapping("operating_mode", "Operating mode", unit, "", "", false)
	case "operatingstate":
		return diagnosticSensorMapping("operating_state", "Operating state", unit, "", "", false)
	case "batterycheckstatus":
		return diagnosticSensorMapping("battery_check_status", "Battery check status", unit, "", "", false)
	case "lastupdate":
		mapping := diagnosticSensorMapping("device_last_update", "Last update", "", "timestamp", "", false)
		mapping.Timestamp = true
		return mapping
	case "valveposition":
		return sensorMapping("valve_position", "Valve position", unit, "", "", false)
	case "smokealarm":
		return binaryMapping("smoke_alarm", "Smoke alarm", "smoke")
	case "criticalsmokealarm":
		return binaryMapping("critical_smoke_alarm", "Critical smoke alarm", "smoke")
	case "heatalarm":
		return binaryMapping("heat_alarm", "Heat alarm", "heat")
	case "rapidtemperaturerisealarm":
		return binaryMapping("rapid_temperature_rise_alarm", "Rapid temperature rise alarm", "heat")
	case "carbonmonoxidealarm":
		return binaryMapping("carbon_monoxide_alarm", "Carbon monoxide alarm", "carbon_monoxide")
	case "criticalcarbonmonoxidealarm":
		return binaryMapping("critical_carbon_monoxide_alarm", "Critical carbon monoxide alarm", "carbon_monoxide")
	case "radioconnection":
		return diagnosticBinaryMapping("radio_connection", "Radio connection", "connectivity")
	case "photochannelconnection":
		return diagnosticBinaryMapping("photo_channel_connection", "Photo channel connection", "connectivity")
	case "photochannelsignal":
		return channelSignalMapping("photo_channel_signal", "Photo channel signal", evt, unit)
	case "ethernetenabled":
		return diagnosticBinaryMapping("ethernet_enabled", "Ethernet enabled", "connectivity")
	case "ethernetconnected":
		return diagnosticBinaryMapping("ethernet_connected", "Ethernet connected", "connectivity")
	case "jewellerantennastatus":
		return diagnosticSensorMapping("jeweller_antenna_status", "Jeweller antenna status", unit, "", "", false)
	case "wingsantennastatus":
		return diagnosticSensorMapping("wings_antenna_status", "Wings antenna status", unit, "", "", false)
	case "batterycharging":
		return diagnosticBinaryMapping("battery_charging", "Battery charging", "battery_charging")
	case "batteryfault":
		return diagnosticBinaryMapping("battery_fault", "Battery fault", "problem")
	case "detectorpowerfault":
		return diagnosticBinaryMapping("detector_power_fault", "Detector power fault", "problem")
	case "firedetectorpowerfault":
		return diagnosticBinaryMapping("fire_detector_power_fault", "Fire detector power fault", "problem")
	case "detectorpowerundervoltage":
		return diagnosticBinaryMapping("detector_power_undervoltage", "Detector power undervoltage", "problem")
	case "firedetectorpowerundervoltage":
		return diagnosticBinaryMapping("fire_detector_power_undervoltage", "Fire detector power undervoltage", "problem")
	case "fibrapowertest":
		return diagnosticSensorMapping("fibra_power_test", "Fibra power test", unit, "", "", false)
	case "chargerfault":
		return diagnosticBinaryMapping("charger_fault", "Charger fault", "problem")
	case "chargerfaults":
		return diagnosticSensorMapping("charger_faults", "Charger faults", unit, "", "", false)
	case "batterychargerfaults":
		return diagnosticSensorMapping("battery_charger_faults", "Battery and charger faults", unit, "", "", false)
	case "datachannelstatus":
		return diagnosticSensorMapping("data_channel_status", "Data channel status", unit, "", "", false)
	case "datachannelsignal":
		return channelSignalMapping("data_channel_signal", "Data channel signal", evt, unit)
	case "detectorsupplystate":
		return diagnosticSensorMapping("detector_supply_state", "Detector supply state", unit, "", "", false)
	case "firedetectorsupplystate":
		return diagnosticSensorMapping("fire_detector_supply_state", "Fire detector supply state", unit, "", "", false)
	default:
		return fallbackMapping(evt, command, unit)
	}
}

// contractMappingFor intentionally uses exact known Jeedom identifiers. These
// stable fields take precedence over localized or user-renamed display names,
// while unknown identifiers still fall through to the normal alias mapping.
func contractMappingFor(evt Event, unit string) (Mapping, bool) {
	switch commandKey(evt.LogicalID) {
	case "temperature", "actualtemperature":
		return contractSensorMapping("temperature", unit), true
	case "humidity", "actualhumidity":
		return contractSensorMapping("humidity", unit), true
	case "co2", "actualco2":
		return contractSensorMapping("co2", unit), true
	case "batterychargelevelpercentage":
		return contractSensorMapping("battery", unit), true
	}

	switch strings.ToUpper(strings.TrimSpace(RepairText(evt.GenericType))) {
	case "TEMPERATURE":
		return contractSensorMapping("temperature", unit), true
	case "HUMIDITY":
		return contractSensorMapping("humidity", unit), true
	case "CO2":
		return contractSensorMapping("co2", unit), true
	case "BATTERY":
		return contractSensorMapping("battery", unit), true
	default:
		return Mapping{}, false
	}
}

func contractSensorMapping(kind, unit string) Mapping {
	switch kind {
	case "temperature":
		return sensorMapping("temperature_c", "Temperature", firstNonEmpty(unit, "\u00b0C"), "temperature", "measurement", true)
	case "humidity":
		return sensorMapping("humidity_percent", "Humidity", firstNonEmpty(unit, "%"), "humidity", "measurement", true)
	case "co2":
		return sensorMapping("co2_ppm", "Carbon dioxide", firstNonEmpty(unit, "ppm"), "carbon_dioxide", "measurement", true)
	case "battery":
		return sensorMapping("battery_percent", "Battery", firstNonEmpty(unit, "%"), "battery", "measurement", true)
	default:
		return Mapping{}
	}
}

// canonicalCommandKey keeps command aliases in one place for both entity
// mapping and English metadata. The exact aliases cover current Jeedom labels;
// the narrowly-scoped patterns tolerate small wording changes between device
// families without turning unrelated commands into binary sensors.
func canonicalCommandKey(key string) string {
	switch key {
	case "nombredefaut", "nombredefauts", "nombrededefaut", "nombrededefauts", "nombredeproblemes", "faultcount", "issuecount", "numberoffaults", "numberofissues":
		return "issuecount"
	case "carbondioxide", "co2", "co2concentration", "co2level", "dioxydedecarbone", "tauxdeco2":
		return "co2"
	case "firmware", "firmwareversion", "versionfirmware", "versiondufirmware":
		return "firmwareversion"
	case "mode", "modefonctionnement", "modedefonctionnement", "operatingmode":
		return "operatingmode"
	case "etatdefonctionnement", "etatdufonctionnement", "operatingstate", "operatingstatus":
		return "operatingstate"
	case "batterycheckstate", "batterycheckstatus", "controlebatterie", "etatcontrolebatterie", "etatducontroledebatterie":
		return "batterycheckstatus"
	case "datededernieremiseajour", "dernieremiseajour", "lastupdate", "lastupdated", "lastupdatetime":
		return "lastupdate"
	case "etatdelavanne", "etatvanne", "valveposition", "valvestate", "valvestatus":
		return "valveposition"
	case "alarmefumee", "smokealarm":
		return "smokealarm"
	case "alarmefumeecritique", "criticalsmokealarm", "smokecriticalalarm":
		return "criticalsmokealarm"
	case "alarmetemperature", "heatalarm", "temperaturealarm":
		return "heatalarm"
	case "alarmehausserapidedetemperature", "rapidtemperatureincreasealarm", "rapidtemperaturerisealarm", "rateofrisealarm":
		return "rapidtemperaturerisealarm"
	case "alarmeco", "carbonmonoxidealarm", "coalarm":
		return "carbonmonoxidealarm"
	case "alarmecocritique", "criticalcarbonmonoxidealarm", "criticalcoalarm":
		return "criticalcarbonmonoxidealarm"
	case "connexionradio", "etatconnexionradio", "etatdelaconnexionradio", "radioconnection", "radioconnectionstatus", "radiolink", "radiolinkstatus", "statutconnexionradio":
		return "radioconnection"
	case "connexioncanalphoto", "connexionducanalphoto", "etatconnexioncanalphoto", "etatdelaconnexionducanalphoto", "photochannelconnection", "photochannelconnectionstatus", "photochannelstatus":
		return "photochannelconnection"
	case "niveaudusignalducanalphoto", "niveausignalcanalphoto", "photochannelsignal", "photochannelsignalstrength", "signalcanalphoto", "signalducanalphoto":
		return "photochannelsignal"
	case "activationethernet", "ethernet", "ethernetactive", "ethernetactif", "ethernetenabled", "etatethernet":
		return "ethernetenabled"
	case "connexionethernet", "ethernetconnected", "ethernetconnection", "ethernetconnectionstatus", "ethernetlink", "ethernetlinkstatus", "ethernetstatus", "etatconnexionethernet", "etatdelaconnexionethernet":
		return "ethernetconnected"
	case "antennejeweller", "etatantennejeweller", "etatdelantennejeweller", "etatdesantennesjeweller", "jewellerantenna", "jewellerantennastate", "jewellerantennastatus", "statutantennejeweller", "statutdelantennejeweller":
		return "jewellerantennastatus"
	case "antennewings", "etatantennewings", "etatdelantennewings", "etatdesantenneswings", "statutantennewings", "statutdelantennewings", "wingsantenna", "wingsantennastate", "wingsantennastatus":
		return "wingsantennastatus"
	case "batterycharging", "batterieencharge", "chargementencours", "encharge", "ischarging":
		return "batterycharging"
	case "batteryfailure", "batteryfault", "batterymalfunction", "defautbatterie", "defautdelabatterie", "pannebatterie":
		return "batteryfault"
	case "defautalimentationdetecteur", "defautdalimentationdudetecteur", "detectorpowerfault", "detectorpowersupplyfault", "detectorsupplyfault":
		return "detectorpowerfault"
	case "defautalimentationdetecteurincendie", "defautdalimentationdudetecteurincendie", "defautdalimentationdesdetecteursdincendie", "firedetectorpowerfault", "firedetectorpowersupplyfault", "firedetectorsupplyfault":
		return "firedetectorpowerfault"
	case "detectorpowerundervoltage", "detectorpowersupplyundervoltage", "detectorundervoltage", "soustensionalimentationdetecteur", "soustensiondetecteur", "soustensiondudetecteur":
		return "detectorpowerundervoltage"
	case "firedetectorpowerundervoltage", "firedetectorpowersupplyundervoltage", "firedetectorundervoltage", "soustensionalimentationdetecteurincendie", "soustensiondetecteurincendie", "soustensiondudetecteurincendie":
		return "firedetectorpowerundervoltage"
	case "etatdutestdalimentationfibra", "fibrapowertest", "fibrapowersupplytest", "testalimentationfibra", "testdalimentationfibra":
		return "fibrapowertest"
	case "chargerfailure", "chargerfault", "defautchargeur", "defautduchargeur", "pannechargeur":
		return "chargerfault"
	case "chargerfaultlist", "chargerfaults", "defautschargeur", "defautsduchargeur", "listedesdefautsduchargeur":
		return "chargerfaults"
	case "batteryandchargerfaults", "batterychargerfaultlist", "batterychargerfaults", "listedesdefautsdelabatterieetduchargeur", "listedesdefautsdelabatterieduchargeur":
		return "batterychargerfaults"
	case "datachannelstate", "datachannelstatus", "etatcanaldedonnees", "etatducanaldedonnees", "statutcanaldedonnees", "statutducanaldedonnees":
		return "datachannelstatus"
	case "datachannelsignal", "niveaudusignalducanaldedonnees", "niveausignalcanaldedonnees", "signalcanaldedonnees", "signalducanaldedonnees":
		return "datachannelsignal"
	case "detectorpowerstate", "detectorpowersupplystate", "detectorsupplystate", "detectorsupplystatus", "etatalimentationdetecteur", "etatdelalimentationdudetecteur":
		return "detectorsupplystate"
	case "etatdelalimentationdudetecteurincendie", "etatalimentationdetecteurincendie", "firedetectorpowerstate", "firedetectorpowersupplystate", "firedetectorsupplystate", "firedetectorsupplystatus":
		return "firedetectorsupplystate"
	}

	switch {
	case containsAll(key, "jeweller") && containsAny(key, "antenna", "antenne"):
		return "jewellerantennastatus"
	case containsAll(key, "wings") && containsAny(key, "antenna", "antenne"):
		return "wingsantennastatus"
	case containsAny(key, "photo", "wings") && containsAny(key, "signal"):
		return "photochannelsignal"
	case containsAny(key, "photo", "wings") && containsAny(key, "connect", "connexion", "link"):
		return "photochannelconnection"
	case containsAll(key, "ethernet") && containsAny(key, "connect", "connexion", "link"):
		return "ethernetconnected"
	case containsAll(key, "ethernet") && containsAny(key, "active", "actif", "activation", "enable"):
		return "ethernetenabled"
	case containsAll(key, "fibra", "test") && containsAny(key, "alimentation", "power", "supply"):
		return "fibrapowertest"
	case isFireDetectorKey(key) && containsAny(key, "undervoltage", "soustension", "lowvoltage"):
		return "firedetectorpowerundervoltage"
	case isDetectorKey(key) && containsAny(key, "undervoltage", "soustension", "lowvoltage"):
		return "detectorpowerundervoltage"
	case isFireDetectorKey(key) && containsAny(key, "fault", "failure", "defaut", "panne") && containsAny(key, "alimentation", "power", "supply"):
		return "firedetectorpowerfault"
	case isDetectorKey(key) && containsAny(key, "fault", "failure", "defaut", "panne") && containsAny(key, "alimentation", "power", "supply"):
		return "detectorpowerfault"
	case isFireDetectorKey(key) && containsAny(key, "alimentation", "power", "supply") && containsAny(key, "etat", "state", "status", "statut"):
		return "firedetectorsupplystate"
	case isDetectorKey(key) && containsAny(key, "alimentation", "power", "supply") && containsAny(key, "etat", "state", "status", "statut"):
		return "detectorsupplystate"
	case containsAny(key, "chargeur", "charger") && containsAny(key, "faults", "defauts", "list"):
		if containsAny(key, "battery", "batterie") {
			return "batterychargerfaults"
		}
		return "chargerfaults"
	case containsAny(key, "chargeur", "charger") && containsAny(key, "fault", "failure", "defaut", "panne"):
		return "chargerfault"
	case isDataChannelKey(key) && containsAny(key, "signal"):
		return "datachannelsignal"
	case isDataChannelKey(key) && containsAny(key, "etat", "state", "status", "statut"):
		return "datachannelstatus"
	case containsAny(key, "battery", "batterie") && containsAny(key, "fault", "failure", "defaut", "panne"):
		return "batteryfault"
	case containsAny(key, "radio", "jeweller") && containsAny(key, "connect", "connexion", "link"):
		return "radioconnection"
	default:
		return key
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func isDetectorKey(key string) bool {
	return containsAny(key, "detector", "detecteur")
}

func isFireDetectorKey(key string) bool {
	return containsAny(key, "firedetector", "detecteurincendie", "detecteurdincendie")
}

func isDataChannelKey(key string) bool {
	return containsAny(key, "datachannel", "canaldedonnees")
}

func sensorMapping(metric, name, unit, deviceClass, stateClass string, numeric bool) Mapping {
	return Mapping{
		Metric:      metric,
		Component:   ComponentSensor,
		EntityName:  name,
		Unit:        unit,
		DeviceClass: deviceClass,
		StateClass:  stateClass,
		Numeric:     numeric,
	}
}

func diagnosticSensorMapping(metric, name, unit, deviceClass, stateClass string, numeric bool) Mapping {
	mapping := sensorMapping(metric, name, unit, deviceClass, stateClass, numeric)
	mapping.EntityCategory = "diagnostic"
	return mapping
}

func channelSignalMapping(metric, name string, evt Event, unit string) Mapping {
	if strings.EqualFold(strings.TrimSpace(evt.Subtype), "numeric") || strings.EqualFold(unit, "dBm") {
		mapping := sensorMapping(metric, name, firstNonEmpty(unit, "dBm"), "signal_strength", "measurement", true)
		mapping.EntityCategory = "diagnostic"
		return mapping
	}
	return diagnosticSensorMapping(metric, name, unit, "", "", false)
}

func binaryMapping(metric, name, deviceClass string) Mapping {
	return Mapping{
		Metric:      metric,
		Component:   ComponentBinarySensor,
		EntityName:  name,
		DeviceClass: deviceClass,
		Binary:      true,
	}
}

func diagnosticBinaryMapping(metric, name, deviceClass string) Mapping {
	mapping := binaryMapping(metric, name, deviceClass)
	mapping.EntityCategory = "diagnostic"
	return mapping
}

func fallbackMapping(evt Event, command, unit string) Mapping {
	metric := MetricName(command, "cmd_"+evt.CommandID)
	name := firstNonEmpty(translateCommandLabel(command), titleName(command))
	subtype := strings.ToLower(strings.TrimSpace(evt.Subtype))
	var mapping Mapping
	switch subtype {
	case "numeric":
		mapping = sensorMapping(metric+"_value", name, unit, "", "measurement", true)
	case "binary":
		mapping = binaryMapping(metric, name, "")
	default:
		mapping = sensorMapping(metric, name, unit, "", "", false)
	}
	mapping.fallback = true
	return mapping
}

func commandKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(RepairText(value)))
	var b strings.Builder
	for _, r := range value {
		r = unicode.ToLower(foldLatin(r))
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func titleName(value string) string {
	value = strings.TrimSpace(RepairText(value))
	if value == "" {
		return "Value"
	}
	parts := strings.Fields(strings.ReplaceAll(value, "_", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
