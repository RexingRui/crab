#!/usr/bin/env bash
#
# 在服务器上以管理员身份调一次后端接口，专治「小程序用不了但事情得办」。
#
#   cd /opt/crab-order
#   ./scripts/admin-api.sh GET    '/api/specs?all=1'
#   ./scripts/admin-api.sh POST   /api/specs '{"gender":"mixed","spec_gram":1200,...}'
#   ./scripts/admin-api.sh DELETE /api/specs/3
#
# 原理：读 .env 的 AUTH_SECRET，自签一个 10 分钟有效的管理员 token
# （格式与 internal/auth 一致），带着它调接口。没有新增任何鉴权旁路——
# 能跑这个脚本的人本来就能读到 AUTH_SECRET，权限没有被放大。
#
# 参数：
#   $1  方法：GET / POST / PUT / DELETE
#   $2  路径：/api/... （带查询串时记得整个用单引号包起来）
#   $3  请求体 JSON，可省略
# 环境变量：
#   API 后端地址，默认 http://127.0.0.1:8080（compose 把它映射在回环地址上）
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

die() { echo "✗ $*" >&2; exit 1; }

METHOD="${1:-}"
REQ_PATH="${2:-}"
BODY="${3:-}"
[ -n "$METHOD" ] && [ -n "$REQ_PATH" ] || die "用法：$0 <METHOD> </api/路径> [JSON 请求体]"

[ -f .env ] || die "当前目录没有 .env，请在项目目录（/opt/crab-order）里执行"
command -v openssl >/dev/null || die "需要 openssl"
command -v curl >/dev/null || die "需要 curl"

# 只挑需要的键，不 source .env——那等于把配置文件当脚本执行。
# 同名键以最后出现的为准，和后端 internal/config 的解析规则一致。
envval() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2- ; }

SECRET="$(envval AUTH_SECRET)"
[ -n "$SECRET" ] || die "读不到 AUTH_SECRET"

# ADMIN_OPENIDS 为空时后端是引导模式，不校验白名单，给个占位的就行
OPENID="$(envval ADMIN_OPENIDS | cut -d, -f1)"
[ -n "$OPENID" ] || OPENID="bootstrap"

API="${API:-http://127.0.0.1:8080}"

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '=' ; }

# 只用来跑这一条命令，10 分钟就过期，别拿它当长期凭证
EXP=$(( $(date +%s) + 600 ))
PAYLOAD="$(printf '{"openid":"%s","exp":%s}' "$OPENID" "$EXP" | b64url)"
SIG="$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" -binary | b64url)"

set -- -sS -X "$METHOD" "${API}${REQ_PATH}" -H "Authorization: Bearer ${PAYLOAD}.${SIG}"
[ -n "$BODY" ] && set -- "$@" -H 'Content-Type: application/json' -d "$BODY"

curl "$@"
echo
