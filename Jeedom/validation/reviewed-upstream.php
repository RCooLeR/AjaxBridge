<?php
// Offline checksum-contract regression. Reads only two public stock templates.
// Run in a disposable Linux container: php reviewed-upstream.php PLUGIN_ROOT.
if (PHP_SAPI !== 'cli' || $argc !== 2) { exit("Use reviewed-upstream.php PLUGIN_ROOT\n"); }
$package = dirname(__DIR__);
$sourceRoot = realpath($argv[1]);
if (!$sourceRoot) { throw new RuntimeException('Stock source directory is missing'); }
$manifest = json_decode(file_get_contents($package . '/manifest.json'), true);
$stock = array();
foreach ($manifest['files'] as $entry) {
    if (!isset($entry['reviewed_upstream_sha256_lf'])) { continue; }
    $bytes = file_get_contents($sourceRoot . '/' . $entry['path']);
    if ($bytes === false || !in_array(hash('sha256', str_replace("\r\n", "\n", $bytes)), (array) $entry['reviewed_upstream_sha256_lf'], true)) {
        throw new RuntimeException('Source is not the reviewed stock template: ' . $entry['path']);
    }
    $stock[$entry['path']] = $bytes;
}
if (count($stock) !== 2) { throw new RuntimeException('Expected two exact reviewed stock templates'); }
$fixture = '/tmp/jeedom-reviewed-upstream-' . bin2hex(random_bytes(8));
mkdir($fixture, 0700);
function copyOverlay($source, $target) {
    if (!is_dir($target)) { mkdir($target, 0700, true); }
    foreach (new DirectoryIterator($source) as $entry) {
        if ($entry->isDot()) { continue; }
        if ($entry->isDir()) { copyOverlay($entry->getPathname(), $target . '/' . $entry->getFilename()); }
        else { copy($entry->getPathname(), $target . '/' . $entry->getFilename()); }
    }
}
copyOverlay($package . '/files', $fixture);
file_put_contents($fixture . '/plugin_info/info.json', '{"id":"ajaxSystem"}');
foreach ($stock as $path => $bytes) { file_put_contents($fixture . '/' . $path, $bytes); }
function checkVerifier($package, $fixture, $expected, $mode = '--base') {
    $command = escapeshellarg(PHP_BINARY) . ' -d opcache.enable_cli=0 ' . escapeshellarg($package . '/verify.php') . ' ' . $mode . ' ' . escapeshellarg($fixture);
    exec($command . ' 2>&1', $output, $status);
    if (($status === 0) !== $expected) { throw new RuntimeException('Unexpected checksum-verifier result'); }
}
checkVerifier($package, $fixture, true);
checkVerifier($package, $fixture, false, '--installed');
foreach ($stock as $path => $bytes) {
    file_put_contents($fixture . '/' . $path, $bytes . ' ');
    checkVerifier($package, $fixture, false);
    file_put_contents($fixture . '/' . $path, str_replace("\n", "\r\n", str_replace("\r\n", "\n", $bytes)));
    checkVerifier($package, $fixture, true);
    file_put_contents($fixture . '/' . $path, $bytes);
}
echo "PASS: exact reviewed stock/LF normalization accepted; mutations and installed-stock mismatch rejected.\n";
