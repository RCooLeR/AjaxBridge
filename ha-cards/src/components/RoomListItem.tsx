import type { CSSProperties } from 'react';
import type { Room, RoomSummary } from '../models/dashboard';
import { getToneClass, withOpacity } from '../utils/assets';
import { Icon } from './Icon';
import { StatusBadge } from './StatusBadge';

interface RoomListItemProps {
  room: Room;
  summary: RoomSummary;
  selected: boolean;
  onSelect: (roomId: string) => void;
}

export function RoomListItem({ room, summary, selected, onSelect }: RoomListItemProps) {
  return (
    <button
      type="button"
      className={[
        'room-list-item',
        selected ? 'room-list-item--selected' : '',
        getToneClass(summary.tone),
      ]
        .filter(Boolean)
        .join(' ')}
      style={{ '--room-accent': withOpacity(room.accent, selected ? 0.42 : 0.18) } as CSSProperties}
      onClick={() => onSelect(room.id)}
      aria-pressed={selected}
    >
      <span className="room-list-item__icon">
          <Icon icon={room.icon} size={42} />
      </span>
      <span className="room-list-item__content">
        <span className="room-list-item__name">{room.name}</span>
        <span className="room-list-item__summary">{summary.latestEventLabel}</span>
      </span>
      <span className="room-list-item__meta">
        {summary.gridPower && summary.gridPower.known > 0 ? (
          <StatusBadge
            label={summary.gridPower.outage > 0 ? `${summary.gridPower.outage} grid outage` : 'Grid OK'}
            tone={summary.gridPower.outage > 0 ? 'amber' : 'green'}
          />
        ) : null}
        <StatusBadge
          label={summary.attentionCount > 0 ? `${summary.attentionCount} alert` : `${summary.onlineCount}/${summary.deviceCount} online`}
          tone={summary.attentionCount > 0 ? 'amber' : 'green'}
        />
      </span>
    </button>
  );
}
