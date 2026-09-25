import { createHash } from 'node:crypto';
import { lstat, mkdir, mkdtemp, open, readdir, rename, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const ENTRY = 'ajaxbridge-lovelace.js';
const MANIFEST = 'release.json';
const VERSION = 1;
const DIGEST = /^[a-f0-9]{64}$/;

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex');
}

function releaseId(files) {
  return sha256(`ajaxbridge-dashboard-release-v${VERSION}\n${JSON.stringify(files)}`);
}

function checkRelativePath(value) {
  if (typeof value !== 'string' || !value || value.includes('\\') || path.posix.isAbsolute(value)) {
    throw new Error(`Unsafe release path: ${JSON.stringify(value)}`);
  }
  for (const segment of value.split('/')) {
    if (!segment || segment === '.' || segment === '..' || /[<>:"|?*\x00-\x1f\x7f]/.test(segment)
      || /[. ]$/.test(segment) || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(segment)) {
      throw new Error(`Unsafe release path: ${JSON.stringify(value)}`);
    }
  }
  if (value.toLowerCase() === MANIFEST) {
    throw new Error(`${MANIFEST} is reserved for release metadata`);
  }
}

function hasExactKeys(value, keys) {
  return value && typeof value === 'object' && !Array.isArray(value)
    && Object.keys(value).sort().join('\0') === [...keys].sort().join('\0');
}

function validateManifest(manifest) {
  if (!hasExactKeys(manifest, ['version', 'id', 'entry', 'files']) || manifest.version !== VERSION
    || !DIGEST.test(manifest.id) || manifest.entry !== ENTRY || !Array.isArray(manifest.files) || !manifest.files.length) {
    throw new Error('Invalid release manifest');
  }
  const seen = new Set();
  let previous = '';
  const files = manifest.files.map((file) => {
    if (!hasExactKeys(file, ['path', 'size', 'sha256']) || !Number.isSafeInteger(file.size)
      || file.size < 0 || !DIGEST.test(file.sha256)) {
      throw new Error('Invalid release file metadata');
    }
    checkRelativePath(file.path);
    const folded = file.path.toLowerCase();
    if (seen.has(folded) || (previous && previous >= file.path)) {
      throw new Error('Release paths must be unique and sorted');
    }
    seen.add(folded);
    previous = file.path;
    return { path: file.path, size: file.size, sha256: file.sha256 };
  });
  for (const file of files) {
    const segments = file.path.toLowerCase().split('/');
    while (segments.length > 1) {
      segments.pop();
      if (seen.has(segments.join('/'))) {
        throw new Error('A release path is both a file and a directory');
      }
    }
  }
  if (!files.some((file) => file.path === ENTRY) || releaseId(files) !== manifest.id) {
    throw new Error('Release manifest content digest does not match its id');
  }
  return { version: VERSION, id: manifest.id, entry: ENTRY, files };
}

// Check every existing path component, including parents of the supplied root.
// This rejects directory symlinks and Windows junctions as well as file links.
async function rejectSymlinks(target, allowMissing = false) {
  const absolute = path.resolve(target);
  const root = path.parse(absolute).root;
  let current = root;
  for (const segment of absolute.slice(root.length).split(path.sep).filter(Boolean)) {
    current = path.join(current, segment);
    let info;
    try {
      info = await lstat(current);
    } catch (error) {
      if (allowMissing && error.code === 'ENOENT') return;
      throw error;
    }
    if (info.isSymbolicLink()) throw new Error(`Symlinks are not allowed: ${current}`);
    if (current !== absolute && !info.isDirectory()) throw new Error(`Not a directory: ${current}`);
  }
}

async function readRegularFile(filePath) {
  await rejectSymlinks(filePath);
  const before = await lstat(filePath);
  if (!before.isFile()) throw new Error(`Not a regular file: ${filePath}`);
  const handle = await open(filePath, 'r');
  try {
    const opened = await handle.stat();
    if (!opened.isFile() || before.ino !== opened.ino || before.dev !== opened.dev) {
      throw new Error(`File changed while opening: ${filePath}`);
    }
    const bytes = await handle.readFile();
    const after = await handle.stat();
    await rejectSymlinks(filePath);
    if (after.size !== opened.size || after.mtimeMs !== opened.mtimeMs) {
      throw new Error(`File changed while reading: ${filePath}`);
    }
    return bytes;
  } finally {
    await handle.close();
  }
}

async function readTree(root, allowManifest = false) {
  await rejectSymlinks(root);
  if (!(await lstat(root)).isDirectory()) throw new Error(`Not a directory: ${root}`);
  const files = [];
  async function visit(directory, prefix = '') {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      const target = path.join(directory, entry.name);
      if (entry.isSymbolicLink()) throw new Error(`Symlinks are not allowed: ${target}`);
      if (allowManifest && relative === MANIFEST && entry.isFile()) continue;
      checkRelativePath(relative);
      if (entry.isDirectory()) {
        await rejectSymlinks(target);
        await visit(target, relative);
      } else if (entry.isFile()) {
        const bytes = await readRegularFile(target);
        files.push({ path: relative, size: bytes.length, sha256: sha256(bytes), bytes });
      } else {
        throw new Error(`Not a regular file or directory: ${target}`);
      }
    }
  }
  await visit(root);
  files.sort((left, right) => left.path < right.path ? -1 : left.path > right.path ? 1 : 0);
  return files;
}

async function readManifest(manifestPath) {
  if (path.basename(manifestPath) !== MANIFEST) throw new Error(`Expected a ${MANIFEST} path`);
  const manifest = validateManifest(JSON.parse((await readRegularFile(manifestPath)).toString('utf8')));
  if (path.basename(path.dirname(path.resolve(manifestPath))) !== manifest.id) {
    throw new Error('Release directory name does not match manifest id');
  }
  return manifest;
}

async function checkExistingRelease(directory, expected) {
  const manifest = await readManifest(path.join(directory, MANIFEST));
  if (JSON.stringify(manifest) !== JSON.stringify(expected)) throw new Error('Existing release manifest differs');
  const actual = (await readTree(directory, true)).map(({ bytes: _bytes, ...file }) => file);
  if (JSON.stringify(actual) !== JSON.stringify(expected.files)) {
    throw new Error('Existing release files differ; immutable releases cannot be overwritten');
  }
}

function isWithin(parent, child) {
  const relative = path.relative(parent, child);
  return relative === '' || (!relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative));
}

/** Copy a complete build into out/<content-id> without overwriting releases. */
export async function prepareRelease({ dist, out }) {
  const source = path.resolve(dist);
  const output = path.resolve(out);
  if (isWithin(source, output)) throw new Error('Release output must be outside the build directory');
  await rejectSymlinks(output, true);
  const sourceFiles = await readTree(source);
  const files = sourceFiles.map(({ bytes: _bytes, ...file }) => file);
  const manifest = validateManifest({ version: VERSION, id: releaseId(files), entry: ENTRY, files });
  await mkdir(output, { recursive: true });
  await rejectSymlinks(output);
  const releaseDir = path.join(output, manifest.id);
  const result = {
    id: manifest.id,
    releaseDir,
    manifestPath: path.join(releaseDir, MANIFEST),
    resourcePath: `${manifest.id}/${ENTRY}`,
  };
  try {
    await lstat(releaseDir);
    await checkExistingRelease(releaseDir, manifest);
    return result;
  } catch (error) {
    // Only a missing destination permits creation; missing/corrupt files in an
    // existing release must fail rather than being silently replaced.
    if (error.code !== 'ENOENT' || error.path !== releaseDir) throw error;
  }

  let staging = await mkdtemp(path.join(output, '.release-'));
  try {
    for (const file of sourceFiles) {
      const target = path.join(staging, ...file.path.split('/'));
      await mkdir(path.dirname(target), { recursive: true });
      await writeFile(target, file.bytes, { flag: 'wx' });
    }
    await writeFile(path.join(staging, MANIFEST), `${JSON.stringify(manifest, null, 2)}\n`, { flag: 'wx' });
    await rejectSymlinks(output);
    try {
      await rename(staging, releaseDir);
      staging = null;
    } catch (error) {
      if (!['EEXIST', 'ENOTEMPTY', 'EPERM'].includes(error.code)) throw error;
      await checkExistingRelease(releaseDir, manifest);
    }
    return result;
  } finally {
    // Cleanup is restricted to this invocation's mkdtemp directory. Existing
    // release directories are never removed, including failed verification.
    if (staging && path.dirname(staging) === output && path.basename(staging).startsWith('.release-')) {
      await rm(staging, { recursive: true, force: true });
    }
  }
}

function releaseBaseUrl(baseUrl) {
  const base = new URL(baseUrl);
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash) {
    throw new Error('Base URL must be HTTP(S) without credentials, query, or fragment');
  }
  base.pathname = `${base.pathname.replace(/\/+$/, '')}/`;
  return base;
}

function fileUrl(base, id, file) {
  return new URL(`${id}/${file.split('/').map(encodeURIComponent).join('/')}`, base).href;
}

async function verifyServedFile(url, file) {
  const response = await fetch(url, { redirect: 'manual', cache: 'no-store', signal: AbortSignal.timeout(30_000) });
  const javascript = /\.(?:[cm]?js)$/i.test(file.path);
  if (response.status !== 200 || response.redirected) {
    await response.body?.cancel();
    throw new Error(`HTTP ${response.status} for ${file.path}; expected 200 without redirects`);
  }
  const mime = (response.headers.get('content-type') ?? '').split(';', 1)[0].trim().toLowerCase();
  if (javascript && !/^(?:text|application)\/(?:javascript|ecmascript)$/.test(mime)) {
    await response.body?.cancel();
    throw new Error(`Invalid JavaScript MIME type for ${file.path}: ${mime || '(missing)'}`);
  }
  const hash = createHash('sha256');
  let size = 0;
  let prefix = Buffer.alloc(0);
  if (response.body) {
    for await (const chunk of response.body) {
      size += chunk.length;
      if (size > file.size) throw new Error(`Served size mismatch for ${file.path}`);
      hash.update(chunk);
      if (javascript && prefix.length < 1024) {
        prefix = Buffer.concat([prefix, chunk.subarray(0, 1024 - prefix.length)]);
      }
    }
  }
  if (javascript && /^\s*(?:<!--[\s\S]*?-->\s*)*<(?:!doctype\s+html\b|html\b|head\b|body\b)/i.test(prefix.toString('utf8'))) {
    throw new Error(`HTML was served as JavaScript for ${file.path}`);
  }
  if (size !== file.size || hash.digest('hex') !== file.sha256) {
    throw new Error(`Served content mismatch for ${file.path}`);
  }
}

/** Return a usable resource URL only after every manifest file is verified. */
export async function verifyRelease({ manifestPath, baseUrl }) {
  const manifest = await readManifest(path.resolve(manifestPath));
  const base = releaseBaseUrl(baseUrl);
  for (let index = 0; index < manifest.files.length; index += 4) {
    await Promise.all(manifest.files.slice(index, index + 4).map((file) => verifyServedFile(fileUrl(base, manifest.id, file.path), file)));
  }
  return { id: manifest.id, verifiedFiles: manifest.files.length, resourceURL: fileUrl(base, manifest.id, manifest.entry) };
}

const usage = `Usage:
  node scripts/dashboard-release.mjs prepare --dist dist --out <ha-www>/ajax/releases
  node scripts/dashboard-release.mjs verify --manifest <release-dir>/release.json --base-url https://ha.example/local/ajax/releases

prepare stages an immutable release. verify checks all served bytes and prints
resourceURL only on success. Neither command activates a Home Assistant resource.
`;

async function main(args) {
  if (args.length === 1 && ['--help', '-h'].includes(args[0])) {
    process.stdout.write(usage);
    return;
  }
  const [command, ...rest] = args;
  const expected = command === 'prepare' ? ['--dist', '--out'] : command === 'verify' ? ['--manifest', '--base-url'] : [];
  const options = new Map();
  for (let index = 0; index < rest.length; index += 2) {
    const name = rest[index];
    const value = rest[index + 1];
    if (!expected.includes(name) || options.has(name) || !value || value.startsWith('--')) throw new Error(usage);
    options.set(name, value);
  }
  if (expected.length !== 2 || options.size !== 2) throw new Error(usage);
  const result = command === 'prepare'
    ? await prepareRelease({ dist: options.get('--dist'), out: options.get('--out') })
    : await verifyRelease({ manifestPath: options.get('--manifest'), baseUrl: options.get('--base-url') });
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`dashboard-release: ${error.message}\n`);
    process.exitCode = 1;
  });
}
