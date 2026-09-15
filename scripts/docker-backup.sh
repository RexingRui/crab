#!/usr/bin/env bash
#
# Docker 部署下的 SQLite 热备份：在 api 容器里执行 scripts/backup.sh，
# 备份文件落到容器的 /backup，也就是宿主机的 <项目目录>/backup。
#
# 宿主机 crontab 每天凌晨跑一次：
#   0 3 * * * /opt/crab-order/scripts/docker-backup.sh >> /var/log/crab-backup.log 2>&1
#
# 可用环境变量：
#   KEEP_DAYS   保留天数，默认 30
#   COMPOSE_DIR docker-compose.yml 所在目录，默认取本脚本的上一级目录
set -euo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$COMPOSE_DIR"

# 容器没在跑就别悄悄地什么都不做，直接报错让 cron 邮件/日志里留痕
if [ -z "$(docker compose ps -q api)" ]; then
    echo "[$(date "+%FT%T%z")] 错误：api 容器未运行，跳过备份（cd $COMPOSE_DIR && docker compose up -d）" >&2
    exit 1
fi

exec docker compose exec -T -e "KEEP_DAYS=${KEEP_DAYS:-30}" api /app/scripts/backup.sh
