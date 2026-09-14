# 大闸蟹订单管理 — 后端

个人大闸蟹卖家的订单管理后端：卖家在微信小程序里录单、查单、标记发货与收款，买家用免登录链接自助查物流。

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

## 部署

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
- **备份**：`scripts/backup.sh` 用 `sqlite3 .backup` 做热备（WAL 模式下**不要直接 cp**，可能拷到不一致的状态），保留最近 30 天。建议 crontab 每天凌晨跑一次：

  ```cron
  0 3 * * * /opt/crab-order/scripts/backup.sh >> /var/log/crab-backup.log 2>&1
  ```

- **优雅退出**：收到 `SIGINT`/`SIGTERM` 后给在途请求 10 秒排空，之后关闭数据库连接。

## 本期不含

微信支付在线收款、商品库存管理、多商家/多店铺、快递面单打印对接、买家在线下单、消息推送/订阅消息。数据模型上给微信支付与订阅消息预留了字段。
