#!/bin/sh
set -eu
# Only the isolated throwaway database and credentials used by run-mysql84-smoke.mjs.
export MYSQL_PWD=local-smoke-only
mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names <<'SQL'
SELECT VERSION();
SHOW STATUS LIKE 'Ssl_cipher';
SQL
# Use Jeedom's actual command names, without bootstrapping backup.php/restore.php.
mysqldump --host=127.0.0.1 --user=jeedom_smoke --ssl --single-transaction --routines --triggers --events jeedom_test > /tmp/jeedom-smoke-backup.sql
mysql --host=127.0.0.1 --user=jeedom_smoke --ssl jeedom_restore < /tmp/jeedom-smoke-backup.sql
rows=$(mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names -e 'SELECT COUNT(*) FROM jeedom_restore.history;')
tables=$(mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='jeedom_restore';")
test "$rows" = 6
test "$tables" = 31
source_fingerprint=$(mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names -e "SELECT GROUP_CONCAT(CONCAT(cmd_id,'/',datetime,'/',value) ORDER BY cmd_id,datetime SEPARATOR '|') FROM jeedom_test.history;")
restored_fingerprint=$(mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names -e "SELECT GROUP_CONCAT(CONCAT(cmd_id,'/',datetime,'/',value) ORDER BY cmd_id,datetime SEPARATOR '|') FROM jeedom_restore.history;")
test "$source_fingerprint" = "$restored_fingerprint"
source_archive=$(mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names -e "SELECT GROUP_CONCAT(CONCAT(cmd_id,'/',datetime,'/',value) ORDER BY cmd_id,datetime SEPARATOR '|') FROM jeedom_test.historyArch;")
restored_archive=$(mysql --host=127.0.0.1 --user=jeedom_smoke --ssl --batch --skip-column-names -e "SELECT GROUP_CONCAT(CONCAT(cmd_id,'/',datetime,'/',value) ORDER BY cmd_id,datetime SEPARATOR '|') FROM jeedom_restore.historyArch;")
test "$source_archive" = "$restored_archive"
printf 'CLI authenticated backup/restore: %s tables, %s history rows, exact fixture values preserved\n' "$tables" "$rows"
