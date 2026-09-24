import iconRegistryData from '../data/iconRegistry.json';
import type { Device, GlowTone, IconRef, IconRegistry } from '../models/dashboard';

const iconRegistry = iconRegistryData as IconRegistry;
let assetBaseUrl = '/assets/';

export function setAssetBaseUrl(baseUrl: string): void {
  assetBaseUrl = ensureTrailingSlash(baseUrl);
}

export function getIconColor(icon: IconRef): string | undefined {
  return iconRegistry[icon.category]?.[icon.key]?.color;
}

export function getMaterialIconName(icon: IconRef): string {
  const key = `${icon.category}:${icon.key}`;
  return materialIconNames[key] ?? materialIconNames[`devices:${icon.key}`] ?? materialIconNames['misc:info'];
}

export function getDeviceImageAsset(device: Pick<Device, 'type' | 'name' | 'model'>): string {
  const descriptor = `${device.type} ${device.name} ${device.model}`.toLowerCase();
  const matchedEntry = deviceImageMap.find((entry) => entry.pattern.test(descriptor));
  const fileName =
    typeof matchedEntry?.fileName === 'function'
      ? matchedEntry.fileName(descriptor)
      : (matchedEntry?.fileName ?? jeedomDeviceImage('MotionProtect_white.png'));
  return resolveAssetPath(`devices/${fileName}`);
}

export function getRoomImageAsset(fileName: string): string {
  if (!fileName) {
    return '';
  }
  if (/^(https?:)?\/\//.test(fileName) || fileName.startsWith('/')) {
    return normalizeHomeAssistantImageUrl(fileName);
  }
  return `${assetBaseUrl}rooms/${fileName}`;
}

export function getStaticAsset(path: string): string {
  return resolveAssetPath(path);
}

export function getToneClass(tone: GlowTone): string {
  return `tone-${tone}`;
}

export function withOpacity(hexColor: string, alpha: number): string {
  const normalized = hexColor.replace('#', '');

  if (normalized.length !== 6) {
    return `rgba(45, 226, 230, ${alpha})`;
  }

  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);

  return `rgba(${red}, ${green}, ${blue}, ${alpha})`;
}

function resolveAssetPath(path: string): string {
  return `${assetBaseUrl}${stripAssetPrefix(path)}`;
}

function stripAssetPrefix(path: string): string {
  return path.replace(/^\/?assets\//, '');
}

function ensureTrailingSlash(path: string): string {
  return path.endsWith('/') ? path : `${path}/`;
}

function normalizeHomeAssistantImageUrl(url: string): string {
  return url.replace(/(\/api\/image\/serve\/[^/?]+)\/\d+x\d+(\?.*)?$/i, '$1/original$2');
}

type DeviceImageEntry = {
  pattern: RegExp;
  fileName: string | ((descriptor: string) => string);
};

const jeedomDeviceImage = (fileName: string): string => `jeedom/${fileName}`;
const dahuaDeviceImage = (fileName: string): string => `dahua/${fileName}`;

function colorVariantImage(descriptor: string, whiteFileName: string, blackFileName = whiteFileName): string {
  return jeedomDeviceImage(/\bblack\b|_black\b/.test(descriptor) ? blackFileName : whiteFileName);
}

const deviceImageMap: DeviceImageEntry[] = [
  {
    pattern: /dh-ipc-hfw2849s-s-il-be|hfw2849s-s-il-be/,
    fileName: dahuaDeviceImage('dh-ipc-hfw2849s-s-il-black-white.png'),
  },
  { pattern: /dh-ipc-hfw2849s-s-il|hfw2849/, fileName: dahuaDeviceImage('dh-ipc-hfw2849s-s-il.png') },
  { pattern: /dh-ipc-hfw1430ds1-saw|hfw1430/, fileName: dahuaDeviceImage('dh-ipc-hfw1430ds1-saw.png') },
  { pattern: /ipc-s7xe-10m0wed|s7xe/, fileName: dahuaDeviceImage('ipc-s7xe-10m0wed.jpg') },
  { pattern: /dh-t4a-pv|t4a-pv/, fileName: dahuaDeviceImage('dh-t4a-pv.png') },
  { pattern: /dh-h4c-ge|h4c-ge/, fileName: dahuaDeviceImage('dh-h4c-ge.webp') },
  { pattern: /dhi-vto2311r-wp|vto2311|vto|doorbell/, fileName: dahuaDeviceImage('dhi-vto2311r-wp.png') },
  { pattern: /dhi-nvr5232-ei|dh-nvr5232|nvr5232|nvr/, fileName: dahuaDeviceImage('dhi-nvr5232-ei.png') },
  { pattern: /nvr\s*channel|dahua.*camera|camera.*dahua/, fileName: dahuaDeviceImage('dh-ipc-hfw1430ds1-saw.png') },
  { pattern: /fireprotect2plus|fireprotect\s*2\s*plus/, fileName: jeedomDeviceImage('FireProtect2Plus_black.png') },
  {
    pattern: /fireprotect2(?!plus)|fireprotect\s*2/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'FireProtect2_white.png', 'FireProtect2_black.png'),
  },
  {
    pattern: /fireprotect\s*plus|fireprotect_plus/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'FireProtectPlus_white.png', 'FireProtectPlus_black.png'),
  },
  { pattern: /spacecontrol|space control|ajax_spacecontrol/, fileName: jeedomDeviceImage('SpaceControl_white.png') },
  {
    pattern: /double\s*button|doublebutton/,
    fileName: (descriptor) =>
      colorVariantImage(descriptor, 'DoubleButton_white.png', 'DoubleButton_black.png'),
  },
  {
    pattern: /\bbutton(?:\s+s|s)?\b|panic_button/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'Button_white.png', 'Button_black.png'),
  },
  { pattern: /waterstop|water stop|valve/, fileName: jeedomDeviceImage('WaterStop.png') },
  {
    pattern: /multi\s*transmitter|multitransmitter|multi_transmitter/,
    fileName: (descriptor) =>
      colorVariantImage(
        descriptor,
        'MultiTransmitterWireInput_white.png',
        'MultiTransmitterWireInput_black.png',
      ),
  },
  {
    pattern: /(^|[\s_-])transmitter($|[\s_-])|transmitter_jeweller|superior_transmitter/,
    fileName: jeedomDeviceImage('Transmitter.png'),
  },
  { pattern: /lifequality|life quality/, fileName: jeedomDeviceImage('LifeQuality.png') },
  {
    pattern: /combi\s*protect|combiprotect/,
    fileName: jeedomDeviceImage('CombiProtect_white.png'),
  },
  { pattern: /light\s*switch.*(2|two).*gang.*(2|two).*way|lightswitch_2_gang_2_way/, fileName: jeedomDeviceImage('LightSwitchTwoChannelTwoWay.png') },
  { pattern: /light\s*switch.*(2|two).*way|lightswitch_2_way/, fileName: jeedomDeviceImage('LightSwitchTwoWay.png') },
  { pattern: /lightswitch|light switch/, fileName: jeedomDeviceImage('LightSwitchTwoGang.png') },
  { pattern: /wallswitch|wall switch/, fileName: jeedomDeviceImage('WallSwitch.png') },
  {
    pattern: /socket|smart[_\s-]?plug|plug|outlet/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'Socket_white.png', 'Socket_black.png'),
  },
  { pattern: /\brelay\b/, fileName: jeedomDeviceImage('Relay.png') },
  {
    pattern: /hub_2_plus|hub\s*2\s*plus|hub2plus/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'HUB_2_PLUS_white.png', 'HUB_2_PLUS_black.png'),
  },
  { pattern: /hub_plus|hub\s*plus/, fileName: jeedomDeviceImage('HUB_PLUS_white.png') },
  { pattern: /hub_2($|[\s_-])|hub\s*2\b/, fileName: jeedomDeviceImage('HUB_2_white.png') },
  { pattern: /(^|[\s_-])hub($|[\s_-])|ajax account|account/, fileName: jeedomDeviceImage('HUB_white.png') },
  { pattern: /rex|repeater|range\s*extender|rangeextender/, fileName: jeedomDeviceImage('RangeExtender_white.png') },
  { pattern: /keypad.*touch|keypad_touchscreen/, fileName: jeedomDeviceImage('KeypadTouchscreen.png') },
  { pattern: /keypad.*plus|keypad_plus/, fileName: jeedomDeviceImage('KeypadPlus_white.png') },
  { pattern: /keypad/, fileName: jeedomDeviceImage('Keypad_white.png') },
  { pattern: /street\s*siren.*double\s*deck|streetsiren.*doubledeck/, fileName: jeedomDeviceImage('StreetSirenDoubleDeck_white.png') },
  { pattern: /street\s*siren|streetsiren/, fileName: jeedomDeviceImage('StreetSiren_white.png') },
  { pattern: /home\s*siren|homesiren|siren/, fileName: jeedomDeviceImage('HomeSiren_white.png') },
  { pattern: /glass/, fileName: jeedomDeviceImage('GlassProtect_white.png') },
  { pattern: /dual\s*curtain|dualcurtain/, fileName: jeedomDeviceImage('DualCurtainOutdoor.png') },
  { pattern: /curtain/, fileName: jeedomDeviceImage('MotionProtectCurtain_white.png') },
  { pattern: /motioncam.*outdoor.*phod|motion\s*cam.*outdoor.*phod/, fileName: jeedomDeviceImage('MotionCamOutdoorPhod.png') },
  { pattern: /motioncam.*outdoor|motion\s*cam.*outdoor/, fileName: jeedomDeviceImage('MotionCamOutdoor.png') },
  { pattern: /outdoor/, fileName: jeedomDeviceImage('MotionProtectOutdoor_white.png') },
  { pattern: /doorprotect.*plus|door\s*protect.*plus/, fileName: jeedomDeviceImage('DoorProtectPlus_white.png') },
  { pattern: /door|opening|window/, fileName: jeedomDeviceImage('DoorProtect_white.png') },
  {
    pattern: /smoke|fire|heat/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'FireProtect_white.png', 'FireProtect_black.png'),
  },
  {
    pattern: /leak|moisture|water/,
    fileName: (descriptor) => colorVariantImage(descriptor, 'LeaksProtect_white.png', 'LeaksProtect_black.png'),
  },
  { pattern: /motioncam.*phod|motion\s*cam.*phod/, fileName: jeedomDeviceImage('MotionCamPhod.png') },
  { pattern: /motioncam|motion cam/, fileName: jeedomDeviceImage('MotionCam_white.png') },
  { pattern: /motion/, fileName: jeedomDeviceImage('MotionProtect_white.png') },
];

const materialIconNames: Record<string, string> = {
  'rooms:attic': 'mdi:home-roof',
  'rooms:bathroom': 'mdi:shower',
  'rooms:bedroom': 'mdi:bed',
  'rooms:garage': 'mdi:garage',
  'rooms:guests_room': 'mdi:account-multiple',
  'rooms:house': 'mdi:home',
  'rooms:kids_room': 'mdi:teddy-bear',
  'rooms:kitchen': 'mdi:silverware-fork-knife',
  'rooms:living_room': 'mdi:sofa',
  'rooms:office': 'mdi:desk',
  'rooms:server_room': 'mdi:server',
  'rooms:small_house': 'mdi:home-variant',
  'rooms:terrace': 'mdi:balcony',
  'rooms:yard': 'mdi:grass',
  'devices:camera': 'mdi:cctv',
  'devices:curtain_motion': 'mdi:curtains',
  'devices:door_sensor': 'mdi:door',
  'devices:fire_detector': 'mdi:fire-alert',
  'devices:glass_break': 'mdi:glass-fragile',
  'devices:hub': 'mdi:router-wireless',
  'devices:keypad': 'mdi:dialpad',
  'devices:leak_detector': 'mdi:water-alert',
  'devices:light_switch': 'mdi:light-switch',
  'devices:motion_sensor': 'mdi:motion-sensor',
  'devices:outdoor_motion': 'mdi:motion-sensor',
  'devices:panic_button': 'mdi:button-pointer',
  'devices:relay': 'mdi:electric-switch',
  'devices:repeater': 'mdi:wifi-sync',
  'devices:siren': 'mdi:alarm-light',
  'devices:smart_plug': 'mdi:power-socket-eu',
  'devices:smoke_detector': 'mdi:smoke-detector',
  'devices:thermostat': 'mdi:thermostat',
  'devices:transmitter': 'mdi:access-point',
  'devices:multitransmitter': 'mdi:access-point-network',
  'devices:wall_switch': 'mdi:electric-switch',
  'devices:waterstop': 'mdi:valve',
  'devices:window_sensor': 'mdi:window-closed',
  'sensors:battery': 'mdi:battery',
  'sensors:co2': 'mdi:molecule-co2',
  'sensors:fire': 'mdi:fire',
  'sensors:gas': 'mdi:gas-cylinder',
  'sensors:humidity': 'mdi:water-percent',
  'sensors:light': 'mdi:brightness-6',
  'sensors:motion': 'mdi:motion-sensor',
  'sensors:noise': 'mdi:volume-high',
  'sensors:signal': 'mdi:signal',
  'sensors:smoke': 'mdi:smoke',
  'sensors:temperature': 'mdi:thermometer',
  'sensors:vibration': 'mdi:vibrate',
  'sensors:water_leak': 'mdi:water-alert',
  'events:alarm': 'mdi:alarm-light',
  'events:armed_event': 'mdi:shield-lock',
  'events:battery_low_event': 'mdi:battery-alert',
  'events:device_offline': 'mdi:cloud-off-outline',
  'events:disarmed_event': 'mdi:shield-off',
  'events:door_opened': 'mdi:door-open',
  'events:fire_detected': 'mdi:fire-alert',
  'events:gas_detected': 'mdi:gas-cylinder',
  'events:human_detected': 'mdi:account-alert',
  'events:intrusion_detected': 'mdi:shield-alert',
  'events:leak_detected': 'mdi:water-alert',
  'events:maintenance_event': 'mdi:wrench-clock',
  'events:motion_detected': 'mdi:motion-sensor',
  'events:night_mode_event': 'mdi:weather-night',
  'events:ok': 'mdi:check-circle',
  'events:panic': 'mdi:alert-octagon',
  'events:power_lost': 'mdi:power-plug-off',
  'events:restored': 'mdi:restore',
  'events:smoke_detected': 'mdi:smoke-detector-alert',
  'events:tamper_detected': 'mdi:shield-bug',
  'events:tripwire_detected': 'mdi:vector-line',
  'events:vehicle_detected': 'mdi:car-alert',
  'events:window_opened': 'mdi:window-open',
  'connectivity:cloud': 'mdi:cloud-check',
  'connectivity:cloud_off': 'mdi:cloud-off-outline',
  'connectivity:ethernet': 'mdi:ethernet',
  'connectivity:gsm': 'mdi:cellphone-wireless',
  'connectivity:lan': 'mdi:lan',
  'connectivity:signal_high': 'mdi:signal-cellular-3',
  'connectivity:signal_low': 'mdi:signal-cellular-1',
  'connectivity:signal_medium': 'mdi:signal-cellular-2',
  'connectivity:wifi': 'mdi:wifi',
  'connectivity:wifi_off': 'mdi:wifi-off',
  'misc:alert': 'mdi:alert',
  'misc:check': 'mdi:check',
  'misc:delete': 'mdi:delete',
  'misc:edit': 'mdi:pencil',
  'misc:energy': 'mdi:lightning-bolt',
  'misc:favorite': 'mdi:star',
  'misc:history': 'mdi:history',
  'misc:info': 'mdi:information',
  'misc:minus': 'mdi:minus',
  'misc:play': 'mdi:play',
  'misc:plus': 'mdi:plus',
  'misc:volume_high': 'mdi:volume-high',
  'misc:volume_off': 'mdi:volume-off',
  'navigation:back': 'mdi:arrow-left',
  'navigation:close': 'mdi:close',
  'navigation:down': 'mdi:chevron-down',
  'navigation:home': 'mdi:home',
  'navigation:menu': 'mdi:menu',
  'navigation:next': 'mdi:arrow-right',
  'navigation:rooms': 'mdi:floor-plan',
  'navigation:search': 'mdi:magnify',
  'navigation:settings': 'mdi:cog',
  'navigation:up': 'mdi:chevron-up',
  'security-states:breach': 'mdi:shield-alert',
  'security-states:door_closed': 'mdi:door-closed',
  'security-states:door_open': 'mdi:door-open',
  'security-states:locked': 'mdi:lock',
  'security-states:perimeter': 'mdi:shield-home',
  'security-states:safe': 'mdi:shield-check',
  'security-states:tamper': 'mdi:shield-bug',
  'security-states:unlocked': 'mdi:lock-open',
  'security-states:window_closed': 'mdi:window-closed',
  'security-states:window_open': 'mdi:window-open',
  'system-states:alarm_active': 'mdi:alarm-light',
  'system-states:armed': 'mdi:shield-lock',
  'system-states:battery_low': 'mdi:battery-alert',
  'system-states:disarmed': 'mdi:shield-off',
  'system-states:maintenance': 'mdi:wrench-clock',
  'system-states:night_mode': 'mdi:weather-night',
  'system-states:offline': 'mdi:cloud-off-outline',
  'system-states:ok': 'mdi:check-circle',
  'system-states:online': 'mdi:cloud-check',
  'system-states:grid_power': 'mdi:transmission-tower',
  'system-states:power_loss': 'mdi:power-plug-off',
  'system-states:trouble': 'mdi:alert-circle',
  'system-states:updating': 'mdi:update',
};
