#!/usr/bin/env bash
# AjaxBridge installation helper, MIT (../LICENSE.MIT).
# Offline: no Jeedom bootstrap, database access, sync, refresh or network calls.
set -euo pipefail

usage() {
  printf '%s\n' 'Usage: bash install-patch.sh --check|--apply PACKAGE PLUGIN_DIR [BACKUP_DIR]'
  printf '%s\n' 'Install the official ajaxSystem plugin normally first. Disable it before --apply.'
}
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

if [[ ${1:-} == --help ]]; then usage; exit 0; fi
[[ $# -ge 3 && $# -le 4 ]] || { usage >&2; exit 2; }
mode=$1
[[ $mode == --check || $mode == --apply ]] || { usage >&2; exit 2; }
for command in php realpath find mktemp cp mkdir tar stat chmod chown dirname rm; do
  command -v "$command" >/dev/null || fail "Missing command: $command"
done
package=$(realpath -e -- "$2")
plugin_dir=$(realpath -e -- "$3")
[[ $plugin_dir == */plugins/ajaxSystem ]] || fail 'Target must be a complete plugins/ajaxSystem directory'
[[ $package != "$plugin_dir" && $package != "$plugin_dir/"* && $plugin_dir != "$package/"* ]] || fail 'Package and installed plugin must be separate directories'
[[ -f $package/verify.php && -f $package/SHA256SUMS && -f $package/manifest.json && -d $package/files ]] || fail 'Expected the whole verified Jeedom patch bundle'
[[ -f $plugin_dir/plugin_info/info.json && -f $plugin_dir/core/config/devices/Relay.json ]] || fail 'Install the complete official Ajax Systems plugin first'
[[ -z $(find "$package" "$plugin_dir" -type l -print -quit) ]] || fail 'Symlinks require a manual installation review'

php "$package/verify.php"
php "$package/verify.php" --base "$plugin_dir"

# The checksum verifier validates relative names before this manifest is used.
overlay=()
while IFS= read -r line; do
  relative=${line#*  }
  if [[ $relative == files/* ]]; then overlay+=("${relative#files/}"); fi
done < "$package/SHA256SUMS"
[[ ${#overlay[@]} -gt 0 ]] || fail 'Bundle contains no plugin overlay files'
for relative in "${overlay[@]}"; do
  [[ $relative != /* && $relative != *..* && $relative != *\\* ]] || fail 'Unsafe overlay path'
  [[ -f $package/files/$relative ]] || fail "Missing overlay file: $relative"
  if [[ $relative == *.php ]]; then php -l "$package/files/$relative"; fi
done

# Validate a private copy of the complete installed plugin before any live write.
stage=$(mktemp -d /tmp/ajaxbridge-jeedom-patch.XXXXXXXX)
cleanup() {
  # Remove only the exact generated temporary directory under /tmp.
  if [[ $stage == /tmp/ajaxbridge-jeedom-patch.* && -d $stage && ! -L $stage ]]; then
    rm -rf -- "$stage"
  fi
}
trap cleanup EXIT
cp -a -- "$plugin_dir" "$stage/ajaxSystem"
for relative in "${overlay[@]}"; do
  mkdir -p -- "$(dirname -- "$stage/ajaxSystem/$relative")"
  cp -- "$package/files/$relative" "$stage/ajaxSystem/$relative"
done
php "$package/verify.php" --installed "$stage/ajaxSystem"
php "$package/validation/telemetry.php" "$stage/ajaxSystem"

printf 'Verified %s overlay files; original target: %s\n' "${#overlay[@]}" "$plugin_dir"
if [[ $mode == --check ]]; then
  printf '%s\n' 'Check complete; installed plugin unchanged. Disable Ajax Systems before --apply.'
  exit 0
fi
[[ $EUID -eq 0 ]] || fail '--apply must run as root to preserve the installed plugin ownership'
backup_dir=${4:-$(dirname -- "$(dirname -- "$plugin_dir")")/backup/ajaxbridge-patches}
mkdir -p -- "$backup_dir"
backup_dir=$(realpath -e -- "$backup_dir")
[[ $backup_dir != "$plugin_dir" && $backup_dir != "$plugin_dir/"* && $backup_dir != "$package" && $backup_dir != "$package/"* && $backup_dir != / ]] || fail 'Backup directory must be separate from the plugin and package'
backup=$(mktemp "$backup_dir/ajaxSystem-original-XXXXXXXX.tar.gz")
chmod 600 -- "$backup"
tar -czf "$backup" -C "$plugin_dir" .
tar -tzf "$backup" >/dev/null
printf 'Original plugin backup: %s\n' "$backup"

owner=$(stat -c '%u:%g' "$plugin_dir")
for relative in "${overlay[@]}"; do
  destination_dir=$(dirname -- "$plugin_dir/$relative")
  missing_dirs=()
  while [[ ! -d $destination_dir ]]; do
    missing_dirs+=("$destination_dir")
    destination_dir=$(dirname -- "$destination_dir")
  done
  mkdir -p -- "$(dirname -- "$plugin_dir/$relative")"
  for destination_dir in "${missing_dirs[@]}"; do
    chown "$owner" -- "$destination_dir"
    chmod 755 -- "$destination_dir"
  done
  cp -- "$package/files/$relative" "$plugin_dir/$relative"
  chown "$owner" -- "$plugin_dir/$relative"
  chmod 644 -- "$plugin_dir/$relative"
done
php "$package/verify.php" --installed "$plugin_dir"
printf '%s\n' 'Patch installed. Keep Ajax Systems disabled until apartment-only access and callback URL are configured.'
printf '%s\n' 'No equipment synchronization, refresh, action, database migration or service restart was performed.'
