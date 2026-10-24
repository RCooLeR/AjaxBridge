# syntax=docker/dockerfile:1
# Current official 4.6-bookworm is DATABASE=0 and has MariaDB clients only.
# Add the server for this self-contained instance; never use the house DB.
FROM jeedom/jeedom:4.6-bookworm@sha256:a84fc9b54902a8264d5281da18e7ecd2b8a0a3c6d5957a0b674a2f44eec9d0e6

USER root
ENV DATABASE=1

# Keep the supported Bookworm/PHP branch, applying its available security updates.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends mariadb-server \
    && command -v mysqld \
    && command -v mariadb-admin

# Inherit the official bootstrap and cron supervision. Use Docker-managed
# volumes with nocopy for both /var/www/html and /var/lib/mysql: NAS shared
# folder ACLs may prevent mysql/www-data from writing even after chown.
# Fresh nocopy volumes also avoid an old common.config.php skipping bootstrap.
# Compose disables database networking via cnf.

