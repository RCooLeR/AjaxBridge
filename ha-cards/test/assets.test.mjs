import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';

import { createServer } from 'vite';

let assets;
let server;

before(async () => {
  server = await createServer({
    appType: 'custom',
    configFile: false,
    logLevel: 'silent',
    server: { middlewareMode: true },
  });
  assets = await server.ssrLoadModule('/src/utils/assets.ts');
  assets.setAssetBaseUrl('/local/ajax/assets/');
});

after(async () => {
  await server?.close();
});

test('uses product-specific images for Ajax physical button models', () => {
  const cases = [
    ['Button', 'Button_white.png'],
    ['ButtonS', 'Button_white.png'],
    ['Button S', 'Button_white.png'],
    ['DoubleButton', 'DoubleButton_white.png'],
    ['SuperiorDoubleButtonG3', 'DoubleButton_white.png'],
    ['SpaceControl', 'SpaceControl_white.png'],
  ];

  for (const [model, fileName] of cases) {
    assert.equal(
      assets.getDeviceImageAsset({ type: 'panic_button', name: 'Test device', model }),
      `/local/ajax/assets/devices/jeedom/${fileName}`,
      model,
    );
  }
});

test('uses the matching black button asset when color metadata is available', () => {
  assert.equal(
    assets.getDeviceImageAsset({ type: 'panic_button', name: 'Black emergency button', model: 'Button' }),
    '/local/ajax/assets/devices/jeedom/Button_black.png',
  );
  assert.equal(
    assets.getDeviceImageAsset({ type: 'panic_button', name: 'Black dual button', model: 'DoubleButton' }),
    '/local/ajax/assets/devices/jeedom/DoubleButton_black.png',
  );
});
