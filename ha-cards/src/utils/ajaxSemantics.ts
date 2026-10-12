import type { GlowTone } from '../models/dashboard';

const SIGNALS = new Set(['alarm', 'burglary', 'panic', 'duress', 'emergency', 'medical', 'hold_up', 'fire', 'smoke', 'co', 'gas', 'gas_or_co', 'water_leak', 'leak', 'flood', 'tamper', 'connectivity', 'battery', 'power', 'hardware', 'interference', 'accelerometer', 'fire_detector', 'configuration', 'firmware', 'supervision', 'button', 'arming', 'night_mode', 'temperature']);
export const SECURITY_SIGNALS = new Set(['alarm', 'burglary', 'panic', 'duress', 'emergency', 'medical', 'hold_up', 'fire', 'smoke', 'co', 'gas', 'gas_or_co', 'water_leak', 'leak', 'flood', 'tamper', 'temperature']);
const CONNECTIVITY_CHANNEL = /(?:^|_)(?:gsm|cms|ethernet|cellular|mobile|radio|photo_channel|jeweller|wings|lan|wi_?fi|cloud)(?:_|$)/;
const ALIASES: Record<string, string[]> = {
  alarm_active: ['alarm_active', 'input_alarm', 'alarm', 'alarme', 'тривога'],
  tamper_active: ['tamper_active', 'tamper', 'sabotage', 'саботаж'],
  trouble_active: ['trouble_active', 'trouble', 'problem', 'несправність'],
  last_event_name: ['last_event_name', 'last_event', 'dernier_evenement'],
  last_event_at: ['last_event_at', 'last_event_time'],
  last_signal: ['last_signal'], alarm_signal: ['alarm_signal'],
  battery_percent: ['battery_percent', 'batterychargelevelpercentage', 'battery', 'batterie', 'батарея'],
  battery_check_status: ['battery_check_status', 'batterycheckstatus', 'battery_check', 'test_de_la_batterie', 'controle_de_la_batterie'],
  battery_state: ['battery_state', 'etat_de_la_batterie'],
  issue_count: ['issue_count', 'issuescount', 'issues', 'nombre_de_defauts', 'кількість_несправностей'],
  firmware_version: ['firmware_version', 'firmwareversion', 'version_du_firmware'],
  valve_position: ['valve_position', 'valveposition', 'etat_de_la_vanne', 'position_de_la_vanne'],
  operating_mode: ['operating_mode', 'button_mode', 'mode_de_fonctionnement'],
  device_last_update: ['device_last_update', 'derniere_mise_a_jour'],
  connectivity: ['connectivity', 'online', 'connected', 'connection', 'connexion', 'зв’язок'],
  state: ['state', 'etat'],
};

function text(value: unknown): string { return typeof value === 'string' ? value.trim() : ''; }
function normalized(value: unknown): string {
  return text(value).normalize('NFD').replace(/\p{M}/gu, '').toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '_').replace(/^_+|_+$/g, '');
}

/** Source prefixes are part of identity; an omitted prefix is the legacy installation. */
export function ajaxIdentity(value: unknown): { identity: string; sourceId: string } | null {
  const raw = text(value);
  const separator = raw.indexOf(':ajaxbridge_');
  if (separator > 0 && /^[a-z0-9][a-z0-9_-]{0,63}$/.test(raw.slice(0, separator))) {
    return { identity: raw.slice(separator + 1), sourceId: raw.slice(0, separator) };
  }
  return /^ajaxbridge_/i.test(raw) ? { identity: raw, sourceId: '' } : null;
}

export function ajaxSourceId(uniqueId: unknown, attributes: Record<string, unknown> = {}, identifiers: string[] = []): string {
  const uniqueSource = ajaxIdentity(uniqueId)?.sourceId;
  if (uniqueSource) return uniqueSource;
  const attributeSource = text(attributes.source_id);
  if (attributeSource) return attributeSource;
  return identifiers.map((identifier) => ajaxIdentity(identifier)?.sourceId).find(Boolean) ?? '';
}

function sourceOwner(owner: string, sourceId: string): string {
  return sourceId ? `${sourceId}::${owner}` : owner;
}

export function ajaxOwnerIdentity(owner: string): string {
  const separator = owner.lastIndexOf('::');
  return separator < 0 ? owner : owner.slice(separator + 2);
}

export function ajaxOwnerAccount(owner: string): string | null {
  const identity = ajaxOwnerIdentity(owner);
  return identity.match(/^sia_(.+)_zone_\d+$/)?.[1] ?? identity.match(/^account_(.+)$/)?.[1] ?? null;
}

/** Display names and HA-generated numeric suffixes are fallbacks, never identities. */
export function ajaxEntitySemantic(entry: { entity_id: string; unique_id?: string | null; name?: string | null; original_name?: string | null }, attributes: Record<string, unknown> = {}): string | null {
  const unique = (ajaxIdentity(entry.unique_id)?.identity ?? text(entry.unique_id)).toLowerCase();
  const stable = unique.match(/^ajaxbridge_(?:zone_[^_]+_\d+|account_[^_]+)_(.+)$/)?.[1];
  if (stable) return canonicalSemantic(stable);
  for (const value of [attributes.metric, attributes.logical_id, entry.original_name, entry.name, attributes.friendly_name, entry.entity_id.split('.').slice(1).join('.')]) {
    const descriptor = normalized(value).replace(/_\d+$/, '');
    const signal = descriptor.match(/(?:^|_)signal_(.+)$/)?.[1];
    if (signal && SIGNALS.has(canonicalSemantic(`signal_${signal}`).slice(7))) return canonicalSemantic(`signal_${signal}`);
    for (const [semantic, aliases] of Object.entries(ALIASES)) {
      if (aliases.some((alias) => descriptor === alias || descriptor.endsWith(`_${alias}`))) {
        return semantic === 'connectivity' && CONNECTIVITY_CHANNEL.test(descriptor) ? 'connectivity_channel' : semantic;
      }
    }
    // Old discoveries used translated display names for physical-input signals.
    if (['panic', 'panique', 'паніка'].some((alias) => descriptor === alias || descriptor.endsWith(`_${alias}`))) return 'signal_panic';
  }
  if (entry.entity_id.startsWith('binary_sensor.')) {
    const deviceClass = text(attributes.device_class).toLowerCase();
    // A connectivity device class alone does not say whether an entity is the
    // overall device status or one optional transport channel. Name/identity
    // aliases above identify authoritative online entities; health resolution
    // handles the remaining connectivity-class entities as channel fallbacks.
    const binarySemantics: Record<string, string> = { smoke: 'signal_smoke', gas: 'signal_gas', moisture: 'signal_water_leak', tamper: 'tamper_active', problem: 'trouble_active', battery: 'battery_percent' };
    return binarySemantics[deviceClass] ?? null;
  }
  if (entry.entity_id.startsWith('sensor.') && text(attributes.device_class).toLowerCase() === 'battery') return 'battery_percent';
  return null;
}

function canonicalSemantic(value: string): string {
  if (value === 'input_alarm') return 'alarm_active';
  if (value === 'signal_power_failure') return 'signal_power';
  if (value === 'signal_temperature_alarm') return 'signal_temperature';
  if (ALIASES.connectivity.includes(value)) return 'connectivity';
  return value;
}

export function ajaxEntityOwner(uniqueId: unknown, attributes: Record<string, unknown>): string | null {
  const identity = ajaxIdentity(uniqueId);
  const sourceId = ajaxSourceId(uniqueId, attributes);
  const sia = text(identity?.identity).match(/^ajaxbridge_zone_([^_]+)_(\d+)_/i);
  if (sia) return sourceOwner(`sia_${sia[1].toLowerCase()}_zone_${sia[2]}`, sourceId);
  const slug = text(attributes.device_slug).toLowerCase();
  if (/^(?:sia_[^_]+_zone_\d+|account_[^_]+)$/.test(slug)) return sourceOwner(slug, sourceId);
  const account = text(identity?.identity).match(/^ajaxbridge_account_([^_]+)_/i);
  return account ? sourceOwner(`account_${account[1].toLowerCase()}`, sourceId) : null;
}

export function ajaxRegistryOwner(identifiers: string[]): string | null {
  for (const identifier of identifiers) {
    const identity = ajaxIdentity(identifier);
    if (!identity) continue;
    const zone = identity.identity.match(/^ajaxbridge_([^_]+)_zone_(\d+)$/i);
    if (zone) return sourceOwner(`sia_${zone[1].toLowerCase()}_zone_${zone[2]}`, identity.sourceId);
    const account = identity.identity.match(/^ajaxbridge_account_([^_]+)$/i);
    if (account) return sourceOwner(`account_${account[1].toLowerCase()}`, identity.sourceId);
  }
  return null;
}

export function diagnosticHealth(kind: string, raw: string, binary = false): { issue: boolean; tone: GlowTone } {
  const value = raw.trim().toLowerCase();
  const number = value === '' ? NaN : Number(value.replace(',', '.'));
  if (['', 'unknown', 'unavailable', 'none', 'null'].includes(value)) return { issue: false, tone: 'slate' };
  if ((kind === 'issue_count' || (kind === 'battery_percent' && !binary)) && (!Number.isFinite(number) || number < 0 || (kind === 'battery_percent' && number > 100))) return { issue: false, tone: 'slate' };
  let issue = false;
  if (kind === 'battery_percent') issue = binary ? ['on', 'true', '1', 'low'].includes(value) : Number.isFinite(number) && number >= 0 && number <= 20;
  if (kind === 'issue_count') issue = Number.isFinite(number) && number > 0;
  if (kind === 'battery_check_status') issue = ['failed', 'fail', 'failure', 'error', 'bad', 'low', 'defective', 'not_ok', 'battery_test_failed'].includes(value);
  return { issue, tone: issue ? 'amber' : 'green' };
}

export interface HealthObservation { semantic: string | null; domain: string; value: string; deviceClass: string }
export function ajaxDeviceHealth(observations: HealthObservation[], activeSignals: string[]): { online: boolean; connectivity: string; warning: string; severity: number } {
  const explicitConnectivity = observations.filter((item) => item.semantic === 'connectivity');
  const inferredConnectivity = observations.filter((item) => item.deviceClass === 'connectivity' && item.semantic !== 'signal_connectivity' && item.semantic !== 'connectivity');
  // An explicit overall online/connected entity is authoritative. Transport
  // channels such as GSM, Ethernet and CMS may legitimately be unavailable or
  // disconnected while the Hub remains reachable through another channel.
  const connectivity = explicitConnectivity.length > 0 ? explicitConnectivity : inferredConnectivity;
  const faultLinks = observations.filter((item) => item.semantic === 'signal_connectivity');
  const unknown = (value: string) => ['', 'unknown', 'unavailable', 'offline', 'none', 'null'].includes(value.trim().toLowerCase());
  const connected = (value: string) => ['on', 'true', '1', 'online', 'connected', 'available'].includes(value.trim().toLowerCase());
  const disconnected = (value: string) => ['off', 'false', '0', 'offline', 'disconnected'].includes(value.trim().toLowerCase());
  const linkUnknown = explicitConnectivity.length > 0
    ? connectivity.some((item) => unknown(item.value))
    : connectivity.length > 0
      ? !connectivity.some((item) => connected(item.value)) && connectivity.some((item) => unknown(item.value))
      : faultLinks.some((item) => unknown(item.value));
  const explicitOffline = activeSignals.includes('connectivity') || (explicitConnectivity.length > 0
    ? connectivity.some((item) => disconnected(item.value))
    : connectivity.length > 0 && connectivity.every((item) => disconnected(item.value)));
  const current = observations.filter((item) => item.domain !== 'button' && !['last_event_name', 'last_event_at', 'last_signal', 'alarm_signal'].includes(item.semantic ?? ''));
  const noCurrentState = current.length > 0 && current.every((item) => unknown(item.value));
  if (explicitOffline) return { online: false, connectivity: 'Offline', warning: 'Connectivity issue', severity: 3 };
  if (linkUnknown || noCurrentState) return { online: false, connectivity: 'Unknown', warning: 'Connectivity unknown', severity: 2 };
  if (observations.some((item) => diagnosticHealth(item.semantic ?? '', item.value, item.domain === 'binary_sensor').issue)) {
    const batteryIssue = observations.some((item) => (item.semantic === 'battery_percent' || item.semantic === 'battery_check_status') && diagnosticHealth(item.semantic, item.value, item.domain === 'binary_sensor').issue);
    return { online: true, connectivity: 'Online', warning: batteryIssue ? 'Battery attention' : 'Device issues', severity: 2 };
  }
  return { online: true, connectivity: 'Online', warning: '', severity: 0 };
}
