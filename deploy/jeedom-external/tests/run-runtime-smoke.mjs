#!/usr/bin/env node
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const options = { context: 'desktop-linux', runtimeImage: 'ajaxbridge-jeedom-external:mysql84-test', mysqlImage: 'mysql:8.4' };
for (let i = 2; i < process.argv.length; i += 2) {
  const key = { '--context': 'context', '--runtime-image': 'runtimeImage', '--mysql-image': 'mysqlImage', '--keep-on-failure': 'keepOnFailure', '--reuse-fixture': 'reuseFixture' }[process.argv[i]];
  assert(key && process.argv[i + 1], 'Usage: node run-runtime-smoke.mjs [--context desktop-linux] [--runtime-image image] [--mysql-image mysql:8.4] [--keep-on-failure true] [--reuse-fixture previously-preserved-local-path]');
  options[key] = process.argv[i + 1];
}
const testDir = path.dirname(fileURLToPath(import.meta.url));
async function docker(args, allowed = [0], timeoutMs = 60000) {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', ['--context', options.context, ...args], { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', value => { stdout += value; });
    child.stderr.on('data', value => { stderr += value; });
    const timeout = setTimeout(() => child.kill(), timeoutMs);
    child.on('error', error => { clearTimeout(timeout); reject(error); });
    child.on('close', code => {
      clearTimeout(timeout);
      if (!allowed.includes(code)) return reject(new Error(`Docker ${args[0]} exited ${code}: ${stdout}\n${stderr}`));
      resolve({ code, stdout: stdout.trim(), stderr: stderr.trim() });
    });
  });
}
const endpoint = JSON.parse((await docker(['context', 'inspect', options.context])).stdout)[0].Endpoints.docker.Host;
assert(/^(?:npipe:\/\/|unix:\/\/)/.test(endpoint), 'Refusing a non-local Docker engine');
const runtimeImageId = (await docker(['image', 'inspect', '--format', '{{.Id}}', options.runtimeImage])).stdout;
assert(/^sha256:[a-f0-9]{64}$/.test(runtimeImageId), 'Runtime image must resolve to an immutable local image ID');
const temporary = options.reuseFixture ? path.resolve(options.reuseFixture) : await mkdtemp(path.join(os.tmpdir(), 'jeedom-runtime-smoke-'));
assert(path.dirname(temporary) === os.tmpdir() && path.basename(temporary).startsWith('jeedom-runtime-smoke-'), 'Refusing an unrelated fixture path');
const ownershipPath = path.join(temporary, 'smoke-owned.json');
let ownership = { owner: 'AjaxBridge isolated runtime smoke', seeded: false };
if (options.reuseFixture) {
  ownership = JSON.parse(await readFile(ownershipPath, 'utf8'));
  assert(ownership.owner === 'AjaxBridge isolated runtime smoke' && ownership.seeded === true, 'Reuse requires a previously completed owned local seed');
} else await writeFile(ownershipPath, JSON.stringify(ownership));
const html = path.join(temporary, 'html');
const runtime = path.join(temporary, 'runtime');
if (!options.reuseFixture) { await mkdir(html); await mkdir(runtime); }
const suffix = randomUUID().slice(0, 8);
const imageAlias = `jeedom-runtime-smoke:${suffix}`;
const network = `jeedom-runtime-smoke-${suffix}`;
const dbName = `jeedom-runtime-db-${suffix}`;
const appName = `jeedom-runtime-app-${suffix}`;
const createdContainers = new Set();
let networkCreated = false;
let aliasCreated = false;
const mountArgs = ['--mount', `type=bind,source=${html},target=/var/www/html`, '--mount', `type=bind,source=${runtime},target=/var/lib/jeedom-external`, '--mount', `type=bind,source=${testDir},target=/tests,readonly`, '--tmpfs', '/var/lib/mysql:rw,size=16m,mode=0700'];
const environment = ['-e', `DB_HOST=${dbName}`, '-e', 'DB_PORT=3306', '-e', 'DB_NAME=jeedom_runtime', '-e', 'DB_USERNAME=runtime_user', '-e', 'DB_PASSWORD=local-runtime-only', '-e', 'DB_WAIT_TIMEOUT=30', '-e', 'TZ=Europe/Kyiv'];
let stage = 'setup';
let failed = false;
try {
  await docker(['image', 'tag', options.runtimeImage, imageAlias]);
  aliasCreated = true;
  assert.equal((await docker(['image', 'inspect', '--format', '{{.Id}}', imageAlias])).stdout, runtimeImageId, 'Runtime tag changed during test setup; retry after building finishes');
  await docker(['network', 'create', '--internal', network]);
  networkCreated = true;
  createdContainers.add(dbName);
  await docker(['run', '-d', '--name', dbName, '--network', network, '--tmpfs', '/var/lib/mysql:rw,size=768m', '-e', 'MYSQL_ROOT_PASSWORD=local-runtime-root-only', '-e', 'MYSQL_DATABASE=jeedom_runtime', '-e', 'MYSQL_USER=runtime_user', '-e', 'MYSQL_PASSWORD=local-runtime-only', options.mysqlImage]);
  async function once(mode, allowed = [0]) {
    const name = `jeedom-runtime-once-${suffix}-${mode}-${randomUUID().slice(0, 4)}`;
    createdContainers.add(name);
    return docker(['run', '--rm', '--name', name, '--network', network, ...mountArgs, ...environment, imageAlias, mode], allowed, mode === 'bootstrap' ? 600000 : mode === 'seed' ? 180000 : 60000);
  }
  async function freshProbe(mode = 'snapshot') {
    return docker(['run', '--rm', '--network', network, ...mountArgs, ...environment, '--entrypoint', 'php', imageAlias, '/tests/runtime-probe.php', mode]);
  }
  stage = 'seed';
  console.error('Runtime smoke: seed pinned HTML into fresh local bind storage.');
  if (!ownership.seeded) {
    await once('seed');
    ownership.seeded = true;
    await writeFile(ownershipPath, JSON.stringify(ownership));
  }
  const refusedSeed = await once('seed', [1]);
  assert.match(refusedSeed.stderr, /HTML directory is not empty/);
  stage = 'uninitialized start guard';
  const refusedStart = await once('start', [1]);
  assert.match(refusedStart.stderr, /DB is not initialized/);
  const empty = JSON.parse((await freshProbe()).stdout);
  assert.equal(empty.tables, 0, 'Normal start initialized an empty DB without explicit authorization');
  stage = 'bootstrap';
  console.error('Runtime smoke: explicit empty-schema bootstrap.');
  const bootstrap = await once('bootstrap');
  assert.match(bootstrap.stdout, /\[END INSTALL SUCCESS\]/, 'Upstream installer did not report successful completion');
  const initialized = JSON.parse((await freshProbe()).stdout);
  assert.equal(initialized.tables, 31, 'Explicit bootstrap did not initialize the actual 31-table Jeedom schema');
  assert(initialized.user_count >= 1, 'Bootstrap did not create a Jeedom user');
  stage = 'populated bootstrap guard';
  const refusedBootstrap = await once('bootstrap', [1]);
  assert.match(refusedBootstrap.stderr, /destination schema already contains tables/);
  assert.deepEqual(JSON.parse((await freshProbe()).stdout), initialized, 'Refused bootstrap changed existing application data');
  stage = 'start';
  createdContainers.add(appName);
  console.error('Runtime smoke: full HTTP/services health and restart persistence.');
  await docker(['run', '-d', '--name', appName, '--network', network, ...mountArgs, ...environment, imageAlias, 'start']);
  async function ready() {
    let last;
    for (let attempt = 0; attempt < 30; attempt++) {
      last = await docker(['exec', appName, 'php', '-d', 'opcache.enable_cli=0', '/opt/jeedom-external/healthcheck.php'], [0, 1]);
      const state = JSON.parse((await docker(['inspect', '--format', '{{json .State}}', appName])).stdout);
      if (!state.Running) throw new Error(`Runtime stopped before health readiness, exit ${state.ExitCode}`);
      if (last.code === 0 && state.Health?.Status === 'healthy') return last.stdout;
      await new Promise(resolve => setTimeout(resolve, 1500));
    }
    throw new Error(`Health did not become ready: ${last.stderr}`);
  }
  const health = await ready();
  const http = (await docker(['exec', '--user', 'www-data', appName, 'php', '/tests/runtime-probe.php', 'http'])).stdout;
  await docker(['exec', appName, 'php', '/tests/runtime-probe.php', 'marker']);
  const before = JSON.parse((await docker(['exec', appName, 'php', '/tests/runtime-probe.php'])).stdout);
  assert.equal(before.local_sql_binary, false);
  assert.equal(before.local_sql_process, false);
  stage = 'graceful stop';
  await docker(['stop', '--time', '20', appName]);
  const stopped = JSON.parse((await docker(['inspect', '--format', '{{json .State}}', appName])).stdout);
  assert.equal(stopped.ExitCode, 0, 'SIGTERM did not produce a graceful clean shutdown');
  stage = 'restart';
  await docker(['start', appName]);
  await ready();
  const after = JSON.parse((await docker(['exec', appName, 'php', '/tests/runtime-probe.php'])).stdout);
  assert.deepEqual(after, before, 'Core configuration or application data fingerprints changed over restart');
  await docker(['exec', '--user', 'www-data', appName, 'php', '/tests/runtime-probe.php', 'http']);
  await docker(['stop', '--time', '20', appName]);
  assert.equal(JSON.parse((await docker(['inspect', '--format', '{{json .State}}', appName])).stdout).ExitCode, 0);
  console.log(JSON.stringify({ runtime_image: options.runtimeImage, runtime_image_id: runtimeImageId, seed_empty_guard: true, uninitialized_start_refused: true, explicit_bootstrap_tables: initialized.tables, populated_bootstrap_refused_unchanged: true, health, http, no_local_sql_server: true, graceful_stop_exit_code: stopped.ExitCode, restart_data_unchanged: true, user_count: before.user_count, source_core_sha256: before.core_sha256, scope: 'Fresh local bind storage and internal Docker network only; no NAS data or credentials, no host ports' }, null, 2));
} catch (error) {
  failed = true;
  console.error(`Runtime smoke failed at: ${stage}`);
  if (networkCreated) {
    const probe = await docker(['run', '--rm', '--network', network, ...mountArgs, ...environment, '--entrypoint', 'php', imageAlias, '/tests/runtime-probe.php'], [0, 1]);
    if (probe.code === 0) console.error(`Safe failed-fixture counts/fingerprints: ${probe.stdout}`);
  }
  if (createdContainers.has(appName)) {
    const logs = await docker(['logs', '--tail', '35', appName], [0, 1]);
    console.error(logs.stdout, logs.stderr);
  }
  throw error;
} finally {
  for (const name of createdContainers) await docker(['rm', '-f', '-v', name], [0, 1]);
  if (networkCreated) await docker(['network', 'rm', network]);
  if (aliasCreated) await docker(['image', 'rm', imageAlias]);
  assert(path.dirname(temporary) === os.tmpdir() && path.basename(temporary).startsWith('jeedom-runtime-smoke-'), 'Invalid local temporary cleanup target');
  if (failed && options.keepOnFailure === 'true') console.error(`Preserved owned local fixture for reuse: ${temporary}`);
  else await rm(temporary, { recursive: true, force: true });
}
