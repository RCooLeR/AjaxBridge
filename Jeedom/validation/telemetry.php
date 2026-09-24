<?php
// Adapted 2026-09-24 for AjaxBridge packaging; original regression tests unchanged.
// Run with: php validation/telemetry.php /path/to/complete/ajaxSystem
// Executes the plugin's real parser/migration with a small in-memory Jeedom adapter.
// Payloads are synthetic API examples, not recordings from a live installation.
error_reporting(E_ALL);
set_error_handler(function ($severity, $message, $file, $line) {
  throw new ErrorException($message, 0, $severity, $file, $line);
});

class cmd {
  private static $nextId = 1;
  public $attributes = array('type' => 'info', 'configuration' => array());
  public $value = null;
  public function __call($method, $args) {
    $key = strtolower(substr($method, 3));
    if (substr($method, 0, 3) == 'set') {
      $this->attributes[$key] = $args[0];
      return $this;
    }
    return isset($this->attributes[$key]) ? $this->attributes[$key] : null;
  }
  public function setConfiguration($key, $value) {
    $this->attributes['configuration'][$key] = $value;
  }
  public function save() {
    if (!isset($this->attributes['id'])) {
      $this->attributes['id'] = self::$nextId++;
    }
    eqLogic::$equipment[$this->getEqLogic_id()]->commands[$this->getId()] = $this;
  }
  public function execCmd() { return $this->value; }
  public function formatValue($value) {
    // Match Jeedom's numeric scaling for the formulas used by these templates.
    $formula = $this->attributes['configuration']['calculValueOffset'] ?? '';
    if ($formula == '#value# / 10') { return $value / 10; }
    if ($formula == '#value# / 1000') { return $value / 1000; }
    return $value;
  }
}

class eqLogic {
  public static $equipment = array();
  public $commands = array();
  public $battery = null;
  private $id;
  private $configuration;
  public function __construct($device = 'Transmitter', $type = 'device') {
    $this->id = count(self::$equipment) + 1;
    self::$equipment[$this->id] = $this;
    $this->configuration = array('device' => $device, 'type' => $type);
  }
  public function getId() { return $this->id; }
  public function getConfiguration($key) { return $this->configuration[$key] ?? ''; }
  public function setConfiguration($key, $value) { $this->configuration[$key] = $value; }
  public function save($direct = false) {}
  public function getCmd($type = null, $logicalId = null) {
    $commands = array_filter($this->commands, function ($cmd) use ($type, $logicalId) {
      return ($type === null || $cmd->getType() == $type) && ($logicalId === null || $cmd->getLogicalId() == $logicalId);
    });
    return $logicalId === null ? array_values($commands) : (reset($commands) ?: null);
  }
  public function checkAndUpdateCmd($command, $value) {
    $cmd = is_object($command) ? $command : $this->getCmd('info', $command);
    if ($cmd) { $cmd->value = $cmd->formatValue($value); }
  }
  public function batteryStatus($value) { $this->battery = $value; }
}

class utils {
  public static function a2o($object, $definition) {
    foreach ($definition as $key => $value) {
      if ($key == 'configuration') {
        foreach ($value as $name => $setting) { $object->setConfiguration($name, $setting); }
      } else {
        $object->{'set' . $key}($value);
      }
    }
  }
}
class config { public static function byKey($key, $plugin = null) { return 'test'; } }
class cache {
  public static function byKey($key) { return new self(); }
  public function getValue() { return 'test-session'; }
}
class com_http {
  public static $requests = 0;
  public function __construct($url) {
    self::$requests++;
    throw new RuntimeException('Unexpected API request: ' . $url);
  }
}
function ls($directory, $pattern, $unused, $options) { return array_map('basename', glob($directory . '/' . $pattern)); }
function is_json($data, $default) { return json_decode($data, true); }

if (PHP_SAPI !== 'cli') { exit('CLI only'); }
$pluginRoot = isset($argv[1]) ? realpath($argv[1]) : false;
if (!$pluginRoot || !is_file($pluginRoot . '/core/config/devices/Relay.json')) {
  fwrite(STDERR, "Usage: php validation/telemetry.php /path/to/complete/ajaxSystem\n");
  exit(2);
}
$classFile = $pluginRoot . '/core/class/ajaxSystem.class.php';
$source = file_get_contents($classFile);
$source = preg_replace('/^require_once .*$/m', '', $source, 1, $removed);
if ($removed !== 1) { throw new RuntimeException('Expected one Jeedom bootstrap include'); }
// Keep __DIR__ equivalent to the real file when loading it without Jeedom core.
$source = str_replace('__DIR__', var_export(dirname($classFile), true), $source);
eval(substr($source, 5));

$checks = 0;
function same($expected, $actual, $message) {
  global $checks;
  $checks++;
  if ($expected !== $actual) {
    throw new RuntimeException($message . ': expected ' . var_export($expected, true) . ', got ' . var_export($actual, true));
  }
}
function addInfo($eq, $id, $subtype = 'numeric', $name = null, $order = 0) {
  $cmd = new ajaxSystemCmd();
  utils::a2o($cmd, array('type' => 'info', 'subtype' => $subtype, 'name' => $name ?? $id, 'logicalId' => $id));
  $cmd->setEqLogic_id($eq->getId());
  $cmd->setOrder($order);
  $cmd->save();
  return $cmd;
}
function reading($eq, $id) { return $eq->getCmd('info', $id)->execCmd(); }

// Battery values must not depend on command ordering or the transport's key.
$hub = new ajaxSystem('HUB_2', 'hub');
addInfo($hub, 'battery::chargeLevelPercentage');
addInfo($hub, 'online', 'binary');
$hub->refreshData(array('battery' => array('chargeLevelPercentage' => 85), 'online' => true));
same(85, reading($hub, 'battery::chargeLevelPercentage'), 'Battery survives a later online command');
same(85, $hub->battery, 'Equipment battery matches snapshot');
$hub->updateData(array('batteryCharge' => 0), true);
same(0, reading($hub, 'battery::chargeLevelPercentage'), 'Callback can report empty battery');
same(0, $hub->battery, 'Equipment battery accepts zero');
$hub->updateData(array('batteryCharge' => -1), true);
same(0, reading($hub, 'battery::chargeLevelPercentage'), 'Unknown battery sentinel preserves prior value');

$button = new ajaxSystem('Button');
$custom = addInfo($button, 'customBattery', 'numeric', 'Batterie', 4);
$issues = addInfo($button, 'issuesCount', 'numeric', 'My health command', 9);
$issues->setConfiguration('custom', 'keep');
$issues->setIsHistorized(0);
$issuesId = $issues->getId();
$button->ensureInfoCommands();
$count = count($button->getCmd());
$button->ensureInfoCommands();
same($count, count($button->getCmd()), 'Migration is idempotent');
same($issuesId, $button->getCmd('info', 'issuesCount')->getId(), 'Migration preserves command ID');
same('keep', $issues->attributes['configuration']['custom'], 'Migration preserves user settings');
same(0, $issues->getIsHistorized(), 'Migration preserves history setting');
same(9, $issues->getOrder(), 'Migration preserves ordering');
same($custom, $button->getCmd('info', 'customBattery'), 'Same-name custom command survives');
same('Batterie (batteryChargeLevelPercentage)', $button->getCmd('info', 'batteryChargeLevelPercentage')->getName(), 'Name collision is disambiguated');
$button->updateData(array('batteryCharge' => 72, 'issuesCount' => 0, 'batteryPingStatus' => 'OK'), true);
same(72, reading($button, 'batteryChargeLevelPercentage'), 'Device battery callback alias');
same('OK', reading($button, 'batteryPingStatus'), 'Button battery check status');
$button->updateData(array('issuesCount' => 1), true);
same(72, reading($button, 'batteryChargeLevelPercentage'), 'Unrelated update preserves battery');

$water = new ajaxSystem('WaterStop');
$water->ensureInfoCommands();
foreach (array('OPEN', 'INTERMEDIATE_STATE', 'CLOSED') as $state) {
  $water->updateData(array('valveState' => $state), true);
  same($state, reading($water, 'valveState'), 'Valve position is reported without a command request');
}
$water->refreshData(array());
same('CLOSED', reading($water, 'valveState'), 'Empty snapshot neither fetches nor resets readings');
$water->updateData(array('valveState' => null), true);
same('CLOSED', reading($water, 'valveState'), 'Unknown valve state is not fabricated');

$rex = new ajaxSystem('RangeExtender2');
$rex->ensureInfoCommands();
$rex->updateData(array('networkDetails' => array('ethernet' => array('connectionOk' => true))), true);
same(true, reading($rex, 'networkDetails::ethernet::connectionOk'), 'Nested callback path');
$rex->updateData(array('networkDetails::ethernet::connectionOk' => false), true);
same(false, reading($rex, 'networkDetails::ethernet::connectionOk'), 'Flattened callback path and false value');

$multi = new ajaxSystem('MultiTransmitterFibra');
$multi->ensureInfoCommands();
$multi->updateData(array('malfunctionStates' => array('CHARGER_ERROR')), true);
same('["CHARGER_ERROR"]', reading($multi, 'malfunctionStates'), 'Fault lists stored as JSON');
$multi->updateData(array('malfunctionStates' => array()), true);
same('[]', reading($multi, 'malfunctionStates'), 'Empty fault list clears previous faults');

$fire = new ajaxSystem('FireProtectPlus');
$fire->ensureInfoCommands();
$fire->updateData(array('smokeAlarmDetected' => true, 'coAlarmDetected' => false), true);
same(true, reading($fire, 'smokeAlarmDetected'), 'Smoke alarm independent of CO');
same(false, reading($fire, 'coAlarmDetected'), 'CO clear state');
$fire->updateData(array('smokeAlarmDetected' => false), true);
same(false, reading($fire, 'smokeAlarmDetected'), 'Alarm restoration preserves false');
$co = new ajaxSystem('FireProtect2Crb');
$co->ensureInfoCommands();
same(null, $co->getCmd('info', 'smokeAlarm'), 'CO-only device has no smoke alarm');
same(null, $co->getCmd('info', 'tempAlarm'), 'CO-only device has no heat alarm');
$co->updateData(array('coAlarm' => 'CO_ALARM_DETECTED'), true);
$co->updateData(array('coAlarm' => 'CO_ALARM_NOT_DETECTED'), true);
same('CO_ALARM_NOT_DETECTED', reading($co, 'coAlarm'), 'Enum alarm restoration remains a string');

$lite = new ajaxSystem('LifeQualityLite');
$lite->ensureInfoCommands();
$lite->refreshData(array('actualTemperature' => -50, 'actualHumidity' => 450));
same(-5, reading($lite, 'actualTemperature'), 'Negative temperature scaling');
same(45, reading($lite, 'actualHumidity'), 'Humidity follows existing LifeQuality scaling');
$lite->updateData(array('actualTemperature' => 0), true);
same(0, reading($lite, 'actualTemperature'), 'Zero temperature survives partial callback');
same(45, reading($lite, 'actualHumidity'), 'Missing humidity does not reset history');
same(null, $lite->getCmd('info', 'actualCO2'), 'Lite has no CO2 sensor command');

// Preserve callback normalization used by existing equipment.
$switch = new ajaxSystem('LightSwitchTwoGang');
addInfo($switch, 'channelStatus_1', 'binary');
addInfo($switch, 'channelStatus_2', 'binary');
foreach (array(0, 1, 2, 3) as $mask) {
  $switch->updateData(array('channelStatus' => $mask), true);
  same(($mask & 1) ? 1 : 0, reading($switch, 'channelStatus_1'), 'First channel bit');
  same(($mask & 2) ? 1 : 0, reading($switch, 'channelStatus_2'), 'Second channel bit');
}
addInfo($hub, 'state', 'string');
addInfo($hub, 'externallyPowered', 'binary');
$hub->updateData(array('state' => 2, 'hubPowered' => false), true);
same('NIGHT_MODE', reading($hub, 'state'), 'Hub state normalization');
same(false, reading($hub, 'externallyPowered'), 'Hub power normalization');
$socket = new ajaxSystem('Socket');
$current = addInfo($socket, 'currentMA');
$current->setConfiguration('calculValueOffset', '#value# / 1000');
addInfo($socket, 'voltage');
addInfo($socket, 'power');
addInfo($socket, 'realState', 'binary');
$socket->updateData(array('currentMA' => 1000, 'voltage' => 230, 'realState' => 0), true);
same(230, reading($socket, 'power'), 'Socket power uses updated scaled current and voltage');
same(1, reading($socket, 'realState'), 'Socket callback inversion preserved');

// WallSwitch/Relay expose confirmed device state from both transport formats.
foreach (array('WallSwitch', 'Relay') as $model) {
  $output = new ajaxSystem($model);
  $output->ensureInfoCommands();
  same('binary', $output->getCmd('info', 'realState')->getSubType(), $model . ' state command is binary');
  same(1, $output->getCmd('info', 'realState')->getIsVisible(), $model . ' state is visible');
  same(1, $output->getCmd('info', 'realState')->getIsHistorized(), $model . ' state is historized');
  $stateId = $output->getCmd('info', 'realState')->getId();
  $output->ensureInfoCommands();
  same($stateId, $output->getCmd('info', 'realState')->getId(), $model . ' migration preserves state ID');

  foreach (array(false, true) as $isUpdate) {
    foreach (array('SWITCHED_ON' => 1, 'SWITCHED_OFF' => 0) as $flag => $value) {
      $payload = array('switchState' => array($flag));
      if ($isUpdate) { $output->updateData($payload, true); }
      else { $output->refreshData($payload); }
      same($value, reading($output, 'realState'), $model . ' API state in ' . ($isUpdate ? 'callback' : 'snapshot'));
    }
  }
  $offStates = array('OFF_TOO_LOW_VOLTAGE', 'OFF_HIGH_VOLTAGE', 'OFF_HIGH_TEMPERATURE');
  if ($model == 'WallSwitch') {
    $offStates[] = 'OFF_HIGH_CURRENT';
    $offStates[] = 'OFF_SHORT_CIRCUIT';
  }
  foreach ($offStates as $flag) {
    $output->updateData(array('switchState' => array('SWITCHED_ON')), true);
    $output->updateData(array('switchState' => array($flag)), true);
    same(0, reading($output, 'realState'), $model . ' protection shutdown: ' . $flag);
  }
  $output->updateData(array('realState' => 0), true);
  same(1, reading($output, 'realState'), $model . ' legacy on callback');
  $output->updateData(array('realState' => 1), true);
  same(0, reading($output, 'realState'), $model . ' legacy off callback');
  $output->updateData(array('realState' => 1, 'switchState' => array('SWITCHED_ON')), true);
  same(1, reading($output, 'realState'), $model . ' explicit API state wins without inversion');

  foreach (array(null, array(), array('CONTACT_HANG'), array('UNKNOWN'), array('SWITCHED_ON', 'SWITCHED_OFF'), array('SWITCHED_ON', 'OFF_HIGH_TEMPERATURE'), 'SWITCHED_OFF', array(0)) as $unknown) {
    $output->updateData(array('switchState' => $unknown), true);
    same(1, reading($output, 'realState'), $model . ' ambiguous or invalid state preserves last reading');
  }
  $output->updateData(array('online' => false), true);
  same(1, reading($output, 'realState'), $model . ' unrelated callback preserves state');
  $output->updateData(array('realState' => 1, 'switchState' => array('CONTACT_HANG')), true);
  same(0, reading($output, 'realState'), $model . ' legacy state works when API flags are inconclusive');
}
$socket->updateData(array('switchState' => array('SWITCHED_OFF')), true);
same(1, reading($socket, 'realState'), 'New switch mapping does not alter other device models');
same(0, com_http::$requests, 'All telemetry parsing and migration performed without API requests');

echo 'PASS: ' . $checks . ' telemetry checks; zero API requests.' . PHP_EOL;
