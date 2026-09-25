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
function fixture(inputs, model = 'MotionProtect', name = 'Detector', devices = [device('6', model, name)]) {
  const states = {};
  const entities = inputs.map(([entity_id, value, attributes = {}, metadata = {}]) => {
    states[entity_id] = { entity_id, state: value, attributes, last_changed: '2026-09-24T10:00:00Z' };
    return { entity_id, device_id: devices[0].id, original_name: attributes.friendly_name, ...metadata };
  });
  const registry = { areas: [{area_id: 'room', name: 'Room'}], devices, entities };
  const data = buildDashboardDataFromHomeAssistant(states, buildRegistryIndex(registry));
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
    assert.equal(chip(data,'system-mode').value,'Monitoring');
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
    assert.equal(chip(data,'system-mode').value,'Monitoring');
  }
  const {card} = fixture([['binary_sensor.detector_signal_connectivity_42','off']]);
  assert.equal(card.isOnline,true);
  assert.equal(card.metrics.find((metric) => metric.label === 'Link').value,'Online');
});

test('operational on/open/online and missing historical events are not faults', () => {
  for (const [model, inputs] of [
    ['WallSwitch',[['switch.pump','on'],['sensor.last_event_time','unknown']]],
    ['WaterStop',[['switch.valve','off'],['sensor.valve_position','OPEN']]],
    ['DoorProtect',[['binary_sensor.door','on',{device_class:'door'}],['binary_sensor.online','on',{device_class:'connectivity'}]]],
  ]) {
    const {card,data} = fixture(inputs,model);
    assert.equal(card.attention,false,model);
    assert.equal(chip(data,'system-alerts'),undefined);
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
  assert.equal(data.devices.find((item) => item.name === 'Garage light').type,'wall_switch');
});

test('unknown WallSwitch and intermediate/moving/unknown WaterStop disable both UI surfaces and dispatch', async () => {
  for (const [model,inputs] of [
    ['WallSwitch',[['switch.pump','unknown']]],
    ...['INTERMEDIATE','OPENING','CLOSING','MOVING','unknown','unavailable'].map((value) => ['WaterStop',[['switch.valve','on'],['sensor.valve_position',value]]]),
  ]) {
    const {card,states} = fixture(inputs,model);
    const action = card.actions[0];
    assert.equal(action.disabled,true,`${model} ${inputs.at(-1)[1]}`);
    assert.equal(action.service,'');
    assert.equal(card.attention,true);
    assert.equal(chip(fixture(inputs,model).data,'system-mode').value,'Monitoring');
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
    assert.equal(chip(data,'system-mode').value,'Monitoring');
    if (value==='off') assert.ok(!card.metrics?.some((metric)=>metric.tone==='amber'));
  }
  const {card,data}=fixture([['binary_sensor.temperature_alarm_55','on',{}, {unique_id:'ajaxbridge_zone_a0f80d_6_signal_temperature_alarm'}]],'FireProtect');
  assert.equal(card.tone,'red');
  assert.equal(data.rooms[0].safety.smokeHigh,1);
});
