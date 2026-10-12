#!/usr/bin/env bash
# Explicit user-run migration phases. Never activates Jeedom or alters/drops an
# existing schema/user. Temporary source server has no network or app processes.
set -euo pipefail
umask 077

usage() {
  printf '%s\n' 'Usage: migrate.sh provision house|appt [PASSWORD_FILE]' \
    '       migrate.sh dump house|appt STOPPED_ORIGINAL_CONTAINER ABS_BACKUP_DIR' \
    '       migrate.sh dump-vm house ABS_BACKUP_DIR' \
    '       migrate.sh import house|appt ABS_BACKUP_DIR' \
    '       migrate.sh verify house|appt ABS_BACKUP_DIR'
}
fail() { printf 'STOP: %s\n' "$*" >&2; exit 1; }
[[ $# -ge 2 ]] || { usage >&2; exit 2; }
phase=$1
instance=$2
shift 2
case "$instance" in house|appt) ;; *) fail 'Instance must be house or appt' ;; esac
target_db="jeedom_$instance"
target_user=$target_db
target_container=${JEEDOM_TARGET_DB_CONTAINER:-mysql-84}
source_db=${JEEDOM_SOURCE_DATABASE:-jeedom}
[[ $target_container =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || fail 'Invalid database container name'
[[ $source_db =~ ^[a-zA-Z0-9_]+$ ]] || fail 'Invalid source database name'
maintenance=''
maintenance_created=0
source_mode=''

cleanup() {
  if [[ $maintenance_created == 1 ]]; then
    # Never remove the original container or any volume/data directory.
    local label
    label=$(docker inspect --format '{{index .Config.Labels "ajaxbridge.jeedom-migration"}}' "$maintenance" 2>/dev/null || true)
    if [[ $label == source-db-only ]]; then
      docker stop --time 30 "$maintenance" >/dev/null 2>&1 || true
      docker rm "$maintenance" >/dev/null 2>&1 || true
    fi
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

target_sql() {
  # Root secret stays inside the existing MySQL container environment, never
  # command arguments, output, SQL files or the Jeedom application credentials.
  docker exec -i "$target_container" sh -ec '
    : "${MYSQL_ROOT_PASSWORD:?MySQL root secret unavailable}"
    export MYSQL_PWD=$MYSQL_ROOT_PASSWORD
    exec mysql --protocol=socket --user=root --batch --skip-column-names
  '
}
target_query() { printf '%s\n' "$1" | target_sql; }
read_app_password() {
  local password_file=${JEEDOM_PASSWORD_FILE:-./secrets/jeedom-$instance-db-password}
  [[ -f $password_file && ! -L $password_file ]] || fail 'Private application password file missing or symlinked'
  app_password=$(cat "$password_file")
  app_password=${app_password%$'\r'}
  [[ $app_password =~ ^[a-zA-Z0-9]{24,128}$ ]] || fail 'Application secret must be 24-128 alphanumeric characters'
}
target_app_sql() {
  read_app_password
  # Supply the secret over stdin, before the SQL stream. It is never a Docker
  # argument, host environment variable or generated SQL file. Imports use only
  # the schema-scoped application account, not the central server's root user.
  local status=0
  { printf '%s\n' "$app_password"; cat; } | docker exec -i "$target_container" sh -ec '
    IFS= read -r password || exit 1
    export MYSQL_PWD=$password
    exec mysql --protocol=socket --user="$1" --database="$2" --batch --skip-column-names
  ' sh "$target_user" "$target_db" || status=$?
  unset app_password
  return "$status"
}
target_app_query() { printf '%s\n' "$1" | target_app_sql; }
check_app_scope() {
  [[ $(target_app_query 'SELECT 1;') == 1 ]] || fail 'Application account authentication check failed'
  if target_app_query 'SELECT COUNT(*) FROM mysql.user;' >/dev/null 2>&1; then fail 'Application user has unexpected mysql system-schema access'; fi
}
check_target() {
  command -v docker >/dev/null || fail 'Docker CLI required'
  [[ $(docker inspect --format '{{.State.Running}}' "$target_container") == true ]] || fail 'Central MySQL container is not running'
  local version
  version=$(target_query 'SELECT VERSION();')
  [[ $version == 8.4.* ]] || fail 'This workflow is reviewed for central MySQL 8.4 only'
}
source_sql() {
  if [[ $source_mode == docker ]]; then
    docker exec -i --user root "$maintenance" mysql --protocol=socket --user=root --batch --skip-column-names "$source_db"
  else
    mysql --protocol=socket --user=root --batch --skip-column-names "$source_db"
  fi
}
source_query() { printf '%s\n' "$1" | source_sql; }

new_backup_dir() {
  backup_dir=$1
  shift
  [[ $backup_dir == /* && $backup_dir != / ]] || fail 'Backup directory must be an explicit absolute private path'
  [[ ! -L $backup_dir ]] || fail 'Backup directory must not be a symlink'
  [[ $(realpath -m -- "$backup_dir") == "$backup_dir" ]] || fail 'Use a canonical backup path without symlinked ancestors or dot segments'
  local source_path
  for source_path in "$@"; do
    source_path=$(realpath -e -- "$source_path")
    [[ $backup_dir != "$source_path" && $backup_dir != "$source_path/"* ]] || fail 'Backup must be outside every source mount/web root'
  done
  [[ ! -e $backup_dir ]] || fail 'Backup directory already exists; never overwrite a prior backup'
  mkdir -p -- "$backup_dir"
  chmod 700 -- "$backup_dir"
  backup_dir=$(realpath -e -- "$backup_dir")
}

metadata() {
  local mode=$1 destination=$2 table count
  local tables
  if [[ $mode == source ]]; then
    tables=$(source_query "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA='$source_db' AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME;")
  else
    tables=$(target_query "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA='$target_db' AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME;")
  fi
  [[ -n $tables ]] || fail 'Schema has no base tables'
  : > "$destination/table-counts.tsv"
  while IFS= read -r table; do
    [[ $table =~ ^[a-zA-Z0-9_]+$ ]] || fail 'Unexpected table identifier; manual review needed'
    if [[ $mode == source ]]; then
      count=$(printf 'SELECT COUNT(*) FROM `%s`;\n' "$table" | source_sql)
    else
      count=$(printf 'SELECT COUNT(*) FROM `%s`.`%s`;\n' "$target_db" "$table" | target_sql)
    fi
    [[ $count =~ ^[0-9]+$ ]] || fail 'Invalid table count'
    printf '%s\t%s\n' "$table" "$count" >> "$destination/table-counts.tsv"
  done <<< "$tables"
  for table in eqLogic cmd; do
    grep -q "^${table}"$'\t' "$destination/table-counts.tsv" || fail 'Not a complete Jeedom schema'
    if [[ $mode == source ]]; then
      printf 'SELECT id FROM `%s` ORDER BY id;\n' "$table" | source_sql > "$destination/$table.ids"
    else
      printf 'SELECT id FROM `%s`.`%s` ORDER BY id;\n' "$target_db" "$table" | target_sql > "$destination/$table.ids"
    fi
  done
  if [[ $mode == source ]]; then
    source_query "SELECT TABLE_NAME, COALESCE(CAST(AUTO_INCREMENT AS CHAR),'NULL') FROM information_schema.TABLES WHERE TABLE_SCHEMA='$source_db' AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME;" > "$destination/auto-increment.tsv"
  else
    target_query "SELECT TABLE_NAME, COALESCE(CAST(AUTO_INCREMENT AS CHAR),'NULL') FROM information_schema.TABLES WHERE TABLE_SCHEMA='$target_db' AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME;" > "$destination/auto-increment.tsv"
  fi
}

finish_dump() {
  local extras before
  # Stored programs/views can contain source-schema names and DEFINER accounts;
  # do not rewrite them or grant global privileges to force a cross-engine import.
  extras=$(source_query "SELECT (SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='$source_db' AND TABLE_TYPE <> 'BASE TABLE')+(SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA='$source_db')+(SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA='$source_db')+(SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA='$source_db');")
  [[ $extras == 0 ]] || fail 'Views/triggers/routines/events require a separately reviewed migration'
  metadata source "$backup_dir"
  if [[ $source_mode == docker ]]; then
    docker exec --user root "$maintenance" mysqldump --protocol=socket --user=root --single-transaction --skip-lock-tables --no-tablespaces --skip-add-drop-table --hex-blob --default-character-set=utf8mb4 "$source_db" > "$backup_dir/database.raw.sql"
    docker exec --user root "$maintenance" tar -czf - -C /var/www/html . > "$backup_dir/html.tar.gz"
  else
    mysqldump --protocol=socket --user=root --single-transaction --skip-lock-tables --no-tablespaces --skip-add-drop-table --hex-blob --default-character-set=utf8mb4 "$source_db" > "$backup_dir/database.raw.sql"
    tar -czf "$backup_dir/html.tar.gz" -C "${JEEDOM_SOURCE_HTML:-/var/www/html}" .
  fi
  [[ -s $backup_dir/database.raw.sql && -s $backup_dir/html.tar.gz ]] || fail 'Empty dump/archive'
  # MySQL clients reject the MariaDB-only sandbox first line. Preserve raw bytes
  # separately and remove ONLY that known first-line directive in the copy.
  sed '1{/^\/\*M!999999.*enable the sandbox mode/d;}' "$backup_dir/database.raw.sql" > "$backup_dir/database.sql"
  if grep -Eiq '^[[:space:]]*(CREATE|DROP|ALTER)[[:space:]]+DATABASE|^[[:space:]]*USE[[:space:]]|^[[:space:]]*(CREATE|ALTER|DROP)[[:space:]]+USER|^[[:space:]]*(GRANT|REVOKE)[[:space:]]' "$backup_dir/database.sql"; then
    fail 'Dump contains database/user switching statements; manual review required'
  fi
  before=$(mktemp -d)
  metadata source "$before"
  for file in table-counts.tsv eqLogic.ids cmd.ids auto-increment.tsv; do
    cmp -s "$backup_dir/$file" "$before/$file" || fail 'Source changed during dump; backup is NOT approved for import'
  done
  # Only the temporary comparison files created by this run are removed.
  rm -- "$before/table-counts.tsv" "$before/eqLogic.ids" "$before/cmd.ids" "$before/auto-increment.tsv"
  rmdir -- "$before"
  printf '%s\n' "$instance" > "$backup_dir/instance"
  (cd "$backup_dir" && sha256sum database.raw.sql database.sql html.tar.gz table-counts.tsv eqLogic.ids cmd.ids auto-increment.tsv instance > SHA256SUMS)
  chmod 600 -- "$backup_dir"/*
  printf 'Quiesced %s backup complete: %s (source left stopped).\n' "$instance" "$backup_dir"
}

check_backup() {
  backup_dir=$1
  [[ $backup_dir == /* && ! -L $backup_dir ]] || fail 'Use an absolute non-symlink backup directory'
  backup_dir=$(realpath -e -- "$backup_dir")
  for file in database.raw.sql database.sql html.tar.gz table-counts.tsv eqLogic.ids cmd.ids auto-increment.tsv instance SHA256SUMS; do
    [[ -f $backup_dir/$file && ! -L $backup_dir/$file ]] || fail 'Incomplete or symlinked backup'
  done
  [[ $(cat "$backup_dir/instance") == "$instance" ]] || fail 'Backup belongs to a different Jeedom instance'
  [[ $(wc -l < "$backup_dir/SHA256SUMS") == 8 ]] || fail 'Unexpected backup checksum manifest'
  # Only fixed local basenames are accepted; never let a manifest read elsewhere.
  awk 'NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-f]/ || $2 !~ /^(database\.raw\.sql|database\.sql|html\.tar\.gz|table-counts\.tsv|eqLogic\.ids|cmd\.ids|auto-increment\.tsv|instance)$/ || seen[$2]++ {exit 1} END {if (NR != 8) exit 1}' "$backup_dir/SHA256SUMS" || fail 'Unsafe or duplicated checksum manifest'
  (cd "$backup_dir" && sha256sum --check --status SHA256SUMS) || fail 'Backup integrity check failed'
}

case "$phase" in
  provision)
    [[ $# -le 1 ]] || fail 'Unexpected provisioning arguments'
    password_file=${1:-./secrets/jeedom-$instance-db-password}
    [[ -f $password_file && ! -L $password_file ]] || fail 'Private password file missing or symlinked'
    password=$(cat "$password_file")
    password=${password%$'\r'}
    [[ $password =~ ^[a-zA-Z0-9]{24,128}$ ]] || fail 'Use a newly generated 24-128 character alphanumeric secret'
    check_target
    present=$(target_query "SELECT (SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='$target_db')+(SELECT COUNT(*) FROM mysql.user WHERE User='$target_user');")
    [[ $present == 0 ]] || fail 'Schema or user already exists; nothing altered or dropped'
    # MySQL DB-level grants treat underscore as a wildcard when partial_revokes
    # is OFF, even with backticks. Escape it in that mode, without changing any
    # global server option. With partial_revokes ON the DB name is already literal.
    partial_revokes=$(target_query 'SELECT @@GLOBAL.partial_revokes;')
    case "$partial_revokes" in
      0) grant_db="jeedom\\_$instance" ;;
      1) grant_db=$target_db ;;
      *) fail 'Cannot establish literal database grant semantics' ;;
    esac
    # Unconditional CREATE also fails on a concurrent creation. It never reuses
    # a schema, changes a password or modifies another application's grants.
    printf "CREATE DATABASE \140%s\140 CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\nCREATE USER '%s'@'%%' IDENTIFIED BY '%s';\nGRANT ALL PRIVILEGES ON \140%s\140.* TO '%s'@'%%';\n" "$target_db" "$target_user" "$password" "$grant_db" "$target_user" | target_sql
    unset password
    printf 'Created %s with its own schema-scoped user; no existing database/user changed.\n' "$target_db"
    ;;
  dump)
    [[ $# == 2 ]] || fail 'dump requires stopped original container and absolute backup directory'
    original=$1
    [[ $original =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || fail 'Invalid original container name'
    [[ $(docker inspect --format '{{.State.Running}}' "$original") == false ]] || fail 'Stop the original Jeedom container first; helper never stops it'
    image=$(docker inspect --format '{{.Image}}' "$original")
    mysql_bind=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/mysql"}}{{if and (eq .Type "bind") .RW}}{{.Source}}{{end}}{{end}}{{end}}' "$original")
    html_bind=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/www/html"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' "$original")
    [[ $mysql_bind == /* && $html_bind == /* ]] || fail 'Expected the original writable local MariaDB bind and HTML bind'
    new_backup_dir "$2" "$html_bind" "$mysql_bind"
    maintenance="jeedom-$instance-db-migration-$(date +%s)-$$"
    # Docker may create the labelled container before its start/OCI setup fails.
    # Enable exact-name+label guarded cleanup before invoking docker run.
    maintenance_created=1
    docker run -d --name "$maintenance" --label ajaxbridge.jeedom-migration=source-db-only --network none --volumes-from "$original" --entrypoint sh "$image" -ec 'command -v mariadbd >/dev/null; install -d -o mysql -g mysql /run/mysqld; exec mariadbd --user=mysql --datadir=/var/lib/mysql --socket=/run/mysqld/mysqld.sock --pid-file=/run/mysqld/mysqld.pid --skip-networking --event-scheduler=OFF --read-only=ON' >/dev/null
    ready=0
    for _ in {1..30}; do
      if docker exec --user root "$maintenance" mysqladmin --protocol=socket --user=root ping >/dev/null 2>&1; then ready=1; break; fi
      sleep 1
    done
    [[ $ready == 1 ]] || fail 'Isolated source MariaDB failed to start; original remains stopped'
    source_mode=docker
    finish_dump
    ;;
  dump-vm)
    [[ $instance == house && $# == 1 ]] || fail 'dump-vm is house-only with an absolute backup directory'
    [[ $EUID == 0 ]] || fail 'Run dump-vm locally as root on the source VM'
    command -v mysqldump >/dev/null || fail 'VM database client unavailable'
    # Fail closed while likely Jeedom writers are running. Stop these manually;
    # do not kill unrelated processes or stop the database with this helper.
    for comm in /proc/[0-9]*/comm; do
      name=$(cat "$comm" 2>/dev/null || true)
      case "$name" in apache2|nginx|cron|crond|atd|node|nodejs|php*) fail 'VM application writers still running; quiesce Jeedom before dumping' ;; esac
    done
    html=${JEEDOM_SOURCE_HTML:-/var/www/html}
    [[ $html == /* && -f $html/core/config/common.config.php ]] || fail 'Source VM HTML/config missing'
    new_backup_dir "$1" "$html"
    source_mode=vm
    finish_dump
    ;;
  import)
    [[ $# == 1 ]] || fail 'import requires the immutable backup directory'
    check_backup "$1"
    check_target
    [[ $(target_query "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='$target_db';") == 1 ]] || fail 'Provision the dedicated target schema first'
    objects=$(target_query "SELECT (SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='$target_db')+(SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA='$target_db')+(SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA='$target_db');")
    [[ $objects == 0 ]] || fail 'Target schema is not empty; import refused, nothing deleted'
    check_app_scope
    target_app_sql < "$backup_dir/database.sql"
    printf 'SQL imported into %s. Verify before configuring/starting Jeedom; helper starts no app.\n' "$target_db"
    ;;
  verify)
    [[ $# == 1 ]] || fail 'verify requires the immutable backup directory'
    check_backup "$1"
    check_target
    check_app_scope
    result=$(mktemp -d)
    metadata target "$result"
    for file in table-counts.tsv eqLogic.ids cmd.ids auto-increment.tsv; do
      cmp -s "$backup_dir/$file" "$result/$file" || fail "Target mismatch in $file; do not start Jeedom"
    done
    rm -- "$result/table-counts.tsv" "$result/eqLogic.ids" "$result/cmd.ids" "$result/auto-increment.tsv"
    rmdir -- "$result"
    printf 'Verified %s: app login, table counts, equipment/command IDs and AUTO_INCREMENT unchanged.\n' "$target_db"
    ;;
  *) usage >&2; exit 2 ;;
esac
