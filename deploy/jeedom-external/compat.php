<?php
declare(strict_types=1);

// Apply narrowly scoped compatibility fixes to the mounted Jeedom core before
// starting its workers. Original files are kept outside the served HTML tree.
const SESSION_MARKER = 'AjaxBridge MySQL 8.4 compatibility v1: session-only GROUP BY';
const INDEX_MARKER = 'AjaxBridge MySQL 8.4 compatibility v1: FULLTEXT has no sort order';
const TYPE_MARKER = 'AjaxBridge MySQL 8.4 compatibility v1: integer display widths';

function methodBounds(string $source, string $name): array
{
    $tokens = token_get_all($source);
    $offset = 0;
    $seekingName = false;
    $matched = false;
    $start = null;
    $depth = 0;
    foreach ($tokens as $token) {
        $text = is_array($token) ? $token[1] : $token;
        if (is_array($token) && $token[0] === T_FUNCTION) {
            $seekingName = true;
            $matched = false;
        } elseif ($seekingName && is_array($token) && $token[0] === T_STRING) {
            $matched = $text === $name;
            $seekingName = false;
        }
        // Once inside the selected method, closures/nested functions must not
        // reset brace tracking. Interpolated-string braces have tokenized opens.
        if ($start !== null && is_array($token)
            && in_array($token[0], [T_CURLY_OPEN, T_DOLLAR_OPEN_CURLY_BRACES], true)) {
            $depth++;
        } elseif (($start !== null || $matched) && $text === '{' && !is_array($token)) {
            if ($start === null) {
                $start = $offset;
            }
            $depth++;
        } elseif ($start !== null && $text === '}' && !is_array($token)) {
            if (--$depth === 0) {
                return [$start, $offset + 1];
            }
        }
        $offset += strlen($text);
    }
    throw new RuntimeException("Unsupported Jeedom core: method {$name} was not found");
}

function patchMethod(string $source, string $name, callable $transform): string
{
    [$start, $end] = methodBounds($source, $name);
    return substr($source, 0, $start)
        . $transform(substr($source, $start, $end - $start))
        . substr($source, $end);
}

function compatibleSource(string $source): string
{
    $eol = str_contains($source, "\r\n") ? "\r\n" : "\n";
    $sessionStatement = 'static::$connection->exec("SET SESSION sql_mode = REPLACE(@@SESSION.sql_mode, \'ONLY_FULL_GROUP_BY\', \'\')")';
    $source = patchMethod($source, 'initConnection', function (string $method) use ($eol, $sessionStatement): string {
        if (str_contains($method, SESSION_MARKER)) {
            if (substr_count($method, $sessionStatement) !== 1) {
                throw new RuntimeException('Modified Jeedom session compatibility patch; review the mounted core');
            }
            return $method;
        }
        if (substr_count($method, 'new PDO(') !== 2
            || !str_contains($method, 'PDO::MYSQL_ATTR_INIT_COMMAND')
            || str_contains($method, 'sql_mode')) {
            throw new RuntimeException('Unsupported Jeedom initConnection implementation; compatibility patch was not applied');
        }
        $body = rtrim(substr($method, 0, -1));
        return $body . $eol . "\t\t// " . SESSION_MARKER . '.' . $eol
            . "\t\t// Retain all other strict modes; shared MySQL server settings are unchanged." . $eol
            . "\t\tif (" . $sessionStatement . ' === false) {' . $eol
            . "\t\t\tthrow new \\RuntimeException('Cannot apply Jeedom MySQL session compatibility');" . $eol
            . "\t\t}" . $eol . "\t}";
    });
    $source = patchMethod($source, 'buildDefinitionIndex', function (string $method) use ($eol): string {
        $replacement = '// ' . INDEX_MARKER . '.' . $eol
            . "\t\t\t" . '$return .= (isset($_index[\'Index_type\']) && $_index[\'Index_type\'] === \'FULLTEXT\') ? \',\' : \' ASC,\';';
        if (str_contains($method, INDEX_MARKER)) {
            if (substr_count($method, $replacement) !== 1) {
                throw new RuntimeException('Modified Jeedom FULLTEXT compatibility patch; review the mounted core');
            }
            return $method;
        }
        $original = '$return .= \' ASC,\';';
        if (substr_count($method, $original) !== 1 || !str_contains($method, 'CREATE FULLTEXT INDEX')) {
            throw new RuntimeException('Unsupported Jeedom buildDefinitionIndex implementation; compatibility patch was not applied');
        }
        return str_replace($original, $replacement, $method);
    });
    return patchMethod($source, 'compareField', function (string $method) use ($eol): string {
        $replacement = '// ' . TYPE_MARKER . '.' . $eol
            . "\t\t// Compare metadata only; preserve column DDL, unsigned and ZEROFILL semantics." . $eol
            . "\t\t" . '$mysql84FieldType = static function ($type) {' . $eol
            . "\t\t\t" . 'return preg_replace(\'/^(tinyint|smallint|mediumint|int|bigint)\\([0-9]+\\)(?= unsigned$|$)/i\', \'$1\', $type);' . $eol
            . "\t\t};" . $eol
            . "\t\t" . 'if ($mysql84FieldType($_ref_field[\'type\']) != $mysql84FieldType($_real_field[\'Type\'])) {';
        if (str_contains($method, TYPE_MARKER)) {
            if (substr_count($method, $replacement) !== 1) {
                throw new RuntimeException('Modified Jeedom integer compatibility patch; review the mounted core');
            }
            return $method;
        }
        $original = 'if ($_ref_field[\'type\'] != $_real_field[\'Type\']) {';
        if (substr_count($method, $original) !== 1 || !str_contains($method, 'static::buildDefinitionField($_ref_field)')) {
            throw new RuntimeException('Unsupported Jeedom compareField implementation; compatibility patch was not applied');
        }
        return str_replace($original, $replacement, $method);
    });
}

function compatibleListenerSource(string $source): string
{
    return patchMethod($source, 'clean', function (string $method): string {
        $original = 'FROM listener GROUP BY class, function, event, option';
        $replacement = 'FROM listener GROUP BY `class`, `function`, `event`, `option`';
        // FUNCTION and OPTION are reserved in MySQL 8.4. Change only this
        // maintenance query, never listener lookup/dispatch or deletion rules.
        if (substr_count($method, $original) + substr_count($method, $replacement) !== 1
            || substr_count($method, 'GROUP BY') !== 1
            || preg_match('/(?:' . preg_quote($original, '/') . '|' . preg_quote($replacement, '/') . ')\r?\n[\t ]*HAVING count\(\*\) > 1\';/', $method) !== 1
            || substr_count($method, "HAVING count(*) > 1'") !== 1
            || substr_count($method, '$duplicateds = DB::Prepare($sql, array(), DB::FETCH_TYPE_ALL, PDO::FETCH_CLASS, __CLASS__);') !== 1) {
            throw new RuntimeException('Unsupported Jeedom listener clean implementation; compatibility patch was not applied');
        }
        return str_replace($original, $replacement, $method);
    });
}

try {
    $options = getopt('', ['root:', 'check']);
    $root = realpath($options['root'] ?? '/var/www/html');
    if ($root === false || $root === DIRECTORY_SEPARATOR) {
        throw new RuntimeException('A mounted Jeedom HTML directory is required');
    }
    $sources = [];
    $changes = [];
    // Validate every target before creating backups, candidates or changing any
    // mounted core file. A listener source mismatch must not partly patch DB.
    foreach (['DB' => 'compatibleSource', 'listener' => 'compatibleListenerSource'] as $name => $transform) {
        $file = $root . '/core/class/' . $name . '.class.php';
        if (!is_file($file) || is_link($file)) {
            throw new RuntimeException('Jeedom core/class/' . $name . '.class.php must be a regular file');
        }
        $source = file_get_contents($file);
        if ($source === false) {
            throw new RuntimeException('Cannot read the mounted Jeedom ' . $name . ' class');
        }
        $patched = $transform($source);
        token_get_all($patched, TOKEN_PARSE);
        $sources[$file] = hash('sha256', $source);
        if ($patched !== $source) {
            $changes[$file] = ['name' => $name, 'source' => $source, 'patched' => $patched];
        }
    }
    if (!$changes) {
        fwrite(STDOUT, "Jeedom MySQL 8.4 compatibility: ready\n");
        exit(0);
    }
    if (array_key_exists('check', $options)) {
        throw new RuntimeException('Jeedom MySQL compatibility patch is missing; recreate the Jeedom container to apply it before serving requests');
    }
    $runtime = getenv('JEEDOM_RUNTIME_DIR') ?: '/var/lib/jeedom-external';
    if (!is_dir($runtime) && !mkdir($runtime, 0700, true)) {
        throw new RuntimeException('Cannot create the external runtime backup directory');
    }
    $runtime = realpath($runtime);
    if ($runtime === false || $runtime === $root || str_starts_with($runtime, $root . DIRECTORY_SEPARATOR)) {
        throw new RuntimeException('Runtime backups must be outside the served HTML directory');
    }
    $candidates = [];
    try {
        foreach ($changes as $file => $change) {
            $backup = $runtime . '/' . $change['name'] . '.class.' . $sources[$file] . '.php.original';
            if (is_link($backup) || (file_exists($backup) && !is_file($backup))) {
                throw new RuntimeException('Compatibility backup must be a regular file');
            }
            if (file_exists($backup)) {
                if (hash_file('sha256', $backup) !== $sources[$file]) {
                    throw new RuntimeException('Existing compatibility backup does not match the original');
                }
            } elseif (file_put_contents($backup, $change['source'], LOCK_EX) !== strlen($change['source']) || !chmod($backup, 0600)) {
                throw new RuntimeException('Cannot preserve the original Jeedom ' . $change['name'] . ' class');
            }
            $temp = tempnam(dirname($file), '.mysql84-');
            if ($temp === false) {
                throw new RuntimeException('The mounted Jeedom core directory is not writable');
            }
            $candidates[$file] = $temp;
            if (file_put_contents($temp, $change['patched'], LOCK_EX) !== strlen($change['patched'])) {
                throw new RuntimeException('Cannot write the compatibility candidate');
            }
            exec(escapeshellarg(PHP_BINARY) . ' -l ' . escapeshellarg($temp) . ' 2>&1', $lintOutput, $lintStatus);
            if ($lintStatus !== 0) {
                throw new RuntimeException('Compatibility candidate failed PHP syntax validation');
            }
            $stat = stat($file);
            if ($stat === false || !chmod($temp, $stat['mode'] & 0777)
                || !chown($temp, $stat['uid']) || !chgrp($temp, $stat['gid'])) {
                throw new RuntimeException('Cannot preserve mounted Jeedom file ownership/permissions');
            }
        }
        foreach ($sources as $file => $hash) {
            if (is_link($file) || hash_file('sha256', $file) !== $hash) {
                throw new RuntimeException('Jeedom core changed during compatibility preparation; retry after its update finishes');
            }
        }
        foreach ($candidates as $file => $temp) {
            if (!rename($temp, $file)) {
                throw new RuntimeException('Cannot activate the compatibility candidate');
            }
        }
    } finally {
        foreach ($candidates as $temp) {
            if (is_file($temp)) {
                unlink($temp);
            }
        }
    }
    fwrite(STDOUT, "Jeedom MySQL 8.4 compatibility applied; original preserved outside HTML\n");
} catch (Throwable $error) {
    fwrite(STDERR, $error->getMessage() . "\n");
    exit(1);
}
