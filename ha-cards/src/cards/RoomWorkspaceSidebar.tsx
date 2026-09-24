import { useMemo, useState } from 'react';
import type { Device, DeviceActionDomain, DeviceControlState, EventItem } from '../models/dashboard';
import type { HomeAssistant } from '../ha/types';
import { Icon } from '../components/Icon';
import { callEntityService } from '../ha/services';
import { getDeviceImageAsset, getToneClass } from '../utils/assets';
import { controlStateLabel, deviceControlAccessibility, selectSidebarMetrics } from '../utils/deviceSemantics';
import { formatEventStamp } from '../utils/format';

interface RoomWorkspaceSidebarProps {
  devices: Device[];
  events: EventItem[];
  selectedDeviceId: string | null;
  onSelectDevice: (deviceId: string | null) => void;
  hass?: HomeAssistant;
}

type SidebarTab = 'devices' | 'events';

const DAHUA_EVENT_TYPES = new Set(['human_detected', 'vehicle_detected', 'tripwire_detected', 'intrusion_detected']);
const AJAX_EVENT_TYPES = new Set(['alarm', 'armed', 'disarmed', 'turn_on', 'turn_off', 'impulse']);
export function RoomWorkspaceSidebar({
  devices,
  events,
  selectedDeviceId,
  onSelectDevice,
  hass,
}: RoomWorkspaceSidebarProps) {
  const [activeTab, setActiveTab] = useState<SidebarTab>('devices');
  const deviceById = useMemo(() => new Map(devices.map((device) => [device.id, device])), [devices]);
  const ajaxDevices = devices.filter((device) => device.type !== 'camera');
  const meaningfulEvents = events.filter((event) => isMeaningfulEvent(event, deviceById));

  return (
    <aside className="room-workspace-sidebar glass-panel">
      <div className="room-workspace-sidebar__tabs" role="tablist" aria-label="Room sidebar">
        <button type="button" className={activeTab === 'devices' ? 'is-active' : ''} onClick={() => setActiveTab('devices')}>
          Devices
        </button>
        <button type="button" className={activeTab === 'events' ? 'is-active' : ''} onClick={() => setActiveTab('events')}>
          Events
        </button>
      </div>
      {activeTab === 'devices' ? (
        <div className="room-workspace-sidebar__list">
          {ajaxDevices.length > 0 ? (
            ajaxDevices.map((device) => (
              <AjaxDeviceListItem
                key={device.id}
                device={device}
                selected={device.id === selectedDeviceId}
                onSelect={onSelectDevice}
                hass={hass}
              />
            ))
          ) : (
            <EmptySidebarState title="No Ajax devices" copy="Assign Ajax entities to this area to control them here." />
          )}
        </div>
      ) : (
        <div className="room-workspace-sidebar__list">
          {meaningfulEvents.length > 0 ? (
            meaningfulEvents.map((event) => <SidebarEvent key={event.id} event={event} />)
          ) : (
            <EmptySidebarState title="No meaningful events" copy="Alarm, arm/disarm, relay, and Dahua SMD/IVS events will appear here." />
          )}
        </div>
      )}
    </aside>
  );
}

interface AjaxDeviceListItemProps {
  device: Device;
  selected: boolean;
  onSelect: (deviceId: string | null) => void;
  hass?: HomeAssistant;
}

function AjaxDeviceListItem({ device, selected, onSelect, hass }: AjaxDeviceListItemProps) {
  const [pending, setPending] = useState(false);
  const metrics = selectSidebarMetrics(device.metrics ?? []);
  const toggleAction = getToggleAction(device);
  const impulseAction = getImpulseAction(device);
  const buttonActions = getButtonActions(device);
  const controlState = getDeviceControlState(device);
  const isOn = controlState === 'on';
  const isIndeterminate = controlState !== 'on' && controlState !== 'off';
  const toggleLabel = controlStateLabel(controlState, isWaterStopDevice(device));
  const toggleAccessibility = toggleAction
    ? deviceControlAccessibility({
      actionLabel: toggleAction.label,
      deviceName: device.name,
      state: controlState,
      stateLabel: toggleLabel,
    })
    : null;

  async function callAction(action: { domain: DeviceActionDomain; service: string; entityId: string }) {
    if (!hass?.callService) {
      return;
    }
    setPending(true);
    try {
      await callEntityService(hass, action.domain, action.service, action.entityId);
    } catch (error) {
      console.error('[ajaxbridge] Home Assistant action failed', { action, error });
    } finally {
      setPending(false);
    }
  }

  return (
    <article className={['ajax-device-item', getToneClass(device.tone), selected ? 'ajax-device-item--selected' : ''].join(' ')}>
      <button
        type="button"
        className="ajax-device-item__main"
        aria-label={`${selected ? 'Clear selection for' : 'Select'} ${device.name}`}
        onClick={() => onSelect(selected ? null : device.id)}
      >
        <span className="ajax-device-item__media">
          <img src={getDeviceImageAsset(device)} alt="" loading="lazy" />
        </span>
        <span className="ajax-device-item__copy">
          <strong>{device.name}</strong>
        </span>
        <span className="ajax-device-item__type-row">
          <span
            className={[
              'ajax-device-item__state-dot',
              device.isOnline ? 'ajax-device-item__state-dot--online' : 'ajax-device-item__state-dot--offline',
            ].join(' ')}
          />
          <span>{displayDeviceType(device)}</span>
        </span>
      </button>
      {metrics.length > 0 ? (
        <div className="ajax-device-item__metrics">
          {metrics.map((metric) => (
            <span key={metric.id} className={getToneClass(metric.tone)}>
              <Icon icon={metric.icon} size={16} />
              <small>{metric.label}</small>
              <strong>{metric.value}</strong>
            </span>
          ))}
        </div>
      ) : null}
      {toggleAction || impulseAction || buttonActions.length > 0 ? (
        <div className="ajax-device-item__controls">
          {toggleAction ? (
            <button
              type="button"
              className={[
                'ajax-device-item__toggle',
                isOn ? 'ajax-device-item__toggle--on' : '',
                isIndeterminate ? 'ajax-device-item__toggle--indeterminate' : '',
              ].join(' ')}
              role={toggleAccessibility?.role}
              aria-checked={toggleAccessibility?.ariaChecked}
              aria-label={toggleAccessibility?.label}
              title={toggleAccessibility?.label}
              disabled={pending || !hass?.callService}
              onClick={() => void callAction(toggleAction)}
            >
              <span className="ajax-device-item__toggle-track">
                <span className="ajax-device-item__toggle-thumb" />
              </span>
              <span>{toggleLabel}</span>
            </button>
          ) : null}
          {impulseAction ? (
            <button
              type="button"
              className="ajax-device-item__impulse"
              aria-label={`Send impulse to ${device.name}`}
              title="Impulse"
              disabled={pending || !hass?.callService}
              onClick={() => void callAction(impulseAction)}
            >
              <Icon icon={{ category: 'devices', key: 'relay' }} size={20} />
            </button>
          ) : null}
          {buttonActions.map((action) => (
            <button
              key={action.entityId}
              type="button"
              className={['ajax-device-item__action', actionClass(action.label)].join(' ')}
              disabled={pending || !hass?.callService}
              onClick={() => void callAction(action)}
            >
              <Icon icon={iconForAction(action.label)} size={18} />
              <span>{actionLabel(action.label)}</span>
            </button>
          ))}
        </div>
      ) : null}
    </article>
  );
}

function SidebarEvent({ event }: { event: EventItem }) {
  return (
    <article className={['sidebar-event', getToneClass(event.tone)].join(' ')}>
      <span className="sidebar-event__icon-wrap">
        <Icon icon={normalizeEventIcon(event)} size={30} />
      </span>
      <div className="sidebar-event__copy">
        <strong>{event.title}</strong>
        <span>{event.description}</span>
        <small>{event.source}</small>
      </div>
      <time dateTime={event.occurredAt}>{formatEventStamp(event.occurredAt)}</time>
    </article>
  );
}

function EmptySidebarState({ title, copy }: { title: string; copy: string }) {
  return (
    <div className="event-timeline__empty">
      <strong>{title}</strong>
      <span>{copy}</span>
    </div>
  );
}

function isMeaningfulEvent(event: EventItem, deviceById: Map<string, Device>): boolean {
  if (DAHUA_EVENT_TYPES.has(event.type)) {
    return true;
  }
  if (event.type === 'motion_detected' && event.deviceId && deviceById.get(event.deviceId)?.type === 'camera') {
    return true;
  }
  return AJAX_EVENT_TYPES.has(event.type);
}

function normalizeEventIcon(event: EventItem) {
  if (event.type === 'armed') {
    return { category: 'events' as const, key: 'armed_event' };
  }
  if (event.type === 'disarmed') {
    return { category: 'events' as const, key: 'disarmed_event' };
  }
  if (event.type === 'turn_on') {
    return { category: 'misc' as const, key: 'energy' };
  }
  if (event.type === 'turn_off') {
    return { category: 'system-states' as const, key: 'power_loss' };
  }
  if (event.type === 'impulse') {
    return { category: 'devices' as const, key: 'relay' };
  }
  return event.icon;
}

function getToggleAction(device: Device): {
  domain: 'switch' | 'valve';
  service: string;
  entityId: string;
  label: string;
  controlState?: DeviceControlState;
} | null {
  if (!isSwitchControlledDevice(device)) {
    return null;
  }
  const action = device.actions?.find((candidate) => candidate.domain === 'valve' || candidate.domain === 'switch');
  if (action) {
    return {
      domain: action.domain as 'switch' | 'valve',
      service: action.service,
      entityId: action.entityId,
      label: action.label,
      controlState: action.controlState,
    };
  }
  if (device.entityId.startsWith('valve.')) {
    const isOn = deviceLooksOn(device);
    return {
      domain: 'valve',
      service: isOn ? 'close_valve' : 'open_valve',
      entityId: device.entityId,
      label: isOn ? 'Close' : 'Open',
    };
  }
  if (device.entityId.startsWith('switch.')) {
    const isOn = deviceLooksOn(device);
    return {
      domain: 'switch',
      service: isOn ? 'turn_off' : 'turn_on',
      entityId: device.entityId,
      label: isOn ? 'Turn off' : 'Turn on',
    };
  }
  return null;
}

function getImpulseAction(device: Device): { domain: DeviceActionDomain; service: string; entityId: string } | null {
  if (!isImpulseRelay(device)) {
    return null;
  }
  const action = device.actions?.find((candidate) => candidate.domain === 'button') ?? device.actions?.[0];
  if (action) {
    return { domain: action.domain, service: action.service, entityId: action.entityId };
  }
  if (device.entityId.startsWith('switch.')) {
    return { domain: 'switch', service: 'turn_on', entityId: device.entityId };
  }
  return null;
}

function getButtonActions(device: Device): Array<{ domain: DeviceActionDomain; service: string; entityId: string; label: string }> {
  if (isImpulseRelay(device)) {
    return [];
  }
  return (device.actions ?? [])
    .filter((action) => action.domain === 'button')
    .map((action) => ({
      domain: action.domain,
      service: action.service,
      entityId: action.entityId,
      label: action.label,
    }))
    .sort((left, right) => actionOrder(left.label) - actionOrder(right.label) || left.label.localeCompare(right.label));
}

function isSwitchControlledDevice(device: Device): boolean {
  const text = deviceText(device);
  return !isImpulseRelay(device)
    && (device.type === 'smart_plug'
      || device.type === 'wall_switch'
      || device.type === 'light_switch'
      || device.type === 'waterstop'
      || /wallswitch|wall switch|lightswitch|light switch|waterstop|water stop|socket|plug|outlet/.test(text));
}

function isWaterStopDevice(device: Device): boolean {
  return device.type === 'waterstop' || /waterstop|water stop|\bvalve\b/.test(deviceText(device));
}

function actionOrder(label: string): number {
  const normalized = normalizedActionLabel(label);
  if (normalized.includes('arm') && !normalized.includes('disarm')) {
    return 0;
  }
  if (normalized.includes('night')) {
    return 1;
  }
  if (normalized.includes('disarm')) {
    return 2;
  }
  if (normalized.includes('mute') || normalized.includes('fire')) {
    return 3;
  }
  return 10;
}

function actionClass(label: string): string {
  const normalized = normalizedActionLabel(label);
  if (normalized.includes('disarm')) {
    return 'ajax-device-item__action--safe';
  }
  if (normalized.includes('night')) {
    return 'ajax-device-item__action--night';
  }
  if (normalized.includes('mute') || normalized.includes('fire')) {
    return 'ajax-device-item__action--alert';
  }
  if (normalized.includes('arm')) {
    return 'ajax-device-item__action--armed';
  }
  return '';
}

function iconForAction(label: string) {
  const normalized = normalizedActionLabel(label);
  if (normalized.includes('disarm')) {
    return { category: 'system-states' as const, key: 'disarmed' };
  }
  if (normalized.includes('night')) {
    return { category: 'system-states' as const, key: 'night_mode' };
  }
  if (normalized.includes('mute') || normalized.includes('fire')) {
    return { category: 'sensors' as const, key: 'fire' };
  }
  if (normalized.includes('arm')) {
    return { category: 'system-states' as const, key: 'armed' };
  }
  return { category: 'misc' as const, key: 'energy' };
}

function actionLabel(label: string): string {
  const normalized = normalizedActionLabel(label);
  if (normalized.includes('mute') || normalized.includes('fire')) {
    return 'Mute fire';
  }
  return label;
}

function normalizedActionLabel(label: string): string {
  return label.toLowerCase().replace(/[_-]+/g, ' ');
}

function isImpulseRelay(device: Device): boolean {
  const text = deviceText(device);
  return device.type === 'relay' && !/wallswitch|wall switch|lightswitch|light switch|waterstop|water stop/.test(text);
}

function deviceLooksOn(device: Device): boolean {
  return getDeviceControlState(device) === 'on';
}

function getDeviceControlState(device: Device): DeviceControlState {
  const toggleAction = device.actions?.find((action) => action.domain === 'switch' || action.domain === 'valve');
  if (toggleAction?.controlState) {
    return toggleAction.controlState;
  }
  if (device.actions?.some((action) => action.service === 'turn_off')) {
    return 'on';
  }
  if (device.actions?.some((action) => action.service === 'turn_on')) {
    return 'off';
  }
  if (device.actions?.some((action) => action.service === 'close_valve')) {
    return 'on';
  }
  if (device.actions?.some((action) => action.service === 'open_valve')) {
    return 'off';
  }
  const text = `${device.status} ${device.signal}`.toLowerCase();
  if (/\bon\b|\bopen\b|active|enabled|load|w\b/.test(text)) {
    return 'on';
  }
  if (/\boff\b|\bclosed\b|inactive|disabled/.test(text)) {
    return 'off';
  }
  return 'unknown';
}

function deviceText(device: Device): string {
  return `${device.type} ${device.name} ${device.model} ${device.entityId}`.toLowerCase();
}

function displayDeviceType(device: Device): string {
  return humanizeDeviceType(device.model || device.type);
}

function humanizeDeviceType(value: string): string {
  const compact = value.trim();
  if (!compact) {
    return 'Device';
  }
  return compact
    .replace(/_/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
