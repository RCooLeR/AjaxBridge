<?php
declare(strict_types=1);

// Default: small offline parser-contract fixtures. Use --source-root to repeat
// against public unpatched Jeedom core. Never bootstrap Jeedom or connect to SQL.
$options = getopt('', ['source-root:', 'patcher:']);
$sourceRoot = $options['source-root'] ?? __DIR__ . '/fixtures/mysql84';
$patcher = $options['patcher'] ?? dirname(__DIR__) . '/compat.php';
$db = file_get_contents($sourceRoot . '/core/class/DB.class.php');
$listener = file_get_contents($sourceRoot . '/core/class/listener.class.php');
if ($db === false || $listener === false) { throw new RuntimeException('Public core fixture is missing'); }
$originalGroup = 'FROM listener GROUP BY class, function, event, option';
$fixedGroup = 'FROM listener GROUP BY `class`, `function`, `event`, `option`';
if (str_contains($db, 'AjaxBridge MySQL 8.4 compatibility') || substr_count($listener, $originalGroup) !== 1) {
    throw new RuntimeException('Use unpatched core for regression fixtures');
}
umask(0077);
$base = sys_get_temp_dir() . '/jeedom-compat-regression-' . bin2hex(random_bytes(8));
mkdir($base, 0700);
$caseCount = 0;
function check(bool $ok, string $message): void {
    if (!$ok) { throw new RuntimeException($message); }
}
function fixture(string $label, string $db, string $listener): array {
    global $base;
    $root = $base . '/' . $label . '/html';
    mkdir($root . '/core/class', 0700, true);
    foreach (['DB' => $db, 'listener' => $listener] as $name => $source) {
        $file = $root . '/core/class/' . $name . '.class.php';
        file_put_contents($file, $source);
        chmod($file, 0640);
        if (function_exists('posix_geteuid') && posix_geteuid() === 0) {
            chown($file, 33);
            chgrp($file, 33);
        }
    }
    return [$root, $base . '/' . $label . '/runtime'];
}
function runPatcher(array $fixture, bool $checkOnly = false): array {
    global $patcher;
    $args = [PHP_BINARY, '-d', 'opcache.enable_cli=0', $patcher, '--root', $fixture[0]];
    if ($checkOnly) { $args[] = '--check'; }
    $process = proc_open($args, [0 => ['pipe', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes,
        null, array_merge(getenv(), ['JEEDOM_RUNTIME_DIR' => $fixture[1]]));
    if (!is_resource($process)) { throw new RuntimeException('Cannot launch isolated patcher'); }
    fclose($pipes[0]);
    $output = stream_get_contents($pipes[1]) . stream_get_contents($pipes[2]);
    fclose($pipes[1]); fclose($pipes[2]);
    return [proc_close($process), $output];
}
function readTarget(array $fixture, string $name): string {
    return file_get_contents($fixture[0] . '/core/class/' . $name . '.class.php');
}
function assertRefused(string $label, string $db, string $listener): void {
    global $caseCount;
    $fixture = fixture($label, $db, $listener);
    [$status] = runPatcher($fixture);
    check($status === 1, $label . ': unsupported source was accepted');
    check(readTarget($fixture, 'DB') === $db && readTarget($fixture, 'listener') === $listener,
        $label . ': source refusal partially changed core');
    check(!file_exists($fixture[1]), $label . ': source refusal wrote backups');
    check(glob($fixture[0] . '/core/class/.mysql84-*') === [], $label . ': candidate leaked');
    $caseCount++;
}
function removeFixture(string $path): void {
    global $base;
    check(str_starts_with($path, $base . '/') || $path === $base, 'Invalid temporary cleanup target');
    if (is_link($path) || !is_dir($path)) { unlink($path); return; }
    foreach (array_diff(scandir($path), ['.', '..']) as $entry) { removeFixture($path . '/' . $entry); }
    rmdir($path);
}
try {
    $patchedDB = null;
    $patchedListener = null;
    foreach (['lf' => "\n", 'crlf' => "\r\n"] as $label => $eol) {
        $dbFixture = str_replace("\n", $eol, str_replace("\r\n", "\n", $db));
        $listenerFixture = str_replace("\n", $eol, str_replace("\r\n", "\n", $listener));
        $fixture = fixture($label, $dbFixture, $listenerFixture);
        $metadata = [];
        foreach (['DB', 'listener'] as $name) { $metadata[$name] = stat($fixture[0] . '/core/class/' . $name . '.class.php'); }
        [$status] = runPatcher($fixture, true);
        check($status === 1 && !file_exists($fixture[1]), '--check should refuse without writing');
        check(readTarget($fixture, 'DB') === $dbFixture && readTarget($fixture, 'listener') === $listenerFixture,
            '--check changed a core file');
        [$status, $output] = runPatcher($fixture);
        check($status === 0, $label . ': patch failed: ' . $output);
        $actualDB = readTarget($fixture, 'DB');
        $actualListener = readTarget($fixture, 'listener');
        check($actualListener === str_replace($originalGroup, $fixedGroup, $listenerFixture),
            'Listener changes extend beyond the four GROUP BY identifiers');
        foreach (['session-only GROUP BY', 'FULLTEXT has no sort order', 'integer display widths'] as $marker) {
            check(substr_count($actualDB, 'AjaxBridge MySQL 8.4 compatibility v1: ' . $marker) === 1,
                'Existing DB compatibility transform changed: ' . $marker);
        }
        if ($label === 'crlf') {
            check(preg_match('/(?<!\r)\n/', $actualDB . $actualListener) === 0, 'CRLF source acquired bare LF');
        } else { $patchedDB = $actualDB; $patchedListener = $actualListener; }
        clearstatcache();
        foreach (['DB' => $dbFixture, 'listener' => $listenerFixture] as $name => $source) {
            $file = $fixture[0] . '/core/class/' . $name . '.class.php';
            $backup = $fixture[1] . '/' . $name . '.class.' . hash('sha256', $source) . '.php.original';
            check(file_get_contents($backup) === $source, 'Original backup differs');
            check((fileperms($backup) & 0777) === 0600, 'Original backup is not private');
            foreach (['uid', 'gid', 'mode'] as $field) {
                check(stat($file)[$field] === $metadata[$name][$field], 'Core file metadata changed: ' . $field);
            }
        }
        $backupNames = scandir($fixture[1]);
        check(runPatcher($fixture, true)[0] === 0 && runPatcher($fixture)[0] === 0, 'Patched source was not accepted');
        check(readTarget($fixture, 'DB') === $actualDB && readTarget($fixture, 'listener') === $actualListener,
            'Second application was not byte-idempotent');
        check(scandir($fixture[1]) === $backupNames, 'Idempotent application added backups');
        $caseCount++;
    }
    $dbLF = str_replace("\r\n", "\n", $db);
    $listenerLF = str_replace("\r\n", "\n", $listener);
    foreach ([[$patchedDB, $listenerLF, 'listener'], [$dbLF, $patchedListener, 'DB']] as [$dbSource, $listenerSource, $changed]) {
        $fixture = fixture('partial-' . $changed, $dbSource, $listenerSource);
        check(runPatcher($fixture)[0] === 0, 'Partially patched installation was rejected');
        check(readTarget($fixture, 'DB') === $patchedDB && readTarget($fixture, 'listener') === $patchedListener,
            'Partially patched installation was not completed');
        $backups = glob($fixture[1] . '/*.original');
        check(count($backups) === 1 && str_starts_with(basename($backups[0]), $changed . '.class.'),
            'Already-compatible target was unnecessarily rewritten/backed up');
        $caseCount++;
    }
    assertRefused('listener-drift', $dbLF, str_replace($originalGroup, $originalGroup . ', id', $listenerLF));
    assertRefused('listener-duplicate', $dbLF, str_replace($originalGroup, $originalGroup . "\n-- " . $originalGroup, $listenerLF));
    assertRefused('listener-patched-drift', $patchedDB, str_replace('HAVING count(*) > 1', 'HAVING count(*) > 2', $patchedListener));
    assertRefused('listener-parse-error', $dbLF, $listenerLF . "\ninvalid syntax;\n");
    assertRefused('db-drift', str_replace('new PDO(', 'new OtherPDO(', $dbLF), $listenerLF);
    assertRefused('db-patched-drift', str_replace('SET SESSION sql_mode', 'SET GLOBAL sql_mode', $patchedDB), $listenerLF);
    $fixture = fixture('missing-listener', $dbLF, $listenerLF);
    unlink($fixture[0] . '/core/class/listener.class.php');
    check(runPatcher($fixture)[0] === 1 && readTarget($fixture, 'DB') === $dbLF && !file_exists($fixture[1]),
        'Missing listener source partly patched DB');
    $caseCount++;
    $fixture = fixture('symlink-listener', $dbLF, $listenerLF);
    rename($fixture[0] . '/core/class/listener.class.php', $fixture[0] . '/core/class/original.php');
    symlink('original.php', $fixture[0] . '/core/class/listener.class.php');
    check(runPatcher($fixture)[0] === 1 && readTarget($fixture, 'DB') === $dbLF && !file_exists($fixture[1]),
        'Symlink listener source was accepted');
    $caseCount++;
    echo json_encode(['passed' => $caseCount, 'line_endings' => ['LF', 'CRLF'],
        'check_read_only' => true, 'private_backups' => true, 'metadata_preserved' => true,
        'idempotent' => true, 'dispatch_unchanged' => true, 'all_sources_validated_before_writes' => true]), PHP_EOL;
} finally { removeFixture($base); }
