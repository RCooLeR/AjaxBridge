<?php
declare(strict_types=1);

// Synthetic configuration regression only; no database, Jeedom core or daemons.
require __DIR__ . '/configure.php';
use function AjaxBridge\JeedomExternal\configure;
use function AjaxBridge\JeedomExternal\loadConfig;
use function AjaxBridge\JeedomExternal\matchesRequestedDatabase;
use function AjaxBridge\JeedomExternal\settings;

function verify(bool $condition, string $message): void {
    if (!$condition) { throw new RuntimeException($message); }
}
function refuses(callable $action, string $message): void {
    try { $action(); } catch (Throwable $error) { return; }
    throw new RuntimeException($message);
}
$fixture = '/tmp/jeedom-external-test-' . bin2hex(random_bytes(6));
mkdir($fixture, 0700);
$html = $fixture . '/html';
mkdir($html . '/core/config', 0700, true);
$runtime = $fixture . '/runtime';
putenv('JEEDOM_RUNTIME_DIR=' . $runtime);
putenv('DB_HOST=central-mysql');
putenv('DB_PORT=3306');
putenv('DB_NAME=jeedom_fixture');
putenv('DB_USERNAME=fixture_user');
putenv('DB_PASSWORD=fixture password\'with\\symbols$');
putenv('DB_PASSWORD_FILE');
$path = $html . '/core/config/common.config.php';
$original = <<<'PHP'
<?php
// Preserve this exact unrelated source and configuration.
global $CONFIG;
$CONFIG = ['db' => ['host' => 'old-db', 'port' => 3306, 'dbname' => 'old_fixture',
    'username' => 'old_user', 'password' => 'old_fixture_password', 'unix_socket' => '/tmp/old.sock',
    'extra_db_setting' => 'preserve-db-extra'], 'custom' => ['nested' => 7]];
PHP;
try {
    file_put_contents($path, $original);
    verify(configure($html), 'First external DB switch was not applied.');
    $config = loadConfig($html);
    verify($config['custom']['nested'] === 7 && $config['db']['extra_db_setting'] === 'preserve-db-extra', 'Unrelated config changed.');
    verify(!isset($config['db']['unix_socket']), 'Local socket was not removed.');
    verify($config['db']['password'] === getenv('DB_PASSWORD'), 'Secret-safe PHP escaping changed the password.');
    matchesRequestedDatabase($config);
    $foreign = $config;
    $foreign['db']['dbname'] = 'foreign_fixture';
    refuses(fn() => matchesRequestedDatabase($foreign), 'Health accepted a foreign installation schema.');
    verify(str_starts_with(file_get_contents($path), $original), 'Existing PHP source was not preserved.');
    $backups = glob($runtime . '/config-backups/*.php.bak');
    verify(count($backups) === 1 && file_get_contents($backups[0]) === $original, 'Original config backup is missing or altered.');
    verify((fileperms($backups[0]) & 0777) === 0600 && (fileperms(dirname($backups[0])) & 0777) === 0700, 'Backup permissions are not private.');
    verify((fileperms($path) & 0777) === 0640, 'Installed config permissions are not restricted.');
    $first = file_get_contents($path);
    verify(!configure($html) && file_get_contents($path) === $first, 'Idempotent start rewrote configuration.');
    verify(count(glob($runtime . '/config-backups/*.php.bak')) === 1, 'Idempotent start created another backup.');
    putenv('DB_NAME=second_fixture');
    verify(configure($html), 'Second explicit DB switch was not applied.');
    verify(substr_count(file_get_contents($path), 'AJAXBRIDGE_EXTERNAL_DB_BEGIN') === 1, 'Override blocks accumulated.');
    verify(loadConfig($html)['db']['dbname'] === 'second_fixture', 'Second DB selection was not installed.');
    $secret = $fixture . '/db-password';
    file_put_contents($secret, "fixture-file-secret\n");
    putenv('DB_PASSWORD_FILE=' . $secret);
    verify(settings()['password'] === 'fixture-file-secret', 'Password file did not override environment password.');
    putenv('DB_PASSWORD_FILE=' . $fixture);
    refuses(fn() => settings(), 'Password directory was accepted as a Docker secret.');
    putenv('DB_PASSWORD_FILE');
    putenv('DB_PORT=0');
    refuses(fn() => settings(), 'Invalid DB port was accepted.');
    putenv('DB_PORT=3306');
    putenv('DB_HOST=localhost');
    refuses(fn() => settings(), 'Local Unix-socket hostname was accepted.');
    putenv('DB_HOST=central-mysql');
    putenv('DB_NAME=fixture;invalid');
    refuses(fn() => settings(), 'Invalid schema name was accepted.');
    putenv('DB_NAME=second_fixture');
    // Force a change before checking confinement; unchanged config performs no writes.
    putenv('DB_PASSWORD=different-fixture-secret');
    putenv('JEEDOM_RUNTIME_DIR=' . $html . '/private');
    refuses(fn() => configure($html), 'Backup directory inside web root was accepted.');
    putenv('JEEDOM_RUNTIME_DIR=' . $runtime);
    $saved = file_get_contents($path);
    unlink($path);
    symlink($fixture . '/outside-config', $path);
    refuses(fn() => configure($html), 'Symlinked config was overwritten.');
    unlink($path);
    file_put_contents($path, $saved);
    $fresh = $fixture . '/fresh-html';
    mkdir($fresh . '/core/config', 0700, true);
    verify(configure($fresh), 'Explicit configuration did not support a fresh complete tree.');
    verify(loadConfig($fresh)['db']['dbname'] === 'second_fixture', 'Fresh configuration selected the wrong DB.');
    echo "PASS: scoped DB switch, original-source preservation, private backup, escaping, idempotence, file-secret precedence and validation guards.\n";
} catch (Throwable $error) {
    fwrite(STDERR, $error->getMessage() . "\n");
    exit(1);
}
