#!/usr/bin/env bash
#
# 在服务器上签一条买家登记链接，打印出来直接发给买家。
#
#   cd /opt/crab-order && ./scripts/reg-link.sh "老张介绍"
#   https://你的域名/r?t=eyJqdGk...
#
# 平时这件事在小程序「记一笔」页点一下就行；这个脚本是给「小程序还没发版」
# 或者「手边只有 ssh」时用的后路，不是日常流程。
#
# 参数：
#   $1              给这位买家预填的备注名（可不填），买家那头改不了
# 环境变量：
#   API             后端地址，默认 http://127.0.0.1:8080（compose 把它映射在回环地址上）
#   CRAB_DOMAIN     买家页域名，默认读 .env 里的同名键
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

die() { echo "✗ $*" >&2; exit 1; }

[ -f .env ] || die "当前目录没有 .env，请在项目目录（/opt/crab-order）里执行"
command -v openssl >/dev/null || die "需要 openssl"
command -v curl >/dev/null || die "需要 curl"

# 只挑需要的几个键，不 source .env——那等于把配置文件当脚本执行。
# 同名键以最后出现的为准，和后端 internal/config 的解析规则保持一致。
envval() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2- ; }

SECRET="$(envval AUTH_SECRET)"
[ -n "$SECRET" ] || die "读不到 AUTH_SECRET"

# ADMIN_OPENIDS 为空时后端是引导模式，不校验白名单，openid 随便给一个占位的就行
OPENID="$(envval ADMIN_OPENIDS | cut -d, -f1)"
[ -n "$OPENID" ] || OPENID="bootstrap"

DOMAIN="${CRAB_DOMAIN:-$(envval CRAB_DOMAIN)}"
[ -n "$DOMAIN" ] || die "读不到 CRAB_DOMAIN，用 CRAB_DOMAIN=你的域名 $0 也行"

API="${API:-http://127.0.0.1:8080}"

# 备注名会拼进 JSON，先把能破坏结构的字符去掉
REMARK="$(printf '%s' "${1:-}" | tr -d '"\\')"

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '=' ; }

# 自签一个管理员 token：格式与 internal/auth 一致，只用来调下面这一个接口，
# 10 分钟就过期，不要拿它当长期凭证。
EXP=$(( $(date +%s) + 600 ))
PAYLOAD="$(printf '{"openid":"%s","exp":%s}' "$OPENID" "$EXP" | b64url)"
SIG="$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" -binary | b64url)"
ADMIN_TOKEN="$PAYLOAD.$SIG"

RESP="$(curl -fsS -X POST "$API/api/reg-links" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"remark\":\"$REMARK\"}")" \
    || die "调 $API/api/reg-links 失败，服务起来了吗？（curl $API/healthz 看看）"

# 后端返回 {"code":0,...,"data":{"token":...,"path":"/r?t=...","expires_at":...}}
LINK_PATH="$(printf '%s' "$RESP" | grep -o '"path":"[^"]*"' | cut -d'"' -f4)"
[ -n "$LINK_PATH" ] || die "响应里没有 path，后端返回的是：$RESP"

EXPIRES="$(printf '%s' "$RESP" | grep -o '"expires_at":"[^"]*"' | cut -d'"' -f4)"

echo "https://${DOMAIN}${LINK_PATH}"
[ -n "$EXPIRES" ] && echo "（有效期到 $EXPIRES，一条链接只能登记一单）" >&2
exit 0
