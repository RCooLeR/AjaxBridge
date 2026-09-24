package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/config"
	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
	"github.com/RCooLeR/AjaxBridge/internal/event"
	"github.com/RCooLeR/AjaxBridge/internal/forward"
	"github.com/RCooLeR/AjaxBridge/internal/hamqtt"
	"github.com/RCooLeR/AjaxBridge/internal/httpapi"
	"github.com/RCooLeR/AjaxBridge/internal/jeedom"
	"github.com/RCooLeR/AjaxBridge/internal/metrics"
	"github.com/RCooLeR/AjaxBridge/internal/notifications"
	"github.com/RCooLeR/AjaxBridge/internal/sia"
	"github.com/RCooLeR/AjaxBridge/internal/state"
	"github.com/RCooLeR/AjaxBridge/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type App struct {
	cfg       config.Config
	log       zerolog.Logger
	store     *store.Store
	state     *state.Engine
	devices   *devicecatalog.Catalog
	metrics   *metrics.Metrics
	parser    *sia.Parser
	responder *sia.Responder
	forwarder *forward.Group
	mqtt      *hamqtt.Publisher
	mqttQueue chan hamqtt.Update
	notifier  *notifications.Manager
	jeedom    *jeedom.Store
	jeedomPub *jeedom.Publisher
}

func Run(parent context.Context, cfg config.Config, log zerolog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	eventStore := store.New()
	defer eventStore.Close()

	devices, err := devicecatalog.Load(ctx, cfg.DevicesPath)
	if err != nil {
		return err
	}
	if cfg.DevicesPath != "" {
		log.Info().Str("path", cfg.DevicesPath).Int("devices", len(devices.Devices())).Msg("device catalog loaded")
	}

	parser, err := sia.NewParser(cfg.Account, cfg.StrictCRC, cfg.EncryptionKey)
	if err != nil {
		return err
	}
	responder, err := sia.NewResponder(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	registry := prometheus.NewRegistry()
	metricSet := metrics.New(registry)
	stateEngine := state.NewEngine(cfg.OfflineGrace, devices)
	metricSet.SetSnapshot(stateEngine.Snapshot())
	notificationStore, err := notifications.Load(ctx, cfg.NotificationsPath)
	if err != nil {
		return err
	}
	notifier := notifications.NewManager(notificationStore, log.With().Str("component", "notifications").Logger())
	if cfg.NotificationsPath != "" {
		log.Info().Str("path", cfg.NotificationsPath).Msg("notification config loaded")
	}
	var mqttPublisher *hamqtt.Publisher
	var mqttQueue chan hamqtt.Update
	if cfg.MQTTEnabled() {
		mqttPublisher = hamqtt.New(hamqtt.Config{
			Broker:          cfg.MQTTBroker,
			Username:        cfg.MQTTUsername,
			Password:        cfg.MQTTPassword,
			ClientID:        cfg.MQTTClientID,
			TopicPrefix:     cfg.MQTTTopicPrefix,
			Discovery:       cfg.MQTTDiscovery,
			DiscoveryPrefix: cfg.MQTTDiscoveryPrefix,
			Timeout:         cfg.MQTTTimeout,
			Retain:          cfg.MQTTRetain,
		}, log.With().Str("component", "mqtt").Logger())
		mqttPublisher.ConnectAsync(ctx)
		mqttQueue = make(chan hamqtt.Update, 1)
		defer mqttPublisher.Close()
	}
	var siaForwarder *forward.Group
	forwardAddrs := cfg.ForwardAddresses()
	if len(forwardAddrs) > 0 {
		siaForwarder = forward.NewGroup(forwardAddrs, cfg.ForwardTimeout)
		log.Info().
			Strs("addrs", forwardAddrs).
			Dur("timeout", cfg.ForwardTimeout).
			Bool("require_ack", cfg.ForwardRequireACK).
			Msg("SIA forwarders enabled")
	}

	var jeedomStore *jeedom.Store
	var jeedomController *jeedom.Controller
	var jeedomPublisher *jeedom.Publisher
	if cfg.JeedomEnabled {
		jeedomStoreLoaded := false
		resolver := jeedom.NewCatalogResolver(devices, jeedom.CatalogResolverConfig{
			Account:          cfg.Account,
			AccountNames:     cfg.JeedomAccountNames,
			DiscoverUnlinked: cfg.JeedomDiscoverUnlinked,
		})
		jeedomStore, err = jeedom.LoadStore(ctx, cfg.JeedomStorePath, cfg.JeedomEmptyValuePolicy, resolver)
		if err != nil {
			log.Warn().Err(err).Str("path", cfg.JeedomStorePath).Msg("Jeedom cache not loaded; starting empty")
			jeedomStore = jeedom.NewStoreWithResolver(cfg.JeedomEmptyValuePolicy, resolver)
			jeedomStore.SetPath(cfg.JeedomStorePath)
		} else {
			jeedomStoreLoaded = true
			if cfg.JeedomStorePath != "" {
				log.Info().Str("path", cfg.JeedomStorePath).Int("devices", len(jeedomStore.Devices())).Msg("Jeedom cache loaded")
			}
		}
		jeedomStore.ReconcileResolver(resolver)
		if jeedomStoreLoaded && cfg.JeedomStorePath != "" {
			if saveErr := jeedomStore.Save(ctx); saveErr != nil {
				log.Warn().Err(saveErr).Str("path", cfg.JeedomStorePath).Msg("persist reconciled Jeedom cache")
			}
		}
		if mqttPublisher != nil {
			jeedomController = jeedom.NewController(jeedom.ControllerConfig{
				Enabled:              cfg.JeedomControlsEnabled,
				StateTopicPrefix:     cfg.JeedomStateTopicPrefix,
				JeedomSetTopicPrefix: cfg.JeedomSetTopicPrefix,
				CommandPayload:       cfg.JeedomControlPayload,
			}, jeedomStore, mqttPublisher, log.With().Str("component", "jeedom_control").Logger())
			jeedomPublisher = jeedom.NewPublisher(jeedom.PublisherConfig{
				StateTopicPrefix: cfg.JeedomStateTopicPrefix,
				Discovery:        cfg.JeedomDiscovery,
				DiscoveryPrefix:  cfg.MQTTDiscoveryPrefix,
				DiscoveryNode:    cfg.MQTTTopicPrefix,
				RetainState:      cfg.JeedomRetainState,
				RetainDiscovery:  cfg.JeedomRetainDiscovery,
				Controls:         cfg.JeedomControlsEnabled,
			}, mqttPublisher)
		}
	}

	application := &App{
		cfg:       cfg,
		log:       log,
		store:     eventStore,
		state:     stateEngine,
		devices:   devices,
		metrics:   metricSet,
		parser:    parser,
		responder: responder,
		forwarder: siaForwarder,
		mqtt:      mqttPublisher,
		mqttQueue: mqttQueue,
		notifier:  notifier,
		jeedom:    jeedomStore,
		jeedomPub: jeedomPublisher,
	}
	notificationObserver := notificationObserver{app: application}
	if jeedomController != nil {
		jeedomController.SetObserver(notificationObserver)
	}
	if application.mqtt != nil {
		application.mqtt.AddConnectHandler(func(context.Context) {
			application.publishRetainedMQTT(ctx)
		})
	}

	httpServer := httpapi.New(
		cfg.HTTPAddr,
		stateEngine,
		eventStore,
		devices,
		jeedomStore,
		jeedomController,
		notifier,
		registry,
		log.With().Str("component", "http").Logger(),
		application.handleCatalogChanged,
	)
	siaServer := sia.NewServer(cfg.SIAListenAddr, cfg.ReadTimeout, application.handleSIAFrame, log.With().Str("component", "sia").Logger())

	if cfg.JeedomEnabled && mqttPublisher != nil {
		jeedomService := jeedom.NewService(
			jeedom.ServiceConfig{EventTopic: cfg.JeedomEventTopic, DiscoveryTopic: cfg.JeedomDiscoveryTopic, SetTopicPrefix: cfg.JeedomSetTopicPrefix},
			jeedomStore,
			mqttPublisher,
			jeedomPublisher,
			metricSet,
			jeedom.NewSampleWriter(cfg.JeedomSampleDir),
			log.With().Str("component", "jeedom").Logger(),
		)
		jeedomService.SetController(jeedomController)
		jeedomService.SetObserver(notificationObserver)
		if err := jeedomService.Start(ctx); err != nil {
			log.Warn().Err(err).Str("topic", cfg.JeedomEventTopic).Msg("Jeedom MQTT input not started")
		} else {
			log.Info().
				Str("event_topic", cfg.JeedomEventTopic).
				Str("discovery_topic", cfg.JeedomDiscoveryTopic).
				Str("state_prefix", cfg.JeedomStateTopicPrefix).
				Str("sample_dir", cfg.JeedomSampleDir).
				Bool("controls", cfg.JeedomControlsEnabled).
				Msg("Jeedom MQTT input enabled")
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() {
		if err := httpServer.Run(ctx); err != nil {
			errs <- err
			stop()
		}
	})
	wg.Go(func() {
		if err := siaServer.Run(ctx); err != nil {
			errs <- err
			stop()
		}
	})
	go application.refreshOnline(ctx)
	if application.mqtt != nil {
		go application.publishMQTTSnapshots(ctx)
		initialSnapshot := stateEngine.Snapshot()
		application.enqueueMQTTUpdate(hamqtt.Update{
			Accounts: initialSnapshot.Accounts,
			Zones:    initialSnapshot.Zones,
		})
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return nil
}

type notificationObserver struct {
	app *App
}

func (o notificationObserver) ObserveJeedomUpdate(ctx context.Context, result jeedom.ApplyResult) {
	if o.app == nil || o.app.notifier == nil {
		return
	}
	o.app.notifier.ObserveJeedomUpdate(ctx, result, o.app.state.Snapshot())
}

func (o notificationObserver) ObserveJeedomControl(ctx context.Context, result jeedom.ControlResult, err error) {
	if o.app == nil {
		return
	}
	if err == nil && result.StateUpdated && o.app.jeedom != nil && o.app.jeedomPub != nil {
		if device, ok := o.app.jeedom.Device(result.DeviceSlug); ok {
			if publishErr := o.app.jeedomPub.PublishDevice(ctx, device); publishErr != nil {
				o.app.log.Debug().Err(publishErr).Str("device", result.DeviceSlug).Msg("publish optimistic Jeedom control state")
			}
		}
	}
	if o.app.notifier != nil {
		o.app.notifier.ObserveJeedomControl(ctx, result, err, o.app.state.Snapshot())
	}
}

func (a *App) handleCatalogChanged(snapshot state.Snapshot) {
	if a.jeedom != nil {
		resolver := jeedom.NewCatalogResolver(a.devices, jeedom.CatalogResolverConfig{
			Account:          a.cfg.Account,
			AccountNames:     a.cfg.JeedomAccountNames,
			DiscoverUnlinked: a.cfg.JeedomDiscoverUnlinked,
		})
		devices := a.jeedom.ReconcileResolver(resolver)
		if err := a.jeedom.Save(context.Background()); err != nil {
			a.log.Warn().Err(err).Msg("persist Jeedom cache after catalog change")
		}
		if a.jeedomPub != nil {
			ctx, cancel := context.WithTimeout(context.Background(), a.cfg.MQTTTimeout)
			defer cancel()
			a.publishJeedomDevices(ctx, devices, "publish Jeedom device after catalog change")
		}
	}
	a.metrics.SetSnapshot(snapshot)
	a.enqueueMQTTUpdate(hamqtt.Update{Accounts: snapshot.Accounts, Zones: snapshot.Zones})
}

func (a *App) publishRetainedMQTT(ctx context.Context) {
	if a == nil || a.mqtt == nil {
		return
	}
	// MQTT retained messages can disappear after broker maintenance, so every
	// connect/reconnect republishes the last known SIA and Jeedom surfaces.
	snapshot := a.state.Snapshot()
	if err := a.mqtt.PublishUpdate(ctx, hamqtt.Update{Accounts: snapshot.Accounts, Zones: snapshot.Zones}); err != nil {
		a.log.Debug().Err(err).Msg("publish retained MQTT snapshot")
	}
	a.publishJeedomDevices(ctx, a.storedJeedomDevices(), "publish retained Jeedom MQTT state")
}

func (a *App) storedJeedomDevices() []jeedom.Device {
	if a == nil || a.jeedom == nil {
		return nil
	}
	return a.jeedom.Devices()
}

func (a *App) publishJeedomDevices(ctx context.Context, devices []jeedom.Device, message string) {
	if a == nil || a.jeedomPub == nil {
		return
	}
	acknowledgedCleanup := false
	for _, device := range devices {
		published, err := a.jeedomPub.PublishDeviceWithResult(ctx, device)
		if err != nil {
			a.log.Debug().Err(err).Str("device", device.DeviceSlug).Msg(message)
			continue
		}
		if a.jeedom != nil && published && a.jeedomPub.DiscoveryEnabled() {
			commandsAcknowledged := a.jeedom.AcknowledgeCommandCleanups(device.PendingDiscoveryCleanups)
			actionsAcknowledged := a.jeedom.AcknowledgeActionCleanups(device.PendingActionDiscoveryCleanups)
			if commandsAcknowledged || actionsAcknowledged {
				acknowledgedCleanup = true
			}
		}
	}
	if acknowledgedCleanup {
		if err := a.jeedom.Save(ctx); err != nil {
			a.log.Warn().Err(err).Msg("persist acknowledged Jeedom MQTT discovery cleanup")
		}
	}
}

func (a *App) handleSIAFrame(ctx context.Context, raw []byte, remoteAddr string) ([]byte, error) {
	startedAt := time.Now().UTC()
	evt, frame, err := a.parser.Parse(raw)
	if evt == nil {
		evt = &event.Normalized{
			ReceivedAt:  time.Now().UTC(),
			OccurredAt:  time.Now().UTC(),
			RawMessage:  string(raw),
			ParseStatus: sia.ParseStatusFromError(err),
			ParseError:  errString(err),
		}
	}
	a.metrics.ObserveEvent(*evt)
	if storeErr := a.store.AppendEvent(ctx, *evt); storeErr != nil {
		a.log.Error().Err(storeErr).Msg("append SIA event")
	}

	var response []byte
	responseKind := string(sia.ResponseACK)
	var snapshot state.Snapshot
	forwardResults := []forward.Result{forward.Disabled()}
	if err != nil && a.forwarder != nil {
		forwardResults = forward.SkippedAll(a.forwarder.Addrs(), "parse_status="+string(evt.ParseStatus))
		a.observeForwardResults(forwardResults)
	}

	if err != nil {
		responseKind = "none"
		a.log.Warn().
			Err(err).
			Interface("event", normalizedLogEvent(evt)).
			Msg("sia_event_rejected")
		if evt.ParseStatus == event.ParseStatusCRCInvalid {
			snapshot = a.state.Snapshot()
			a.logAjaxRequest(startedAt, remoteAddr, raw, evt, frame, responseKind, response, err, snapshot, forwardResults)
			return nil, err
		}
		if evt.ParseStatus == event.ParseStatusAccountInvalid || evt.ParseStatus == event.ParseStatusDecryptInvalid {
			responseKind = string(sia.ResponseNAK)
			response = a.responder.Build(sia.ResponseNAK, frame)
			snapshot = a.state.Snapshot()
			a.logAjaxRequest(startedAt, remoteAddr, raw, evt, frame, responseKind, response, err, snapshot, forwardResults)
			return response, err
		}
		responseKind = string(sia.ResponseDUH)
		response = a.responder.Build(sia.ResponseDUH, frame)
		snapshot = a.state.Snapshot()
		a.logAjaxRequest(startedAt, remoteAddr, raw, evt, frame, responseKind, response, err, snapshot, forwardResults)
		return response, err
	}

	a.discoverDevice(ctx, evt)
	snapshot = a.state.Apply(*evt)
	a.metrics.SetSnapshot(snapshot)
	a.enqueueMQTTUpdate(mqttUpdateForEvent(snapshot, evt))
	forwardResults = a.forwardAjaxFrame(ctx, raw, evt)
	a.log.Info().
		Interface("event", normalizedLogEvent(evt)).
		Str("forward_summary", forward.Summary(forwardResults)).
		Msg("sia_event")
	if a.cfg.ForwardRequireACK && !forward.AllACK(forwardResults) {
		forwardErr := fmt.Errorf("one or more upstream SIA receivers did not ACK: %s", forward.Summary(forwardResults))
		a.log.Warn().
			Err(forwardErr).
			Str("remote", remoteAddr).
			Str("forward_summary", forward.Summary(forwardResults)).
			Msg("returning NAK because upstream SIA ACK is required")
		responseKind = string(sia.ResponseNAK)
		response = a.responder.Build(sia.ResponseNAK, frame)
		a.logAjaxRequest(startedAt, remoteAddr, raw, evt, frame, responseKind, response, forwardErr, snapshot, forwardResults)
		return response, forwardErr
	}
	response = a.responder.Build(sia.ResponseACK, frame)
	a.logAjaxRequest(startedAt, remoteAddr, raw, evt, frame, responseKind, response, nil, snapshot, forwardResults)
	return response, nil
}

func (a *App) discoverDevice(ctx context.Context, evt *event.Normalized) {
	if a.devices == nil || evt == nil || evt.ParseStatus != event.ParseStatusOK || evt.Zone == "" {
		return
	}
	result, err := a.devices.UpsertFromEvent(ctx, *evt)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("account", evt.Account).
			Str("zone", evt.Zone).
			Str("device", evt.Device).
			Msg("upsert device catalog")
		return
	}
	switch {
	case result.Created:
		a.log.Info().
			Str("account", result.Device.Account).
			Str("zone", result.Device.Zone).
			Str("device", result.Device.Device).
			Str("name", result.Device.Name).
			Str("path", a.devices.Path()).
			Msg("auto-discovered device added to catalog")
	case result.Updated:
		a.log.Debug().
			Str("account", result.Device.Account).
			Str("zone", result.Device.Zone).
			Str("device", result.Device.Device).
			Str("path", a.devices.Path()).
			Msg("device catalog updated from event")
	}
}

func (a *App) forwardAjaxFrame(ctx context.Context, raw []byte, evt *event.Normalized) []forward.Result {
	if a.forwarder == nil || !a.forwarder.Enabled() {
		return []forward.Result{forward.Disabled()}
	}
	if evt == nil {
		results := forward.SkippedAll(a.forwarder.Addrs(), "event=nil")
		a.observeForwardResults(results)
		return results
	}
	if evt.ParseStatus != event.ParseStatusOK {
		results := forward.SkippedAll(a.forwarder.Addrs(), "parse_status="+string(evt.ParseStatus))
		a.observeForwardResults(results)
		return results
	}
	results := a.forwarder.Send(ctx, raw)
	a.observeForwardResults(results)
	for _, result := range results {
		if result.ACK {
			a.log.Debug().
				Str("target", result.Target).
				Dur("duration", result.Duration).
				Msg("forwarded SIA frame upstream")
			continue
		}
		a.log.Warn().
			Str("target", result.Target).
			Str("status", result.Status).
			Str("error", result.Error).
			Dur("duration", result.Duration).
			Msg("SIA upstream forward did not ACK")
	}
	return results
}

func (a *App) observeForwardResults(results []forward.Result) {
	for _, result := range results {
		a.metrics.ObserveForward(result)
	}
}

func (a *App) enqueueMQTTUpdate(update hamqtt.Update) {
	if a.mqtt == nil || a.mqttQueue == nil {
		return
	}
	select {
	case a.mqttQueue <- update:
		return
	default:
	}
	select {
	case <-a.mqttQueue:
	default:
	}
	select {
	case a.mqttQueue <- update:
	default:
	}
}

func (a *App) publishMQTTSnapshots(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-a.mqttQueue:
			if err := a.mqtt.PublishUpdate(ctx, update); err != nil {
				a.log.Debug().Err(err).Msg("publish MQTT update")
			}
		}
	}
}

func (a *App) logAjaxRequest(startedAt time.Time, remoteAddr string, raw []byte, evt *event.Normalized, frame *sia.Frame, responseKind string, response []byte, parseErr error, snapshot state.Snapshot, forwardResults []forward.Result) {
	dump := a.ajaxRequestDump(startedAt, remoteAddr, raw, evt, frame, responseKind, response, parseErr, snapshot, forwardResults)
	a.log.Debug().
		Str("remote", remoteAddr).
		Str("parse_status", string(evt.ParseStatus)).
		Str("event_code", evt.EventCode).
		Str("response", responseKind).
		Msg("ajax hub request debug\n" + dump)
}

func (a *App) ajaxRequestDump(startedAt time.Time, remoteAddr string, raw []byte, evt *event.Normalized, frame *sia.Frame, responseKind string, response []byte, parseErr error, snapshot state.Snapshot, forwardResults []forward.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== AJAX SIA DC-09 REQUEST ===\n")
	fmt.Fprintf(&b, "received_at_utc: %s\n", startedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "remote_addr: %s\n", remoteAddr)
	fmt.Fprintf(&b, "processing_duration: %s\n", time.Since(startedAt))
	fmt.Fprintf(&b, "configured_account: %s\n", emptyValue(a.cfg.Account))
	fmt.Fprintf(&b, "strict_crc: %t\n", a.cfg.StrictCRC)
	fmt.Fprintf(&b, "encrypted_receiver_configured: %t\n", a.cfg.EncryptionKey != "")
	fmt.Fprintf(&b, "forward_enabled: %t\n", anyForwardEnabled(forwardResults))
	fmt.Fprintf(&b, "forward_targets: %d\n", countForwardTargets(forwardResults))
	fmt.Fprintf(&b, "in_memory_events_count: %d\n", a.store.Count())

	fmt.Fprintf(&b, "\n--- raw request ---\n")
	fmt.Fprintf(&b, "bytes_len: %d\n", len(raw))
	fmt.Fprintf(&b, "ascii_escaped: %q\n", string(raw))
	fmt.Fprintf(&b, "hex: %s\n", strings.ToUpper(hex.EncodeToString(raw)))

	fmt.Fprintf(&b, "\n--- dc09 frame ---\n")
	if frame == nil {
		fmt.Fprintf(&b, "parsed: false\n")
	} else {
		fmt.Fprintf(&b, "parsed: true\n")
		fmt.Fprintf(&b, "raw_without_lf_cr: %q\n", frame.Raw)
		fmt.Fprintf(&b, "crc: %s\n", emptyValue(frame.CRC))
		fmt.Fprintf(&b, "length_hex: %s\n", emptyValue(frame.Length))
		fmt.Fprintf(&b, "payload_len: %d\n", len(frame.Payload))
		fmt.Fprintf(&b, "payload: %q\n", frame.Payload)
		fmt.Fprintf(&b, "token: %s\n", emptyValue(frame.Token))
		fmt.Fprintf(&b, "encrypted: %t\n", frame.Encrypted)
		fmt.Fprintf(&b, "sequence: %s\n", emptyValue(frame.Sequence))
		fmt.Fprintf(&b, "receiver: %s\n", emptyValue(frame.Receiver))
		fmt.Fprintf(&b, "line: %s\n", emptyValue(frame.Line))
		fmt.Fprintf(&b, "account: %s\n", emptyValue(frame.Account))
	}

	fmt.Fprintf(&b, "\n--- normalized event ---\n")
	fmt.Fprintf(&b, "account: %s\n", emptyValue(evt.Account))
	fmt.Fprintf(&b, "protocol: %s\n", emptyValue(evt.Protocol))
	fmt.Fprintf(&b, "sequence: %s\n", emptyValue(evt.Sequence))
	fmt.Fprintf(&b, "receiver: %s\n", emptyValue(evt.Receiver))
	fmt.Fprintf(&b, "line: %s\n", emptyValue(evt.Line))
	fmt.Fprintf(&b, "event_code: %s\n", emptyValue(evt.EventCode))
	fmt.Fprintf(&b, "contact_id: %s\n", emptyValue(evt.ContactID))
	fmt.Fprintf(&b, "event_class: %s\n", emptyValue(string(evt.EventClass)))
	fmt.Fprintf(&b, "event_action: %s\n", emptyValue(evt.EventAction))
	fmt.Fprintf(&b, "event_name: %s\n", emptyValue(evt.EventName))
	fmt.Fprintf(&b, "description: %s\n", emptyValue(evt.Description))
	fmt.Fprintf(&b, "source: %s\n", emptyValue(evt.Source))
	fmt.Fprintf(&b, "signal: %s\n", emptyValue(evt.Signal))
	fmt.Fprintf(&b, "severity: %s\n", emptyValue(evt.Severity))
	fmt.Fprintf(&b, "partition: %s\n", emptyValue(evt.Partition))
	fmt.Fprintf(&b, "group: %s\n", emptyValue(evt.Group))
	fmt.Fprintf(&b, "zone: %s\n", emptyValue(evt.Zone))
	fmt.Fprintf(&b, "device: %s\n", emptyValue(evt.Device))
	fmt.Fprintf(&b, "user: %s\n", emptyValue(evt.User))
	fmt.Fprintf(&b, "occurred_at_utc: %s\n", formatTime(evt.OccurredAt))
	fmt.Fprintf(&b, "received_at_utc: %s\n", formatTime(evt.ReceivedAt))
	fmt.Fprintf(&b, "encrypted: %t\n", evt.Encrypted)
	fmt.Fprintf(&b, "parse_status: %s\n", evt.ParseStatus)
	fmt.Fprintf(&b, "parse_error: %s\n", emptyValue(evt.ParseError))
	fmt.Fprintf(&b, "raw_data: %q\n", evt.RawData)
	fmt.Fprintf(&b, "raw_payload: %q\n", evt.RawPayload)
	fmt.Fprintf(&b, "raw_message: %q\n", evt.RawMessage)
	fmt.Fprintf(&b, "xdata_count: %d\n", len(evt.XData))
	for _, entry := range evt.XData {
		fmt.Fprintf(&b, "xdata identifier=%s value=%q raw=%q\n", emptyValue(entry.Identifier), entry.Value, entry.Raw)
	}
	if parseErr != nil {
		fmt.Fprintf(&b, "handler_error: %s\n", parseErr.Error())
	}

	fmt.Fprintf(&b, "\n--- response ---\n")
	fmt.Fprintf(&b, "response_kind: %s\n", responseKind)
	fmt.Fprintf(&b, "bytes_len: %d\n", len(response))
	fmt.Fprintf(&b, "ascii_escaped: %q\n", string(response))
	fmt.Fprintf(&b, "hex: %s\n", strings.ToUpper(hex.EncodeToString(response)))

	fmt.Fprintf(&b, "\n--- upstream SIA forward ---\n")
	fmt.Fprintf(&b, "require_ack: %t\n", a.cfg.ForwardRequireACK)
	fmt.Fprintf(&b, "all_ack: %t\n", forward.AllACK(forwardResults))
	fmt.Fprintf(&b, "summary: %s\n", forward.Summary(forwardResults))
	for i, result := range forwardResults {
		fmt.Fprintf(&b, "target_index: %d\n", i)
		fmt.Fprintf(&b, "enabled: %t\n", result.Enabled)
		fmt.Fprintf(&b, "target: %s\n", emptyValue(result.Target))
		fmt.Fprintf(&b, "status: %s\n", emptyValue(result.Status))
		fmt.Fprintf(&b, "ack: %t\n", result.ACK)
		fmt.Fprintf(&b, "duration: %s\n", result.Duration)
		fmt.Fprintf(&b, "request_bytes: %d\n", result.RequestBytes)
		fmt.Fprintf(&b, "response_bytes: %d\n", result.ResponseBytes)
		fmt.Fprintf(&b, "response_ascii_escaped: %q\n", result.ResponseASCII)
		fmt.Fprintf(&b, "response_hex: %s\n", emptyValue(result.ResponseHex))
		fmt.Fprintf(&b, "error: %s\n", emptyValue(result.Error))
	}

	fmt.Fprintf(&b, "\n--- current state snapshot ---\n")
	fmt.Fprintf(&b, "accounts: %d\n", len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		fmt.Fprintf(
			&b,
			"account=%s online=%t armed=%t partially_armed=%t alarm_active=%t tamper_active=%t trouble_active=%t last_event=%s last_ping=%s\n",
			account.Account,
			account.Online,
			account.Armed,
			account.PartiallyArmed,
			account.AlarmActive,
			account.TamperActive,
			account.TroubleActive,
			formatTime(account.LastEventAt),
			formatTime(account.LastPingAt),
		)
		fmt.Fprintf(
			&b,
			"account=%s mode=%s night_mode=%t last_code=%s last_name=%q last_signal=%s\n",
			account.Account,
			account.Mode,
			account.NightMode,
			emptyValue(account.LastEventCode),
			account.LastEventName,
			emptyValue(account.LastSignal),
		)
	}
	fmt.Fprintf(&b, "zones: %d\n", len(snapshot.Zones))
	for _, zone := range snapshot.Zones {
		fmt.Fprintf(
			&b,
			"account=%s partition=%s group=%s zone=%s device=%s device_name=%q room=%q kind=%q device_events=%q alarm_active=%t alarm_signal=%s alarm_action=%s alarm_code=%s alarm_name=%q tamper_active=%t trouble_active=%t last_code=%s last_name=%q last_signal=%s last_event=%s\n",
			zone.Account,
			emptyValue(zone.Partition),
			emptyValue(zone.Group),
			zone.Zone,
			emptyValue(zone.Device),
			zone.DeviceName,
			zone.Room,
			zone.Kind,
			zone.DeviceEventsLabel,
			zone.AlarmActive,
			emptyValue(zone.AlarmSignal),
			emptyValue(zone.AlarmAction),
			emptyValue(zone.AlarmEventCode),
			zone.AlarmEventName,
			zone.TamperActive,
			zone.TroubleActive,
			emptyValue(zone.LastEventCode),
			zone.LastEventName,
			emptyValue(zone.LastSignal),
			formatTime(zone.LastEventAt),
		)
	}
	fmt.Fprintf(&b, "=== END AJAX SIA DC-09 REQUEST ===")
	return b.String()
}

func (a *App) refreshOnline(ctx context.Context) {
	ticker := time.NewTicker(minDuration(a.cfg.PingInterval, 30*time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			snapshot := a.state.RefreshOnline(now.UTC())
			a.metrics.SetSnapshot(snapshot)
			a.enqueueMQTTUpdate(hamqtt.Update{Accounts: snapshot.Accounts})
		}
	}
}

func mqttUpdateForEvent(snapshot state.Snapshot, evt *event.Normalized) hamqtt.Update {
	update := hamqtt.Update{}
	if evt == nil {
		return update
	}
	if account, ok := snapshotAccount(snapshot, evt.Account); ok {
		update.Accounts = append(update.Accounts, account)
	}
	if evt.Zone != "" {
		if zone, ok := snapshotZone(snapshot, evt.Account, evt.Zone); ok {
			update.Zones = append(update.Zones, zone)
		}
	}
	return update
}

func snapshotAccount(snapshot state.Snapshot, accountID string) (state.Account, bool) {
	for _, account := range snapshot.Accounts {
		if account.Account == accountID {
			return account, true
		}
	}
	return state.Account{}, false
}

func snapshotZone(snapshot state.Snapshot, accountID, zoneID string) (state.Zone, bool) {
	for _, zone := range snapshot.Zones {
		if zone.Account == accountID && zone.Zone == zoneID {
			return zone, true
		}
	}
	return state.Zone{}, false
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type logEvent struct {
	Account     string    `json:"account"`
	Protocol    string    `json:"protocol"`
	Sequence    string    `json:"sequence"`
	Receiver    string    `json:"receiver"`
	Line        string    `json:"line"`
	EventCode   string    `json:"event_code"`
	ContactID   string    `json:"contact_id,omitempty"`
	EventClass  string    `json:"event_class"`
	EventAction string    `json:"event_action"`
	EventName   string    `json:"event_name"`
	Source      string    `json:"source"`
	Signal      string    `json:"signal"`
	Severity    string    `json:"severity"`
	Partition   string    `json:"partition"`
	Group       string    `json:"group"`
	Zone        string    `json:"zone"`
	Device      string    `json:"device,omitempty"`
	User        string    `json:"user,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
	ReceivedAt  time.Time `json:"received_at"`
	ParseStatus string    `json:"parse_status"`
	Encrypted   bool      `json:"encrypted"`
}

func normalizedLogEvent(evt *event.Normalized) logEvent {
	if evt == nil {
		return logEvent{}
	}
	return logEvent{
		Account:     evt.Account,
		Protocol:    evt.Protocol,
		Sequence:    evt.Sequence,
		Receiver:    evt.Receiver,
		Line:        evt.Line,
		EventCode:   evt.EventCode,
		ContactID:   evt.ContactID,
		EventClass:  string(evt.EventClass),
		EventAction: evt.EventAction,
		EventName:   evt.EventName,
		Source:      evt.Source,
		Signal:      evt.Signal,
		Severity:    evt.Severity,
		Partition:   evt.Partition,
		Group:       evt.Group,
		Zone:        evt.Zone,
		Device:      evt.Device,
		User:        evt.User,
		OccurredAt:  evt.OccurredAt,
		ReceivedAt:  evt.ReceivedAt,
		ParseStatus: string(evt.ParseStatus),
		Encrypted:   evt.Encrypted,
	}
}

func emptyValue(value string) string {
	if value == "" {
		return "<empty>"
	}
	return value
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "<zero>"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func anyForwardEnabled(results []forward.Result) bool {
	for _, result := range results {
		if result.Enabled {
			return true
		}
	}
	return false
}

func countForwardTargets(results []forward.Result) int {
	count := 0
	for _, result := range results {
		if result.Enabled {
			count++
		}
	}
	return count
}
