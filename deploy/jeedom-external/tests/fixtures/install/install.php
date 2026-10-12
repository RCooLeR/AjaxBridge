<?php
// Minimal reproduction of the upstream installer process guard, no bootstrap or DB.
$lines = [];
exec('(ps ax || ps w) | grep -ie "install/install.php" | grep -v "grep" | grep -v "sudo"', $lines);
echo json_encode(['matching_installer_processes' => count($lines), 'upstream_guard_would_refuse' => count($lines) > 1]), PHP_EOL;
