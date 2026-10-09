<?php
declare(strict_types=1);
require '/opt/jeedom-external/configure.php';
use function AjaxBridge\JeedomExternal\connect;
use function AjaxBridge\JeedomExternal\loadConfig;
use function AjaxBridge\JeedomExternal\root;

$mode = $argv[1] ?? 'snapshot';
$root = root();
$pdo = connect(loadConfig($root));
if ($mode === 'marker') {
    $pdo->exec('CREATE TABLE runtime_smoke_marker (id INT PRIMARY KEY, value VARCHAR(64) NOT NULL)');
    $pdo->exec("INSERT INTO runtime_smoke_marker VALUES (1, 'preserve-local-smoke-data')");
    echo "Local marker stored.\n";
    exit;
}
if ($mode === 'http') {
    $curl = curl_init('http://127.0.0.1:' . (getenv('APACHE_HTTP_PORT') ?: '80') . '/index.php?v=d&p=connection');
    curl_setopt_array($curl, [CURLOPT_RETURNTRANSFER => true, CURLOPT_CONNECTTIMEOUT => 2, CURLOPT_TIMEOUT => 4]);
    $body = curl_exec($curl);
    $status = curl_getinfo($curl, CURLINFO_RESPONSE_CODE);
    curl_close($curl);
    if ($status !== 200 || !is_string($body) || !str_contains($body, 'in_login_username') || preg_match('/SQLSTATE|\[END INSTALL ERROR\]/i', $body)) {
        throw new RuntimeException('Expected a successful Jeedom login page without DB errors');
    }
    echo "HTTP 200: Jeedom login page rendered without DB errors.\n";
    exit;
}
if ($mode !== 'snapshot') { throw new RuntimeException('Unknown runtime test mode'); }
$tables = $pdo->query('SHOW TABLES')->fetchAll(PDO::FETCH_COLUMN);
$out = ['tables' => count($tables), 'server' => $pdo->query('SELECT VERSION()')->fetchColumn(),
    'core_sha256' => hash_file('sha256', $root . '/core/class/DB.class.php'),
    'config_sha256' => hash_file('sha256', $root . '/core/config/common.config.php'),
    'local_sql_binary' => is_executable('/usr/sbin/mysqld') || is_executable('/usr/sbin/mariadbd'),
    'local_sql_process' => trim((string) shell_exec('pgrep -x mysqld; pgrep -x mariadbd')) !== ''];
foreach (['user', 'eqLogic', 'cmd'] as $table) {
    if (!in_array($table, $tables, true)) { continue; }
    $rows = $pdo->query('SELECT * FROM `' . $table . '` ORDER BY id')->fetchAll(PDO::FETCH_ASSOC);
    $out[$table . '_count'] = count($rows);
    $out[$table . '_sha256'] = hash('sha256', json_encode($rows, JSON_THROW_ON_ERROR));
}
if (in_array('runtime_smoke_marker', $tables, true)) {
    $out['marker'] = $pdo->query('SELECT value FROM runtime_smoke_marker WHERE id=1')->fetchColumn();
}
echo json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES), PHP_EOL;
