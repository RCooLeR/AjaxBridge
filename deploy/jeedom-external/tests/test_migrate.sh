#!/usr/bin/env bash
# Run only inside a network-isolated test container with a writable temporary FS.
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
export FAKE_STATE=$scratch/state
mkdir "$FAKE_STATE" "$scratch/bin" "$scratch/backup" "$scratch/html"
mkdir "$FAKE_STATE/source-html" "$FAKE_STATE/source-mysql"
cp "$root/tests/fake-docker.sh" "$scratch/bin/docker"
chmod 700 "$scratch/bin/docker"
export PATH="$scratch/bin:$PATH"
export JEEDOM_PASSWORD_FILE=$scratch/secret
printf '0123456789abcdef0123456789abcdef\n' > "$JEEDOM_PASSWORD_FILE"
bash "$root/migrate.sh" provision house "$JEEDOM_PASSWORD_FILE" >/dev/null
grep -Fq 'ON `jeedom\_house`.*' "$FAKE_STATE/provision.sql"
! grep -Eq 'DROP |ALTER |ON \*\.\*' "$FAKE_STATE/provision.sql"
FAKE_PARTIAL_REVOKES=1 bash "$root/migrate.sh" provision house "$JEEDOM_PASSWORD_FILE" >/dev/null
grep -Fq 'ON `jeedom_house`.*' "$FAKE_STATE/provision.sql"
touch "$FAKE_STATE/existing"
if bash "$root/migrate.sh" provision house "$JEEDOM_PASSWORD_FILE" >/dev/null 2>&1; then exit 1; fi
rm "$FAKE_STATE/existing"
printf 'CREATE TABLE cmd(id int);\n' > "$scratch/backup/database.sql"
cp "$scratch/backup/database.sql" "$scratch/backup/database.raw.sql"
tar -czf "$scratch/backup/html.tar.gz" -C "$scratch/html" .
printf 'cmd\t2\neqLogic\t1\n' > "$scratch/backup/table-counts.tsv"
printf '1\n7\n' > "$scratch/backup/cmd.ids"
printf '3\n' > "$scratch/backup/eqLogic.ids"
printf 'cmd\t20\neqLogic\t10\n' > "$scratch/backup/auto-increment.tsv"
printf 'house\n' > "$scratch/backup/instance"
(cd "$scratch/backup" && sha256sum database.raw.sql database.sql html.tar.gz table-counts.tsv eqLogic.ids cmd.ids auto-increment.tsv instance > SHA256SUMS)
bash "$root/migrate.sh" import house "$scratch/backup" >/dev/null
cmp "$scratch/backup/database.sql" "$FAKE_STATE/imported.sql"
! grep -q 0123456789abcdef "$FAKE_STATE/imported.sql"
bash "$root/migrate.sh" verify house "$scratch/backup" >/dev/null
touch "$FAKE_STATE/nonempty"
if bash "$root/migrate.sh" import house "$scratch/backup" >/dev/null 2>&1; then exit 1; fi
rm "$FAKE_STATE/nonempty"
touch "$FAKE_STATE/bad-app-login"
if bash "$root/migrate.sh" verify house "$scratch/backup" >/dev/null 2>&1; then exit 1; fi
rm "$FAKE_STATE/bad-app-login"
if bash "$root/migrate.sh" import appt "$scratch/backup" >/dev/null 2>&1; then exit 1; fi
printf 'changed\n' >> "$scratch/backup/database.sql"
if bash "$root/migrate.sh" import house "$scratch/backup" >/dev/null 2>&1; then exit 1; fi
cp "$scratch/backup/database.raw.sql" "$scratch/backup/database.sql"
# An omitted checksum replaced by a duplicate must not pass manifest validation.
sed 's/  auto-increment.tsv$/  eqLogic.ids/' "$scratch/backup/SHA256SUMS" > "$scratch/duplicate-manifest"
cp "$scratch/duplicate-manifest" "$scratch/backup/SHA256SUMS"
if bash "$root/migrate.sh" import house "$scratch/backup" >/dev/null 2>&1; then exit 1; fi
# Failed OCI start must still remove only the exact labelled helper container.
if bash "$root/migrate.sh" dump appt stopped-original "$scratch/new-backup" >/dev/null 2>&1; then exit 1; fi
[[ $(cat "$FAKE_STATE/maintenance-cleanup") == $'stop\nrm' ]]
grep -Eq '^jeedom-appt-db-migration-[0-9]+-[0-9]+$' "$FAKE_STATE/maintenance-name"
# Debian versioned PHP CLI names are writers, even without the plain `php` name.
if command -v php8.2 >/dev/null; then
  php8.2 -r 'usleep(30000000);' &
  writer=$!
  trap 'kill "$writer" 2>/dev/null || true' EXIT
  for _ in {1..50}; do
    [[ $(cat "/proc/$writer/comm" 2>/dev/null || true) == php8.2 ]] && break
    sleep 0.02
  done
  [[ $(cat "/proc/$writer/comm") == php8.2 ]]
  if bash "$root/migrate.sh" dump-vm house "$scratch/vm-backup" > "$scratch/vm-output" 2>&1; then exit 1; fi
  grep -Fq 'VM application writers still running' "$scratch/vm-output"
  [[ ! -e $scratch/vm-backup ]]
  kill "$writer"
  wait "$writer" 2>/dev/null || true
  trap - EXIT
fi
printf 'Migration offline contracts passed (literal grants, scoped import/login, ID/counter checks and fail-closed guards).\n'
