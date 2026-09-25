package metrics

import (
	"regexp"
	"strconv"

	"github.com/RCooLeR/AjaxBridge/internal/event"
	"github.com/RCooLeR/AjaxBridge/internal/forward"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var schedulerRuntimeMetrics = regexp.MustCompile(`^/sched/(goroutines-created|goroutines/(not-in-go|runnable|running|waiting)|threads/total):`)

type Metrics struct {
	eventsTotal     *prometheus.CounterVec
	parseErrors     *prometheus.CounterVec
	forwardTotal    *prometheus.CounterVec
	forwardDuration *prometheus.HistogramVec

	accountOnline    *prometheus.GaugeVec
	accountArmed     *prometheus.GaugeVec
	accountNightMode *prometheus.GaugeVec
	accountPartial   *prometheus.GaugeVec
	accountAlarm     *prometheus.GaugeVec
	accountTamper    *prometheus.GaugeVec
	accountTrouble   *prometheus.GaugeVec
	accountLastEvent *prometheus.GaugeVec
	accountLastPing  *prometheus.GaugeVec
	zoneAlarm        *prometheus.GaugeVec
	zoneAlarmLast    *prometheus.GaugeVec
	zoneTamper       *prometheus.GaugeVec
	zoneTamperLast   *prometheus.GaugeVec
	zoneTrouble      *prometheus.GaugeVec
	zoneLastEvent    *prometheus.GaugeVec

	jeedomMessages    prometheus.Counter
	jeedomParseErrors prometheus.Counter
	jeedomEmptyValues prometheus.Counter
	jeedomSnapshot    *jeedomCollector
}

func New(reg prometheus.Registerer) *Metrics {
	reg.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{
			Matcher: schedulerRuntimeMetrics,
		}),
	))

	m := &Metrics{
		eventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ajax_sia_events_total",
			Help: "Total SIA events received from Ajax hubs.",
		}, []string{"account", "event_code", "event_class", "parse_status"}),
		parseErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ajax_sia_parse_errors_total",
			Help: "Total SIA frames rejected or degraded by parse/validation reason.",
		}, []string{"reason"}),
		forwardTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ajax_sia_forward_total",
			Help: "Total SIA frame forward attempts to an upstream receiver.",
		}, []string{"target", "status"}),
		forwardDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ajax_sia_forward_duration_seconds",
			Help:    "Duration of SIA frame forwarding attempts to an upstream receiver.",
			Unit:    "seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"target", "status"}),
		accountOnline: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_online",
			Help: "Whether the account is considered online based on last ping or event.",
		}, []string{"account"}),
		accountArmed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_armed",
			Help: "Whether the account is currently armed.",
		}, []string{"account"}),
		accountNightMode: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_night_mode",
			Help: "Whether Night mode is currently active for the account.",
		}, []string{"account"}),
		accountPartial: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_partially_armed",
			Help: "Whether the account is currently partially armed.",
		}, []string{"account"}),
		accountAlarm: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_alarm_active",
			Help: "Whether an alarm is currently active for the account.",
		}, []string{"account"}),
		accountTamper: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_tamper_active",
			Help: "Whether tamper is currently active for the account.",
		}, []string{"account"}),
		accountTrouble: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_trouble_active",
			Help: "Whether trouble is currently active for the account.",
		}, []string{"account"}),
		accountLastEvent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_last_event_timestamp_seconds",
			Help: "Unix timestamp for the last received event per account.",
			Unit: "seconds",
		}, []string{"account"}),
		accountLastPing: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_account_last_ping_timestamp_seconds",
			Help: "Unix timestamp for the last test/ping event per account.",
			Unit: "seconds",
		}, []string{"account"}),
		zoneAlarm: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_zone_alarm_active",
			Help: "Whether an alarm is currently active for a zone/device.",
		}, []string{"account", "partition", "group", "zone", "device", "device_name", "room", "device_kind", "device_events", "alarm_signal", "alarm_action"}),
		zoneAlarmLast: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_zone_alarm_last_event_timestamp_seconds",
			Help: "Unix timestamp for the last alarm event per zone/device.",
			Unit: "seconds",
		}, []string{"account", "partition", "group", "zone", "device", "device_name", "room", "device_kind", "device_events", "alarm_signal", "alarm_action"}),
		zoneTamper: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_zone_tamper_active",
			Help: "Whether tamper is currently active for a zone/device.",
		}, []string{"account", "partition", "group", "zone", "device", "device_name", "room", "device_kind", "device_events"}),
		zoneTamperLast: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_zone_tamper_last_event_timestamp_seconds",
			Help: "Unix timestamp for the last tamper or tamper restore event per zone/device.",
			Unit: "seconds",
		}, []string{"account", "partition", "group", "zone", "device", "device_name", "room", "device_kind", "device_events"}),
		zoneTrouble: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_zone_trouble_active",
			Help: "Whether trouble is currently active for a zone/device.",
		}, []string{"account", "partition", "group", "zone", "device", "device_name", "room", "device_kind", "device_events"}),
		zoneLastEvent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ajax_zone_last_event_timestamp_seconds",
			Help: "Unix timestamp for the last received event per zone/device.",
			Unit: "seconds",
		}, []string{"account", "partition", "group", "zone", "device", "device_name", "room", "device_kind", "device_events"}),
		jeedomMessages: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ajax_jeedom_mqtt_messages_total",
			Help: "Total Jeedom MQTT messages received.",
		}),
		jeedomParseErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ajax_jeedom_mqtt_parse_errors_total",
			Help: "Total Jeedom MQTT messages that could not be parsed.",
		}),
		jeedomEmptyValues: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ajax_jeedom_empty_values_total",
			Help: "Total Jeedom MQTT messages with empty or null values.",
		}),
		jeedomSnapshot: newJeedomCollector(),
	}

	reg.MustRegister(
		m.eventsTotal,
		m.parseErrors,
		m.forwardTotal,
		m.forwardDuration,
		m.accountOnline,
		m.accountArmed,
		m.accountNightMode,
		m.accountPartial,
		m.accountAlarm,
		m.accountTamper,
		m.accountTrouble,
		m.accountLastEvent,
		m.accountLastPing,
		m.zoneAlarm,
		m.zoneAlarmLast,
		m.zoneTamper,
		m.zoneTamperLast,
		m.zoneTrouble,
		m.zoneLastEvent,
		m.jeedomMessages,
		m.jeedomParseErrors,
		m.jeedomEmptyValues,
		m.jeedomSnapshot,
	)

	return m
}

func (m *Metrics) ObserveJeedomMessage() {
	m.jeedomMessages.Inc()
}

func (m *Metrics) ObserveJeedomParseError() {
	m.jeedomParseErrors.Inc()
}

func (m *Metrics) ObserveJeedomEmptyValue() {
	m.jeedomEmptyValues.Inc()
}

func (m *Metrics) ObserveEvent(evt event.Normalized) {
	account := labelValue(evt.Account, "unknown")
	code := labelValue(evt.EventCode, "unknown")
	class := labelValue(string(evt.EventClass), "unknown")
	status := labelValue(string(evt.ParseStatus), "unknown")
	m.eventsTotal.WithLabelValues(account, code, class, status).Inc()
	if evt.ParseStatus != event.ParseStatusOK {
		m.parseErrors.WithLabelValues(status).Inc()
	}
}

func (m *Metrics) ObserveForward(result forward.Result) {
	if !result.Enabled {
		return
	}
	status := labelValue(result.Status, "unknown")
	target := labelValue(result.Target, "unknown")
	m.forwardTotal.WithLabelValues(target, status).Inc()
	if result.Duration > 0 {
		m.forwardDuration.WithLabelValues(target, status).Observe(result.Duration.Seconds())
	}
}

func (m *Metrics) SetSnapshot(snapshot state.Snapshot) {
	m.resetSnapshotGauges()
	for _, account := range snapshot.Accounts {
		labels := prometheus.Labels{"account": account.Account}
		m.accountOnline.With(labels).Set(boolFloat(account.Online))
		m.accountArmed.With(labels).Set(boolFloat(account.Armed))
		m.accountNightMode.With(labels).Set(boolFloat(account.NightMode))
		m.accountPartial.With(labels).Set(boolFloat(account.PartiallyArmed))
		m.accountAlarm.With(labels).Set(boolFloat(account.AlarmActive))
		m.accountTamper.With(labels).Set(boolFloat(account.TamperActive))
		m.accountTrouble.With(labels).Set(boolFloat(account.TroubleActive))
		m.accountLastEvent.With(labels).Set(timestamp(account.LastEventAt))
		m.accountLastPing.With(labels).Set(timestamp(account.LastPingAt))
	}
	for _, zone := range snapshot.Zones {
		labels := deviceLabels(zone)
		m.zoneTrouble.With(labels).Set(boolFloat(zone.TroubleActive))
		m.zoneLastEvent.With(labels).Set(timestamp(zone.LastEventAt))

		alarmLabels := deviceLabels(zone)
		alarmLabels["alarm_signal"] = labelValue(zone.AlarmSignal, "none")
		alarmLabels["alarm_action"] = labelValue(zone.AlarmAction, "none")
		m.zoneAlarm.With(alarmLabels).Set(boolFloat(zone.AlarmActive))
		if !zone.AlarmStartedAt.IsZero() {
			m.zoneAlarmLast.With(alarmLabels).Set(timestamp(zone.AlarmStartedAt))
		}

		tamperLabels := deviceLabels(zone)
		m.zoneTamper.With(tamperLabels).Set(boolFloat(zone.TamperActive))
		if zone.LastSignal == "tamper" {
			m.zoneTamperLast.With(tamperLabels).Set(timestamp(zone.LastEventAt))
		}
	}
}

func (m *Metrics) resetSnapshotGauges() {
	m.accountOnline.Reset()
	m.accountArmed.Reset()
	m.accountNightMode.Reset()
	m.accountPartial.Reset()
	m.accountAlarm.Reset()
	m.accountTamper.Reset()
	m.accountTrouble.Reset()
	m.accountLastEvent.Reset()
	m.accountLastPing.Reset()
	m.zoneAlarm.Reset()
	m.zoneAlarmLast.Reset()
	m.zoneTamper.Reset()
	m.zoneTamperLast.Reset()
	m.zoneTrouble.Reset()
	m.zoneLastEvent.Reset()
}

func deviceLabels(zone state.Zone) prometheus.Labels {
	device := zone.Device
	if device == "" {
		device = zone.Zone
	}
	return prometheus.Labels{
		"account":       labelValue(zone.Account, "unknown"),
		"partition":     labelValue(zone.Partition, "unknown"),
		"group":         labelValue(zone.Group, "unknown"),
		"zone":          labelValue(zone.Zone, "unknown"),
		"device":        labelValue(device, "unknown"),
		"device_name":   labelValue(zone.DeviceName, "unknown"),
		"room":          labelValue(zone.Room, "unknown"),
		"device_kind":   labelValue(zone.Kind, "unknown"),
		"device_events": labelValue(zone.DeviceEventsLabel, "unknown"),
	}
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func timestamp(value interface {
	Unix() int64
	IsZero() bool
}) float64 {
	if value.IsZero() {
		return 0
	}
	return float64(value.Unix())
}

func labelValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if _, err := strconv.Atoi(value); err == nil {
		return value
	}
	return value
}
