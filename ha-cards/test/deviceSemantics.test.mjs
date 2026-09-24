import assert from 'node:assert/strict';
import test from 'node:test';

import {
  classifyAjaxDiagnosticMetric,
  controlStateLabel,
  deviceControlAccessibility,
  inferAjaxButtonDeviceType,
  isPhysicalAjaxButtonType,
  resolveDeviceControlState,
  selectSidebarMetrics,
} from '../src/utils/deviceSemantics.ts';

test('recognizes only the supported Ajax physical button models', () => {
  for (const model of ['Button', 'ButtonS', 'Button S', 'DoubleButton', 'SuperiorDoubleButtonG3', 'SpaceControl']) {
    assert.equal(inferAjaxButtonDeviceType(model), 'panic_button', model);
  }

  assert.equal(inferAjaxButtonDeviceType('WallSwitch'), null);
  assert.equal(inferAjaxButtonDeviceType('Generic button controller'), null);
  assert.equal(isPhysicalAjaxButtonType('panic_button'), true);
  assert.equal(isPhysicalAjaxButtonType('motion_sensor'), false);
});

test('keeps WallSwitch unknown separate from off', () => {
  assert.equal(resolveDeviceControlState({ domain: 'switch', rawState: 'ON', deviceType: 'wall_switch' }), 'on');
  assert.equal(resolveDeviceControlState({ domain: 'switch', rawState: 'OFF', deviceType: 'wall_switch' }), 'off');
  assert.equal(resolveDeviceControlState({ domain: 'switch', rawState: 'unknown', deviceType: 'wall_switch' }), 'unknown');
  assert.equal(controlStateLabel('unknown'), 'Unknown');
});

test('prefers the actual WaterStop position and preserves transitional states', () => {
  assert.equal(
    resolveDeviceControlState({ domain: 'switch', rawState: 'OFF', deviceType: 'waterstop', valvePosition: 'OPEN' }),
    'on',
  );
  assert.equal(
    resolveDeviceControlState({ domain: 'switch', rawState: 'ON', deviceType: 'waterstop', valvePosition: 'CLOSED' }),
    'off',
  );
  assert.equal(
    resolveDeviceControlState({ domain: 'switch', rawState: 'unknown', deviceType: 'waterstop', valvePosition: 'INTERMEDIATE' }),
    'intermediate',
  );
  assert.equal(
    resolveDeviceControlState({ domain: 'switch', rawState: 'unknown', deviceType: 'waterstop', valvePosition: 'OPENING' }),
    'opening',
  );
  assert.equal(controlStateLabel('intermediate', true), 'Intermediate');
  assert.equal(controlStateLabel('opening', true), 'Opening');
});

test('classifies the new diagnostics without treating last-event timestamps as device updates', () => {
  assert.equal(classifyAjaxDiagnosticMetric('sensor.valve_position Valve position', ''), 'valve_position');
  assert.equal(classifyAjaxDiagnosticMetric('sensor.issue_count Issue count', ''), 'issue_count');
  assert.equal(classifyAjaxDiagnosticMetric('sensor.operating_mode Operating mode', ''), 'operating_mode');
  assert.equal(classifyAjaxDiagnosticMetric('sensor.firmware_version Firmware version', ''), 'firmware_version');
  assert.equal(classifyAjaxDiagnosticMetric('sensor.battery_check_status Battery check status', ''), 'battery_check_status');
  assert.equal(classifyAjaxDiagnosticMetric('sensor.device_last_update Last update', 'timestamp'), 'device_last_update');
  assert.equal(classifyAjaxDiagnosticMetric('sensor.last_event_time Last event time', 'timestamp'), null);
});

test('selects compact sidebar telemetry and diagnostics without duplicate labels', () => {
  const metrics = [
    { id: 'temperature', label: 'Temperature' },
    { id: 'temperature-duplicate', label: ' temperature ' },
    { id: 'battery', label: 'Battery' },
    { id: 'signal', label: 'Signal' },
    { id: 'valve', label: 'Valve position' },
    { id: 'issues', label: 'Issues' },
    { id: 'battery-check', label: 'Battery check' },
    { id: 'mode', label: 'Mode' },
    { id: 'firmware', label: 'Firmware' },
    { id: 'updated', label: 'Device updated' },
    { id: 'event', label: 'Last event' },
  ];

  assert.deepEqual(
    selectSidebarMetrics(metrics).map((metric) => metric.id),
    ['temperature', 'battery', 'signal', 'valve', 'issues', 'battery-check', 'mode', 'firmware', 'updated'],
  );
});

test('uses switch semantics only for known binary states', () => {
  assert.deepEqual(
    deviceControlAccessibility({ actionLabel: 'Turn off', deviceName: 'Server power', state: 'on', stateLabel: 'On' }),
    { role: 'switch', ariaChecked: true, label: 'Server power: On' },
  );
  assert.deepEqual(
    deviceControlAccessibility({ actionLabel: 'Turn on', deviceName: 'Server power', state: 'unknown', stateLabel: 'Unknown' }),
    { label: 'Server power: Unknown. Action: Turn on' },
  );
  assert.deepEqual(
    deviceControlAccessibility({ actionLabel: 'Open', deviceName: 'Water valve', state: 'closing', stateLabel: 'Closing' }),
    { label: 'Water valve: Closing. Action: Open' },
  );
});
