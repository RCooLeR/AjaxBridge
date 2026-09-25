# syntax=docker/dockerfile:1
# check=error=true

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=1970-01-01T00:00:00Z

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG COMMIT
ARG BUILD_DATE

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates git

WORKDIR /src

COPY --link go.mod go.sum ./

RUN --mount=type=cache,id=ajaxbridge-go-mod,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY --link cmd ./cmd
COPY --link internal ./internal

RUN --mount=type=cache,id=ajaxbridge-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=ajaxbridge-go-build,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -mod=readonly -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
      -o /out/ajaxbridge ./cmd/ajaxbridge

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

ARG VERSION
ARG COMMIT
ARG BUILD_DATE

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates \
    && addgroup -S ajaxbridge \
    && adduser -S -G ajaxbridge ajaxbridge \
    && mkdir -p /data \
    && chown ajaxbridge:ajaxbridge /data

WORKDIR /app

COPY --from=builder --chown=ajaxbridge:ajaxbridge /out/ajaxbridge /app/ajaxbridge
COPY --chown=ajaxbridge:ajaxbridge logo.png /app/logo.png

LABEL org.opencontainers.image.title="AjaxBridge" \
      org.opencontainers.image.description="Ajax security hub SIA DC-09 to Prometheus exporter" \
      org.opencontainers.image.url="https://github.com/RCooLeR/AjaxBridge" \
      org.opencontainers.image.source="https://github.com/RCooLeR/AjaxBridge" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"

USER ajaxbridge

EXPOSE 8080 8099

ENV AJAXBRIDGE_HTTP_ADDR=:8080 \
    AJAXBRIDGE_SIA_ADDR=:8099 \
    AJAXBRIDGE_LOG_PRETTY=false \
    AJAXBRIDGE_DEVICES_PATH=/data/devices.json \
    AJAXBRIDGE_NOTIFICATIONS_PATH=/data/notifications.json \
    AJAXBRIDGE_JEEDOM_STORE_PATH=/data/jeedom.json

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --start-interval=2s --retries=3 CMD ["/app/ajaxbridge", "healthcheck"]

ENTRYPOINT ["/app/ajaxbridge"]
