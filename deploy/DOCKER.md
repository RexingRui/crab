# Docker 部署（Ubuntu 24.04）

后端是纯 Go 静态二进制 + SQLite 单文件数据库，容器化只需要管好两件事：
**配置从环境变量进来**，**数据库落在挂载目录里**。

涉及的文件：

| 文件 | 作用 |
|---|---|
| `Dockerfile` | 多阶段构建，运行阶段 alpine + 静态二进制 + sqlite3 |
| `docker-compose.yml` | `api` 服务；可选的 `caddy` 服务（`proxy` profile） |
| `deploy/Caddyfile` | 容器化 Caddy 的配置：`/api` 反代后端、`/t` 托管买家查单页、`/r` 托管买家登记页 |
| `scripts/docker-backup.sh` | 宿主机 crontab 调用，在容器里做 SQLite 热备份 |
| `scripts/deploy.sh` | 更新线上：备份 → 拉代码 → 构建 → 替换 → 自检，不过则自动回滚 |
| `scripts/tls-check.sh` | 买家页打不开时逐层排查 HTTPS：域名 → DNS → 容器 → 端口 → 证书 → 页面 |

## 腾讯云轻量应用服务器

轻量云和普通 CVM 有几处不一样，装之前先过一遍。下面的结论按 **2核4G** 的实例给。

### 防火墙在控制台，不在机器里

轻量云的「防火墙」是云平台层的规则（控制台 → 实例 → 防火墙），默认只放通 22 之类的少数端口。
用容器化 Caddy 就加 TCP `80` 和 `443`（想启用 HTTP/3 再加 UDP `443`）。

注意 **Docker 发布端口会直接写 iptables，绕过机器里的 ufw**，机器内的 ufw 规则基本不作数，
控制台那道防火墙才是真正的闸门。这也正是 compose 里后端只绑 `127.0.0.1:8080` 的原因：
哪怕控制台不小心放通了 8080，公网也连不到后端。

### 镜像源：确认已生效

`golang` / `alpine` / `caddy` 这些基础镜像从 Docker Hub 直接拉，国内经常超时，所以要走腾讯云内网源。
已经配过的话跑一遍确认就行：

```bash
docker info | grep -A2 "Registry Mirrors"
# Registry Mirrors:
#  https://mirror.ccs.tencentyun.com/
docker run --rm hello-world          # 能拉能跑，说明镜像源确实通了
```

<details>
<summary>没配上的话（点开）</summary>

写 `/etc/docker/daemon.json`：

```json
{
  "registry-mirrors": ["https://mirror.ccs.tencentyun.com"],
  "log-driver": "json-file",
  "log-opts": {"max-size": "10m", "max-file": "3"}
}
```

```bash
sudo systemctl restart docker
```

这个地址是腾讯云的内网镜像源，只有腾讯云的机器能访问，走内网不占公网流量包。

</details>

### Go 依赖默认已经走国内代理

`.env.example` 里默认 `GOPROXY=https://goproxy.cn,direct`，轻量云上不用改。

### 内存：2核4G 很宽裕，不用加 swap

在服务器上直接 `docker compose build` 就行，不需要任何额外处理。跑一眼确认规格：

```bash
nproc && free -h        # 2 核 / available 3G 以上即可
```

几个参考数字：`modernc.org/sqlite` 依赖的 `modernc.org/libc` 是个大包，编译是整个流程里最吃内存的一步，
实测 4 并发峰值约 **640MB**（并发数跟 CPU 核数走，2 核只会更低）；构建完成后后端常驻内存只有几十 MB。
4G 内存下构建和运行同时进行都绰绰有余。

<details>
<summary>只有 1G / 2G 的小套餐才需要看这段（点开）</summary>

轻量云默认没有 swap，1G 套餐容易编译到一半被 OOM 杀掉。加 2G swap：

```bash
sudo fallocate -l 2G /swapfile
sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

或者干脆不在服务器上编译，本地构建好传上去（本地要和服务器同架构，轻量云基本都是 amd64）：

```bash
# 本地
docker build -t crab-order:latest . && docker save crab-order:latest | gzip > crab.tar.gz
scp crab.tar.gz root@<服务器IP>:/opt/crab-order/
# 服务器
gunzip -c crab.tar.gz | docker load && docker compose up -d      # 不加 --build
```

</details>

### 域名必须备案

微信小程序的 request 合法域名只认已备案的域名；未备案的域名解析到国内地域的机器上，
80/443 的访问也会被拦截。轻量云控制台可以直接申请备案服务码（免费），一般十来天。

备案下来之前可以先这样自测：机器上 `curl localhost:8080/healthz` 验证后端，
小程序侧用开发者工具勾「不校验合法域名」连 IP 调试。**别为了图快把 `ENV` 改成 `dev` 长期跑**——
dev 不校验 `WECHAT_SECRET` 还开 CORS，只适合本地。

### 备份：快照不能替代数据库热备

轻量云的定时快照是**磁盘级**的，SQLite 开着 WAL 时可能快照到不一致的状态，
只能当整机兜底，不能替代下面第 6 节的 `scripts/docker-backup.sh` 热备。两个都做，
再把 `backup/` 定期同步到 COS 或另一台机器；同地域走 COS 内网域名不占公网流量包。

## 0. 前置检查

Docker 已经装好的话，这一步只是确认：

```bash
docker version && docker compose version   # compose 需要 v2（docker-compose-plugin）
```

`docker compose` 不存在就 `sudo apt-get update && sudo apt-get install -y docker-compose-plugin`；
`docker` 要 sudo 才能跑就 `sudo usermod -aG docker "$USER"`（重新登录生效）。

云控制台的防火墙 / 安全组要放通：用容器化 Caddy 就放 `80` + `443`，用宿主机已有的反代就按它的来。
**后端自己不对公网开端口**（只绑 `127.0.0.1:8080`）。轻量云的防火墙位置见上一节。

## 1. 放代码与写配置

```bash
sudo mkdir -p /opt/crab-order && sudo chown "$USER" /opt/crab-order
git clone <仓库地址> /opt/crab-order
cd /opt/crab-order

cp .env.example .env
echo "AUTH_SECRET=$(openssl rand -hex 32)" >> .env
vi .env     # 填 WECHAT_APPID / WECHAT_SECRET；国内机器建议 GOPROXY=https://goproxy.cn,direct

# 确认一眼：AUTH_SECRET 是 64 位十六进制、WECHAT_SECRET 非空
grep -nE '^(ENV|AUTH_SECRET|WECHAT_APPID|WECHAT_SECRET)=' .env
```

> 模板里本来有一行空的 `AUTH_SECRET=`，追加在末尾的那行会盖掉它（同名键以后出现的为准）。
> 用 `sed -i "s|^AUTH_SECRET=.*|AUTH_SECRET=$(openssl rand -hex 32)|" .env` 原地替换也一样。

私有仓库要让服务器能 `git fetch`（第 7 节的一键更新依赖它）。用**只读部署密钥**，
别把个人 PAT 扔在生产机上：

```bash
ssh-keygen -t ed25519 -N '' -f ~/.ssh/crab_deploy    # 公钥填到 GitHub 仓库
# Settings → Deploy keys → Add deploy key，不要勾 Allow write access
cat >> ~/.ssh/config <<'CONF'
Host github.com
    IdentityFile ~/.ssh/crab_deploy
CONF
git remote set-url origin git@github.com:<你>/<仓库>.git
git fetch origin main                                 # 验证一下
```

`.env` 里这几项 Docker 部署时不用管，compose 会强制覆盖成容器里的值：

- `HTTP_ADDR` → `:8080`（容器里必须监听 `0.0.0.0`，写成 `127.0.0.1` 外面就连不上）
- `DB_PATH` → `/data/crab.db`（挂载出来的目录，容器重建数据不丢）

> `.env` 行尾不要写注释。后端的 `.env` 解析器很朴素，`ENV=prod # 注释` 会被当成值 `prod # 注释` 而启动失败。

## 2. 建数据目录并授权

镜像里以非 root 用户 `crab`（uid/gid 固定为 `10001`）运行，宿主机目录要归它，
否则 SQLite 会 `attempt to write a readonly database`：

```bash
sudo make docker-init      # 等价于 mkdir -p data backup && chown -R 10001:10001 data backup
```

## 3. 起服务

```bash
docker compose up -d --build
docker compose ps                       # STATUS 应为 healthy
curl localhost:8080/healthz
# {"code":0,"msg":"ok","data":{"status":"ok"}}
docker compose logs -f api              # JSON 结构化日志
```

首次启动会自动建表并写入 8 条当季参考价。

## 4. 拿到自己的 openid

`ADMIN_OPENIDS` 为空时后端处于**引导模式**：用小程序登录一次，openid 会随响应返回，
也会打进日志：

```bash
docker compose logs api | grep -i openid
```

填进 `.env` 的 `ADMIN_OPENIDS=` 然后重启，引导模式自动关闭：

```bash
docker compose up -d      # .env 变了，compose 会重建容器让新环境变量生效
```

> `docker compose restart` **不会**重新读取 `.env`，改完配置一律用 `up -d`。

## 5. HTTPS

微信小程序强制 HTTPS，且域名要在小程序后台配到 request 合法域名。二选一：

### 方案 A：用这里自带的 Caddy 容器（推荐，证书自动申请与续期）

```bash
echo "CRAB_DOMAIN=你的域名" >> .env     # 域名必须已解析到本机；国内主机还需已备案
docker compose --profile proxy up -d
```

它做四件事：`/api/*` 反代到 `api:8080`、`/t*` 用 `web/track/index.html` 托管买家查单页、
`/r*` 用 `web/register/index.html` 托管买家自助登记页（两个页面都带 `X-Robots-Tag: noindex`）、
其余路径一律 404。证书存在 `caddy-data` 卷里，
**别随手 `docker compose down -v`**，删了要重新申请，会撞 ACME 频率限制。

对外只宣告 HTTP/1.1 + HTTP/2，**默认不开 HTTP/3**：云控制台的防火墙默认只放通 TCP 80/443，
而 Safari 一看到 `Alt-Svc` 里的 h3 就会转去试 QUIC（UDP 443），包被静默丢掉时它不一定退回 TCP，
页面就停在「无法与服务器建立安全连接」。确实在控制台放通了 UDP 443 再打开：

```bash
echo 'CRAB_PROTOCOLS=h1 h2 h3' >> .env && docker compose --profile proxy up -d
```

起完之后验一遍，它会把域名、DNS、端口、证书、页面逐层走一遍：

```bash
./scripts/tls-check.sh
```

### 方案 B：宿主机上已经有 Nginx / Caddy

后端已经映射在 `127.0.0.1:8080`，直接反代过去即可。Nginx 大致长这样：

```nginx
server {
    listen 443 ssl;
    server_name 你的域名;
    # ssl_certificate / ssl_certificate_key 由 certbot 管理

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /t {
        add_header X-Robots-Tag noindex;
        alias /opt/crab-order/web/track/index.html;
    }

    location /r {
        add_header X-Robots-Tag noindex;
        alias /opt/crab-order/web/register/index.html;
    }
}
```

买家查单与登记接口都按 IP 限流，所以 `X-Real-IP` / `X-Forwarded-For` 一定要传，
不然所有买家会共用同一个限流桶。

## 6. 备份

WAL 模式下**不能直接 cp 数据库文件**，必须走 `sqlite3 .backup` 热备。
`scripts/docker-backup.sh` 会在容器里执行备份，文件落到宿主机的 `/opt/crab-order/backup/`，
保留最近 30 天：

```bash
./scripts/docker-backup.sh          # 先手动跑一次确认没问题
sudo crontab -e
```

```cron
0 3 * * * /opt/crab-order/scripts/docker-backup.sh >> /var/log/crab-backup.log 2>&1
```

备份只在本机意义不大，建议再用 rsync / 对象存储把 `backup/` 同步出去。

## 7. 更新与回滚

日常更新一条命令，在服务器上：

```bash
cd /opt/crab-order && ./scripts/deploy.sh      # 等价于 make deploy
```

或者从本地一把梭，不用登机器：

```bash
ssh <服务器> 'cd /opt/crab-order && ./scripts/deploy.sh'
```

它按顺序做这几件事，任何一步出问题都不会让线上停在半截状态：

| 步骤 | 失败时 |
|---|---|
| 工作区必须干净、分支必须对得上 | 直接拒绝，什么都不动 |
| 数据库热备（调 `docker-backup.sh`） | 中止部署 |
| `git fetch` + `git merge --ff-only` | 中止部署（生产机上不产生合并提交） |
| `docker compose build` | 代码退回原提交，线上仍跑旧容器 |
| `docker compose up -d` 滚动替换 | —— |
| 在容器里打 `/healthz`，最多等 60s | 打印新版本日志，回滚代码 + 回滚镜像并重启 |

可用的环境变量：`BRANCH`（默认 `main`）、`HEALTH_TIMEOUT`（默认 60 秒）、`SKIP_BACKUP=1`。

**只改了 `.env`**（比如填 `ADMIN_OPENIDS`）不用走 deploy，代码没变：

```bash
docker compose up -d
```

**手动回滚到任意历史版本**：

```bash
git reset --hard <提交号> && docker compose up -d --build
```

上一个版本的镜像会被打上 `crab-order:rollback` 留着，所以紧急回退也可以不重新构建：

```bash
docker image tag crab-order:rollback crab-order:latest
docker compose up -d --force-recreate api
```

攒了几次更新之后清一下旧镜像层（`rollback` 标签不会被清掉）：

```bash
docker image prune -f
```

### 要不要上自动部署（CD）

个人项目、单机 SQLite、更新节奏跟着小程序审核走，**不建议**做 push 即自动发布：

- 自动部署的价值在于「一天十几次发布」，这个项目一周可能一次。
- 真要 GitHub Actions 直连这台机器，得把 SSH 暴露给 GitHub 的 IP 段，或者在生产机上常驻
  self-hosted runner —— 为了省一条命令，换来一个常开的入口，不划算。
- SQLite 是单机文件，没有多实例滚动发布的问题，`deploy.sh` 这种「原地替换 + 自检 + 回滚」
  已经把该有的保护都做了。

建议的分工是 **CI 自动、CD 手动**：GitHub 上自动跑测试（见 `.github/workflows/ci.yml`），
发布由人敲一条 `deploy.sh`。等哪天真的需要自动发布了，再让 Actions 构建镜像推到
腾讯云 TCR，服务器侧改成 `docker compose pull && up -d` 即可，`deploy.sh` 的骨架不用变。

## 常见问题

**买家说页面打不开：Safari「无法与服务器建立安全连接」/ Chrome `ERR_SSL_PROTOCOL_ERROR`**

> **先查备案。** 国内主机上这个报错十有八九是域名未备案，而且它伪装得很像 TLS 故障：
> 云厂商把 HTTP 请求跳到自家拦截页（腾讯云是 `dnspod.qcloud.com/static/webblock.html`），
> 但 HTTPS 没法这么跳——要跳就得伪造证书——于是直接握不上手，浏览器就报「无法建立安全连接」。
> 拦截不是 100% 命中，漏过去的连接能正常打开，所以表现成「刷十几次才出来一次」。
>
> 一分钟确认：在**手机或外网的电脑**上访问 `http://你的域名/t`（注意是 http）。
> 跳到拦截页就是它。**服务端这边怎么查都是正常的**——本机自测的流量不经过拦截，
> 证书、Caddy、HTTP/3 全都没问题，改它们也全都没用。唯一的解法是办备案。

TCP 通了但 TLS 没握上手，先跑排查脚本，它会直接指出是哪一层：

```bash
cd /opt/crab-order && ./scripts/tls-check.sh
```

脚本会把每个地址、每条路径各打 10 次（`REPEAT=` 可调），所以**时好时坏也照得出来**。
十几次才中一次的用 `REPEAT=50 ./scripts/tls-check.sh`，它会把失败那一次 openssl 的原话留下来。

一条重要的分界线：**失败只在打开页面时出现、页面里后续的接口调用一路正常**，
说明毛病只在「新建连接的第一次完整握手」，后续请求走已建好的连接（keep-alive / 会话复用）
所以不受影响。这反过来排除了证书错、域名不匹配、未备案这类稳定因素——它们会次次失败。
该查的是浏览器为新连接做选择的那一步：挑地址（IPv4/IPv6）、挑协议（h2/h3）、以及
443 后面是不是有不止一套服务在轮流应答。


「刷新几次又能进去」是另一类原因，别和「一直打不开」混为一谈：

| 时好时坏的现象 | 原因 | 怎么修 |
|---|---|---|
| 域名有 AAAA 记录，脚本报「IPv4 握得上手，IPv6 握不上」 | Safari 的 Happy Eyeballs 优先试 IPv6。compose 的端口映射默认只绑 IPv4，AAAA 却指着这台机器，于是 IPv6 那条路没人应答 | 删掉 AAAA 记录，或让 IPv6 上也真的提供服务 |
| 多条 A 记录，脚本报其中某个地址 443 不通 | 浏览器每次随机挑一个，挑中坏的就报错 | 删掉 DNS 里过期/多余的记录，只留这台机器 |
| 脚本报 caddy 重启过很多次 | 容器在崩溃重启，每次重启的几秒内 443 没人监听 | `docker compose logs caddy` 找崩溃原因 |
| 同一地址握手 `3/5` 这种 | 443 上两个进程在抢（宿主机的 Nginx/Caddy 和容器里的 Caddy 都开着），或机器负载高握手超时 | 脚本第 2 节会列出 443 上的进程；两套反代只能留一套，或 `docker stats` 看负载 |
| 脚本报「应答的不是 Caddy」 | 中间还隔着一层：宿主机的 Nginx、云厂商 CDN / 负载均衡，或买家自己开着网络代理 | 证书归那一层管，先确定那层是谁；买家侧先让他关掉 VPN / 代理再试 |
| 以上都正常，只有取页面时概率性失败 | 公网这一段在概率性重置 | 国内机器优先怀疑未备案；也可能是宣告了 HTTP/3 而 UDP 443 不通（见上面第 5 节） |

> **在服务器上跑的结果不能当验收标准。** 本机连自己的公网 IP，流量在云内就折返了，
> 不经过运营商、云防护和备案过滤，而买家的包恰恰走那一段。所以服务器上全绿只说明
> 服务端没病。判断公网这一段必须换一台机器测（家里电脑，或手机开热点给电脑上网）：
> `REPEAT=50 ./scripts/tls-check.sh 你的域名`。

#### 用 IP 直连做对照，区分「域名被拦」和「机器不通」

默认关闭。打开后 `http://<公网IP>/diag` 会返回 `ip-ok`：

```bash
echo "CRAB_DIAG_IP=你的公网IP" >> .env
docker compose --profile proxy up -d --force-recreate caddy
```

然后在手机或电脑上分别访问，两者走同一个端口、同一个进程，只差一个域名：

| `http://<IP>/diag` | `http://<域名>/t` | 结论 |
|---|---|---|
| 通 | 不通 | 域名在链路上被拦，国内主机首先查备案 |
| 不通 | 不通 | 机器或防火墙的问题，和域名无关 |
| 通 | 通 | 链路没问题，回头查 TLS 那一层 |

注意别直接在浏览器里输 `https://<IP>`：那个 IP 上没有证书，必然报「无法建立安全连接」，
和你要排查的故障长得一模一样但毫无关系。**对照测试一律用 `http://`**。

测完把 `CRAB_DIAG_IP` 从 `.env` 删掉再 `up -d` 一次，别长期开着。登记页 `/r` 不走这个入口
（token 在 URL 里，明文 HTTP 会被看光），只放了 `/t` 和 `/diag`。

#### 时好时坏的三种，怎么修

按「先确认再动手」的顺序，每种都给了验证方法——没验证过就别认为修好了，
这种十几次才中一次的毛病，凭感觉判断「好像好了」最容易误判。

**一、域名有 AAAA 记录（IPv6）**

compose 的端口映射默认只绑 IPv4，AAAA 却指着这台机器，于是 IPv6 那条路没人应答。
Safari 每开一条新连接都要在 v4/v6 之间赛跑（Happy Eyeballs），v6 赢了就报错。

```bash
dig AAAA 你的域名 +short        # 有输出就是有 AAAA 记录
```

去 DNS 控制台（腾讯云是 DNSPod → 解析 → 记录管理）**删掉 AAAA 记录**，只留 A。
等 TTL 过期后确认：

```bash
dig AAAA 你的域名 +short        # 应该没有输出
```

真要支持 IPv6 是另一件事：Docker daemon 要开 `"ipv6": true`、端口映射绑 `[::]`、
云防火墙也要放通 v6——为了这个页面不值得，删记录就好。

**二、443 上有两套服务在应答**

宿主机上原来装的 Nginx/Caddy 没停，又起了容器化 Caddy。新连接被分到哪套是随机的，
分到没有正确证书的那套就握手失败。

```bash
ss -lntp | grep :443            # 看有几个进程
```

两套只能留一套。**留容器化 Caddy**（对应本文方案 A）：

```bash
sudo systemctl stop nginx && sudo systemctl disable nginx
docker compose --profile proxy up -d
```

**留宿主机的 Nginx**（对应方案 B），那就别起 proxy profile：

```bash
docker compose stop caddy       # 不要用 down -v，证书卷删了要重新申请
```

改完再看一眼，`:443` 上应该只剩一个进程；脚本第 4 节的证书指纹也应该只有一张。

**三、还在宣告 HTTP/3**

Safari 缓存了 `Alt-Svc` 之后，下次开页面会先试 QUIC（UDP 443），云防火墙默认不放通 UDP，
它不一定退回 TCP。

```bash
curl -sI https://你的域名/r | grep -i alt-svc      # 有 h3 就是还开着
```

新版 Caddyfile 已经默认只宣告 `h1 h2`，但**挂载进容器的配置文件改了不会自动重载**，
要强制重建容器才生效：

```bash
cd /opt/crab-order && git pull
docker compose --profile proxy up -d --force-recreate caddy
curl -sI https://你的域名/r | grep -i alt-svc      # 应该没有输出了
```

服务端关掉之后，手机上那份缓存还在，得清一次才不会继续试 QUIC：
iPhone 设置 → Safari → 高级 → 网站数据 → 找到这个域名删掉。

一直打不开的，按出现频率是这几种：

| 现象 | 原因 | 怎么修 |
|---|---|---|
| 握手阶段连接被重置，端口和证书都正常 | **域名没备案**。未备案域名解析到国内主机，443 的握手会被直接掐断 | 轻量云控制台申请备案服务码，等下来即好。备案前只能用小程序侧（开发者工具勾「不校验合法域名」）自测 |
| 证书颁发者是 `Caddy Local Authority` | Caddy 没签到公网证书，退回了自签的内部 CA，浏览器一律不认 | 看 `docker compose logs caddy`：多半是 80 没放通（HTTP-01 验不过）、域名没解析到本机，或撞了 ACME 频率限制（同域名一周 5 次，别反复重建容器） |
| 证书上的域名和链接里的域名对不上 | 发出去的链接带了 `www.` 之类的前缀，而 `CRAB_DOMAIN` 没有 | 两者必须完全一致；真要两个域名都能开，就在 `deploy/Caddyfile` 的站点地址里写成 `域名A, 域名B` |
| 只有 Safari / 只有 iPhone 打不开 | 宣告了 HTTP/3 但 UDP 443 不通 | 控制台放通 UDP 443，或确认 `CRAB_PROTOCOLS` 用默认的 `h1 h2`（见上面第 5 节） |
| `CRAB_DOMAIN` 是 IP 或没填 | Caddy 签不出证书，甚至整个容器起不来 | `.env` 里填成已解析到本机的域名，`docker compose --profile proxy up -d` |
| 域名解析到了 CDN | 证书归 CDN 管，源站这边的 Caddy 证书不生效 | 在 CDN 控制台上传/申请证书，或把解析改回源站 |

页面能开但接口报错是另一回事，看 `docker compose logs api`。

**容器反复重启，日志说 `AUTH_SECRET 必须配置，且至少 32 个字符`**
`.env` 里没填、短于 32 字符，或者行尾写了注释。`grep -n '^AUTH_SECRET' .env` 看一眼，
改完 `docker compose up -d`。

**日志说 `ENV=prod 时必须配置 WECHAT_SECRET`**
生产必须填小程序密钥；只想本地试跑可以临时 `ENV=dev`（不校验密钥并开启 CORS，别用于线上）。

**`attempt to write a readonly database` / `unable to open database file`**
`data/` 目录不是 uid `10001` 的：`sudo chown -R 10001:10001 data backup`。

**时间差 8 小时**
镜像里装了 `tzdata` 并设了 `TZ=Asia/Shanghai`，正常不会有。真出现了看日志里有没有
「时区加载失败，退回固定 +08:00」——退回的也是 +08:00，业务时间不会错。

**`docker compose build` 卡在下载 Go 依赖 / 拉不到基础镜像**
`GOPROXY` 默认已经是 `https://goproxy.cn,direct`。拉不到 `golang` / `alpine` 基础镜像
是 Docker Hub 的访问问题，配镜像加速器，见上面「腾讯云轻量应用服务器 · 镜像源」。

**构建过程中 SSH 断开 / 容器被杀，`dmesg` 里有 `Out of memory`**
2核4G 上不该出现。小内存套餐才需要加 swap 或改成本地构建后 `docker load`，
见上面「腾讯云轻量应用服务器 · 内存」。

**磁盘被日志吃满**
compose 里已经限制了 `json-file` 每个容器最多 `10m × 5`。此外 SQLite 的 `-wal` 文件
会随写入增长，属正常现象。

**怎么进容器看一眼**
```bash
docker compose exec api sh
docker compose exec api sqlite3 /data/crab.db '.tables'
```
