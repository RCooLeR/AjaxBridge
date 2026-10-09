# Jeedom with the existing central MySQL 8.4

This optional deployment keeps house and apartment HTML in their existing NAS
bind mounts, but uses **separate databases and users** in the existing `mysql-84`
container from `90-db`. It never shares Jeedom data between installations.

| Installation | Database/user | HTML bind | Bridge input |
| --- | --- | --- | --- |
| House | `jeedom_house` | `80-bridges/jeedom/html` | Existing house root, unchanged |
| Apartment | `jeedom_appt` | `80-bridges/jeedom-appt/html` | `jeedom_appt`, source `appt` |

The application image is the pinned official client-only Jeedom 4.6 Bookworm
image plus current Debian package updates and explicit external-DB startup. It
contains no local MariaDB server. Do not add `/var/lib/mysql` bind mounts to these
services. MySQL data stays in the existing `90-db` stack, unchanged.

`compose.example.yaml` is staged beside the NAS `80-bridges/docker-compose.yaml`
as `docker-compose.jeedom-mysql84.yaml`. It uses project `bridges` and the existing
external network `ugos_shared`; the DB address is `db-mysql-84:3306`, not the NAS
published port. The original Compose remains available for rollback. Never run
both Compose definitions for the same Jeedom service at once.
This two-service file is not a replacement for the complete bridges stack: use
the explicit `-f` commands below, without `--remove-orphans`. It does not manage
or restart Ajax bridges or other services. Do not launch a second UGOS project
with the same container names.

## Runtime and MySQL 8.4 compatibility

The custom entrypoint starts Apache, cron and atd only after checking the mounted
Jeedom installation and its intended database. It does not start a database,
download Jeedom, restore backups, or create an empty installation implicitly.
Fresh installation is the explicit `seed` / `bootstrap` workflow below.

Private configuration/bootstrap work uses umask `0077`; long-running services
use `0022` so Jeedom can read root-owned plugin PID files. An overly restrictive
service umask makes MQTT Manager report `NOK` even while its daemon is connected.
Existing credential-file permissions are not relaxed.

Three narrow, backed-up changes to Jeedom's `DB.class.php` are required for the
tested core: omit unsupported `ASC` on `FULLTEXT` indexes; compare integer types
without obsolete display widths so consistency checks do not repeatedly rebuild
unchanged columns; and disable `ONLY_FULL_GROUP_BY` for the Jeedom connection
only. Actual column types, strict SQL modes and the central MySQL server's global
settings remain unchanged. The patcher checks the source shape, preserves
original files outside the webroot, and fails if a core version cannot be safely
patched. Other applications on MySQL are unaffected.

Health checks verify the expected DB credentials/schema, compatibility patch,
and the actual Jeedom login page. A core update or backup restore may overwrite
the patch: stop that Jeedom instance, run `configure`, `compat`, and `check`, then
start it again as shown below. Never ignore an unhealthy compatibility check.

## Preparation only: no Jeedom startup

Run the following from the actual Linux NAS `80-bridges` directory, not from the
Windows SMB path. Keep both backup archives and secrets private: they contain
credentials, Ajax keys, MQTT settings and automation configuration. Do not send
their contents to support or commit them.

```bash
(
set -euo pipefail
umask 077
mkdir -p secrets
chmod 700 secrets
# Run each only if its file does not already exist; never rotate a working secret.
(set -o noclobber; openssl rand -hex 32 > secrets/jeedom-house-db-password)
(set -o noclobber; openssl rand -hex 32 > secrets/jeedom-appt-db-password)
chmod 600 secrets/jeedom-house-db-password secrets/jeedom-appt-db-password

docker compose -f docker-compose.jeedom-mysql84.yaml build jeedom jeedom-appt
bash ./build/jeedom-external/migrate.sh provision house
bash ./build/jeedom-external/migrate.sh provision appt
)
```

The helper reads the central container's existing root secret internally. It
creates only the selected new schema, with `utf8mb4_unicode_ci`, and a new user
granted privileges on that schema alone. It refuses an existing schema or user;
it never drops databases, changes another password, or changes global grants.
Do not reuse the unrelated DB stack's existing application user/database.

If provisioning reports a partial failure, stop and inspect that new schema/user
with an administrator. Do not bypass the existing-object guard or automatically
drop a partially created/imported database. A corrected migration needs a
deliberate recovery decision.

## House: fresh target first, then restore the VM backup

The current house Docker installation is not the authoritative VM database.
Do not treat its failed/partly initialized local MariaDB as a migration source.
Retain the VM and a verified full Jeedom backup before doing anything else.

1. In the original VM, create and download a **full Jeedom backup**. Keep a VM
   snapshot too. The full backup preserves `eqLogic` and `cmd` IDs; recreating
   devices manually can change/reuse IDs and make the existing bridge catalog
   refer to the wrong devices.
2. Stop the current house Docker container before changing its mounted config:
   `docker stop jeedom`. Leave its HTML/MySQL directories and old image intact.
   Keep a private copy of its current HTML/config, even if it is only a fresh
   install. Never delete `common.config.php` to force the official initializer.
3. If the house HTML bind is completely empty, explicitly seed it from the
   pinned image. `seed` refuses a nonempty directory; skip it for the already
   complete house HTML tree.
4. Bootstrap only the new empty central schema, then start the new house service:

   ```bash
   # Only for a truly empty HTML bind:
   docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom seed
   # Existing complete HTML is accepted, but the selected SQL schema MUST be empty.
   docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom bootstrap
   docker compose -f docker-compose.jeedom-mysql84.yaml up -d --no-deps jeedom
   ```

   The bootstrap uses Jeedom's initial administrator credentials. Change the
   initial password immediately and keep the fresh target off public access
   until the VM backup has been restored and access checked.

5. Before using Jeedom's built-in full-backup restore on this target, **stop the
   old VM's Jeedom services/plugin daemons**. Restore can restart Jeedom and
   daemons automatically. Two active copies would receive/poll/control the same
   Ajax hub and publish duplicate house MQTT data. Keep the VM stopped after
   migration; do not leave it as a second active house installation.
6. Restore the VM's full backup through Jeedom's normal backup/restore UI. **Always
   stop and reconfigure/reapply compatibility immediately after this restore**:
   it can overwrite the patched `DB.class.php` even if it preserves DB settings.
   Do not accept the installation before these checks pass:

   ```bash
   docker stop jeedom
   docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom configure
   docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom compat
   docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom check
   docker compose -f docker-compose.jeedom-mysql84.yaml up -d --no-deps jeedom
   ```

   Verify the DB connection points to `jeedom_house` on `db-mysql-84`, not
   `localhost`, and compare equipment/command IDs with the VM. The startup
   `configure` command preserves other configuration and backs up the old file;
   it changes only the final DB settings. The `compat` command applies the small
   reviewed core compatibility patch to the restored version or fails closed if
   that version cannot be safely patched.
   The native restore's progress UI does not reliably surface a failed SQL
   client import. Compare table row counts, equipment/command IDs and sequence
   counters with the source as well as testing login; an "OK" message alone is
   not acceptance of the migration.
7. Confirm the customized Ajax plugin is still installed. If required, use the
   existing reviewed `Jeedom/tools/install-patch.sh` workflow against the house
   plugin, not a copied apartment configuration. The current callback numeric
   value issue also needs its separately reviewed fix: applying an older metric
   overlay alone is not proof callbacks work. Check Ajax login, callback URL,
   MQTT Manager house root, bridge catalog and HA values before retiring the VM.

The alternative `dump-vm house /absolute/private/new-backup` phase can create a
quiesced SQL+HTML migration bundle directly on the VM, as root with local
socket-admin access. It refuses running Apache/nginx, cron/atd, Node/PHP writers.
Stop those services and Ajax/MQTT daemons yourself first; leave MariaDB running.
The command never stops services. Its bundle is not Jeedom's native UI backup
format: import it only with this helper into an **unbootstrapped empty** target,
restore its HTML separately into a verified empty dedicated bind, then run
`configure`/`compat`/`check`. Do not import a native Jeedom backup archive through
this helper.

## Apartment: migrate the working local MariaDB, preserving every ID

Do not bootstrap the apartment target: importing requires the provisioned schema
to contain **zero objects**. Do not reinstall/discover its devices. First record
its current command/equipment IDs and keep a native full Jeedom backup as a
second recovery option.

```bash
(
set -euo pipefail
umask 077
# Quiesce its complete application before opening the source DB independently.
docker stop jeedom-appt

# Run from the Linux NAS 80-bridges directory, outside both source mounts.
# The per-migration directory must not exist yet; keep the bundle private.
mkdir -p backups
chmod 700 backups
task_backup="$(pwd -P)/backups/jeedom-appt-migration-$(date +%Y%m%d-%H%M%S)"
bash ./build/jeedom-external/migrate.sh dump appt jeedom-appt "$task_backup"
bash ./build/jeedom-external/migrate.sh import appt "$task_backup"
bash ./build/jeedom-external/migrate.sh verify appt "$task_backup"

# These one-off commands do NOT start Apache, cron, Ajax or MQTT daemons.
docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom-appt configure
docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom-appt compat
docker compose -f docker-compose.jeedom-mysql84.yaml run --rm --no-deps jeedom-appt check

# Start only after every preceding check succeeds.
docker compose -f docker-compose.jeedom-mysql84.yaml up -d --no-deps jeedom-appt
)
```

`dump` reuses the stopped original container's exact image and mounts in a
temporary **DB-only, network-isolated** container. It runs MariaDB with networking
and scheduled SQL events disabled, never the Jeedom initializer, Apache or cron.
It stops/removes only this temporary labelled container; never the original or
its volumes. Starting MariaDB can perform normal recovery writes to its old
datadir, so keep the native backup/snapshot before this step. The source mount
must already be usable by MariaDB: this is not a permission-repair command.

The bundle retains the original SQL bytes and full HTML archive plus an exact
checksum manifest. Only the MariaDB-specific first-line sandbox directive is
removed in the portable SQL copy. Tables/counts, ordered `eqLogic`/`cmd` IDs and
all table `AUTO_INCREMENT` values are checked before/after dumping and after
import. Source changes, checksum failures, foreign instance bundles,
views/triggers/routines/events, or nonempty targets fail closed. Imports use the
schema-scoped application account, not root; no `--force` or automatic retries
are used. If an import fails, keep both sources stopped and investigate the
partially imported target instead of rerunning it over existing data.

The helper expects the source DB name `jeedom`, target container `mysql-84`, and
private application secret in `./secrets/jeedom-<instance>-db-password`. Explicit
overrides are `JEEDOM_SOURCE_DATABASE`, `JEEDOM_TARGET_DB_CONTAINER`,
`JEEDOM_PASSWORD_FILE`; VM HTML can use `JEEDOM_SOURCE_HTML`. Overrides do not
change the fixed target schema/user or its per-instance guard.

## Verify before accepting either migration

- No DB logs mentioning collation, invalid defaults, authentication or missing
  tables. This cross-engine migration must pass the actual MySQL 8.4 import and
  PDO login checks; it is not a claim of universal Jeedom/MySQL compatibility.
- Equipment and command IDs unchanged. Same customized Ajax plugin, separate
  Ajax credentials/hub permissions, callbacks and MQTT Manager roots as before.
- House bridge identity remains unchanged. Apartment uses source `appt`, its
  `jeedom_appt` input root and account `A4488`; no copied house MQTT/callback/hub
  configuration is introduced into the apartment.
- Fresh real Jeedom values reach the correct bridge and HA devices, with no
  duplicate Jeedom daemons. Do not test by switching relays or valves unless
  explicitly intended.
- Both native backup and immutable migration bundle remain private and retained.
  Do not remove the old VM, source DB mounts/images or original Compose until the
  migrated installation has been accepted. Rollback requires stopping the new
  app first and deliberately restoring its old DB config before starting the
  old source; never activate old and new simultaneously.

Never use `docker compose down -v`, delete DB directories, alter the existing
`90-db` stack, or globally enable legacy MySQL authentication for this workflow.
