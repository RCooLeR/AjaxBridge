import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { rolldown } from 'rolldown';

const root = resolve(import.meta.dirname, '..').replaceAll('\\', '/');
const directory = await mkdtemp(join(tmpdir(), 'ajaxbridge-cards-test-'));
after(() => rm(directory, { recursive: true, force: true }));
const bundle = await rolldown({
  input: 'test-harness', platform: 'node',
  plugins: [{ name: 'harness', resolveId(id) { if (id === 'test-harness') return '\0test-harness'; }, load(id) {
    if (id !== '\0test-harness') return;
    return `
      export { buildRegistryIndex, buildDashboardDataFromHomeAssistant } from '${root}/src/data/liveDashboardData.ts';
      export { callDeviceAction, callEntityService } from '${root}/src/ha/services.ts';
      import { createElement } from '${root}/node_modules/react/index.js';
      import { renderToStaticMarkup } from '${root}/node_modules/react-dom/server.node.js';
      import { RoomWorkspaceSidebar } from '${root}/src/cards/RoomWorkspaceSidebar.tsx';
      import { RoomHero } from '${root}/src/cards/RoomHero.tsx';
      import { DeviceGrid } from '${root}/src/cards/DeviceGrid.tsx';
      export function render(device, hass, events = []) {
        const room = { id:'room',name:'Room',summary:'Room',heroLabel:'Room',image:'house.png' };
        const summary = {smdIvs:{human:0,vehicle:0,animal:0,ivs:0},dahuaCameraCount:0,safety:{smokeHigh:0,coHigh:0}};
        return {
          sidebar:renderToStaticMarkup(createElement(RoomWorkspaceSidebar,{devices:[device],events,selectedDeviceId:null,onSelectDevice:()=>{},hass})),
          hero:renderToStaticMarkup(createElement(RoomHero,{room,roomSummary:summary,roomEvents:events,selectedDevice:device,streamProfile:'main',audioMuted:true,audioVolume:0,hass})),
          grid:renderToStaticMarkup(createElement(DeviceGrid,{devices:[device],events,selectedDeviceId:null,onSelectDevice:()=>{}})),
        };
      }`;
  } }],
});
const output = join(directory, 'harness.mjs');
await bundle.write({ file: output, format: 'esm' });
await bundle.close();
const { buildRegistryIndex, buildDashboardDataFromHomeAssistant, callDeviceAction, callEntityService, render } = await import(pathToFileURL(output).href);

function device(zone = '6', model = 'MotionProtect', name = 'Detector', extra = {}) {
  return { id: `ha-${zone}`, name, model, manufacturer: 'Ajax Systems', area_id: 'room', identifiers: [['mqtt', `ajaxbridge_A0F80D_zone_${zone}`]], ...extra };
}
function fixture(
  inputs,
  model = 'MotionProtect',
  name = 'Detector',
  devices = [device('6', model, name)],
  gridPowerAlarmEntities = [],
  roomSmdIvsCounts = {},
  dahuaAnalyticsConfigured = false,
) {
  const states = {};
  const entities = inputs.map(([entity_id, value, attributes = {}, metadata = {}]) => {
    states[entity_id] = { entity_id, state: value, attributes, last_changed: '2026-09-24T10:00:00Z' };
    return { entity_id, device_id: devices[0].id, original_name: attributes.friendly_name, ...metadata };
  });
  const registry = { areas: [{area_id: 'room', name: 'Room'}], devices, entities };
  const data = buildDashboardDataFromHomeAssistant(
    states,
    buildRegistryIndex(registry),
    undefined,
    roomSmdIvsCounts,
    gridPowerAlarmEntities,
    dahuaAnalyticsConfigured,
  );
  return { data, states, registry, card: data.devices[0] };
}
const chip = (data, id) => data.systemState.chips.find((entry) => entry.id === id);

test('WallSwitch voltage times current is apparent VA without exposing ambiguous plugin power', () => {
  for (const [current, unit] of [['0.96', 'A'], ['960', 'mA']]) {
    const {card} = fixture([
      ['sensor.voltage', '232', {device_class:'voltage',unit_of_measurement:'V'}],
      ['sensor.current', current, {device_class:'current',unit_of_measurement:unit}],
      ['sensor.power', '180', {device_class:'power',unit_of_measurement:'W',logical_id:'power'}],
    ], 'WallSwitch');
    assert.equal(card.metrics.find((metric) => metric.label === 'Apparent power').value, '223 VA');
    assert.equal(card.metrics.some((metric) => metric.label === 'Power'), false);
    const markup = render(card);
    assert.match(markup.sidebar, /Apparent power/);
    assert.match(markup.sidebar, /223 VA/);
  }
  const {card} = fixture([['sensor.power', '180', {device_class:'power',unit_of_measurement:'W',logical_id:'power'}]], 'Socket');
  assert.equal(card.metrics.find((metric) => metric.label === 'Power').value, '180 W');
});

test('derived power requires explicit units and never treats the legacy powerWtH counter as watts', () => {
  for (const [voltageUnit, currentUnit] of [['V', ''], ['', 'A'], ['V', 'unknown']]) {
    const {card} = fixture([
      ['sensor.voltage', '232', {device_class:'voltage',unit_of_measurement:voltageUnit}],
      ['sensor.current', '960', {device_class:'current',unit_of_measurement:currentUnit}],
      ['sensor.legacy_power', '78410', {device_class:'power',unit_of_measurement:'W',logical_id:'powerWtH'}],
    ], 'WallSwitch');
    assert.equal(card.metrics.some((metric) => metric.label === 'Apparent power'), false);
    assert.equal(card.metrics.some((metric) => metric.label === 'Power'), false);
  }
});

test('raw electrical diagnostics do not regain invented units from entity names', () => {
  for (const [current, power] of [['current_raw', 'power_raw'], ['courant', 'puissance']]) {
    const {card} = fixture([
      [`sensor.${current}`, '960', {metric:'current_raw'}],
      [`sensor.${power}`, '78410', {metric:'power_raw'}],
      ['sensor.voltage', '232', {device_class:'voltage',unit_of_measurement:'V'}],
    ], 'WallSwitch');
    assert.equal(card.metrics.find((metric) => metric.label === 'Current (raw)').value, '960.0');
    assert.equal(card.metrics.find((metric) => metric.label === 'Power (raw)').value, '78,410');
    assert.equal(card.metrics.some((metric) => metric.label === 'Apparent power'), false);
    assert.equal(card.metrics.some((metric) => / (A|W|VA)$/.test(metric.value)), false);
    const markup = render(card);
    assert.match(markup.sidebar, /Current \(raw\)/);
    assert.match(markup.sidebar, /Power \(raw\)/);
    assert.doesNotMatch(markup.sidebar, /Apparent power/);
  }
  const {card} = fixture([['sensor.current_profile', 'Outdoor']], 'WallSwitch');
  assert.equal((card.metrics ?? []).some((metric) => /Current|Power/.test(metric.label)), false);
});

test('verified cumulative energy keeps its historic power entity ID without becoming watts', () => {
  for (const model of ['Socket', 'WallSwitch']) {
    const {card} = fixture([
      ['sensor.zhivlennia_servera_power', '81.001', {device_class:'energy',unit_of_measurement:'kWh',logical_id:'powerWtH',friendly_name:'Power'}],
    ], model);
    assert.equal(card.metrics.find((metric) => metric.label === 'Energy').value, '81.0 kWh');
    assert.equal(card.metrics.some((metric) => /Power|power/.test(metric.label)), false);
    assert.match(render(card).sidebar, /81.0 kWh/);
  }
  const {card} = fixture([['sensor.energy_kwh', '81.001', {logical_id:'powerWtH'}]], 'Socket');
  assert.equal((card.metrics ?? []).some((metric) => metric.label === 'Energy'), false);
});

test('unverified energy stays raw and every supported energy unit remains visible', () => {
  for (const unit of ['', 'widgets', 'kWh']) {
    const {card} = fixture([['sensor.energy_raw', '123', {metric:'energy_raw',unit_of_measurement:unit,friendly_name:'Energy'}]], 'Socket');
    assert.equal(card.metrics.some((metric) => metric.label === 'Energy'), false);
    assert.ok(card.metrics.find((metric) => metric.label === 'Energy (raw)'));
    assert.match(render(card).sidebar, /Energy \(raw\)/);
    if (!unit) assert.equal(card.metrics.find((metric) => metric.label === 'Energy (raw)').value, '123.0');
  }
  for (const unit of ['mWh','Wh','kWh','MWh','GWh','TWh','J','kJ','MJ','GJ','cal','kcal','Mcal','Gcal']) {
    const {card} = fixture([['sensor.legacy_power', '123', {device_class:'energy',unit_of_measurement:unit,logical_id:'powerWtH'}]], 'Socket');
    assert.equal(card.metrics.find((metric) => metric.label === 'Energy').value, `123.0 ${unit}`);
    assert.equal(card.metrics.some((metric) => metric.label === 'Power'), false);
  }
});

test('stable unique IDs and arbitrary HA suffixes preserve alarm, tamper and panic precedence', () => {
  for (const suffix of ['', '_2', '_37', '_9001']) {
    for (const semantic of ['alarm_active', 'tamper_active', 'signal_panic']) {
      const {card, data} = fixture([[`binary_sensor.detector_${semantic}${suffix}`, 'on'], ['sensor.detector_battery', '100', {device_class:'battery'}]]);
      assert.equal(card.tone, 'red', `${semantic}${suffix}`);
      assert.equal(card.attention, true);
      assert.equal(chip(data, 'system-mode').value, 'Alarm');
    }
  }
  const {card} = fixture([['binary_sensor.completely_renamed_27','on',{}, {unique_id:'ajaxbridge_zone_a0f80d_6_alarm_active'}]]);
  assert.equal(card.tone, 'red');
});

test('translated legacy diagnostics and physical panic names retain semantics', () => {
  const {card} = fixture([['binary_sensor.diana_panique_41', 'on'], ['sensor.nombre_de_defauts_3','0'],['sensor.batterie_72','100']], 'SpaceControl');
  assert.equal(card.tone, 'red');
  assert.equal(card.actions, undefined);
  assert.equal(card.metrics.find((item) => item.label === 'Issues').value, '0');
});

test('canonical ownership splits same-name devices and merges SIA/Jeedom despite stale registry links', () => {
  const devices = ['2','20','21','501','502'].map((zone) => device(zone, Number(zone) > 500 ? 'App' : 'SpaceControl', 'Діана'));
  devices.push(device('legacy', 'SpaceControl', 'Діана', { identifiers: [['mqtt','ajaxbridge_jeedom_diana']] }));
  const inputs = devices.slice(0, 5).map((item, index) => [`binary_sensor.arbitrary_${index}`, 'off', {}, { device_id:item.id, unique_id:`ajaxbridge_zone_a0f80d_${['2','20','21','501','502'][index]}_alarm_active` }]);
  inputs.push(['sensor.firmware_wrong_registry', '5.54', { device_slug:'sia_a0f80d_zone_2', friendly_name:'Version du firmware' }, { device_id:'ha-20', unique_id:'ajaxbridge_jeedom_cmd_370' }]);
  inputs.push(['sensor.legacy_battery', '85', { device_slug:'sia_a0f80d_zone_20', device_class:'battery' }, { device_id:'ha-legacy', unique_id:'ajaxbridge_jeedom_cmd_374' }]);
  const {data} = fixture(inputs, '', '', devices);
  assert.equal(data.devices.length, 5);
  assert.equal(new Set(data.devices.map((item) => item.id)).size, 5);
  assert.equal(data.devices.find((item) => item.id === 'sia_a0f80d_zone_2').metrics.find((item) => item.label === 'Firmware').value, '5.54');
  assert.equal(data.devices.find((item) => item.id === 'sia_a0f80d_zone_20').battery, '85 %');
  assert.equal(data.devices.filter((item) => item.type === 'app').length, 2);
});

test('low numeric battery, positive issues and failed battery check share card/global warning policy without security alarm', () => {
  for (const [entity, value, attrs] of [['sensor.battery', '5', {device_class:'battery'}], ['sensor.nombre_de_defauts_52','3',{}], ['sensor.battery_check_status','FAILED',{}]]) {
    const {card, data} = fixture([[entity,value,attrs]]);
    assert.equal(card.attention, true, entity);
    assert.equal(card.tone, 'amber');
    assert.equal(chip(data,'system-alerts').value,'1');
    assert.equal(chip(data,'system-mode'),undefined);
    assert.ok(card.metrics.some((metric) => metric.tone === 'amber'));
  }
  for (const value of ['21', '90', '100']) assert.equal(fixture([['sensor.battery',value,{device_class:'battery'}]]).card.attention, false);
});

test('connectivity faults, explicit off, explicit unknown and unavailable cannot look healthy', () => {
  for (const input of [
    ['binary_sensor.detector_online','off',{device_class:'connectivity'}],
    ['binary_sensor.detector_online','unknown',{device_class:'connectivity'}],
    ['binary_sensor.detector_signal_connectivity_81','on'],
    ['binary_sensor.detector_alarm_active','unavailable'],
  ]) {
    const {card, data} = fixture([input]);
    assert.equal(card.isOnline,false,input[0]);
    assert.equal(card.attention,true);
    assert.notEqual(card.status,'Nominal');
    assert.notEqual(chip(data,'system-mode')?.value,'Monitoring');
  }
  const {card} = fixture([['binary_sensor.detector_signal_connectivity_42','off']]);
  assert.equal(card.isOnline,true);
  assert.equal(card.metrics.find((metric) => metric.label === 'Link').value,'Online');
});

test('Hub overall online state is authoritative over optional transport channels', () => {
  const hub = {
    id:'ha-account',name:'Ajax Hub 2 Plus',model:'Ajax account',manufacturer:'Ajax Systems',area_id:'room',
    identifiers:[['mqtt','ajaxbridge_account_a0f80d']],
  };
  const inputs = [
    ['binary_sensor.ajax_account_a0f80d_online','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Online'}, {unique_id:'ajaxbridge_account_a0f80d_online'}],
    ['binary_sensor.ajax_account_a0f80d_gsm','unknown',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'GSM'}, {unique_id:'ajaxbridge_jeedom_cmd_154'}],
    ['sensor.ajax_account_a0f80d_signal','unknown',{device_class:'signal_strength',device_slug:'account_a0f80d',friendly_name:'Signal',unit_of_measurement:'dBm'}, {unique_id:'ajaxbridge_jeedom_cmd_155'}],
    ['binary_sensor.ajax_account_a0f80d_cellular_data_active','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Cellular data active'}, {unique_id:'ajaxbridge_jeedom_cmd_157'}],
    ['binary_sensor.ajax_account_a0f80d_cms','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'CMS'}, {unique_id:'ajaxbridge_jeedom_cmd_158'}],
    ['binary_sensor.ajax_account_a0f80d_ethernet','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Ethernet Enabled'}, {unique_id:'ajaxbridge_jeedom_cmd_159'}],
    ['button.ajax_account_a0f80d_disarm','unknown',{friendly_name:'Disarm'}, {unique_id:'ajaxbridge_account_a0f80d_disarm'}],
  ];
  const {card} = fixture(inputs,'','',[hub]);
  assert.equal(card.name,'Ajax Hub 2 Plus');
  assert.equal(card.connectivity,'Online');
  assert.equal(card.isOnline,true);
  assert.equal(card.attention,false);
  assert.equal(card.metrics.find((metric) => metric.label === 'Link').value,'Online');
  assert.equal(card.metrics.find((metric) => metric.label === 'Link').tone,'green');

  inputs[0][1] = 'off';
  const offline = fixture(inputs,'','',[hub]).card;
  assert.equal(offline.connectivity,'Offline');
  assert.equal(offline.isOnline,false);
  assert.equal(offline.attention,true);
  assert.equal(offline.metrics.find((metric) => metric.label === 'Link').value,'Offline');

  inputs[0][1] = 'on';
  inputs.push(['binary_sensor.ajax_account_a0f80d_signal_connectivity','on',{}, {unique_id:'ajaxbridge_account_a0f80d_signal_connectivity'}]);
  const faulted = fixture(inputs,'','',[hub]).card;
  assert.equal(faulted.connectivity,'Offline');
  assert.equal(faulted.isOnline,false);
  assert.equal(faulted.attention,true);
  assert.equal(faulted.metrics.find((metric) => metric.label === 'Link').value,'Offline');

  inputs[0][1] = 'unknown';
  inputs.pop();
  inputs[3][1] = 'on';
  const unknown = fixture(inputs,'','',[hub]).card;
  assert.equal(unknown.connectivity,'Unknown');
  assert.equal(unknown.isOnline,false);
  assert.equal(unknown.metrics.find((metric) => metric.label === 'Link').value,'Unknown');
});

test('Hub card keeps active uplinks separate and treats Jeweller as a radio diagnostic', () => {
  const hub = {
    id:'ha-account',name:'Ajax Hub 2 Plus',model:'Ajax account',manufacturer:'Ajax Systems',area_id:'room',
    identifiers:[['mqtt','ajaxbridge_account_a0f80d']],
  };
  const inputs = [
    ['binary_sensor.ajax_account_a0f80d_online','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Online'}, {unique_id:'ajaxbridge_account_a0f80d_online'}],
    ['binary_sensor.ajax_account_a0f80d_ethernet_active','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Ethernet active'}, {unique_id:'ajaxbridge_jeedom_cmd_201'}],
    ['binary_sensor.ajax_account_a0f80d_wifi_active','off',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Wi-Fi active'}, {unique_id:'ajaxbridge_jeedom_cmd_202'}],
    ['binary_sensor.ajax_account_a0f80d_gsm_active','unknown',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'GSM active'}, {unique_id:'ajaxbridge_jeedom_cmd_203'}],
    ['sensor.ajax_account_a0f80d_jeweller_antenna_status','ANTENNA_DISCONNECTED',{device_slug:'account_a0f80d',friendly_name:'Jeweller antenna status'}, {unique_id:'ajaxbridge_jeedom_cmd_204'}],
    ['sensor.ajax_account_a0f80d_wifi_signal_level','STRONG',{device_slug:'account_a0f80d',friendly_name:'Wi-Fi signal'}, {unique_id:'ajaxbridge_jeedom_cmd_207'}],
    ['sensor.ajax_account_a0f80d_gsm_signal_level','NORMAL',{device_slug:'account_a0f80d',friendly_name:'GSM signal'}, {unique_id:'ajaxbridge_jeedom_cmd_208'}],
    ['sensor.ajax_account_a0f80d_wings_noise','-87',{device_slug:'account_a0f80d',friendly_name:'Wings noise'}, {unique_id:'ajaxbridge_jeedom_cmd_209'}],
    ['binary_sensor.ajax_account_a0f80d_ethernet_enabled','on',{device_slug:'account_a0f80d',friendly_name:'Ethernet enabled'}, {unique_id:'ajaxbridge_jeedom_cmd_205'}],
    ['binary_sensor.ajax_account_a0f80d_cellular_data_enabled','on',{device_slug:'account_a0f80d',friendly_name:'Cellular data enabled'}, {unique_id:'ajaxbridge_jeedom_cmd_206'}],
    ['button.ajax_account_a0f80d_disarm','unknown',{device_slug:'account_a0f80d',friendly_name:'Disarm'}, {unique_id:'ajaxbridge_account_a0f80d_disarm'}],
  ];

  const {card} = fixture(inputs,'','',[hub]);
  const metric = (label) => card.metrics.find((item) => item.label === label);
  assert.deepEqual(
    ['Link','Ethernet','GSM','Wi-Fi','Jeweller antenna'].map((label) => [label,metric(label)?.value,metric(label)?.tone]),
    [
      ['Link','Online','green'],
      ['Ethernet','Active','green'],
      ['GSM','Unknown','slate'],
      ['Wi-Fi','Inactive','slate'],
      ['Jeweller antenna','Disconnected','amber'],
    ],
  );
  assert.deepEqual(
    ['Wi-Fi signal','GSM signal','Wings noise'].map((label) => [label,metric(label)?.value,metric(label)?.tone]),
    [
      ['Wi-Fi signal','Strong','green'],
      ['GSM signal','Normal','cyan'],
      ['Wings noise','-87','cyan'],
    ],
  );
  assert.equal(card.metrics.some((item) => item.label === 'Signal'),false);
  assert.equal(card.metrics.filter((item) => item.label === 'Link').length,1);
  assert.equal(card.connectivity,'Online');
  assert.equal(card.isOnline,true);
  assert.equal(card.attention,false);

  const markup = render(card).grid;
  for (const label of ['Link','Ethernet','GSM','Wi-Fi','Jeweller antenna']) assert.match(markup,new RegExp(`>${label}<`));
  assert.match(markup,/>More</);

  inputs[0][1] = 'off';
  const offline = fixture(inputs,'','',[hub]).card;
  assert.equal(offline.connectivity,'Offline');
  assert.equal(offline.isOnline,false);
  assert.equal(offline.metrics.find((item) => item.label === 'Link').value,'Offline');
  assert.equal(offline.metrics.find((item) => item.label === 'Ethernet').value,'Last known active');
  assert.equal(offline.metrics.find((item) => item.label === 'Ethernet').tone,'slate');
  assert.equal(offline.metrics.find((item) => item.label === 'Wi-Fi').value,'Last known inactive');

  inputs[0][1] = 'unknown';
  const unknown = fixture(inputs,'','',[hub]).card;
  assert.equal(unknown.connectivity,'Unknown');
  assert.equal(unknown.metrics.find((item) => item.label === 'Ethernet').value,'Last known active');
  assert.equal(unknown.metrics.find((item) => item.label === 'Ethernet').tone,'slate');
});

test('Hub card never invents missing transport or Jeweller metrics', () => {
  const hub = {
    id:'ha-account',name:'Ajax Hub',model:'Ajax account',manufacturer:'Ajax Systems',area_id:'room',
    identifiers:[['mqtt','ajaxbridge_account_a0f80d']],
  };
  const {card} = fixture([
    ['binary_sensor.ajax_account_a0f80d_online','on',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Online'}, {unique_id:'ajaxbridge_account_a0f80d_online'}],
    ['binary_sensor.ajax_account_a0f80d_ethernet_active','off',{device_class:'connectivity',device_slug:'account_a0f80d',friendly_name:'Ethernet active'}, {unique_id:'ajaxbridge_jeedom_cmd_211'}],
    ['button.ajax_account_a0f80d_disarm','unknown',{device_slug:'account_a0f80d',friendly_name:'Disarm'}, {unique_id:'ajaxbridge_account_a0f80d_disarm'}],
  ],'','',[hub]);
  const labels = card.metrics.map((item) => item.label);
  assert.deepEqual(labels,['Link','Ethernet']);
  assert.equal(card.metrics.find((item) => item.label === 'Ethernet').value,'Inactive');
  for (const absent of ['Wi-Fi','GSM','Jeweller link','Jeweller antenna','Jeweller interference']) {
    assert.equal(labels.includes(absent),false,absent);
  }
});

test('redundant connectivity channels stay online when any known channel is connected', () => {
  const authoritative = fixture([
    ['binary_sensor.detector_online','on',{device_class:'connectivity'}],
    ['binary_sensor.detector_ethernet_connected','off',{device_class:'connectivity'}],
    ['binary_sensor.detector_radio_connection','unknown',{device_class:'connectivity'}],
  ]).card;
  assert.equal(authoritative.connectivity,'Online');
  assert.equal(authoritative.isOnline,true);
  assert.equal(authoritative.metrics.find((metric) => metric.label === 'Link').value,'Online');

  const connected = fixture([
    ['binary_sensor.detector_ethernet_connected','on',{device_class:'connectivity'}],
    ['binary_sensor.detector_radio_connection','unknown',{device_class:'connectivity'}],
  ]).card;
  assert.equal(connected.connectivity,'Online');
  assert.equal(connected.isOnline,true);
  assert.equal(connected.metrics.find((metric) => metric.label === 'Link').value,'Online');

  const uncertain = fixture([
    ['binary_sensor.detector_ethernet_connected','off',{device_class:'connectivity'}],
    ['binary_sensor.detector_radio_connection','unknown',{device_class:'connectivity'}],
  ]).card;
  assert.equal(uncertain.connectivity,'Unknown');
  assert.equal(uncertain.isOnline,false);
  assert.equal(uncertain.metrics.find((metric) => metric.label === 'Link').value,'Unknown');

  const disconnected = fixture([
    ['binary_sensor.detector_ethernet_connected','off',{device_class:'connectivity'}],
    ['binary_sensor.detector_radio_connection','off',{device_class:'connectivity'}],
  ]).card;
  assert.equal(disconnected.connectivity,'Offline');
  assert.equal(disconnected.isOnline,false);
  assert.equal(disconnected.metrics.find((metric) => metric.label === 'Link').value,'Offline');

  const unknown = fixture([
    ['binary_sensor.detector_ethernet_connected','unknown',{device_class:'connectivity'}],
    ['binary_sensor.detector_radio_connection','unavailable',{device_class:'connectivity'}],
  ]).card;
  assert.equal(unknown.connectivity,'Unknown');
  assert.equal(unknown.isOnline,false);
  assert.equal(unknown.metrics.find((metric) => metric.label === 'Link').value,'Unknown');
});

test('operational on/open/online and missing historical events are not faults', () => {
  for (const [model, inputs] of [
    ['WallSwitch',[['switch.pump','on'],['sensor.last_event_time','unknown']]],
    ['WaterStop',[['switch.valve','off'],['sensor.valve_position','OPEN']]],
    ['DoorProtect',[['binary_sensor.door','on',{device_class:'door'}],['binary_sensor.online','on',{device_class:'connectivity'}]]],
  ]) {
    const {card,data} = fixture(inputs,model);
    assert.equal(card.attention,false,model);
    assert.equal(chip(data,'system-alerts').value,'0');
  }
});

test('WallSwitch lighting role stays in full product power inventory; foreign names are not Ajax evidence', () => {
  const devices = Array.from({length:7}, (_,i) => device(String(i+1),'WallSwitch',i === 0 ? 'Garage light' : `Pump ${i}`));
  devices.push(device('8','Socket','Socket'));
  devices.push(device('9','SpaceControl','Ajax keyfob power',{manufacturer:'Jinko',identifiers:[['mqtt','jinko_power']]}));
  const inputs = devices.map((item,i) => [`switch.item_${i}`,'on',{}, {device_id:item.id}]);
  const {data} = fixture(inputs,'','',devices);
  assert.equal(data.devices.length,8);
  assert.equal(chip(data,'system-outlets').value,'8/8');
  assert.equal(chip(data,'system-outlets').details.items.length,8);
  assert.ok(chip(data,'system-outlets').details.summary.includes('first number'));
  assert.ok(chip(data,'system-outlets').details.items.some((item)=>item.label==='Garage light'&&item.value==='On'));
  assert.ok(chip(data,'system-lights').details.items.some((item)=>item.label==='Garage light'&&item.value==='On'));
  assert.equal(data.devices.find((item) => item.name === 'Garage light').type,'wall_switch');
});

test('analytics chip details separate event types from scrollable zone groups', () => {
  const {data}=fixture([['sensor.detector_battery','100',{device_class:'battery'}]], 'MotionProtect', 'Detector', undefined, [], {
    room:{total:14,human:7,vehicle:3,animal:2,ivs:2},
  });
  const smd=chip(data,'system-smd');
  assert.equal(smd.value,'12');
  assert.deepEqual(
    smd.details.sections.map((section)=>[section.title,section.scrollable??false]),
    [['Event types',false],['Zones',true]],
  );
  assert.deepEqual(
    smd.details.sections[0].items.map((item)=>[item.label,item.value]),
    [['People','7'],['Vehicles','3'],['Animals','2']],
  );
  assert.deepEqual(smd.details.sections[1].items.map((item)=>[item.label,item.value]),[['Room','12']]);
  assert.equal(smd.details.items.some((item)=>item.label.startsWith('Room:')),false);
  const ivs=chip(data,'system-ivs');
  assert.equal(ivs.value,'2');
  assert.ok(ivs.details.summary.includes('Tripwire'));
  assert.deepEqual(
    ivs.details.sections.map((section)=>[section.title,section.scrollable??false]),
    [['Event types',false],['Zones',true]],
  );
  assert.deepEqual(ivs.details.sections[0].items.map((item)=>[item.label,item.value]),[['Tripwire / intrusion events','2']]);
  assert.deepEqual(ivs.details.sections[1].items.map((item)=>[item.label,item.value]),[['Room','2']]);
});

test('summary chips render only when their backing source is configured or discovered', () => {
  const empty = fixture([], 'MotionProtect', 'Detector').data;
  assert.equal(chip(empty,'system-mode'),undefined);
  assert.equal(chip(empty,'system-smd'),undefined);
  assert.equal(chip(empty,'system-ivs'),undefined);
  assert.equal(chip(empty,'system-outlets'),undefined);
  assert.equal(chip(empty,'system-lights'),undefined);
  assert.equal(chip(empty,'system-alerts'),undefined);

  const motion = fixture([
    ['sensor.detector_battery','100',{device_class:'battery'}],
  ],'MotionProtect').data;
  assert.equal(chip(motion,'system-mode'),undefined);
  assert.equal(chip(motion,'system-smd'),undefined);
  assert.equal(chip(motion,'system-ivs'),undefined);
  assert.equal(chip(motion,'system-outlets'),undefined);
  assert.equal(chip(motion,'system-lights'),undefined);
  assert.equal(chip(motion,'system-alerts').value,'0');

  const wallSwitch = fixture([['switch.pump','off']],'WallSwitch','Pump').data;
  assert.equal(chip(wallSwitch,'system-outlets').value,'0/1');
  assert.equal(chip(wallSwitch,'system-lights'),undefined);

  const dahuaCamera = {
    id:'dahua-camera',name:'Front camera',model:'IPC',manufacturer:'Dahua',area_id:'room',identifiers:[['dahua','channel_1']],
  };
  const cameraOnly = fixture([['camera.front','streaming']], '', '', [dahuaCamera]).data;
  assert.equal(chip(cameraOnly,'system-mode'),undefined);
  assert.equal(chip(cameraOnly,'system-smd'),undefined);
  assert.equal(chip(cameraOnly,'system-ivs'),undefined);
  assert.equal(chip(cameraOnly,'system-alerts').value,'0');

  const analytics = fixture([['camera.front','streaming']], '', '', [dahuaCamera], [], {}, true).data;
  assert.equal(chip(analytics,'system-smd').value,'0');
  assert.equal(chip(analytics,'system-ivs').value,'0');
  assert.deepEqual(chip(analytics,'system-smd').details.sections.map((section)=>section.title),['Event types']);
  assert.deepEqual(chip(analytics,'system-ivs').details.sections.map((section)=>section.title),['Event types']);
});

test('security mode requires a live account state and represents unavailable values as unknown', () => {
  const modeEntity = {
    entity_id:'select.ajax_mode',device_id:'ha-account',unique_id:'ajaxbridge_account_a0f80d_mode',original_name:'Mode',
  };
  const registry = buildRegistryIndex({
    areas:[{area_id:'room',name:'Room'}],
    devices:[{
      id:'ha-account',name:'Ajax account',model:'Hub',manufacturer:'Ajax Systems',area_id:'room',
      identifiers:[['mqtt','ajaxbridge_account_a0f80d']],
    }],
    entities:[modeEntity],
  });
  const missing = buildDashboardDataFromHomeAssistant({}, registry);
  assert.equal(chip(missing,'system-mode'),undefined);

  for (const state of ['unknown','unavailable']) {
    const data = buildDashboardDataFromHomeAssistant({
      [modeEntity.entity_id]:{entity_id:modeEntity.entity_id,state,attributes:{}},
    },registry);
    const security = chip(data,'system-mode');
    assert.equal(security.value,'Unknown');
    assert.equal(security.tone,'slate');
    assert.equal(security.active,false);
    assert.deepEqual(
      security.details.items.map((item)=>[item.label,item.value,item.tone]),
      [['Mode','Unknown','slate']],
    );
  }
});

test('unknown WallSwitch and intermediate/moving/unknown WaterStop disable both UI surfaces and dispatch', async () => {
  for (const [model,inputs] of [
    ['WallSwitch',[['switch.pump','unknown']]],
    ...['INTERMEDIATE','OPENING','CLOSING','MOVING','unknown','unavailable'].map((value) => ['WaterStop',[['switch.valve','on'],['sensor.valve_position',value]]]),
  ]) {
    const {card,states,data} = fixture(inputs,model);
    const action = card.actions[0];
    assert.equal(action.disabled,true,`${model} ${inputs.at(-1)[1]}`);
    assert.equal(action.service,'');
    assert.equal(card.attention,true);
    if (model === 'WallSwitch') {
      assert.ok(chip(data,'system-outlets').details.items.some((item)=>item.label==='Detector'&&item.value==='Unknown'));
    }
    assert.equal(chip(fixture(inputs,model).data,'system-mode'),undefined);
    let calls=0;
    const hass={states,callService:async()=>{calls++;}};
    await assert.rejects(callDeviceAction(hass,card,action),/known device state|Control unavailable/);
    const markup=render(card,hass);
    assert.match(markup.sidebar, /class="ajax-device-item__toggle[^>]+disabled=""/);
    assert.match(markup.hero, /class="room-hero__action-button"[^>]+disabled=""/);
    assert.equal(calls,0);
  }
});

test('observed WallSwitch state and WaterStop position win over optimistic switch state and are rechecked at dispatch', async () => {
  for (const [model,inputs,expected] of [
    ['WallSwitch',[['switch.pump','on'],['binary_sensor.pump_state_123','off',{friendly_name:'State'}]],'turn_on'],
    ['WaterStop',[['switch.valve','off'],['sensor.valve_position','OPEN']],'turn_off'],
  ]) {
    const {card,states}=fixture(inputs,model); const action=card.actions[0]; const calls=[];
    assert.equal(action.service,expected);
    const hass={states,callService:async(...args)=>{calls.push(args);}};
    assert.equal(await callDeviceAction(hass,card,action),true);
    assert.equal(calls.length,1);
    states[action.valvePositionEntityId ?? action.observedStateEntityId].state='unknown';
    await assert.rejects(callDeviceAction(hass,card,action),/state changed/);
    assert.equal(calls.length,1);
  }
});

test('physical Button/SpaceControl are read-only and actual Hub/App controls require confirmation', async () => {
  for (const model of ['Button','DoubleButton','SpaceControl']) {
    const {card,states}=fixture([['button.panic','unknown',{friendly_name:'Panic'}]],model);
    assert.equal(card.actions,undefined);
    await assert.rejects(callDeviceAction({states,callService:async()=>assert.fail('physical service')},card,{id:'forged',entityId:'button.panic',domain:'button',service:'press'}),/read-only/);
  }
  for (const model of ['Hub','App']) {
    const {card,states}=fixture([['button.disarm','unknown',{friendly_name:'Disarm'}]],model); const action=card.actions[0]; let calls=0;
    const hass={states,callService:async()=>{calls++;}};
    assert.ok(action.confirmation);
    assert.equal(await callDeviceAction(hass,card,action),false);
    assert.equal(await callDeviceAction(hass,card,action,()=>false),false);
    assert.equal(calls,0);
    assert.equal(await callDeviceAction(hass,card,action,()=>true),true);
    assert.equal(calls,1);
  }
});

test('ambiguous transport failure is surfaced once and never retried', async () => {
  let calls=0;
  const hass={states:{},callService:async()=>{calls++;throw new Error('timeout after dispatch');}};
  await assert.rejects(callEntityService(hass,'button','press','button.relay_impulse'),/timeout/);
  assert.equal(calls,1);
  await assert.rejects(callEntityService({states:{}},'button','press','button.test'),/API unavailable/);
});

test('restored historical smoke does not inflate current protection counts or override card health', () => {
  const {card,data,states}=fixture([
    ['binary_sensor.fire_signal_smoke_50','off'],['binary_sensor.fire_alarm_active_80','off'],
    ['sensor.last_signal','smoke'],['sensor.last_event_name','Smoke restored'],['sensor.last_event_time','2026-09-24T10:00:00Z'],
  ],'FireProtect');
  assert.equal(card.attention,false);
  assert.equal(data.rooms[0].safety.smokeHigh,0);
  const markup=render(card,{states},[{deviceId:card.id,type:'fire_detected',title:'Old fire',tone:'red'}]);
  assert.doesNotMatch(markup.grid,/Old fire/);
  assert.match(markup.grid,/Nominal/);
});

test('SIA power-failure is an operational fault, while clear is nominal and temperature alarm is security', () => {
  for (const value of ['off','on']) {
    const {card,data}=fixture([['binary_sensor.power_failure_50',value,{}, {unique_id:'ajaxbridge_zone_a0f80d_6_signal_power_failure'}]]);
    assert.equal(card.attention,value==='on');
    assert.equal(chip(data,'system-mode'),undefined);
    if (value==='off') assert.ok(!card.metrics?.some((metric)=>metric.tone==='amber'));
  }
  const {card,data}=fixture([['binary_sensor.temperature_alarm_55','on',{}, {unique_id:'ajaxbridge_zone_a0f80d_6_signal_temperature_alarm'}]],'FireProtect');
  assert.equal(card.tone,'red');
  assert.equal(data.rooms[0].safety.smokeHigh,1);
});

test('HA unique-id-mapped Transmitter input alarm survives an entity-id rename', () => {
  const {card,data}=fixture([
    ['binary_sensor.ob_iekt_grid_input_alarm','on',{device_class:'safety',friendly_name:'Input alarm',metric:'input_alarm'},{unique_id:'ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm'}],
    ['sensor.last_event_name','Input alarm',{}, {unique_id:'ajaxbridge_zone_a0f80d_6_last_event_name'}],
    ['sensor.last_event_at','2026-09-24T10:00:00Z',{}, {unique_id:'ajaxbridge_zone_a0f80d_6_last_event_at'}],
  ],'Transmitter','Grid power detector',undefined,['ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm']);
  assert.equal(card.status,'Grid power off');
  assert.equal(card.tone,'amber');
  assert.equal(card.attention,false);
  assert.ok(card.metrics?.some((metric)=>metric.label==='Grid power'&&metric.value==='Off'));
  assert.ok(!card.metrics?.some((metric)=>metric.label==='Safety'||metric.label==='Alarm signal'));
  assert.equal(chip(data,'system-mode'),undefined);
  assert.equal(chip(data,'system-alerts').value,'0');
  assert.equal(chip(data,'system-grid-power').value,'1 outage');
  assert.equal(chip(data,'system-grid-power').tone,'amber');
  assert.match(chip(data,'system-grid-power').details.summary,/informational/i);
  assert.match(chip(data,'system-grid-power').details.summary,/not a security alarm/i);
  assert.deepEqual(
    chip(data,'system-grid-power').details.items.map((item)=>[item.label,item.value,item.tone]),
    [['Grid power detector','Outage','amber']],
  );
  assert.deepEqual(data.rooms[0].gridPower,{known:1,online:0,outage:1});
  assert.equal(data.events[0].type,'power_loss');
  assert.equal(data.events[0].tone,'amber');
  assert.doesNotMatch(data.events[0].description,/normal state/i);
});

test('unmapped Transmitter input alarm remains a critical safety alert', () => {
  const {card,data}=fixture([
    ['binary_sensor.input_alarm','on',{device_class:'safety',friendly_name:'Input alarm',metric:'input_alarm'},{unique_id:'ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm'}],
  ],'Transmitter','Gate contact');
  assert.notEqual(card.status,'Grid power off');
  assert.equal(card.attention,true);
  assert.equal(card.tone,'red');
  assert.equal(chip(data,'system-mode').value,'Alarm');
  assert.ok(chip(data,'system-mode').details.items.some((item)=>item.label==='Gate contact'&&item.value===card.status));
  assert.ok(chip(data,'system-alerts').details.items.some((item)=>item.label==='Gate contact'&&item.tone==='red'));
  assert.equal(chip(data,'system-grid-power'),undefined);
  assert.equal(data.rooms[0].gridPower,undefined);
});

test('missing or blank grid mapping omits the informational chip', () => {
  for (const selectors of [[], ['', '  ']]) {
    const {data}=fixture([], 'MotionProtect', 'Detector', undefined, selectors);
    assert.equal(chip(data,'system-grid-power'),undefined);
  }
});

test('stale grid input selector is reported as unmatched and does not suppress alarms', () => {
  const {card,data}=fixture([
    ['binary_sensor.input_alarm','on',{device_class:'safety',friendly_name:'Input alarm',metric:'input_alarm'},{unique_id:'ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm'}],
  ],'Transmitter','Gate contact',undefined,['ajaxbridge_jeedom_sia_a0f80d_zone_999_input_alarm']);
  assert.equal(card.attention,true);
  assert.equal(chip(data,'system-grid-power').value,'Unknown');
  assert.deepEqual(
    chip(data,'system-grid-power').details.items.map((item)=>[item.label,item.value]),
    [['Configured grid inputs','No matching HA entity']],
  );
});

test('HA-mapped Transmitter clear state reports restored mains as one known source', () => {
  const {card,data}=fixture([
    ['binary_sensor.grid_input_alarm','off',{device_class:'safety',friendly_name:'Input alarm',metric:'input_alarm'},{unique_id:'ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm'}],
  ],'Transmitter','Grid power detector',undefined,['binary_sensor.grid_input_alarm']);
  assert.equal(card.attention,false);
  assert.ok(card.metrics?.some((metric)=>metric.label==='Grid power'&&metric.value==='Mains'));
  assert.ok(!card.metrics?.some((metric)=>metric.label==='Safety'));
  assert.equal(chip(data,'system-grid-power').value,'1/1 OK');
  assert.deepEqual(data.rooms[0].gridPower,{known:1,online:1,outage:0});
});

test('HA-mapped unknown and unavailable input states remain explicitly unknown', () => {
  for (const state of ['unknown','unavailable']) {
    const {card,data}=fixture([
      ['binary_sensor.grid_input_alarm',state,{device_class:'safety',friendly_name:'Input alarm',metric:'input_alarm'},{unique_id:'ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm'}],
    ],'Transmitter','Grid power detector',undefined,['binary_sensor.grid_input_alarm']);
    assert.equal(card.status,'Grid power unknown');
    assert.equal(card.attention,false);
    assert.ok(card.metrics?.some((metric)=>metric.label==='Grid power'&&metric.value==='Unknown'&&metric.tone==='slate'));
    assert.ok(!card.metrics?.some((metric)=>metric.label==='Safety'));
    assert.equal(chip(data,'system-grid-power').value,'Unknown');
    assert.equal(chip(data,'system-grid-power').tone,'slate');
    assert.equal(data.rooms[0].gridPower,undefined);
  }
});

test('grid mapping suppresses exactly its configured entity and leaves other alarms critical', () => {
  const {card,data}=fixture([
    ['binary_sensor.grid_input_alarm','on',{device_class:'safety',friendly_name:'Input alarm',metric:'input_alarm'},{unique_id:'ajaxbridge_jeedom_sia_a0f80d_zone_6_input_alarm'}],
    ['binary_sensor.other_alarm','on',{device_class:'safety',friendly_name:'Other alarm',metric:'alarm_active'},{unique_id:'ajaxbridge_zone_a0f80d_6_alarm_active'}],
  ],'Transmitter','Grid power detector',undefined,['binary_sensor.grid_input_alarm']);
  assert.equal(card.attention,true);
  assert.equal(card.tone,'red');
  assert.equal(chip(data,'system-mode').value,'Alarm');
  assert.equal(chip(data,'system-grid-power').value,'1 outage');
  assert.ok(card.metrics?.some((metric)=>metric.label==='Safety'));
});
