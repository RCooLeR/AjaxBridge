import type { DeviceControlState, DeviceType } from '../models/dashboard';

const AJAX_BUTTON_MODELS = new Set([
  'button',
  'buttons',
  'doublebutton',
  'spacecontrol',
  'superiordoublebuttong3',
]);

const SIDEBAR_METRIC_LABELS = new Set([
  'battery',
  'battery check',
  'current',
  'device updated',
  'firmware',
  'issues',
  'mode',
  'power',
  'signal',
  'temperature',
  'valve position',
  'voltage',
]);

export type AjaxDiagnosticMetricKind =
  | 'battery_check_status'
  | 'device_last_update'
  | 'firmware_version'
  | 'issue_count'
  | 'operating_mode'
  | 'valve_position';

export function inferAjaxButtonDeviceType(model: string): DeviceType | null {
  return AJAX_BUTTON_MODELS.has(compactModel(model)) ? 'panic_button' : null;
}

export function isPhysicalAjaxButtonType(deviceType: string): boolean {
  return deviceType === 'panic_button';
}

export function selectSidebarMetrics<T extends { label: string }>(metrics: T[]): T[] {
  const selectedLabels = new Set<string>();

  return metrics.filter((metric) => {
    const label = metric.label.trim().toLowerCase();
    if (!SIDEBAR_METRIC_LABELS.has(label) || selectedLabels.has(label)) {
      return false;
    }
    selectedLabels.add(label);
    return true;
  });
}

export function deviceControlAccessibility(input: {
  actionLabel: string;
  deviceName: string;
  state: DeviceControlState;
  stateLabel: string;
}): { role?: 'switch'; ariaChecked?: boolean; label: string } {
  const baseLabel = `${input.deviceName}: ${input.stateLabel}`;
  if (input.state === 'on' || input.state === 'off') {
    return {
      role: 'switch',
      ariaChecked: input.state === 'on',
      label: baseLabel,
    };
  }

  return {
    label: `${baseLabel}. Action: ${input.actionLabel}`,
  };
}

export function resolveDeviceControlState(input: {
  deviceType?: string;
  domain: string;
  rawState?: string;
  valvePosition?: string;
}): DeviceControlState {
  if (isWaterStopType(input.deviceType)) {
    const positionState = controlStateFromValue(input.valvePosition);
    if (positionState !== 'unknown') {
      return positionState;
    }
  }

  const domain = input.domain.trim().toLowerCase();
  if (domain === 'lock') {
    const lockState = (input.rawState ?? '').trim().toLowerCase();
    return lockState === 'unlocked' ? 'on' : lockState === 'locked' ? 'off' : 'unknown';
  }
  if (domain !== 'switch' && domain !== 'valve') {
    return 'unknown';
  }

  return controlStateFromValue(input.rawState);
}

export function controlStateLabel(state: DeviceControlState, valve = false): string {
  switch (state) {
    case 'on':
      return valve ? 'Open' : 'On';
    case 'off':
      return valve ? 'Closed' : 'Off';
    case 'opening':
      return 'Opening';
    case 'closing':
      return 'Closing';
    case 'intermediate':
      return 'Intermediate';
    case 'moving':
      return 'Moving';
    default:
      return 'Unknown';
  }
}

export function classifyAjaxDiagnosticMetric(text: string, deviceClass: string): AjaxDiagnosticMetricKind | null {
  const descriptor = text.toLowerCase();
  const normalizedDeviceClass = deviceClass.toLowerCase();

  if (
    matchesDescriptor(descriptor, ['device_last_update'])
    || (normalizedDeviceClass === 'timestamp' && descriptor.includes('last update'))
  ) {
    return 'device_last_update';
  }
  if (matchesDescriptor(descriptor, ['valve_position']) || descriptor.includes('valve position')) {
    return 'valve_position';
  }
  if (matchesDescriptor(descriptor, ['issue_count']) || descriptor.includes('issue count')) {
    return 'issue_count';
  }
  if (matchesDescriptor(descriptor, ['battery_check_status']) || descriptor.includes('battery check status')) {
    return 'battery_check_status';
  }
  if (matchesDescriptor(descriptor, ['operating_mode', 'button_mode']) || descriptor.includes('operating mode')) {
    return 'operating_mode';
  }
  if (matchesDescriptor(descriptor, ['firmware_version']) || descriptor.includes('firmware version')) {
    return 'firmware_version';
  }
  return null;
}

function compactModel(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '');
}

function isWaterStopType(deviceType?: string): boolean {
  const compact = compactModel(deviceType ?? '');
  return compact === 'waterstop' || compact === 'valve';
}

function controlStateFromValue(value?: string): DeviceControlState {
  switch ((value ?? '').trim().toLowerCase()) {
    case 'on':
    case 'open':
    case 'opened':
    case 'true':
    case '1':
      return 'on';
    case 'off':
    case 'close':
    case 'closed':
    case 'false':
    case '0':
      return 'off';
    case 'opening':
      return 'opening';
    case 'closing':
      return 'closing';
    case 'intermediate':
    case 'partially_open':
    case 'partially open':
      return 'intermediate';
    case 'moving':
      return 'moving';
    default:
      return 'unknown';
  }
}

function matchesDescriptor(text: string, needles: string[]): boolean {
  return needles.some((needle) => {
    const escaped = needle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`(^|[\\s._-])${escaped}([\\s._-]|$)`, 'i').test(text);
  });
}
