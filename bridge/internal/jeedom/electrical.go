package jeedom

import "strings"

// Unit metadata is authoritative; a command name or logical ID alone cannot
// establish transport scaling (Jeedom may apply its own value offset).
func electricalMapping(kind, unit string) Mapping {
	unit = strings.TrimSpace(unit)
	if kind == "current" {
		switch unit {
		case "A":
			return sensorMapping("current_a", "Current", unit, "current", "measurement", true)
		case "mA":
			return sensorMapping("current_ma", "Current", unit, "current", "measurement", true)
		case "µA", "μA", "uA":
			return sensorMapping("current_ua", "Current", "μA", "current", "measurement", true)
		}
		return diagnosticSensorMapping("current_raw", "Current", unit, "", "", true)
	}
	if mapping, ok := energyMappingForUnit(unit); ok {
		return mapping
	}
	switch unit {
	case "W":
		return sensorMapping("power_w", "Power", unit, "power", "measurement", true)
	case "kW":
		return sensorMapping("power_kw", "Power", unit, "power", "measurement", true)
	case "mW":
		return sensorMapping("power_mw", "Power", unit, "power", "measurement", true)
	}
	return diagnosticSensorMapping("power_raw", "Power", unit, "", "", true)
}

// A declared energy unit establishes the quantity even when the legacy Jeedom
// logical ID is powerWtH or the generic type is POWER. Values stay in that unit.
func energyMappingForUnit(unit string) (Mapping, bool) {
	unit = strings.TrimSpace(unit)
	var metric string
	switch unit {
	case "mWh":
		metric = "energy_milliwh"
	case "Wh":
		metric = "energy_wh"
	case "kWh":
		metric = "energy_kwh"
	case "MWh":
		metric = "energy_mwh"
	case "GWh":
		metric = "energy_gwh"
	case "TWh":
		metric = "energy_twh"
	case "J", "kJ", "MJ", "GJ", "cal", "kcal", "Mcal", "Gcal":
		metric = "energy_" + strings.ToLower(unit)
	default:
		return Mapping{}, false
	}
	return sensorMapping(metric, "Energy", unit, "energy", "total_increasing", true), true
}

func energyMapping(unit string) Mapping {
	unit = strings.TrimSpace(unit)
	if mapping, ok := energyMappingForUnit(unit); ok {
		return mapping
	}
	return diagnosticSensorMapping("energy_raw", "Energy", unit, "", "", true)
}

func energyCommandContract(command Command) bool {
	return command.DeviceClass == "energy" || command.Metric == "energy_raw"
}

func electricalMetricKind(metric string) string {
	switch metric {
	case "current_a", "current_ma", "current_ua", "current_raw":
		return "current"
	case "power_w", "power_kw", "power_mw", "power_raw":
		return "power"
	default:
		return ""
	}
}

func electricalRequiresFreshValue(previous Command, mapping Mapping) bool {
	if mapping.DeviceClass == "energy" {
		if _, verified := energyMappingForUnit(mapping.Unit); verified {
			return !previous.SourceUnitKnown || previous.SourceUnit != mapping.Unit ||
				previous.Unit != mapping.Unit || previous.DeviceClass != "energy"
		}
	}
	if !physicalElectricalMetric(mapping.Metric) {
		return false
	}
	verified := electricalMapping(electricalMetricKind(mapping.Metric), previous.SourceUnit)
	return !previous.SourceUnitKnown || verified.Unit != mapping.Unit || verified.DeviceClass != mapping.DeviceClass ||
		electricalMetricKind(previous.Metric) != electricalMetricKind(mapping.Metric) ||
		previous.Unit != mapping.Unit || previous.DeviceClass != mapping.DeviceClass
}

func hasRecordedControlState(device *Device, value any) bool {
	state, ok := value.(bool)
	return ok && !device.LastControlStateAt.IsZero() && state == device.LastControlState
}

func physicalElectricalMetric(metric string) bool {
	return electricalMetricKind(metric) != "" && !strings.HasSuffix(metric, "_raw")
}

func sourceUnitValue(unit *string) string {
	if unit == nil {
		return ""
	}
	return *unit
}

func sourceUnitForEvent(evt Event, previous string, known bool) (string, bool) {
	if evt.UnitProvided || evt.Unit != "" {
		unit := strings.TrimSpace(RepairText(evt.Unit))
		return unit, true
	}
	return previous, known
}
