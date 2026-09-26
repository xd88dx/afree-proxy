#!/bin/sh
# AFree Proxy 容器入口。
#
# NAS bind-mount 场景下数据目录属主常与镜像内置用户（app, 100:101）不一致，
# 导致 permission denied。本入口支持以 PUID/PGID 环境变量指定运行身份：
# 以 root 启动时先把数据目录属主自愈为 PUID:PGID，再降权执行网关 ——
# 重建容器、换宿主机用户都不再需要手动 chown。
#
#   docker run -e PUID=1000 -e PGID=1000 ...
#
# 未设置 PUID/PGID 时默认 100:101（与镜像内置 app 用户一致，兼容既有部署）。
set -eu

if [ "$(id -u)" = "0" ]; then
  PUID="${PUID:-100}"
  PGID="${PGID:-101}"
  for dir in /app/data /app/.opencode-home; do
    mkdir -p "$dir"
    chown -R "$PUID:$PGID" "$dir" 2>/dev/null || true
  done
  exec su-exec "$PUID:$PGID" /app/afree-proxy "$@"
fi

# 非 root 启动（如 compose 指定了 user:）：无从自愈，直接运行
exec /app/afree-proxy "$@"
