# Local MySQL 8.4 compatibility smoke test

Run with Node.js 22+ and a **local** Docker engine. Build the external Jeedom runtime image first. `--source-dir` can point at an existing Jeedom HTML tree, or a snapshot containing `DB.class.php`, `history.class.php`, and `database.json`.

```powershell
docker --context desktop-linux build -t ajaxbridge-jeedom-external:mysql84-test deploy/jeedom-external
```

Use unpatched Jeedom source (or the patcher's preserved original) to exercise both patch application and database behavior. The default tests the patcher packaged in the built image. For an inner-loop test before building, `--runtime-image ajaxbridge-jeedom-appt:4.6-bookworm --patcher-dir deploy/jeedom-external` explicitly mounts the current patcher instead.

```powershell
node deploy/jeedom-external/tests/run-mysql84-smoke.mjs --source-dir '\\192.168.100.100\docker\volumes\80-bridges\jeedom-appt\html'
```

The harness copies only those three non-secret files into a newly created local temporary directory. It does not mount or modify the input HTML tree, read production DB credentials, run Jeedom bootstrap/daemons, expose MySQL ports, or connect to the NAS database. It refuses a remote Docker endpoint and removes only its own exact disposable container and temporary directory.

Checks cover:

- `compat.php --check` is read-only, the original is backed up outside the fixture webroot, and applying twice is byte-idempotent.
- Actual Jeedom schema generation creates all 31 tables and their FULLTEXT indexes. Ordinary indexes keep their existing sort direction. Immediately checking that schema again must propose zero column/index alterations (MySQL 8.4 omits legacy integer display widths). Thirteen metadata comparisons ensure signedness, storage type, decimal precision/scale, varchar length and ZEROFILL differences still trigger real repairs without altering their DDL.
- The patched Jeedom PDO connection removes only `ONLY_FULL_GROUP_BY`; strict and zero-date modes survive. Independent PDO connections and global modes remain unchanged.
- The actual `history::getPlurality` day/month/year paths and eleven representative `history::all` groupings execute against live + archived fixture values without crossing command IDs.
- A new `caching_sha2_password` user connects through the runtime's real PHP driver after explicitly clearing the server authentication cache. Actual `mysql`/`mysqldump` clients export and restore 31 tables and preserve live plus archived fixture history exactly.

The CLI TLS check uses the isolated server's generated certificate. It is not a claim of production hostname/certificate validation. This test intentionally preserves legacy history grouping semantics; it does not repair pre-existing cross-year/month grouping or offset behavior. It is not a production-data migration test or a compatibility guarantee for arbitrary third-party plugins.

## Full runtime smoke test

```powershell
node deploy/jeedom-external/tests/run-runtime-smoke.mjs
```

This uses the built runtime's normal entrypoint and pinned image HTML, fresh **local** HTML/runtime bind directories, and a new MySQL 8.4 database on a Docker `--internal` network with no exposed host ports. It checks seed refusal on nonempty HTML, normal start refusal on an uninitialized DB, explicit bootstrap, refusal to bootstrap a populated schema, HTTP login rendering, health, absence of a local SQL server, graceful SIGTERM, and preservation of user/entity/command/configuration fingerprints and a DB marker across restart. No existing Jeedom files, production credentials, databases, networks, or containers are used. Its temporary local image alias, containers, network, and bind directories are removed on completion or failure.

Seeding many vendor files and the installer's filesystem permission check on a Windows bind mount can take longer than the database checks; seed allows three minutes and bootstrap ten minutes, both bounded. Jeedom's initial credentials exist only inside this isolated test and are discarded with its database. This does not log into a production installation or operate any physical device. For local debugging, `--keep-on-failure true` retains only the owned local HTML/runtime fixture (still removes its containers/network), and `--reuse-fixture <reported-path>` avoids re-extracting that seed on the next run. A successfully reused fixture is removed on completion.

Reuse is restricted to this harness's `jeedom-runtime-smoke-*` directories directly inside the local OS temporary directory, with its matching ownership marker and completed-seed flag. It always creates a fresh disposable database; it does not resume or overwrite an existing database.

## Focused service/PID permissions regression

```powershell
node deploy/jeedom-external/tests/run-pid-permissions-smoke.mjs
```

This runs the actual entrypoint source with synthetic PHP/service/cron stubs and `--network none`. Removing only service-stage `umask 0022` reproduces a live root Node process with a `0600` PID unreadable by www-data. The positive variant verifies atd, Apache and cron inherit `0022`, the PID is `0644`, and www-data can read it and check its live session. Private configuration stays directory `0700` / file `0600` in both cases. Docker calls are bounded to 30 seconds. Only test containers and temporary files are removed; no SQL, NAS files, real plugin processes, credentials or live permissions are touched.
