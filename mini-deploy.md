# 小程序发版手册（mini-deploy）

只讲小程序怎么发版。后端部署看 [README 的「部署」一节](README.md#部署)，两件事互不相干：
改小程序不用碰服务器。

## 一句话

**打一个 `mp-v*` 标签，GitHub Actions 自动构建并上传到微信后台。**
版本号从标签名来，`mp-v0.1.1` → `0.1.1`，不用改任何文件。

```bash
git tag mp-v0.1.1 && git push origin mp-v0.1.1
```

## 自动到哪一步（重要）

自动化只能做到「代码进微信后台」为止，后面三步微信强制人工，任何 CLI 都代劳不了：

```
  打 tag ──▶ GitHub Actions ─────────────────▶ 微信后台「开发版本」
             npm ci → 单测 → 构建 → 上传          ↑ 自动到这里为止
                                                  │
                            ─────────────────────┴──────────────────────
                            以下在微信后台手点：
                            选为体验版 → 提交审核 → 审核通过后点「发布」
```

所以「打 tag 就自动部署」这个理解，准确说是**自动打包上传**：
两三分钟后你在后台「管理 → 版本管理 → 开发版本」里能看到这一版，
然后自己决定是设成体验版自测，还是直接提审。审核通过后还要再点一次「发布」才真正上线。

微信不开放「自动提审/自动发布」给个人主体的普通小程序，这是平台规则，不是这套流程偷懒。

## 一次性准备

只需做一次，两个地方。

### 微信小程序后台

1. 「开发管理 → 开发设置 → 服务器域名」：把 `https://你的域名` 加进 **request 合法域名**。
   小程序只能请求这里登记过的域名，漏了就是所有接口都失败。
2. 「开发管理 → 开发设置 → 小程序代码上传密钥」：生成并下载 `private.<appid>.key`。
   **只能下载一次**，丢了只能重置。
3. 同一页的 **IP 白名单要关掉** —— GitHub 托管 runner 出口 IP 不固定，开着必然 403。
   想保留白名单见文末。

### GitHub 仓库

Settings → Secrets and variables → Actions，配四项（前两个是 Secret，后两个是 Variable）：

| 类型 | 名字 | 值 |
| --- | --- | --- |
| Secret | `WX_APPID` | 小程序 AppID |
| Secret | `WX_PRIVATE_KEY` | `private.<appid>.key` 全文，连 `-----BEGIN/END-----` 一起贴 |
| Variable | `TARO_APP_API_BASE_URL` | `https://你的域名`，接口域名，会打进包里 |
| Variable | `TARO_APP_TRACK_URL` | `https://你的域名`，买家查单页域名 |

密钥文件在 iPad 上不方便打开，直接在 GitHub 网页版把文件内容粘进 Secret 输入框就行。
这两个 Variable 不配，构建会直接失败退出，免得发出一个指向 `example.com` 的包。

## 发一版

iPad 浏览器里全程可做，不需要电脑、不需要开发者工具。

1. 代码合进 `main`。
2. 打标签：`mp-v0.1.1`。在 GitHub 网页上也能打 —— 仓库首页 → Releases → Draft a new release
   → Choose a tag 里直接输 `mp-v0.1.1` → Publish。
3. 去 Actions 看「小程序上传」跑完（约 2–3 分钟）。
4. 微信后台「版本管理 → 开发版本」找到这一版 → 选为体验版，或提交审核。
5. 审核通过后点「发布」。

版本号用微信认的格式：**只能是数字和点**，`0.1.1` 可以，`v0.1.1`、`0.1.1-beta` 会被拒
（标签前面的 `mp-v` 由 workflow 自己剥掉，不算在内）。同一个版本号可以重复传，后传的覆盖前传的。

这个版本号同时会显示在小程序「设置」页底部，方便你对着后台确认用户装的是哪一版。

## 只想先真机看一眼

不打标签，走手动触发的预览：

Actions →「小程序上传」→ Run workflow → `action` 选 **preview** → Run。
跑完在这次运行的页面底部下载 artifact `miniprogram-preview-qrcode`，
用微信扫里面的 `preview.jpg` 就能打开开发版。预览码有效期约 25 分钟，过期重跑一次。

手动触发还能填这几项（都可留空）：

| 输入 | 作用 | 留空时 |
| --- | --- | --- |
| `version` | 版本号 | 取 `miniprogram/package.json` 的 `version` |
| `desc` | 版本备注，后台版本列表里看得到 | `版本号 @ 提交号` |
| `robot` | CI 机器人编号 1–30 | `1`。不同用途占不同号，后台好区分谁传的 |

## 回滚

| 状态 | 怎么退 |
| --- | --- |
| 已发布上线 | 微信后台「版本管理 → 线上版本 → 版本回退」，退回上一个线上版本 |
| 只到体验版 | 不用退，重新传一版把体验版换掉 |
| 只到开发版 | 不用管，没用户能看到 |

代码侧对应地把有问题的提交 revert 掉，再打一个新标签（比如 `mp-v0.1.2`）重新走一遍。
**不要复用或删了重打同一个标签**，后台会出现两个同版本号的记录，事后分不清哪个是哪个。

## 出错对照

| 现象 | 多半是 |
| --- | --- |
| Actions 报 `tunneling socket` / 403 | 上传密钥的 IP 白名单没关 |
| Actions 报 `invalid signature` / `40013` | `WX_PRIVATE_KEY` 贴漏了 BEGIN/END 行，或密钥和 `WX_APPID` 不是同一个小程序 |
| Actions 报「仓库变量 TARO_APP_API_BASE_URL 没配」 | 配到 Secret 里去了，或名字拼错。它必须是 Variable |
| 版本号被拒 | 带了 `v` 或后缀，微信只认数字和点 |
| 传上去了，但小程序里所有请求都失败 | 域名没加进 request 合法域名；或 Variable 里的域名带了末尾 `/` |
| 打了标签但 Actions 没触发 | 标签没 push 上去（`git push origin mp-v0.1.1`），或名字不匹配 `mp-v*` |
| 页面空白、只有顶部一条提示 | 进了只读演示模式：服务端 `.env` 的 `ADMIN_OPENIDS` 里没有你的 openid。提审前务必先填 |

## 想保留 IP 白名单

把一台固定公网 IP 的机器（比如你的腾讯云）注册成 GitHub 的 self-hosted runner：
Settings → Actions → Runners → New self-hosted runner，按页面指引装好（机器上要有 Node 20），
再把 `.github/workflows/miniprogram-deploy.yml` 里的 `runs-on: ubuntu-latest` 改成
`runs-on: self-hosted`。之后上传请求从这台机器的固定 IP 发出，白名单填它即可。
代价是这台机器要常驻一个 runner 进程，构建时也吃它的 CPU 和内存。

## 不用 CI，手动传

没有 CI 或想在电脑上直接传时（iPad 上做不了，这里备个案）：

```bash
cd miniprogram
cp .env.example .env            # 填 WX_APPID、WX_PRIVATE_KEY_PATH、两个域名
set -a && . ./.env && set +a
npm install
npm run build:weapp
npm run ci:preview              # 出预览码 dist/preview.jpg
npm run ci:upload               # 传开发版
```

仓库根目录也有 `make mp-preview` / `make mp-upload`，会自动先构建。
脚本是 `miniprogram/scripts/mp-ci.js`，用的是微信官方的
[`miniprogram-ci`](https://developers.weixin.qq.com/miniprogram/dev/devtools/ci.html)。
