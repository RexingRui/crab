# 迭代部署手册（mini-deploy）

写给「本机是 iPad、服务器是一台腾讯云」的日常发版场景。
首次部署的完整细节在 [README 的「部署」一节](README.md#部署)，这里只讲**每次迭代怎么走**。

## 谁在哪儿跑

iPad 不参与构建，它只是遥控器：浏览器点 GitHub Actions，SSH 客户端（Termius / Blink /
腾讯云控制台自带的网页终端）连服务器。真正干活的是另外三处。

```
   iPad Pro                                    你只做两件事：
   ├── 浏览器 → GitHub                          push 代码、点一下发布
   └── SSH   → 腾讯云
                     │
        ┌────────────┴─────────────┐
        ▼                          ▼
  GitHub Actions              腾讯云服务器（Ubuntu/Debian）
  构建小程序 dist/            ├── crab-server (systemd, 127.0.0.1:8080)
  miniprogram-ci 上传         ├── SQLite /opt/crab-order/data/crab.db
        │                     └── Caddy: /api → 8080, /t → 查单页, 自动 HTTPS
        ▼
  微信小程序后台
  开发版 → 体验版 → 提审 → 线上
```

**两条线互相独立**：改后端不用重发小程序，改小程序不用碰服务器。
只有接口契约变了才需要一起发，顺序是**先后端后小程序**（旧小程序要能跑在新后端上）。

## 一次性准备

做完这一节，后面每次发版就只剩「点一下」。

### 1. 腾讯云服务器

按 README 的「部署」一节装好：编译好的二进制放 `/opt/crab-order/bin/`、`.env` 放
`/opt/crab-order/.env`、`crab-order.service` 装进 systemd、Caddy 反代 `/api` 和 `/t`。
另外为了后面能在服务器上自助构建，装上 Go 和 git：

```bash
# 服务器上，一次
sudo apt update && sudo apt install -y git sqlite3
curl -fsSL https://go.dev/dl/go1.22.12.linux-amd64.tar.gz | sudo tar -C /usr/local -xz  # ≥1.22 即可
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
sudo mkdir -p /opt/crab-order/src && sudo chown "$USER" /opt/crab-order/src
git clone https://github.com/RexingRui/crab.git /opt/crab-order/src
```

源码目录用你自己的账号拿着就行，编译也用你自己跑（`crab` 这个用户是给 systemd 跑服务用的，
它只需要拥有 `/opt/crab-order/data`）。装完 Go 记得 `source /etc/profile.d/go.sh` 或重连一次 SSH。

`.env` 里必须填对的三个：`AUTH_SECRET`（`openssl rand -hex 32`）、`WECHAT_APPID` /
`WECHAT_SECRET`、`ADMIN_OPENIDS`（你自己的 openid，不填就是谁都能登的引导模式）。
`HTTP_ADDR` 保持 `127.0.0.1:8080`，外网由 Caddy 兜。

> 内存小于 2G 的轻量服务器编译 Go 可能被 OOM 杀掉（`modernc.org/sqlite` 是纯 Go 实现，
> 挺吃内存）。加 2G swap 就能过：
> `sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile`。
> 实在不想在服务器上编译，看文末的「不想在服务器上编译」。

### 2. 微信小程序后台

1. 「开发管理 → 开发设置 → 服务器域名」把 `https://你的域名` 加进 **request 合法域名**。
2. 同页面「小程序代码上传密钥」生成并下载 `private.<appid>.key`，**只能下载一次**。
3. 同页面的 **IP 白名单要关掉** —— GitHub 托管 runner 的出口 IP 不固定。
   想保留白名单就走文末的「自建 runner」。

### 3. GitHub 仓库

Settings → Secrets and variables → Actions，配四项：

| 类型 | 名字 | 值 |
| --- | --- | --- |
| Secret | `WX_APPID` | 小程序 AppID |
| Secret | `WX_PRIVATE_KEY` | `private.<appid>.key` 全文（连 BEGIN/END 行一起贴） |
| Variable | `TARO_APP_API_BASE_URL` | `https://你的域名` |
| Variable | `TARO_APP_TRACK_URL` | `https://你的域名` |

密钥文件在 iPad 上不好打开，可以在 GitHub 网页版直接把下载下来的 key 文件内容粘进 Secret 输入框。

## 日常迭代

### A. 只改了后端（Go）

```bash
# SSH 上服务器
cd /opt/crab-order/src
git pull origin main
make build                                  # 产出 /opt/crab-order/src/bin/crab-server
sudo cp /opt/crab-order/bin/crab-server /opt/crab-order/bin/crab-server.prev   # 留一手好回滚
sudo systemctl stop crab-order
sudo cp bin/crab-server /opt/crab-order/bin/crab-server
sudo systemctl start crab-order
systemctl status crab-order --no-pager      # 确认 active (running)
curl -sS localhost:8080/healthz             # 返回 {"code":0,...} 就算起来了
```

停机时间就是 `stop`→`start` 之间那一两秒。数据库不用动，表结构迁移在程序启动时自己跑。

### B. 只改了小程序

全程在 iPad 浏览器里完成，不需要 SSH：

1. 改 `miniprogram/package.json` 的 `version`（比如 `0.1.0` → `0.1.1`），提交。
   这个版本号同时是体验版版本号和「设置」页显示的版本号，一处改两处生效。
2. **打标签发布**：`git tag mp-v0.1.1 && git push origin mp-v0.1.1`，
   或者在 GitHub → Actions →「小程序上传」→ Run workflow，手动选 `upload` 并填版本号、备注。
3. Actions 跑完（`npm ci` → 单测 → 带域名构建 → 上传，约两三分钟），
   去小程序后台「版本管理 → 开发版本」，把刚上传的这版**选为体验版**，或直接提交审核。
4. 审核通过后点「发布」。

想先在真机上看一眼再决定要不要提审，就把第 2 步的动作选成 `preview`：
跑完在 Actions 的运行页面下载 artifact `miniprogram-preview-qrcode`，微信扫 `preview.jpg` 打开开发版。

> 提审前确认服务端 `.env` 的 `ADMIN_OPENIDS` 已经填了你自己的 openid。
> 否则审核员登录不会拿到 `40300`，只读演示模式不触发，审核看到的是一片空白。

### C. 两边都改了（接口契约变了）

先 A 后 B，中间验证一次。因为线上还跑着老版本小程序，**后端的接口改动要向后兼容**：
加字段可以，改字段名、改返回结构、删接口都会把线上用户打挂。
真要做不兼容的改动，就先发一版后端同时认新老两种，等小程序线上版本都升级完再删老的。

## 回滚

| 出问题的是 | 怎么退 |
| --- | --- |
| 后端 | `sudo cp /opt/crab-order/bin/crab-server.prev /opt/crab-order/bin/crab-server && sudo systemctl restart crab-order` |
| 小程序（已发布） | 小程序后台「版本管理 → 线上版本 → 版本回退」，退回上一个线上版本 |
| 小程序（只到体验版） | 不用退，重新传一版把体验版换掉即可 |
| 数据 | `/backup/crab-<日期>.db`（`scripts/backup.sh` 每天凌晨跑，留 30 天）：停服务 → 覆盖 `data/crab.db` → 删掉同名的 `-wal` / `-shm` → 启服务 |

数据库回滚会丢掉备份点之后的数据，动手前先把当前的 `crab.db` 另存一份。

## 出错对照

| 现象 | 多半是 |
| --- | --- |
| Actions 里 `getrandstr` / `tunneling socket` 403 | 上传密钥的 IP 白名单没关 |
| Actions 报 `invalid signature` / `40013` | `WX_PRIVATE_KEY` 贴漏了 BEGIN/END 行，或 `WX_APPID` 和密钥不是同一个小程序 |
| Actions 报「仓库变量 TARO_APP_API_BASE_URL 没配」 | Variable 配到了 Secret 里，或名字拼错 |
| 上传成功但小程序里所有请求都失败 | 域名没加进 request 合法域名；或 Variable 填的域名带了末尾 `/` |
| 版本号被拒 | 微信只认数字和点，`v0.1.1`、`0.1.1-beta` 都不行，标签的 `mp-v` 前缀由 workflow 自己剥掉 |
| 小程序页面空白、只有顶部提示条 | 进了只读演示模式，服务端 `ADMIN_OPENIDS` 里没有你的 openid |
| `systemctl status` 显示 `activating (auto-restart)` 反复重启 | `journalctl -u crab-order -n 50` 看日志，通常是 `.env` 缺 `AUTH_SECRET` 或 data 目录没权限 |

## 两个可选项

**自建 runner（想保留 IP 白名单）**：在腾讯云服务器上装 Node 20，按 GitHub → Settings →
Actions → Runners 的指引注册成 self-hosted runner，再把 workflow 里的
`runs-on: ubuntu-latest` 改成 `runs-on: self-hosted`。这样上传请求从服务器固定 IP 发出，
白名单填这个 IP 就行。代价是服务器要常驻一个 runner 进程，构建时也吃它的 CPU 和内存。

**不想在服务器上编译**：让 GitHub Actions 交叉编译好二进制（`make build` 本来就是
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`，产物是一个静态文件），传成 release asset，
服务器上 `curl` 下来替换。适合内存吃紧的轻量服务器，代价是私有仓库下载 asset 要带 token。
需要的话我可以补一个后端的 workflow。
