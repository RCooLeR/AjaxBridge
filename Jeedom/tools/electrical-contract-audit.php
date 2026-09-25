<?php
// AjaxBridge diagnostic helper, MIT (../LICENSE.MIT).
// Reads local Jeedom metadata/cache only. It never evaluates a formula or executes a command.
namespace AjaxBridge\ElectricalContractAudit;

if (PHP_SAPI !== 'cli') {
    http_response_code(403);
    exit("CLI only.\n");
}

function usage() {
    return "Usage: php electrical-contract-audit.php --jeedom-root /var/www/html\n"
        . "Read-only JSON audit of ajaxSystem WallSwitch/Socket electrical info commands.\n"
        . "Run inside the Jeedom installation/container as its normal PHP user.\n"
        . "Values come from the existing cache; no refresh, sync, formula evaluation or actions.\n";
}

function numericId($value) {
    return (is_int($value) || is_string($value)) && preg_match('/\A[1-9][0-9]*\z/D', (string) $value)
        ? (string) $value : null;
}

function safeFormula($value) {
    if (!is_string($value) || strlen($value) > 512) {
        return null;
    }
    // Do not print arbitrary strings, URLs, credentials or executable expressions.
    // This is a token allowlist, not an evaluator or proof of formula correctness.
    $tokens = '~\A(?:\s+|\#value\#|\#[0-9]+\#|[0-9]+(?:\.[0-9]*)?(?:[eE][+-]?[0-9]+)?|\.[0-9]+(?:[eE][+-]?[0-9]+)?|[()+*/%,\-]|(?:abs|round|floor|ceil|min|max|pow)(?=\s*\())*\z~D';
    return preg_match($tokens, $value) === 1 ? $value : null;
}

function collectCommands() {
    $models = array('WallSwitch', 'Socket', 'SocketTypeG', 'SocketTypeGPlus', 'SocketTypeB', 'SocketOutletTypeE', 'SocketOutletTypeF');
    $logicalIds = array('currentMA', 'currentMilliAmpers', 'currentMilliAmpere', 'powerWtH', 'powerConsumedWattsPerHour', 'power', 'powerConsumptionWatts', 'voltage', 'voltageVolts');
    $units = array('', 'A', 'mA', 'µA', 'μA', 'uA', 'W', 'kW', 'mW', 'MW', 'Wh', 'kWh', 'MWh', 'V', 'mV', 'kV', 'VA', 'kVA');
    $rows = array();

    foreach (\eqLogic::byType('ajaxSystem', false) as $equipment) {
        if (!is_object($equipment) || $equipment->getEqType_name() !== 'ajaxSystem') {
            continue;
        }
        $model = $equipment->getConfiguration('device', '');
        $equipmentId = numericId($equipment->getId());
        if ($equipmentId === null || !in_array($model, $models, true)) {
            continue;
        }
        foreach ($equipment->getCmd('info') as $command) {
            if (!is_object($command) || $command->getType() !== 'info' || $command->getSubType() !== 'numeric'
                || $command->getEqType_name() !== 'ajaxSystem'
                || numericId($command->getEqLogic_id()) !== $equipmentId) {
                continue;
            }
            $commandId = numericId($command->getId());
            $logicalId = $command->getLogicalId();
            if ($commandId === null || !in_array($logicalId, $logicalIds, true)) {
                continue;
            }
            $unit = $command->getUnite();
            $safeUnit = in_array($unit, $units, true) ? $unit : null;
            $formula = safeFormula($command->getConfiguration('calculValueOffset', ''));
            $row = array(
                'commandId' => $commandId,
                'eqLogicId' => $equipmentId,
                'model' => $model,
                'logicalId' => $logicalId,
                'unit' => $safeUnit,
                'unitStatus' => $safeUnit === null ? 'withheld_unrecognized' : 'read',
                'calculValueOffset' => $formula,
                'calculValueOffsetStatus' => $formula === null ? 'withheld_non_arithmetic' : 'read',
                'cachedValue' => null,
                'cachedValueStatus' => 'missing',
            );
            // cmd::getCache reads cmdCacheAttr<ID>. Do not use execCmd(), even for info commands.
            try {
                $value = $command->getCache('value', null);
                if ($value !== null) {
                    if ((is_int($value) || is_float($value) || is_string($value))
                        && is_numeric($value) && is_finite((float) $value)) {
                        $row['cachedValue'] = $value;
                        $row['cachedValueStatus'] = 'read';
                    } else {
                        $row['cachedValueStatus'] = 'withheld_non_numeric';
                    }
                }
            } catch (\Throwable $error) {
                $row['cachedValueStatus'] = 'unavailable';
            }
            $rows[] = $row;
        }
    }
    usort($rows, function ($left, $right) {
        return strnatcmp($left['eqLogicId'], $right['eqLogicId']) ?: strnatcmp($left['commandId'], $right['commandId']);
    });
    return $rows;
}

if ($argc === 2 && $argv[1] === '--help') {
    fwrite(STDOUT, usage());
    exit(0);
}
if ($argc !== 3 || $argv[1] !== '--jeedom-root') {
    fwrite(STDERR, usage());
    exit(2);
}
$root = realpath($argv[2]);
if ($root === false || !is_dir($root)) {
    fwrite(STDERR, "Invalid Jeedom root directory.\n");
    exit(2);
}
foreach (array('core/php/core.inc.php', 'core/class/jeedom.class.php', 'core/class/eqLogic.class.php', 'core/class/cmd.class.php') as $requiredFile) {
    if (!is_file($root . '/' . $requiredFile) || !is_readable($root . '/' . $requiredFile)) {
        fwrite(STDERR, "Jeedom root is missing required readable core files.\n");
        exit(2);
    }
}

// Bootstrap/database errors may contain private configuration. Do not serialize them.
ini_set('display_errors', '0');
ini_set('log_errors', '0');
$outputLevel = ob_get_level();
ob_start(function ($output) { return ''; });
try {
    require_once $root . '/core/php/core.inc.php';
    $result = array(
        'schemaVersion' => 1,
        'source' => 'local_jeedom_metadata_and_cache',
        'readOnly' => true,
        'commands' => collectCommands(),
    );
    $json = json_encode($result, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
} catch (\Throwable $error) {
    while (ob_get_level() > $outputLevel) { ob_end_clean(); }
    fwrite(STDERR, "Audit failed while reading Jeedom. No command was executed. Check local Jeedom/PHP access.\n");
    exit(1);
}
while (ob_get_level() > $outputLevel) { ob_end_clean(); }
fwrite(STDOUT, $json . "\n");
