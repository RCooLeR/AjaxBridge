import type { Device, DeviceAction, DeviceActionDomain } from '../models/dashboard';
import type { HomeAssistant } from './types';
import { inferAjaxButtonDeviceType, isPhysicalAjaxButtonType, resolveDeviceControlState } from '../utils/deviceSemantics';

export async function callEntityService(
  hass: HomeAssistant,
  domain: DeviceActionDomain,
  service: string,
  entityId: string,
): Promise<unknown> {
  if (!hass.callService) throw new Error('Home Assistant service API unavailable');
  // A timeout may occur after the device acted. Never retry an ambiguous result.
  return hass.callService(domain, service, {}, { entity_id: entityId });
}

export async function callDeviceAction(hass: HomeAssistant, device: Device, action: DeviceAction, confirm?: (message: string) => boolean): Promise<boolean> {
  if (isPhysicalAjaxButtonType(device.type) || inferAjaxButtonDeviceType(device.model)) throw new Error('Physical inputs are read-only');
  if (action.disabled || !action.service) throw new Error(action.disabledReason ?? 'Control unavailable');
  const currentAction = device.actions?.find((candidate) => candidate.id === action.id && candidate.entityId === action.entityId);
  if (!currentAction || currentAction.disabled) throw new Error('Action is no longer available');
  const verifyState = () => {
    if (action.domain === 'button') return;
    const rawState = hass.states[action.observedStateEntityId ?? action.entityId]?.state;
    const valvePosition = action.valvePositionEntityId ? hass.states[action.valvePositionEntityId]?.state ?? 'unknown' : undefined;
    const observed = resolveDeviceControlState({ domain: action.domain, deviceType: device.type, rawState, valvePosition });
    if ((observed !== 'on' && observed !== 'off') || observed !== action.controlState) throw new Error('Device state changed; wait for the controls to update');
  };
  verifyState();
  if (action.confirmation && !confirm?.(`${device.name}: ${action.confirmation}`)) return false;
  verifyState();
  await callEntityService(hass, action.domain, action.service, action.entityId);
  return true;
}
