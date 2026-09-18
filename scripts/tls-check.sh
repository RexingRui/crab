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
#   REPEAT          每个地址重复握手的次数，默认 10。「刷新几次又能进」这种
#                   时好时坏的毛病，就靠这个把偶发率测出来；十几次才中一次的
#                   用 REPEAT=50 多打几轮，才抓得到那一次失败的原话
set -uo pipefail          # 故意不开 -e：每一项都要跑完，不能中途退出

cd "$(dirname "${BASH_SOURCE[0]}")/.."

TIMEOUT="${TIMEOUT:-8}"
REPEAT="${REPEAT:-10}"
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

resolve4() {
    if command -v dig >/dev/null; then dig +short +time=3 +tries=1 "$1" A | grep -E '^[0-9.]+$'
    elif command -v host >/dev/null; then host -t A "$1" 2>/dev/null | awk '/has address/{print $NF}'
    else getent ahostsv4 "$1" 2>/dev/null | awk '{print $1}' | sort -u
    fi
}
resolve6() {
    if command -v dig >/dev/null; then dig +short +time=3 +tries=1 "$1" AAAA | grep -E '^[0-9a-fA-F:]+:[0-9a-fA-F:]*$'
    elif command -v host >/dev/null; then host -t AAAA "$1" 2>/dev/null | awk '/has IPv6 address/{print $NF}'
    else getent ahostsv6 "$1" 2>/dev/null | awk '{print $1}' | sort -u
    fi
}
IPS="$(resolve4 "$DOMAIN")"
IPS6="$(resolve6 "$DOMAIN")"
ALL_IPS="$(printf '%s\n%s\n' "$IPS" "$IPS6" | grep -v '^$')"

if [ -z "$ALL_IPS" ]; then
    bad "解析不到 A / AAAA 记录"
    suspect "域名没有解析记录，或解析还没生效。先把 A 记录指到这台机器的公网 IP。"
    TARGET_IP=""
else
    [ -n "$IPS" ]  && ok "A 记录：$(echo "$IPS" | tr '\n' ' ')"
    [ -z "$IPS" ]  && bad "没有 A 记录"
    [ -n "$IPS6" ] && ok "AAAA 记录：$(echo "$IPS6" | tr '\n' ' ')"
    TARGET_IP="$(echo "$ALL_IPS" | head -n1)"
fi

# 多个 A 记录时，只要有一个指着坏机器，浏览器就会「有时候能开有时候不能」
if [ "$(echo "$IPS" | grep -c .)" -gt 1 ]; then
    warn "有多条 A 记录，浏览器会随机挑一个"
    info "下面第 3、4 节会逐个地址验，只要有一个不通，就是时好时坏的原因"
fi

# AAAA 同理，而且更隐蔽：Safari 的 Happy Eyeballs 优先试 IPv6
if [ -n "$IPS6" ]; then
    warn "域名有 AAAA 记录（IPv6）"
    info "Safari 会优先走 IPv6，它不通时才退回 IPv4——退不干净就是时好时坏"
fi

# 在服务器上顺手核一下解析是不是指着自己；指到别处（比如挂了 CDN）就是另一套证书了
MYIP="$(curl -fsS --max-time "$TIMEOUT" https://ifconfig.me 2>/dev/null \
        || curl -fsS --max-time "$TIMEOUT" https://api.ipify.org 2>/dev/null)"
if [ -n "$MYIP" ] && [ -n "$TARGET_IP" ]; then
    if echo "$ALL_IPS" | grep -qx "$MYIP"; then
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
            # 反复重启的容器会有几秒钟 443 上没人听，表现就是时好时坏
            CID="$(docker compose ps -q caddy)"
            RESTARTS="$(docker inspect -f '{{.RestartCount}}' "$CID" 2>/dev/null)"
            STARTED="$(docker inspect -f '{{.State.StartedAt}}' "$CID" 2>/dev/null)"
            if [ "${RESTARTS:-0}" -gt 3 ] 2>/dev/null; then
                bad "caddy 已经重启过 $RESTARTS 次，最近一次启动于 $STARTED"
                suspect "caddy 在反复重启（$RESTARTS 次）。每次重启的那几秒 443 上没人监听，
    买家刷到那几秒就打不开、再刷一下又好了。看 docker compose logs caddy 找崩溃原因。"
            else
                info "重启 ${RESTARTS:-0} 次，最近一次启动于 $STARTED"
            fi
        else
            bad "caddy 容器状态是 $STATE"
            suspect "caddy 容器没正常跑起来（$STATE）。看 docker compose logs caddy，
    最常见是 CRAB_DOMAIN 没填或 Caddyfile 语法错，容器起不来 443 上就没人监听。"
        fi

        # 只看 warn/error 级别。Caddy 的 info 里有不少带 failed 字样却无害的话
        # （最典型的是 quic-go 那条 UDP 缓冲区提示），按关键字抓会一路误报成「证书失败」。
        CADDY_LOG="$(docker compose logs --tail 800 caddy 2>/dev/null)"
        # 有几条 warn 是 Caddy 的日常噪音，不是故障，滤掉免得淹没真问题：
        #   - "skipped because it requires TLS"：:80 上只做 HTTP→HTTPS 跳转，
        #     h2/h3 用不上本来就会跳过，和买家页无关
        #   - "Caddyfile input is not formatted"：格式提示，跑 caddy fmt 即可
        BENIGN='skipped because it requires TLS|input is not formatted'
        REAL_ERR="$(echo "$CADDY_LOG" | grep -E '"level":"(error|warn)"' \
                    | grep -vE "$BENIGN" | tail -n 8)"
        NOISE="$(echo "$CADDY_LOG" | grep -E '"level":"(error|warn)"' \
                 | grep -cE "$BENIGN")"
        [ "${NOISE:-0}" -gt 0 ] && info "另有 $NOISE 条无害的 warn（:80 跳过 h2/h3、格式提示），已忽略"
        if [ -n "$REAL_ERR" ]; then
            bad "caddy 日志里有 warn/error（最近 8 条）："
            echo "$REAL_ERR" | sed 's/^/      /'
            if echo "$REAL_ERR" | grep -qiE 'acme|certificate|obtain|challenge|rate limit'; then
                suspect "Caddy 申请证书失败，上面的日志就是原因。常见三种：
    80 端口不通（ACME HTTP-01 验不过）→ 云控制台防火墙放通 TCP 80；
    域名没解析到本机 → 先把 A 记录改对；
    撞了 Let's Encrypt 频率限制（同域名一周 5 次）→ 等一小时再试，别反复重建容器。"
            else
                suspect "caddy 日志里有 warn/error，内容见上。不一定和证书有关，先看清楚再动手。"
            fi
        else
            ok "caddy 日志里没有 warn/error"
        fi

        # quic-go 的缓冲区提示是 info 级，不是故障，但它证明 HTTP/3 监听是活的
        if echo "$CADDY_LOG" | grep -q 'receive buffer size'; then
            warn "日志里有 quic-go 的 UDP 缓冲区提示，说明 HTTP/3 监听开着"
            info "这条本身是 info 级、不是故障，但配合第 5 节的 alt-svc 一起看"
        fi
    else
        bad "没有 caddy 容器"
        suspect "容器化 Caddy 没启动。用宿主机自己的 Nginx/Caddy 就检查那一套；
    否则起它：docker compose --profile proxy up -d"
    fi
else
    info "不在服务器上（或 docker 不可用），跳过容器检查"
fi

# 宿主机自己的 Nginx/Caddy 和容器化 Caddy 同时开着，是「时好时坏」的经典原因：
# 两个进程抢同一个 443，谁先绑上谁应答，另一个可能连证书都没有。
if command -v ss >/dev/null 2>&1; then
    L443="$(ss -lntp 2>/dev/null | awk '$4 ~ /:443$/')"
    if [ -n "$L443" ]; then
        PROCS="$(echo "$L443" | grep -o '"[^"]*"' | tr -d '"' | sort -u | tr '\n' ' ')"
        if [ -n "$PROCS" ]; then
            info "443 上监听的进程：$PROCS"
            # docker-proxy 是 compose 的端口映射，正常；再冒出 nginx/apache 就是抢端口了
            if echo "$PROCS" | grep -qiE 'nginx|apache|httpd|haproxy'; then
                bad "除了容器的端口映射，宿主机上还有别的反代在 443 上"
                suspect "443 上同时有容器化 Caddy 和宿主机的反代（$PROCS）。
    两个进程抢同一个端口，谁绑上谁应答；宿主机那个多半没有这个域名的证书，
    于是有时候能开、有时候报「无法建立安全连接」——正是「刷新几次又能进」。
    二选一：停掉宿主机的反代（systemctl stop nginx && systemctl disable nginx），
    或者别用容器化 Caddy（去掉 --profile proxy），按 DOCKER.md 第 5 节方案 B 配宿主机那套。"
            fi
        fi
    else
        warn "443 上没有监听的进程"
    fi
fi

# ---------- 3. 端口连通 ----------
head_ "3. 端口"

tcp_probe() { timeout "$TIMEOUT" bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; }

DEAD_IPS=""      # 443 不通的地址，攒着最后一起说
for IP in $ALL_IPS; do
    if tcp_probe "$IP" 443; then
        ok "$IP TCP 443 可连"
    else
        bad "$IP TCP 443 连不上"
        DEAD_IPS="$DEAD_IPS $IP"
    fi
done

if [ -n "$TARGET_IP" ]; then
    if tcp_probe "$TARGET_IP" 80; then
        ok "TCP 80 可连"
    else
        warn "TCP 80 连不上"
        suspect "80 不通，ACME 的 HTTP-01 验证就过不了（Caddy 还能退到 TLS-ALPN，
    但 80 同时也负责 HTTP→HTTPS 跳转）。云控制台放通 TCP 80。"
    fi
fi

if [ -n "$DEAD_IPS" ]; then
    LIVE_COUNT="$(( $(echo "$ALL_IPS" | grep -c .) - $(echo "$DEAD_IPS" | wc -w) ))"
    if [ "$LIVE_COUNT" -gt 0 ]; then
        suspect "解析出的地址里，$DEAD_IPS 的 443 不通，另外 $LIVE_COUNT 个通。
    浏览器每次随机挑一个，挑中好的就能开、挑中坏的就报错——这正是「刷新几次又能进」。
    把 DNS 里多余/过期的那几条记录删掉，只留这台机器的地址。"
    else
        suspect "443 一个都没通。云控制台的防火墙放通 TCP 443（轻量云的防火墙在控制台，
    机器里的 ufw 不作数），再确认 caddy 容器在跑。"
    fi
fi

# ---------- 4. TLS 握手 ----------
head_ "4. TLS 握手"

# openssl 要求 IPv6 地址带方括号
hs_once() {
    local h="$1"
    case "$h" in *:*) h="[$h]";; esac
    echo | timeout "$TIMEOUT" openssl s_client -connect "${h}:443" -servername "$DOMAIN" 2>&1
}

# 每个地址打 REPEAT 次全新握手（openssl 每次都是新进程，不会复用会话，
# 正好对应浏览器「新开页面」那一下）。十几次才中一次的毛病，握一次手照不出来。
BEST_IP=""
if [ -n "$ALL_IPS" ] && command -v openssl >/dev/null; then
    for IP in $ALL_IPS; do
        PASS=0
        FAIL_SAMPLE=""       # 偶发失败时，把那一次的原始输出留下来——这才是断案的东西
        FPS=""               # 每次拿到的证书指纹，用来看是不是同一套服务在应答
        for _ in $(seq 1 "$REPEAT"); do
            OUT="$(hs_once "$IP")"
            if echo "$OUT" | grep -q 'BEGIN CERTIFICATE'; then
                PASS=$((PASS+1))
                FP="$(echo "$OUT" | sed -n '/BEGIN CERTIFICATE/,/END CERTIFICATE/p' \
                      | openssl x509 -noout -fingerprint -sha256 2>/dev/null | cut -d= -f2)"
                FPS="$FPS$FP
"
            else
                [ -z "$FAIL_SAMPLE" ] && FAIL_SAMPLE="$OUT"
            fi
        done

        if [ "$PASS" = "$REPEAT" ]; then
            ok "$IP 握手 $PASS/$REPEAT 成功"
            [ -z "$BEST_IP" ] && BEST_IP="$IP"
        elif [ "$PASS" = "0" ]; then
            bad "$IP 握手 0/$REPEAT，全败"
        else
            bad "$IP 握手 $PASS/$REPEAT——时好时坏"
            [ -z "$BEST_IP" ] && BEST_IP="$IP"
            suspect "同一个地址 $IP 上，$REPEAT 次全新握手成了 $PASS 次。
    失败只发生在新建连接那一下，页面里后续的接口调用走的是已建好的连接，所以不受影响——
    和「只有打开页面时偶发、提交之后一路正常」完全吻合。看下面那段失败样本定原因。"
        fi

        # 偶发失败的原始输出：alert 码 / reset / timeout，一眼定性
        if [ -n "$FAIL_SAMPLE" ]; then
            info "$IP 失败样本（第一次失败时 openssl 的原话）："
            echo "$FAIL_SAMPLE" | grep -iE 'alert|error|reset|refused|timeout|no peer|errno' \
                | head -n 4 | sed 's/^/        /'
            [ -z "$(echo "$FAIL_SAMPLE" | grep -iE 'alert|error|reset|refused|timeout|no peer|errno')" ] \
                && echo "$FAIL_SAMPLE" | head -n 4 | sed 's/^/        /'
        fi

        # 同一个地址上冒出两张不同的证书，说明 443 后面不止一套服务
        UNIQ_FP="$(echo "$FPS" | grep -v '^$' | sort -u | grep -c .)"
        if [ "${UNIQ_FP:-0}" -gt 1 ]; then
            bad "$IP 上先后拿到 $UNIQ_FP 张不同的证书"
            echo "$FPS" | grep -v '^$' | sort -u | sed 's/^/        /'
            suspect "同一个地址返回了 $UNIQ_FP 张不同的证书，说明 443 后面有不止一套服务在应答
    （宿主机的 Nginx/Caddy 和容器化 Caddy 都绑着，或者前面还有一层负载均衡）。
    新建连接被分到哪一套是随机的，分到没有正确证书的那套就报「无法建立安全连接」，
    而已经建好的连接不受影响——正好是你看到的现象。两套反代只能留一套。"
        fi
    done
    [ -z "$BEST_IP" ] && BEST_IP="$TARGET_IP"
fi

# v4 能通、v6 不通（或反过来）是 Safari 最典型的「有时候打不开」
if [ -n "$IPS6" ] && [ -n "$IPS" ]; then
    V4_OK=0; V6_OK=0
    hs_once "$(echo "$IPS"  | head -n1)" | grep -q 'BEGIN CERTIFICATE' && V4_OK=1
    hs_once "$(echo "$IPS6" | head -n1)" | grep -q 'BEGIN CERTIFICATE' && V6_OK=1
    if [ "$V4_OK" = "1" ] && [ "$V6_OK" = "0" ]; then
        bad "IPv4 握得上手，IPv6 握不上"
        suspect "AAAA 记录指的地址上没有正常的 HTTPS。Safari 的 Happy Eyeballs 优先试 IPv6，
    试不通才退回 IPv4，退回前那几秒就是「无法建立安全连接」，再刷新可能就走了 IPv4——
    完全对得上「刷新几次又能进」。要么把 AAAA 记录删掉，要么让 IPv6 上也正常提供服务
    （compose 的端口映射默认只绑 IPv4）。"
    elif [ "$V6_OK" = "1" ] && [ "$V4_OK" = "0" ]; then
        bad "IPv6 握得上手，IPv4 握不上"
        suspect "A 记录指的地址上没有正常的 HTTPS，只有 IPv6 是好的。纯 IPv4 网络的买家
    一直打不开，双栈的买家时好时坏。把 A 记录修对。"
    fi
fi

if [ -n "$BEST_IP" ] && command -v openssl >/dev/null; then
    HS="$(hs_once "$BEST_IP")"

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
            HOSTB="$BEST_IP"; case "$HOSTB" in *:*) HOSTB="[$HOSTB]";; esac
            if echo | timeout "$TIMEOUT" openssl s_client -"$FLAG" -connect "${HOSTB}:443" \
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
# 每条路径也打 REPEAT 次：买家说的「刷新几次又能进」就是这里的成功率不到 100%
for P in /r /t /api/public/specs; do
    # 公开接口按 IP 限流（PUBLIC_RATE_LIMIT，默认 20 次/分钟），压几十次必然撞上，
    # 那是限流在正常工作、不是故障。所以接口只打 3 次，静态页才压满 REPEAT 次。
    case "$P" in
        /api/*) TRIES=3 ;;
        *)      TRIES="$REPEAT" ;;
    esac
    PASS=0; LIMITED=0; LAST_ERR=0; LAST_CODE=""
    for _ in $(seq 1 "$TRIES"); do
        R="$(curl_code "https://$DOMAIN$P")"
        CODE="${R%%|*}"; ERR="${R##*|}"
        if [ "$ERR" = "0" ] && [ "$CODE" = "200" ]; then
            PASS=$((PASS+1))
        elif [ "$ERR" = "0" ] && [ "$CODE" = "429" ]; then
            LIMITED=$((LIMITED+1))      # 限流命中，说明链路是通的
        else
            LAST_ERR="$ERR"; LAST_CODE="$CODE"
        fi
    done

    if [ "$LIMITED" -gt 0 ] && [ "$((PASS + LIMITED))" = "$TRIES" ]; then
        ok "https://$DOMAIN$P → $PASS 次 200、$LIMITED 次 429（限流生效，不是故障）"
    elif [ "$PASS" = "$TRIES" ]; then
        ok "https://$DOMAIN$P → $PASS/$TRIES 次 200"
    elif [ "$LAST_ERR" != "0" ]; then
        bad "https://$DOMAIN$P → $PASS/$TRIES 次成功，失败时：$(explain_curl "$LAST_ERR")"
        # 前面几节可能都过了，但真去取页面时才炸——这条必须自己算一处可疑，
        # 否则汇总会误报「没发现问题」。三条路径一起挂是同一个毛病，只记一次。
        if [ "$FETCH_FAILED" = "0" ]; then
            FETCH_FAILED=1
            if [ "$PASS" = "0" ]; then
                suspect "取 https://$DOMAIN 上的页面每次都失败：$(explain_curl "$LAST_ERR")。
    这就是买家在 Safari 里看到的那一幕，按上面 TLS / 端口两节的结论先修；
    若那两节都正常，那么问题出在公网到这台机器的路上：防火墙、备案、或中间的 CDN。"
            else
                suspect "取 https://$DOMAIN 上的页面 $TRIES 次里成了 $PASS 次，失败时报
    $(explain_curl "$LAST_ERR")——和「刷新几次又能进去」完全对上。看上面第 1、3、4 节：
    多条 A/AAAA 记录里有坏的、caddy 在反复重启、或 443 上有两个进程在抢，都会这样。
    以上都正常的话，是公网这一段在概率性丢包/重置，国内机器优先怀疑未备案。"
            fi
        fi
    else
        warn "https://$DOMAIN$P → $PASS/$TRIES 次成功，失败时 HTTP $LAST_CODE"
        [ "$LAST_CODE" = "404" ] && suspect "$P 返回 404：Caddyfile 里的 handle 规则或静态目录挂载不对。
    容器化 Caddy 要求 web/track、web/register 挂进 /srv 下，见 deploy/DOCKER.md 第 5 节。"
    fi
done

# Caddy 会回 Server: Caddy。回的是别的东西，说明请求压根没到我们这套里来
SRV="$(curl -sSI --max-time "$TIMEOUT" "https://$DOMAIN/r" 2>/dev/null \
       | grep -i '^server:' | tr -d '\r' | cut -d' ' -f2-)"
if [ -n "$SRV" ]; then
    if echo "$SRV" | grep -qi 'caddy'; then
        ok "应答的是 Caddy（Server: $SRV）"
    else
        bad "应答的不是 Caddy，是 $SRV"
        suspect "回应请求的不是这套容器里的 Caddy，而是 $SRV——中间还隔着一层
    （宿主机的 Nginx、云厂商的 CDN / 负载均衡、或者你这边的网络代理）。
    证书就归那一层管，Caddy 这边配得再对也不生效。先确定那层是谁，再决定证书配在哪。"
    fi
fi

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

# 这一条必须显眼：在服务器上连本机公网 IP，流量在云内就折返了，不走
# 运营商、不过云防护、也不过备案过滤。买家的包走的是另一条路。
# 本机全绿只说明服务端没病，不等于外面能打开——别拿它当验收标准。
if [ "$ON_SERVER" = "1" ]; then
    echo "  ⚠ 以上都是在服务器上自测的结果。"
    echo "    本机连自己的公网 IP，流量在云内就折返了，不经过运营商、云防护和备案过滤，"
    echo "    而买家的包恰恰走那一段。所以上面全绿只能说明服务端没病。"
    echo "    要判断公网这一段，必须换一台机器测（家里电脑、或手机开热点给电脑上网）："
    echo "      REPEAT=50 ./scripts/tls-check.sh $DOMAIN"
    echo
fi

if [ ${#SUSPECTS[@]} -eq 0 ]; then
    if [ "$ON_SERVER" = "1" ]; then
        echo "  服务端这边没发现问题。接着往外测，别停在这里。"
    else
        echo "  从这台机器看没发现问题。若手机上仍打不开："
    fi
    echo "  - 换个网络试（4G / Wi-Fi 各来一次），排除本地 DNS 劫持"
    echo "  - 用国内多地拨测看是不是只有某些地区/运营商不通（itdog.cn 这类）"
    echo "  - 查一下域名备案状态：未备案的域名解析到国内主机，80/443 会被拦"
    echo "  - 确认发给买家的链接域名和 CRAB_DOMAIN 完全一致（大小写、有没有 www）"
else
    echo "  共 ${#SUSPECTS[@]} 处可疑："
    for i in "${!SUSPECTS[@]}"; do
        echo
        echo "  $((i+1)). ${SUSPECTS[$i]}"
    done
fi
echo
