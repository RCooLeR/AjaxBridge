import devicesData from './devices.json';
import eventsData from './events.json';
import roomsData from './rooms.json';
import type { DashboardChipDetailItem, DashboardData, Device, EventItem, Room, RoomSafety, RoomSmdIvsCounts, RoomSummary, SystemState } from '../models/dashboard';

export const dashboardData: DashboardData = {
  systemState: buildStaticSystemState(devicesData as Device[], eventsData as EventItem[], roomsData as Room[]),
  rooms: roomsData as Room[],
  devices: devicesData as Device[],
  events: eventsData as EventItem[],
};

function buildStaticSystemState(devices: Device[], events: EventItem[], rooms: Room[]): SystemState {
  const smdIvsTotals = rooms.reduce<RoomSmdIvsCounts>((totals, room) => {
    const counts = room.smdIvs ?? emptySmdIvsCounts();
    totals.total += counts.total;
    totals.human += counts.human;
    totals.vehicle += counts.vehicle;
    totals.animal += counts.animal;
    totals.ivs += counts.ivs;
    return totals;
  }, emptySmdIvsCounts());
  const fallbackSmdCount = events.filter((event) => ['human_detected', 'vehicle_detected'].includes(event.type)).length;
  const fallbackIvsCount = events.filter((event) => ['tripwire_detected', 'intrusion_detected'].includes(event.type)).length;
  const useFallbackSmd = smdIvsTotals.human + smdIvsTotals.vehicle + smdIvsTotals.animal === 0;
  const shownHumanCount = useFallbackSmd ? events.filter((event) => event.type === 'human_detected').length : smdIvsTotals.human;
  const shownVehicleCount = useFallbackSmd ? events.filter((event) => event.type === 'vehicle_detected').length : smdIvsTotals.vehicle;
  const shownAnimalCount = useFallbackSmd ? 0 : smdIvsTotals.animal;
  const shownIvsCount = smdIvsTotals.ivs || fallbackIvsCount;
  const outlets = devices.filter(isOutletOrWallSwitchDevice);
  const lights = devices.filter(isLightSwitchDevice);
  const outletOnCount = outlets.filter(deviceLooksOn).length;
  const lightOnCount = lights.filter(deviceLooksOn).length;
  const alerts = devices.filter((device) => device.attention).length;
  const staticSwitchItems = (items: Device[]): DashboardChipDetailItem[] => items.length > 0
    ? items.map((device) => ({
      id: `device:${device.id}`,
      label: device.name,
      value: deviceLooksOn(device) ? 'On' : 'Off',
      tone: deviceLooksOn(device) ? 'green' : 'slate',
    }))
    : [{ id: 'none-discovered', label: 'Discovered devices', value: 'None', tone: 'slate' }];
  const chips = [
    {
      id: 'system-mode',
      label: 'Security mode',
      value: 'Armed',
      icon: { category: 'system-states', key: 'armed' },
      tone: 'green',
      active: true,
      details: {
        title: 'Security mode',
        summary: 'Current Ajax security arming state. Device warnings are listed separately under Alerts.',
        items: [{ id: 'security-state', label: 'Current state', value: 'Armed', tone: 'green' }],
      },
    },
    {
      id: 'system-smd',
      label: 'SMD today',
      value: String(smdIvsTotals.human + smdIvsTotals.vehicle + smdIvsTotals.animal || fallbackSmdCount),
      icon: { category: 'events', key: 'human_detected' },
      tone: 'violet',
      active: smdIvsTotals.total > 0 || fallbackSmdCount > 0,
      details: {
        title: 'Smart Motion Detection',
        summary: 'The total is people + vehicles + animals reported by SMD analytics.',
        items: [
          { id: 'smd-human', label: 'People', value: String(shownHumanCount), tone: shownHumanCount > 0 ? 'violet' : 'slate' },
          { id: 'smd-vehicle', label: 'Vehicles', value: String(shownVehicleCount), tone: shownVehicleCount > 0 ? 'violet' : 'slate' },
          { id: 'smd-animal', label: 'Animals', value: String(shownAnimalCount), tone: shownAnimalCount > 0 ? 'violet' : 'slate' },
        ],
      },
    },
    {
      id: 'system-ivs',
      label: 'IVS today',
      value: String(shownIvsCount),
      icon: { category: 'events', key: 'tripwire_detected' },
      tone: 'amber',
      active: smdIvsTotals.ivs > 0 || fallbackIvsCount > 0,
      details: {
        title: 'IVS events',
        summary: 'Tripwire and intrusion events reported by camera IVS analytics.',
        items: [{ id: 'ivs-total', label: 'Tripwire / intrusion events', value: String(shownIvsCount), tone: shownIvsCount > 0 ? 'amber' : 'slate' }],
      },
    },
    {
      id: 'system-outlets',
      label: 'Outlets',
      value: `${outletOnCount}/${outlets.length || 0}`,
      icon: { category: 'devices', key: 'smart_plug' },
      tone: outletOnCount > 0 ? 'green' : 'slate',
      active: outlets.length > 0 && outletOnCount > 0,
      details: {
        title: 'Outlets',
        summary: 'The first number is outlets currently on; the second is every Ajax Socket or WallSwitch counted by this card.',
        items: staticSwitchItems(outlets),
      },
    },
    {
      id: 'system-lights',
      label: 'Light switches',
      value: `${lightOnCount}/${lights.length || 0}`,
      icon: { category: 'devices', key: 'light_switch' },
      tone: lightOnCount > 0 ? 'green' : 'slate',
      active: lights.length > 0 && lightOnCount > 0,
      details: {
        title: 'Light switches',
        summary: 'The first number is light switches currently on; the second is every light switch counted by this card.',
        items: staticSwitchItems(lights),
      },
    },
  ] as SystemState['chips'];

  if (alerts > 0) {
    chips.push({
      id: 'system-alerts',
      label: 'Alerts',
      value: String(alerts),
      icon: { category: 'misc', key: 'alert' },
      tone: 'amber',
      active: true,
      details: {
        title: 'Devices needing attention',
        summary: 'Current device faults, security alarms, offline devices, or unknown control states. Informational grid outages are excluded.',
        items: devices
          .filter((device) => device.attention)
          .map((device) => ({ id: `device:${device.id}`, label: device.name, value: device.status, tone: device.tone })),
      },
    });
  }

  return { chips };
}

export function getDefaultRoomId(data: DashboardData): string {
  return data.rooms.find((room) => room.id === 'living-room')?.id ?? data.rooms[0]?.id ?? '';
}

export function getDevicesForRoom(data: DashboardData, roomId: string): Device[] {
  return data.devices.filter((device) => device.roomId === roomId);
}

export function getEventsForRoom(data: DashboardData, roomId: string): EventItem[] {
  return data.events
    .filter((event) => event.roomId === roomId)
    .sort((left, right) => Date.parse(right.occurredAt) - Date.parse(left.occurredAt));
}

export function getRoomSummaries(data: DashboardData): Record<string, RoomSummary> {
  return data.rooms.reduce<Record<string, RoomSummary>>((summaries, room) => {
    const roomDevices = getDevicesForRoom(data, room.id);
    const roomEvents = getEventsForRoom(data, room.id);
    const onlineCount = roomDevices.filter((device) => device.isOnline).length;
    const attentionCount = roomDevices.filter((device) => device.attention).length;
    const latestEvent = roomEvents[0];

    summaries[room.id] = {
      roomId: room.id,
      deviceCount: roomDevices.length,
      onlineCount,
      attentionCount,
      smdIvs: room.smdIvs ?? emptySmdIvsCounts(),
      dahuaCameraCount: room.dahuaCameraCount ?? 0,
      climate: room.climate,
      safety: room.safety ?? getRoomSafety(roomEvents),
      gridPower: room.gridPower,
      latestEventLabel: latestEvent?.title ?? 'No recent events',
      tone: attentionCount > 0 ? 'amber' : room.statusTone,
    };

    return summaries;
  }, {});
}

function emptySmdIvsCounts(): RoomSmdIvsCounts {
  return { total: 0, human: 0, vehicle: 0, animal: 0, ivs: 0 };
}

function getRoomSafety(events: EventItem[]): RoomSafety {
  return events.reduce<RoomSafety>((safety, event) => {
    if (event.type === 'smoke_detected' || event.type === 'fire_detected') {
      safety.smokeHigh += 1;
    }
    if (event.type === 'gas_detected') {
      safety.coHigh += 1;
    }
    return safety;
  }, { smokeHigh: 0, coHigh: 0 });
}

function isOutletOrWallSwitchDevice(device: Device): boolean {
  const text = deviceDescriptor(device);
  if (isLightSwitchDevice(device)) {
    return false;
  }
  return device.type === 'smart_plug' || /(socket|plug|outlet|wallswitch|wall switch)/.test(text);
}

function isLightSwitchDevice(device: Device): boolean {
  return device.type === 'light_switch' || /lightswitch|light switch|\blight\b/.test(deviceDescriptor(device));
}

function deviceLooksOn(device: Device): boolean {
  if (device.actions?.some((action) => action.service === 'turn_off')) {
    return true;
  }
  return /\bon\b|active|load|w\b/.test(`${device.status} ${device.signal}`.toLowerCase());
}

function deviceDescriptor(device: Device): string {
  return `${device.type} ${device.name} ${device.model} ${device.entityId}`.toLowerCase();
}
