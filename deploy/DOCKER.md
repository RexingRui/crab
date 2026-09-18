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
TCP 通了但 TLS 没握上手，先跑排查脚本，它会直接指出是哪一层：

```bash
cd /opt/crab-order && ./scripts/tls-check.sh
```

按出现频率，原因就这么几种：

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
