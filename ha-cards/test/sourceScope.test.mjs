import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { rolldown } from 'rolldown';

const root = resolve(import.meta.dirname, '..').replaceAll('\\', '/');
const directory = await mkdtemp(join(tmpdir(), 'ajaxbridge-source-scope-test-'));
const globals = Object.fromEntries(['HTMLElement', 'document', 'window', 'customElements', '__cardScopeCalls', '__hookRegistry'].map((key) => [key, globalThis[key]]));
after(async () => {
  await rm(directory, { recursive: true, force: true });
  for (const [key, value] of Object.entries(globals)) {
    if (value === undefined) delete globalThis[key];
    else globalThis[key] = value;
  }
});

const registeredCards = new Map();
globalThis.HTMLElement = class {
  style = {};
  attachShadow() {
    this.shadowRoot = { replaceChildren() {}, appendChild() {} };
    return this.shadowRoot;
  }
};
globalThis.document = { createElement: () => ({ className: '', textContent: '' }) };
globalThis.window = { location: { origin: 'https://ha.example' } };
globalThis.customElements = {
  get: (name) => registeredCards.get(name),
  define: (name, constructor) => registeredCards.set(name, constructor),
};
globalThis.__cardScopeCalls = [];

const bundle = await rolldown({
  input: 'scope-test-harness',
  platform: 'node',
  plugins: [{
    name: 'scope-harness',
    resolveId(id, importer) {
      if (id === 'scope-test-harness') return '\0scope-test-harness';
      if (id === 'react-dom/client') return '\0scope-root';
      if (id === 'react' && importer?.replaceAll('\\', '/').endsWith('/src/data/liveDashboardData.ts')) return '\0scope-react-hooks';
      if (id.endsWith('?raw')) return '\0scope-css';
      if (id === '../data/liveDashboardData' && importer?.replaceAll('\\', '/').includes('/src/views/')) return '\0scope-hook';
    },
    load(id) {
      if (id === '\0scope-css') return 'export default "";';
      if (id === '\0scope-react-hooks') return `
        export function useState(initial) { return [initial === null ? globalThis.__hookRegistry ?? null : initial, () => {}]; }
        export function useMemo(factory) { return factory(); }
        export function useEffect() {}
        export function useEffectEvent(callback) { return callback; }
      `;
      if (id === '\0scope-root') return `
        import { renderToStaticMarkup } from 'react-dom/server';
        export function createRoot() { return { render(node) { renderToStaticMarkup(node); }, unmount() {} }; }
      `;
      if (id === '\0scope-hook') return `
        export function useDashboardData(hass, account, dahuaBase, gridEntities, scope) {
          globalThis.__cardScopeCalls.push({ account, dahuaBase, gridEntities, scope });
          return { rooms: [], devices: [], events: [], systemState: { chips: [] } };
        }
      `;
      if (id !== '\0scope-test-harness') return;
      return `
        export { buildRegistryIndex, buildDashboardDataFromHomeAssistant, scopeDashboardInputs, useDashboardData, loadSmdIvsCountsByRoom, dahuaBridgeChannelSignature } from '${root}/src/data/liveDashboardData.ts';
        export { ajaxEntitySemantic, ajaxEntityOwner, ajaxIdentity, ajaxRegistryOwner, ajaxSourceId } from '${root}/src/utils/ajaxSemantics.ts';
        import '${root}/src/ha/register.tsx';
      `;
    },
    transform(code, id) {
      if (id.replaceAll('\\', '/').endsWith('/src/data/liveDashboardData.ts')) {
        return { code: `${code}\nexport { loadSmdIvsCountsByRoom, dahuaBridgeChannelSignature };`, map: null };
      }
    },
  }],
});
const output = join(directory, 'harness.mjs');
await bundle.write({ file: output, format: 'esm' });
await bundle.close();
const {
  buildRegistryIndex, buildDashboardDataFromHomeAssistant, scopeDashboardInputs, useDashboardData,
  loadSmdIvsCountsByRoom, dahuaBridgeChannelSignature,
  ajaxEntitySemantic, ajaxEntityOwner, ajaxIdentity, ajaxRegistryOwner, ajaxSourceId,
} = await import(pathToFileURL(output).href);

function multiSiteFixture() {
  const areas = [{ area_id: 'house-room', name: 'Кухня будинок' }, { area_id: 'flat-room', name: 'Кухня квартира' }];
  const devices = [];
  const entities = [];
  const states = {};
  function addDevice(id, source, area, model = 'WallSwitch', identifiers) {
    devices.push({ id, name: 'Кухня', model, manufacturer: 'Ajax Systems', area_id: area,
      identifiers: identifiers ?? [['mqtt', `${source}:ajaxbridge_A0F80D_zone_2`]] });
  }
  function addEntity(id, device, unique, value, attributes = {}, metadata = {}) {
    entities.push({ entity_id: id, device_id: device, unique_id: unique, ...metadata });
    states[id] = { entity_id: id, state: value, attributes, last_changed: '2026-10-08T10:00:00Z' };
  }
  for (const [source, area] of [['house', 'house-room'], ['apartment', 'flat-room']]) {
    addDevice(`${source}-switch`, source, area);
    addEntity(`switch.${source}`, `${source}-switch`, `${source}:ajaxbridge_jeedom_control_sia_a0f80d_zone_2`, source === 'house' ? 'off' : 'on',
      { source_id: source, device_slug: 'sia_a0f80d_zone_2', metric: 'state' });
    // Both Jeedom installations deliberately reuse command ID 56.
    addEntity(`sensor.${source}_temperature`, `${source}-switch`, `${source}:ajaxbridge_jeedom_cmd_56`, source === 'house' ? '90' : '21',
      { source_id: source, device_slug: 'sia_a0f80d_zone_2', metric: 'temperature', device_class: 'temperature', unit_of_measurement: '°C' });
    addEntity(`binary_sensor.${source}_alarm`, `${source}-switch`, `${source}:ajaxbridge_zone_a0f80d_2_alarm_active`, source === 'house' ? 'on' : 'off', {});
    addDevice(`${source}-hub`, source, area, 'Ajax account', [['mqtt', `${source}:ajaxbridge_account_A0F80D`]]);
    addEntity(`sensor.${source}_mode`, `${source}-hub`, `${source}:ajaxbridge_account_a0f80d_mode`, source === 'house' ? 'armed' : 'disarmed', {});
    devices.push({ id: `${source}-camera`, name: 'Camera', model: 'IPC', manufacturer: 'Dahua', area_id: area });
    addEntity(`camera.${source}`, `${source}-camera`, `dahua_${source}_camera`, 'idle', {
      bridge_device_kind: 'nvr_channel', bridge_base_url: `https://${source}.example/bridge`, bridge_root_device_id: 'nvr', bridge_channel: 1,
    });
  }
  addDevice('unlinked', '', 'flat-room', 'MotionProtect', [['mqtt', 'ajaxbridge_jeedom_unknown']]);
  addEntity('sensor.unlinked', 'unlinked', 'ajaxbridge_jeedom_cmd_56', '80', { device_class: 'temperature', unit_of_measurement: '°C' });
  // A foreign source deliberately shares the selected HA area, testing source
  // filtering before climate averaging and hero camera selection.
  addDevice('foreign-same-room', 'house', 'flat-room', 'MotionProtect', [['mqtt', 'house:ajaxbridge_A0F80D_zone_3']]);
  addEntity('sensor.foreign_same_room', 'foreign-same-room', 'house:ajaxbridge_zone_a0f80d_3_temperature', '100',
    { source_id: 'house', device_class: 'temperature', unit_of_measurement: '°C' });
  addEntity('camera.foreign_same_room', 'foreign-same-room', 'foreign_camera', 'idle',
    { source_id: 'house', bridge_device_kind: 'nvr_channel', bridge_base_url: 'https://house.example/bridge', bridge_root_device_id: 'nvr', bridge_channel: 2 });
  return { states, registry: buildRegistryIndex({ areas, devices, entities }) };
}

function dashboard(fixture, scope = {}) {
  return buildDashboardDataFromHomeAssistant(fixture.states, fixture.registry, 'A0F80D', {}, [], false, scope);
}

test('source namespaces preserve semantics and keep identical account, zone and Jeedom IDs separate', () => {
  const fixture = multiSiteFixture();
  const house = dashboard(fixture, { sourceId: 'house' });
  const apartment = dashboard(fixture, { sourceId: 'apartment' });
  assert.ok(house.devices.some((device) => device.id === 'house::sia_a0f80d_zone_2'));
  assert.ok(apartment.devices.some((device) => device.id === 'apartment::sia_a0f80d_zone_2'));
  assert.equal(ajaxEntitySemantic({ entity_id: 'binary_sensor.renamed_80', unique_id: 'apartment:ajaxbridge_zone_a0f80d_2_alarm_active' }), 'alarm_active');
  assert.equal(ajaxEntityOwner('apartment:ajaxbridge_jeedom_cmd_56', { device_slug: 'sia_a0f80d_zone_2' }), 'apartment::sia_a0f80d_zone_2');
  assert.equal(ajaxRegistryOwner(['apartment:ajaxbridge_A0F80D_zone_2']), 'apartment::sia_a0f80d_zone_2');
  assert.equal(ajaxSourceId('house:ajaxbridge_zone_a0f80d_2_alarm_active', { source_id: 'apartment' }), 'house');
  assert.equal(ajaxSourceId('site_ajaxbridge_backup:ajaxbridge_zone_a0f80d_2_alarm_active'), 'site_ajaxbridge_backup');
});

test('legacy slugs and source IDs containing ajaxbridge have unambiguous identities without state attributes', () => {
  const legacy = 'ajaxbridge_jeedom_control_heater_ajaxbridge_monitor';
  assert.deepEqual(ajaxIdentity(legacy), { identity: legacy, sourceId: '' });
  assert.equal(ajaxSourceId(legacy), '');
  for (const sourceId of ['ajaxbridge', 'home_ajaxbridge_lab', 'home_ajaxbridge_jeedom_heater']) {
    assert.deepEqual(ajaxIdentity(`${sourceId}:${legacy}`), { identity: legacy, sourceId });
    const device = { id: sourceId, manufacturer: 'Ajax Systems', model: 'MotionProtect', name: 'Detector', area_id: 'room', identifiers: [['mqtt', `${sourceId}:ajaxbridge_A0F80D_zone_2`]] };
    const entity = { entity_id: 'binary_sensor.arbitrary', unique_id: `${sourceId}:ajaxbridge_zone_a0f80d_2_alarm_active`, device_id: sourceId };
    const states = { 'binary_sensor.arbitrary': { state: 'unavailable', attributes: {} } };
    const registry = buildRegistryIndex({ areas: [{ area_id: 'room', name: 'Room' }], devices: [device], entities: [entity] });
    assert.equal(buildDashboardDataFromHomeAssistant(states, registry, undefined, {}, [], false, { sourceId }).devices[0].id, `${sourceId}::sia_a0f80d_zone_2`);
    assert.deepEqual(buildDashboardDataFromHomeAssistant(states, registry).devices, []);
  }
});

test('source-only card excludes unmapped Jeedom and every ancillary source lacking a source identity', () => {
  const data = dashboard(multiSiteFixture(), { sourceId: 'apartment' });
  assert.deepEqual(data.rooms.map((room) => room.id), ['flat-room']);
  assert.deepEqual(data.devices.map((device) => device.id), ['apartment::sia_a0f80d_zone_2']);
  assert.match(data.rooms[0].climate.temperature, /^21/);
  assert.doesNotMatch(data.rooms[0].image, /camera\.(?:house|foreign|apartment)/);
  assert.equal(data.systemState.chips.find((chip) => chip.id === 'system-mode').value, 'Disarmed');
  assert.equal(data.systemState.chips.find((chip) => chip.id === 'system-outlets').value, '1/1');
  assert.equal(data.systemState.chips.find((chip) => chip.id === 'system-alerts').value, '0');
  assert.ok(data.events.every((event) => event.deviceId === 'apartment::sia_a0f80d_zone_2'));
  assert.deepEqual(data.devices.flatMap((device) => device.actions ?? []).map((action) => action.entityId), ['switch.apartment']);
  const serialized = JSON.stringify(data);
  assert.doesNotMatch(serialized, /switch\.house|sensor\.house|unlinked|foreign_same_room/);
});

test('explicit areas add only local ancillary devices and retain strict Ajax source isolation', () => {
  const data = dashboard(multiSiteFixture(), { sourceId: 'apartment', areaIds: ['flat-room'] });
  assert.deepEqual(data.rooms.map((room) => room.id), ['flat-room']);
  assert.deepEqual(new Set(data.devices.map((device) => device.id)), new Set(['apartment::sia_a0f80d_zone_2', 'dahua:apartment-camera']));
  assert.match(data.rooms[0].climate.temperature, /^21/);
  assert.doesNotMatch(JSON.stringify(data), /house|unlinked|foreign_same_room/);
  const none = dashboard(multiSiteFixture(), { sourceId: 'apartment', areaIds: [] });
  assert.deepEqual(none.rooms, []);
  assert.deepEqual(none.devices, []);
  assert.deepEqual(none.systemState.chips, []);
});

test('ancillary source metadata allows source-only camera selection without admitting unassigned cameras', () => {
  const fixture = multiSiteFixture();
  fixture.states['camera.apartment'].attributes.source_id = 'apartment';
  const data = dashboard(fixture, { sourceId: 'apartment' });
  assert.deepEqual(new Set(data.devices.map((device) => device.id)), new Set(['apartment::sia_a0f80d_zone_2', 'dahua:apartment-camera']));
  assert.deepEqual(data.rooms.map((room) => room.id), ['flat-room']);
  assert.doesNotMatch(JSON.stringify(data), /house|unlinked|foreign_same_room/);
});

test('registry namespace filters unavailable states, and legacy metadata sources still work', () => {
  const fixture = multiSiteFixture();
  delete fixture.states['binary_sensor.apartment_alarm'];
  fixture.states['switch.apartment'].state = 'unavailable';
  delete fixture.states['switch.apartment'].attributes.source_id;
  delete fixture.states['sensor.apartment_temperature'].attributes.source_id;
  assert.equal(dashboard(fixture, { sourceId: 'apartment' }).devices.length, 1);
  const legacy = {
    areas: [{ area_id: 'room', name: 'Room' }],
    devices: [{ id: 'legacy', name: 'Legacy', model: 'MotionProtect', manufacturer: 'Ajax Systems', area_id: 'room', identifiers: [['mqtt', 'ajaxbridge_A0F80D_zone_2']] }],
    entities: [{ entity_id: 'binary_sensor.legacy', device_id: 'legacy', unique_id: 'ajaxbridge_zone_a0f80d_2_alarm_active' }],
  };
  const states = { 'binary_sensor.legacy': { state: 'off', attributes: {} } };
  const registry = buildRegistryIndex(legacy);
  assert.equal(buildDashboardDataFromHomeAssistant(states, registry).devices[0].id, 'sia_a0f80d_zone_2');
  assert.equal(buildDashboardDataFromHomeAssistant(states, registry, undefined, {}, [], false, { sourceId: 'apartment' }).devices.length, 0);
  states['binary_sensor.legacy'].attributes.source_id = 'apartment';
  assert.equal(buildDashboardDataFromHomeAssistant(states, registry, undefined, {}, [], false, { sourceId: 'apartment' }).devices[0].id, 'apartment::sia_a0f80d_zone_2');
});

test('area-only scoping preserves legacy selection and excludes foreign rooms from all data paths', () => {
  const fixture = multiSiteFixture();
  const data = dashboard(fixture, { areaIds: ['house-room'] });
  assert.deepEqual(data.rooms.map((room) => room.id), ['house-room']);
  assert.ok(data.devices.every((device) => device.roomId === 'house-room'));
  assert.doesNotMatch(JSON.stringify(data), /apartment|unlinked|foreign_same_room/);
  assert.ok(dashboard(fixture).devices.some((device) => device.id === 'unlinked'));
});

test('omitted and empty source select the legacy namespace even when names, account and zone overlap', () => {
  const fixture = multiSiteFixture();
  const legacyDevice = { id: 'legacy-switch', name: 'Кухня', model: 'WallSwitch', manufacturer: 'Ajax Systems', area_id: 'house-room', identifiers: [['mqtt', 'ajaxbridge_A0F80D_zone_2']] };
  const legacyEntry = { entity_id: 'switch.legacy', device_id: 'legacy-switch', unique_id: 'ajaxbridge_jeedom_control_sia_a0f80d_zone_2' };
  fixture.registry = buildRegistryIndex({ areas: fixture.registry.areas, devices: [...fixture.registry.devices, legacyDevice], entities: [...fixture.registry.entities, legacyEntry] });
  fixture.states['switch.legacy'] = { state: 'on', attributes: { device_slug: 'sia_a0f80d_zone_2', metric: 'state' } };
  for (const scope of [{}, { sourceId: '' }]) {
    const data = dashboard(fixture, scope);
    const ajax = data.devices.filter((device) => device.id.includes('sia_') || device.id === 'unlinked');
    assert.deepEqual(new Set(ajax.map((device) => device.id)), new Set(['sia_a0f80d_zone_2', 'unlinked']));
    assert.deepEqual(ajax.flatMap((device) => device.actions ?? []).map((action) => action.entityId), ['switch.legacy']);
    assert.doesNotMatch(JSON.stringify(ajax), /switch\.(house|apartment)|sensor\.(house|apartment)/);
  }
});

test('source and area filtering preserve canonical ownership when HA has a stale cross-site registry link', () => {
  const fixture = multiSiteFixture();
  const moved = fixture.registry.entities.map((entry) => entry.entity_id.startsWith('sensor.apartment') || entry.entity_id.startsWith('switch.apartment') || entry.entity_id.startsWith('binary_sensor.apartment')
    ? { ...entry, device_id: 'house-switch' } : entry);
  fixture.registry = buildRegistryIndex({ areas: fixture.registry.areas, devices: fixture.registry.devices, entities: moved });
  const data = dashboard(fixture, { sourceId: 'apartment', areaIds: ['flat-room'] });
  const detector = data.devices.find((device) => device.id === 'apartment::sia_a0f80d_zone_2');
  assert.equal(detector.roomId, 'flat-room');
  assert.equal(detector.actions[0].entityId, 'switch.apartment');
  assert.equal(detector.metrics.find((metric) => metric.label === 'Temperature').value, '21.0 °C');
  assert.doesNotMatch(JSON.stringify(data), /house-room|switch\.house/);
});

test('the actual live hook retains the selected source through analytics and dashboard building', () => {
  const fixture = multiSiteFixture();
  globalThis.__hookRegistry = { areas: fixture.registry.areas, devices: fixture.registry.devices, entities: fixture.registry.entities };
  const hass = { states: fixture.states, callWS: async () => [] };
  const data = useDashboardData(hass, 'A0F80D', undefined, [], { sourceId: 'apartment', areaIds: ['flat-room'] });
  assert.ok(data.devices.some((device) => device.id === 'apartment::sia_a0f80d_zone_2'));
  assert.ok(data.devices.some((device) => device.id === 'dahua:apartment-camera'));
  assert.deepEqual(data.rooms.map((room) => room.id), ['flat-room']);
  assert.equal(data.systemState.chips.find((chip) => chip.id === 'system-mode').value, 'Disarmed');
  assert.doesNotMatch(JSON.stringify(data), /house|unlinked|foreign_same_room/);
  const legacy = useDashboardData(hass, 'A0F80D');
  assert.ok(legacy.devices.every((device) => !device.id.includes('::')));
  globalThis.__hookRegistry = null;
});

test('analytics discovery and endpoint overrides run only for scoped camera channels', async () => {
  const fixture = multiSiteFixture();
  const selected = scopeDashboardInputs(fixture.states, fixture.registry, { sourceId: 'apartment', areaIds: ['flat-room'] });
  const proxy = 'https://ha.example/apartment-dahua';
  const signature = dahuaBridgeChannelSignature(selected.states, selected.registryIndex, proxy);
  assert.equal(signature, `flat-room:${proxy}:nvr:1`);
  const calls = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url) => {
    calls.push(String(url));
    return { ok: true, json: async () => ({ channels: [{ channel: 1, total_count: 3, items: [{ code: 'human', count: 3 }] }] }) };
  };
  try {
    const counts = await loadSmdIvsCountsByRoom(selected.states, selected.registryIndex, proxy, new AbortController().signal);
    assert.equal(calls.length, 1);
    assert.ok(calls[0].startsWith(`${proxy}/api/v1/nvr/nvr/events/summary?`));
    assert.deepEqual(Object.keys(counts), ['flat-room']);
    assert.equal(counts['flat-room'].human, 3);
    const sourceOnly = scopeDashboardInputs(fixture.states, fixture.registry, { sourceId: 'apartment' });
    assert.equal(dahuaBridgeChannelSignature(sourceOnly.states, sourceOnly.registryIndex, proxy), '');
    assert.deepEqual(await loadSmdIvsCountsByRoom(sourceOnly.states, sourceOnly.registryIndex, proxy, new AbortController().signal), {});
    assert.equal(calls.length, 1);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('both registered card types forward YAML source and area configuration into the live adapter', () => {
  for (const name of ['ajaxbridge-detailed-card', 'ajaxbridge-chips-card']) {
    globalThis.__cardScopeCalls.length = 0;
    const Card = registeredCards.get(name);
    const card = new Card();
    card.setConfig({ type: `custom:${name}`, source_id: 'apartment', area_ids: ['flat-room'], account: 'A0F80D' });
    card.hass = { states: {} };
    card.connectedCallback();
    assert.deepEqual(globalThis.__cardScopeCalls.at(-1), {
      account: 'A0F80D', dahuaBase: undefined, gridEntities: undefined,
      scope: { sourceId: 'apartment', areaIds: ['flat-room'] },
    });
    card.disconnectedCallback();
  }
});

test('both cards reject malformed scope options instead of silently selecting all data', () => {
  for (const name of ['ajaxbridge-detailed-card', 'ajaxbridge-chips-card']) {
    const Card = registeredCards.get(name);
    assert.throws(() => new Card().setConfig({ type: `custom:${name}`, source_id: 12 }), /source_id must be a string/);
    assert.throws(() => new Card().setConfig({ type: `custom:${name}`, area_ids: 'flat-room' }), /area_ids must be a list/);
    assert.throws(() => new Card().setConfig({ type: `custom:${name}`, area_ids: ['flat-room', ''] }), /area_ids must be a list/);
  }
});
