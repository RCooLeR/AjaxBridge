import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { rolldown } from 'rolldown';

const root = resolve(import.meta.dirname, '..').replaceAll('\\', '/');
const directory = await mkdtemp(join(tmpdir(), 'ajaxbridge-chip-details-test-'));
after(() => rm(directory, { recursive: true, force: true }));

const bundle = await rolldown({
  input: 'test-harness',
  platform: 'node',
  plugins: [{
    name: 'harness',
    resolveId(id) {
      if (id === 'test-harness') return '\0test-harness';
    },
    load(id) {
      if (id !== '\0test-harness') return;
      return `
        import { createElement } from '${root}/node_modules/react/index.js';
        import { renderToStaticMarkup } from '${root}/node_modules/react-dom/server.node.js';
        import { SystemChipDetailLists } from '${root}/src/components/SystemChip.tsx';
        export function renderDetails(details) {
          return renderToStaticMarkup(createElement(SystemChipDetailLists, { details, idPrefix: 'details' }));
        }
      `;
    },
  }],
});
const output = join(directory, 'harness.mjs');
await bundle.write({ file: output, format: 'esm' });
await bundle.close();
const { renderDetails } = await import(pathToFileURL(output).href);

test('grouped chip details keep event types outside the keyboard-scrollable zone list', () => {
  const eventTypes = [
    { id: 'people', label: 'People', value: '11', tone: 'violet' },
    { id: 'vehicles', label: 'Vehicles', value: '125', tone: 'violet' },
  ];
  const zones = [
    { id: 'zone-basement', label: 'Підвальне приміщення з дуже довгою назвою', value: '136', tone: 'violet' },
    { id: 'zone-yard', label: 'Yard', value: '2', tone: 'violet' },
  ];
  const markup = renderDetails({
    title: 'Smart Motion Detection',
    summary: 'Summary',
    items: [...eventTypes, ...zones],
    sections: [
      { id: 'event-types', title: 'Event types', items: eventTypes },
      { id: 'zones', title: 'Zones', items: zones, scrollable: true },
    ],
  });

  assert.equal(markup.match(/system-chip-dialog__section-title/g)?.length, 2);
  assert.equal(markup.match(/system-chip-dialog__scroll-region/g)?.length, 1);
  assert.match(markup, /id="details-event-types-title"[^>]*>Event types/);
  assert.match(markup, /id="details-zones-title"[^>]*>Zones/);
  assert.match(markup, /system-chip-dialog__scroll-region" aria-labelledby="details-zones-title" tabindex="0"/);
  assert.ok(markup.indexOf('Event types') < markup.indexOf('Zones'));
  assert.match(markup, /Підвальне приміщення з дуже довгою назвою/);
});

test('ordinary chip details retain the flat-list fallback', () => {
  const markup = renderDetails({
    title: 'Alerts',
    summary: 'Summary',
    items: [{ id: 'alarm', label: 'Front door', value: 'Open', tone: 'red' }],
  });

  assert.match(markup, /system-chip-dialog__items/);
  assert.doesNotMatch(markup, /system-chip-dialog__section/);
  assert.doesNotMatch(markup, /tabindex=/);
});
