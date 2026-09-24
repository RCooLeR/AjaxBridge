<?php
// AjaxBridge packaging utility, MIT (../LICENSE). Read-only; no Jeedom bootstrap or network calls.
if (PHP_SAPI !== 'cli') { exit('CLI only'); }

function fail($message) {
    fwrite(STDERR, $message . "\n");
    exit(1);
}
function shaLf($path) {
    $data = file_get_contents($path);
    if ($data === false) { fail('Cannot read: ' . $path); }
    return hash('sha256', str_replace("\r\n", "\n", $data));
}

$mode = $argv[1] ?? '--bundle';
if (!in_array($mode, array('--bundle', '--base', '--installed'), true)
    || ($mode === '--bundle' && $argc !== 1 && $argc !== 2)
    || ($mode !== '--bundle' && $argc !== 3)) {
    fail('Usage: php verify.php [--bundle | --base /path/to/ajaxSystem | --installed /path/to/ajaxSystem]');
}
$sums = file(__DIR__ . '/SHA256SUMS', FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES);
if ($sums === false || count($sums) === 0) { fail('Missing or empty SHA256SUMS'); }
$hashes = array();
foreach ($sums as $line) {
    if (!preg_match('/^([a-f0-9]{64})  ([A-Za-z0-9_.\/\-]+)$/', $line, $match)
        || $match[2][0] === '/' || strpos($match[2], '..') !== false) {
        fail('Invalid SHA256SUMS entry');
    }
    $path = $match[2];
    if (!is_file(__DIR__ . '/' . $path) || hash_file('sha256', __DIR__ . '/' . $path) !== $match[1]) {
        fail('Bundle checksum mismatch: ' . $path);
    }
    $hashes[$path] = $match[1];
}
echo 'Bundle checksums OK: ' . count($hashes) . " files\n";
if ($mode === '--bundle') { exit(0); }

$root = realpath($argv[2]);
if (!$root || !is_file($root . '/plugin_info/info.json')) { fail('Expected a complete installed ajaxSystem directory'); }
$info = json_decode(file_get_contents($root . '/plugin_info/info.json'), true);
if (!is_array($info) || ($info['id'] ?? null) !== 'ajaxSystem') { fail('Target is not the ajaxSystem plugin'); }

$problems = 0;
if ($mode === '--installed') {
    $checked = 0;
    foreach ($hashes as $path => $hash) {
        if (strpos($path, 'files/') !== 0) { continue; }
        $relative = substr($path, strlen('files/'));
        if (!is_file($root . '/' . $relative) || hash_file('sha256', $root . '/' . $relative) !== $hash) {
            fwrite(STDERR, 'MISMATCH ' . $relative . "\n");
            $problems++;
        }
        $checked++;
    }
    if ($problems) { fail('Installed file mismatches: ' . $problems); }
    echo 'Installed overlay checksums OK: ' . $checked . " files\n";
    exit(0);
}

$manifest = json_decode(file_get_contents(__DIR__ . '/manifest.json'), true);
if (!is_array($manifest) || !isset($manifest['files'])) { fail('Invalid manifest'); }
foreach ($manifest['files'] as $entry) {
    $path = $root . '/' . $entry['path'];
    if (!file_exists($path) && $entry['kind'] === 'added') { continue; }
    $accepted = array_filter(array($entry['baseline_sha256_lf'], $entry['source_sha256_lf'], $entry['packaged_sha256']));
    if (!is_file($path) || !in_array(shaLf($path), $accepted, true)) {
        fwrite(STDERR, 'REVIEW ' . $entry['path'] . "\n");
        $problems++;
    }
}
if ($problems) {
    fail('Review required for ' . $problems . ' runtime files. Do not overwrite blindly; see README.md compatibility guidance.');
}
echo "Known baseline/patched files match. This checks the 43 overlay paths, not full Jeedom runtime compatibility.\n";
