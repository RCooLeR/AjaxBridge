#!/bin/bash
set -Eeuo pipefail
umask 0077

runtime=/opt/jeedom-external
html=${WEBSERVER_HOME:-/var/www/html}
mode=${1:-start}
php_runtime() { php -d opcache.enable_cli=0 -d display_errors=0 -d log_errors=0 "$@"; }
fail() { printf 'External Jeedom: %s\n' "$*" >&2; exit 1; }

prepare_work_dirs() {
  install -d -m 0770 -o www-data -g www-data /tmp/jeedom
  for name in log data backup; do
    path="$html/$name"
    [[ ! -L "$path" ]] || fail "Required writable directory is symlinked: $name."
    if [[ ! -d "$path" ]]; then
      mkdir -m 0770 "$path"
      chown www-data:www-data "$path"
    fi
    # Fix only the required directory itself when necessary. Never rewrite
    # permissions recursively on existing user assets or plugin files.
    if ! runuser -u www-data -- test -w "$path"; then
      chown www-data:www-data "$path"
      chmod u+rwx "$path"
    fi
    runuser -u www-data -- test -w "$path" || fail "www-data cannot write required directory: $name."
  done
}

# DATABASE=0 does not disable the official startup's server detection.
if command -v mysqld >/dev/null || command -v mariadbd >/dev/null; then
  fail 'This runtime must use a client-only image without a local SQL server.'
fi

seed() {
  [[ "$html" == /* && "$html" != / && "$html" != /var && "$html" != /var/www && ! -L "$html" ]] || fail 'Invalid seed destination.'
  [[ -d "$html" && "$(realpath "$html")" == "$html" ]] || fail 'Seed destination must already be a dedicated directory.'
  [[ -z "$(find "$html" -mindepth 1 -maxdepth 1 -print -quit)" ]] || fail 'Refusing seed: HTML directory is not empty.'
  tar --no-same-owner -xf "$runtime/seed.tar" -C "$html"
  chown -R www-data:www-data "$html"
  printf 'Pinned image HTML seeded. Provision a dedicated DB, then explicitly bootstrap or migrate a backup.\n'
}

case "$mode" in
  seed) seed; exit 0 ;;
  configure) exec php -d opcache.enable_cli=0 "$runtime/configure.php" configure ;;
  check) exec php -d opcache.enable_cli=0 "$runtime/configure.php" check ;;
  compat) exec php -d opcache.enable_cli=0 "$runtime/compat.php" --root "$html" ;;
  compat-check) exec php -d opcache.enable_cli=0 "$runtime/compat.php" --root "$html" --check ;;
  start|bootstrap) ;;
  *) fail 'Supported commands: start, seed, configure, check, compat, compat-check, bootstrap.' ;;
esac

php_runtime "$runtime/configure.php" configure
php_runtime "$runtime/configure.php" wait
if [[ "$mode" == bootstrap ]]; then
  php_runtime "$runtime/configure.php" empty
  prepare_work_dirs
  php_runtime "$runtime/compat.php" --root "$html"
  printf 'EXPLICIT BOOTSTRAP: initializing only the verified empty schema. This is not a backup restore. Change the initial admin password afterwards.\n'
  # Jeedom's installer excludes sudo parents from its concurrent-install guard.
  # A runuser parent is counted as a second installer and causes an early exit.
  sudo -u www-data -- php -d opcache.enable_cli=0 -d display_errors=0 -d log_errors=0 "$html/install/install.php"
  php_runtime "$runtime/configure.php" check
  exit 0
fi
php_runtime "$runtime/configure.php" check
prepare_work_dirs
php_runtime "$runtime/compat.php" --root "$html"

# Configure ports without rewriting the mounted Jeedom tree.
http_port=${APACHE_HTTP_PORT:-80}
https_port=${APACHE_HTTPS_PORT:-443}
[[ "$http_port" =~ ^[0-9]+$ && "$https_port" =~ ^[0-9]+$ ]] || fail 'Invalid Apache port.'
(( 10#$http_port >= 1 && 10#$http_port <= 65535 && 10#$https_port >= 1 && 10#$https_port <= 65535 )) || fail 'Apache port is out of range.'
printf 'Listen %s\n<IfModule ssl_module>\n  Listen %s\n</IfModule>\n' "$http_port" "$https_port" > /etc/apache2/ports.conf
sed -i -E "s/<VirtualHost \*:[0-9]+>/<VirtualHost *:${http_port}>/" /etc/apache2/sites-available/000-default.conf
sed -i -E "s/<VirtualHost \*:[0-9]+>/<VirtualHost *:${https_port}>/" /etc/apache2/sites-available/default-ssl.conf
if [[ -n "${TZ:-}" ]]; then
  [[ "$TZ" =~ ^[A-Za-z0-9_+.-]+(/[A-Za-z0-9_+.-]+)*$ && "$TZ" != *'..'* ]] || fail 'Invalid timezone.'
  [[ -f "/usr/share/zoneinfo/$TZ" ]] || fail 'Invalid timezone.'
  ln -snf "/usr/share/zoneinfo/$TZ" /etc/localtime
  printf '%s\n' "$TZ" > /etc/timezone
fi
if [[ -n "${ROOT_PASSWD:-}" ]]; then
  [[ "$ROOT_PASSWD" != *$'\n'* && "$ROOT_PASSWD" != *$'\r'* ]] || fail 'ROOT_PASSWD must not contain line breaks.'
  printf 'root:%s\n' "$ROOT_PASSWD" | chpasswd
fi
printf '* * * * * www-data /usr/bin/php %s/core/php/jeeCron.php >> /dev/null\n' "$html" > /etc/cron.d/jeedom
printf '*/5 * * * * root /usr/bin/php %s/core/php/watchdog.php >> /dev/null\n' "$html" > /etc/cron.d/jeedom_watchdog
chmod 0644 /etc/cron.d/jeedom /etc/cron.d/jeedom_watchdog

cron_pid=''
shutdown() {
  trap - TERM INT
  trap - EXIT
  printf 'Stopping external Jeedom services.\n'
  service apache2 stop >/dev/null 2>&1 || true
  service atd stop >/dev/null 2>&1 || true
  if [[ -n "$cron_pid" ]]; then kill -TERM "$cron_pid" 2>/dev/null || true; wait "$cron_pid" 2>/dev/null || true; fi
}
trap 'shutdown; exit 0' TERM INT
trap shutdown EXIT
# Keep configuration/bootstrap private, but use the normal service umask for
# Jeedom daemons. MQTT Manager runs Node through sudo and writes a root-owned
# PID file that Apache/www-data must be able to read for its health check.
umask 0022
service atd start
service apache2 start
cron -f &
cron_pid=$!
printf 'External Jeedom ready: mounted HTML, external SQL, Apache/cron/atd supervised. No local database or automatic restore.\n'
while true; do
  kill -0 "$cron_pid" 2>/dev/null || fail 'Cron exited unexpectedly.'
  service apache2 status >/dev/null 2>&1 || fail 'Apache exited unexpectedly.'
  pgrep -x atd >/dev/null || fail 'ATD exited unexpectedly.'
  sleep 5 &
  wait $!
done
