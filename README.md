# 大闸蟹订单管理

个人大闸蟹卖家的订单管理：卖家在微信小程序里录单、查单、标记发货与收款，买家用免登录链接自助查物流。

| 目录 | 是什么 |
|---|---|
| `cmd/` `internal/` | 后端，Go + SQLite，单文件二进制 |
| `miniprogram/` | 小程序「蟹记」，Taro 4 + React |
| `web/track/` | 买家查单页，单个静态 HTML |

后端：

- Go 1.22+ / 标准库 `net/http`（Go 1.22 增强版 `ServeMux`）
- SQLite（`modernc.org/sqlite`，纯 Go 无 CGO），`go.mod` 里只有这一个第三方依赖
- `database/sql` + 手写 SQL，不引入 ORM
- 自签 HMAC-SHA256 token，不引入 JWT 库

## 快速开始

```bash
cp .env.example .env
# AUTH_SECRET 必填，至少 32 字符：
echo "AUTH_SECRET=$(openssl rand -hex 32)" >> .env
# 本地调试可以先用 dev（不校验 WECHAT_SECRET，并开启 CORS）
sed -i 's/^ENV=prod/ENV=dev/' .env

make run        # 或 go run ./cmd/server
curl localhost:8080/healthz
# {"code":0,"msg":"ok","data":{"status":"ok"}}
```

首次启动会自动建表，并在 `specs` 表为空时写入 8 条当季参考价。

### 构建与测试

```bash
make build      # CGO_ENABLED=0，产出单文件二进制 bin/crab-server
make test       # 单元测试 + httptest 接口测试
make test-race  # 带竞态检测
make cover      # 覆盖率
```

## 目录结构

```
cmd/server/         装配依赖、启动 HTTP server、优雅退出
internal/config/    环境变量加载与校验
internal/model/     实体、枚举与状态机、金额格式化
internal/store/     Queries 接口 + SQLite 实现 + 内嵌 schema.sql
internal/service/   业务逻辑：金额计算、状态流转、幂等、收款重算
internal/api/       路由、中间件、handler、DTO
internal/auth/      token 签发与校验
internal/wechat/    code2session
internal/errs/      业务错误码与错误类型
internal/timex/     业务时区与时间格式
```

分层规则：`handler → service → store`，严格单向。handler 不碰 SQL，store 不含业务判断。

## 几条不能破的规矩

1. **金额只用整数「分」**。见到 `float64` 参与金额计算即为 bug。API 出入参也是分，响应里额外给 `*_yuan` 字符串方便展示。
2. **`paid_amount` 与 `pay_status` 是派生值**，只能由 `recalcPayment` 写入，不接受客户端赋值。实收 = 该订单未删除收款流水之和；状态由实收与应收的大小关系推导。
3. **`goods_amount` / `payable_amount` 由服务端算**，忽略客户端传入值。
4. **发货状态与收款状态互不约束**。熟客先发后付、预售先付后发都是正常业务，没有「未付款不能发货」这种校验。
5. **明细里的 `spec_label` / `unit_price` 是快照**，不关联 `specs` 表。改价只影响新订单，历史订单金额不跟着变（有测试守着）。
6. **所有多表写入都在一个 `BEGIN IMMEDIATE` 事务里**（`store.WithTx`）。
7. **时间全走 `Asia/Shanghai`**，服务器时区可能是 UTC，业务时间一律经 `internal/timex`。

## 配置

见 `.env.example`。环境变量优先级高于 `.env` 文件（方便 systemd / docker 注入）。

启动时的硬校验：

- `AUTH_SECRET` 为空或短于 32 字符 → 直接退出，不允许使用默认密钥
- `ENV=prod` 且 `WECHAT_SECRET` 为空 → 直接退出

### 第一次部署怎么拿到 openid

`ADMIN_OPENIDS` 为空时 `/api/login` 进入**引导模式**：不校验白名单，直接把 openid 返回在 `data.openid` 里并打进服务端日志。把它填进 `ADMIN_OPENIDS` 重启，引导模式自动关闭。

## API

Base URL `https://<domain>/api`，请求与响应均为 `application/json; charset=utf-8`。
除 `/api/login` 与 `/api/public/**` 外都需要 `Authorization: Bearer <token>`。

统一响应：

```json
{ "code": 0, "msg": "ok", "data": {} }
```

列表类接口的 `data` 固定为 `{"list": [], "total": 128, "page": 1, "page_size": 20}`。

### 错误码

| code | HTTP | 含义 |
|---|---|---|
| 0 | 200 | 成功 |
| 40001 | 400 | 参数校验失败（`msg` 指明具体字段） |
| 40100 | 401 | 未登录 / token 无效或过期 |
| 40300 | 403 | 无权限（openid 不在白名单） |
| 40400 | 404 | 资源不存在 |
| 40901 | 409 | 幂等冲突 |
| 40902 | 409 | 状态流转非法 |
| 40903 | 409 | 数据已被修改（乐观锁冲突） |
| 42900 | 429 | 请求过于频繁 |
| 50000 | 500 | 服务内部错误 |

> 幂等冲突不会真的返回 40901：`request_id` 重复时直接返回**已存在的那笔订单**，`code = 0`，响应里多一个 `"idempotent": true`。小程序网络抖动重试既不会重复建单，也不会给用户报错。

### curl 示例

下面用 `$TOKEN` 代表登录拿到的 token，`$ID` 代表订单 ID。

**登录**

```bash
curl -X POST localhost:8080/api/login \
  -H 'Content-Type: application/json' \
  -d '{"code":"081xxxxx"}'
# {"code":0,"msg":"ok","data":{"token":"eyJvcGVu...","expires_at":"2026-10-14T10:30:00+08:00","openid":"oXxxxx"}}
```

**建单**

```bash
curl -X POST localhost:8080/api/orders \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "request_id": "mp-1726300000-abc123",
    "receiver_name": "张三",
    "phone": "13800138000",
    "address": "江苏省苏州市工业园区xx路88号3栋201",
    "wechat_nick": "老张",
    "wechat_remark": "同学介绍",
    "items": [
      {"gender":"male","spec_gram":225,"spec_label":"4.5两","unit":"piece","quantity":5,"unit_price":8800},
      {"gender":"female","spec_gram":175,"spec_label":"3.5两","unit":"piece","quantity":5,"unit_price":6800}
    ],
    "freight_fee": 2000,
    "discount": 1000,
    "expect_ship_date": "2026-09-16",
    "remark": "周五之前务必发出"
  }'
```

**列表 / 筛选**（全部可选，多条件 AND）

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'localhost:8080/api/orders?ship_status=pending,shipped&pay_status=unpaid,partial&keyword=张三&expect_ship_date_start=2026-09-01&expect_ship_date_end=2026-09-30&sort=expect_asc&page=1&page_size=20'
```

| 参数 | 说明 |
|---|---|
| `ship_status` / `pay_status` | 支持逗号分隔多值 |
| `keyword` | 模糊匹配单号 / 收货人 / 手机 / 微信昵称 / 微信备注 / 运单号 |
| `expect_ship_date` | 精确匹配某天 |
| `expect_ship_date_start` / `expect_ship_date_end` | 约定发货日区间 |
| `created_start` / `created_end` | 创建时间区间（Unix 秒或 RFC3339） |
| `sort` | `created_desc`(默认) / `created_asc` / `expect_asc` |
| `page` / `page_size` | 分页，`page_size` 默认 20 最大 100 |

列表返回的是**订单摘要**：不含 `items` / `payments` / `logs` 全量，只带 `items_summary`（如 `"公4.5两×5, 母3.5两×5"`）。

**详情**

```bash
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/orders/$ID
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/orders/by-no/20260914-007
```

**改单**（`items` 整体替换；不能改状态、金额汇总、物流单号）

```bash
curl -X PUT localhost:8080/api/orders/$ID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "receiver_name":"张三","phone":"13800138000",
    "address":"江苏省苏州市工业园区xx路88号3栋201",
    "items":[{"gender":"male","spec_gram":225,"spec_label":"4.5两","unit":"piece","quantity":8,"unit_price":8800}],
    "freight_fee":2000,"discount":1000,"expect_ship_date":"2026-09-16","remark":"加了3只",
    "updated_at":"2026-09-14T11:00:00+08:00"
  }'
```

`updated_at` 可选，带上就是乐观锁：与库里不一致返回 `40903`。改单会重算应收，**并重算收款状态**（原本 `paid` 可能退回 `partial`）。

**状态流转**

```bash
# 发货（ship_company + tracking_no 必填；ship_time 可空，空则取当前时间，支持补录昨天发的货）
curl -X POST localhost:8080/api/orders/$ID/ship \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"ship_company":"顺丰速运","tracking_no":"SF1234567890","ship_time":null}'

curl -X POST localhost:8080/api/orders/$ID/receive \
  -H "Authorization: Bearer $TOKEN" -d '{"receive_time":null}'

curl -X POST localhost:8080/api/orders/$ID/cancel \
  -H "Authorization: Bearer $TOKEN" -d '{"reason":"买家临时不要了"}'

# 回退（误操作撤销），to 只能是 pending / shipped，reason 必填
curl -X POST localhost:8080/api/orders/$ID/revert-ship \
  -H "Authorization: Bearer $TOKEN" -d '{"to":"pending","reason":"点错了"}'

# 软删除
curl -X DELETE localhost:8080/api/orders/$ID -H "Authorization: Bearer $TOKEN"
```

合法流转：

```
pending  → shipped      pending  → cancelled
shipped  → received     shipped  → cancelled
回退：shipped → pending、received → shipped、cancelled → pending
```

其余任意跳转（如 `pending → received`）一律返回 `40902`。

**收款**

```bash
# 记一笔收款，amount 为负表示退款
curl -X POST localhost:8080/api/orders/$ID/payments \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"amount":39000,"pay_method":"wechat","paid_at":null,"remark":"尾款"}'

# 记错了就删（软删 + 重算）
curl -X DELETE localhost:8080/api/payments/1 -H "Authorization: Bearer $TOKEN"
```

`pay_method` ∈ `wechat` / `alipay` / `cash` / `transfer` / `other`。
允许超付，此时 `pay_status = paid`、`unpaid_amount` 为负数，前端显示「多收 X 元」。

**统计**

```bash
curl -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/stats/dashboard?start=2026-09-01&end=2026-09-14'
curl -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/stats/ship-plan?date=2026-09-16'
```

口径：`today.revenue` 是当天**实际收到的钱**（按 `payments.paid_at` 计，含退款负数）；`range.*` 按订单**创建时间**落在区间内统计；`pending.unpaid_amount` 不含已取消的订单。`ship-plan` 的 `date` 默认明天。

**地址簿 / 规格价目表**

```bash
curl -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/addresses?keyword=张&limit=20'

curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/specs          # 只返回启用中的
curl -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/specs?all=1'  # 连停用的一起返回

curl -X POST localhost:8080/api/specs -H "Authorization: Bearer $TOKEN" \
  -d '{"gender":"male","spec_gram":300,"spec_label":"6.0两","unit":"piece","unit_price":16800}'
curl -X PUT localhost:8080/api/specs/3 -H "Authorization: Bearer $TOKEN" \
  -d '{"gender":"male","spec_gram":225,"spec_label":"4.5两","unit":"piece","unit_price":9500}'
curl -X DELETE localhost:8080/api/specs/3 -H "Authorization: Bearer $TOKEN"   # 停用，不物理删
```

地址簿从历史订单聚合，不单独建客户表，永远和实际下单信息一致。

**导出 CSV**

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'localhost:8080/api/orders/export?ship_status=pending' -o orders.csv
```

复用列表的全部筛选参数，带 UTF-8 BOM（Excel 打开不乱码），金额列输出**元**，可直接求和。

**买家免登录查单**

```bash
curl 'localhost:8080/api/public/orders?order_no=20260914-007&phone_tail=8000'
```

强制「单号 + 手机号后 4 位」双因子匹配，任一不对都统一返回 `40400`（不区分「单号不存在」和「手机号不对」，避免被枚举）。返回数据脱敏：姓名只留姓、手机中间 4 位打码、地址只到区县级、不含金额与备注。按 IP 限流每分钟 20 次。

## 前端

### 小程序「蟹记」（`miniprogram/`）

```bash
cd miniprogram
npm install
npm run dev:weapp        # 或 npm run build:weapp
npm test                 # 地址解析与金额换算的单测
```

编译完用微信开发者工具打开 `miniprogram/` 目录。接口域名**构建时注入**，不在源码里写死：

```bash
TARO_APP_API_BASE_URL=https://your.domain TARO_APP_TRACK_URL=https://your.domain npm run build:weapp
```

不传就退回 `src/utils/config.js` 里的占位域名 `https://example.com`；也可以在小程序「设置」页
临时改，存本地。小程序后台记得配 request 合法域名。上传体验版见下面的「小程序上传」。

页面：今天（要发的清单 + 汇总）、记一笔（录单）、全部（列表筛选）、详情、设置。

几条界面上的规矩：

- 主色**蟹壳青** `#2F4739`，待办用**金爪**金 `#C8A227`，逾期和危险操作才用**熟蟹红** `#D2542A`。
- **已完成的状态一律用灰**，这是整套界面唯一的信息层级。取消掉的单连收款状态也一并淡化——
  没有要追的钱，就不该用告警色喊人。
- 金额、数量、单号一律 `tabular-nums` 且右对齐：几十行金额要能竖着对齐扫读。
- 动效只在状态变更时出现，并且尊重 `prefers-reduced-motion`。

金额在前端也一律是「分」的整数，分转元走整数运算。录单页显示的「应收」只是预估，
**以后端返回的 `payable_amount` 为准**。

小程序是个人主体 + 笔记类目，所以全站不出现下单、购买、购物车、支付这类电商词汇，
**变量名和注释也不行**（审核会看包体）；没有 `web-view`，买家链接只能复制文本。

#### 接口对接上几个不显然的地方

后端契约以上面的 API 章节为准，前端字段名一律跟它保持一致，不另造一套。几个踩过的点：

- **`/api/stats/ship-plan?date=` 必须传 `YYYY-MM-DD`**，传 `today` 这种字面量会 400；
  而且不传时后端默认给的是**明天**，首页要今天就得显式传当天。
- **订单明细是快照**，建单时要把 `gender / spec_gram / spec_label / unit / quantity / unit_price`
  整条带过去，后端不认 `spec_id`。改价不影响历史订单就是靠这个。
- **允许超付**：`unpaid_amount` 会是负数，详情页显示「多收 ¥X」并且用主色，不是告警色。
- **`DELETE /api/specs/{id}` 是停用**，不是物理删；设置页要 `?all=1` 才看得到停用的档。
- **回退发货**要带 `{to, reason}`，`to` 只能是 `pending` / `shipped`。
- 列表筛选的时间参数是 `created_start` / `created_end` 与 `expect_ship_date[_start|_end]`；
  后端会忽略不认识的参数，写错了不会报错，只会**静默不生效**。
- 建单幂等命中时返回的是 `code: 0` + `idempotent: true` 和已存在的那笔单，不是错误码，
  重试逻辑照常走成功分支即可。

#### 审核演示模式

审核员的 openid 不在 `ADMIN_OPENIDS` 里，`/api/login` 会返回 `40300`，直接提审只会看到
一片空白。所以小程序收到 `40300` 时自动进入**只读演示模式**：用 `src/static/demo.json`
的本地假数据渲染页面，顶部挂一条提示，写操作一律拒绝。`demo.json` 存的是**相对天数**，
不管哪天提审，「今天要发的」都还是今天。

> 注意：`ADMIN_OPENIDS` 为空时后端是**引导模式**，不校验白名单也就不会返回 `40300`，
> 演示模式不会触发。提审前务必先把自己的 openid 填进配置。

### 买家查单页（`web/track/index.html`）

单个静态 HTML，9.6KB，无构建步骤、无框架、无 CDN、无埋点。链接带 `?no=` 时自动填好
订单号并锁住，买家只需要填手机后四位。只显示物流时间轴，不显示任何金额和备注。

发给买家的链接形如 `https://<域名>/t?no=20260914-007`，小程序详情页的「发链接给买家」
会把这段话复制到剪贴板。

## 部署

本节讲服务端。小程序发版是另一条线，见 [`mini-deploy.md`](mini-deploy.md)。

```bash
make build
sudo mkdir -p /opt/crab-order/{bin,data,scripts}
sudo cp bin/crab-server /opt/crab-order/bin/
sudo cp .env /opt/crab-order/
sudo cp scripts/backup.sh /opt/crab-order/scripts/
sudo cp deploy/crab-order.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now crab-order
```

- **HTTPS**：微信小程序强制要求 HTTPS 且域名需在小程序后台配置。生产由 Caddy 或 Nginx 反代并自动签证书，Go 服务只监听 `127.0.0.1:8080`（把 `HTTP_ADDR` 设成 `127.0.0.1:8080`）。
- **买家查单页**：后端不托管静态文件，由反代把 `/t` 指到 `web/track/index.html`，
  `/api` 反代到 Go。同源，不涉及 CORS。顺手给 `/t` 加一条 `X-Robots-Tag: noindex`。
  Caddy 大致长这样：

  ```caddy
  你的域名 {
      handle /api/* { reverse_proxy 127.0.0.1:8080 }
      handle /t* {
          header X-Robots-Tag noindex
          rewrite * /index.html
          file_server { root /opt/crab-order/web/track }
      }
  }
  ```
- **小程序**：见下面的「小程序上传」，走 CLI，不用开发者工具点上传。与服务端部署无关。
- **备份**：`scripts/backup.sh` 用 `sqlite3 .backup` 做热备（WAL 模式下**不要直接 cp**，可能拷到不一致的状态），保留最近 30 天。建议 crontab 每天凌晨跑一次：

  ```cron
  0 3 * * * /opt/crab-order/scripts/backup.sh >> /var/log/crab-backup.log 2>&1
  ```

- **优雅退出**：收到 `SIGINT`/`SIGTERM` 后给在途请求 10 秒排空，之后关闭数据库连接。

### 小程序上传

用微信官方的 [`miniprogram-ci`](https://developers.weixin.qq.com/miniprogram/dev/devtools/ci.html)
把 `dist/` 传到微信后台，不用开发者工具点「上传」。开发者工具只留着本地调试用。

**一次性准备**

1. 小程序后台 →「开发管理 → 开发设置 → 小程序代码上传密钥」生成密钥，下载 `private.<appid>.key`。
   密钥**只能下载一次**，丢了就重置；仓库里不放它（`.gitignore` 已经挡掉 `private.*.key`）。
2. 同一页面的 **IP 白名单**：GitHub 托管 runner 出口 IP 不固定，用它就得关掉白名单；
   要留白名单就换自建 runner，把固定 IP 填进去。
3. `project.config.json` 里的 `appid` 仍是 `touristappid`，真实 AppID 通过 `WX_APPID` 传，
   不进仓库。开发者工具本地调试时自己填。

**本地跑一次**

```bash
cd miniprogram
cp .env.example .env          # 填 appid、密钥路径、域名
set -a && . ./.env && set +a  # 或用 direnv / dotenv 之类
npm run build:weapp
npm run ci:preview            # 生成预览码 dist/preview.jpg，微信扫码开开发版
npm run ci:upload             # 传体验版
```

仓库根目录也有对应的 make 目标：`make mp-build` / `make mp-preview` / `make mp-upload`，
后两个会自动先构建。

脚本是 `miniprogram/scripts/mp-ci.js`，参数都能用环境变量代替：

| 参数 | 环境变量 | 默认值 |
| --- | --- | --- |
| `--version` | `MP_VERSION` | `package.json` 的 `version` |
| `--desc` | `MP_DESC` | `版本号 @ 提交号` |
| `--robot` | `WX_CI_ROBOT` | `1`（1-30，不同用途占不同号，后台好区分） |
| `--page` / `--query` | — | 仅 `preview`，指定打开的页面与参数 |
| — | `WX_APPID` | 无，必填 |
| — | `WX_PRIVATE_KEY` / `WX_PRIVATE_KEY_PATH` | 无，二选一必填 |

几个会踩的点：

- 传的是 `dist/`，所以**先构建再上传**；`src/` 比 `dist/` 新时脚本会警告可能在传旧产物。
- 上传用的 `setting` 取 `project.config.json` 里那份（`es6` / `minified` / `postcss` 全 `false`）。
  Taro 自己已经编译压缩过了，再让微信编译一次反而容易出问题，别去打开。
- `version` 同时决定包里「设置」页显示的版本号（构建期由 `MP_VERSION` 注入），
  所以构建和上传要用**同一个**版本号——CI 里已经是同一个变量。
- 上传完还要去后台「版本管理」把这一版设成体验版或提交审核，CLI 不代劳。
  提审前记得先把自己的 openid 填进服务端 `ADMIN_OPENIDS`，否则审核演示模式不会触发。

**GitHub Actions**

`.github/workflows/miniprogram-deploy.yml`：打 `mp-v*` 标签（版本号取标签名，`mp-v0.1.1` → `0.1.1`）
或在 Actions 页手动跑，流程是 `npm ci` → `npm test` → 带域名构建 → 上传。
需要配的 Secrets / Variables、发版步骤、回滚和排错都在 [`mini-deploy.md`](mini-deploy.md)。

## 本期不含

微信支付在线收款、商品库存管理、多商家/多店铺、快递面单打印对接、买家在线下单、消息推送/订阅消息。数据模型上给微信支付与订阅消息预留了字段。
