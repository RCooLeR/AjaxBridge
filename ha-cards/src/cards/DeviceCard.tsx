import type { Device, GlowTone } from '../models/dashboard';
import { getToneClass } from '../utils/assets';
import { Icon } from '../components/Icon';
import { StatusBadge } from '../components/StatusBadge';

interface DeviceCardProps {
  device: Device;
  eventStatusLabel: string;
  eventStatusTone: GlowTone;
  selected: boolean;
  onSelect: (deviceId: string | null) => void;
}

function getEventStatusClass(tone: GlowTone): string {
  if (tone === 'green') {
    return 'device-card__status-value--good';
  }

  if (tone === 'red') {
    return 'device-card__status-value--critical';
  }

  return 'device-card__status-value--alert';
}

export function DeviceCard({ device, eventStatusLabel, eventStatusTone, selected, onSelect }: DeviceCardProps) {
  const metrics = device.metrics ?? [];
  const visibleMetrics = metrics.slice(0, 4);
  const hiddenMetricCount = Math.max(0, metrics.length - visibleMetrics.length);

  return (
    <button
      type="button"
      className={['device-card', getToneClass(device.tone), selected ? 'device-card--selected' : ''].join(' ')}
      onClick={() => onSelect(selected ? null : device.id)}
      aria-pressed={selected}
    >
      <div className="device-card__head">
        <div className="device-card__identity">
          <span className="device-card__icon-wrap">
            <Icon icon={device.icon} size={42} />
          </span>
          <div className="device-card__body">
            <div className="device-card__name">{device.name}</div>
            <div className="device-card__model">{device.model}</div>
          </div>
        </div>
        <StatusBadge label={device.connectivity} tone={device.isOnline ? 'green' : 'amber'} />
      </div>
      <div className="device-card__status-list">
        <div className="device-card__status-row">
          <span className="device-card__status-label">Event status</span>
          <strong className={['device-card__status-value', getEventStatusClass(eventStatusTone)].join(' ')}>
            {eventStatusLabel}
          </strong>
        </div>
        <div className="device-card__status-row">
          <span className="device-card__status-label">Telemetry</span>
          <strong className="device-card__status-value">
            {[device.connectivity, device.battery, device.signal].filter(Boolean).join(' / ')}
          </strong>
        </div>
      </div>
      {visibleMetrics.length > 0 || hiddenMetricCount > 0 || device.actions?.length ? (
        <div className="device-card__metric-grid">
          {visibleMetrics.map((metric) => (
            <span key={metric.id} className={`device-card__metric ${getToneClass(metric.tone)}`}>
              <span className="device-card__metric-icon">
                <Icon icon={metric.icon} size={16} />
              </span>
              <span className="device-card__metric-copy">
                <span>{metric.label}</span>
                <strong>{metric.value}</strong>
              </span>
            </span>
          ))}
          {hiddenMetricCount > 0 ? (
            <span className={`device-card__metric ${getToneClass('slate')}`}>
              <span className="device-card__metric-icon">
                <Icon icon={{ category: 'misc', key: 'info' }} size={16} />
              </span>
              <span className="device-card__metric-copy">
                <span>More</span>
                <strong>{hiddenMetricCount}</strong>
              </span>
            </span>
          ) : null}
          {device.actions?.length ? (
            <span className={`device-card__metric ${getToneClass('cyan')}`}>
              <span className="device-card__metric-icon">
                <Icon icon={{ category: 'misc', key: 'energy' }} size={16} />
              </span>
              <span className="device-card__metric-copy">
                <span>Controls</span>
                <strong>{device.actions.length}</strong>
              </span>
            </span>
          ) : null}
        </div>
      ) : null}
    </button>
  );
}
