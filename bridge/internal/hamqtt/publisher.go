package hamqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/AjaxBridge/internal/state"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

const (
	payloadOnline  = "online"
	payloadOffline = "offline"
	payloadOn      = "ON"
	payloadOff     = "OFF"
)

var legacyDiscoveryNodes = []string{"ajax2prometheus"}

type Config struct {
	SourceID        string
	Broker          string
	Username        string
	Password        string
	ClientID        string
	TopicPrefix     string
	Discovery       bool
	DiscoveryPrefix string
	Timeout         time.Duration
	Retain          bool
}

type Publisher struct {
	cfg             Config
	client          paho.Client
	log             zerolog.Logger
	mu              sync.Mutex
	discovered      map[string]string
	publishedStates map[string]string
	accountPlans    map[string]accountPlan
	zonePlans       map[string]zonePlan
	subscriptions   map[string]MessageHandler
	connectHandlers []func(context.Context)
}

type Update struct {
	Accounts []state.Account
	Zones    []state.Zone
}

type MessageHandler = func(topic string, payload []byte)

type accountPlan struct {
	stateTopic      string
	attributesTopic string
	cleanup         []discoveryMessage
	discovery       []discoveryMessage
}

type zonePlan struct {
	signature       string
	stateTopic      string
	attributesTopic string
	cleanup         []discoveryMessage
	discovery       []discoveryMessage
}

type discoveryMessage struct {
	key     string
	topic   string
	payload []byte
}

type deviceInfo struct {
	Identifiers   []string `json:"identifiers"`
	Name          string   `json:"name"`
	Manufacturer  string   `json:"manufacturer"`
	Model         string   `json:"model,omitempty"`
	SuggestedArea string   `json:"suggested_area,omitempty"`
}

type discoveryConfig struct {
	Name                string     `json:"name"`
	UniqueID            string     `json:"unique_id"`
	StateTopic          string     `json:"state_topic"`
	ValueTemplate       string     `json:"value_template"`
	PayloadOn           string     `json:"payload_on,omitempty"`
	PayloadOff          string     `json:"payload_off,omitempty"`
	AvailabilityTopic   string     `json:"availability_topic"`
	PayloadAvailable    string     `json:"payload_available"`
	PayloadNotAvailable string     `json:"payload_not_available"`
	DeviceClass         string     `json:"device_class,omitempty"`
	EntityCategory      string     `json:"entity_category,omitempty"`
	Icon                string     `json:"icon,omitempty"`
	JSONAttributesTopic string     `json:"json_attributes_topic,omitempty"`
	Device              deviceInfo `json:"device"`
}

type entity struct {
	Component      string
	ObjectID       string
	Name           string
	ValueTemplate  string
	DeviceClass    string
	EntityCategory string
	Icon           string
	Binary         bool
}

type zonePayload struct {
	SourceID         string          `json:"source_id,omitempty"`
	Account          string          `json:"account"`
	Partition        string          `json:"partition"`
	Group            string          `json:"group"`
	Zone             string          `json:"zone"`
	Device           string          `json:"device"`
	DeviceName       string          `json:"device_name"`
	Room             string          `json:"room"`
	Kind             string          `json:"kind"`
	DeviceEvents     []string        `json:"device_events"`
	AlarmActive      bool            `json:"alarm_active"`
	AlarmSignal      string          `json:"alarm_signal"`
	AlarmAction      string          `json:"alarm_action"`
	AlarmEventCode   string          `json:"alarm_event_code"`
	AlarmEventName   string          `json:"alarm_event_name"`
	TamperActive     bool            `json:"tamper_active"`
	TroubleActive    bool            `json:"trouble_active"`
	SignalActive     map[string]bool `json:"signal_active"`
	LastEventCode    string          `json:"last_event_code"`
	LastEventName    string          `json:"last_event_name"`
	LastSignal       string          `json:"last_signal"`
	LastEventAt      string          `json:"last_event_at"`
	LastEventUnix    int64           `json:"last_event_unix"`
	AlarmStartedAt   string          `json:"alarm_started_at"`
	AlarmStartedUnix int64           `json:"alarm_started_unix"`
}

type accountPayload struct {
	SourceID       string `json:"source_id,omitempty"`
	Account        string `json:"account"`
	Online         bool   `json:"online"`
	Mode           string `json:"mode"`
	Armed          bool   `json:"armed"`
	NightMode      bool   `json:"night_mode"`
	PartiallyArmed bool   `json:"partially_armed"`
	AlarmActive    bool   `json:"alarm_active"`
	TamperActive   bool   `json:"tamper_active"`
	TroubleActive  bool   `json:"trouble_active"`
	LastEventCode  string `json:"last_event_code"`
	LastEventName  string `json:"last_event_name"`
	LastSignal     string `json:"last_signal"`
	LastEventAt    string `json:"last_event_at"`
	LastEventUnix  int64  `json:"last_event_unix"`
	LastPingAt     string `json:"last_ping_at"`
	LastPingUnix   int64  `json:"last_ping_unix"`
}

type accountAttributesPayload struct {
	SourceID string `json:"source_id,omitempty"`
	Account  string `json:"account"`
}

type zoneAttributesPayload struct {
	SourceID     string   `json:"source_id,omitempty"`
	Account      string   `json:"account"`
	Partition    string   `json:"partition"`
	Group        string   `json:"group"`
	Zone         string   `json:"zone"`
	Device       string   `json:"device"`
	DeviceName   string   `json:"device_name"`
	Room         string   `json:"room"`
	Kind         string   `json:"kind"`
	DeviceEvents []string `json:"device_events"`
}

func New(cfg Config, log zerolog.Logger) *Publisher {
	cfg.SourceID = strings.TrimSpace(cfg.SourceID)
	cfg.Broker = strings.TrimSpace(cfg.Broker)
	cfg.ClientID = fallback(strings.TrimSpace(cfg.ClientID), "ajaxbridge")
	cfg.TopicPrefix = trimTopic(fallback(strings.TrimSpace(cfg.TopicPrefix), "ajaxbridge"))
	cfg.DiscoveryPrefix = trimTopic(fallback(strings.TrimSpace(cfg.DiscoveryPrefix), "homeassistant"))
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Publisher{
		cfg:             cfg,
		log:             log,
		discovered:      make(map[string]string),
		publishedStates: make(map[string]string),
		accountPlans:    make(map[string]accountPlan),
		zonePlans:       make(map[string]zonePlan),
		subscriptions:   make(map[string]MessageHandler),
	}
}

func (p *Publisher) Enabled() bool {
	return p != nil && p.cfg.Broker != ""
}

func (p *Publisher) Connect(ctx context.Context) error {
	token := p.connect()
	if token == nil {
		return nil
	}
	return p.wait(ctx, token)
}

func (p *Publisher) ConnectAsync(ctx context.Context) {
	token := p.connect()
	if token == nil {
		return
	}
	go p.observeConnect(ctx, token)
}

func (p *Publisher) connect() paho.Token {
	if !p.Enabled() {
		return nil
	}

	opts := paho.NewClientOptions()
	opts.AddBroker(p.cfg.Broker)
	opts.SetClientID(p.cfg.ClientID)
	opts.SetUsername(p.cfg.Username)
	opts.SetPassword(p.cfg.Password)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetCleanSession(true)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetWill(p.availabilityTopic(), payloadOffline, 1, true)
	opts.OnConnect = func(client paho.Client) {
		p.resetCaches()
		token := client.Publish(p.availabilityTopic(), 1, true, payloadOnline)
		token.WaitTimeout(p.cfg.Timeout)
		p.resubscribe(client)
		p.log.Info().Str("broker", p.cfg.Broker).Msg("MQTT connected")
		p.notifyConnected()
	}
	opts.OnConnectionLost = func(_ paho.Client, err error) {
		p.log.Warn().Err(err).Str("broker", p.cfg.Broker).Msg("MQTT connection lost")
	}

	p.client = paho.NewClient(opts)
	return p.client.Connect()
}

func (p *Publisher) observeConnect(ctx context.Context, token paho.Token) {
	err := p.wait(ctx, token)
	switch {
	case err == nil:
		return
	case errors.Is(err, context.Canceled):
		return
	default:
		p.log.Warn().Err(err).Str("broker", p.cfg.Broker).Msg("MQTT initial connect pending; continuing startup")
	}
}

func (p *Publisher) Close() {
	if !p.Enabled() || p.client == nil {
		return
	}
	if p.client.IsConnected() {
		_ = p.publish(context.Background(), p.availabilityTopic(), payloadOffline, true)
	}
	p.client.Disconnect(250)
}

func (p *Publisher) PublishSnapshot(ctx context.Context, snapshot state.Snapshot) error {
	return p.PublishUpdate(ctx, Update{
		Accounts: snapshot.Accounts,
		Zones:    snapshot.Zones,
	})
}

func (p *Publisher) PublishUpdate(ctx context.Context, update Update) error {
	if !p.Enabled() || p.client == nil {
		return nil
	}
	if token := ctx.Err(); token != nil {
		return token
	}
	if !p.client.IsConnectionOpen() {
		return errors.New("MQTT client is not connected")
	}

	var errs []error
	for _, account := range update.Accounts {
		if err := p.publishAccount(ctx, account); err != nil {
			errs = append(errs, err)
		}
	}

	for _, zone := range update.Zones {
		if err := p.publishZone(ctx, zone); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (p *Publisher) Subscribe(ctx context.Context, topic string, handler MessageHandler) error {
	if !p.Enabled() || p.client == nil {
		return nil
	}
	topic = strings.TrimSpace(topic)
	if topic == "" || handler == nil {
		return nil
	}

	p.mu.Lock()
	p.subscriptions[topic] = handler
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.client.IsConnectionOpen() {
		return nil
	}
	return p.wait(ctx, p.client.Subscribe(topic, 1, wrapMessageHandler(handler)))
}

func (p *Publisher) AddConnectHandler(handler func(context.Context)) {
	if p == nil || handler == nil {
		return
	}
	p.mu.Lock()
	p.connectHandlers = append(p.connectHandlers, handler)
	connected := p.client != nil && p.client.IsConnectionOpen()
	p.mu.Unlock()
	if connected {
		go handler(context.Background())
	}
}

func (p *Publisher) PublishStateMessage(ctx context.Context, topic string, payload []byte, retain bool) error {
	if !p.Enabled() || p.client == nil {
		return nil
	}
	return p.publishState(ctx, topic, payload, retain)
}

func (p *Publisher) PublishDiscoveryMessage(ctx context.Context, key, topic string, payload []byte, retain bool) error {
	if !p.Enabled() || p.client == nil {
		return nil
	}
	if retain {
		return p.publishDiscoveryMessage(ctx, discoveryMessage{key: key, topic: topic, payload: payload})
	}
	return p.publish(ctx, topic, payload, false)
}

func (p *Publisher) PublishCommandMessage(ctx context.Context, topic string, payload []byte) error {
	if !p.Enabled() || p.client == nil {
		return nil
	}
	return p.publish(ctx, topic, payload, false)
}

func (p *Publisher) AvailabilityTopic() string {
	if p == nil {
		return ""
	}
	return p.availabilityTopic()
}

func (p *Publisher) publishAccount(ctx context.Context, account state.Account) error {
	plan, err := p.accountPlanFor(account)
	if err != nil {
		return err
	}
	if p.cfg.Discovery {
		for _, message := range plan.cleanup {
			if err := p.publishDiscoveryMessage(ctx, message); err != nil {
				return err
			}
		}
		for _, message := range plan.discovery {
			if err := p.publishDiscoveryMessage(ctx, message); err != nil {
				return err
			}
		}
	}
	currentState := accountState(account)
	currentState.SourceID = p.cfg.SourceID
	payload, err := json.Marshal(currentState)
	if err != nil {
		return err
	}
	if err := p.publishState(ctx, plan.stateTopic, payload, p.cfg.Retain); err != nil {
		return err
	}
	currentAttributes := accountAttributes(account)
	currentAttributes.SourceID = p.cfg.SourceID
	attributes, err := json.Marshal(currentAttributes)
	if err != nil {
		return err
	}
	return p.publishState(ctx, plan.attributesTopic, attributes, true)
}

func (p *Publisher) publishZone(ctx context.Context, zone state.Zone) error {
	plan, err := p.zonePlanFor(zone)
	if err != nil {
		return err
	}
	if p.cfg.Discovery {
		for _, message := range plan.cleanup {
			if err := p.publishDiscoveryMessage(ctx, message); err != nil {
				return err
			}
		}
		for _, message := range plan.discovery {
			if err := p.publishDiscoveryMessage(ctx, message); err != nil {
				return err
			}
		}
	}
	currentState := zoneState(zone)
	currentState.SourceID = p.cfg.SourceID
	payload, err := json.Marshal(currentState)
	if err != nil {
		return err
	}
	if err := p.publishState(ctx, plan.stateTopic, payload, p.cfg.Retain); err != nil {
		return err
	}
	currentAttributes := zoneAttributes(zone)
	currentAttributes.SourceID = p.cfg.SourceID
	attributes, err := json.Marshal(currentAttributes)
	if err != nil {
		return err
	}
	return p.publishState(ctx, plan.attributesTopic, attributes, true)
}

func (p *Publisher) publishDiscoveryMessage(ctx context.Context, message discoveryMessage) error {
	if !p.markDiscoveredPending(message.topic, message.payload) {
		return nil
	}
	if err := p.publish(ctx, message.topic, message.payload, true); err != nil {
		p.clearDiscovered(message.topic)
		return err
	}
	return nil
}

func (p *Publisher) publishState(ctx context.Context, topic string, payload []byte, retain bool) error {
	if !p.shouldPublishState(topic, payload) {
		return nil
	}
	if err := p.publish(ctx, topic, payload, retain); err != nil {
		p.clearPublishedState(topic)
		return err
	}
	return nil
}

func (p *Publisher) publish(ctx context.Context, topic string, payload interface{}, retain bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.client == nil || !p.client.IsConnectionOpen() {
		return fmt.Errorf("MQTT publish %s: client is not connected", topic)
	}
	return p.wait(ctx, p.client.Publish(topic, 1, retain, payload))
}

func (p *Publisher) wait(ctx context.Context, token paho.Token) error {
	timer := time.NewTimer(p.cfg.Timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("MQTT operation timed out after %s", p.cfg.Timeout)
	case <-token.Done():
		return token.Error()
	}
}

func (p *Publisher) resetCaches() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.discovered = make(map[string]string)
	p.publishedStates = make(map[string]string)
}

func (p *Publisher) resubscribe(client paho.Client) {
	p.mu.Lock()
	subscriptions := make(map[string]MessageHandler, len(p.subscriptions))
	for topic, handler := range p.subscriptions {
		subscriptions[topic] = handler
	}
	p.mu.Unlock()

	for topic, handler := range subscriptions {
		token := client.Subscribe(topic, 1, wrapMessageHandler(handler))
		if ok := token.WaitTimeout(p.cfg.Timeout); !ok {
			p.log.Warn().Str("topic", topic).Msg("MQTT subscription timed out")
			continue
		}
		if err := token.Error(); err != nil {
			p.log.Warn().Err(err).Str("topic", topic).Msg("MQTT subscription failed")
		}
	}
}

func (p *Publisher) notifyConnected() {
	p.mu.Lock()
	handlers := append([]func(context.Context){}, p.connectHandlers...)
	p.mu.Unlock()
	for _, handler := range handlers {
		go handler(context.Background())
	}
}

func (p *Publisher) markDiscoveredPending(topic string, payload []byte) bool {
	payloadValue := string(payload)
	p.mu.Lock()
	defer p.mu.Unlock()
	if cached, ok := p.discovered[topic]; ok && cached == payloadValue {
		return false
	}
	p.discovered[topic] = payloadValue
	return true
}

func (p *Publisher) clearDiscovered(topic string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.discovered, topic)
}

func (p *Publisher) shouldPublishState(topic string, payload []byte) bool {
	value := string(payload)
	p.mu.Lock()
	defer p.mu.Unlock()
	if cached, ok := p.publishedStates[topic]; ok && cached == value {
		return false
	}
	p.publishedStates[topic] = value
	return true
}

func (p *Publisher) clearPublishedState(topic string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.publishedStates, topic)
}

func (p *Publisher) accountPlanFor(account state.Account) (accountPlan, error) {
	key := account.Account
	p.mu.Lock()
	plan, ok := p.accountPlans[key]
	p.mu.Unlock()
	if ok {
		return plan, nil
	}

	discovery, err := p.buildDiscoveryMessages(
		accountEntities(account.Account),
		p.accountStateTopic(account.Account),
		p.accountAttributesTopic(account.Account),
		accountDevice(account),
		p.discoveryNode(),
	)
	if err != nil {
		return accountPlan{}, err
	}
	plan = accountPlan{
		stateTopic:      p.accountStateTopic(account.Account),
		attributesTopic: p.accountAttributesTopic(account.Account),
		cleanup:         p.legacyCleanupMessages(accountEntities(account.Account)),
		discovery:       discovery,
	}

	p.mu.Lock()
	if cached, ok := p.accountPlans[key]; ok {
		p.mu.Unlock()
		return cached, nil
	}
	p.accountPlans[key] = plan
	p.mu.Unlock()
	return plan, nil
}

func (p *Publisher) zonePlanFor(zone state.Zone) (zonePlan, error) {
	key := zonePlanKey(zone.Account, zone.Zone)
	signature := zoneDiscoverySignature(zone)

	p.mu.Lock()
	plan, ok := p.zonePlans[key]
	p.mu.Unlock()
	if ok && plan.signature == signature {
		return plan, nil
	}

	entities := zoneEntities(zone)
	discovery, err := p.buildDiscoveryMessages(
		entities,
		p.zoneStateTopic(zone.Account, zone.Zone),
		p.zoneAttributesTopic(zone.Account, zone.Zone),
		zoneDevice(zone),
		p.discoveryNode(),
	)
	if err != nil {
		return zonePlan{}, err
	}
	plan = zonePlan{
		signature:       signature,
		stateTopic:      p.zoneStateTopic(zone.Account, zone.Zone),
		attributesTopic: p.zoneAttributesTopic(zone.Account, zone.Zone),
		cleanup:         p.zoneCleanupMessages(zone, entities),
		discovery:       discovery,
	}

	p.mu.Lock()
	cached, ok := p.zonePlans[key]
	if ok && cached.signature == signature {
		p.mu.Unlock()
		return cached, nil
	}
	p.zonePlans[key] = plan
	p.mu.Unlock()
	return plan, nil
}

func (p *Publisher) buildDiscoveryMessages(entities []entity, stateTopic, attributesTopic string, device deviceInfo, node string) ([]discoveryMessage, error) {
	device.Identifiers = append([]string(nil), device.Identifiers...)
	for index, identifier := range device.Identifiers {
		device.Identifiers[index] = p.sourceIdentity(identifier)
	}
	messages := make([]discoveryMessage, 0, len(entities))
	for _, ent := range entities {
		cfg := discoveryConfig{
			Name:                ent.Name,
			UniqueID:            p.uniqueID(ent.ObjectID),
			StateTopic:          stateTopic,
			ValueTemplate:       ent.ValueTemplate,
			AvailabilityTopic:   p.availabilityTopic(),
			PayloadAvailable:    payloadOnline,
			PayloadNotAvailable: payloadOffline,
			DeviceClass:         ent.DeviceClass,
			EntityCategory:      ent.EntityCategory,
			Icon:                ent.Icon,
			JSONAttributesTopic: attributesTopic,
			Device:              device,
		}
		if ent.Binary {
			cfg.PayloadOn = payloadOn
			cfg.PayloadOff = payloadOff
		}

		payload, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		messages = append(messages, discoveryMessage{
			key:     "discover:" + slug(node) + ":" + ent.Component + "/" + ent.ObjectID,
			topic:   p.discoveryTopic(ent.Component, node, ent.ObjectID),
			payload: payload,
		})
	}
	return messages, nil
}

func (p *Publisher) legacyCleanupMessages(entities []entity) []discoveryMessage {
	if p.cfg.SourceID != "" {
		return nil
	}
	currentNode := slug(p.discoveryNode())
	messages := make([]discoveryMessage, 0, len(entities)*len(legacyDiscoveryNodes))
	for _, legacyNode := range legacyDiscoveryNodes {
		if slug(legacyNode) == currentNode {
			continue
		}
		for _, ent := range entities {
			messages = append(messages, discoveryMessage{
				key:     "cleanup:" + slug(legacyNode) + ":" + ent.Component + "/" + ent.ObjectID,
				topic:   p.discoveryTopic(ent.Component, legacyNode, ent.ObjectID),
				payload: []byte{},
			})
		}
	}
	return messages
}

func (p *Publisher) zoneCleanupMessages(zone state.Zone, entities []entity) []discoveryMessage {
	messages := make([]discoveryMessage, 0, len(entities)*len(legacyDiscoveryNodes))
	messages = append(messages, p.legacyCleanupMessages(entities)...)
	messages = append(messages, p.legacySIAObjectCleanupMessages(zone, entities)...)
	messages = append(messages, p.renamedSignalCleanupMessages(zone)...)
	return dedupeDiscoveryMessages(messages)
}

func (p *Publisher) legacySIAObjectCleanupMessages(zone state.Zone, entities []entity) []discoveryMessage {
	if len(entities) == 0 {
		return nil
	}
	messages := make([]discoveryMessage, 0, len(entities)*4)
	nodes := p.cleanupDiscoveryNodes()
	base := "zone_" + zone.Account + "_" + zone.Zone + "_"
	for _, ent := range entities {
		suffix := strings.TrimPrefix(ent.ObjectID, base)
		if suffix == ent.ObjectID {
			continue
		}
		for _, objectID := range legacySIAObjectIDs(zone, suffix) {
			for _, node := range nodes {
				messages = append(messages, discoveryMessage{
					key:     "cleanup:legacy_sia_object:" + node + ":" + ent.Component + "/" + objectID,
					topic:   p.discoveryTopic(ent.Component, node, objectID),
					payload: []byte{},
				})
			}
		}
	}
	return messages
}

func cleanupDiscoveryNodes(currentNode string) []string {
	seen := make(map[string]struct{}, len(legacyDiscoveryNodes)+1)
	nodes := make([]string, 0, len(legacyDiscoveryNodes)+1)
	for _, node := range append([]string{currentNode}, legacyDiscoveryNodes...) {
		node = slug(node)
		if node == "" || node == "unknown" {
			continue
		}
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		nodes = append(nodes, node)
	}
	return nodes
}

func (p *Publisher) cleanupDiscoveryNodes() []string {
	if p.cfg.SourceID != "" {
		return []string{slug(p.discoveryNode())}
	}
	return cleanupDiscoveryNodes(p.discoveryNode())
}

func legacySIAObjectIDs(zone state.Zone, suffix string) []string {
	base := "zone_" + zone.Account + "_" + zone.Zone + "_"
	candidates := []string{
		"zone_" + zone.Zone + "_" + suffix,
		"zone_" + zone.Account + "_" + suffix,
	}
	if strings.HasPrefix(suffix, "signal_") {
		signal := strings.TrimPrefix(suffix, "signal_")
		candidates = append(candidates,
			base+signal,
			"zone_"+zone.Zone+"_"+signal,
			"zone_"+zone.Zone+"_signal_"+signal,
			"zone_"+zone.Account+"_"+signal,
			"zone_"+zone.Account+"_signal_"+signal,
		)
	}
	return uniqueObjectIDs(candidates)
}

func uniqueObjectIDs(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func dedupeDiscoveryMessages(messages []discoveryMessage) []discoveryMessage {
	seen := make(map[string]struct{}, len(messages))
	out := messages[:0]
	for _, message := range messages {
		if message.topic == "" {
			continue
		}
		if _, ok := seen[message.topic]; ok {
			continue
		}
		seen[message.topic] = struct{}{}
		out = append(out, message)
	}
	return out
}

func (p *Publisher) renamedSignalCleanupMessages(zone state.Zone) []discoveryMessage {
	base := "zone_" + zone.Account + "_" + zone.Zone + "_"
	signals := sortedSignals(zone.DeviceEvents, zone.SignalActive)
	messages := make([]discoveryMessage, 0, len(signals))
	nodes := p.cleanupDiscoveryNodes()
	for _, signal := range signals {
		for _, suffix := range legacySignalObjectSuffixes(signal) {
			objectID := base + suffix
			for _, node := range nodes {
				messages = append(messages, discoveryMessage{
					key:     "cleanup:renamed_signal:" + node + ":binary_sensor/" + objectID,
					topic:   p.discoveryTopic("binary_sensor", node, objectID),
					payload: []byte{},
				})
			}
		}
	}
	return messages
}

func (p *Publisher) availabilityTopic() string {
	return p.cfg.TopicPrefix + "/status"
}

func (p *Publisher) accountStateTopic(account string) string {
	return p.cfg.TopicPrefix + "/accounts/" + topicPart(account) + "/state"
}

func (p *Publisher) accountAttributesTopic(account string) string {
	return p.cfg.TopicPrefix + "/accounts/" + topicPart(account) + "/attributes"
}

func (p *Publisher) zoneStateTopic(account, zone string) string {
	return p.cfg.TopicPrefix + "/accounts/" + topicPart(account) + "/zones/" + topicPart(zone) + "/state"
}

func (p *Publisher) zoneAttributesTopic(account, zone string) string {
	return p.cfg.TopicPrefix + "/accounts/" + topicPart(account) + "/zones/" + topicPart(zone) + "/attributes"
}

func (p *Publisher) discoveryTopic(component, node, objectID string) string {
	return strings.Join([]string{
		p.cfg.DiscoveryPrefix,
		component,
		slug(node),
		p.sourceObjectID(slug(objectID)),
		"config",
	}, "/")
}

func (p *Publisher) uniqueID(objectID string) string {
	if p.cfg.SourceID != "" {
		// Source is the stable identity namespace; changing an MQTT routing
		// prefix must not create a new Home Assistant entity for this source.
		return p.sourceIdentity(slug("ajaxbridge_" + objectID))
	}
	return p.sourceIdentity(slug(p.discoveryNode() + "_" + objectID))
}

func (p *Publisher) sourceIdentity(identity string) string {
	if p.cfg.SourceID == "" {
		return identity
	}
	return p.cfg.SourceID + ":" + identity
}

func (p *Publisher) sourceObjectID(objectID string) string {
	if p.cfg.SourceID == "" {
		return objectID
	}
	return p.cfg.SourceID + "_" + objectID
}

func (p *Publisher) discoveryNode() string {
	if strings.TrimSpace(p.cfg.TopicPrefix) != "" {
		return p.cfg.TopicPrefix
	}
	return p.cfg.ClientID
}

func accountDevice(account state.Account) deviceInfo {
	name := "Ajax account " + account.Account
	return deviceInfo{
		Identifiers:  []string{"ajaxbridge_account_" + account.Account},
		Name:         name,
		Manufacturer: "Ajax Systems",
		Model:        "Ajax account",
	}
}

func zoneDevice(zone state.Zone) deviceInfo {
	name := fallback(zone.DeviceName, "Ajax zone "+zone.Zone)
	model := fallback(zone.Kind, "Ajax device")
	device := deviceInfo{
		Identifiers:  []string{"ajaxbridge_" + zone.Account + "_zone_" + zone.Zone},
		Name:         name,
		Manufacturer: "Ajax Systems",
		Model:        model,
	}
	if zone.Room != "" && zone.Room != "unknown" {
		device.SuggestedArea = zone.Room
	}
	return device
}

func accountEntities(account string) []entity {
	base := "account_" + account + "_"
	return []entity{
		binaryEntity(base+"online", "Online", "connectivity", "", "{{ '"+payloadOn+"' if value_json.online else '"+payloadOff+"' }}"),
		binaryEntity(base+"armed", "Armed", "", "mdi:shield-lock", "{{ '"+payloadOn+"' if value_json.armed else '"+payloadOff+"' }}"),
		binaryEntity(base+"night_mode", "Night mode", "", "mdi:weather-night", "{{ '"+payloadOn+"' if value_json.night_mode else '"+payloadOff+"' }}"),
		binaryEntity(base+"partially_armed", "Partially armed", "", "mdi:shield-half-full", "{{ '"+payloadOn+"' if value_json.partially_armed else '"+payloadOff+"' }}"),
		binaryEntity(base+"alarm_active", "Alarm active", "safety", "", "{{ '"+payloadOn+"' if value_json.alarm_active else '"+payloadOff+"' }}"),
		binaryEntity(base+"tamper_active", "Tamper active", "tamper", "", "{{ '"+payloadOn+"' if value_json.tamper_active else '"+payloadOff+"' }}"),
		binaryEntity(base+"trouble_active", "Trouble active", "problem", "", "{{ '"+payloadOn+"' if value_json.trouble_active else '"+payloadOff+"' }}"),
		sensorEntity(base+"mode", "Mode", "", "mdi:shield-home", "{{ value_json.mode }}"),
		sensorEntity(base+"last_event_name", "Last event", "", "mdi:message-alert", "{{ value_json.last_event_name }}"),
		sensorEntity(base+"last_event_code", "Last event code", "", "mdi:identifier", "{{ value_json.last_event_code }}"),
		sensorEntity(base+"last_signal", "Last signal", "", "mdi:signal", "{{ value_json.last_signal }}"),
		sensorEntity(base+"last_event_at", "Last event time", "timestamp", "", "{{ value_json.last_event_at }}"),
		sensorEntity(base+"last_ping_at", "Last ping time", "timestamp", "", "{{ value_json.last_ping_at }}"),
	}
}

func zoneEntities(zone state.Zone) []entity {
	base := "zone_" + zone.Account + "_" + zone.Zone + "_"
	entities := []entity{
		binaryEntity(base+"alarm_active", "Alarm active", "safety", "", "{{ '"+payloadOn+"' if value_json.alarm_active else '"+payloadOff+"' }}"),
		binaryEntity(base+"tamper_active", "Tamper active", "tamper", "", "{{ '"+payloadOn+"' if value_json.tamper_active else '"+payloadOff+"' }}"),
		binaryEntity(base+"trouble_active", "Trouble active", "problem", "", "{{ '"+payloadOn+"' if value_json.trouble_active else '"+payloadOff+"' }}"),
		sensorEntity(base+"last_event_name", "Last event", "", "mdi:message-alert", "{{ value_json.last_event_name }}"),
		sensorEntity(base+"last_event_code", "Last event code", "", "mdi:identifier", "{{ value_json.last_event_code }}"),
		sensorEntity(base+"last_signal", "Last signal", "", "mdi:signal", "{{ value_json.last_signal }}"),
		sensorEntity(base+"last_event_at", "Last event time", "timestamp", "", "{{ value_json.last_event_at }}"),
		sensorEntity(base+"alarm_signal", "Alarm signal", "", "mdi:alarm-light", "{{ value_json.alarm_signal }}"),
		sensorEntity(base+"alarm_action", "Alarm action", "", "mdi:alarm-light-outline", "{{ value_json.alarm_action }}"),
	}
	for _, signal := range sortedSignals(zone.DeviceEvents, zone.SignalActive) {
		entities = append(entities, signalEntity(base, signal))
	}
	return entities
}

func signalEntity(base, signal string) entity {
	deviceClass, icon := signalPresentation(signal)
	name := signalName(signal)
	return binaryEntity(
		base+signalObjectSuffix(signal),
		name,
		deviceClass,
		icon,
		"{{ '"+payloadOn+"' if value_json.signal_active.get('"+signal+"', false) else '"+payloadOff+"' }}",
	)
}

func signalObjectSuffix(signal string) string {
	switch signal {
	case "power":
		return "signal_power_failure"
	case "temperature":
		return "signal_temperature_alarm"
	default:
		return "signal_" + signal
	}
}

func legacySignalObjectSuffixes(signal string) []string {
	switch signal {
	case "power":
		return []string{"signal_power", "power"}
	case "temperature":
		return []string{"signal_temperature", "temperature"}
	default:
		return nil
	}
}

func binaryEntity(objectID, name, deviceClass, icon, template string) entity {
	return entity{
		Component:     "binary_sensor",
		ObjectID:      objectID,
		Name:          name,
		ValueTemplate: template,
		DeviceClass:   deviceClass,
		Icon:          icon,
		Binary:        true,
	}
}

func sensorEntity(objectID, name, deviceClass, icon, template string) entity {
	return entity{
		Component:     "sensor",
		ObjectID:      objectID,
		Name:          name,
		ValueTemplate: template,
		DeviceClass:   deviceClass,
		Icon:          icon,
	}
}

func zoneState(zone state.Zone) zonePayload {
	signals := make(map[string]bool, len(zone.DeviceEvents)+len(zone.SignalActive))
	for _, signal := range zone.DeviceEvents {
		if signal != "" {
			signals[signal] = false
		}
	}
	for signal, active := range zone.SignalActive {
		if signal != "" {
			signals[signal] = active
		}
	}
	return zonePayload{
		Account:          zone.Account,
		Partition:        zone.Partition,
		Group:            zone.Group,
		Zone:             zone.Zone,
		Device:           fallback(zone.Device, zone.Zone),
		DeviceName:       zone.DeviceName,
		Room:             zone.Room,
		Kind:             zone.Kind,
		DeviceEvents:     append([]string(nil), zone.DeviceEvents...),
		AlarmActive:      zone.AlarmActive,
		AlarmSignal:      fallback(zone.AlarmSignal, "none"),
		AlarmAction:      fallback(zone.AlarmAction, "none"),
		AlarmEventCode:   zone.AlarmEventCode,
		AlarmEventName:   zone.AlarmEventName,
		TamperActive:     zone.TamperActive,
		TroubleActive:    zone.TroubleActive,
		SignalActive:     signals,
		LastEventCode:    zone.LastEventCode,
		LastEventName:    zone.LastEventName,
		LastSignal:       zone.LastSignal,
		LastEventAt:      mqttTime(zone.LastEventAt),
		LastEventUnix:    unixTime(zone.LastEventAt),
		AlarmStartedAt:   mqttTime(zone.AlarmStartedAt),
		AlarmStartedUnix: unixTime(zone.AlarmStartedAt),
	}
}

func accountState(account state.Account) accountPayload {
	return accountPayload{
		Account:        account.Account,
		Online:         account.Online,
		Mode:           account.Mode,
		Armed:          account.Armed,
		NightMode:      account.NightMode,
		PartiallyArmed: account.PartiallyArmed,
		AlarmActive:    account.AlarmActive,
		TamperActive:   account.TamperActive,
		TroubleActive:  account.TroubleActive,
		LastEventCode:  account.LastEventCode,
		LastEventName:  account.LastEventName,
		LastSignal:     account.LastSignal,
		LastEventAt:    mqttTime(account.LastEventAt),
		LastEventUnix:  unixTime(account.LastEventAt),
		LastPingAt:     mqttTime(account.LastPingAt),
		LastPingUnix:   unixTime(account.LastPingAt),
	}
}

func accountAttributes(account state.Account) accountAttributesPayload {
	return accountAttributesPayload{Account: account.Account}
}

func zoneAttributes(zone state.Zone) zoneAttributesPayload {
	return zoneAttributesPayload{
		Account:      zone.Account,
		Partition:    zone.Partition,
		Group:        zone.Group,
		Zone:         zone.Zone,
		Device:       fallback(zone.Device, zone.Zone),
		DeviceName:   zone.DeviceName,
		Room:         zone.Room,
		Kind:         zone.Kind,
		DeviceEvents: append([]string(nil), zone.DeviceEvents...),
	}
}

func sortedSignals(events []string, active map[string]bool) []string {
	seen := make(map[string]struct{}, len(events)+len(active))
	for _, signal := range events {
		if signal != "" {
			seen[signal] = struct{}{}
		}
	}
	for signal := range active {
		if signal != "" {
			seen[signal] = struct{}{}
		}
	}
	signals := make([]string, 0, len(seen))
	for signal := range seen {
		signals = append(signals, signal)
	}
	sort.Strings(signals)
	return signals
}

func signalPresentation(signal string) (string, string) {
	switch signal {
	case "battery":
		return "battery", ""
	case "co", "gas", "gas_or_co":
		return "gas", ""
	case "connectivity", "hardware", "fire_detector", "interference", "accelerometer", "bypass", "tamper_bypass":
		return "problem", ""
	case "duress", "emergency", "fire", "medical", "panic", "temperature":
		return "safety", ""
	case "power":
		return "problem", "mdi:power-plug-off"
	case "smoke":
		return "smoke", ""
	case "tamper":
		return "tamper", ""
	case "water_leak":
		return "moisture", ""
	case "arming":
		return "", "mdi:shield-lock"
	case "burglary":
		return "safety", "mdi:shield-alert"
	case "configuration":
		return "", "mdi:cog"
	case "firmware":
		return "", "mdi:update"
	case "night_mode":
		return "", "mdi:weather-night"
	default:
		return "problem", ""
	}
}

func signalName(signal string) string {
	switch signal {
	case "battery":
		return "Battery low"
	case "burglary":
		return "Burglary"
	case "bypass":
		return "Bypassed"
	case "co":
		return "Carbon monoxide"
	case "connectivity":
		return "Connection lost"
	case "fire_detector":
		return "Fire detector fault"
	case "gas_or_co":
		return "Gas or CO"
	case "hardware":
		return "Hardware fault"
	case "night_mode":
		return "Night mode"
	case "power":
		return "Power failure"
	case "temperature":
		return "Temperature alarm"
	case "tamper_bypass":
		return "Tamper bypassed"
	case "water_leak":
		return "Water leak"
	default:
		return titleLabel(signal)
	}
}

func titleLabel(value string) string {
	words := strings.Fields(strings.ReplaceAll(value, "_", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func mqttTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func unixTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func topicPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.NewReplacer("#", "_", "+", "_", " ", "_").Replace(value)
}

func zonePlanKey(account, zone string) string {
	return account + "/" + zone
}

func zoneDiscoverySignature(zone state.Zone) string {
	signals := sortedSignals(zone.DeviceEvents, zone.SignalActive)
	var b strings.Builder
	b.Grow(len(zone.Account) + len(zone.Zone) + len(zone.DeviceName) + len(zone.Room) + len(zone.Kind) + len(signals)*16 + 8)
	b.WriteString(zone.Account)
	b.WriteByte('|')
	b.WriteString(zone.Zone)
	b.WriteByte('|')
	b.WriteString(zone.DeviceName)
	b.WriteByte('|')
	b.WriteString(zone.Room)
	b.WriteByte('|')
	b.WriteString(zone.Kind)
	for _, signal := range signals {
		b.WriteByte('|')
		b.WriteString(signal)
	}
	return b.String()
}

func trimTopic(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func wrapMessageHandler(handler MessageHandler) paho.MessageHandler {
	return func(_ paho.Client, message paho.Message) {
		handler(message.Topic(), append([]byte(nil), message.Payload()...))
	}
}
