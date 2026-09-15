#!/usr/bin/env bash
#
# 一条命令更新线上：检查 → 备份 → 拉代码 → 构建 → 滚动替换 → 自检，
# 自检不过自动回滚到上一个提交 + 上一份镜像。
#
# 在服务器上：
#   cd /opt/crab-order && ./scripts/deploy.sh
# 从本地一把梭：
#   ssh <服务器> 'cd /opt/crab-order && ./scripts/deploy.sh'
#
# 环境变量：
#   BRANCH          要部署的分支，默认 main
#   HEALTH_TIMEOUT  自检超时秒数，默认 60
#   SKIP_BACKUP=1   跳过部署前备份（不建议）
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

BRANCH="${BRANCH:-main}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
ROLLBACK_IMAGE="crab-order:rollback"

log() { echo "[$(date "+%FT%T%z")] $*"; }
die() { echo "[$(date "+%FT%T%z")] ✗ $*" >&2; exit 1; }

# ---------- 0. 前置检查 ----------
[ -f docker-compose.yml ] || die "找不到 docker-compose.yml，请在项目目录里执行"
[ -f .env ] || die "缺少 .env，见 deploy/DOCKER.md 第 1 节"

# 工作区不干净的话，git merge 和回滚都会踩到未提交的改动，直接拒绝
if [ -n "$(git status --porcelain)" ]; then
    git status --short | sed 's/^/    /' >&2
    die "工作区有未提交的改动。生产机上不要改代码，先处理干净再部署"
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" != "$BRANCH" ]; then
    die "当前在 $CURRENT_BRANCH 分支，目标是 $BRANCH。切过去，或者 BRANCH=$CURRENT_BRANCH $0"
fi

OLD_SHA="$(git rev-parse HEAD)"
log "当前版本 ${OLD_SHA:0:8}（分支 $BRANCH）"

# ---------- 1. 部署前备份 ----------
# 升级最怕的是改了库结构又想回滚，所以先留一份当天的热备。
if [ "${SKIP_BACKUP:-0}" = "1" ]; then
    log "跳过部署前备份（SKIP_BACKUP=1）"
elif [ -n "$(docker compose ps -q api 2>/dev/null)" ]; then
    log "部署前备份 ..."
    ./scripts/docker-backup.sh || die "备份失败，已中止部署"
else
    log "api 容器未运行，跳过部署前备份（首次部署）"
fi

# ---------- 2. 拉代码 ----------
# 只接受快进：生产机上不产生合并提交，回滚才有确定的落点。
log "拉取 origin/$BRANCH ..."
git fetch origin "$BRANCH" || die "git fetch 失败，检查网络或仓库凭证"
git merge --ff-only "origin/$BRANCH" || die "无法快进到 origin/$BRANCH（本地有额外提交？）"

NEW_SHA="$(git rev-parse HEAD)"
if [ "$NEW_SHA" = "$OLD_SHA" ]; then
    log "代码无变化（${NEW_SHA:0:8}），仍继续重建以应用 .env 等改动"
else
    log "更新到 ${NEW_SHA:0:8}，本次带上："
    git --no-pager log --oneline "$OLD_SHA..$NEW_SHA" | sed 's/^/    /'
fi

# ---------- 3. 留一份可回滚的镜像 ----------
if docker image inspect crab-order:latest >/dev/null 2>&1; then
    docker image tag crab-order:latest "$ROLLBACK_IMAGE"
    HAVE_ROLLBACK=1
else
    HAVE_ROLLBACK=0
fi

# ---------- 4. 构建 ----------
# 构建失败时容器还没被替换，线上跑的仍是旧版本，只需把代码退回去。
log "构建镜像 ..."
if ! docker compose build api; then
    git reset --hard "$OLD_SHA" >/dev/null
    die "构建失败，线上仍是旧版本（容器未替换），代码已退回 ${OLD_SHA:0:8}"
fi

# ---------- 5. 滚动替换 + 自检 ----------
log "替换容器 ..."
docker compose up -d api

log "自检 /healthz（最多 ${HEALTH_TIMEOUT}s）..."
DEADLINE=$(( $(date +%s) + HEALTH_TIMEOUT ))
HEALTHY=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    # 在容器里打，不依赖宿主机端口映射，用不用 caddy 都一样
    if docker compose exec -T api wget -qO- http://127.0.0.1:8080/healthz 2>/dev/null \
        | grep -q '"status":"ok"'; then
        HEALTHY=1
        break
    fi
    sleep 2
done

if [ "$HEALTHY" = "1" ]; then
    log "✓ 部署完成：${OLD_SHA:0:8} → ${NEW_SHA:0:8}"
    docker compose ps
    exit 0
fi

# ---------- 6. 自检不过 → 回滚 ----------
echo "---------- 新版本最后 50 行日志 ----------" >&2
docker compose logs --tail=50 api >&2 || true
echo "-----------------------------------------" >&2

log "自检未通过，回滚到 ${OLD_SHA:0:8} ..."
git reset --hard "$OLD_SHA" >/dev/null
if [ "$HAVE_ROLLBACK" = "1" ]; then
    docker image tag "$ROLLBACK_IMAGE" crab-order:latest
    docker compose up -d --force-recreate api
else
    docker compose build api && docker compose up -d --force-recreate api
fi
die "已回滚到 ${OLD_SHA:0:8}，按上面的日志排查后再部署"
