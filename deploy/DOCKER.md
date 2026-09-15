# Docker 部署（Ubuntu 24.04）

后端是纯 Go 静态二进制 + SQLite 单文件数据库，容器化只需要管好两件事：
**配置从环境变量进来**，**数据库落在挂载目录里**。

涉及的文件：

| 文件 | 作用 |
|---|---|
| `Dockerfile` | 多阶段构建，运行阶段 alpine + 静态二进制 + sqlite3 |
| `docker-compose.yml` | `api` 服务；可选的 `caddy` 服务（`proxy` profile） |
| `deploy/Caddyfile` | 容器化 Caddy 的配置：`/api` 反代后端、`/t` 托管买家查单页 |
| `scripts/docker-backup.sh` | 宿主机 crontab 调用，在容器里做 SQLite 热备份 |

## 腾讯云轻量应用服务器

轻量云和普通 CVM 有几处不一样，装之前先过一遍。

### 防火墙在控制台，不在机器里

轻量云的「防火墙」是云平台层的规则（控制台 → 实例 → 防火墙），默认只放通 22 之类的少数端口。
用容器化 Caddy 就加 TCP `80` 和 `443`（想启用 HTTP/3 再加 UDP `443`）。

注意 **Docker 发布端口会直接写 iptables，绕过机器里的 ufw**，机器内的 ufw 规则基本不作数，
控制台那道防火墙才是真正的闸门。这也正是 compose 里后端只绑 `127.0.0.1:8080` 的原因：
哪怕控制台不小心放通了 8080，公网也连不到后端。

### 基础镜像拉不动就配腾讯云内网镜像源

`golang` / `alpine` / `caddy` 这些基础镜像从 Docker Hub 直接拉，国内经常超时。写 `/etc/docker/daemon.json`：

```json
{
  "registry-mirrors": ["https://mirror.ccs.tencentyun.com"],
  "log-driver": "json-file",
  "log-opts": {"max-size": "10m", "max-file": "3"}
}
```

```bash
sudo systemctl restart docker
docker info | grep -A2 "Registry Mirrors"
```

这个地址是腾讯云的内网镜像源，只有腾讯云的机器能访问，走内网不占公网流量包。

### Go 依赖默认已经走国内代理

`.env.example` 里默认 `GOPROXY=https://goproxy.cn,direct`，轻量云上不用改。

### 内存：2核2G 够用但没余量，1G 套餐要加 swap

`modernc.org/sqlite` 依赖的 `modernc.org/libc` 是个大包，编译比较吃内存——
实测 4 并发编译峰值约 **640MB**（并发数跟 CPU 核数走，2 核会低一些），再加上 Docker 自己的开销。
轻量云默认一般没有 swap，1G 内存的套餐容易编译到一半被 OOM 杀掉。先看一眼，没有就加 2G：

```bash
free -h
sudo fallocate -l 2G /swapfile
sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

实在不想在服务器上编译，就在本地构建好传上去（注意本地要和服务器同架构，轻量云基本都是 amd64）：

```bash
# 本地
docker build -t crab-order:latest . && docker save crab-order:latest | gzip > crab.tar.gz
scp crab.tar.gz root@<服务器IP>:/opt/crab-order/
# 服务器
gunzip -c crab.tar.gz | docker load && docker compose up -d      # 不加 --build
```

只有依赖（`go.mod` / `go.sum`）变了才会重新下载依赖，平时改代码的增量构建很快。

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

```bash
docker version && docker compose version   # 需要 compose v2（docker-compose-plugin）
```

Ubuntu 24.04 上如果 `docker compose` 不存在：

```bash
sudo apt-get update && sudo apt-get install -y docker-compose-plugin
```

`docker` 要 sudo 才能跑的话，把自己加进 docker 组（重新登录生效）：

```bash
sudo usermod -aG docker "$USER"
```

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

它做三件事：`/api/*` 反代到 `api:8080`、`/t*` 用 `web/track/index.html` 托管买家查单页
（带 `X-Robots-Tag: noindex`）、其余路径一律 404。证书存在 `caddy-data` 卷里，
**别随手 `docker compose down -v`**，删了要重新申请，会撞 ACME 频率限制。

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
}
```

买家查单接口按 IP 限流，所以 `X-Real-IP` / `X-Forwarded-For` 一定要传，
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

## 7. 升级与回滚

```bash
cd /opt/crab-order
git pull
docker compose up -d --build        # 重新构建并滚动替换容器，data/ 里的数据不动
docker image prune -f               # 清理旧镜像层
```

升级前先备份一次（第 6 节）。回滚就是 `git checkout <上一个提交> && docker compose up -d --build`。

## 常见问题

**容器反复重启，日志说 `AUTH_SECRET 必须配置，且至少 32 个字符`**
`.env` 里没填或短了，或者行尾写了注释。改完 `docker compose up -d`。

**日志说 `ENV=prod 时必须配置 WECHAT_SECRET`**
生产必须填小程序密钥；只想本地试跑可以临时 `ENV=dev`（不校验密钥并开启 CORS，别用于线上）。

**`attempt to write a readonly database` / `unable to open database file`**
`data/` 目录不是 uid `10001` 的：`sudo chown -R 10001:10001 data backup`。

**时间差 8 小时**
镜像里装了 `tzdata` 并设了 `TZ=Asia/Shanghai`，正常不会有。真出现了看日志里有没有
「时区加载失败，退回固定 +08:00」——退回的也是 +08:00，业务时间不会错。

**`docker compose build` 卡在下载 Go 依赖 / 拉不到基础镜像**
`GOPROXY` 默认已经是 `https://goproxy.cn,direct`。拉不到 `golang` / `alpine` 基础镜像
是 Docker Hub 的访问问题，配镜像加速器，见上面「腾讯云轻量应用服务器 · 基础镜像拉不动」。

**构建过程中 SSH 断开 / 容器被杀，`dmesg` 里有 `Out of memory`**
小内存套餐编译不动，加 swap 或改成本地构建后 `docker load`，见上面「腾讯云轻量应用服务器 · 内存」。

**磁盘被日志吃满**
compose 里已经限制了 `json-file` 每个容器最多 `10m × 5`。此外 SQLite 的 `-wal` 文件
会随写入增长，属正常现象。

**怎么进容器看一眼**
```bash
docker compose exec api sh
docker compose exec api sqlite3 /data/crab.db '.tables'
```
