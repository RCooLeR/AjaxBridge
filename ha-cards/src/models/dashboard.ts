export type GlowTone = 'cyan' | 'green' | 'amber' | 'red' | 'violet' | 'slate';

export type RoomType = string;

export type DeviceType = string;

export type EventType = string;

export type IconCategory =
  | 'rooms'
  | 'devices'
  | 'system-states'
  | 'security-states'
  | 'sensors'
  | 'events'
  | 'connectivity'
  | 'misc';

export interface IconRef {
  category: IconCategory;
  key: string;
}

export interface IconRegistryEntry {
  color: string;
}

export type IconRegistry = Record<IconCategory, Record<string, IconRegistryEntry>>;

export interface DashboardChip {
  id: string;
  label: string;
  value: string;
  icon: IconRef;
  tone: GlowTone;
  active: boolean;
}

export interface DashboardMetric {
  id: string;
  label: string;
  value: string;
  icon: IconRef;
  tone: GlowTone;
}

export interface RoomSmdIvsCounts {
  total: number;
  human: number;
  vehicle: number;
  animal: number;
  ivs: number;
}

export interface RoomClimate {
  temperature?: string;
  humidity?: string;
}

export interface RoomSafety {
  smokeHigh: number;
  coHigh: number;
  smokeCapable?: number;
  coCapable?: number;
}

export interface GridPowerSummary {
  known: number;
  online: number;
  outage: number;
}

export interface Room {
  id: string;
  name: string;
  type: RoomType;
  summary: string;
  heroLabel: string;
  image: string;
  accent: string;
  icon: IconRef;
  statusTone: GlowTone;
  smdIvs?: RoomSmdIvsCounts;
  dahuaCameraCount?: number;
  climate?: RoomClimate;
  safety?: RoomSafety;
  gridPower?: GridPowerSummary;
}

export interface DeviceHeroMedia {
  entityId: string;
  title: string;
  kind: 'stream' | 'image';
  src: string;
  posterSrc?: string;
}

export interface DeviceAction {
  id: string;
  entityId: string;
  label: string;
  domain: DeviceActionDomain;
  service: string;
  stateLabel?: string;
  controlState?: DeviceControlState;
  disabled?: boolean;
  disabledReason?: string;
  confirmation?: string;
  observedStateEntityId?: string;
  valvePositionEntityId?: string;
}

export type DeviceActionDomain = 'button' | 'switch' | 'lock' | 'valve';

export type DeviceControlState = 'on' | 'off' | 'opening' | 'closing' | 'intermediate' | 'moving' | 'unknown';

export type CameraStreamProfile = 'main' | 'sub';

export interface Device {
  id: string;
  roomId: string;
  type: DeviceType;
  name: string;
  model: string;
  icon: IconRef;
  tone: GlowTone;
  status: string;
  connectivity: string;
  battery: string;
  signal: string;
  entityId: string;
  isOnline: boolean;
  attention: boolean;
  heroMedia?: DeviceHeroMedia;
  actions?: DeviceAction[];
  metrics?: DashboardMetric[];
}

export interface EventItem {
  id: string;
  roomId: string;
  deviceId?: string;
  type: EventType;
  title: string;
  description: string;
  occurredAt: string;
  source: string;
  icon: IconRef;
  tone: GlowTone;
}

export interface SystemState {
  chips: DashboardChip[];
}

export interface RoomSummary {
  roomId: string;
  deviceCount: number;
  onlineCount: number;
  attentionCount: number;
  smdIvs: RoomSmdIvsCounts;
  dahuaCameraCount: number;
  climate?: RoomClimate;
  safety: RoomSafety;
  gridPower?: GridPowerSummary;
  latestEventLabel: string;
  tone: GlowTone;
}

export interface DashboardData {
  systemState: SystemState;
  rooms: Room[];
  devices: Device[];
  events: EventItem[];
}
