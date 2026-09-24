package jeedom

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type Subscriber interface {
	Subscribe(ctx context.Context, topic string, handler func(topic string, payload []byte)) error
}

type MetricsRecorder interface {
	ObserveJeedomMessage()
	ObserveJeedomParseError()
	ObserveJeedomEmptyValue()
	ObserveJeedomCommand(device, command, commandID, metric string, value float64, lastUpdate time.Time)
}

type UpdateObserver interface {
	ObserveJeedomUpdate(ctx context.Context, result ApplyResult)
	ObserveJeedomControl(ctx context.Context, result ControlResult, err error)
}

type ServiceConfig struct {
	EventTopic     string
	DiscoveryTopic string
	SetTopicPrefix string
}

type Service struct {
	cfg        ServiceConfig
	store      *Store
	subscriber Subscriber
	publisher  *Publisher
	controller *Controller
	metrics    MetricsRecorder
	samples    *SampleWriter
	log        zerolog.Logger
	observer   UpdateObserver
}

func NewService(cfg ServiceConfig, store *Store, subscriber Subscriber, publisher *Publisher, metrics MetricsRecorder, samples *SampleWriter, log zerolog.Logger) *Service {
	cfg.EventTopic = firstNonEmpty(cfg.EventTopic, "jeedom/cmd/event/#")
	cfg.DiscoveryTopic = firstNonEmpty(cfg.DiscoveryTopic, "jeedom/discovery/eqLogic/#")
	cfg.SetTopicPrefix = trimTopic(firstNonEmpty(cfg.SetTopicPrefix, "jeedom/cmd/set"))
	return &Service{
		cfg:        cfg,
		store:      store,
		subscriber: subscriber,
		publisher:  publisher,
		metrics:    metrics,
		samples:    samples,
		log:        log,
	}
}

func (s *Service) SetController(controller *Controller) {
	if s != nil {
		s.controller = controller
	}
}

func (s *Service) SetObserver(observer UpdateObserver) {
	if s != nil {
		s.observer = observer
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil || s.subscriber == nil {
		return nil
	}
	var topics []string
	topics = appendUniqueTopic(topics, s.cfg.EventTopic)
	topics = appendUniqueTopic(topics, s.cfg.DiscoveryTopic)
	if s.cfg.SetTopicPrefix != "" {
		topics = appendUniqueTopic(topics, s.cfg.SetTopicPrefix+"/#")
	}
	if s.controller != nil && s.controller.Enabled() {
		topics = appendUniqueTopic(topics, s.controller.CommandTopicPattern())
	}
	for _, topic := range topics {
		if err := s.subscriber.Subscribe(ctx, topic, func(topic string, payload []byte) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			go s.HandleMessage(ctx, topic, payload)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) HandleMessage(ctx context.Context, topic string, payload []byte) {
	if s == nil || s.store == nil {
		return
	}
	receivedAt := time.Now()
	if s.metrics != nil {
		s.metrics.ObserveJeedomMessage()
	}
	if err := s.samples.Write(topic, payload, receivedAt); err != nil {
		s.log.Warn().Err(err).Str("topic", topic).Msg("capture Jeedom MQTT sample")
	}
	if s.controller != nil && s.controller.IsCommandTopic(topic) {
		if _, err := s.controller.HandleMQTTCommand(ctx, topic, payload); err != nil {
			s.log.Warn().Err(err).Str("topic", topic).Msg("execute Jeedom MQTT control command")
		}
		return
	}
	if s.isSetTopic(topic) {
		s.handleSetTopic(ctx, topic)
		return
	}
	if IsDiscoveryEqLogicTopic(topic) {
		discovery, err := ParseDiscoveryMessage(topic, payload, receivedAt)
		if err != nil {
			if s.metrics != nil {
				s.metrics.ObserveJeedomParseError()
			}
			s.log.Warn().Err(err).Str("topic", topic).Msg("parse Jeedom MQTT discovery")
			return
		}
		result := s.store.ApplyDiscovery(discovery)
		s.persist(ctx, "persist Jeedom MQTT discovery")
		if s.publisher != nil {
			s.publishPreviousDevices(ctx, result.PreviousDevices)
			published, err := s.publisher.PublishDeviceWithResult(ctx, result.Device)
			if err != nil {
				s.log.Debug().Err(err).Str("device", result.Device.DeviceSlug).Msg("publish Jeedom MQTT discovery state")
			} else if published {
				s.acknowledgePublishedCleanups(ctx, result.Device)
			}
		}
		return
	}
	if !IsCommandEventTopic(topic) {
		s.log.Debug().Str("topic", topic).Msg("captured non-event Jeedom MQTT message")
		return
	}

	evt, err := ParseMessage(topic, payload, receivedAt)
	if err != nil {
		if s.metrics != nil {
			s.metrics.ObserveJeedomParseError()
		}
		s.log.Warn().Err(err).Str("topic", topic).Msg("parse Jeedom MQTT event")
		return
	}

	result := s.store.Apply(evt)
	if result.UpdatedValue || result.StoreChanged {
		s.persist(ctx, "persist Jeedom MQTT state")
	}
	if result.EmptyValue && s.metrics != nil {
		s.metrics.ObserveJeedomEmptyValue()
	}
	if result.HasNumeric && s.metrics != nil {
		s.metrics.ObserveJeedomCommand(result.Device.DeviceSlug, result.Command.Name, result.Command.CommandID, result.Mapping.Metric, result.NumericValue, evt.ReceivedAt)
	}
	if s.observer != nil {
		s.observer.ObserveJeedomUpdate(ctx, result)
	}

	if s.publisher != nil {
		s.publishPreviousDevices(ctx, result.PreviousDevices)
		published, err := s.publisher.PublishDeviceWithResult(ctx, result.Device)
		if err != nil {
			s.log.Debug().Err(err).Str("device", result.Device.DeviceSlug).Msg("publish Jeedom MQTT state")
		} else if published {
			s.acknowledgePublishedCleanups(ctx, result.Device)
		}
	}
}

func (s *Service) publishPreviousDevices(ctx context.Context, devices []Device) {
	for _, device := range devices {
		published, err := s.publisher.PublishDeviceWithResult(ctx, device)
		if err != nil {
			s.log.Debug().Err(err).Str("device", device.DeviceSlug).Msg("publish previous Jeedom command owner state")
			continue
		}
		if published {
			s.acknowledgePublishedCleanups(ctx, device)
		}
	}
}

func (s *Service) acknowledgePublishedCleanups(ctx context.Context, device Device) {
	if s == nil || s.store == nil || s.publisher == nil || !s.publisher.DiscoveryEnabled() {
		return
	}
	commandsAcknowledged := s.store.AcknowledgeCommandCleanups(device.PendingDiscoveryCleanups)
	actionsAcknowledged := s.store.AcknowledgeActionCleanups(device.PendingActionDiscoveryCleanups)
	if commandsAcknowledged || actionsAcknowledged {
		s.persist(ctx, "persist acknowledged Jeedom MQTT discovery cleanup")
	}
}

func (s *Service) persist(ctx context.Context, message string) {
	if s == nil || s.store == nil {
		return
	}
	if err := s.store.Save(ctx); err != nil {
		s.log.Warn().Err(err).Msg(message)
	}
}

func (s *Service) isSetTopic(topic string) bool {
	prefix := trimTopic(s.cfg.SetTopicPrefix)
	topic = trimTopic(topic)
	if prefix == "" || topic == "" {
		return false
	}
	if topic == prefix {
		return false
	}
	return strings.HasPrefix(topic, prefix+"/")
}

func (s *Service) handleSetTopic(ctx context.Context, topic string) {
	commandID, err := CommandIDFromTopic(topic)
	if err != nil {
		s.log.Debug().Err(err).Str("topic", topic).Msg("ignore Jeedom set topic without command id")
		return
	}
	action, ok := s.store.ActionByCommandID(commandID)
	if !ok {
		s.log.Debug().Str("topic", topic).Str("command_id", commandID).Msg("ignore Jeedom set topic for unknown action")
		return
	}
	if s.store.HasRecentBridgeControl(commandID, 2*time.Second) {
		s.log.Debug().Str("topic", topic).Str("command_id", commandID).Msg("ignore Jeedom set topic already observed through bridge control")
		return
	}
	result := ControlResult{
		DeviceSlug: action.DeviceSlug,
		Device:     action.Device,
		DeviceType: action.DeviceType,
		Action:     action.Action,
		CommandID:  action.CommandID,
		Topic:      topic,
		Published:  true,
	}
	if device, ok := s.store.Device(action.DeviceSlug); ok {
		result.DeviceType = firstNonEmpty(result.DeviceType, device.JeedomDeviceType, device.HAModel)
		result.Account = device.LinkedAccount
		result.Zone = device.LinkedZone
	}
	if _, updated := s.store.RecordOptimisticControlState(action, time.Now().UTC()); updated {
		result.StateUpdated = true
		s.persist(ctx, "persist external Jeedom control state")
	}
	s.store.RecordControl(action, "jeedom_mqtt_set:"+topic, topic, nil)
	if s.observer != nil {
		s.observer.ObserveJeedomControl(ctx, result, nil)
	}
}

func appendUniqueTopic(topics []string, topic string) []string {
	topic = trimTopic(topic)
	if topic == "" {
		return topics
	}
	for _, current := range topics {
		if current == topic {
			return topics
		}
	}
	return append(topics, topic)
}

func (s *Service) Store() *Store {
	if s == nil {
		return nil
	}
	return s.store
}
