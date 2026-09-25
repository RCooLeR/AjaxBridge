import type { Device, EventItem, GlowTone } from '../models/dashboard';
import { DeviceCard } from './DeviceCard';

interface DeviceGridProps {
  devices: Device[];
  events: EventItem[];
  selectedDeviceId: string | null;
  onSelectDevice: (deviceId: string | null) => void;
}

function getDeviceEventStatus(device: Device): { label: string; tone: GlowTone } {
  if (device.attention) {
    return {
      label: device.status,
      tone: device.tone === 'green' ? 'amber' : device.tone,
    };
  }

  return {
    label: 'Nominal',
    tone: 'green',
  };
}

export function DeviceGrid({ devices, selectedDeviceId, onSelectDevice }: DeviceGridProps) {
  return (
    <section className="device-grid glass-panel">
      <div className="section-heading">
        <span>Room devices</span>
        <strong>{devices.length}</strong>
      </div>
      <div className="device-grid__list">
        {devices.map((device) => {
          const eventStatus = getDeviceEventStatus(device);

          return (
            <DeviceCard
              key={device.id}
              device={device}
              eventStatusLabel={eventStatus.label}
              eventStatusTone={eventStatus.tone}
              selected={device.id === selectedDeviceId}
              onSelect={onSelectDevice}
            />
          );
        })}
      </div>
    </section>
  );
}
