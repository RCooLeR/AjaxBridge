<?php
declare(strict_types=1);

// Run only inside the isolated smoke-test network. No Jeedom bootstrap, credentials, or daemons.
define('DEBUG', true);
$root = '/fixture';
$password = 'local-smoke-only';
$admin = null;
for ($attempt = 0; $attempt < 60; $attempt++) {
    try {
        $admin = new PDO('mysql:host=127.0.0.1;dbname=jeedom_test', 'root', $password, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
        break;
    } catch (Throwable $error) { usleep(500000); }
}
if (!$admin) { throw new RuntimeException('Isolated MySQL did not become ready within 30 seconds'); }
$admin->exec('CREATE DATABASE jeedom_restore');
$admin->exec("GRANT ALL PRIVILEGES ON jeedom_restore.* TO 'jeedom_smoke'@'%'");
$globalMode = $admin->query('SELECT @@global.sql_mode')->fetchColumn();
$plugin = $admin->query("SELECT plugin FROM mysql.user WHERE User='jeedom_smoke' AND Host='%'")->fetchColumn();
// Flush caching_sha2's authentication cache before the first application login.
$admin->exec('FLUSH PRIVILEGES');
$CONFIG = ['db' => ['host' => '127.0.0.1', 'port' => '3306', 'dbname' => 'jeedom_test', 'username' => 'jeedom_smoke', 'password' => $password]];
require $root . '/core/class/DB.class.php';
$pdo = DB::getConnection();
$sessionMode = $pdo->query('SELECT @@session.sql_mode')->fetchColumn();
$pdoCipher = $pdo->query("SHOW STATUS LIKE 'Ssl_cipher'")->fetch(PDO::FETCH_NUM)[1];
function check(bool $condition, string $message): void {
    if (!$condition) { throw new RuntimeException($message); }
}
check(str_contains($globalMode, 'ONLY_FULL_GROUP_BY'), 'Server default was unexpectedly relaxed');
check(!str_contains($sessionMode, 'ONLY_FULL_GROUP_BY'), 'Jeedom connection still enables ONLY_FULL_GROUP_BY');
$expectedMode = implode(',', array_values(array_diff(explode(',', $globalMode), ['ONLY_FULL_GROUP_BY'])));
check($sessionMode === $expectedMode, 'Compatibility patch changed SQL modes beyond ONLY_FULL_GROUP_BY');
check(str_contains($sessionMode, 'STRICT_TRANS_TABLES'), 'Strict transactional validation was removed');
check(str_contains($sessionMode, 'NO_ZERO_DATE'), 'Zero-date validation was removed');
check($plugin === 'caching_sha2_password', 'Fixture user does not use MySQL 8.4 default authentication');
$independent = new PDO('mysql:host=127.0.0.1;dbname=jeedom_test', 'jeedom_smoke', $password, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
check($independent->query('SELECT @@session.sql_mode')->fetchColumn() === $globalMode, 'Unrelated connection inherited Jeedom compatibility changes');

$schema = json_decode(file_get_contents($root . '/install/database.json'), true, flags: JSON_THROW_ON_ERROR);
$schemaTables = [];
$fulltextCount = 0;
foreach ($schema['tables'] as $table) {
    $plan = DB::compareTable($table);
    $pdo->exec($plan[$table['name']]['sql']);
    $schemaTables[] = $table['name'];
    foreach ($table['indexes'] as $index) {
        $sql = DB::buildDefinitionIndex($index, $table['name']);
        if (($index['Index_type'] ?? '') === 'FULLTEXT') {
            check(!str_contains($sql, ' ASC'), 'FULLTEXT index has unsupported explicit sort direction');
            $fulltextCount++;
        }
    }
}
check(count($schemaTables) === 31, 'Expected the actual 31-table Jeedom 4.6 schema');
check($fulltextCount > 0, 'Actual Jeedom FULLTEXT definitions were not exercised');
$actualIndexes = (int) $pdo->query("SELECT COUNT(DISTINCT CONCAT(TABLE_NAME, '/', INDEX_NAME)) FROM information_schema.statistics WHERE TABLE_SCHEMA='jeedom_test' AND INDEX_TYPE='FULLTEXT'")->fetchColumn();
check($actualIndexes === $fulltextCount, 'Not all FULLTEXT indexes were created');
$schemaRecheck = DB::compareDatabase($schema);
$recheckIssues = [];
foreach ($schemaRecheck as $tableName => $table) {
    $issues = array_filter(array_merge($table['fields'] ?? [], $table['indexes'] ?? []), fn($item) => ($item['status'] ?? 'ok') !== 'ok');
    if ($issues || trim($table['sql'] ?? '') !== '') { $recheckIssues[$tableName] = count($issues); }
}
check(!$recheckIssues, 'Fresh schema immediately requires repeated alterations: ' . json_encode($recheckIssues));
$typeCases = [];
foreach ([
    ['tinyint(1)', 'tinyint', true], ['smallint(6)', 'smallint', true],
    ['mediumint(9)', 'mediumint', true], ['int(11)', 'int', true],
    ['bigint(20)', 'bigint', true], ['int(10) unsigned', 'int unsigned', true],
    ['int(11)', 'int unsigned', false], ['int(10) unsigned', 'int', false],
    ['int(11)', 'bigint', false], ['decimal(12,2)', 'decimal(12,3)', false],
    ['varchar(127)', 'varchar(128)', false],
    ['int(11) unsigned zerofill', 'int unsigned zerofill', false],
    ['int(11) unsigned zerofill', 'int(11) unsigned', false],
] as [$referenceType, $actualType, $equivalent]) {
    $reference = ['name' => 'probe', 'type' => $referenceType, 'null' => 'YES', 'default' => null, 'extra' => ''];
    $actual = ['Type' => $actualType, 'Null' => 'YES', 'Default' => null, 'Extra' => ''];
    $comparison = DB::compareField($reference, $actual, 'type_fixture')['probe'];
    check(($comparison['status'] === 'ok') === $equivalent, 'Integer normalization changed a real type difference: ' . $referenceType . ' / ' . $actualType);
    if (!$equivalent) { check(str_contains($comparison['sql'], $referenceType), 'Type normalization changed repair DDL'); }
    $typeCases[] = ['reference' => $referenceType, 'actual' => $actualType, 'equivalent' => $equivalent];
}
$ordinaryIndex = DB::buildDefinitionIndex(['Key_name' => 'ordinary_fixture', 'Non_unique' => 1, 'columns' => [['column' => 'cmd_id', 'Sub_part' => null]]], 'history');
check(str_contains($ordinaryIndex, ' ASC'), 'Ordinary BTREE index generation changed unexpectedly');

$pdo->exec("INSERT INTO history(cmd_id,datetime,value) VALUES (1,'2026-10-09 09:00:00','1.2'),(1,'2026-10-09 09:10:00','2.4'),(1,'2026-10-09 10:00:00','4.8'),(1,'2026-11-01 09:00:00','7.2'),(1,'2027-01-02 09:00:00','8.4'),(2,'2026-10-09 09:00:00','99')");
$pdo->exec("INSERT INTO historyArch(cmd_id,datetime,value) VALUES (1,'2026-10-08 09:00:00','0.6')");
// Load the actual history class, explicitly excluding its production core.inc.php bootstrap.
$historySource = file_get_contents($root . '/core/class/history.class.php');
check(substr_count($historySource, 'require_once') === 1, 'Unexpected history includes; review sandbox loading before continuing');
$historySource = preg_replace('/^<\?php/', '', $historySource);
$historySource = preg_replace('/^require_once __DIR__ .*core\.inc\.php.*;\r?$/m', '', $historySource, -1, $bootstrapCount);
check($bootstrapCount === 1, 'Production Jeedom bootstrap was not explicitly excluded');
eval($historySource);
$queries = [];
foreach (['day' => 4, 'month' => 3, 'year' => 2] as $period => $expectedRows) {
    $rows = history::getPlurality(1, null, null, $period);
    check(count($rows) === $expectedRows, 'Unexpected plurality row count for ' . $period);
    foreach ($rows as $row) { check((int) $row->getCmd_id() === 1, 'History crossed command IDs'); }
    $expectedValues = ['day' => [0.6, 4.8, 7.2, 8.4], 'month' => [4.8, 7.2, 8.4], 'year' => [7.2, 8.4]][$period];
    check(array_map(fn($row) => (float) $row->getValue(), $rows) === $expectedValues, 'Plurality maxima or date ordering changed for ' . $period);
    $queries['plurality_' . $period] = count($rows);
}
foreach (['avg::minute', 'avg::hour', 'avg::day', 'avg::week', 'avg::month', 'avg::year', 'high::hour', 'low::day', 'sum::day', 'avg::day::max::hour', 'avg::hour||delta'] as $group) {
    $rows = history::all(1, null, null, $group);
    check(count($rows) > 0, 'Empty history grouping result: ' . $group);
    foreach ($rows as $row) { check((int) $row->getCmd_id() === 1, 'Grouped history crossed command IDs'); }
    $queries['all_' . $group] = count($rows);
}
$raw = history::all(1);
check(count($raw) === 6, 'Raw history did not combine live and archived values correctly');
$queries['all_raw'] = count($raw);
foreach (['avg::day' => 2.8, 'sum::day' => 8.4, 'low::day' => 1.2] as $group => $expectedValue) {
    $target = array_values(array_filter(history::all(1, null, null, $group), fn($row) => $row->getDatetime() === '2026-10-09'));
    check(count($target) === 1 && abs((float) $target[0]->getValue() - $expectedValue) < 0.000001, 'History aggregate value changed: ' . $group);
}
try {
    $pdo->exec("INSERT INTO history(cmd_id,datetime,value) VALUES (3,'0000-00-00 00:00:00','1')");
    throw new RuntimeException('Strict zero-date protection no longer rejects invalid dates');
} catch (PDOException $expected) {
    check($expected->errorInfo[1] === 1292, 'Unexpected invalid-date failure: ' . $expected->getMessage());
}
check($admin->query('SELECT @@global.sql_mode')->fetchColumn() === $globalMode, 'Server global sql_mode changed');
check($independent->query('SELECT @@session.sql_mode')->fetchColumn() === $globalMode, 'Independent session sql_mode changed');
echo json_encode([
    'php' => PHP_VERSION,
    'pdo_client' => $pdo->getAttribute(PDO::ATTR_CLIENT_VERSION),
    'server' => $pdo->query('SELECT VERSION()')->fetchColumn(),
    'auth_plugin' => $plugin,
    'cold_authentication_cache' => true,
    'pdo_ssl_cipher' => $pdoCipher,
    'schema_tables' => count($schemaTables),
    'schema_recheck_alterations' => count($recheckIssues),
    'metadata_type_comparisons' => count($typeCases),
    'fulltext_indexes' => $fulltextCount,
    'jeedom_session_mode' => $sessionMode,
    'unchanged_global_mode' => $globalMode,
    'strict_invalid_date_rejected' => true,
    'history_queries' => $queries,
], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES), PHP_EOL;
