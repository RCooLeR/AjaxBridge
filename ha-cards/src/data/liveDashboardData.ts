import { useEffect, useEffectEvent, useMemo, useState } from 'react';
import type {
  DashboardChip,
  DashboardData,
  DashboardMetric,
  Device,
  DeviceAction,
  DeviceActionDomain,
  DeviceHeroMedia,
  EventItem,
  EventType,
  GlowTone,
  GridPowerSummary,
  IconRef,
  Room,
  RoomClimate,
  RoomSafety,
  RoomSmdIvsCounts,
  SystemState,
} from '../models/dashboard';
import { dashboardData as fallbackDashboardData } from './loadDashboardData';
import type { HomeAssistant, HomeAssistantState } from '../ha/types';
import { ajaxDeviceHealth, ajaxEntityOwner, ajaxEntitySemantic, ajaxRegistryOwner, diagnosticHealth, SECURITY_SIGNALS } from '../utils/ajaxSemantics';
import {
  classifyAjaxDiagnosticMetric,
  controlStateLabel,
  inferAjaxButtonDeviceType,
  isAuthoritativeRelayStateEntity,
  isPhysicalAjaxButtonType,
  resolveDeviceControlState,
  resolveRelayState,
} from '../utils/deviceSemantics';

interface HomeAssistantArea {
  area_id: string;
  name: string;
  picture?: unknown;
  [key: string]: unknown;
}

interface HomeAssistantDeviceEntry {
  id: string;
  area_id?: string | null;
  via_device_id?: string | null;
  suggested_area?: string | null;
  manufacturer?: string | null;
  model?: string | null;
  name?: string | null;
  name_by_user?: string | null;
  identifiers?: unknown;
  [key: string]: unknown;
}

interface HomeAssistantEntityEntry {
  entity_id: string;
  area_id?: string | null;
  device_id?: string | null;
  platform?: string | null;
  unique_id?: string | null;
  hidden_by?: string | null;
  disabled_by?: string | null;
  entity_category?: string | null;
  name?: string | null;
  original_name?: string | null;
  icon?: string | null;
  [key: string]: unknown;
}

interface RegistrySnapshot {
  areas: HomeAssistantArea[];
  devices: HomeAssistantDeviceEntry[];
  entities: HomeAssistantEntityEntry[];
}

interface RegistryIndex {
  areas: HomeAssistantArea[];
  devices: HomeAssistantDeviceEntry[];
  entities: HomeAssistantEntityEntry[];
  areaById: Map<string, HomeAssistantArea>;
  areaIdByName: Map<string, string>;
  deviceById: Map<string, HomeAssistantDeviceEntry>;
  entityById: Map<string, HomeAssistantEntityEntry>;
  entitiesByDeviceId: Map<string, HomeAssistantEntityEntry[]>;
  resolvedAreaByDeviceId: Map<string, string>;
  entitiesByAreaId: Map<string, HomeAssistantEntityEntry[]>;
}

interface DahuaBridgeChannel {
  entityId: string;
  roomId: string;
  bridgeBaseUrl: string;
  rootDeviceId: string;
  channel: number;
}

interface SmdIvsSummaryItem {
  code?: unknown;
  count?: unknown;
}

interface SmdIvsSummaryChannel {
  channel?: unknown;
  total_count?: unknown;
  items?: SmdIvsSummaryItem[];
}

interface SmdIvsSummaryResponse {
  channels?: SmdIvsSummaryChannel[];
}

type RoomSmdIvsCountsByRoom = Record<string, RoomSmdIvsCounts>;

interface ResolvedDevice extends Device {
  lastEventAt: string;
  lastEventType: EventType;
  lastEventDescription: string;
  sourceLabel: string;
  timelineEvents?: EventItem[];
  integration: 'ajax' | 'dahua';
  cameraLike: boolean;
  sensorLike: boolean;
  severity: number;
  securityAlarm?: boolean;
  activeSafety?: { smoke: boolean; co: boolean };
  metrics?: DashboardMetric[];
}

interface RoomMetrics {
  deviceCount: number;
  onlineCount: number;
  attentionCount: number;
  cameraCount: number;
  dahuaCameraCount: number;
  sensorCount: number;
  smdIvs: RoomSmdIvsCounts;
  safety: RoomSafety;
  gridPower: GridPowerSummary;
  latestEventLabel: string;
}

interface MetricCandidate extends DashboardMetric {
  kind: string;
  priority: number;
}

interface AjaxDeviceMetricContext {
  lastEventName: string;
  lastEventAt: string;
  lastSignal: string;
  alarmSignal: string;
  alarmActive: boolean;
  tamperActive: boolean;
  troubleActive: boolean;
  activeSignals: string[];
}

interface DeviceMetricOptions {
  calculateApparentPower?: boolean;
  deviceType?: string;
}

interface DeviceActionContext {
  deviceType?: string;
  linkedEntries?: HomeAssistantEntityEntry[];
}

interface ClimateSample {
  value: number;
  unit: string;
}

const DAHUA_HINT = /(dahua|rroller)/i;
const LEGACY_AJAX2PROM_HINT = /ajax2prometheus/i;
const GO2RTC_HINT = /go2rtc/i;
const VTO_DEBUG_HINT = /(vto|doorbell|bell|дзвінок|вызывная|calling panel)/i;
const MAX_DEVICE_METRICS = 12;
const DAHUA_ENTITY_DOMAINS = new Set(['camera', 'image', 'sensor', 'binary_sensor', 'switch', 'lock']);
const EMPTY_REGISTRIES: RegistrySnapshot = {
  areas: [],
  devices: [],
  entities: [],
};
const EMPTY_ROOM_SMD_IVS_COUNTS: RoomSmdIvsCountsByRoom = {};
const loggedDahuaDebug = new Set<string>();
const ISSUE_SIGNALS = new Set([
  'alarm',
  'burglary',
  'panic',
  'duress',
  'emergency',
  'medical',
  'hold_up',
  'fire',
  'smoke',
  'co',
  'gas',
  'gas_or_co',
  'water_leak',
  'leak',
  'flood',
  'tamper',
  'connectivity',
  'battery',
  'power',
  'hardware',
  'interference',
  'accelerometer',
  'fire_detector',
  'configuration',
  'firmware',
  'supervision',
  'button',
  'temperature',
  'bypass',
  'tamper_bypass',
]);

export function useDashboardData(hass?: HomeAssistant, account?: string, dahuaBase?: string): DashboardData {
  const [registries, setRegistries] = useState<RegistrySnapshot | null>(null);
  const [smdIvsSnapshot, setSmdIvsSnapshot] = useState<{
    signature: string;
    counts: RoomSmdIvsCountsByRoom;
  }>({ signature: '', counts: EMPTY_ROOM_SMD_IVS_COUNTS });
  const normalizedDahuaBase = normalizeDahuaBaseUrl(dahuaBase);
  const canLoadRegistries = Boolean(hass && typeof Reflect.get(hass, 'callWS') === 'function');

  const loadRegistries = useEffectEvent(async (): Promise<RegistrySnapshot> => {
    if (!hass?.callWS) {
      return EMPTY_REGISTRIES;
    }

    const [areas, devices, entities] = await Promise.all([
      hass.callWS<HomeAssistantArea[]>({ type: 'config/area_registry/list' }),
      hass.callWS<HomeAssistantDeviceEntry[]>({ type: 'config/device_registry/list' }),
      hass.callWS<HomeAssistantEntityEntry[]>({ type: 'config/entity_registry/list' }),
    ]);

    return {
      areas: Array.isArray(areas) ? areas : [],
      devices: Array.isArray(devices) ? devices : [],
      entities: Array.isArray(entities) ? entities : [],
    };
  });

  useEffect(() => {
    if (!canLoadRegistries || registries !== null) {
      return;
    }

    let active = true;

    void loadRegistries()
      .then((snapshot) => {
        if (!active) {
          return;
        }
        setRegistries(snapshot);
      })
      .catch(() => {
        if (active) {
          setRegistries(EMPTY_REGISTRIES);
        }
      });

    return () => {
      active = false;
    };
  }, [canLoadRegistries, registries]);

  const registryIndex = useMemo(
    () => buildRegistryIndex(registries ?? EMPTY_REGISTRIES),
    [registries],
  );
  const smdIvsSignature = useMemo(
    () => (hass ? dahuaBridgeChannelSignature(hass.states, registryIndex, normalizedDahuaBase) : ''),
    [hass, registryIndex, normalizedDahuaBase],
  );
  const roomSmdIvsCounts = smdIvsSignature && smdIvsSnapshot.signature === smdIvsSignature
    ? smdIvsSnapshot.counts
    : EMPTY_ROOM_SMD_IVS_COUNTS;

  const loadCurrentSmdIvsCounts = useEffectEvent((signal: AbortSignal) => {
    if (!hass) {
      return Promise.resolve(EMPTY_ROOM_SMD_IVS_COUNTS);
    }
    return loadSmdIvsCountsByRoom(hass.states, registryIndex, normalizedDahuaBase, signal);
  });

  useEffect(() => {
    if (!smdIvsSignature) {
      return;
    }

    let active = true;
    let controller = new AbortController();

    const refresh = async () => {
      controller.abort();
      controller = new AbortController();

      try {
        const counts = await loadCurrentSmdIvsCounts(controller.signal);
        if (active) {
          setSmdIvsSnapshot({ signature: smdIvsSignature, counts });
        }
      } catch (error) {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) {
          console.error('[ajaxbridge] Unable to refresh Dahua SMD/IVS counts', error);
        }
      }
    };

    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, 60_000);

    return () => {
      active = false;
      controller.abort();
      window.clearInterval(timer);
    };
  }, [smdIvsSignature]);

  const dashboardData = useMemo(() => {
    if (!hass) {
      return fallbackDashboardData;
    }

    return buildDashboardDataFromHomeAssistant(hass.states, registryIndex, account, roomSmdIvsCounts);
  }, [account, hass, registryIndex, roomSmdIvsCounts]);

  return dashboardData;
}

export function buildRegistryIndex(registries: RegistrySnapshot): RegistryIndex {
  const areaById = new Map(registries.areas.map((area) => [area.area_id, area]));
  const areaIdByName = new Map(registries.areas.map((area) => [slugPart(area.name), area.area_id]));
  const deviceById = new Map(registries.devices.map((device) => [device.id, device]));
  const entityById = new Map(registries.entities.map((entity) => [entity.entity_id, entity]));
  const entitiesByDeviceId = groupByDeviceId(registries.entities);
  const resolvedAreaByDeviceId = new Map<string, string>();

  for (const deviceEntry of registries.devices) {
    const linkedEntities = entitiesByDeviceId.get(deviceEntry.id) ?? [];
    const areaId = resolveAreaId(deviceEntry, linkedEntities, areaIdByName);
    if (areaId) {
      resolvedAreaByDeviceId.set(deviceEntry.id, areaId);
    }
  }

  return {
    areas: registries.areas,
    devices: registries.devices,
    entities: registries.entities,
    areaById,
    areaIdByName,
    deviceById,
    entityById,
    entitiesByDeviceId,
    resolvedAreaByDeviceId,
    entitiesByAreaId: groupByAreaId(registries.entities, resolvedAreaByDeviceId),
  };
}

export function buildDashboardDataFromHomeAssistant(
  states: Record<string, HomeAssistantState>,
  registryIndex: RegistryIndex,
  accountFilter?: string,
  roomSmdIvsCounts: RoomSmdIvsCountsByRoom = {},
): DashboardData {
  const { areas, areaById, deviceById, devices, entities, entitiesByDeviceId, entitiesByAreaId, resolvedAreaByDeviceId } =
    registryIndex;
  const ajaxDevices = buildAjaxDevices(
    devices,
    entitiesByDeviceId,
    resolvedAreaByDeviceId,
    states,
    accountFilter,
  );
  const ajaxDeviceIds = new Set(ajaxDevices.map((device) => device.id));
  const dahuaDevices = buildDahuaDevices(
    devices,
    entities,
    deviceById,
    entitiesByDeviceId,
    resolvedAreaByDeviceId,
    entitiesByAreaId,
    states,
    ajaxDeviceIds,
  );

  const resolvedDevices = [...ajaxDevices, ...dahuaDevices]
    .filter((device) => areaById.has(device.roomId))
    .sort(sortResolvedDevices);
  const resolvedEvents = buildEvents(resolvedDevices);
  const roomMetrics = buildRoomMetrics(resolvedDevices, resolvedEvents, roomSmdIvsCounts);
  const roomIds = new Set(resolvedDevices.map((device) => device.roomId));
  const rooms = areas
    .filter((area) => roomIds.has(area.area_id))
    .map((area) => buildRoom(area, entitiesByAreaId.get(area.area_id) ?? [], states, roomMetrics.get(area.area_id)))
    .sort(sortRooms);

  return {
    systemState: buildSystemState(states, rooms, resolvedDevices, entities, accountFilter),
    rooms,
    devices: resolvedDevices.map(toPublicDevice),
    events: resolvedEvents,
  };
}

function buildAjaxDevices(
  devices: HomeAssistantDeviceEntry[],
  entitiesByDeviceId: Map<string, HomeAssistantEntityEntry[]>,
  resolvedAreaByDeviceId: Map<string, string>,
  states: Record<string, HomeAssistantState>,
  accountFilter?: string,
): ResolvedDevice[] {
  const output: ResolvedDevice[] = [];
  const groups = new Map<string, { device: HomeAssistantDeviceEntry; entries: HomeAssistantEntityEntry[]; roomId?: string }>();
  const registeredOwners = new Map(devices.flatMap((device) => {
    const owner = ajaxRegistryOwner(extractIdentifiers(device));
    return owner ? [[owner, device] as const] : [];
  }));
  for (const device of devices) {
    const entries = entitiesByDeviceId.get(device.id) ?? [];
    if ((!isAjaxDevice(device) && !entries.some((entry) => /^ajaxbridge_/i.test(entry.unique_id ?? ''))) || isLegacyAjax2PrometheusDevice(device)) continue;
    for (const entry of entries) {
      if (isLegacyAjax2PrometheusEntity(entry) || isEntityHiddenOrDisabled(entry)) continue;
      const owner = ajaxEntityOwner(entry.unique_id, states[entry.entity_id]?.attributes ?? {}) ?? ajaxRegistryOwner(extractIdentifiers(device)) ?? device.id;
      const metadata = registeredOwners.get(owner) ?? device;
      const group = groups.get(owner) ?? { device: metadata, entries: [], roomId: resolvedAreaByDeviceId.get(metadata.id) ?? resolvedAreaByDeviceId.get(device.id) };
      group.entries.push(entry);
      groups.set(owner, group);
    }
  }

  for (const [owner, group] of groups) {
    const deviceEntry = group.device;

    const account = owner.match(/^sia_(.+)_zone_\d+$/)?.[1] ?? extractAjaxAccount(deviceEntry);
    if (accountFilter && account && account.toLowerCase() !== accountFilter.toLowerCase()) {
      continue;
    }

    const linkedEntities = group.entries;
    const actionEntries = linkedEntities.filter((entry) => isActionableEntity(entry));
    const accountDevice = isAjaxAccountDevice(deviceEntry);
    if (accountDevice && actionEntries.length === 0) {
      continue;
    }
    const roomId = group.roomId;
    if (!roomId || linkedEntities.length === 0) {
      continue;
    }

    const lastEventName = firstEntityState(linkedEntities, states, '_last_event_name')?.state ?? 'Awaiting event';
    const eventTimestamp = firstEntityState(linkedEntities, states, '_last_event_at')?.state ?? '';
    const lastEventAt = toUnix(eventTimestamp) > 0 ? eventTimestamp : '';
    const lastSignal = firstEntityState(linkedEntities, states, '_last_signal')?.state ?? 'idle';
    const alarmSignal = firstEntityState(linkedEntities, states, '_alarm_signal')?.state ?? '';
    const activeSemantic = (semantic: string) => linkedEntities.some((entry) => ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) === semantic && isOn(states[entry.entity_id]));
    const alarmActive = activeSemantic('alarm_active');
    const tamperActive = activeSemantic('tamper_active');
    const troubleActive = activeSemantic('trouble_active');
    const activeSignals = linkedEntities
      .map((entry) => ({ entry, semantic: ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) }))
      .filter(({ semantic }) => semantic?.startsWith('signal_'))
      .filter(({ entry }) => isOn(states[entry.entity_id]))
      .map(({ semantic }) => semantic?.slice('signal_'.length) ?? null)
      .filter((signal): signal is string => signal !== null);
    const health = ajaxDeviceHealth(linkedEntities.map((entry) => ({
      semantic: ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes),
      domain: entityDomain(entry.entity_id),
      value: states[entry.entity_id]?.state ?? 'unknown',
      deviceClass: safeString(states[entry.entity_id]?.attributes.device_class).toLowerCase(),
    })), activeSignals);

    const signalHeadline = summarizeHeadline({
      alarmActive,
      tamperActive,
      troubleActive,
      activeSignals,
    });
    const signalSeverity = severityFromSignals({
      alarmActive,
      tamperActive,
      troubleActive,
      activeSignals,
    });
    const securityAlarm = alarmActive || tamperActive || activeSignals.some((signal) => SECURITY_SIGNALS.has(signal));
    let severity = Math.max(signalSeverity, health.severity);
    let headline = securityAlarm || signalSeverity >= health.severity ? signalHeadline : health.warning;
    const offline = !health.online;
    const sourceLabel = displayName(deviceEntry, linkedEntities, states);
    const type = inferDeviceType({
      name: sourceLabel,
      model: accountDevice ? 'Hub' : safeString(deviceEntry.model),
      entityIds: linkedEntities.map((entry) => entry.entity_id),
    });
    const eventType = mapSignalToEventType(alarmSignal || lastSignal, alarmActive, offline);
    const actions = isPhysicalAjaxButtonType(type)
      ? undefined
      : buildDeviceActions(actionEntries, states, { deviceType: type, linkedEntries: linkedEntities });
    if (actions?.some((action) => action.disabled)) {
      if (severity < 2) headline = 'Control state unknown';
      severity = Math.max(severity, 2);
    }
    const metrics = buildDeviceMetrics(
      linkedEntities,
      states,
      {
        lastEventName,
        lastEventAt,
        lastSignal,
        alarmSignal,
        alarmActive,
        tamperActive,
        troubleActive,
        activeSignals,
      },
      { calculateApparentPower: type === 'wall_switch', deviceType: type },
    );

    output.push({
      id: owner,
      roomId,
      type,
      name: sourceLabel,
      model: accountDevice ? 'Hub' : safeString(deviceEntry.model) || humanizeSlug(type),
      icon: iconForDevice(type, sourceLabel, accountDevice ? 'Hub' : safeString(deviceEntry.model)),
      tone: toneFromSeverity(severity),
      status: headline,
      connectivity: health.connectivity,
      battery: readAjaxBatteryLabel(linkedEntities, states, activeSignals),
      signal: readAjaxSignalLabel(linkedEntities, states, lastSignal),
      entityId: linkedEntities[0]?.entity_id ?? '',
      isOnline: !offline,
      attention: severity >= 2,
      actions,
      metrics,
      lastEventAt,
      lastEventType: eventType,
      lastEventDescription: `${sourceLabel}: ${lastEventName}`,
      sourceLabel,
      integration: 'ajax',
      cameraLike: false,
      sensorLike: true,
      severity,
      securityAlarm,
      activeSafety: { smoke: activeSignals.some((signal) => ['fire', 'smoke', 'temperature'].includes(signal)), co: activeSignals.some((signal) => ['co', 'gas', 'gas_or_co'].includes(signal)) },
    });
  }

  return output;
}

function buildDahuaDevices(
  devices: HomeAssistantDeviceEntry[],
  entities: HomeAssistantEntityEntry[],
  deviceById: Map<string, HomeAssistantDeviceEntry>,
  entitiesByDeviceId: Map<string, HomeAssistantEntityEntry[]>,
  resolvedAreaByDeviceId: Map<string, string>,
  entitiesByAreaId: Map<string, HomeAssistantEntityEntry[]>,
  states: Record<string, HomeAssistantState>,
  skipDeviceIds: Set<string>,
): ResolvedDevice[] {
  const output: ResolvedDevice[] = [];
  const consumedEntityIds = new Set<string>();

  for (const deviceEntry of devices) {
    if (skipDeviceIds.has(deviceEntry.id) || !isDahuaDevice(deviceEntry, entitiesByDeviceId.get(deviceEntry.id) ?? [])) {
      continue;
    }

    const linkedEntities = entitiesByDeviceId.get(deviceEntry.id) ?? [];
    const visibleLinkedEntities = linkedEntities.filter(
      (entry) => !isIgnoredDahuaEntity(deviceEntry, entry, states[entry.entity_id]),
    );
    const roomId = resolvedAreaByDeviceId.get(deviceEntry.id);
    if (!roomId || visibleLinkedEntities.length === 0) {
      continue;
    }

    const nonCameraEntries = visibleLinkedEntities.filter((entry) => !isCameraDomain(entry.entity_id));
    const statusEntries = nonCameraEntries.filter((entry) => entityDomain(entry.entity_id) !== 'button');
    const preferredEntries = statusEntries.length > 0 ? statusEntries : nonCameraEntries.length > 0 ? nonCameraEntries : visibleLinkedEntities;
    const mediaEntries = linkedEntities.filter((entry) => isHeroMediaEntity(entry) && !safeString(entry.disabled_by));
    const actionEntries = visibleLinkedEntities.filter((entry) => isActionableEntity(entry));

    for (const entry of linkedEntities) {
      consumedEntityIds.add(entry.entity_id);
    }

    const resolvedDevice = buildGenericIntegrationDevice(
      `dahua:${deviceEntry.id}`,
      roomId,
      displayName(deviceEntry, preferredEntries),
      safeString(deviceEntry.model) || 'Integration device',
      preferredEntries,
      visibleLinkedEntities,
      mediaEntries,
      actionEntries,
      states,
      'dahua',
    );
    if (resolvedDevice) {
      output.push(resolvedDevice);
    }
  }

  for (const entityEntry of entities) {
    const parentDevice = deviceById.get(entityEntry.device_id ?? '');
    if (
      consumedEntityIds.has(entityEntry.entity_id) ||
      isIgnoredDahuaEntity(parentDevice, entityEntry, states[entityEntry.entity_id]) ||
      !isRelevantDahuaEntity(entityEntry, parentDevice)
    ) {
      continue;
    }

    const roomId = entityEntry.area_id ?? resolvedAreaByDeviceId.get(entityEntry.device_id ?? '') ?? findAreaForEntity(entityEntry, entitiesByAreaId);
    if (!roomId) {
      continue;
    }

    const resolvedDevice = buildGenericIntegrationDevice(
      `dahua-entity:${entityEntry.entity_id}`,
      roomId,
      entityDisplayName(entityEntry, states[entityEntry.entity_id]),
      humanizeSlug(entityDomain(entityEntry.entity_id)),
      [entityEntry],
      [entityEntry],
      isHeroMediaEntity(entityEntry) ? [entityEntry] : [],
      isActionableEntity(entityEntry) ? [entityEntry] : [],
      states,
      'dahua',
    );
    if (resolvedDevice) {
      output.push(resolvedDevice);
    }
  }

  return output;
}

function buildGenericIntegrationDevice(
  id: string,
  roomId: string,
  name: string,
  model: string,
  entityEntries: HomeAssistantEntityEntry[],
  classificationEntries: HomeAssistantEntityEntry[],
  mediaEntries: HomeAssistantEntityEntry[],
  actionEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  integration: 'dahua',
): ResolvedDevice | null {
  const primary = pickPrimaryEntity(entityEntries, states);
  if (!primary) {
    return null;
  }

  const alertEntry = entityEntries.find((entry) => {
    const state = states[entry.entity_id];
    return !isOfflineState(state) && entityIsAlert(entry, state);
  });
  const online = entityEntries.some((entry) => hasAvailableState(states[entry.entity_id]));
  const offlineEntry = online ? undefined : entityEntries.find((entry) => isOfflineState(states[entry.entity_id]));
  const activeEntry = alertEntry ?? primary ?? offlineEntry;
  const activeState = states[activeEntry.entity_id];
  const resolvedModel = resolveIntegrationModel(model, entityEntries, states, integration);
  const type = inferDeviceType({
    name,
    model: resolvedModel,
    entityIds: classificationEntries.map((entry) => entry.entity_id),
  });
  const timelineEntries =
    integration === 'dahua' && type === 'camera'
      ? filterDahuaCameraTimelineEntries(entityEntries, states)
      : entityEntries;
  const attention = Boolean(alertEntry || !online);
  const eventType = eventTypeFromEntity(activeEntry, activeState);
  const description = buildGenericEventDescription(activeEntry, activeState, name);
  const lastEventAt = activeState?.last_changed ?? activeState?.last_updated ?? '';
  const severity = !online ? 3 : alertEntry ? severityFromEntity(alertEntry, activeState) : 0;
  const timelineEvents = integration === 'dahua' ? buildDahuaTimelineEvents(id, roomId, name, timelineEntries, states) : undefined;
  const latestTimelineEvent = timelineEvents?.[0];
  const heroMedia = buildHeroMedia(mediaEntries, states);
  const actions = type === 'camera' ? undefined : buildDeviceActions(actionEntries, states);
  const metrics = buildDeviceMetrics(entityEntries, states);

  debugDahuaCandidate(name, resolvedModel, entityEntries, states, activeEntry, activeState);

  return {
    id,
    roomId,
    type,
    name,
    model: resolvedModel,
    icon: iconForDevice(type, name, resolvedModel),
    tone: toneFromSeverity(severity),
    status: attention ? description : 'Nominal',
    connectivity: online ? 'Online' : 'Offline',
    battery: readBatteryLabel(entityEntries, states),
    signal: entityDomain(primary.entity_id),
    entityId: primary.entity_id,
    isOnline: online,
    attention,
    heroMedia,
    actions,
    metrics,
    lastEventAt: latestTimelineEvent?.occurredAt ?? lastEventAt,
    lastEventType: latestTimelineEvent?.type ?? eventType,
    lastEventDescription: latestTimelineEvent?.description ?? description,
    sourceLabel: name,
    timelineEvents,
    integration,
    cameraLike: type === 'camera',
    sensorLike: type !== 'camera',
    severity,
  };
}

function resolveIntegrationModel(
  fallbackModel: string,
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  integration: string,
): string {
  if (integration !== 'dahua') {
    return fallbackModel;
  }

  return (
    readDahuaDirectIpcModel(entityEntries, states) ||
    readFirstEntityAttribute(entityEntries, states, [
      'direct_ipc_model',
      'bridge_device_model',
      'bridge_model',
      'model',
      'device_model',
    ]) || fallbackModel
  );
}

function readDahuaDirectIpcModel(
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): string {
  for (const entry of entityEntries) {
    const state = states[entry.entity_id];
    if (!state || isOfflineState(state)) {
      continue;
    }
    const text = [
      entry.entity_id,
      entry.name,
      entry.original_name,
      state.attributes.friendly_name,
    ].map(safeString).join(' ').toLowerCase();
    if (!/(^|[\s._-])direct[\s._-]*ipc[\s._-]*model($|[\s._-])/.test(text)) {
      continue;
    }
    const model = safeString(state.state);
    if (model && !['unknown', 'unavailable', 'none'].includes(model.toLowerCase())) {
      return model;
    }
  }

  return '';
}

function readFirstEntityAttribute(
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  attributeNames: string[],
): string {
  for (const entry of entityEntries) {
    const attributes = states[entry.entity_id]?.attributes ?? {};
    for (const attributeName of attributeNames) {
      const value = safeString(attributes[attributeName]);
      if (value) {
        return value;
      }
    }
  }

  return '';
}

function buildEvents(devices: ResolvedDevice[]): EventItem[] {
  const nominalIcon: IconRef = { category: 'events', key: 'ok' };
  return devices
    .flatMap((device) => {
      if (device.integration === 'dahua' && device.cameraLike && device.timelineEvents) {
        return device.timelineEvents;
      }

      if (device.timelineEvents && device.timelineEvents.length > 0) {
        return device.timelineEvents;
      }

      if (!device.lastEventAt) {
        return [];
      }

      return [{
        id: `event:${device.id}`,
        roomId: device.roomId,
        deviceId: device.id,
        type: device.lastEventType,
        title: device.attention ? device.lastEventDescription : `${device.name}: ${device.status}`,
        description: device.attention
          ? device.lastEventDescription
          : `${device.name} reports normal state in this room.`,
        occurredAt: device.lastEventAt,
        source: device.sourceLabel,
        icon: device.attention ? iconForEvent(device.lastEventType) : nominalIcon,
        tone: device.attention ? device.tone : 'green',
      }];
    })
    .sort((left, right) => toUnix(right.occurredAt) - toUnix(left.occurredAt));
}

function buildRoomMetrics(
  devices: ResolvedDevice[],
  events: EventItem[],
  roomSmdIvsCounts: RoomSmdIvsCountsByRoom,
): Map<string, RoomMetrics> {
  const metrics = new Map<string, RoomMetrics>();

  for (const device of devices) {
    const current = metrics.get(device.roomId) ?? emptyRoomMetrics();

    current.deviceCount += 1;
    current.onlineCount += device.isOnline ? 1 : 0;
    current.attentionCount += device.attention ? 1 : 0;
    current.cameraCount += device.cameraLike ? 1 : 0;
    current.dahuaCameraCount += device.integration === 'dahua' && device.cameraLike ? 1 : 0;
    current.sensorCount += device.sensorLike ? 1 : 0;
    applyDeviceSafetyCapability(current.safety, device);
    current.safety.smokeHigh += device.activeSafety?.smoke ? 1 : 0;
    current.safety.coHigh += device.activeSafety?.co ? 1 : 0;
    applyDeviceGridPower(current.gridPower, device);
    metrics.set(device.roomId, current);
  }

  for (const event of events) {
    const current = metrics.get(event.roomId);
    if (current && devices.find((device) => device.id === event.deviceId)?.integration !== 'ajax') {
      applySafetyEvent(current.safety, event.type);
    }
    if (current && current.latestEventLabel === 'No recent events') {
      current.latestEventLabel = event.title;
    }
  }

  for (const [roomId, counts] of Object.entries(roomSmdIvsCounts)) {
    const current = metrics.get(roomId);
    if (!current) {
      continue;
    }
    current.smdIvs = { ...counts };
    metrics.set(roomId, current);
  }

  return metrics;
}

function emptyRoomMetrics(): RoomMetrics {
  return {
    deviceCount: 0,
    onlineCount: 0,
    attentionCount: 0,
    cameraCount: 0,
    dahuaCameraCount: 0,
    sensorCount: 0,
    smdIvs: emptySmdIvsCounts(),
    safety: emptyRoomSafety(),
    gridPower: emptyGridPowerSummary(),
    latestEventLabel: 'No recent events',
  };
}

function emptyRoomSafety(): RoomSafety {
  return { smokeHigh: 0, coHigh: 0, smokeCapable: 0, coCapable: 0 };
}

function emptyGridPowerSummary(): GridPowerSummary {
  return { known: 0, online: 0, outage: 0 };
}

function applyDeviceGridPower(summary: GridPowerSummary, device: ResolvedDevice): void {
  const state = readGridPowerState(device);
  if (state === null) {
    return;
  }
  summary.known += 1;
  if (state) {
    summary.online += 1;
  } else {
    summary.outage += 1;
  }
}

function applyDeviceSafetyCapability(safety: RoomSafety, device: ResolvedDevice): void {
  if (deviceMeasuresSmoke(device)) {
    safety.smokeCapable = (safety.smokeCapable ?? 0) + 1;
  }
  if (deviceMeasuresCo(device)) {
    safety.coCapable = (safety.coCapable ?? 0) + 1;
  }
}

function deviceMeasuresSmoke(device: ResolvedDevice): boolean {
  const text = safetyCapabilityText(device);
  return (
    device.type === 'smoke_detector' ||
    device.type === 'fire_detector' ||
    /fire[\s._-]*protect/.test(text) ||
    /(^|[\s._-])smoke($|[\s._-])/.test(text)
  );
}

function deviceMeasuresCo(device: ResolvedDevice): boolean {
  const text = safetyCapabilityText(device);
  return (
    /life[\s._-]*quality/.test(text) ||
    /fire[\s._-]*protect[\s._-]*(2[\s._-]*)?plus/.test(text) ||
    /(^|[\s._-])(co|co2|carbon|gas|gas_or_co)($|[\s._-])/.test(text)
  );
}

function safetyCapabilityText(device: ResolvedDevice): string {
  const metricText = (device.metrics ?? [])
    .map((metric) => `${metric.id} ${metric.label}`)
    .join(' ');
  return `${device.type} ${device.name} ${device.model} ${device.entityId} ${metricText}`.toLowerCase();
}

function applySafetyEvent(safety: RoomSafety, eventType: EventType): void {
  if (eventType === 'smoke_detected' || eventType === 'fire_detected') {
    safety.smokeHigh += 1;
  }
  if (eventType === 'gas_detected') {
    safety.coHigh += 1;
  }
}

function buildRoom(
  area: HomeAssistantArea,
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  metrics?: RoomMetrics,
): Room {
  const accent = accentForRoom(area.name);
  const image = resolveRoomImage(area, entityEntries, states);
  const climate = buildRoomClimate(entityEntries, states);
  const counts = metrics ?? {
    deviceCount: 0,
    onlineCount: 0,
    attentionCount: 0,
    cameraCount: 0,
    dahuaCameraCount: 0,
    sensorCount: 0,
    smdIvs: emptySmdIvsCounts(),
    safety: emptyRoomSafety(),
    gridPower: emptyGridPowerSummary(),
    latestEventLabel: 'No recent events',
  };

  return {
    id: area.area_id,
    name: area.name,
    type: slugPart(area.name),
    summary: buildRoomSummary(counts),
    heroLabel: 'Home Assistant area',
    image,
    accent,
    icon: iconForRoom(area.name),
    statusTone: counts.attentionCount > 0 ? 'amber' : counts.onlineCount === 0 ? 'red' : 'green',
    smdIvs: counts.smdIvs,
    dahuaCameraCount: counts.dahuaCameraCount,
    climate,
    safety: counts.safety,
    gridPower: counts.gridPower.known > 0 ? { ...counts.gridPower } : undefined,
  };
}

function buildSystemState(
  states: Record<string, HomeAssistantState>,
  rooms: Room[],
  devices: ResolvedDevice[],
  entities: HomeAssistantEntityEntry[] = [],
  accountFilter?: string,
): SystemState {
  const alertCount = devices.filter((device) => device.attention).length;
  const smdIvsTotals = rooms.reduce<RoomSmdIvsCounts>((totals, room) => {
    const counts = room.smdIvs ?? emptySmdIvsCounts();
    totals.total += counts.total;
    totals.human += counts.human;
    totals.vehicle += counts.vehicle;
    totals.animal += counts.animal;
    totals.ivs += counts.ivs;
    return totals;
  }, emptySmdIvsCounts());
  const outletDevices = devices.filter(isOutletOrWallSwitchDevice);
  const lightSwitchDevices = devices.filter(isLightSwitchDevice);
  const outletOnCount = outletDevices.filter(deviceIsOn).length;
  const lightSwitchOnCount = lightSwitchDevices.filter(deviceIsOn).length;
  const gridPower = summarizeGridPower(devices);
  const accountEntities = entities.filter((entry) => {
    const owner = ajaxEntityOwner(entry.unique_id, states[entry.entity_id]?.attributes ?? {});
    return owner?.startsWith('account_') && (!accountFilter || owner === `account_${accountFilter.toLowerCase()}`);
  });
  const accountModes = accountEntities
    .filter((entry) => ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) === 'mode')
    .map((entry) => safeString(states[entry.entity_id]?.state))
    .filter(Boolean);
  const alarmActive = accountEntities.some(
    (entry) => ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) === 'alarm_active' && isOn(states[entry.entity_id]),
  ) || devices.some((device) => device.securityAlarm);
  const armed = accountModes.some((mode) => mode === 'armed' || mode === 'night');
  const primaryMode = alarmActive ? 'Alarm' : accountModes[0] ? humanizeSlug(accountModes[0]) : 'Monitoring';
  const chips: DashboardChip[] = [
    {
      id: 'system-mode',
      label: 'Security mode',
      value: primaryMode,
      icon: { category: 'system-states', key: alarmActive ? 'alarm_active' : armed ? 'armed' : 'disarmed' },
      tone: alarmActive ? 'red' : armed ? 'amber' : 'green',
      active: true,
    },
    {
      id: 'system-grid-power',
      label: 'Grid power',
      value: gridPower.known > 0
        ? gridPower.outage > 0
          ? `${gridPower.outage} outage`
          : `${gridPower.online}/${gridPower.known} OK`
        : 'Unknown',
      icon: { category: 'system-states', key: gridPower.outage > 0 ? 'power_loss' : 'grid_power' },
      tone: gridPower.outage > 0 ? 'red' : gridPower.known > 0 ? 'green' : 'slate',
      active: gridPower.known > 0,
    },
    {
      id: 'system-smd',
      label: 'SMD today',
      value: String(smdIvsTotals.human + smdIvsTotals.vehicle + smdIvsTotals.animal),
      icon: { category: 'events', key: 'human_detected' },
      tone: 'violet',
      active: smdIvsTotals.human + smdIvsTotals.vehicle + smdIvsTotals.animal > 0,
    },
    {
      id: 'system-ivs',
      label: 'IVS today',
      value: String(smdIvsTotals.ivs),
      icon: { category: 'events', key: 'tripwire_detected' },
      tone: 'amber',
      active: smdIvsTotals.ivs > 0,
    },
    {
      id: 'system-outlets',
      label: 'Outlets',
      value: `${outletOnCount}/${outletDevices.length || 0}`,
      icon: { category: 'devices', key: 'smart_plug' },
      tone: outletOnCount > 0 ? 'green' : 'slate',
      active: outletDevices.length > 0 && outletOnCount > 0,
    },
    {
      id: 'system-lights',
      label: 'Light switches',
      value: `${lightSwitchOnCount}/${lightSwitchDevices.length || 0}`,
      icon: { category: 'devices', key: 'light_switch' },
      tone: lightSwitchOnCount > 0 ? 'green' : 'slate',
      active: lightSwitchDevices.length > 0 && lightSwitchOnCount > 0,
    },
  ];

  if (alertCount > 0) {
    chips.push({
      id: 'system-alerts',
      label: 'Alerts',
      value: String(alertCount),
      icon: { category: 'misc', key: 'alert' },
      tone: 'amber',
      active: true,
    });
  }

  return { chips };
}

function isOutletOrWallSwitchDevice(device: ResolvedDevice): boolean {
  return device.type === 'smart_plug'
    || device.type === 'wall_switch';
}

function isLightSwitchDevice(device: ResolvedDevice): boolean {
  return device.type === 'light_switch' || /lightswitch|light switch|\blight\b/.test(deviceDescriptor(device));
}

function deviceIsOn(device: ResolvedDevice): boolean {
  const control = device.actions?.find((action) => action.domain === 'switch' || action.domain === 'valve');
  if (control) return control.controlState === 'on';
  if (device.actions?.some((action) => action.service === 'turn_off')) {
    return true;
  }
  if (device.actions?.some((action) => action.service === 'turn_on')) {
    return false;
  }
  if (device.metrics?.some((metric) => metric.label.toLowerCase().includes('switch') && metric.value.toLowerCase() === 'on')) {
    return true;
  }
  return false;
}

function summarizeGridPower(devices: ResolvedDevice[]): GridPowerSummary {
  return devices.reduce<GridPowerSummary>((summary, device) => {
    applyDeviceGridPower(summary, device);
    return summary;
  }, emptyGridPowerSummary());
}

function readGridPowerState(device: Pick<Device, 'type' | 'name' | 'model' | 'metrics'>): boolean | null {
  if (!isGridPowerDetector(device)) {
    return null;
  }

  const metric = (device.metrics ?? []).find((candidate) => {
    const label = candidate.label.toLowerCase();
    return label.includes('grid power') || label === 'power';
  });
  if (!metric) {
    return null;
  }

  const value = metric.value.toLowerCase();
  if (/\bmains\b|\bon\b|\bok\b|online|restored|available/.test(value)) {
    return true;
  }
  if (/off|lost|outage|unavailable|offline|fail/.test(value)) {
    return false;
  }
  return null;
}

function isGridPowerDetector(device: Pick<Device, 'type' | 'name' | 'model'>): boolean {
  const text = `${device.type} ${device.name} ${device.model}`.toLowerCase();
  return /(^|[\s_-])transmitter($|[\s_-])|transmitter_jeweller|superior_transmitter/.test(text);
}

function deviceDescriptor(device: ResolvedDevice): string {
  return `${device.type} ${device.name} ${device.model} ${device.entityId}`.toLowerCase();
}

function dahuaBridgeChannelSignature(
  states: Record<string, HomeAssistantState>,
  registryIndex: RegistryIndex,
  dahuaBase: string,
): string {
  return discoverDahuaBridgeChannels(states, registryIndex, dahuaBase)
    .map((camera) => `${camera.roomId}:${camera.bridgeBaseUrl}:${camera.rootDeviceId}:${camera.channel}`)
    .sort()
    .join('|');
}

async function loadSmdIvsCountsByRoom(
  states: Record<string, HomeAssistantState>,
  registryIndex: RegistryIndex,
  dahuaBase: string,
  signal: AbortSignal,
): Promise<RoomSmdIvsCountsByRoom> {
  const channels = discoverDahuaBridgeChannels(states, registryIndex, dahuaBase);
  if (channels.length === 0) {
    return {};
  }

  const end = new Date();
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000);
  const nvrGroups = new Map<string, {
    bridgeBaseUrl: string;
    rootDeviceId: string;
    channels: DahuaBridgeChannel[];
  }>();

  for (const channel of channels) {
    const key = `${channel.bridgeBaseUrl}|${channel.rootDeviceId}`;
    const group = nvrGroups.get(key) ?? {
      bridgeBaseUrl: channel.bridgeBaseUrl,
      rootDeviceId: channel.rootDeviceId,
      channels: [],
    };
    group.channels.push(channel);
    nvrGroups.set(key, group);
  }

  const roomCounts: RoomSmdIvsCountsByRoom = {};

  await Promise.all([...nvrGroups.values()].map(async (group) => {
    try {
      const summary = await fetchSmdIvsSummary(group.bridgeBaseUrl, group.rootDeviceId, start, end, signal);
      const summaryByChannel = new Map(
        (summary.channels ?? []).map((entry) => [numberFromUnknown(entry.channel), entry]),
      );

      for (const channel of group.channels) {
        const channelSummary = summaryByChannel.get(channel.channel);
        if (!channelSummary) {
          continue;
        }

        const counts = roomCounts[channel.roomId] ?? emptySmdIvsCounts();
        counts.total += numberFromUnknown(channelSummary.total_count);
        addSmdIvsSummaryItems(counts, channelSummary.items);
        roomCounts[channel.roomId] = counts;
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw error;
      }
    }
  }));

  return roomCounts;
}

function discoverDahuaBridgeChannels(
  states: Record<string, HomeAssistantState>,
  registryIndex: RegistryIndex,
  dahuaBase: string,
): DahuaBridgeChannel[] {
  const channels: DahuaBridgeChannel[] = [];

  for (const [entityId, state] of Object.entries(states)) {
    if (!entityId.startsWith('camera.')) {
      continue;
    }

    const attrs = state.attributes ?? {};
    const kind = safeString(attrs.bridge_device_kind);
    const bridgeBaseUrl = resolveDahuaBridgeBaseUrl(safeString(attrs.bridge_base_url), dahuaBase);
    const rootDeviceId = safeString(attrs.bridge_root_device_id);
    const channel = numberFromUnknown(attrs.bridge_channel);
    const roomId = resolveRoomIdForEntity(entityId, registryIndex);

    if (kind !== 'nvr_channel' || !bridgeBaseUrl || !rootDeviceId || channel <= 0 || !roomId) {
      continue;
    }

    channels.push({
      entityId,
      roomId,
      bridgeBaseUrl,
      rootDeviceId,
      channel,
    });
  }

  return channels;
}

function resolveRoomIdForEntity(entityId: string, registryIndex: RegistryIndex): string | null {
  const entityEntry = registryIndex.entityById.get(entityId);
  if (entityEntry?.area_id && registryIndex.areaById.has(entityEntry.area_id)) {
    return entityEntry.area_id;
  }

  let deviceId = safeString(entityEntry?.device_id);
  const seen = new Set<string>();

  while (deviceId && !seen.has(deviceId)) {
    seen.add(deviceId);
    const directAreaId = registryIndex.resolvedAreaByDeviceId.get(deviceId);
    if (directAreaId && registryIndex.areaById.has(directAreaId)) {
      return directAreaId;
    }

    const device = registryIndex.deviceById.get(deviceId);
    if (!device) {
      break;
    }
    if (device.area_id && registryIndex.areaById.has(device.area_id)) {
      return device.area_id;
    }

    deviceId = safeString(device.via_device_id);
  }

  return null;
}

async function fetchSmdIvsSummary(
  bridgeBaseUrl: string,
  rootDeviceId: string,
  start: Date,
  end: Date,
  signal: AbortSignal,
): Promise<SmdIvsSummaryResponse> {
  const url = new URL(
    `${bridgeBaseUrl}/api/v1/nvr/${encodeURIComponent(rootDeviceId)}/events/summary`,
    window.location.origin,
  );
  url.searchParams.set('start', start.toISOString());
  url.searchParams.set('end', end.toISOString());
  url.searchParams.set('event', 'all');

  const response = await fetch(url, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal,
  });
  if (!response.ok) {
    throw new Error(`DahuaBridge summary failed: HTTP ${response.status}`);
  }

  return response.json() as Promise<SmdIvsSummaryResponse>;
}

function normalizeDahuaBaseUrl(value: unknown): string {
  return safeString(value).replace(/\/+$/, '');
}

function resolveDahuaBridgeBaseUrl(attributeBaseUrl: string, dahuaBase: string): string {
  return normalizeDahuaBaseUrl(dahuaBase || attributeBaseUrl);
}

function emptySmdIvsCounts(): RoomSmdIvsCounts {
  return { total: 0, human: 0, vehicle: 0, animal: 0, ivs: 0 };
}

function addSmdIvsSummaryItems(target: RoomSmdIvsCounts, items: SmdIvsSummaryItem[] = []): void {
  for (const item of items) {
    const code = safeString(item.code).toLowerCase();
    const count = numberFromUnknown(item.count);

    if (code === 'human') {
      target.human += count;
    } else if (code === 'vehicle') {
      target.vehicle += count;
    } else if (code === 'animal') {
      target.animal += count;
    } else if (code === 'tripwire' || code === 'intrusion') {
      target.ivs += count;
    }
  }
}

function numberFromUnknown(value: unknown): number {
  const numberValue = Number(value);
  return Number.isFinite(numberValue) && numberValue > 0 ? numberValue : 0;
}

function toPublicDevice(device: ResolvedDevice): Device {
  return {
    id: device.id,
    roomId: device.roomId,
    type: device.type,
    name: device.name,
    model: device.model,
    icon: device.icon,
    tone: device.tone,
    status: device.status,
    connectivity: device.connectivity,
    battery: device.battery,
    signal: device.signal,
    entityId: device.entityId,
    isOnline: device.isOnline,
    attention: device.attention,
    heroMedia: device.heroMedia,
    actions: device.actions,
    metrics: device.metrics,
  };
}

function groupByDeviceId(entities: HomeAssistantEntityEntry[]): Map<string, HomeAssistantEntityEntry[]> {
  const grouped = new Map<string, HomeAssistantEntityEntry[]>();
  for (const entry of entities) {
    if (!entry.device_id) {
      continue;
    }
    const current = grouped.get(entry.device_id) ?? [];
    current.push(entry);
    grouped.set(entry.device_id, current);
  }
  return grouped;
}

function groupByAreaId(
  entities: HomeAssistantEntityEntry[],
  resolvedAreaByDeviceId: Map<string, string>,
): Map<string, HomeAssistantEntityEntry[]> {
  const grouped = new Map<string, HomeAssistantEntityEntry[]>();
  for (const entry of entities) {
    const areaId = entry.area_id ?? resolvedAreaByDeviceId.get(entry.device_id ?? '');
    if (!areaId) {
      continue;
    }
    const current = grouped.get(areaId) ?? [];
    current.push(entry);
    grouped.set(areaId, current);
  }
  return grouped;
}

function resolveAreaId(
  deviceEntry: HomeAssistantDeviceEntry,
  linkedEntities: HomeAssistantEntityEntry[],
  areaIdByName: Map<string, string>,
): string | null {
  if (deviceEntry.area_id) {
    return deviceEntry.area_id;
  }
  const entityAreaId = linkedEntities.find((entry) => entry.area_id)?.area_id;
  if (entityAreaId) {
    return entityAreaId;
  }
  const suggestedArea = safeString(deviceEntry.suggested_area);
  if (suggestedArea) {
    return areaIdByName.get(slugPart(suggestedArea)) ?? null;
  }
  return null;
}

function firstEntityState(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  suffix: string,
): HomeAssistantState | undefined {
  const match = entries.find((entry) => ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) === suffix.replace(/^_/, ''));
  return match ? states[match.entity_id] : undefined;
}

function displayName(deviceEntry: HomeAssistantDeviceEntry, linkedEntities: HomeAssistantEntityEntry[], states: Record<string, HomeAssistantState> = {}): string {
  const registryName = safeString(deviceEntry.name);
  const genericName = /^(?:ajax\s+)?(?:zone|sia|device)[\s_\d-]+$/i.test(registryName);
  const telemetryName = linkedEntities.map((entry) => safeString(states[entry.entity_id]?.attributes.device)).find((name) => name && !/^(?:ajax\s+)?zone\s+\d+$/i.test(name));
  return (
    safeString(deviceEntry.name_by_user) ||
    (genericName ? telemetryName || registryName : registryName) ||
    linkedEntities.map((entry) => safeString(entry.name) || safeString(entry.original_name)).find(Boolean) ||
    'Unknown device'
  );
}

function entityDisplayName(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): string {
  return (
    safeString(state?.attributes.friendly_name) ||
    safeString(entry.name) ||
    safeString(entry.original_name) ||
    humanizeSlug(entityIdLeaf(entry.entity_id))
  );
}

function isAjaxDevice(deviceEntry: HomeAssistantDeviceEntry): boolean {
  return /^ajax(?: systems| via jeedom)?$/i.test(safeString(deviceEntry.manufacturer))
    || extractIdentifiers(deviceEntry).some((identifier) => /^ajaxbridge_/i.test(identifier));
}

function isLegacyAjax2PrometheusDevice(deviceEntry: HomeAssistantDeviceEntry): boolean {
  return LEGACY_AJAX2PROM_HINT.test(
    [deviceEntry.manufacturer, deviceEntry.model, deviceEntry.name, deviceEntry.name_by_user, ...extractIdentifiers(deviceEntry)]
      .map(safeString)
      .join(' '),
  );
}

function isLegacyAjax2PrometheusEntity(entityEntry: HomeAssistantEntityEntry): boolean {
  return LEGACY_AJAX2PROM_HINT.test(
    [
      entityEntry.entity_id,
      entityEntry.platform,
      entityEntry.unique_id,
      entityEntry.name,
      entityEntry.original_name,
      entityEntry['unique_id'],
    ]
      .map(safeString)
      .join(' '),
  );
}

function isAjaxAccountDevice(deviceEntry: HomeAssistantDeviceEntry): boolean {
  return safeString(deviceEntry.model).toLowerCase().includes('account') || extractIdentifiers(deviceEntry).some((value) => value.startsWith('ajaxbridge_account_'));
}

function extractAjaxAccount(deviceEntry: HomeAssistantDeviceEntry): string | null {
  for (const identifier of extractIdentifiers(deviceEntry)) {
    if (identifier.startsWith('ajaxbridge_account_')) {
      return identifier.slice('ajaxbridge_account_'.length);
    }
    const zoneIndex = identifier.indexOf('_zone_');
    if (identifier.startsWith('ajaxbridge_') && zoneIndex > 'ajaxbridge_'.length) {
      return identifier.slice('ajaxbridge_'.length, zoneIndex);
    }
  }
  return null;
}

function extractIdentifiers(deviceEntry: HomeAssistantDeviceEntry): string[] {
  const raw = deviceEntry.identifiers;
  if (!Array.isArray(raw)) {
    return [];
  }

  const output: string[] = [];
  for (const entry of raw) {
    if (Array.isArray(entry)) {
      output.push(...entry.map(safeString).filter(Boolean));
      output.push(entry.map((value) => safeString(value)).filter(Boolean).join(':'));
      output.push(entry.map((value) => safeString(value)).filter(Boolean).join('_'));
      continue;
    }
    output.push(safeString(entry));
  }
  return output.filter(Boolean);
}

function isDahuaDevice(deviceEntry: HomeAssistantDeviceEntry, linkedEntities: HomeAssistantEntityEntry[]): boolean {
  if (isIgnoredDahuaDevice(deviceEntry, linkedEntities)) {
    return false;
  }

  if (DAHUA_HINT.test(
    [deviceEntry.manufacturer, deviceEntry.model, deviceEntry.name, deviceEntry.name_by_user, ...extractIdentifiers(deviceEntry)]
      .map(safeString)
      .join(' '),
  )) {
    return true;
  }

  return linkedEntities.some((entry) => isRelevantDahuaEntity(entry, deviceEntry));
}

function isRelevantDahuaEntity(
  entityEntry: HomeAssistantEntityEntry,
  deviceEntry?: HomeAssistantDeviceEntry,
): boolean {
  if (isIgnoredDahuaDevice(deviceEntry, [entityEntry]) || isEntityHiddenOrDisabled(entityEntry)) {
    return false;
  }

  const text = [
    entityEntry.entity_id,
    entityEntry.platform,
    entityEntry.name,
    entityEntry.original_name,
    deviceEntry?.manufacturer,
    deviceEntry?.model,
    deviceEntry?.name,
    deviceEntry?.name_by_user,
    ...extractIdentifiers(deviceEntry ?? { id: '' }),
  ]
    .map(safeString)
    .join(' ');

  if (!DAHUA_HINT.test(text)) {
    return false;
  }

  const domain = entityDomain(entityEntry.entity_id);
  return DAHUA_ENTITY_DOMAINS.has(domain);
}

function pickPrimaryEntity(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): HomeAssistantEntityEntry | null {
  return (
    entries.find((entry) => isCameraDomain(entry.entity_id) && hasAvailableState(states[entry.entity_id]) && Boolean(readEntityPicture(states[entry.entity_id]))) ??
    entries.find((entry) => isCameraDomain(entry.entity_id) && hasAvailableState(states[entry.entity_id])) ??
    entries.find((entry) => hasAvailableState(states[entry.entity_id])) ??
    entries.find((entry) => isCameraDomain(entry.entity_id) && Boolean(readEntityPicture(states[entry.entity_id]))) ??
    entries.find((entry) => isCameraDomain(entry.entity_id)) ??
    entries.find((entry) => Boolean(states[entry.entity_id])) ??
    entries[0] ??
    null
  );
}

function isIgnoredDahuaEntity(
  deviceEntry: HomeAssistantDeviceEntry | undefined,
  entityEntry: HomeAssistantEntityEntry,
  state?: HomeAssistantState,
): boolean {
  if (isEntityHiddenOrDisabled(entityEntry)) {
    return true;
  }

  return isVtoLikeDevice(deviceEntry, [entityEntry], state ? { [entityEntry.entity_id]: state } : undefined)
    && looksLikeVtoStreamEntity(entityEntry, state);
}

function isEntityHiddenOrDisabled(entityEntry: HomeAssistantEntityEntry): boolean {
  return Boolean(safeString(entityEntry.hidden_by) || safeString(entityEntry.disabled_by));
}

function isVtoLikeDevice(
  deviceEntry?: HomeAssistantDeviceEntry,
  linkedEntities: HomeAssistantEntityEntry[] = [],
  states?: Record<string, HomeAssistantState>,
): boolean {
  const text = [
    deviceEntry?.manufacturer,
    deviceEntry?.model,
    deviceEntry?.name,
    deviceEntry?.name_by_user,
    ...extractIdentifiers(deviceEntry ?? { id: '' }),
    ...linkedEntities.flatMap((entry) => [
      entry.entity_id,
      entry.name,
      entry.original_name,
      states?.[entry.entity_id]?.attributes.friendly_name,
    ]),
  ]
    .map(safeString)
    .join(' ');

  return VTO_DEBUG_HINT.test(text);
}

function looksLikeVtoStreamEntity(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): boolean {
  if (isCameraDomain(entry.entity_id)) {
    return true;
  }

  const text = [
    entry.entity_id,
    entry.name,
    entry.original_name,
    state?.attributes.friendly_name,
  ]
    .map(safeString)
    .join(' ')
    .toLowerCase();

  return /(^|[\s._-])(main|sub)([\s._-]|$)|cam channel|sub channel/.test(text);
}

function isHeroMediaEntity(entry: HomeAssistantEntityEntry): boolean {
  return isCameraDomain(entry.entity_id);
}

function isActionableEntity(entry: HomeAssistantEntityEntry): boolean {
  const domain = entityDomain(entry.entity_id);
  return domain === 'button' || domain === 'switch' || domain === 'lock' || domain === 'valve';
}

function buildHeroMedia(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): DeviceHeroMedia | undefined {
  const entry = pickPrimaryEntity(entries, states);
  if (!entry) {
    return undefined;
  }

  const state = states[entry.entity_id];
  const picture = readEntityPicture(state);
  const title = entityDisplayName(entry, state);
  if (entityDomain(entry.entity_id) === 'camera') {
    const streamEntityId = resolvePreferredStreamEntity(entry, entries, states);
    const streamPicture = streamEntityId ? readEntityPicture(states[streamEntityId]) : '';
    const posterSrc = picture || streamPicture || `/api/camera_proxy/${streamEntityId ?? entry.entity_id}`;

    if (streamEntityId) {
      return {
        entityId: streamEntityId,
        title,
        kind: 'stream',
        src: `/api/camera_proxy_stream/${streamEntityId}`,
        posterSrc,
      };
    }

    return {
      entityId: entry.entity_id,
      title,
      kind: 'image',
      src: picture || `/api/camera_proxy/${entry.entity_id}`,
      posterSrc,
    };
  }

  if (!picture) {
    return undefined;
  }

  return {
    entityId: entry.entity_id,
    title,
    kind: 'image',
    src: picture,
    posterSrc: picture,
  };
}

function buildDeviceActions(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  context: DeviceActionContext = {},
): DeviceAction[] | undefined {
  const valvePosition = readValvePosition(context.linkedEntries ?? [], states);
  const valveEntry = context.linkedEntries?.find((entry) => ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) === 'valve_position');
  const observedState = ['wall_switch', 'relay'].includes(context.deviceType ?? '')
    ? context.linkedEntries?.find((entry) => entityDomain(entry.entity_id) === 'binary_sensor' && ajaxEntitySemantic(entry, states[entry.entity_id]?.attributes) === 'state')
    : undefined;
  const actions = entries
    .map((entry) => buildDeviceAction(entry, states[observedState && entityDomain(entry.entity_id) !== 'button' ? observedState.entity_id : entry.entity_id], context.deviceType, valvePosition))
    .filter((action): action is DeviceAction => action !== null)
    .map((action) => ({ ...action, valvePositionEntityId: valveEntry?.entity_id, observedStateEntityId: observedState?.entity_id ?? action.observedStateEntityId }))
    .sort(sortDeviceActions);

  return actions.length > 0 ? actions : undefined;
}

function buildDeviceAction(
  entry: HomeAssistantEntityEntry,
  state?: HomeAssistantState,
  deviceType?: string,
  valvePosition?: string,
): DeviceAction | null {
  const domain = entityDomain(entry.entity_id);
  if (domain !== 'button' && domain !== 'switch' && domain !== 'lock' && domain !== 'valve') {
    return null;
  }

  const semantic = actionSemantic(entry, state);
  const valveSemantics = domain === 'valve' || deviceType === 'waterstop';
  const controlState = domain === 'switch' || domain === 'valve' || domain === 'lock'
    ? resolveDeviceControlState({ domain, rawState: state?.state, deviceType, valvePosition })
    : undefined;
  const label = actionLabel(entry, state, semantic, controlState, valveSemantics);
  const service = actionService(domain, semantic, controlState);
  if (!label) {
    return null;
  }

  return {
    id: `action:${entry.entity_id}:${service}`,
    entityId: entry.entity_id,
    label,
    domain,
    service,
    stateLabel: controlState ? controlStateLabel(controlState, valveSemantics) : state ? humanizeHomeAssistantState(state) : undefined,
    controlState,
    observedStateEntityId: controlState ? entry.entity_id : undefined,
    disabled: controlState !== undefined && controlState !== 'on' && controlState !== 'off',
    disabledReason: controlState !== undefined && controlState !== 'on' && controlState !== 'off' ? 'Wait for a known device state' : undefined,
    confirmation: (deviceType === 'hub' || deviceType === 'app') && domain === 'button' ? `Confirm ${label}?` : undefined,
  };
}

function readValvePosition(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): string | undefined {
  for (const entry of entries) {
    const state = states[entry.entity_id];
    if (!state) {
      continue;
    }
    const descriptor = entityDescriptorText(entry, state);
    const deviceClass = safeString(state.attributes.device_class);
    if (classifyAjaxDiagnosticMetric(descriptor, deviceClass) === 'valve_position') {
      return state.state;
    }
  }
  return undefined;
}

function actionSemantic(
  entry: HomeAssistantEntityEntry,
  state?: HomeAssistantState,
): 'open_door' | 'hang_up' | 'answer' | 'mute' | 'generic' {
  const text = dahuaEntityText(entry, state);
  if (/open door|unlock|door release|door relay|gate|strike|vto lock|_lock_\d+/.test(text)) {
    return 'open_door';
  }
  if (/hang ?up|end call|reject|decline|cancel call/.test(text)) {
    return 'hang_up';
  }
  if (/answer|accept/.test(text)) {
    return 'answer';
  }
  if (/mute/.test(text)) {
    return 'mute';
  }
  return 'generic';
}

function actionLabel(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState | undefined,
  semantic: 'open_door' | 'hang_up' | 'answer' | 'mute' | 'generic',
  controlState?: DeviceAction['controlState'],
  valveSemantics = false,
): string {
  if (controlState && controlState !== 'on' && controlState !== 'off') return 'Control unavailable';
  switch (semantic) {
    case 'open_door':
      return 'Open door';
    case 'hang_up':
      return 'Hang up';
    case 'answer':
      return 'Answer';
    case 'mute':
      return 'Mute';
    default: {
      if (entityDomain(entry.entity_id) === 'lock') {
        return controlState === 'on' ? 'Lock door' : 'Unlock';
      }
      if (valveSemantics) {
        return controlState === 'on' ? 'Close' : 'Open';
      }
      if (entityDomain(entry.entity_id) === 'switch') {
        return controlState === 'on' ? 'Turn off' : 'Turn on';
      }
      if (entityDomain(entry.entity_id) === 'valve') {
        return controlState === 'on' ? 'Close' : 'Open';
      }
      return entityDisplayName(entry, state);
    }
  }
}

function actionService(
  domain: DeviceActionDomain,
  semantic: 'open_door' | 'hang_up' | 'answer' | 'mute' | 'generic',
  controlState?: DeviceAction['controlState'],
): string {
  if (domain === 'button') {
    return 'press';
  }
  if (controlState !== 'on' && controlState !== 'off') return '';
  if (domain === 'lock') {
    return controlState === 'on' && semantic === 'generic' ? 'lock' : 'unlock';
  }
  if (domain === 'valve') {
    return controlState === 'on' ? 'close_valve' : 'open_valve';
  }
  if (semantic !== 'generic') {
    return 'turn_on';
  }
  return controlState === 'on' ? 'turn_off' : 'turn_on';
}

function sortDeviceActions(left: DeviceAction, right: DeviceAction): number {
  return actionPriority(left.label) - actionPriority(right.label) || left.label.localeCompare(right.label);
}

function actionPriority(label: string): number {
  const text = label.toLowerCase();
  if (text.includes('open door')) {
    return 0;
  }
  if (text.includes('answer')) {
    return 1;
  }
  if (text.includes('hang up')) {
    return 2;
  }
  if (text.includes('mute')) {
    return 3;
  }
  return 10;
}

function buildDeviceMetrics(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  ajaxContext?: AjaxDeviceMetricContext,
  options: DeviceMetricOptions = {},
): DashboardMetric[] | undefined {
  const candidates: MetricCandidate[] = [];

  if (ajaxContext) {
    candidates.push(...ajaxContextMetrics(ajaxContext));
  }

  for (const entry of entries) {
    const relayState = relayStateMetricCandidate(entry, states[entry.entity_id], options.deviceType);
    if (relayState) {
      candidates.push(relayState);
      continue;
    }
    const candidate = metricCandidateFromEntity(entry, states[entry.entity_id]);
    if (candidate) {
      candidates.push(candidate);
    }
  }

  const sourceMetrics = options.calculateApparentPower
    ? candidates.filter((candidate) => candidate.kind !== 'power')
    : candidates;
  if (options.calculateApparentPower) {
    const apparentPower = calculatedApparentPowerMetric(entries, states);
    if (apparentPower) {
      sourceMetrics.push(apparentPower);
    }
  }

  const metrics = dedupeMetrics(sourceMetrics)
    .sort((left, right) => left.priority - right.priority || left.label.localeCompare(right.label))
    .slice(0, MAX_DEVICE_METRICS)
    .map(({ kind: _kind, priority: _priority, ...metric }) => metric);

  return metrics.length > 0 ? metrics : undefined;
}

function relayStateMetricCandidate(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState | undefined,
  deviceType?: string,
): MetricCandidate | null {
  if (!isAuthoritativeRelayStateEntity({
    deviceType: deviceType ?? '',
    entityId: entry.entity_id,
    name: entry.name,
    originalName: entry.original_name,
    friendlyName: state?.attributes.friendly_name,
  })) {
    return null;
  }

  const controlState = resolveRelayState(state?.state);
  return {
    id: `metric:relay_state:${entry.entity_id}`,
    kind: 'relay_state',
    label: 'State',
    value: controlStateLabel(controlState),
    icon: { category: 'devices', key: 'relay' },
    tone: controlState === 'on' ? 'green' : controlState === 'off' ? 'slate' : 'amber',
    priority: 18,
  };
}

function calculatedApparentPowerMetric(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): MetricCandidate | null {
  const voltage = readNumericMetric(entries, states, 'voltage');
  const current = readNumericMetric(entries, states, 'current');
  if (!voltage || !current) {
    return null;
  }

  const volts = normalizeVoltageToVolts(voltage.value, voltage.unit);
  const amps = normalizeCurrentToAmps(current.value, current.unit);
  if (volts === null || amps === null) {
    return null;
  }

  return {
    id: `metric:calculated_apparent_power:${voltage.entityId}:${current.entityId}`,
    kind: 'apparent_power',
    label: 'Apparent power',
    value: formatMetricNumber(volts * amps, 'VA', volts * amps >= 100 ? 0 : 1),
    icon: { category: 'misc', key: 'energy' },
    tone: 'cyan',
    priority: 40,
  };
}

function readNumericMetric(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  kind: 'voltage' | 'current',
): { entityId: string; value: number; unit: string } | null {
  for (const entry of entries) {
    const state = states[entry.entity_id];
    if (!state || isIgnoredMetricState(state)) {
      continue;
    }

    const deviceClass = safeString(state.attributes.device_class).toLowerCase();
    const text = entityDescriptorText(entry, state);
    const matches = kind === 'voltage'
      ? deviceClass === 'voltage' || matchesMetricName(text, ['voltage_v', 'tension'])
      : deviceClass === 'current' || matchesMetricName(text, ['current_a', 'courant']);
    if (!matches) {
      continue;
    }

    const value = parseStateNumber(state);
    if (value !== null) {
      return {
        entityId: entry.entity_id,
        value,
        unit: safeString(state.attributes.unit_of_measurement),
      };
    }
  }

  return null;
}

function normalizeVoltageToVolts(value: number, unit: string): number | null {
  const normalizedUnit = unit.trim().toLowerCase();
  if (normalizedUnit === 'v') {
    return value;
  }
  if (normalizedUnit === 'mv') {
    return value / 1000;
  }
  return null;
}

function normalizeCurrentToAmps(value: number, unit: string): number | null {
  const normalizedUnit = unit.trim().toLowerCase();
  if (normalizedUnit === 'a') {
    return value;
  }
  if (normalizedUnit === 'ma') {
    return value / 1000;
  }
  return null;
}

function ajaxContextMetrics(context: AjaxDeviceMetricContext): MetricCandidate[] {
  const metrics: MetricCandidate[] = [];
  const lastSignal = safeString(context.lastSignal);
  const alarmSignal = safeString(context.alarmSignal);
  const lastEventName = safeString(context.lastEventName);

  if (context.alarmActive && alarmSignal && alarmSignal !== 'none') {
    metrics.push({
      id: `metric:alarm_signal:${slugPart(alarmSignal)}`,
      kind: 'alarm_signal',
      label: 'Alarm signal',
      value: humanizeSlug(alarmSignal),
      icon: { category: 'events', key: 'alarm' },
      tone: 'red',
      priority: 12,
    });
  }

  if (context.tamperActive) {
    metrics.push({
      id: 'metric:tamper:active',
      kind: 'tamper',
      label: 'Tamper',
      value: 'Active',
      icon: { category: 'events', key: 'tamper_detected' },
      tone: 'red',
      priority: 14,
    });
  }

  if (context.troubleActive) {
    metrics.push({
      id: 'metric:trouble:active',
      kind: 'trouble',
      label: 'Trouble',
      value: 'Active',
      icon: { category: 'system-states', key: 'trouble' },
      tone: 'amber',
      priority: 15,
    });
  }

  if (lastSignal && lastSignal !== 'idle' && lastSignal !== 'none') {
    metrics.push({
      id: `metric:last_signal:${slugPart(lastSignal)}`,
      kind: 'last_signal',
      label: 'Last signal',
      value: humanizeSlug(lastSignal),
      icon: { category: 'sensors', key: 'signal' },
      tone: context.activeSignals.length > 0 ? 'amber' : 'slate',
      priority: 80,
    });
  }

  if (lastEventName && lastEventName !== 'Awaiting event') {
    metrics.push({
      id: `metric:last_event:${slugPart(lastEventName)}`,
      kind: 'last_event',
      label: 'Last event',
      value: lastEventName,
      icon: { category: 'misc', key: 'history' },
      tone: 'slate',
      priority: 90,
    });
  }

  if (context.lastEventAt) {
    metrics.push({
      id: 'metric:last_event_at',
      kind: 'last_event_at',
      label: 'Updated',
      value: formatShortDateTime(context.lastEventAt),
      icon: { category: 'misc', key: 'history' },
      tone: 'slate',
      priority: 95,
    });
  }

  return metrics;
}

function metricCandidateFromEntity(
  entry: HomeAssistantEntityEntry,
  state?: HomeAssistantState,
): MetricCandidate | null {
  if (!state || isIgnoredMetricState(state)) {
    return null;
  }
  if (['last_event_name', 'last_event_at', 'last_signal', 'alarm_signal'].includes(ajaxEntitySemantic(entry, state.attributes) ?? '')) return null;

  const domain = entityDomain(entry.entity_id);
  const deviceClass = safeString(state.attributes.device_class).toLowerCase();
  const text = entityDescriptorText(entry, state);

  if (domain === 'sensor') {
    return sensorMetricCandidate(entry, state, deviceClass, text);
  }

  if (domain === 'binary_sensor') {
    return binaryMetricCandidate(entry, state, deviceClass, text);
  }

  if (domain === 'switch') {
    const active = isOn(state);
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'switch',
      label: actionMetricLabel(entry, state, 'Switch'),
      value: active ? 'On' : 'Off',
      icon: { category: 'misc', key: 'energy' },
      tone: active ? 'green' : 'slate',
      priority: 35,
    };
  }

  if (domain === 'lock') {
    const unlocked = safeString(state.state).toLowerCase() === 'unlocked';
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'lock',
      label: actionMetricLabel(entry, state, 'Lock'),
      value: unlocked ? 'Unlocked' : humanizeHomeAssistantState(state),
      icon: { category: 'security-states', key: unlocked ? 'unlocked' : 'locked' },
      tone: unlocked ? 'amber' : 'green',
      priority: 34,
    };
  }

  return null;
}

function sensorMetricCandidate(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState,
  deviceClass: string,
  text: string,
): MetricCandidate | null {
  const unit = safeString(state.attributes.unit_of_measurement);
  // powerWtH is cumulative energy. Accept its verified energy metadata, but
  // never display the legacy W label as instantaneous power.
  const cumulativeEnergy = safeString(state.attributes.logical_id).toLowerCase() === 'powerwth';
  const energyUnit = ['mWh', 'Wh', 'kWh', 'MWh', 'GWh', 'TWh', 'J', 'kJ', 'MJ', 'GJ', 'cal', 'kcal', 'Mcal', 'Gcal'].includes(unit);
  if (cumulativeEnergy && (deviceClass !== 'energy' || !energyUnit)) return null;
  const diagnosticKind = classifyAjaxDiagnosticMetric(text, deviceClass);

  if (diagnosticKind === 'valve_position') {
    const controlState = resolveDeviceControlState({
      deviceType: 'waterstop',
      domain: 'valve',
      valvePosition: state.state,
    });
    return {
      id: `metric:${entry.entity_id}`,
      kind: diagnosticKind,
      label: 'Valve position',
      value: controlStateLabel(controlState, true),
      icon: { category: 'devices', key: 'waterstop' },
      tone: ['opening', 'closing', 'intermediate', 'moving'].includes(controlState) ? 'amber' : controlState === 'unknown' ? 'slate' : 'cyan',
      priority: 19,
    };
  }

  if (diagnosticKind === 'issue_count') {
    const issueCount = parseStateNumber(state);
    return {
      id: `metric:${entry.entity_id}`,
      kind: diagnosticKind,
      label: 'Issues',
      value: formatSensorState(state, '', 0),
      icon: { category: 'system-states', key: issueCount === 0 ? 'ok' : 'trouble' },
      tone: diagnosticHealth('issue_count', state.state).tone,
      priority: 16,
    };
  }

  if (diagnosticKind === 'battery_check_status') {
    return {
      id: `metric:${entry.entity_id}`,
      kind: diagnosticKind,
      label: 'Battery check',
      value: humanizeHomeAssistantState(state),
      icon: { category: 'sensors', key: 'battery' },
      tone: diagnosticHealth('battery_check_status', state.state).tone,
      priority: 32,
    };
  }

  if (diagnosticKind === 'operating_mode') {
    return {
      id: `metric:${entry.entity_id}`,
      kind: diagnosticKind,
      label: 'Mode',
      value: humanizeSlug(state.state),
      icon: { category: 'devices', key: 'panic_button' },
      tone: 'cyan',
      priority: 45,
    };
  }

  if (diagnosticKind === 'firmware_version') {
    return {
      id: `metric:${entry.entity_id}`,
      kind: diagnosticKind,
      label: 'Firmware',
      value: state.state,
      icon: { category: 'misc', key: 'info' },
      tone: 'slate',
      priority: 70,
    };
  }

  if (diagnosticKind === 'device_last_update') {
    return {
      id: `metric:${entry.entity_id}`,
      kind: diagnosticKind,
      label: 'Device updated',
      value: formatTimestampMetric(state.state),
      icon: { category: 'misc', key: 'history' },
      tone: 'slate',
      priority: 75,
    };
  }

  if (deviceClass === 'temperature' || matchesMetricName(text, ['temperature', 'temp', 'temperature_c'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'temperature',
      label: 'Temperature',
      value: formatSensorState(state, unit || 'C', 1),
      icon: { category: 'sensors', key: 'temperature' },
      tone: temperatureTone(parseStateNumber(state)),
      priority: 20,
    };
  }

  if (deviceClass === 'humidity' || matchesMetricName(text, ['humidity', 'humidite', 'humidity_percent'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'humidity',
      label: 'Humidity',
      value: formatSensorState(state, unit || '%', 0),
      icon: { category: 'sensors', key: 'humidity' },
      tone: 'cyan',
      priority: 21,
    };
  }

  if (deviceClass === 'battery' || matchesMetricName(text, ['battery', 'batterie', 'battery_percent'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'battery',
      label: 'Battery',
      value: formatSensorState(state, unit || '%', 0),
      icon: { category: 'sensors', key: 'battery' },
      tone: diagnosticHealth('battery_percent', state.state).tone,
      priority: 30,
    };
  }

  if (deviceClass === 'signal_strength' || matchesMetricName(text, ['signal', 'rssi', 'signal_dbm', 'signal_level'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'signal_strength',
      label: 'Signal',
      value: formatSensorState(state, unit, 0),
      icon: { category: 'sensors', key: 'signal' },
      tone: signalTone(parseStateNumber(state), unit),
      priority: 31,
    };
  }

  // The historic entity ID/name may still contain "power" after metadata repair.
  if (deviceClass === 'energy' || matchesMetricName(text, ['energy', 'energy_raw', 'energy_kwh', 'energy_wh', 'consumption', 'consommation'])) {
    const verifiedEnergy = deviceClass === 'energy' && energyUnit;
    return {
      id: `metric:${entry.entity_id}`,
      kind: verifiedEnergy ? 'energy' : 'energy_raw',
      label: verifiedEnergy ? 'Energy' : 'Energy (raw)',
      value: formatSensorState(state, unit, 1),
      icon: { category: 'misc', key: 'energy' },
      tone: 'cyan',
      priority: 41,
    };
  }

  if (parseStateNumber(state) !== null && (deviceClass === 'power' || matchesMetricName(text, ['power', 'power_w', 'power_raw', 'puissance']))) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: unit ? 'power' : 'power_raw',
      label: unit ? 'Power' : 'Power (raw)',
      value: formatSensorState(state, unit, 0),
      icon: { category: 'misc', key: 'energy' },
      tone: 'cyan',
      priority: 40,
    };
  }

  if (parseStateNumber(state) !== null && (deviceClass === 'current' || matchesMetricName(text, ['current', 'current_a', 'current_ma', 'current_raw', 'courant']))) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: unit ? 'current' : 'current_raw',
      label: unit ? 'Current' : 'Current (raw)',
      value: formatSensorState(state, unit, 1),
      icon: { category: 'misc', key: 'energy' },
      tone: 'cyan',
      priority: 42,
    };
  }

  if (deviceClass === 'voltage' || matchesMetricName(text, ['voltage_v', 'tension'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'voltage',
      label: 'Voltage',
      value: formatSensorState(state, unit || 'V', 0),
      icon: { category: 'misc', key: 'energy' },
      tone: 'cyan',
      priority: 43,
    };
  }

  if (matchesMetricName(text, ['battery_state'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'battery_state',
      label: 'Battery state',
      value: humanizeHomeAssistantState(state),
      icon: { category: 'sensors', key: 'battery' },
      tone: stateLooksNominal(state) ? 'green' : 'amber',
      priority: 32,
    };
  }

  if (matchesMetricName(text, ['state', 'status', 'etat'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'state',
      label: 'State',
      value: humanizeHomeAssistantState(state),
      icon: { category: 'misc', key: 'info' },
      tone: stateLooksNominal(state) ? 'green' : 'slate',
      priority: 50,
    };
  }

  return null;
}

function binaryMetricCandidate(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState,
  deviceClass: string,
  text: string,
): MetricCandidate | null {
  const active = isActiveDahuaState(state);
  const activeTone: GlowTone = active ? 'amber' : 'green';
  const semantic = ajaxEntitySemantic(entry, state.attributes);
  const signal = semantic?.startsWith('signal_') ? semantic.slice('signal_'.length) : '';
  if (signal && ISSUE_SIGNALS.has(signal)) {
    if (!active && signal !== 'connectivity') return null;
    return {
      id: `metric:${entry.entity_id}`, kind: signal === 'connectivity' ? 'connectivity' : `fault_${signal}`,
      label: signal === 'connectivity' ? 'Link' : `${humanizeSlug(signal)} alert`,
      value: signal === 'connectivity' ? active ? 'Offline' : 'Online' : active ? 'Active' : 'Clear',
      icon: { category: 'system-states', key: active ? 'trouble' : 'ok' },
      tone: active ? SECURITY_SIGNALS.has(signal) ? 'red' : 'amber' : 'green', priority: 12,
    };
  }

  if (deviceClass === 'connectivity' || looksLikePositiveConnectivityEntity(entry, state)) {
    const online = isPositiveConnectivityState(state);
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'connectivity',
      label: 'Link',
      value: online ? 'Online' : 'Offline',
      icon: { category: 'system-states', key: online ? 'online' : 'offline' },
      tone: online ? 'green' : 'red',
      priority: 10,
    };
  }

  if (deviceClass === 'battery' || matchesMetricName(text, ['battery'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'battery_low',
      label: 'Battery',
      value: active ? 'Low' : 'Normal',
      icon: { category: 'sensors', key: 'battery' },
      tone: active ? 'amber' : 'green',
      priority: 30,
    };
  }

  if (deviceClass === 'power' || matchesMetricName(text, ['external_power', 'mainspower', 'power'])) {
    const gridPower = matchesMetricName(text, ['grid_power']) || text.includes('grid power');
    return {
      id: `metric:${entry.entity_id}`,
      kind: gridPower ? 'grid_power' : 'external_power',
      label: gridPower ? 'Grid power' : 'Power',
      value: active ? 'Mains' : humanizeHomeAssistantState(state),
      icon: { category: 'misc', key: 'energy' },
      tone: active ? 'green' : 'amber',
      priority: 33,
    };
  }

  if (deviceClass === 'tamper' || matchesMetricName(text, ['tamper', 'sabotage'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'tamper',
      label: 'Tamper',
      value: active ? 'Active' : 'Clear',
      icon: { category: 'events', key: 'tamper_detected' },
      tone: active ? 'red' : 'green',
      priority: 14,
    };
  }

  if (['door', 'window', 'opening'].includes(deviceClass) || matchesMetricName(text, ['door', 'window', 'opening', 'ouverture'])) {
    const isWindow = deviceClass === 'window' || text.includes('window');
    const label = isWindow ? 'Window' : 'Opening';
    return {
      id: `metric:${entry.entity_id}`,
      kind: isWindow ? 'window' : 'opening',
      label,
      value: humanizeHomeAssistantState(state),
      icon: { category: 'security-states', key: active ? (isWindow ? 'window_open' : 'door_open') : (isWindow ? 'window_closed' : 'door_closed') },
      tone: activeTone,
      priority: 22,
    };
  }

  if (deviceClass === 'moisture' || matchesMetricName(text, ['leak', 'fuite', 'water_leak'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'leak',
      label: 'Leak',
      value: active ? 'Detected' : 'Dry',
      icon: { category: 'sensors', key: 'water_leak' },
      tone: active ? 'red' : 'green',
      priority: 23,
    };
  }

  if (['motion', 'occupancy', 'presence'].includes(deviceClass) || matchesMetricName(text, ['motion', 'presence'])) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: 'motion',
      label: 'Motion',
      value: active ? 'Detected' : 'Quiet',
      icon: { category: 'sensors', key: 'motion' },
      tone: active ? 'amber' : 'green',
      priority: 24,
    };
  }

  if (['problem', 'safety', 'smoke', 'gas'].includes(deviceClass)) {
    return {
      id: `metric:${entry.entity_id}`,
      kind: deviceClass || 'problem',
      label: titleMetricLabel(deviceClass || entityIdLeaf(entry.entity_id)),
      value: humanizeHomeAssistantState(state),
      icon: iconForBinaryDeviceClass(deviceClass, active),
      tone: active ? (deviceClass === 'safety' || deviceClass === 'smoke' || deviceClass === 'gas' ? 'red' : 'amber') : 'green',
      priority: active ? 13 : 55,
    };
  }

  return null;
}

function dedupeMetrics(candidates: MetricCandidate[]): MetricCandidate[] {
  const byKind = new Map<string, MetricCandidate>();
  const usedIds = new Set<string>();

  for (const candidate of candidates) {
    if (!candidate.value || usedIds.has(candidate.id)) {
      continue;
    }
    usedIds.add(candidate.id);

    const current = byKind.get(candidate.kind);
    if (!current || candidate.priority < current.priority || (candidate.priority === current.priority && candidate.value.length > current.value.length)) {
      byKind.set(candidate.kind, candidate);
    }
  }

  return [...byKind.values()];
}

function buildRoomClimate(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): RoomClimate | undefined {
  const temperatures: ClimateSample[] = [];
  const humidities: ClimateSample[] = [];

  for (const entry of entries) {
    const state = states[entry.entity_id];
    const sample = climateSampleFromEntity(entry, state);
    if (!sample) {
      continue;
    }
    if (sample.kind === 'temperature') {
      temperatures.push(sample);
    } else {
      humidities.push(sample);
    }
  }

  const climate: RoomClimate = {};
  if (temperatures.length > 0) {
    climate.temperature = formatAverageSample(temperatures, 'C', 1);
  }
  if (humidities.length > 0) {
    climate.humidity = formatAverageSample(humidities, '%', 0);
  }

  return climate.temperature || climate.humidity ? climate : undefined;
}

function climateSampleFromEntity(
  entry: HomeAssistantEntityEntry,
  state?: HomeAssistantState,
): (ClimateSample & { kind: 'temperature' | 'humidity' }) | null {
  if (!state || entityDomain(entry.entity_id) !== 'sensor' || isIgnoredMetricState(state)) {
    return null;
  }

  const value = parseStateNumber(state);
  if (value === null) {
    return null;
  }

  const deviceClass = safeString(state.attributes.device_class).toLowerCase();
  const text = entityDescriptorText(entry, state);
  const unit = safeString(state.attributes.unit_of_measurement);

  if (deviceClass === 'temperature' || matchesMetricName(text, ['temperature', 'temp', 'temperature_c'])) {
    return { kind: 'temperature', value, unit };
  }

  if (deviceClass === 'humidity' || matchesMetricName(text, ['humidity', 'humidite', 'humidity_percent'])) {
    return { kind: 'humidity', value, unit };
  }

  return null;
}

function readAjaxBatteryLabel(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  activeSignals: string[],
): string {
  const batteryMetric = entries
    .map((entry) => metricCandidateFromEntity(entry, states[entry.entity_id]))
    .find((metric) => metric?.kind === 'battery' || metric?.kind === 'battery_low' || metric?.kind === 'battery_state');

  if (batteryMetric) {
    return batteryMetric.value;
  }

  return activeSignals.includes('battery') ? 'Low' : readBatteryLabel(entries, states);
}

function readAjaxSignalLabel(
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  lastSignal: string,
): string {
  const signalMetric = entries
    .map((entry) => metricCandidateFromEntity(entry, states[entry.entity_id]))
    .find((metric) => metric?.kind === 'signal_strength');

  if (signalMetric) {
    return signalMetric.value;
  }

  return humanizeSlug(lastSignal || 'idle');
}

function actionMetricLabel(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState,
  fallbackLabel: string,
): string {
  const label = entityDisplayName(entry, state);
  return /^control$/i.test(label) ? fallbackLabel : label;
}

function matchesMetricName(text: string, needles: string[]): boolean {
  return needles.some((needle) => {
    const escaped = needle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`(^|[\\s._-])${escaped}([\\s._-]|$)`, 'i').test(text);
  });
}

function iconForBinaryDeviceClass(deviceClass: string, active: boolean): IconRef {
  switch (deviceClass) {
    case 'smoke':
      return { category: 'sensors', key: 'smoke' };
    case 'gas':
      return { category: 'sensors', key: 'gas' };
    case 'safety':
      return { category: 'misc', key: active ? 'alert' : 'check' };
    default:
      return { category: 'system-states', key: active ? 'trouble' : 'ok' };
  }
}

function titleMetricLabel(value: string): string {
  const label = humanizeSlug(value);
  return label === 'Problem' ? 'Trouble' : label;
}

function entityDescriptorText(entry: HomeAssistantEntityEntry, state: HomeAssistantState): string {
  return [
    ajaxEntitySemantic(entry, state.attributes),
    entry.unique_id,
    entry.entity_id,
    entry.name,
    entry.original_name,
    state.attributes.friendly_name,
    state.attributes.device_class,
  ]
    .map(safeString)
    .join(' ')
    .toLowerCase();
}

function formatSensorState(state: HomeAssistantState, unit: string, maximumFractionDigits: number): string {
  const number = parseStateNumber(state);
  if (number === null) {
    return humanizeHomeAssistantState(state);
  }
  return formatMetricNumber(number, unit, maximumFractionDigits);
}

function formatAverageSample(samples: ClimateSample[], fallbackUnit: string, maximumFractionDigits: number): string {
  const total = samples.reduce((sum, sample) => sum + sample.value, 0);
  const unit = samples.map((sample) => sample.unit).find(Boolean) ?? fallbackUnit;
  return formatMetricNumber(total / samples.length, unit, maximumFractionDigits);
}

function formatMetricNumber(value: number, unit: string, maximumFractionDigits: number): string {
  const formatted = new Intl.NumberFormat('en-GB', {
    maximumFractionDigits,
    minimumFractionDigits: maximumFractionDigits > 0 ? 1 : 0,
  }).format(value);
  return unit ? `${formatted} ${unit}` : formatted;
}

function formatShortDateTime(value: string): string {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return value;
  }
  return new Intl.DateTimeFormat('en-GB', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(parsed));
}

function formatTimestampMetric(value: string): string {
  const numeric = Number(value);
  if (Number.isFinite(numeric) && numeric > 0) {
    const milliseconds = numeric < 10_000_000_000 ? numeric * 1000 : numeric;
    return formatShortDateTime(new Date(milliseconds).toISOString());
  }
  return formatShortDateTime(value);
}

function parseStateNumber(state?: HomeAssistantState): number | null {
  const raw = safeString(state?.state).replace(',', '.');
  if (!raw || raw === 'unknown' || raw === 'unavailable') {
    return null;
  }
  const value = Number.parseFloat(raw);
  return Number.isFinite(value) ? value : null;
}

function isIgnoredMetricState(state: HomeAssistantState): boolean {
  const value = safeString(state.state).toLowerCase();
  return value === '' || value === 'unknown' || value === 'unavailable';
}

function stateLooksNominal(state: HomeAssistantState): boolean {
  const value = safeString(state.state).toLowerCase();
  return ['ok', 'normal', 'nominal', 'clear', 'off', 'closed', 'locked', 'online', 'connected', 'available'].includes(value);
}

function temperatureTone(value: number | null): GlowTone {
  if (value === null) {
    return 'cyan';
  }
  if (value <= 5 || value >= 35) {
    return 'amber';
  }
  return 'cyan';
}

function signalTone(value: number | null, unit: string): GlowTone {
  if (value === null) {
    return 'cyan';
  }
  if (unit.toLowerCase() === 'dbm') {
    return value <= -95 ? 'amber' : 'green';
  }
  return value <= 1 ? 'amber' : 'green';
}

function resolvePreferredStreamEntity(
  entry: HomeAssistantEntityEntry,
  entries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): string | null {
  const entityId = entry.entity_id;
  const base = streamBaseKey(entityId);
  const explicitCandidates = entries
    .map((candidate) => candidate.entity_id)
    .filter((candidateId) => entityDomain(candidateId) === 'camera');
  const relatedStateCandidates = Object.keys(states).filter(
    (candidateId) => entityDomain(candidateId) === 'camera' && streamEntityMayMatch(candidateId, base),
  );
  const candidates = Array.from(new Set([...explicitCandidates, entityId, ...relatedStateCandidates]));

  const ranked = candidates
    .map((candidate) => ({
      entityId: candidate,
      score: streamCandidateScore(candidate, base, states[candidate]),
    }))
    .filter((candidate) => candidate.score > 0)
    .sort((left, right) => right.score - left.score);

  return ranked[0]?.entityId ?? null;
}

function streamCandidateScore(entityId: string, base: string, state?: HomeAssistantState): number {
  if (!state || !hasAvailableState(state)) {
    return 0;
  }

  const matchScore = streamCandidateMatchScore(entityId, base);
  if (matchScore === 0) {
    return 0;
  }

  return matchScore + focusedStreamPreferenceScore(entityId, state);
}

function streamEntityMayMatch(entityId: string, base: string): boolean {
  return streamCandidateMatchScore(entityId, base) > 0;
}

function streamCandidateMatchScore(entityId: string, base: string): number {
  const candidateBase = streamBaseKey(entityId);
  if (!base || !candidateBase) {
    return 0;
  }
  if (candidateBase === base) {
    return 1000;
  }
  if (candidateBase.endsWith(`_${base}`) || base.endsWith(`_${candidateBase}`)) {
    return 850;
  }
  if (candidateBase.includes(base) || base.includes(candidateBase)) {
    return 650;
  }
  return 0;
}

function focusedStreamPreferenceScore(entityId: string, state: HomeAssistantState): number {
  const text = streamDescriptorText(entityId, state);
  let score = 0;

  if (/(^|[\s._-])main($|[\s._-])/.test(text)) {
    score += 400;
  }
  if (/(^|[\s._-])(quality|high|hd)($|[\s._-])/.test(text)) {
    score += 350;
  }
  if (/(^|[\s._-])(sub|stable|low|sd)($|[\s._-])/.test(text)) {
    score -= 300;
  }

  const profiles = readObjectRecord(state.attributes.bridge_profiles);
  if (profiles?.quality) {
    score += 25;
  }
  if (profiles?.main) {
    score += 25;
  }

  return score;
}

function streamDescriptorText(entityId: string, state: HomeAssistantState): string {
  return [
    entityIdLeaf(entityId),
    state.attributes.friendly_name,
    state.attributes.preferred_video_profile,
    state.attributes.recommended_profile,
    state.attributes.video_profile,
    state.attributes.stream_profile,
    state.attributes.profile,
    state.attributes.stream_source,
  ]
    .map(safeString)
    .join(' ')
    .toLowerCase();
}

function streamBaseKey(entityId: string): string {
  return entityIdLeaf(entityId)
    .toLowerCase()
    .replace(/^dahua_nvr_camera_dahua_nvr_/, '')
    .replace(/^dahua_nvr_/, '')
    .replace(/_(main|sub|quality|stable|high|low|hd|sd)$/i, '')
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
}

function readObjectRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function isNamedDahuaAlertEntity(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): boolean {
  const text = dahuaEntityText(entry, state);
  return /alarm local|audio mutation|button pressed|call no answered|cross line alarm|cross region detection|door status|invite|motion alarm|smart motion human|smart motion vehicle|video blind|video loss/.test(text);
}

function isActiveDahuaState(state?: HomeAssistantState): boolean {
  const value = safeString(state?.state).toLowerCase();
  return ['on', 'open', 'opening', 'unlocked', 'detected', 'active', 'alarm', 'triggered', 'true'].includes(value);
}

function dahuaEntityText(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): string {
  return [
    entry.entity_id,
    entry.name,
    entry.original_name,
    state?.attributes.friendly_name,
    state?.attributes.device_class,
  ]
    .map(safeString)
    .join(' ')
    .toLowerCase();
}

function filterDahuaCameraTimelineEntries(
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): HomeAssistantEntityEntry[] {
  const filtered = entityEntries.filter((entry) => isRelevantDahuaCameraTimelineEntity(entry, states[entry.entity_id]));
  return filtered;
}

function isRelevantDahuaCameraTimelineEntity(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): boolean {
  if (!state) {
    return false;
  }

  return isDahuaCameraOnlineEntity(entry, state)
    || isDahuaCameraSmdIvsEntity(entry, state);
}

function isDahuaCameraOnlineEntity(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): boolean {
  if (!state) {
    return false;
  }

  return looksLikePositiveConnectivityEntity(entry, state);
}

function isDahuaCameraSmdIvsEntity(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): boolean {
  if (!state) {
    return false;
  }
  if (isOfflineState(state)) {
    return false;
  }

  const text = dahuaEntityText(entry, state);
  if (looksLikeDahuaCapabilityStatusEntity(entry, state)) {
    return false;
  }

  return /smart\s*motion\s*human|smartmotionhuman|smd.*human|\bhuman\b|smart\s*motion\s*vehicle|smartmotionvehicle|smd.*vehicle|\bvehicle\b|\banimal\b|cross\s*line\s*(alarm|detection)?|crosslinedetection|tripwire|cross\s*region\s*detection|crossregiondetection|intrusion/.test(text);
}

function dahuaTimelinePresentation(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState,
): { type: EventType; icon: IconRef; tone: GlowTone } {
  if (isDahuaCameraOnlineEntity(entry, state)) {
    return isPositiveConnectivityState(state)
      ? {
          type: 'device_online',
          icon: { category: 'system-states', key: 'online' },
          tone: 'green',
        }
      : {
          type: 'device_offline',
          icon: { category: 'system-states', key: 'offline' },
          tone: 'red',
        };
  }

  const type = eventTypeFromEntity(entry, state);
  const active = isActiveDahuaState(state);

  return {
    type,
    icon: iconForEvent(type),
    tone: active ? toneFromSeverity(severityFromEntity(entry, state)) : 'green',
  };
}

function buildDahuaTimelineEvents(
  deviceId: string,
  roomId: string,
  deviceName: string,
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): EventItem[] {
  const events = entityEntries
    .map((entry) => buildDahuaTimelineEvent(deviceId, roomId, deviceName, entry, states[entry.entity_id]))
    .filter((event): event is EventItem => event !== null);

  if (events.length === 0) {
    return [];
  }

  return events.sort((left, right) => toUnix(right.occurredAt) - toUnix(left.occurredAt));
}

function buildDahuaTimelineEvent(
  deviceId: string,
  roomId: string,
  deviceName: string,
  entry: HomeAssistantEntityEntry,
  state?: HomeAssistantState,
): EventItem | null {
  if (!state) {
    return null;
  }

  const label = entityDisplayName(entry, state);
  const presentation = dahuaTimelinePresentation(entry, state);
  const occurredAt = state.last_changed ?? state.last_updated ?? '';
  const copy = buildDahuaTimelineCopy(deviceName, entry, state, label, presentation.type);

  return {
    id: `event:${deviceId}:${entry.entity_id}`,
    roomId,
    deviceId,
    type: presentation.type,
    title: copy.title,
    description: copy.description,
    occurredAt,
    source: deviceName,
    icon: presentation.icon,
    tone: presentation.tone,
  };
}

function buildDahuaTimelineCopy(
  deviceName: string,
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState,
  fallbackLabel: string,
  eventType: EventType,
): { title: string; description: string } {
  if (isDahuaCameraOnlineEntity(entry, state)) {
    const online = isPositiveConnectivityState(state);
    return {
      title: online ? 'Camera online' : 'Camera offline',
      description: online
        ? `${deviceName} is reachable.`
        : `${deviceName} is not reachable.`,
    };
  }

  const active = isActiveDahuaState(state);
  const detection = dahuaDetectionCopy(eventType);
  if (detection) {
    return {
      title: active ? detection.activeTitle : detection.clearTitle,
      description: active
        ? `${deviceName} ${detection.activeDescription}`
        : `${deviceName} ${detection.clearDescription}`,
    };
  }

  return {
    title: fallbackLabel,
    description: `${deviceName}: ${humanizeHomeAssistantState(state)}`,
  };
}

function dahuaDetectionCopy(eventType: EventType): {
  activeTitle: string;
  clearTitle: string;
  activeDescription: string;
  clearDescription: string;
} | null {
  switch (eventType) {
    case 'human_detected':
      return {
        activeTitle: 'Person detected',
        clearTitle: 'Person detection cleared',
        activeDescription: 'detected a person.',
        clearDescription: 'no longer reports a person.',
      };
    case 'vehicle_detected':
      return {
        activeTitle: 'Vehicle detected',
        clearTitle: 'Vehicle detection cleared',
        activeDescription: 'detected a vehicle.',
        clearDescription: 'no longer reports a vehicle.',
      };
    case 'tripwire_detected':
      return {
        activeTitle: 'Tripwire crossed',
        clearTitle: 'Tripwire clear',
        activeDescription: 'reported a line-crossing event.',
        clearDescription: 'line-crossing detection is clear.',
      };
    case 'intrusion_detected':
      return {
        activeTitle: 'Intrusion detected',
        clearTitle: 'Intrusion area clear',
        activeDescription: 'reported an intrusion event.',
        clearDescription: 'intrusion detection is clear.',
      };
    case 'motion_detected':
      return {
        activeTitle: 'Animal detected',
        clearTitle: 'Animal detection cleared',
        activeDescription: 'detected an animal.',
        clearDescription: 'no longer reports an animal.',
      };
    default:
      return null;
  }
}

function findAreaForEntity(
  entityEntry: HomeAssistantEntityEntry,
  entitiesByAreaId: Map<string, HomeAssistantEntityEntry[]>,
): string | null {
  for (const [areaId, entries] of entitiesByAreaId.entries()) {
    if (entries.some((entry) => entry.entity_id === entityEntry.entity_id)) {
      return areaId;
    }
  }
  return null;
}

function summarizeHeadline(input: {
  alarmActive: boolean;
  tamperActive: boolean;
  troubleActive: boolean;
  activeSignals: string[];
}): string {
  if (input.activeSignals.some((signal) => ['fire', 'smoke', 'co', 'gas', 'gas_or_co', 'temperature'].includes(signal))) {
    return 'Fire response';
  }
  if (input.activeSignals.some((signal) => ['water_leak', 'leak', 'flood'].includes(signal))) {
    return 'Flood response';
  }
  if (input.alarmActive || input.activeSignals.some((signal) => ['alarm', 'burglary', 'panic', 'duress', 'emergency', 'medical', 'hold_up'].includes(signal))) {
    return 'Alarm active';
  }
  if (input.tamperActive || input.activeSignals.includes('tamper')) {
    return 'Tamper active';
  }
  if (input.troubleActive) {
    return 'Trouble active';
  }
  if (input.activeSignals.includes('connectivity')) {
    return 'Connectivity issue';
  }
  if (input.activeSignals.includes('battery')) {
    return 'Battery attention';
  }
  if (input.activeSignals[0]) {
    return humanizeSlug(input.activeSignals[0]);
  }
  return 'Nominal';
}

function severityFromSignals(input: {
  alarmActive: boolean;
  tamperActive: boolean;
  troubleActive: boolean;
  activeSignals: string[];
}): number {
  const fireLike = input.activeSignals.some((signal) => ['fire', 'smoke', 'co', 'gas', 'gas_or_co', 'temperature'].includes(signal));
  const waterLike = input.activeSignals.some((signal) => ['water_leak', 'leak', 'flood'].includes(signal));
  const intrusionLike = input.activeSignals.some((signal) => ['alarm', 'burglary', 'panic', 'duress', 'emergency', 'medical', 'hold_up'].includes(signal));
  const offline = input.activeSignals.includes('connectivity');
  const batteryIssue = input.activeSignals.includes('battery');

  if (fireLike || waterLike || input.alarmActive || intrusionLike) {
    return 4;
  }
  if (input.tamperActive || input.activeSignals.includes('tamper')) return 4;
  if (input.troubleActive || offline) {
    return 3;
  }
  if (batteryIssue || input.activeSignals.some((signal) => ISSUE_SIGNALS.has(signal))) {
    return 2;
  }
  return 0;
}

function toneFromSeverity(severity: number): GlowTone {
  if (severity >= 4) {
    return 'red';
  }
  if (severity >= 2) {
    return 'amber';
  }
  return 'green';
}

function mapSignalToEventType(signal: string, alarmActive: boolean, offline: boolean): EventType {
  const normalized = slugPart(signal);
  if (offline || normalized === 'connectivity') {
    return 'device_offline';
  }
  if (alarmActive || normalized === 'alarm' || normalized === 'burglary' || normalized === 'panic') {
    return 'alarm';
  }
  switch (normalized) {
    case 'smoke':
      return 'smoke_detected';
    case 'fire':
    case 'temperature':
      return 'fire_detected';
    case 'water_leak':
    case 'leak':
    case 'flood':
      return 'leak_detected';
    case 'gas':
    case 'co':
    case 'gas_or_co':
      return 'gas_detected';
    case 'tamper':
      return 'tamper_detected';
    case 'battery':
      return 'battery_low';
    case 'power':
      return 'power_lost';
    case 'night_mode':
      return 'night_mode';
    default:
      return 'restored';
  }
}

function eventTypeFromEntity(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): EventType {
  const text = dahuaEntityText(entry, state);
  const deviceClass = safeString(state?.attributes.device_class).toLowerCase();
  const domain = entityDomain(entry.entity_id);
  if (isOfflineState(state)) {
    return 'device_offline';
  }
  if (state && looksLikePositiveConnectivityEntity(entry, state) && !isPositiveConnectivityState(state)) {
    return 'device_offline';
  }
  if (/door status|door\b/.test(text) && isActiveDahuaState(state)) {
    return 'door_opened';
  }
  if (/window|opening/.test(text) && isActiveDahuaState(state)) {
    return 'window_opened';
  }
  if (/video loss|connectivity|offline/.test(text)) {
    return 'device_offline';
  }
  if (/smart\s*motion\s*human|smartmotionhuman|smd.*human|\bhuman\b/.test(text)) {
    return 'human_detected';
  }
  if (/smart\s*motion\s*vehicle|smartmotionvehicle|smd.*vehicle|\bvehicle\b/.test(text)) {
    return 'vehicle_detected';
  }
  if (/cross\s*line\s*(alarm|detection)?|crosslinedetection|tripwire/.test(text)) {
    return 'tripwire_detected';
  }
  if (/cross\s*region\s*detection|crossregiondetection|intrusion/.test(text)) {
    return 'intrusion_detected';
  }
  if (/\banimal\b/.test(text)) {
    return 'motion_detected';
  }
  if (/tamper|video blind|audio mutation/.test(text)) {
    return 'tamper_detected';
  }
  if (/motion/.test(text)) {
    return 'motion_detected';
  }
  if (/button pressed|invite|call no answered|alarm local/.test(text)) {
    return 'alarm';
  }
  if (deviceClass === 'motion' || domain === 'camera') {
    return 'motion_detected';
  }
  if (deviceClass === 'door') {
    return 'door_opened';
  }
  if (deviceClass === 'window' || deviceClass === 'opening') {
    return 'window_opened';
  }
  if (deviceClass === 'moisture') {
    return 'leak_detected';
  }
  if (deviceClass === 'smoke') {
    return 'smoke_detected';
  }
  if (deviceClass === 'gas') {
    return 'gas_detected';
  }
  return 'alarm';
}

function buildGenericEventDescription(
  entry: HomeAssistantEntityEntry,
  state: HomeAssistantState | undefined,
  deviceName: string,
): string {
  if (isOfflineState(state)) {
    return `${deviceName} is offline`;
  }
  if (state && looksLikePositiveConnectivityEntity(entry, state)) {
    return isPositiveConnectivityState(state) ? `${deviceName} is online` : `${deviceName} is offline`;
  }
  const deviceClass = safeString(state?.attributes.device_class).toLowerCase();
  if (isOn(state)) {
    return `${deviceName}: ${humanizeSlug(deviceClass || entityIdLeaf(entry.entity_id))} active`;
  }
  return `${deviceName}: ${state?.state ?? 'updated'}`;
}

function severityFromEntity(entry: HomeAssistantEntityEntry, state: HomeAssistantState | undefined): number {
  const text = dahuaEntityText(entry, state);
  const eventType = eventTypeFromEntity(entry, state);
  if (eventType === 'smoke_detected' || eventType === 'fire_detected' || eventType === 'leak_detected' || eventType === 'gas_detected') {
    return 4;
  }
  if (eventType === 'device_offline') {
    return 3;
  }
  if (/video blind|audio mutation|alarm local|button pressed|invite|call no answered/.test(text)) {
    return 3;
  }
  return 2;
}

function readBatteryLabel(entries: HomeAssistantEntityEntry[], states: Record<string, HomeAssistantState>): string {
  const batteryState = entries
    .map((entry) => states[entry.entity_id])
    .find((state) => typeof state?.attributes.battery_level === 'number');

  if (batteryState && typeof batteryState.attributes.battery_level === 'number') {
    return `${batteryState.attributes.battery_level}%`;
  }

  return 'Nominal';
}

function resolveRoomImage(
  area: HomeAssistantArea,
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
): string {
  const areaPicture = normalizeHomeAssistantImageUrl(
    safeString(area.picture) || safeString(area['picture_path']) || safeString(area['entity_picture']),
  );
  if (areaPicture) {
    return areaPicture;
  }

  const candidates = entityEntries
    .slice()
    .sort((left, right) => Number(isRelevantDahuaEntity(right)) - Number(isRelevantDahuaEntity(left)));

  for (const entry of candidates) {
    const picture = readEntityPicture(states[entry.entity_id]);
    if (picture) {
      return picture;
    }
    if (isCameraDomain(entry.entity_id)) {
      return `/api/camera_proxy/${entry.entity_id}`;
    }
  }

  return '';
}

function buildRoomSummary(metrics: RoomMetrics): string {
  const parts = [
    `${metrics.deviceCount} devices`,
    metrics.cameraCount > 0 ? `${metrics.cameraCount} cameras` : '',
    metrics.sensorCount > 0 ? `${metrics.sensorCount} sensors` : '',
    metrics.gridPower.known > 0
      ? metrics.gridPower.outage > 0
        ? `${metrics.gridPower.outage} grid outage`
        : 'Grid OK'
      : '',
  ].filter(Boolean);

  return parts.join(' · ') || 'No linked devices';
}

function sortResolvedDevices(left: ResolvedDevice, right: ResolvedDevice): number {
  if (right.attention !== left.attention) {
    return Number(right.attention) - Number(left.attention);
  }
  if (right.isOnline !== left.isOnline) {
    return Number(right.isOnline) - Number(left.isOnline);
  }
  return left.name.localeCompare(right.name);
}

function sortRooms(left: Room, right: Room): number {
  return left.name.localeCompare(right.name);
}

function inferDeviceType(input: { name: string; model: string; entityIds: string[] }): string {
  const product = input.model.toLowerCase().replace(/[^a-z0-9]/g, '');
  const productTypes: Array<[RegExp, string]> = [
    [/^(?:ajax|mobile)?app$/, 'app'], [/^hub/, 'hub'], [/^wallswitch/, 'wall_switch'], [/^lightswitch/, 'light_switch'],
    [/^socket/, 'smart_plug'], [/^waterstop/, 'waterstop'], [/^relay/, 'relay'], [/^multitransmitter/, 'multitransmitter'],
    [/^transmitter/, 'transmitter'], [/^rex/, 'repeater'], [/^keypad/, 'keypad'], [/siren/, 'siren'],
    [/^doorprotect/, 'door_sensor'], [/^glassprotect/, 'glass_break_sensor'], [/^fireprotect/, 'fire_detector'], [/^leaksprotect/, 'leak_detector'],
    [/^motionprotectcurtain/, 'curtain_motion_sensor'], [/^motionprotectoutdoor/, 'outdoor_motion_sensor'], [/^motionprotect|^combi/, 'motion_sensor'],
  ];
  const productType = productTypes.find(([pattern]) => pattern.test(product));
  if (productType) return productType[1];
  const haystack = `${input.name} ${input.model} ${input.entityIds.join(' ')}`.toLowerCase();
  const buttonType = inferAjaxButtonDeviceType(input.model);
  if (buttonType) {
    return buttonType;
  }
  if (haystack.includes('vto') || haystack.includes('doorbell')) {
    return 'camera';
  }
  if (haystack.includes('camera') || haystack.includes('cam.') || input.entityIds.some((entityId) => isCameraDomain(entityId))) {
    return 'camera';
  }
  if (haystack.includes('hub')) {
    return 'hub';
  }
  if (haystack.includes('multitransmitter') || haystack.includes('multi transmitter')) {
    return 'multitransmitter';
  }
  if (haystack.includes('transmitter')) {
    return 'transmitter';
  }
  if (haystack.includes('rex') || haystack.includes('repeater')) {
    return 'repeater';
  }
  if (haystack.includes('keypad')) {
    return 'keypad';
  }
  if (haystack.includes('siren')) {
    return 'siren';
  }
  if (haystack.includes('socket') || haystack.includes('plug')) {
    return 'smart_plug';
  }
  if (haystack.includes('waterstop') || haystack.includes('water stop')) {
    return 'waterstop';
  }
  if (haystack.includes('lightswitch') || haystack.includes('light switch')) {
    return 'light_switch';
  }
  if (haystack.includes('wallswitch') || haystack.includes('wall switch')) {
    return 'wall_switch';
  }
  if (haystack.includes('thermostat')) {
    return 'thermostat';
  }
  if (haystack.includes('relay')) {
    return 'relay';
  }
  if (haystack.includes('glass')) {
    return 'glass_break_sensor';
  }
  if (haystack.includes('curtain')) {
    return 'curtain_motion_sensor';
  }
  if (haystack.includes('outdoor')) {
    return 'outdoor_motion_sensor';
  }
  if (haystack.includes('window')) {
    return 'window_sensor';
  }
  if (haystack.includes('door') || haystack.includes('opening')) {
    return 'door_sensor';
  }
  if (haystack.includes('smoke')) {
    return 'smoke_detector';
  }
  if (haystack.includes('fire') || haystack.includes('heat')) {
    return 'fire_detector';
  }
  if (haystack.includes('leak') || haystack.includes('moisture') || haystack.includes('water')) {
    return 'leak_detector';
  }
  return 'motion_sensor';
}

function iconForDevice(type: string, name: string, model: string): IconRef {
  const lowered = `${type} ${name} ${model}`.toLowerCase();
  if (lowered.includes('camera') || lowered.includes('vto') || lowered.includes('doorbell')) {
    return { category: 'devices', key: 'camera' };
  }
  if (lowered.includes('hub')) {
    return { category: 'devices', key: 'hub' };
  }
  if (type === 'panic_button' || inferAjaxButtonDeviceType(model)) {
    return { category: 'devices', key: 'panic_button' };
  }
  if (lowered.includes('multitransmitter') || lowered.includes('multi transmitter')) {
    return { category: 'devices', key: 'multitransmitter' };
  }
  if (lowered.includes('transmitter')) {
    return { category: 'devices', key: 'transmitter' };
  }
  if (lowered.includes('repeater') || lowered.includes('rex')) {
    return { category: 'devices', key: 'repeater' };
  }
  if (lowered.includes('keypad')) {
    return { category: 'devices', key: 'keypad' };
  }
  if (lowered.includes('siren')) {
    return { category: 'devices', key: 'siren' };
  }
  if (lowered.includes('plug') || lowered.includes('socket')) {
    return { category: 'devices', key: 'smart_plug' };
  }
  if (lowered.includes('waterstop') || lowered.includes('water stop')) {
    return { category: 'devices', key: 'waterstop' };
  }
  if (lowered.includes('lightswitch') || lowered.includes('light switch')) {
    return { category: 'devices', key: 'light_switch' };
  }
  if (lowered.includes('wallswitch') || lowered.includes('wall switch')) {
    return { category: 'devices', key: 'wall_switch' };
  }
  if (lowered.includes('relay')) {
    return { category: 'devices', key: 'relay' };
  }
  if (lowered.includes('thermostat')) {
    return { category: 'devices', key: 'thermostat' };
  }
  if (lowered.includes('glass')) {
    return { category: 'devices', key: 'glass_break' };
  }
  if (lowered.includes('curtain')) {
    return { category: 'devices', key: 'curtain_motion' };
  }
  if (lowered.includes('outdoor')) {
    return { category: 'devices', key: 'outdoor_motion' };
  }
  if (lowered.includes('window')) {
    return { category: 'devices', key: 'window_sensor' };
  }
  if (lowered.includes('door') || lowered.includes('opening')) {
    return { category: 'devices', key: 'door_sensor' };
  }
  if (lowered.includes('smoke')) {
    return { category: 'devices', key: 'smoke_detector' };
  }
  if (lowered.includes('fire') || lowered.includes('heat')) {
    return { category: 'devices', key: 'fire_detector' };
  }
  if (lowered.includes('leak') || lowered.includes('water') || lowered.includes('moisture')) {
    return { category: 'devices', key: 'leak_detector' };
  }
  return { category: 'devices', key: 'motion_sensor' };
}

function iconForEvent(eventType: EventType): IconRef {
  switch (eventType) {
    case 'motion_detected':
      return { category: 'events', key: 'motion_detected' };
    case 'human_detected':
      return { category: 'events', key: 'human_detected' };
    case 'vehicle_detected':
      return { category: 'events', key: 'vehicle_detected' };
    case 'tripwire_detected':
      return { category: 'events', key: 'tripwire_detected' };
    case 'intrusion_detected':
      return { category: 'events', key: 'intrusion_detected' };
    case 'door_opened':
      return { category: 'events', key: 'door_opened' };
    case 'window_opened':
      return { category: 'events', key: 'window_opened' };
    case 'fire_detected':
      return { category: 'events', key: 'fire_detected' };
    case 'smoke_detected':
      return { category: 'events', key: 'smoke_detected' };
    case 'leak_detected':
      return { category: 'events', key: 'leak_detected' };
    case 'gas_detected':
      return { category: 'events', key: 'gas_detected' };
    case 'tamper_detected':
      return { category: 'events', key: 'tamper_detected' };
    case 'night_mode':
      return { category: 'events', key: 'night_mode_event' };
    case 'device_offline':
      return { category: 'events', key: 'device_offline' };
    case 'battery_low':
      return { category: 'events', key: 'battery_low_event' };
    case 'power_lost':
      return { category: 'events', key: 'power_lost' };
    case 'alarm':
      return { category: 'events', key: 'alarm' };
    default:
      return { category: 'events', key: 'ok' };
  }
}

function iconForRoom(name: string): IconRef {
  const text = safeString(name).toLowerCase();
  if (includesAny(text, ['garage', 'гараж'])) {
    return { category: 'rooms', key: 'garage' };
  }
  if (includesAny(text, ['kitchen', 'кухня'])) {
    return { category: 'rooms', key: 'kitchen' };
  }
  if (includesAny(text, ['bed', 'спальн'])) {
    return { category: 'rooms', key: 'bedroom' };
  }
  if (includesAny(text, ['bath', 'ванн', 'санвуз', 'туалет'])) {
    return { category: 'rooms', key: 'bathroom' };
  }
  if (includesAny(text, ['office', 'кабінет', 'кабинет', 'офіс', 'офис'])) {
    return { category: 'rooms', key: 'office' };
  }
  if (includesAny(text, ['living', 'вітальн', 'гостин'])) {
    return { category: 'rooms', key: 'living_room' };
  }
  if (includesAny(text, ['kids', 'child', 'дитяч'])) {
    return { category: 'rooms', key: 'kids_room' };
  }
  if (includesAny(text, ['guest', 'гость', 'гостьов'])) {
    return { category: 'rooms', key: 'guests_room' };
  }
  if (includesAny(text, ['attic', 'горищ', 'чердак'])) {
    return { category: 'rooms', key: 'attic' };
  }
  if (includesAny(text, ['server', 'котельн', 'бойлер', 'техніч', 'техничес', 'utility'])) {
    return { category: 'rooms', key: 'server_room' };
  }
  if (includesAny(text, ['yard', 'garden', 'подвір', 'двор', 'терас', 'терасса'])) {
    return { category: 'rooms', key: 'yard' };
  }
  if (includesAny(text, ['house', 'будинок', 'дом'])) {
    return { category: 'rooms', key: 'house' };
  }
  if (includesAny(text, ['хатин', 'гостьовий будинок', 'small house'])) {
    return { category: 'rooms', key: 'small_house' };
  }
  return { category: 'rooms', key: 'house' };
}

function accentForRoom(name: string): string {
  const text = safeString(name).toLowerCase();
  if (includesAny(text, ['garage', 'гараж'])) {
    return '#f59e0b';
  }
  if (includesAny(text, ['kitchen', 'кухня'])) {
    return '#5cff8d';
  }
  if (includesAny(text, ['bed', 'спальн'])) {
    return '#7dd3fc';
  }
  if (includesAny(text, ['bath', 'ванн', 'санвуз', 'туалет', 'yard', 'garden', 'подвір', 'двор', 'терас'])) {
    return '#2de2e6';
  }
  if (includesAny(text, ['office', 'кабінет', 'кабинет', 'офіс', 'офис', 'server', 'котельн', 'бойлер', 'техніч', 'техничес'])) {
    return '#b582ff';
  }
  return '#7dd3fc';
}

function readEntityPicture(state?: HomeAssistantState): string {
  const picture = safeString(state?.attributes.entity_picture) || safeString(state?.attributes.picture);
  return normalizeHomeAssistantImageUrl(picture);
}

function normalizeHomeAssistantImageUrl(url: string): string {
  if (!url) {
    return '';
  }

  return url.replace(/(\/api\/image\/serve\/[^/?]+)\/\d+x\d+(\?.*)?$/i, '$1/original$2');
}

function entityIsAlert(entry: HomeAssistantEntityEntry, state?: HomeAssistantState): boolean {
  if (!state) {
    return false;
  }
  if (isOfflineState(state)) {
    return true;
  }
  if (looksLikeNeutralVtoStatusEntity(entry, state)) {
    return false;
  }
  if (looksLikeDahuaCapabilityStatusEntity(entry, state)) {
    return false;
  }
  if (isNamedDahuaAlertEntity(entry, state)) {
    return isActiveDahuaState(state);
  }
  const domain = entityDomain(entry.entity_id);
  if (domain === 'binary_sensor') {
    const deviceClass = safeString(state.attributes.device_class).toLowerCase();
    if (deviceClass === 'connectivity' || looksLikePositiveConnectivityEntity(entry, state)) {
      return !isPositiveConnectivityState(state);
    }
    if (looksLikeNeutralDahuaBinarySensor(entry, state)) {
      return false;
    }
    if (deviceClass === 'safety') {
      return looksLikeRealSafetyAlert(entry, state);
    }
    if (isIssueBinarySensorDeviceClass(deviceClass) || looksLikeAlertingDahuaBinarySensor(entry, state)) {
      return isActiveDahuaState(state);
    }
    return false;
  }
  if (domain === 'lock') {
    if (looksLikeVtoDoorControlEntity(entry, state)) {
      return false;
    }
    return safeString(state.state).toLowerCase() === 'unlocked';
  }
  const deviceClass = safeString(state.attributes.device_class).toLowerCase();
  return ['problem', 'smoke', 'gas', 'moisture', 'safety'].includes(deviceClass) && state.state !== 'off';
}

function isOfflineState(state?: HomeAssistantState): boolean {
  return state?.state === 'unavailable' || state?.state === 'unknown' || state?.state === 'offline';
}

function hasAvailableState(state?: HomeAssistantState): boolean {
  return Boolean(state) && !isOfflineState(state);
}

function isCameraDomain(entityId: string): boolean {
  const domain = entityDomain(entityId);
  return domain === 'camera' || domain === 'image';
}

function entityDomain(entityId: string): string {
  return entityId.split('.', 1)[0] ?? '';
}

function entityIdLeaf(entityId: string): string {
  return entityId.split('.', 2)[1] ?? entityId;
}

function isOn(state?: HomeAssistantState): boolean {
  return ['on', 'true', '1', 'active', 'detected', 'alarm'].includes(safeString(state?.state).toLowerCase());
}

function toUnix(value: string): number {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function safeString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function includesAny(text: string, needles: string[]): boolean {
  return needles.some((needle) => text.includes(needle));
}

function slugPart(value: unknown): string {
  return safeString(value)
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, '_')
    .replace(/^_+|_+$/g, '');
}

function humanizeSlug(value: string): string {
  const normalized = safeString(value).replace(/_/g, ' ');
  return normalized.replace(/\b\w/g, (match) => match.toUpperCase()) || 'Unknown';
}

function humanizeHomeAssistantState(state: HomeAssistantState): string {
  const value = safeString(state.state).toLowerCase();
  const deviceClass = safeString(state.attributes.device_class).toLowerCase();
  const entityId = safeString(state.entity_id);

  if (value === 'open') {
    return 'Open';
  }
  if (value === 'closed') {
    return 'Closed';
  }
  if (value === 'detected') {
    return 'Detected';
  }
  if (value === 'clear') {
    return 'Clear';
  }
  if (value === 'locked') {
    return 'Locked';
  }
  if (value === 'unlocked') {
    return 'Unlocked';
  }
  if (value === 'waiting') {
    return 'Waiting';
  }
  if (value === 'idle') {
    return 'Idle';
  }
  if (value === 'normal') {
    return 'Normal';
  }
  if (value === 'enabled') {
    return 'Enabled';
  }
  if (value === 'disabled') {
    return 'Disabled';
  }

  if (value === 'on') {
    if (deviceClass === 'connectivity' || looksLikePositiveConnectivityEntity({ entity_id: entityId, attributes: {} }, state)) {
      return 'Online';
    }
    if (['door', 'window', 'opening'].includes(deviceClass)) {
      return 'Open';
    }
    if (deviceClass === 'problem') {
      return 'Problem';
    }
    if (deviceClass === 'safety') {
      return 'Alert';
    }
    if (['motion', 'occupancy', 'presence', 'sound', 'vibration'].includes(deviceClass)) {
      return 'Detected';
    }
    return 'Active';
  }

  if (value === 'off') {
    if (deviceClass === 'connectivity' || looksLikePositiveConnectivityEntity({ entity_id: entityId, attributes: {} }, state)) {
      return 'Offline';
    }
    if (['door', 'window', 'opening'].includes(deviceClass)) {
      return 'Closed';
    }
    if (['problem', 'safety'].includes(deviceClass)) {
      return 'Safe';
    }
    if (['motion', 'occupancy', 'presence', 'sound', 'vibration'].includes(deviceClass)) {
      return 'Not detected';
    }
    return 'Inactive';
  }

  if (value === 'unavailable') {
    return 'Unavailable';
  }

  if (value === 'unknown') {
    return 'Unknown';
  }

  return safeString(state.state) || 'Unknown';
}

function isIgnoredDahuaDevice(
  deviceEntry?: HomeAssistantDeviceEntry,
  linkedEntities: HomeAssistantEntityEntry[] = [],
): boolean {
  const text = [
    deviceEntry?.manufacturer,
    deviceEntry?.model,
    deviceEntry?.name,
    deviceEntry?.name_by_user,
    ...extractIdentifiers(deviceEntry ?? { id: '' }),
    ...linkedEntities.flatMap((entry) => [entry.entity_id, entry.name, entry.original_name, entry.platform]),
  ]
    .map(safeString)
    .join(' ');

  return GO2RTC_HINT.test(text);
}

function looksLikeRealSafetyAlert(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  if (state.state !== 'on') {
    return false;
  }

  const text = [
    entry.entity_id,
    entry.name,
    entry.original_name,
    state.attributes.friendly_name,
    state.attributes.device_class,
  ]
    .map(safeString)
    .join(' ')
    .toLowerCase();

  if (VTO_DEBUG_HINT.test(text)) {
    return false;
  }

  return /(alarm|tamper|panic|intrusion|motion|smoke|fire|gas|safety)/i.test(text);
}

function looksLikePositiveConnectivityEntity(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  if (looksLikeDahuaCapabilityStatusEntity(entry, state)) {
    return false;
  }

  const text = [
    entry.entity_id,
    entry.name,
    entry.original_name,
    state.attributes.friendly_name,
    state.attributes.device_class,
  ]
    .map(safeString)
    .join(' ')
    .toLowerCase();

  return /(^|[\s._-])(online|connected|connectivity|reachable|available)([\s._-]|$)/.test(text);
}

function isPositiveConnectivityState(state?: HomeAssistantState): boolean {
  const value = safeString(state?.state).toLowerCase();
  return ['on', 'online', 'connected', 'available', 'true'].includes(value);
}

function looksLikeNeutralVtoStatusEntity(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  const text = dahuaEntityText(entry, state);
  return /(call state|bridge session|bridge uplink|external uplink|alarm enable|sensor enabled|sense method|lock mode|unlock hold interval|current profile|audio codec|main codec|sub codec|resolution|lock count|supports? )/.test(text);
}

function looksLikeNeutralDahuaBinarySensor(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  if (looksLikeDahuaCapabilityStatusEntity(entry, state)) {
    return true;
  }

  const text = dahuaEntityText(entry, state);
  return /(enabled|recording|record|ready|profile|stream|snapshot|supported|support|capability|uplink|output|export)/.test(text)
    || looksLikeNeutralVtoStatusEntity(entry, state);
}

function looksLikeDahuaCapabilityStatusEntity(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  const text = dahuaEntityText(entry, state);
  return /(onvif|h\.?264|h\.?265|codec|profile|resolution|snapshot|rtsp|mjpeg|hls|dash|webrtc|capability|supported|support|stream_url|stream source|preferred video|video fallback|recording|output|export)/.test(text)
    || /\b(stream|snapshot|onvif|h264|h265|rtsp|mjpeg|hls|dash|webrtc)[\s._-]+available\b/.test(text);
}

function isIssueBinarySensorDeviceClass(deviceClass: string): boolean {
  return ['door', 'window', 'opening', 'motion', 'occupancy', 'presence', 'sound', 'vibration', 'problem', 'smoke', 'gas', 'moisture'].includes(deviceClass);
}

function looksLikeAlertingDahuaBinarySensor(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  if (looksLikePositiveConnectivityEntity(entry, state) || looksLikeNeutralDahuaBinarySensor(entry, state)) {
    return false;
  }

  const text = dahuaEntityText(entry, state);
  return /(alarm|tamper|panic|intrusion|motion|human|vehicle|cross line|cross region|video blind|video loss|audio mutation|button pressed|invite|call no answered|door status|window|opening|smoke|fire|gas|leak|flood)/.test(text);
}

function looksLikeVtoDoorControlEntity(entry: HomeAssistantEntityEntry, state: HomeAssistantState): boolean {
  const text = dahuaEntityText(entry, state);
  return /(open door|unlock|door release|door relay|gate|strike|vto lock|_lock_\d+)/.test(text);
}

function debugDahuaCandidate(
  name: string,
  model: string,
  entityEntries: HomeAssistantEntityEntry[],
  states: Record<string, HomeAssistantState>,
  activeEntry: HomeAssistantEntityEntry,
  activeState?: HomeAssistantState,
): void {
  const debugText = [
    name,
    model,
    activeEntry.entity_id,
    activeEntry.name,
    activeEntry.original_name,
    activeState?.attributes.friendly_name,
    activeState?.attributes.device_class,
  ]
    .map(safeString)
    .join(' ');

  if (!VTO_DEBUG_HINT.test(debugText) && safeString(activeState?.attributes.device_class).toLowerCase() !== 'safety') {
    return;
  }

  const debugKey = `${name}|${activeEntry.entity_id}`;
  if (loggedDahuaDebug.has(debugKey)) {
    return;
  }
  loggedDahuaDebug.add(debugKey);

  console.log('[ajaxbridge][dahua-debug]', {
    name,
    model,
    chosenEntity: activeEntry.entity_id,
    chosenState: activeState?.state,
    chosenDeviceClass: safeString(activeState?.attributes.device_class),
    chosenFriendlyName: safeString(activeState?.attributes.friendly_name),
    candidates: entityEntries.map((entry) => {
      const state = states[entry.entity_id];
      return {
        entity_id: entry.entity_id,
        registry_name: safeString(entry.name) || safeString(entry.original_name),
        state: state?.state ?? 'missing',
        device_class: safeString(state?.attributes.device_class),
        friendly_name: safeString(state?.attributes.friendly_name),
      };
    }),
  });
}
