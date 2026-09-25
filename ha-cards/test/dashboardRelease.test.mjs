import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { lstat, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

import { prepareRelease, verifyRelease } from '../scripts/dashboard-release.mjs';

const exec = promisify(execFile);
const script = fileURLToPath(new URL('../scripts/dashboard-release.mjs', import.meta.url));

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'ajaxbridge-release-test-'));
  t.after(async () => {
    assert.equal(path.dirname(root), os.tmpdir());
    assert.ok(path.basename(root).startsWith('ajaxbridge-release-test-'));
    await rm(root, { recursive: true, force: true });
  });
  const dist = path.join(root, 'dist');
  const out = path.join(root, 'releases');
  await mkdir(path.join(dist, 'assets'), { recursive: true });
  await mkdir(path.join(dist, '.vite'));
  const content = new Map([
    ['ajaxbridge-lovelace.js', Buffer.from('import "./assets/chunk.js";\n')],
    ['assets/chunk.js', Buffer.from('export const ready = true;\n')],
    ['assets/style.css', Buffer.from('body { color: white; }\n')],
    ['assets/icon.png', Buffer.from([0x89, 0x50, 0x4e, 0x47, 0, 1, 2])],
    ['assets/empty.dat', Buffer.alloc(0)],
    ['index.html', Buffer.from('<!doctype html><title>Preview</title>')],
    ['.vite/manifest.json', Buffer.from('{"entry":{"file":"ajaxbridge-lovelace.js"}}')],
  ]);
  for (const [file, bytes] of content) await writeFile(path.join(dist, file), bytes);
  return { root, dist, out, content };
}

async function readRelease(result) {
  return JSON.parse(await readFile(result.manifestPath, 'utf8'));
}

async function serveRelease(t, release, override = () => null) {
  const manifest = await readRelease(release);
  const routes = new Map();
  for (const file of manifest.files) {
    routes.set(`/local/ajax/releases/${manifest.id}/${file.path.split('/').map(encodeURIComponent).join('/')}`, {
      file: file.path,
      body: await readFile(path.join(release.releaseDir, file.path)),
    });
  }
  const requests = [];
  const server = createServer((request, response) => {
    requests.push({ method: request.method, url: request.url });
    const route = routes.get(request.url);
    if (!route) {
      response.writeHead(404).end('missing');
      return;
    }
    const changed = override(route.file, route.body) ?? {};
    const headers = changed.headers ?? {
      'content-type': /\.js$/.test(route.file) ? 'application/javascript; charset=utf-8'
        : /\.html$/.test(route.file) ? 'text/html' : 'application/octet-stream',
    };
    response.writeHead(changed.status ?? 200, headers).end(changed.body ?? route.body);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  });
  return { baseUrl: `http://127.0.0.1:${server.address().port}/local/ajax/releases`, requests };
}

test('release manifests hash every copied file and staging is deterministic and idempotent', async (t) => {
  const input = await fixture(t);
  const first = await prepareRelease(input);
  const manifest = await readRelease(first);
  assert.equal(manifest.id, first.id);
  assert.equal(path.basename(first.releaseDir), first.id);
  assert.equal(first.resourcePath, `${first.id}/ajaxbridge-lovelace.js`);
  assert.equal(first.resourceURL, undefined);
  assert.deepEqual(manifest.files.map((file) => file.path), [...input.content.keys()].sort());
  for (const file of manifest.files) {
    const bytes = await readFile(path.join(first.releaseDir, file.path));
    assert.deepEqual(bytes, input.content.get(file.path));
    assert.equal(file.size, bytes.length);
    assert.equal(file.sha256, createHash('sha256').update(bytes).digest('hex'));
  }
  const before = await lstat(first.manifestPath);
  assert.deepEqual(await prepareRelease(input), first);
  assert.equal((await lstat(first.manifestPath)).mtimeMs, before.mtimeMs);
  const elsewhere = await prepareRelease({ ...input, out: path.join(input.root, 'other-releases') });
  assert.equal(elsewhere.id, first.id);
  assert.deepEqual(await readdir(input.out), [first.id]);
});

test('changing any entry, chunk, style, image, or path produces a different release id', async (t) => {
  const input = await fixture(t);
  const original = await prepareRelease(input);
  for (const file of ['ajaxbridge-lovelace.js', 'assets/chunk.js', 'assets/style.css', 'assets/icon.png']) {
    const old = input.content.get(file);
    const changed = Buffer.from(old);
    changed[changed.length - 1] ^= 1; // Same length: identity depends on bytes, not metadata.
    await writeFile(path.join(input.dist, file), changed);
    const updated = await prepareRelease(input);
    assert.notEqual(updated.id, original.id, file);
    assert.deepEqual(await readFile(path.join(original.releaseDir, file)), old);
    await writeFile(path.join(input.dist, file), old);
  }
  await writeFile(path.join(input.dist, 'assets/another-name.png'), input.content.get('assets/icon.png'));
  assert.notEqual((await prepareRelease(input)).id, original.id);
});

test('restaging refuses modified, extra, missing files and corrupt or missing metadata', async (t) => {
  for (const change of ['modified', 'extra', 'missing', 'metadata', 'no_metadata']) {
    await t.test(change, async (t) => {
      const input = await fixture(t);
      const staged = await prepareRelease(input);
      const chunk = path.join(staged.releaseDir, 'assets/chunk.js');
      if (change === 'modified') await writeFile(chunk, 'altered');
      if (change === 'extra') await writeFile(path.join(staged.releaseDir, 'injected.js'), 'extra');
      if (change === 'missing') await rm(chunk);
      if (change === 'metadata') await writeFile(staged.manifestPath, '{}');
      if (change === 'no_metadata') await rm(staged.manifestPath);
      await assert.rejects(prepareRelease(input));
      assert.deepEqual(await readdir(input.out), [staged.id]);
      if (change === 'modified') assert.equal(await readFile(chunk, 'utf8'), 'altered');
    });
  }
});

test('preparation rejects nested output, reserved metadata, and missing entry', async (t) => {
  const input = await fixture(t);
  await assert.rejects(prepareRelease({ ...input, out: path.join(input.dist, 'releases') }), /outside/);
  await writeFile(path.join(input.dist, 'release.json'), '{}');
  await assert.rejects(prepareRelease(input), /reserved/);
  await rm(path.join(input.dist, 'release.json'));
  await rm(path.join(input.dist, 'ajaxbridge-lovelace.js'));
  await assert.rejects(prepareRelease(input), /digest/);
});

test('directory links are rejected in the source, output, and existing release', async (t) => {
  const input = await fixture(t);
  const external = path.join(input.root, 'external');
  await mkdir(external);
  await writeFile(path.join(external, 'sentinel'), 'unchanged');
  const sourceLink = path.join(input.dist, 'linked');
  try {
    await symlink(external, sourceLink, process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (['EPERM', 'EACCES'].includes(error.code)) { t.skip('Host does not allow creating directory links'); return; }
    throw error;
  }
  await assert.rejects(prepareRelease(input), /Symlinks/);
  await rm(sourceLink);
  await symlink(external, input.out, process.platform === 'win32' ? 'junction' : 'dir');
  await assert.rejects(prepareRelease(input), /Symlinks/);
  await rm(input.out);
  const staged = await prepareRelease(input);
  await symlink(external, path.join(staged.releaseDir, 'linked'), process.platform === 'win32' ? 'junction' : 'dir');
  await assert.rejects(prepareRelease(input), /Symlinks/);
  assert.equal(await readFile(path.join(external, 'sentinel'), 'utf8'), 'unchanged');
});

test('verification returns a resource URL only after all files pass real HTTP checks', async (t) => {
  const staged = await prepareRelease(await fixture(t));
  const server = await serveRelease(t, staged);
  const verified = await verifyRelease({ manifestPath: staged.manifestPath, baseUrl: server.baseUrl });
  const manifest = await readRelease(staged);
  assert.equal(verified.resourceURL, `${server.baseUrl}/${staged.id}/ajaxbridge-lovelace.js`);
  assert.equal(verified.verifiedFiles, manifest.files.length);
  assert.equal(server.requests.length, manifest.files.length);
  assert.ok(server.requests.every((request) => request.method === 'GET'));
  assert.ok(server.requests.some((request) => request.url.endsWith('/.vite/manifest.json')));
});

test('verification rejects wrong bytes, absent files, redirects, and non-JavaScript MIME', async (t) => {
  const cases = [
    ['wrong_bytes', { body: Buffer.from('export const ready = fals;\n') }, /mismatch/],
    ['missing', { status: 404 }, /HTTP 404/],
    ['redirect', { status: 302, headers: { location: '/should-not-be-followed' } }, /HTTP 302/],
    ['html_type', { headers: { 'content-type': 'text/html' } }, /MIME/],
    ['missing_type', { headers: {} }, /MIME/],
  ];
  for (const [name, changed, message] of cases) {
    await t.test(name, async (t) => {
      const staged = await prepareRelease(await fixture(t));
      const server = await serveRelease(t, staged, (file) => file === 'assets/chunk.js' ? changed : null);
      await assert.rejects(verifyRelease({ manifestPath: staged.manifestPath, baseUrl: server.baseUrl }), message);
      assert.ok(server.requests.every((request) => !request.url.includes('should-not-be-followed')));
    });
  }
});

test('HTML disguised as JavaScript is rejected even with the expected bytes and MIME', async (t) => {
  const input = await fixture(t);
  await writeFile(path.join(input.dist, 'ajaxbridge-lovelace.js'), '\ufeff<!doctype html><html><title>Login</title></html>');
  const staged = await prepareRelease(input);
  const server = await serveRelease(t, staged);
  await assert.rejects(verifyRelease({ manifestPath: staged.manifestPath, baseUrl: server.baseUrl }), /HTML was served/);
});

test('tampered manifest paths, hashes, ordering, entry and metadata are rejected before network access', async (t) => {
  const input = await fixture(t);
  const staged = await prepareRelease(input);
  const original = await readRelease(staged);
  const server = await serveRelease(t, staged);
  const badPaths = ['../escape.js', '/absolute.js', 'C:/escape.js', 'assets\\escape.js', 'assets//file.js',
    'assets/../file.js', 'assets/file.js:stream', 'release.json', 'assets/file.', 'assets/CON.txt'];
  const mutations = badPaths.map((badPath) => (manifest) => { manifest.files[0].path = badPath; });
  mutations.push(
    (manifest) => { manifest.files[0].sha256 = '0'.repeat(64); },
    (manifest) => { manifest.files[0].size = -1; },
    (manifest) => { manifest.files[0].extra = true; },
    (manifest) => { manifest.files.reverse(); },
    (manifest) => { manifest.files.push(manifest.files[0]); },
    (manifest) => { manifest.entry = 'index.html'; },
    (manifest) => { manifest.id = '0'.repeat(64); },
    (manifest) => { manifest.version = 2; },
    (manifest) => { manifest.extra = true; },
  );
  for (const mutate of mutations) {
    const manifest = structuredClone(original);
    mutate(manifest);
    await writeFile(staged.manifestPath, JSON.stringify(manifest));
    await assert.rejects(verifyRelease({ manifestPath: staged.manifestPath, baseUrl: server.baseUrl }));
  }
  assert.equal(server.requests.length, 0);
  const wrongDirectory = path.join(input.root, 'wrong-id');
  await mkdir(wrongDirectory);
  const wrongManifest = path.join(wrongDirectory, 'release.json');
  await writeFile(wrongManifest, JSON.stringify(original));
  await assert.rejects(verifyRelease({ manifestPath: wrongManifest, baseUrl: server.baseUrl }), /directory name/);
});

test('CLI prints an activation URL only on successful verification', async (t) => {
  const input = await fixture(t);
  const prepared = JSON.parse((await exec(process.execPath, [script, 'prepare', '--dist', input.dist, '--out', input.out])).stdout);
  assert.equal(prepared.resourceURL, undefined);
  const good = await serveRelease(t, prepared);
  const verified = JSON.parse((await exec(process.execPath, [script, 'verify', '--manifest', prepared.manifestPath, '--base-url', good.baseUrl])).stdout);
  assert.equal(verified.resourceURL, `${good.baseUrl}/${prepared.id}/ajaxbridge-lovelace.js`);
  const bad = await serveRelease(t, prepared, (file) => file.endsWith('.js') ? { status: 404 } : null);
  await assert.rejects(exec(process.execPath, [script, 'verify', '--manifest', prepared.manifestPath, '--base-url', bad.baseUrl]), (error) => {
    assert.equal(error.code, 1);
    assert.equal(error.stdout, '');
    assert.match(error.stderr, /HTTP 404/);
    return true;
  });
});
