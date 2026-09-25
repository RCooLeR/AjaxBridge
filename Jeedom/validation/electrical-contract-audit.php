<?php
// AjaxBridge diagnostic helper validation, MIT (../LICENSE.MIT).
// Offline subprocess checks. No installed Jeedom, account, device or network is used.
if (PHP_SAPI !== 'cli') { exit('CLI only'); }
$helper = dirname(__DIR__) . '/tools/electrical-contract-audit.php';
$fixture = sys_get_temp_dir() . '/ajaxbridge-electrical-audit-' . bin2hex(random_bytes(8));
mkdir($fixture . '/core/php', 0700, true);
mkdir($fixture . '/core/class', 0700, true);
foreach (array('jeedom', 'eqLogic', 'cmd') as $class) {
    file_put_contents($fixture . '/core/class/' . $class . '.class.php', "<?php\n");
}
$checks = 0;
function checkAudit($condition, $message) {
    global $checks;
    if (!$condition) { throw new RuntimeException($message); }
    $checks++;
}
function invokeAudit($arguments) {
    global $helper;
    // The fixture needs no php.ini; avoid unrelated image startup extensions/warnings.
    $process = proc_open(array_merge(array(PHP_BINARY, '-n', $helper), $arguments), array(0 => array('pipe', 'r'), 1 => array('pipe', 'w'), 2 => array('pipe', 'w')), $pipes);
    if (!is_resource($process)) { throw new RuntimeException('Cannot start helper'); }
    fclose($pipes[0]);
    $out = stream_get_contents($pipes[1]);
    $err = stream_get_contents($pipes[2]);
    fclose($pipes[1]);
    fclose($pipes[2]);
    return array(proc_close($process), $out, $err);
}
$bootstrap = <<<'PHP'
<?php
echo 'PRIVATE_BOOTSTRAP_OUTPUT';
class AuditCommand {
    private $data;
    function __construct($data) { $this->data = $data; }
    function getId() { return $this->data['id']; }
    function getEqLogic_id() { return $this->data['owner'] ?? '20'; }
    function getEqType_name() { return $this->data['plugin'] ?? 'ajaxSystem'; }
    function getType() { return $this->data['type'] ?? 'info'; }
    function getSubType() { return $this->data['subtype'] ?? 'numeric'; }
    function getLogicalId() { return $this->data['logical']; }
    function getUnite() { return $this->data['unit'] ?? ''; }
    function getConfiguration($key, $default = '') {
        if ($key !== 'calculValueOffset') { throw new RuntimeException('Unexpected configuration access'); }
        return $this->data['formula'] ?? '';
    }
    function getCache($key, $default = '') {
        if ($key !== 'value' || !empty($this->data['mustSkip'])) { throw new RuntimeException('Unsafe cache access'); }
        if (!empty($this->data['cacheFails'])) { throw new RuntimeException('PRIVATE_CACHE_ERROR'); }
        return array_key_exists('value', $this->data) ? $this->data['value'] : $default;
    }
    function execCmd() { throw new RuntimeException('Command execution is forbidden'); }
    function event() { throw new RuntimeException('Event mutation is forbidden'); }
    function save() { throw new RuntimeException('Saving is forbidden'); }
}
class eqLogic {
    private $model;
    private $plugin;
    function __construct($model, $plugin = 'ajaxSystem') { $this->model = $model; $this->plugin = $plugin; }
    static function byType($type, $onlyEnabled = false) {
        if ($type !== 'ajaxSystem' || $onlyEnabled !== false) { throw new RuntimeException('Wrong equipment scope'); }
        return array(new self('WallSwitch'), new self('Relay'), new self('Socket', 'otherPlugin'));
    }
    function getId() { return 20; }
    function getEqType_name() { return $this->plugin; }
    function getConfiguration($key, $default = '') {
        if ($key !== 'device') { throw new RuntimeException('Private equipment configuration access'); }
        return $this->model;
    }
    function getCmd($type) {
        if ($type !== 'info' || $this->model !== 'WallSwitch' || $this->plugin !== 'ajaxSystem') { throw new RuntimeException('Wrong command scope'); }
        return array_map(function ($data) { return new AuditCommand($data); }, array(
            array('id' => 205, 'logical' => 'currentMA', 'value' => '0.96', 'unit' => 'A', 'formula' => '#value# / 1000'),
            array('id' => 204, 'logical' => 'powerWtH', 'value' => 78484),
            array('id' => 206, 'logical' => 'power', 'value' => 'PRIVATE_VALUE', 'unit' => 'PRIVATE_UNIT', 'formula' => 'fetch("https://private/?token=PRIVATE_FORMULA")'),
            array('id' => 207, 'logical' => 'voltage', 'cacheFails' => true),
            array('id' => 208, 'logical' => 'voltageVolts', 'value' => INF),
            array('id' => 209, 'logical' => 'currentMilliAmpere', 'formula' => 'round(#value# / 1000, 3)'),
            array('id' => 210, 'logical' => 'currentMilliAmpers', 'value' => 0, 'unit' => 'mA'),
            array('id' => 900, 'logical' => 'currentMA', 'type' => 'action', 'mustSkip' => true),
            array('id' => 901, 'logical' => 'currentMA', 'subtype' => 'string', 'mustSkip' => true),
            array('id' => 902, 'logical' => 'secretToken', 'mustSkip' => true),
            array('id' => 903, 'logical' => 'currentMA', 'owner' => 21, 'mustSkip' => true),
            array('id' => 904, 'logical' => 'currentMA', 'plugin' => 'otherPlugin', 'mustSkip' => true)
        ));
    }
    function refreshData() { throw new RuntimeException('Refresh is forbidden'); }
    function save() { throw new RuntimeException('Saving is forbidden'); }
}
PHP;
try {
    file_put_contents($fixture . '/core/php/core.inc.php', $bootstrap);
    list($code, $out, $err) = invokeAudit(array('--jeedom-root', $fixture));
    checkAudit($code === 0 && $err === '', 'Audit should return clean JSON');
    checkAudit(strpos($out, 'PRIVATE_') === false, 'Private strings leaked');
    $result = json_decode($out, true, 512, JSON_THROW_ON_ERROR);
    checkAudit($result['readOnly'] === true && $result['schemaVersion'] === 1, 'Wrong schema');
    $rows = array_column($result['commands'], null, 'commandId');
    checkAudit(array_keys($rows) === array(204, 205, 206, 207, 208, 209, 210), 'Command allowlist or stable ordering failed');
    checkAudit($rows[205]['calculValueOffset'] === '#value# / 1000' && $rows[205]['cachedValue'] === '0.96', 'Already scaled cache must remain unchanged');
    checkAudit($rows[204]['calculValueOffset'] === '' && $rows[204]['cachedValue'] === 78484 && $rows[204]['unit'] === '', 'Blank formula/unit must remain explicit');
    checkAudit($rows[206]['calculValueOffset'] === null && $rows[206]['calculValueOffsetStatus'] === 'withheld_non_arithmetic', 'Unexpected formula should be withheld');
    checkAudit($rows[206]['unit'] === null && $rows[206]['cachedValue'] === null, 'Unexpected unit/value should be withheld');
    checkAudit($rows[207]['cachedValueStatus'] === 'unavailable', 'Cache failure should be sanitized');
    checkAudit($rows[208]['cachedValueStatus'] === 'withheld_non_numeric', 'Nonfinite cache should be withheld');
    checkAudit($rows[209]['calculValueOffset'] === 'round(#value# / 1000, 3)' && $rows[209]['cachedValueStatus'] === 'missing', 'Math expression/missing cache failed');
    checkAudit($rows[210]['cachedValue'] === 0 && $rows[210]['cachedValueStatus'] === 'read', 'Zero must not be dropped');

    file_put_contents($fixture . '/core/php/core.inc.php', '<?php throw new RuntimeException("PRIVATE_BOOTSTRAP_FAILURE");');
    list($code, $out, $err) = invokeAudit(array('--jeedom-root', $fixture));
    checkAudit($code === 1 && $out === '' && strpos($err, 'PRIVATE_') === false, 'Bootstrap error leaked details');
    list($code, $out, $err) = invokeAudit(array('--help'));
    checkAudit($code === 0 && strpos($out, 'Usage:') === 0 && $err === '', 'Help should not load Jeedom');
    list($code, $out, $err) = invokeAudit(array());
    checkAudit($code === 2 && $out === '', 'Explicit root must be required');
    list($code, $out, $err) = invokeAudit(array('--jeedom-root', $fixture . '/missing'));
    checkAudit($code === 2 && $out === '', 'Missing root should be refused');
    unlink($fixture . '/core/class/cmd.class.php');
    list($code, $out, $err) = invokeAudit(array('--jeedom-root', $fixture));
    checkAudit($code === 2 && $out === '', 'Missing core should be refused before bootstrap');
    echo "Electrical contract audit: $checks checks passed\n";
} finally {
    // Delete only explicitly created fixture files/directories; no recursive path deletion.
    foreach (array('core/php/core.inc.php', 'core/class/jeedom.class.php', 'core/class/eqLogic.class.php', 'core/class/cmd.class.php') as $file) {
        if (is_file($fixture . '/' . $file)) { unlink($fixture . '/' . $file); }
    }
    rmdir($fixture . '/core/php');
    rmdir($fixture . '/core/class');
    rmdir($fixture . '/core');
    rmdir($fixture);
}
