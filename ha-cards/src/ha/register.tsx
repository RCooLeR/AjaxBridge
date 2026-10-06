import type { ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { DetailedDashboardView } from '../views/DetailedDashboardView';
import { ChipsOverviewView } from '../views/ChipsOverviewView';
import { setAssetBaseUrl } from '../utils/assets';
import type {
  AjaxBridgeChipsCardConfig,
  AjaxBridgeDetailedCardConfig,
  HomeAssistant,
} from './types';
import themeCss from '../styles/theme.css?raw';
import dashboardCss from '../styles/dashboard.css?raw';

setAssetBaseUrl(new URL(/* @vite-ignore */ './assets/', import.meta.url).toString());
const cardCss = `${themeCss}\n${dashboardCss}`;

abstract class ReactHomeAssistantElement<TConfig> extends HTMLElement {
  protected root: Root | null = null;
  protected mountNode: HTMLDivElement | null = null;
  protected hassValue?: HomeAssistant;
  protected configValue?: TConfig;

  connectedCallback() {
    this.style.display = 'block';

    if (!this.root) {
      const shadowRoot = this.shadowRoot ?? this.attachShadow({ mode: 'open' });
      shadowRoot.replaceChildren();

      const style = document.createElement('style');
      style.textContent = cardCss;
      shadowRoot.appendChild(style);

      const mountNode = document.createElement('div');
      mountNode.className = 'ajaxbridge-shadow-root';
      shadowRoot.appendChild(mountNode);

      this.mountNode = mountNode;
      this.root = createRoot(mountNode);
    }

    this.renderReact();
  }

  disconnectedCallback() {
    this.root?.unmount();
    this.root = null;
    this.mountNode = null;
  }

  set hass(hass: HomeAssistant) {
    this.hassValue = hass;
    this.renderReact();
  }

  protected renderReact() {
    if (!this.root) {
      return;
    }

    this.root.render(this.renderNode());
  }

  protected abstract renderNode(): ReactNode;
}

class AjaxBridgeDetailedCard extends ReactHomeAssistantElement<AjaxBridgeDetailedCardConfig> {
  setConfig(config: AjaxBridgeDetailedCardConfig) {
    this.configValue = config;
    this.style.height = '100%';
    this.renderReact();
  }

  getCardSize() {
    return 20;
  }

  getGridOptions() {
    return {
      columns: 'full',
      min_rows: 10,
      rows: 12,
    };
  }

  protected renderNode() {
    const config = this.configValue ?? { type: 'custom:ajaxbridge-detailed-card' };

    return (
      <div className="ajaxbridge-ha-card ajaxbridge-ha-card--detailed">
        <DetailedDashboardView
          mode="embedded"
          initialRoomId={config.default_room}
          hass={this.hassValue}
          account={config.account}
          dahuaBase={config.dahua_base}
          gridPowerAlarmEntities={config.grid_power_alarm_entities}
        />
      </div>
    );
  }

  static getStubConfig(): Omit<AjaxBridgeDetailedCardConfig, 'type'> {
    return {
      default_room: 'living-room',
    };
  }
}

class AjaxBridgeChipsCard extends ReactHomeAssistantElement<AjaxBridgeChipsCardConfig> {
  setConfig(config: AjaxBridgeChipsCardConfig) {
    if (config.max_chips !== undefined && config.max_chips < 1) {
      throw new Error('max_chips must be greater than 0');
    }

    this.configValue = config;
    this.renderReact();
  }

  getCardSize() {
    return 3;
  }

  getGridOptions() {
    return {
      columns: 'full',
      rows: 3,
      min_rows: 2,
      max_rows: 4,
    };
  }

  protected renderNode() {
    const config = this.configValue ?? { type: 'custom:ajaxbridge-chips-card' };

    return (
      <div className="ajaxbridge-ha-card ajaxbridge-ha-card--chips">
        <ChipsOverviewView
          maxChips={config.max_chips}
          hass={this.hassValue}
          account={config.account}
          dahuaBase={config.dahua_base}
          gridPowerAlarmEntities={config.grid_power_alarm_entities}
        />
      </div>
    );
  }

  static getStubConfig(): Omit<AjaxBridgeChipsCardConfig, 'type'> {
    return {
      max_chips: 7,
    };
  }
}

if (!customElements.get('ajaxbridge-detailed-card')) {
  customElements.define('ajaxbridge-detailed-card', AjaxBridgeDetailedCard);
}

if (!customElements.get('ajaxbridge-chips-card')) {
  customElements.define('ajaxbridge-chips-card', AjaxBridgeChipsCard);
}

window.customCards = window.customCards || [];
window.customCards.push(
  {
    type: 'ajaxbridge-detailed-card',
    name: 'AjaxBridge Detailed',
    description: 'Fullscreen AjaxBridge security dashboard card for panel views.',
    preview: false,
  },
  {
    type: 'ajaxbridge-chips-card',
    name: 'AjaxBridge Chips',
    description: 'Compact AjaxBridge overview card with global state chips.',
    preview: false,
  },
);

declare global {
  interface Window {
    customCards?: Array<{
      type: string;
      name: string;
      description?: string;
      preview?: boolean;
    }>;
  }
}
