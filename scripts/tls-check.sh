#!/usr/bin/env bash
#
# 排查买家 H5 页打不开：Safari 的「无法与服务器建立安全连接」、Chrome 的
# ERR_SSL_PROTOCOL_ERROR / ERR_CONNECTION_RESET 都归这一类——TCP 通了但 TLS 没握上手。
# 从域名取值一路验到 HTTP 响应，最后把可疑点和对应的修法一起打出来。
#
# 在服务器上（还能看容器状态与证书日志，信息最全）：
#   cd /opt/crab-order && ./scripts/tls-check.sh
# 在自己电脑上（只从外面看，一样能定位大半问题）：
#   ./scripts/tls-check.sh 你的域名
#
# 环境变量：
#   CRAB_DOMAIN     要检查的域名，默认读 .env 里的同名键；也可以用第一个参数给
#   TIMEOUT         单次探测的超时秒数，默认 8
set -uo pipefail          # 故意不开 -e：每一项都要跑完，不能中途退出

cd "$(dirname "${BASH_SOURCE[0]}")/.."

TIMEOUT="${TIMEOUT:-8}"
SUSPECTS=()               # 攒下「可疑点 → 怎么修」，最后统一打印

ok()   { echo "  ✓ $*"; }
warn() { echo "  ! $*"; }
bad()  { echo "  ✗ $*"; }
info() { echo "    $*"; }
head_() { echo; echo "── $* ──"; }
suspect() { SUSPECTS+=("$1"); }

# ---------- 0. 域名从哪来 ----------
head_ "0. 域名"

DOMAIN="${1:-${CRAB_DOMAIN:-}}"
if [ -z "$DOMAIN" ] && [ -f .env ]; then
    # 同名键以最后出现的为准，和 internal/config 的解析规则保持一致
    DOMAIN="$(grep -E '^CRAB_DOMAIN=' .env | tail -n1 | cut -d= -f2-)"
fi
if [ -z "$DOMAIN" ]; then
    bad "没拿到域名：.env 里 CRAB_DOMAIN 是空的，也没给参数"
    info "用法：$0 你的域名"
    suspect "CRAB_DOMAIN 为空 → Caddy 的站点地址会是空的，容器根本起不来，443 上没人监听。
    在 .env 里填 CRAB_DOMAIN=你的域名，然后 docker compose --profile proxy up -d"
    exit 1
fi

# 常见的填错：带协议、带端口、带路径。Caddy 的站点地址只认裸域名。
case "$DOMAIN" in
    *://*) bad "CRAB_DOMAIN 带了协议：$DOMAIN"
           suspect "CRAB_DOMAIN 要写裸域名（crab.example.com），不要带 https://" ;;
    */*)   bad "CRAB_DOMAIN 带了路径：$DOMAIN"
           suspect "CRAB_DOMAIN 要写裸域名，不要带 / 或路径" ;;
    *:*)   bad "CRAB_DOMAIN 带了端口：$DOMAIN"
           suspect "CRAB_DOMAIN 要写裸域名，端口由 compose 的 80/443 映射决定" ;;
    *)     ok "域名：$DOMAIN" ;;
esac
DOMAIN="${DOMAIN#*://}"; DOMAIN="${DOMAIN%%/*}"; DOMAIN="${DOMAIN%%:*}"

IS_IP=0
if [[ "$DOMAIN" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    IS_IP=1
    bad "$DOMAIN 是 IP，不是域名"
    suspect "Let's Encrypt 不给 IP 签证书，Caddy 会退回自签的内部 CA，
    Safari 对自签证书就是「无法建立安全连接」。必须用已备案的域名。"
elif [[ "$DOMAIN" != *.* ]] || [[ "$DOMAIN" == *.local ]] || [[ "$DOMAIN" == localhost ]]; then
    bad "$DOMAIN 不是公网域名"
    suspect "Caddy 只对公网域名走 ACME，其它名字一律用自签的内部 CA，浏览器不认。"
fi

# ---------- 1. DNS ----------
head_ "1. DNS 解析"

resolve() {
    if command -v dig >/dev/null; then dig +short +time=3 +tries=1 "$1" A | grep -E '^[0-9.]+$'
    elif command -v host >/dev/null; then host -t A "$1" 2>/dev/null | awk '/has address/{print $NF}'
    else getent ahostsv4 "$1" 2>/dev/null | awk '{print $1}' | sort -u
    fi
}
IPS="$(resolve "$DOMAIN")"
if [ -z "$IPS" ]; then
    bad "解析不到 A 记录"
    suspect "域名没有解析记录，或解析还没生效。先把 A 记录指到这台机器的公网 IP。"
    TARGET_IP=""
else
    ok "解析到：$(echo "$IPS" | tr '\n' ' ')"
    TARGET_IP="$(echo "$IPS" | head -n1)"
fi

# 在服务器上顺手核一下解析是不是指着自己；指到别处（比如挂了 CDN）就是另一套证书了
MYIP="$(curl -fsS --max-time "$TIMEOUT" https://ifconfig.me 2>/dev/null \
        || curl -fsS --max-time "$TIMEOUT" https://api.ipify.org 2>/dev/null)"
if [ -n "$MYIP" ] && [ -n "$TARGET_IP" ]; then
    if echo "$IPS" | grep -qx "$MYIP"; then
        ok "解析指向本机公网 IP（$MYIP）"
    else
        warn "本机公网 IP 是 $MYIP，域名却解析到 $TARGET_IP"
        info "如果你是在自己电脑上跑这个脚本，这条忽略"
        suspect "域名没指向这台机器，或中间挂了 CDN / 云加速。
    挂 CDN 的话证书归 CDN 管，要在 CDN 控制台上传或申请证书，源站这边的 Caddy 证书不生效。"
    fi
fi

# ---------- 2. 容器与证书日志（仅服务器上有意义）----------
head_ "2. Caddy 容器"

ON_SERVER=0
if [ -f docker-compose.yml ] && command -v docker >/dev/null && docker info >/dev/null 2>&1; then
    ON_SERVER=1
    if [ -n "$(docker compose ps -q caddy 2>/dev/null)" ]; then
        STATE="$(docker inspect -f '{{.State.Status}}' "$(docker compose ps -q caddy)" 2>/dev/null)"
        if [ "$STATE" = "running" ]; then
            ok "caddy 容器 running"
        else
            bad "caddy 容器状态是 $STATE"
            suspect "caddy 容器没正常跑起来（$STATE）。看 docker compose logs caddy，
    最常见是 CRAB_DOMAIN 没填或 Caddyfile 语法错，容器起不来 443 上就没人监听。"
        fi

        # ACME 的失败会一条条写在日志里，比猜快得多
        ACME_ERR="$(docker compose logs --tail 800 caddy 2>/dev/null \
                    | grep -iE 'error|failed|could not|rate limit|urn:ietf:params:acme' \
                    | grep -viE 'debug' | tail -n 8)"
        if [ -n "$ACME_ERR" ]; then
            bad "caddy 日志里有报错（最近 8 条）："
            echo "$ACME_ERR" | sed 's/^/      /'
            suspect "Caddy 申请证书失败，上面的日志就是原因。常见三种：
    80 端口不通（ACME HTTP-01 验不过）→ 云控制台防火墙放通 TCP 80；
    域名没解析到本机 → 先把 A 记录改对；
    撞了 Let's Encrypt 频率限制（同域名一周 5 次）→ 等一小时再试，别反复重建容器。"
        else
            ok "caddy 日志里没有明显报错"
        fi
    else
        bad "没有 caddy 容器"
        suspect "容器化 Caddy 没启动。用宿主机自己的 Nginx/Caddy 就检查那一套；
    否则起它：docker compose --profile proxy up -d"
    fi
else
    info "不在服务器上（或 docker 不可用），跳过容器检查"
fi

# ---------- 3. 端口连通 ----------
head_ "3. 端口"

tcp_probe() { timeout "$TIMEOUT" bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; }

if [ -n "$TARGET_IP" ]; then
    if tcp_probe "$TARGET_IP" 443; then
        ok "TCP 443 可连"
    else
        bad "TCP 443 连不上"
        suspect "443 根本没通。云控制台的防火墙放通 TCP 443（轻量云的防火墙在控制台，
    机器里的 ufw 不作数），再确认 caddy 容器在跑。"
    fi
    if tcp_probe "$TARGET_IP" 80; then
        ok "TCP 80 可连"
    else
        warn "TCP 80 连不上"
        suspect "80 不通，ACME 的 HTTP-01 验证就过不了（Caddy 还能退到 TLS-ALPN，
    但 80 同时也负责 HTTP→HTTPS 跳转）。云控制台放通 TCP 80。"
    fi
fi

# ---------- 4. TLS 握手 ----------
head_ "4. TLS 握手"

if [ -n "$TARGET_IP" ] && command -v openssl >/dev/null; then
    HS="$(echo | timeout "$TIMEOUT" openssl s_client -connect "$TARGET_IP:443" \
          -servername "$DOMAIN" 2>&1)"

    if echo "$HS" | grep -q 'BEGIN CERTIFICATE'; then
        CERT="$(echo "$HS" | sed -n '/BEGIN CERTIFICATE/,/END CERTIFICATE/p' | head -n 200)"
        ISSUER="$(echo "$CERT" | openssl x509 -noout -issuer 2>/dev/null | sed 's/^issuer=//')"
        SUBJ="$(echo "$CERT"   | openssl x509 -noout -subject 2>/dev/null | sed 's/^subject=//')"
        SAN="$(echo "$CERT"    | openssl x509 -noout -ext subjectAltName 2>/dev/null | tail -n +2 | tr -d ' ')"
        NOTAFTER="$(echo "$CERT" | openssl x509 -noout -enddate 2>/dev/null | cut -d= -f2)"

        ok "拿到证书"
        info "颁发者：$ISSUER"
        info "主体：  $SUBJ"
        info "SAN：   ${SAN:-（无）}"
        info "到期：  $NOTAFTER"

        if echo "$ISSUER" | grep -qi 'caddy local authority'; then
            bad "这是 Caddy 自签的内部证书，不是公网证书"
            suspect "Caddy 没能给这个域名签到公网证书，退回了内部 CA——浏览器一律不认，
    Safari 直接报「无法建立安全连接」。原因看第 2 节的日志：多半是域名不是公网域名、
    解析没指过来，或 80/443 被防火墙挡着验证不过。"
        fi

        # 给 IP 签的证书写的是 IPAddress: 而不是 DNS:，这里不比（上面已经判过 IP 不行）
        if [ "$IS_IP" = "0" ] && [ -n "$SAN" ] && ! echo "$SAN" | grep -qiE "(^|,)DNS:(\*\.)?${DOMAIN//./\\.}(,|$)"; then
            # 通配符要单独比一下父域
            PARENT="${DOMAIN#*.}"
            if ! echo "$SAN" | grep -qiE "DNS:\*\.${PARENT//./\\.}(,|$)"; then
                bad "证书的 SAN 里没有 $DOMAIN"
                suspect "证书上的域名和访问用的域名对不上（比如证书签的是 example.com，
    买家链接却是 www.example.com）。两者必须一致：要么改 CRAB_DOMAIN，
    要么在 Caddyfile 的站点地址里把两个域名都写上。"
            fi
        fi

        if ! echo "$HS" | grep -q 'Verify return code: 0 (ok)'; then
            VR="$(echo "$HS" | grep 'Verify return code' | tail -n1)"
            warn "证书链校验没过：$VR"
            info "（本机缺根证书也会这样；以浏览器里看到的为准）"
        else
            ok "证书链校验通过"
        fi

        # Safari 会用 TLS 1.2/1.3，两个都探一下，能看出中间设备在捣乱
        for V in tls1_2:1.2 tls1_3:1.3; do
            FLAG="${V%%:*}"; LABEL="TLS ${V##*:}"
            if echo | timeout "$TIMEOUT" openssl s_client -"$FLAG" -connect "$TARGET_IP:443" \
                 -servername "$DOMAIN" >/dev/null 2>&1; then
                ok "$LABEL 握手正常"
            else
                warn "$LABEL 握不上手"
                suspect "$LABEL 握不上手。Safari 这两个版本都会用，缺一个就可能直接报
    「无法建立安全连接」。服务端若不是 Caddy（被别的反代占了 443），检查那边的 TLS 配置。"
            fi
        done
    else
        bad "握手失败，没拿到证书"
        echo "$HS" | grep -iE 'alert|error|reset|refused|timeout|no peer' | head -n 5 | sed 's/^/      /'
        if echo "$HS" | grep -qiE 'reset|Connection reset'; then
            suspect "握手阶段连接被重置。国内机器上这几乎就是域名没备案——未备案域名
    解析到国内主机，443 的 TLS 握手会被直接掐断，页面在小程序里能开（走 IP/白名单）
    在 Safari 里就是这个报错。去轻量云控制台申请备案服务码，备案下来即好。"
        else
            suspect "TLS 握手没成：443 上监听的可能不是 Caddy（被别的服务占了），
    或者证书还没签出来。服务器上 ss -lntp | grep :443 看一眼是谁在听。"
        fi
    fi
else
    info "没有 openssl 或没解析到 IP，跳过"
fi

# ---------- 5. HTTP 响应 ----------
head_ "5. 页面与接口"

curl_code() {
    curl -sS -o /dev/null -w '%{http_code}' --max-time "$TIMEOUT" "$1" 2>/dev/null
    echo "|$?"
}
explain_curl() {
    case "$1" in
        60) echo "证书校验不过（自签、过期、域名不匹配）" ;;
        35) echo "TLS 握手失败——和 Safari 报的是同一件事" ;;
        7)  echo "连不上（端口没开 / 没人监听）" ;;
        28) echo "超时（防火墙把包丢了）" ;;
        6)  echo "域名解析不了" ;;
        56) echo "收数据时连接被重置（国内机器优先怀疑未备案）" ;;
        *)  echo "curl 退出码 $1" ;;
    esac
}

FETCH_FAILED=0
for P in /r /t /api/public/specs; do
    R="$(curl_code "https://$DOMAIN$P")"
    CODE="${R%%|*}"; ERR="${R##*|}"
    if [ "$ERR" != "0" ]; then
        bad "https://$DOMAIN$P → $(explain_curl "$ERR")"
        # 前面几节可能都过了，但真去取页面时才炸——这条必须自己算一处可疑，
        # 否则汇总会误报「没发现问题」。三个路径一起挂是同一个毛病，只记一次。
        if [ "$FETCH_FAILED" = "0" ]; then
            FETCH_FAILED=1
            suspect "取 https://$DOMAIN 上的页面失败：$(explain_curl "$ERR")。
    这就是买家在 Safari 里看到的那一幕，按上面 TLS / 端口两节的结论先修；
    若那两节都正常，那么问题出在公网到这台机器的路上：防火墙、备案、或中间的 CDN。"
        fi
    elif [ "$CODE" = "200" ]; then
        ok "https://$DOMAIN$P → 200"
    else
        warn "https://$DOMAIN$P → HTTP $CODE"
        [ "$CODE" = "404" ] && suspect "$P 返回 404：Caddyfile 里的 handle 规则或静态目录挂载不对。
    容器化 Caddy 要求 web/track、web/register 挂进 /srv 下，见 deploy/DOCKER.md 第 5 节。"
    fi
done

# Alt-Svc 里有 h3 就说明在向浏览器兜售 QUIC；UDP 443 没放通时 Safari 容易卡在这儿
ALTSVC="$(curl -sSI --max-time "$TIMEOUT" "https://$DOMAIN/r" 2>/dev/null | grep -i '^alt-svc:')"
if echo "$ALTSVC" | grep -qi 'h3'; then
    warn "响应头在宣告 HTTP/3：$ALTSVC"
    suspect "服务端向浏览器宣告了 HTTP/3，而云控制台的防火墙默认只放通 TCP 80/443。
    UDP 443 不通时 Safari 可能一直卡在 QUIC 上，表现就是「无法建立安全连接」。
    要么在控制台放通 UDP 443，要么按 deploy/Caddyfile 的默认只开 h1 h2。"
fi

# 服务器上再从回环打一次：能区分「服务端本身没问题，是公网这段被挡了」
if [ "$ON_SERVER" = "1" ]; then
    LOCAL="$(curl -sS -o /dev/null -w '%{http_code}' -k --max-time "$TIMEOUT" \
             --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/r" 2>/dev/null)"
    if [ "$LOCAL" = "200" ]; then
        ok "本机回环访问 /r 正常（200）"
        info "服务端这边是好的；外面打不开就是公网这一段：防火墙、备案、或 DNS"
    else
        warn "本机回环访问 /r 拿到 $LOCAL"
    fi
fi

# ---------- 汇总 ----------
head_ "结论"
if [ ${#SUSPECTS[@]} -eq 0 ]; then
    echo "  没发现问题。若手机上仍打不开："
    echo "  - 换个网络试（4G / Wi-Fi 各来一次），排除本地 DNS 劫持"
    echo "  - Safari 设置里清一下网站数据，旧的 HSTS/证书缓存会粘住"
    echo "  - 确认发给买家的链接域名和 CRAB_DOMAIN 完全一致（大小写、有没有 www）"
else
    echo "  共 ${#SUSPECTS[@]} 处可疑："
    for i in "${!SUSPECTS[@]}"; do
        echo
        echo "  $((i+1)). ${SUSPECTS[$i]}"
    done
fi
echo
