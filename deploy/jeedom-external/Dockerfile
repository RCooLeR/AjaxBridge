# syntax=docker/dockerfile:1
# This exact official image is client-only: PHP 8.2 / Jeedom 4.6.1 on Bookworm.
FROM jeedom/jeedom:4.6-bookworm@sha256:2bd93edc6006a8d07f147cc1790c2553e1566e630464b35a11d6fe38171e2554

USER root
ENV DATABASE=0 \
    WEBSERVER_HOME=/var/www/html \
    JEEDOM_RUNTIME_DIR=/var/lib/jeedom-external \
    DB_WAIT_TIMEOUT=60

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    --mount=type=secret,id=build_ca,required=false \
    set -eu; \
    export DEBIAN_FRONTEND=noninteractive; \
    if [ -f /run/secrets/build_ca ]; then \
      build_ca_bundle=$(mktemp /tmp/jeedom-apt-ca.XXXXXX); \
      trap 'rm -f "$build_ca_bundle"' EXIT; \
      cat /etc/ssl/certs/ca-certificates.crt /run/secrets/build_ca > "$build_ca_bundle"; \
      chmod 0644 "$build_ca_bundle"; \
      set -- -o "Acquire::https::CaInfo=$build_ca_bundle"; \
    else set --; fi; \
    apt-get "$@" -o APT::Update::Error-Mode=any update; \
    apt-get "$@" upgrade -y; \
    if command -v mysqld || command -v mariadbd; then \
      echo 'A client-only Jeedom base image is required.' >&2; exit 1; \
    fi; \
    mkdir -p /opt/jeedom-external; \
    tar -C /var/www/html -cf /opt/jeedom-external/seed.tar .

COPY --chmod=0755 entrypoint.sh /opt/jeedom-external/entrypoint.sh
COPY configure.php healthcheck.php compat.php /opt/jeedom-external/

# No official init: it can download mutable master or bootstrap a local DB.
# The inherited /var/lib/mysql image volume is unused; do not mount a DB datadir.
ENTRYPOINT ["/opt/jeedom-external/entrypoint.sh"]
CMD ["start"]
HEALTHCHECK --interval=30s --timeout=10s --start-period=2m --retries=3 \
    CMD php -d opcache.enable_cli=0 /opt/jeedom-external/healthcheck.php
