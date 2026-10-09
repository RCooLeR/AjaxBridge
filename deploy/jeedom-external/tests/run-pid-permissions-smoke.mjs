#!/usr/bin/env node
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { readFile, writeFile, mkdir, mkdtemp, rm } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';

const tests = path.dirname(fileURLToPath(import.meta.url));
const image = process.argv[2] ?? 'ajaxbridge-jeedom-external:mysql84-test';

async function docker(args, allowed = [0]) {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', ['--context', 'desktop-linux', ...args], { stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '';
    let err = '';
    child.stdout.on('data', data => { out += data; });
    child.stderr.on('data', data => { err += data; });
    const timeout = setTimeout(() => child.kill(), 30000);
    child.on('error', error => { clearTimeout(timeout); reject(error); });
    child.on('close', code => {
      clearTimeout(timeout);
      if (!allowed.includes(code)) return reject(new Error('Docker ' + args[0] + ' failed: ' + err));
      resolve({ code, out: out.trim(), err: err.trim() });
    });
  });
}

const endpoint = JSON.parse((await docker(['context', 'inspect', 'desktop-linux'])).out)[0].Endpoints.docker.Host;
assert(/^(?:npipe:\/\/|unix:\/\/)/.test(endpoint), 'Local Docker only');
const entrypoint = (await readFile(path.join(tests, '../entrypoint.sh'), 'utf8')).replaceAll('\r\n', '\n');
assert.equal((entrypoint.match(/^umask 0022$/gm) ?? []).length, 1, 'Expected the exact service umask split');
assert(entrypoint.indexOf('umask 0022') > entrypoint.indexOf('php_runtime "$runtime/compat.php" --root "$html"'), 'Relaxation must follow protected configuration');
assert(entrypoint.indexOf('umask 0022') < entrypoint.indexOf('service atd start'), 'Relaxation must precede service launch');

const temp = await mkdtemp(path.join(os.tmpdir(), 'jeedom-pid-smoke-'));
await mkdir(path.join(temp, 'stubs'));
const stubs = {
  php: '#!/bin/sh\nset -e\ncase "$*" in *configure.php*configure*) node /tests/pid-permissions-observer.mjs configure;; esac\nexit 0\n',
  service: '#!/bin/sh\nnode /tests/pid-permissions-observer.mjs service "$1" "$2"\n',
  cron: '#!/bin/sh\nexec node /tests/pid-permissions-observer.mjs cron\n',
  pgrep: '#!/bin/sh\nexit 0\n',
};
for (const [name, source] of Object.entries(stubs)) {
  await writeFile(path.join(temp, 'stubs', name), source, { mode: 0o755 });
}

const reports = [];
const names = [];
try {
  const variants = [
    ['negative_0077', entrypoint.replace(/^umask 0022\n/m, '')],
    ['positive_split', entrypoint],
  ];
  for (const [variant, source] of variants) {
    await writeFile(path.join(temp, variant + '.sh'), source, { mode: 0o755 });
    const name = 'jeedom-pid-smoke-' + randomUUID().slice(0, 8);
    names.push(name);
    await docker([
      'run', '-d', '--name', name, '--network', 'none', '--no-healthcheck',
      '--tmpfs', '/var/lib/mysql:rw,size=1m',
      '--mount', 'type=bind,source=' + temp + ',target=/fixture,readonly',
      '--mount', 'type=bind,source=' + tests + ',target=/tests,readonly',
      '-e', 'PATH=/fixture/stubs:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
      '--entrypoint', 'bash', image, '/fixture/' + variant + '.sh', 'start',
    ]);
    let ready;
    for (let attempt = 0; attempt < 20; attempt++) {
      ready = await docker(['exec', name, 'test', '-f', '/tmp/probe-cron.json'], [0, 1]);
      if (ready.code === 0) break;
      await new Promise(resolve => setTimeout(resolve, 200));
    }
    assert.equal(ready.code, 0, 'Actual entrypoint did not reach stubbed services');

    const root = JSON.parse((await docker(['exec', name, 'node', '/tests/pid-permissions-observer.mjs', 'inspect'])).out);
    const php = '$p="/tmp/jeedom/mqtt2/deamon.pid"; $r=is_readable($p); $id=$r?(int)file_get_contents($p):null; echo json_encode(["readable"=>$r,"live_root_session"=>$r&&posix_getsid($id)!==false]);';
    const rootProcess = JSON.parse((await docker(['exec', name, '/usr/bin/php', '-r', php])).out);
    const www = JSON.parse((await docker(['exec', '--user', 'www-data', name, '/usr/bin/php', '-r', php])).out);
    const mask = variant === 'negative_0077' ? '77' : '22';
    for (const service of Object.values(root.services)) {
      assert.equal(service.umask, mask, 'Service launch did not inherit the actual entrypoint umask');
    }
    assert.equal(rootProcess.live_root_session, true, 'Root daemon exited before the permission test');
    assert.equal(root.pid.uid, 0);
    assert.equal(root.pid.gid, 0);
    assert.equal(root.pid.mode, mask === '77' ? '600' : '644');
    assert.equal(www.readable, mask === '22');
    assert.equal(www.live_root_session, mask === '22');
    assert.equal(root.private_dir.mode, '700');
    assert.equal(root.secret.mode, '600');
    reports.push({ variant, ...root, root_process_alive: rootProcess.live_root_session, www_data: www });
  }
  console.log(JSON.stringify({
    status: 'PASS',
    entrypoint: 'Actual source with PHP/service/cron stubs; negative removes only service0022 line',
    network: 'none',
    results: reports,
    scope: 'No database, secrets, NAS files, plugin commands or live PID permissions accessed',
  }, null, 2));
} finally {
  for (const name of names) await docker(['rm', '-f', '-v', name], [0, 1]);
  assert(path.dirname(temp) === os.tmpdir() && path.basename(temp).startsWith('jeedom-pid-smoke-'));
  await rm(temp, { recursive: true, force: true });
}
