#!/usr/bin/env bash
# Offline migrate.sh contract stub; never contacts Docker or a real DB.
set -euo pipefail
if [[ ${1:-} == inspect ]]; then
  case "$*" in
    *'.State.Running'*) [[ ${@: -1} == mysql-84 ]] && printf 'true\n' || printf 'false\n' ;;
    *'.Image'*) printf 'sha256:source-fixture\n' ;;
    *'/var/lib/mysql'*) printf '%s/source-mysql\n' "$FAKE_STATE" ;;
    *'/var/www/html'*) printf '%s/source-html\n' "$FAKE_STATE" ;;
    *'ajaxbridge.jeedom-migration'*)
      [[ -f $FAKE_STATE/maintenance-name && $(cat "$FAKE_STATE/maintenance-name") == "${@: -1}" ]] && printf 'source-db-only\n'
      ;;
    *) exit 83 ;;
  esac
  exit 0
fi
if [[ ${1:-} == run ]]; then
  for ((index=1; index<=$#; index++)); do
    [[ ${!index} == --name ]] || continue
    next=$((index+1))
    printf '%s\n' "${!next}" > "$FAKE_STATE/maintenance-name"
    break
  done
  # Simulate Docker's create-success/start-failure path.
  exit 125
fi
if [[ ${1:-} == stop || ${1:-} == rm ]]; then
  [[ -f $FAKE_STATE/maintenance-name && $(cat "$FAKE_STATE/maintenance-name") == "${@: -1}" ]] || exit 84
  printf '%s\n' "$1" >> "$FAKE_STATE/maintenance-cleanup"
  exit 0
fi
[[ ${1:-} == exec ]] || exit 80
payload=$(cat)
if [[ $* == *'sh jeedom_house jeedom_house'* ]]; then
  [[ ${payload%%$'\n'*} == 0123456789abcdef0123456789abcdef ]] || exit 81
  payload=${payload#*$'\n'}
  [[ $payload == 'SELECT COUNT(*) FROM mysql.user;' ]] && exit 1
  if [[ $payload == 'SELECT 1;' ]]; then
    [[ ! -e $FAKE_STATE/bad-app-login ]] || exit 1
    printf '1\n'; exit 0
  fi
  printf '%s\n' "$payload" > "$FAKE_STATE/imported.sql"
  exit 0
fi
case "$payload" in
  'SELECT VERSION();') printf '8.4.8\n' ;;
  'SELECT @@GLOBAL.partial_revokes;') printf '%s\n' "${FAKE_PARTIAL_REVOKES:-0}" ;;
  *'FROM mysql.user WHERE User='*) [[ ! -e $FAKE_STATE/existing ]] && printf '0\n' || printf '1\n' ;;
  *'FROM information_schema.SCHEMATA'*) printf '1\n' ;;
  *'FROM information_schema.ROUTINES'*) [[ ! -e $FAKE_STATE/nonempty ]] && printf '0\n' || printf '1\n' ;;
  *'COALESCE(CAST(AUTO_INCREMENT'*) printf 'cmd\t20\neqLogic\t10\n' ;;
  *'SELECT TABLE_NAME FROM information_schema.TABLES'*) printf 'cmd\neqLogic\n' ;;
  'SELECT COUNT(*) FROM `jeedom_house`.`cmd`;') printf '2\n' ;;
  'SELECT COUNT(*) FROM `jeedom_house`.`eqLogic`;') printf '1\n' ;;
  'SELECT id FROM `jeedom_house`.`cmd` ORDER BY id;') printf '1\n7\n' ;;
  'SELECT id FROM `jeedom_house`.`eqLogic` ORDER BY id;') printf '3\n' ;;
  'CREATE DATABASE '* ) printf '%s\n' "$payload" > "$FAKE_STATE/provision.sql" ;;
  *) exit 82 ;;
esac
