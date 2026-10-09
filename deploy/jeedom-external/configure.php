<?php
declare(strict_types=1);

namespace AjaxBridge\JeedomExternal;

// Standalone runtime helper: do not load Jeedom core or start plugin daemons.
function root(): string
{
    $path = rtrim(getenv('WEBSERVER_HOME') ?: '/var/www/html', '/');
    if (!preg_match('~^/[A-Za-z0-9_./-]+$~D', $path) || $path === '' || is_link($path)) {
        throw new \RuntimeException('Invalid or symlinked WEBSERVER_HOME.');
    }
    $resolved = realpath($path);
    if ($resolved === false || $resolved !== $path || in_array($path, ['/', '/var', '/var/www'], true)) {
        throw new \RuntimeException('WEBSERVER_HOME must be an existing dedicated, absolute directory.');
    }
    return $path;
}

function validateCore(string $root): void
{
    foreach (['core/config/common.config.sample.php', 'core/config/version', 'core/php/core.inc.php',
        'core/class/DB.class.php', 'install/install.php', 'install/database.json', 'vendor/autoload.php'] as $file) {
        if (!is_file($root . '/' . $file) || !is_readable($root . '/' . $file)) {
            throw new \RuntimeException('Mounted HTML is incomplete: missing ' . $file . '. Use an existing complete tree or explicit seed.');
        }
    }
}

function runtimeDir(string $root): string
{
    $path = rtrim(getenv('JEEDOM_RUNTIME_DIR') ?: '/var/lib/jeedom-external', '/');
    if (!preg_match('~^/[A-Za-z0-9_./-]+$~D', $path) || is_link($path) ||
        preg_match('~/(?:\.|\.\.)(?:/|$)~', $path) || $path === '/' ||
        $path === $root || str_starts_with($path, $root . '/')) {
        throw new \RuntimeException('Invalid JEEDOM_RUNTIME_DIR.');
    }
    if (!is_dir($path) && !mkdir($path, 0700, true)) {
        throw new \RuntimeException('Cannot create protected runtime directory.');
    }
    $resolved = realpath($path);
    if ($resolved === false || $resolved !== $path || $path === '/' || $path === $root || str_starts_with($path, $root . '/')) {
        throw new \RuntimeException('Runtime backups must be outside the web root.');
    }
    return $path;
}

function settings(): array
{
    $required = [];
    foreach (['DB_HOST', 'DB_NAME', 'DB_USERNAME'] as $key) {
        $value = getenv($key);
        if ($value === false || trim($value) === '' || str_contains($value, "\0")) {
            throw new \RuntimeException('Set ' . $key . ' explicitly for this installation.');
        }
        $required[$key] = $value;
    }
    if (!preg_match('/^[A-Za-z0-9_.:-]+$/D', $required['DB_HOST']) ||
        !preg_match('/^[A-Za-z0-9_]+$/D', $required['DB_NAME'])) {
        throw new \RuntimeException('Invalid DB_HOST or DB_NAME.');
    }
    if (strtolower($required['DB_HOST']) === 'localhost') {
        throw new \RuntimeException('Use the external DB hostname, not localhost (which selects a local Unix socket).');
    }
    $port = getenv('DB_PORT');
    $port = $port === false || $port === '' ? '3306' : $port;
    if (!ctype_digit($port) || (int) $port < 1 || (int) $port > 65535) {
        throw new \RuntimeException('DB_PORT must be a number from 1 to 65535.');
    }
    $passwordFile = getenv('DB_PASSWORD_FILE');
    if ($passwordFile !== false && $passwordFile !== '') {
        if (!is_file($passwordFile) || !is_readable($passwordFile)) {
            throw new \RuntimeException('DB_PASSWORD_FILE must be a readable regular file.');
        }
        $password = file_get_contents($passwordFile);
        if ($password === false) {
            throw new \RuntimeException('Cannot read DB_PASSWORD_FILE.');
        }
        $password = rtrim($password, "\r\n");
    } else {
        $password = getenv('DB_PASSWORD');
    }
    if ($password === false || $password === '' || str_contains($password, "\0")) {
        throw new \RuntimeException('Set a nonempty DB_PASSWORD_FILE or DB_PASSWORD.');
    }
    return ['host' => $required['DB_HOST'], 'port' => $port, 'dbname' => $required['DB_NAME'],
        'username' => $required['DB_USERNAME'], 'password' => $password];
}

function loadConfig(string $root): array
{
    $path = $root . '/core/config/common.config.php';
    if (!is_file($path) || is_link($path)) {
        throw new \RuntimeException('Missing or symlinked common.config.php; run explicit configure first.');
    }
    global $CONFIG;
    $CONFIG = null;
    require $path;
    if (!is_array($CONFIG) || !isset($CONFIG['db']) || !is_array($CONFIG['db'])) {
        throw new \RuntimeException('Existing common.config.php does not define CONFIG.db.');
    }
    return $CONFIG;
}

function configure(string $root): bool
{
    $db = settings();
    $path = $root . '/core/config/common.config.php';
    if (is_link($path) || realpath(dirname($path)) !== $root . '/core/config') {
        throw new \RuntimeException('Refusing to overwrite a symlinked DB configuration or configuration directory.');
    }
    $existing = is_file($path);
    $source = $existing ? file_get_contents($path) : null;
    if ($existing && $source === false) {
        throw new \RuntimeException('Cannot read existing DB configuration.');
    }
    if ($existing) {
        $config = loadConfig($root);
        $changed = isset($config['db']['unix_socket']);
        foreach ($db as $key => $value) {
            $changed = $changed || !array_key_exists($key, $config['db']) || (string) $config['db'][$key] !== $value;
        }
        if (!$changed) {
            return false;
        }
        // Preserve existing PHP source, constants and all unrelated CONFIG fields.
        // Replace only our final override block on subsequent DB changes.
        $begin = '/* AJAXBRIDGE_EXTERNAL_DB_BEGIN */';
        $end = '/* AJAXBRIDGE_EXTERNAL_DB_END */';
        $beginAt = strpos($source, $begin);
        if ($beginAt !== false) {
            $endAt = strpos($source, $end, $beginAt);
            if ($endAt === false || trim(substr($source, $endAt + strlen($end)), "\r\n\t ?>") !== '') {
                throw new \RuntimeException('Malformed or non-final external DB override block.');
            }
            $source = substr($source, 0, $beginAt);
        }
        foreach (token_get_all($source) as $token) {
            if (is_array($token) && $token[0] === T_INLINE_HTML && trim($token[1]) !== '') {
                throw new \RuntimeException('DB configuration contains mixed PHP/HTML; manual migration is required.');
            }
        }
        $source = preg_replace('/\?>\s*$/D', '', $source);
        $newSource = rtrim($source) . "\n\n" . $begin . "\n" .
            '$CONFIG[\'db\'] = array_replace($CONFIG[\'db\'], ' . var_export($db, true) . ");\n" .
            'unset($CONFIG[\'db\'][\'unix_socket\']);' . "\n" . $end . "\n";
    } else {
        $newSource = "<?php\n// Generated external DB configuration; credentials must not be web-served.\n" .
            "define('DEBUG', 0);\nglobal \$CONFIG;\n\$CONFIG = " . var_export(['db' => $db], true) . ";\n";
    }
    // Parse without evaluating the new code before touching the old file.
    token_get_all($newSource, TOKEN_PARSE);
    $runtime = runtimeDir($root);
    $backupDir = $runtime . '/config-backups';
    if (is_link($backupDir) || (!is_dir($backupDir) && !mkdir($backupDir, 0700))) {
        throw new \RuntimeException('Cannot create protected config backup directory.');
    }
    if (!chmod($backupDir, 0700)) {
        throw new \RuntimeException('Cannot protect the config backup directory.');
    }
    if ($existing) {
        $backupPath = $backupDir . '/common.config-' . gmdate('Ymd-His') . '-' . bin2hex(random_bytes(6)) . '.php.bak';
        $backup = fopen($backupPath, 'xb');
        if ($backup === false || fwrite($backup, file_get_contents($path)) !== filesize($path)) {
            throw new \RuntimeException('Cannot save the original DB configuration; refusing update.');
        }
        if (!chmod($backupPath, 0600)) {
            fclose($backup);
            throw new \RuntimeException('Cannot protect the original DB configuration backup.');
        }
        fflush($backup);
        fclose($backup);
    }
    $temporary = tempnam(dirname($path), '.external-db-');
    if ($temporary === false) {
        throw new \RuntimeException('Cannot create an atomic DB configuration update.');
    }
    try {
        if (file_put_contents($temporary, $newSource) !== strlen($newSource) ||
            !chmod($temporary, 0640) || !chown($temporary, 'www-data') || !chgrp($temporary, 'www-data') ||
            !rename($temporary, $path)) {
            throw new \RuntimeException('Cannot atomically install DB configuration; original remains backed up.');
        }
    } finally {
        if (is_file($temporary)) {
            unlink($temporary);
        }
    }
    return true;
}

function connect(array $config): \PDO
{
    $db = $config['db'] ?? [];
    foreach (['host', 'port', 'dbname', 'username', 'password'] as $key) {
        if (!isset($db[$key]) || (string) $db[$key] === '') {
            throw new \RuntimeException('DB configuration is incomplete.');
        }
    }
    if (isset($db['unix_socket']) || !preg_match('/^[A-Za-z0-9_]+$/D', (string) $db['dbname'])) {
        throw new \RuntimeException('Runtime requires an explicit external TCP database.');
    }
    return new \PDO('mysql:host=' . $db['host'] . ';port=' . $db['port'] . ';dbname=' . $db['dbname'] . ';charset=utf8mb4',
        $db['username'], $db['password'], [\PDO::ATTR_TIMEOUT => 3, \PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION,
            \PDO::ATTR_EMULATE_PREPARES => false]);
}

function matchesRequestedDatabase(array $config): void
{
    $requested = settings();
    foreach ($requested as $key => $value) {
        if (!isset($config['db'][$key]) || (string) $config['db'][$key] !== $value) {
            throw new \RuntimeException('DB configuration differs from the requested installation scope; recreate with correct settings.');
        }
    }
    if (isset($config['db']['unix_socket'])) {
        throw new \RuntimeException('DB configuration still selects a local Unix socket.');
    }
}

function initialized(\PDO $connection, array $config): void
{
    $query = $connection->prepare('SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?');
    $query->execute([$config['db']['dbname']]);
    $tables = $query->fetchAll(\PDO::FETCH_COLUMN);
    foreach (['config', 'user', 'eqLogic', 'cmd'] as $table) {
        if (!in_array($table, $tables, true)) {
            throw new \RuntimeException('DB is not initialized. Explicitly bootstrap an empty schema or migrate a backup before start.');
        }
    }
    if ((int) $connection->query('SELECT COUNT(*) FROM `user`')->fetchColumn() < 1) {
        throw new \RuntimeException('DB initialization is incomplete: no Jeedom users.');
    }
}

function main(array $arguments): void
{
    $root = root();
    validateCore($root);
    $mode = $arguments[1] ?? 'check';
    if ($mode === 'configure') {
        echo configure($root) ? "External DB configuration updated; protected original retained.\n" : "External DB configuration unchanged.\n";
        return;
    }
    $config = loadConfig($root);
    matchesRequestedDatabase($config);
    if ($mode === 'wait') {
        $timeout = getenv('DB_WAIT_TIMEOUT');
        $timeout = $timeout === false || $timeout === '' ? '60' : $timeout;
        if (!ctype_digit($timeout) || (int) $timeout < 1 || (int) $timeout > 600) {
            throw new \RuntimeException('DB_WAIT_TIMEOUT must be from 1 to 600 seconds.');
        }
        $deadline = microtime(true) + (int) $timeout;
        do {
            try {
                connect($config)->query('SELECT 1');
                echo "External DB connection ready.\n";
                return;
            } catch (\PDOException $error) {
                if (microtime(true) >= $deadline) {
                    throw new \RuntimeException('External DB connection timed out (SQLSTATE ' . $error->getCode() . ').');
                }
                sleep(2);
            }
        } while (true);
    }
    $connection = connect($config);
    if ($mode === 'empty') {
        $query = $connection->prepare('SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?');
        $query->execute([$config['db']['dbname']]);
        if ((int) $query->fetchColumn() !== 0) {
            throw new \RuntimeException('Refusing bootstrap: destination schema already contains tables. Use explicit migration, never force installation.');
        }
        echo "Destination schema is empty.\n";
    } elseif ($mode === 'check') {
        initialized($connection, $config);
        $connection->query('SELECT 1');
        echo "External Jeedom DB is initialized and reachable.\n";
    } else {
        throw new \RuntimeException('Unknown runtime configuration command.');
    }
}

if (realpath($_SERVER['SCRIPT_FILENAME'] ?? '') === __FILE__) {
    ini_set('display_errors', '0');
    ini_set('log_errors', '0');
    try {
        main($argv);
    } catch (\Throwable $error) {
        // PDO diagnostics can contain connection details; never print them.
        fwrite(STDERR, 'External Jeedom: ' . ($error instanceof \PDOException ?
            'DB operation failed (SQLSTATE ' . $error->getCode() . ').' : $error->getMessage()) . "\n");
        exit(1);
    }
}
