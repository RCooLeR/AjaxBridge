<?php
// AjaxBridge installer regression, MIT. Run only in a disposable Linux container.
// All fixtures are synthetic; no Jeedom core, database, credentials or network.
$effectiveUid = function_exists('posix_geteuid') ? (string) posix_geteuid() : trim((string) shell_exec('id -u'));
if (PHP_SAPI !== 'cli' || $effectiveUid !== '0') {
    fwrite(STDERR, "Run as root in a disposable Linux container with a writable /tmp.\n");
    exit(2);
}
$package = realpath(__DIR__ . '/../Jeedom');
$fixtureRoot = '/tmp/ajaxbridge-install-test-' . bin2hex(random_bytes(8));
mkdir($fixtureRoot, 0700);

function copyTree($source, $target) {
    if (!is_dir($target)) { mkdir($target, 0755, true); }
    foreach (new DirectoryIterator($source) as $entry) {
        if ($entry->isDot()) { continue; }
        $destination = $target . '/' . $entry->getFilename();
        if ($entry->isDir()) { copyTree($entry->getPathname(), $destination); }
        else { copy($entry->getPathname(), $destination); }
    }
}
function snapshotTree($root) {
    $files = array();
    foreach (new RecursiveIteratorIterator(new RecursiveDirectoryIterator($root, FilesystemIterator::SKIP_DOTS)) as $file) {
        if ($file->isFile()) {
            $files[substr($file->getPathname(), strlen($root) + 1)] = hash_file('sha256', $file->getPathname());
        }
    }
    ksort($files);
    return $files;
}
function check($condition, $message) {
    if (!$condition) { throw new RuntimeException($message); }
}
function runHelper($package, $mode, $plugin, $backup, $expectSuccess) {
    $parts = array('bash', $package . '/tools/install-patch.sh', $mode, $package, $plugin, $backup);
    exec(implode(' ', array_map('escapeshellarg', $parts)) . ' 2>&1', $output, $status);
    check(($status === 0) === $expectSuccess, 'Unexpected installer status: ' . implode("\n", $output));
}
try {
    $plugin = $fixtureRoot . '/plugins/ajaxSystem';
    copyTree($package . '/files', $plugin);
    file_put_contents($plugin . '/plugin_info/info.json', json_encode(array('id' => 'ajaxSystem', 'fixture' => true)));
    file_put_contents($plugin . '/core/config/devices/Relay.json', json_encode(array('commands' => array(
        array('name' => 'State', 'logicalId' => 'realState', 'type' => 'info', 'subtype' => 'binary', 'isVisible' => 1, 'isHistorized' => 1)
    ))));
    file_put_contents($plugin . '/unmodified.txt', 'Preserve this official-plugin fixture file');
    // Model an installed baseline where new overlay templates do not exist yet.
    $manifest = json_decode(file_get_contents($package . '/manifest.json'), true);
    foreach ($manifest['files'] as $entry) {
        if ($entry['kind'] === 'added' && file_exists($plugin . '/' . $entry['path'])) {
            unlink($plugin . '/' . $entry['path']);
        }
    }
    $before = snapshotTree($plugin);
    $backup = $fixtureRoot . '/backups';
    runHelper($package, '--check', $plugin, $backup, true);
    check(snapshotTree($plugin) === $before && !file_exists($backup), '--check modified the original plugin or created a backup');

    $class = $plugin . '/core/class/ajaxSystem.class.php';
    $originalClass = file_get_contents($class);
    file_put_contents($class, $originalClass . "\n// Unknown local modification\n");
    $modified = snapshotTree($plugin);
    runHelper($package, '--apply', $plugin, $backup, false);
    check(snapshotTree($plugin) === $modified && !file_exists($backup), 'Unknown baseline was modified');
    file_put_contents($class, $originalClass);

    symlink($class, $plugin . '/linked-file.php');
    runHelper($package, '--apply', $plugin, $backup, false);
    check(!file_exists($backup) && is_link($plugin . '/linked-file.php'), 'Symlink guard failed');
    unlink($plugin . '/linked-file.php');

    runHelper($package, '--apply', $plugin, $backup, true);
    $archives = glob($backup . '/ajaxSystem-original-*.tar.gz');
    check(count($archives) === 1, 'Missing original-plugin backup');
    mkdir($fixtureRoot . '/restored');
    exec('tar -xzf ' . escapeshellarg($archives[0]) . ' -C ' . escapeshellarg($fixtureRoot . '/restored'), $unused, $status);
    check($status === 0 && snapshotTree($fixtureRoot . '/restored') === $before, 'Backup does not restore exact original files');
    check(file_get_contents($plugin . '/unmodified.txt') === 'Preserve this official-plugin fixture file', 'Unlisted original plugin file changed');
    foreach (file($package . '/SHA256SUMS', FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) as $line) {
        list($digest, $relative) = explode('  ', $line, 2);
        if (strpos($relative, 'files/') === 0) {
            check(hash_file('sha256', $plugin . '/' . substr($relative, 6)) === $digest, 'Installed overlay checksum differs');
        }
    }
    echo "PASS: read-only check, baseline/symlink guards, exact backup, scoped apply and unchanged original files; offline parser validation passed.\n";
} catch (Throwable $error) {
    fwrite(STDERR, $error->getMessage() . "\n");
    exit(1);
}
// Fixtures live only inside the disposable container's /tmp mount.
