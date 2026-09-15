#!/usr/bin/env bash
#
# SQLite 热备份。建议 crontab 每天凌晨跑一次：
#   0 3 * * * /opt/crab-order/scripts/backup.sh >> /var/log/crab-backup.log 2>&1
#
# Docker 部署下由 scripts/docker-backup.sh 在 api 容器里调用本脚本。
#
# 注意：WAL 模式下直接 cp 数据库文件可能拷到不一致的状态，
# 必须用 sqlite3 的 .backup 命令做热备。
set -euo pipefail

DB_PATH="${DB_PATH:-/opt/crab-order/data/crab.db}"
BACKUP_DIR="${BACKUP_DIR:-/backup}"
KEEP_DAYS="${KEEP_DAYS:-30}"

if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "[$(date "+%FT%T%z")] 错误：未安装 sqlite3 命令行工具" >&2
    exit 1
fi

if [ ! -f "$DB_PATH" ]; then
    echo "[$(date "+%FT%T%z")] 错误：数据库不存在 $DB_PATH" >&2
    exit 1
fi

mkdir -p "$BACKUP_DIR"
TARGET="$BACKUP_DIR/crab-$(date +%F).db"

sqlite3 "$DB_PATH" ".backup '$TARGET'"
echo "[$(date "+%FT%T%z")] 备份完成：$TARGET ($(du -h "$TARGET" | cut -f1))"

# 只保留最近 KEEP_DAYS 天
find "$BACKUP_DIR" -name 'crab-*.db' -type f -mtime "+$KEEP_DAYS" -print -delete
