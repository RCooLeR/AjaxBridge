#!/usr/bin/env node
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readFile, readdir, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// Copies only three non-secret Jeedom files; never mounts or changes the supplied HTML tree.
const testDir = path.dirname(fileURLToPath(import.meta.url));
const options = { context: 'desktop-linux', runtimeImage: 'ajaxbridge-jeedom-external:mysql84-test', mysqlImage: 'mysql:8.4' };
for (let i = 2; i < process.argv.length; i += 2) {
  const name = process.argv[i];
  const value = process.argv[i + 1];
  if (!value || !['--source-dir', '--context', '--runtime-image', '--mysql-image', '--patcher-dir'].includes(name)) {
    throw new Error('Usage: node run-mysql84-smoke.mjs --source-dir <Jeedom HTML or three-file snapshot> [--context desktop-linux] [--runtime-image image] [--mysql-image mysql:8.4] [--patcher-dir directory]');
  }
  options[{ '--source-dir': 'sourceDir', '--context': 'context', '--runtime-image': 'runtimeImage', '--mysql-image': 'mysqlImage', '--patcher-dir': 'patcherDir' }[name]] = value;
}
assert(options.sourceDir, '--source-dir is required; production credentials are never read');
const dockerPrefix = ['--context', options.context];
async function docker(args, allowedCodes = [0]) {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', [...dockerPrefix, ...args], { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', data => { stdout += data; });
    child.stderr.on('data', data => { stderr += data; });
    const timeout = setTimeout(() => child.kill(), 60000);
    child.on('error', error => { clearTimeout(timeout); reject(error); });
    child.on('close', code => {
      clearTimeout(timeout);
      if (!allowedCodes.includes(code)) return reject(new Error(`Docker ${args[0]} exited ${code}: ${stderr || stdout}`));
      resolve({ code, stdout: stdout.trim(), stderr: stderr.trim() });
    });
  });
}
const endpoint = JSON.parse((await docker(['context', 'inspect', options.context])).stdout)[0].Endpoints.docker.Host;
assert(/^(?:npipe:\/\/|unix:\/\/)/.test(endpoint), `Refusing non-local Docker endpoint: ${endpoint}`);
const fixtureDir = await mkdtemp(path.join(os.tmpdir(), 'jeedom-mysql84-smoke-'));
const htmlDir = path.join(fixtureDir, 'html');
const runtimeDir = path.join(fixtureDir, 'runtime');
const mysqlName = `jeedom-mysql84-smoke-${randomUUID().slice(0, 8)}`;
let mysqlId;
try {
  await mkdir(runtimeDir);
  for (const [relative, flat] of [['core/class/DB.class.php', 'DB.class.php'], ['core/class/history.class.php', 'history.class.php'], ['install/database.json', 'database.json']]) {
    const destination = path.join(htmlDir, relative);
    await mkdir(path.dirname(destination), { recursive: true });
    try { await copyFile(path.join(options.sourceDir, relative), destination); }
    catch (error) {
      if (error.code !== 'ENOENT') throw error;
      await copyFile(path.join(options.sourceDir, flat), destination);
    }
  }
  const originalDB = await readFile(path.join(htmlDir, 'core/class/DB.class.php'));
  const originalHistory = await readFile(path.join(htmlDir, 'core/class/history.class.php'));
  const originalSchema = await readFile(path.join(htmlDir, 'install/database.json'));
  const mountArgs = [
    '--mount', `type=bind,source=${htmlDir},target=/fixture`,
    '--mount', `type=bind,source=${testDir},target=/tests,readonly`,
    '--mount', `type=bind,source=${runtimeDir},target=/runtime`,
    '-e', 'JEEDOM_RUNTIME_DIR=/runtime/backups',
  ];
  if (options.patcherDir) mountArgs.push('--mount', `type=bind,source=${path.resolve(options.patcherDir)},target=/opt/jeedom-external,readonly`);
  async function patcher(extra = [], allowedCodes = [0]) {
    return docker(['run', '--rm', ...mountArgs, '--entrypoint', 'php', options.runtimeImage, '/opt/jeedom-external/compat.php', '--root', '/fixture', ...extra], allowedCodes);
  }
  const beforeCheck = await patcher(['--check'], [1]);
  assert.equal(beforeCheck.code, 1, 'Unpatched Jeedom should not pass compatibility --check');
  assert.deepEqual(await readFile(path.join(htmlDir, 'core/class/DB.class.php')), originalDB, '--check mutated the source');
  const firstPatch = await patcher();
  const patchedDB = await readFile(path.join(htmlDir, 'core/class/DB.class.php'));
  assert.notDeepEqual(patchedDB, originalDB, 'Patcher did not alter incompatible Jeedom DB code');
  const backupsBefore = await readdir(path.join(runtimeDir, 'backups'), { recursive: true });
  assert(backupsBefore.length > 0, 'Patcher did not preserve an original outside the webroot');
  assert.deepEqual(await readFile(path.join(runtimeDir, 'backups', backupsBefore[0])), originalDB, 'Patcher backup is not byte-identical to the original');
  await patcher(['--check']);
  await patcher();
  assert.deepEqual(await readFile(path.join(htmlDir, 'core/class/DB.class.php')), patchedDB, 'Patcher is not idempotent');
  assert.deepEqual(await readdir(path.join(runtimeDir, 'backups'), { recursive: true }), backupsBefore, 'Idempotent patch added duplicate backups');
  assert.deepEqual(await readFile(path.join(htmlDir, 'core/class/history.class.php')), originalHistory, 'Compatibility patch rewrote history semantics');
  assert.deepEqual(await readFile(path.join(htmlDir, 'install/database.json')), originalSchema, 'Compatibility patch changed schema definitions');
  mysqlId = (await docker(['run', '-d', '--name', mysqlName, '--tmpfs', '/var/lib/mysql:rw,size=768m', '-e', 'MYSQL_ROOT_PASSWORD=local-smoke-only', '-e', 'MYSQL_ROOT_HOST=%', '-e', 'MYSQL_DATABASE=jeedom_test', '-e', 'MYSQL_USER=jeedom_smoke', '-e', 'MYSQL_PASSWORD=local-smoke-only', options.mysqlImage])).stdout;
  assert(/^[a-f0-9]{64}$/.test(mysqlId), 'Docker did not return the exact disposable database ID');
  const php = await docker(['run', '--rm', '--network', `container:${mysqlId}`, ...mountArgs, '--entrypoint', 'php', options.runtimeImage, '/tests/mysql84-smoke.php']);
  const phpSummary = JSON.parse(php.stdout);
  const cli = await docker(['run', '--rm', '--network', `container:${mysqlId}`, ...mountArgs, '--entrypoint', 'sh', options.runtimeImage, '/tests/mysql84-cli-smoke.sh']);
  console.log(JSON.stringify({
    patcher: { check_is_read_only: true, original_backup: true, idempotent: true, history_unchanged: true, schema_unchanged: true, source_sha256: createHash('sha256').update(originalDB).digest('hex'), patch_output: firstPatch.stdout },
    database: phpSummary,
    cli_backup_restore: cli.stdout,
    scope: 'Disposable local Docker only; supplied Jeedom files copied read-only; no NAS/HA/production DB changes',
  }, null, 2));
} finally {
  if (mysqlId) await docker(['rm', '-f', mysqlId]);
  assert(path.dirname(fixtureDir) === os.tmpdir() && path.basename(fixtureDir).startsWith('jeedom-mysql84-smoke-'), 'Invalid temporary cleanup target');
  await rm(fixtureDir, { recursive: true, force: true });
}
