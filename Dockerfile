FROM golang:1.26-alpine AS builder

# BuildKit supplies these on multi-arch builds: cross-compile the Go binary
# natively on the builder arch instead of running the whole compile under QEMU.
ARG TARGETOS
ARG TARGETARCH

# Optional module-proxy override for restricted networks (CI leaves it unset
# and keeps the Go default): --build-arg GOPROXY_MIRROR=https://goproxy.cn,direct
ARG GOPROXY_MIRROR
RUN if [ -n "${GOPROXY_MIRROR:-}" ]; then go env -w GOPROXY="$GOPROXY_MIRROR"; fi

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o afree-proxy .

FROM alpine:3.21

# Optional apk mirror override (CI unset = official CDN):
# --build-arg APK_MIRROR=mirrors.tuna.tsinghua.edu.cn
ARG APK_MIRROR
RUN set -eux; \
    if [ -n "${APK_MIRROR:-}" ]; then sed -i "s#dl-cdn.alpinelinux.org#$APK_MIRROR#g" /etc/apk/repositories; fi; \
    apk add --no-cache ca-certificates tzdata su-exec \
    && addgroup -S app && adduser -S app -G app

WORKDIR /app
# COPY --chown 而非事后 chown -R：在建层时就把属主写对，避免 chown 生成
# 一整层大体积二进制的副本。
COPY --from=builder --chown=app:app /build/afree-proxy .
COPY --chown=app:app entrypoint.sh /entrypoint.sh

RUN mkdir -p /app/data \
    && chown app:app /app/data \
    && chmod 755 /entrypoint.sh

# 不再固定 USER app：entrypoint 以 root 完成数据目录属主自愈（PUID/PGID，
# 默认 100:101 与 app 用户一致）后降权运行，NAS bind-mount 无需手动 chown
EXPOSE 3457

VOLUME ["/app/data"]

# 容器内数据目录固定为 /app/data（可用 DATA_DIR 覆盖）
ENV DATA_DIR=/app/data
ENV PORT=3457

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:${PORT}/health || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD []
