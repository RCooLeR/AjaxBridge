import { useEffect, useId, useRef, useState } from 'react';
import type { DashboardChip } from '../models/dashboard';
import { getToneClass } from '../utils/assets';
import { Icon } from './Icon';

interface SystemChipProps {
  chip: DashboardChip;
  compact?: boolean;
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

            {chip.details.items.length > 0 ? (
              <ul className="system-chip-dialog__items">
                {chip.details.items.map((item) => (
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
            ) : (
              <p className="system-chip-dialog__empty">No matching devices to show.</p>
            )}
          </section>
        </dialog>
      ) : null}
    </>
  );
}
