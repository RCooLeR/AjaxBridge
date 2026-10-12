import { dashboardData } from '../data/loadDashboardData';
import { useDashboardData } from '../data/liveDashboardData';
import { TopSystemBar } from '../cards/TopSystemBar';
import type { HomeAssistant } from '../ha/types';
import type { SystemState } from '../models/dashboard';

interface ChipsOverviewViewProps {
  maxChips?: number;
  hass?: HomeAssistant;
  account?: string;
  sourceId?: string;
  areaIds?: readonly string[];
  dahuaBase?: string;
  gridPowerAlarmEntities?: readonly string[];
}

export function ChipsOverviewView({ maxChips, hass, account, sourceId, areaIds, dahuaBase, gridPowerAlarmEntities }: ChipsOverviewViewProps) {
  const liveData = useDashboardData(hass, account, dahuaBase, gridPowerAlarmEntities, { sourceId, areaIds });
  const data = hass ? liveData : dashboardData;
  const compactSystemState = filterCompactSystemState(data.systemState);

  return (
    <div className="ajaxbridge-theme ajaxbridge-compact-card">
      <TopSystemBar systemState={compactSystemState} maxChips={maxChips} compact />
    </div>
  );
}

function filterCompactSystemState(systemState: SystemState): SystemState {
  return {
    chips: systemState.chips.filter((chip) => chip.id !== 'system-rooms' && chip.label.toLowerCase() !== 'rooms'),
  };
}
