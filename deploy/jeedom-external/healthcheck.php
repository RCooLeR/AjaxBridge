<?php
declare(strict_types=1);

require __DIR__ . '/configure.php';

ini_set('display_errors', '0');
ini_set('log_errors', '0');
try {
    $root = \AjaxBridge\JeedomExternal\root();
    \AjaxBridge\JeedomExternal\validateCore($root);
    $config = \AjaxBridge\JeedomExternal\loadConfig($root);
    \AjaxBridge\JeedomExternal\matchesRequestedDatabase($config);
    $connection = \AjaxBridge\JeedomExternal\connect($config);
    \AjaxBridge\JeedomExternal\initialized($connection, $config);
    $connection->query('SELECT 1');
    // A Jeedom update can overwrite compatibility patches while daemons run.
    // Detect that condition without automatically editing live application code.
    $command = [PHP_BINARY, '-d', 'opcache.enable_cli=0', __DIR__ . '/compat.php', '--root', $root, '--check'];
    $process = proc_open($command, [0 => ['file', '/dev/null', 'r'], 1 => ['file', '/dev/null', 'w'],
        2 => ['file', '/dev/null', 'w']], $pipes);
    if (!is_resource($process) || proc_close($process) !== 0) {
        throw new RuntimeException('Compatibility patch is missing or no longer matches core.');
    }
    $port = getenv('APACHE_HTTP_PORT') ?: '80';
    if (!ctype_digit($port) || (int) $port < 1 || (int) $port > 65535) {
        throw new RuntimeException('Invalid HTTP port.');
    }
    // Jeedom's bare / legitimately redirects to its desktop route. Probe the
    // explicit login page so success requires 200 without following redirects.
    $request = curl_init('http://127.0.0.1:' . $port . '/index.php?v=d&p=connection');
    curl_setopt_array($request, [CURLOPT_RETURNTRANSFER => true, CURLOPT_CONNECTTIMEOUT => 2,
        CURLOPT_TIMEOUT => 4, CURLOPT_FOLLOWLOCATION => false]);
    $body = curl_exec($request);
    $status = curl_getinfo($request, CURLINFO_RESPONSE_CODE);
    curl_close($request);
    if ($body === false || $status !== 200 || preg_match('/SQLSTATE|\[END INSTALL ERROR\]/i', $body)) {
        throw new RuntimeException('Application HTTP health failed.');
    }
    echo "External DB, core compatibility and HTTP healthy.\n";
} catch (Throwable $error) {
    // Health output is visible through Docker inspect: no credentials or SQL text.
    fwrite(STDERR, "External Jeedom health failed (DB, initialization, compatibility or HTTP).\n");
    exit(1);
}
