export interface HomeAssistantState {
  entity_id?: string;
  state: string;
  attributes: Record<string, unknown>;
  last_changed?: string;
  last_updated?: string;
}

export interface HomeAssistant {
  states: Record<string, HomeAssistantState>;
  callWS?<TResponse = unknown>(message: {
    type: string;
    [key: string]: unknown;
  }): Promise<TResponse>;
  callService?(
    domain: string,
    service: string,
    serviceData?: Record<string, unknown>,
    target?: Record<string, unknown>,
  ): Promise<unknown>;
  user?: unknown;
  themes?: unknown;
  locale?: unknown;
}

export interface AjaxBridgeDetailedCardConfig {
  type: string;
  default_room?: string;
  account?: string;
  source_id?: string;
  area_ids?: readonly string[];
  dahua_base?: string;
  grid_power_alarm_entities?: readonly string[];
}

export interface AjaxBridgeChipsCardConfig {
  type: string;
  max_chips?: number;
  account?: string;
  source_id?: string;
  area_ids?: readonly string[];
  dahua_base?: string;
  grid_power_alarm_entities?: readonly string[];
}

export interface DashboardScope {
  sourceId?: string;
  areaIds?: readonly string[];
}
