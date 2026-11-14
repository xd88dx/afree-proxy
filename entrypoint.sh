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
  for dir in /app/data; do
    mkdir -p "$dir"
    chown -R "$PUID:$PGID" "$dir" 2>/dev/null || true
  done
  # 播种随镜像分发的脚本到数据目录 scripts/：数据目录无此文件或与镜像内版本不同才覆盖
  # （镜像升级自动跟随新版本；字节一致则不动，不重复覆盖）。镜像内版本
  # 为权威——数据目录里的手工改动会在下次重建时被对齐回镜像版。
  if [ -d /opt/afree ]; then
    mkdir -p /app/data/scripts
    for f in /opt/afree/*.py; do
      [ -f "$f" ] || continue
      dest="/app/data/scripts/${f##*/}"
      if [ ! -f "$dest" ] || ! cmp -s "$dest" "$f"; then
        cp "$f" "$dest"
        echo "[entrypoint] ${f##*/} 已播种/更新到数据目录 scripts/"
      fi
      chown "$PUID:$PGID" "$dest" 2>/dev/null || true
    done
  fi
  exec su-exec "$PUID:$PGID" /app/afree-proxy "$@"
fi

# 非 root 启动（如 compose 指定了 user:）：无从自愈，直接运行
exec /app/afree-proxy "$@"
