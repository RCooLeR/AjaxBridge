import type { GlowTone } from '../models/dashboard';

const SIGNALS = new Set(['alarm', 'burglary', 'panic', 'duress', 'emergency', 'medical', 'hold_up', 'fire', 'smoke', 'co', 'gas', 'gas_or_co', 'water_leak', 'leak', 'flood', 'tamper', 'connectivity', 'battery', 'power', 'hardware', 'interference', 'accelerometer', 'fire_detector', 'configuration', 'firmware', 'supervision', 'button', 'arming', 'night_mode', 'temperature']);
export const SECURITY_SIGNALS = new Set(['alarm', 'burglary', 'panic', 'duress', 'emergency', 'medical', 'hold_up', 'fire', 'smoke', 'co', 'gas', 'gas_or_co', 'water_leak', 'leak', 'flood', 'tamper', 'temperature']);
const ALIASES: Record<string, string[]> = {
  alarm_active: ['alarm_active', 'alarm', 'alarme', 'тривога'],
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

/** Display names and HA-generated numeric suffixes are fallbacks, never identities. */
export function ajaxEntitySemantic(entry: { entity_id: string; unique_id?: string | null; name?: string | null; original_name?: string | null }, attributes: Record<string, unknown> = {}): string | null {
  const unique = text(entry.unique_id).toLowerCase();
  const stable = unique.match(/^ajaxbridge_(?:zone_[^_]+_\d+|account_[^_]+)_(.+)$/)?.[1];
  if (stable) return canonicalSemantic(stable);
  for (const value of [attributes.metric, attributes.logical_id, entry.original_name, entry.name, attributes.friendly_name, entry.entity_id.split('.').slice(1).join('.')]) {
    const descriptor = normalized(value).replace(/_\d+$/, '');
    const signal = descriptor.match(/(?:^|_)signal_(.+)$/)?.[1];
    if (signal && SIGNALS.has(canonicalSemantic(`signal_${signal}`).slice(7))) return canonicalSemantic(`signal_${signal}`);
    for (const [semantic, aliases] of Object.entries(ALIASES)) {
      if (aliases.some((alias) => descriptor === alias || descriptor.endsWith(`_${alias}`))) return semantic;
    }
    // Old discoveries used translated display names for physical-input signals.
    if (['panic', 'panique', 'паніка'].some((alias) => descriptor === alias || descriptor.endsWith(`_${alias}`))) return 'signal_panic';
  }
  if (entry.entity_id.startsWith('binary_sensor.')) {
    const deviceClass = text(attributes.device_class).toLowerCase();
    const binarySemantics: Record<string, string> = { smoke: 'signal_smoke', gas: 'signal_gas', moisture: 'signal_water_leak', tamper: 'tamper_active', problem: 'trouble_active', battery: 'battery_percent', connectivity: 'connectivity' };
    return binarySemantics[deviceClass] ?? null;
  }
  if (entry.entity_id.startsWith('sensor.') && text(attributes.device_class).toLowerCase() === 'battery') return 'battery_percent';
  return null;
}

function canonicalSemantic(value: string): string {
  if (value === 'signal_power_failure') return 'signal_power';
  if (value === 'signal_temperature_alarm') return 'signal_temperature';
  return value;
}

export function ajaxEntityOwner(uniqueId: unknown, attributes: Record<string, unknown>): string | null {
  const sia = text(uniqueId).match(/^ajaxbridge_zone_([^_]+)_(\d+)_/i);
  if (sia) return `sia_${sia[1].toLowerCase()}_zone_${sia[2]}`;
  const slug = text(attributes.device_slug).toLowerCase();
  if (/^(?:sia_[^_]+_zone_\d+|account_[^_]+)$/.test(slug)) return slug;
  const account = text(uniqueId).match(/^ajaxbridge_account_([^_]+)_/i);
  return account ? `account_${account[1].toLowerCase()}` : null;
}

export function ajaxRegistryOwner(identifiers: string[]): string | null {
  for (const identifier of identifiers) {
    const zone = identifier.match(/^ajaxbridge_([^_]+)_zone_(\d+)$/i);
    if (zone) return `sia_${zone[1].toLowerCase()}_zone_${zone[2]}`;
    const account = identifier.match(/^ajaxbridge_account_([^_]+)$/i);
    if (account) return `account_${account[1].toLowerCase()}`;
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
  const connectivity = observations.filter((item) => item.semantic === 'connectivity' || (item.deviceClass === 'connectivity' && item.semantic !== 'signal_connectivity'));
  const faultLinks = observations.filter((item) => item.semantic === 'signal_connectivity');
  const unknown = (value: string) => ['', 'unknown', 'unavailable', 'offline', 'none', 'null'].includes(value.trim().toLowerCase());
  const linkUnknown = [...connectivity, ...faultLinks].some((item) => unknown(item.value));
  const explicitOffline = activeSignals.includes('connectivity') || connectivity.some((item) => ['off', 'false', '0', 'offline', 'disconnected'].includes(item.value.toLowerCase()));
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
