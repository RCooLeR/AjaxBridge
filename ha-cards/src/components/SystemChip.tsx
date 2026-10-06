import { useEffect, useId, useRef, useState } from 'react';
import type { DashboardChip, DashboardChipDetailItem, DashboardChipDetails } from '../models/dashboard';
import { getToneClass } from '../utils/assets';
import { Icon } from './Icon';

interface SystemChipProps {
  chip: DashboardChip;
  compact?: boolean;
}

interface SystemChipDetailListsProps {
  details: DashboardChipDetails;
  idPrefix: string;
}

interface SystemChipDetailItemListProps {
  items: DashboardChipDetailItem[];
  labelledBy?: string;
  scrollable?: boolean;
}

/* oxlint-disable jsx-a11y/no-noninteractive-tabindex -- The bounded zone list must remain keyboard-scrollable. */
function SystemChipDetailItemList({ items, labelledBy, scrollable = false }: SystemChipDetailItemListProps) {
  const list = (
    <ul
      className="system-chip-dialog__items"
      aria-labelledby={scrollable ? undefined : labelledBy}
    >
      {items.map((item) => (
        <li
          key={item.id}
          className={[
            'system-chip-dialog__item',
            item.tone ? getToneClass(item.tone) : '',
          ]
            .filter(Boolean)
            .join(' ')}
        >
          <span className="system-chip-dialog__status" aria-hidden="true" />
          <span className="system-chip-dialog__item-label">{item.label}</span>
          {item.value ? <strong>{item.value}</strong> : null}
        </li>
      ))}
    </ul>
  );

  if (!scrollable) {
    return list;
  }

  return (
    <section
      className="system-chip-dialog__scroll-region"
      aria-labelledby={labelledBy}
      tabIndex={0}
    >
      {list}
    </section>
  );
}
/* oxlint-enable jsx-a11y/no-noninteractive-tabindex */

export function SystemChipDetailLists({ details, idPrefix }: SystemChipDetailListsProps) {
  const sections = details.sections?.filter((section) => section.items.length > 0) ?? [];

  if (sections.length === 0) {
    return details.items.length > 0
      ? <SystemChipDetailItemList items={details.items} />
      : <p className="system-chip-dialog__empty">No matching devices to show.</p>;
  }

  return (
    <div className="system-chip-dialog__sections">
      {sections.map((section) => {
        const titleId = `${idPrefix}-${section.id}-title`;
        return (
          <section key={section.id} className="system-chip-dialog__section" aria-labelledby={titleId}>
            <h3 id={titleId} className="system-chip-dialog__section-title">{section.title}</h3>
            <SystemChipDetailItemList
              items={section.items}
              labelledBy={titleId}
              scrollable={section.scrollable}
            />
          </section>
        );
      })}
    </div>
  );
}

export function SystemChip({ chip, compact = false }: SystemChipProps) {
  const [detailsOpen, setDetailsOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const generatedId = useId();
  const dialogId = `system-chip-details-${generatedId.replaceAll(':', '')}`;
  const titleId = `${dialogId}-title`;
  const summaryId = `${dialogId}-summary`;
  const className = [
    'system-chip',
    compact ? 'system-chip--compact' : '',
    chip.active ? 'system-chip--active' : 'system-chip--inactive',
    chip.details ? 'system-chip--interactive' : '',
    getToneClass(chip.tone),
  ]
    .filter(Boolean)
    .join(' ');

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!detailsOpen || !dialog) {
      return;
    }

    const closeFromBackdrop = (event: MouseEvent) => {
      if (event.target === dialog) {
        setDetailsOpen(false);
        requestAnimationFrame(() => triggerRef.current?.focus());
      }
    };

    dialog.addEventListener('click', closeFromBackdrop);
    dialog.showModal();
    return () => {
      dialog.removeEventListener('click', closeFromBackdrop);
      if (dialog.open) {
        dialog.close();
      }
    };
  }, [detailsOpen]);

  function closeDetails() {
    setDetailsOpen(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  }

  const content = (
    <>
      <div className="system-chip__icon-wrap">
        <Icon icon={chip.icon} size={42} className="system-chip__icon" />
      </div>
      <div className="system-chip__copy">
        <span className="system-chip__label">{chip.label}</span>
        <strong className="system-chip__value">{chip.value}</strong>
      </div>
      {chip.details ? (
        <span className="system-chip__details-hint" aria-hidden="true">
          <Icon icon={{ category: 'misc', key: 'info' }} size={16} />
        </span>
      ) : null}
    </>
  );

  if (!chip.details) {
    return <article className={className}>{content}</article>;
  }

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={className}
        aria-haspopup="dialog"
        aria-expanded={detailsOpen}
        aria-controls={dialogId}
        aria-label={`${chip.label}: ${chip.value}. View details`}
        onClick={() => setDetailsOpen(true)}
      >
        {content}
      </button>

      {detailsOpen ? (
        <dialog
          ref={dialogRef}
          id={dialogId}
          className={`system-chip-dialog ${getToneClass(chip.tone)}`}
          aria-labelledby={titleId}
          aria-describedby={summaryId}
          onCancel={(event) => {
            event.preventDefault();
            closeDetails();
          }}
        >
          <section className="system-chip-dialog__panel">
            <header className="system-chip-dialog__header">
              <div className="system-chip-dialog__heading">
                <span className="system-chip-dialog__eyebrow">{chip.label}</span>
                <h2 id={titleId}>{chip.details.title}</h2>
              </div>
              <button
                type="button"
                className="system-chip-dialog__close"
                aria-label={`Close ${chip.details.title}`}
                onClick={closeDetails}
                autoFocus
              >
                <span className="system-chip-dialog__close-glyph" aria-hidden="true">×</span>
              </button>
            </header>

            <p id={summaryId} className="system-chip-dialog__summary">
              {chip.details.summary}
            </p>

            <SystemChipDetailLists details={chip.details} idPrefix={dialogId} />
          </section>
        </dialog>
      ) : null}
    </>
  );
}
