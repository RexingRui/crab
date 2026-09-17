# 实现日志

按《大闸蟹订单管理 — 后端技术方案》实现，记录进度、决策与过程中遇到的问题。

---

## 环境

- Go 1.24.7（方案要求 1.22+，满足）。
- `modernc.org/sqlite v1.34.5` 拉取成功，**无需**降级到 `mattn/go-sqlite3`，保持 `CGO_ENABLED=0` 纯 Go 构建。`go.mod` 里第三方依赖只有它一个（其余是它的间接依赖）。
- 仓库初始为空（仅 LICENSE/README），从零搭建。

## 进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | config / slog / SQLite 连接与 migrate / 统一响应与错误码 / 中间件 / healthz | ✅ 完成 |
| P1 | model 与枚举 / store / 订单号 / 建单·列表·详情·改单·软删 / 金额 / 幂等 | ✅ 完成 |
| P2 | 发货 / 收货 / 取消 / 回退 / order_logs / 流转校验 | ✅ 完成 |
| P3 | payments 增删 / paid_amount 与 pay_status 重算 / 定金与退款场景 | ✅ 完成 |
| P4 | code2session / token 签发校验 / Auth 中间件 / 引导模式 | ✅ 完成 |
| P5 | 统计看板 / 发货计划 / 地址簿 / 规格价目表与种子数据 / CSV 导出 | ✅ 完成 |
| P6 | public 路由 / 脱敏 / IP 限流 | ✅ 完成 |
| P7 | 测试 / README（含完整 curl 示例）/ Makefile / systemd 与备份脚本 | ✅ 完成 |

## 验证结果

- `gofmt -l .` 无输出，`go vet ./...` 通过。
- `go test ./...` 全部通过；`go test -race ./...` 无竞态。
- 跨包总覆盖率 **72.4%**（`go test -coverpkg=./...`）。未覆盖的主要是 `cmd/server` 装配代码、`wechat` 的真实 HTTP 调用与各处 store 的错误分支。
- 方案第 12 章要求的测试全部落地：
  - `model/enum_test.go` — 发货状态流转 4×4 全组合表驱动（正向 + 回退 + 自环）
  - `model/money_test.go` — 分转元，含 0、负数、大额
  - `service/order_test.go` — 金额计算、优惠超额、退款导致状态回退、超付、改单重算
  - `service/orderno_test.go` — 16 个 goroutine 并发生成 1000 个订单号，无重复
  - `auth/token_test.go` — 签发 / 校验 / 过期 / 篡改签名 / 换密钥
  - `api/api_test.go` — 全链路、幂等、收款链路、非法流转 40902、无 token 40100、买家查单尾号错 40400、限流 42900、CSV BOM、改价不影响历史订单
- 另外跑了一次真实二进制的冒烟测试（`make build` 后起服务打 curl）：建单 → 幂等重试 → 记定金 → 未付款直接发货 → 非法流转 → 买家脱敏查单 → 看板 → 导出 → `SIGTERM` 优雅退出，结果均符合预期；导出文件开头确认是 `ef bb bf`（UTF-8 BOM）。

## 过程中遇到的问题

1. **访问日志永远记到空 openid（已修复）**
   冒烟测试时发现日志里 `"openid":""`，但方案第 10 章要求 AccessLog 记录 openid。
   原因是中间件顺序：AccessLog 在 Auth **外层**，而 Auth 是用 `context.WithValue` 派生出新的 context 往内层传，外层的 AccessLog 根本看不到。
   修法是 AccessLog 先往 context 里放一个可写的 `*openIDHolder`，Auth 校验通过后回填，AccessLog 在 `next.ServeHTTP` 返回后再读。单个请求内是顺序执行的，没有并发写。加了 `TestAccessLogRecordsOpenID` 守着。

2. **建单撞幂等键时浪费订单号日序列**
   第一版在事务内捕获唯一索引冲突后 `return nil`（提交事务），导致 `order_seq` 白白加一，订单号出现空洞。
   改成返回一个内部哨兵错误让事务回滚，提交后再回查已有订单，日序列不受影响。

3. **`go vet` 报 non-constant format string**
   明细校验里用字符串拼接把下标塞进 `errs.InvalidParam`，被 vet 判为格式化字符串风险。改成 `errs.InvalidParam("items[%d].gender 非法…", i)`。

4. **`expect_asc` 排序时空值排在最前**
   SQLite 的 `ORDER BY x ASC` 把 NULL 排在最前，未约定发货日的订单会顶到列表头部。加了 `(expect_ship_date IS NULL) ASC` 前置排序键把它们挪到最后。

5. **BEGIN IMMEDIATE 怎么发出**
   `database/sql` 的 `BeginTx` 默认发的是 `BEGIN`（deferred），写事务升级锁时可能报 `database is locked`。用 DSN 参数 `_txlock=immediate` 让驱动直接发 `BEGIN IMMEDIATE`，配合 `SetMaxOpenConns(1)` 与 `busy_timeout=5000`。PRAGMA 除了写在 DSN 里，打开后还显式执行了一遍并校验，避免驱动参数拼写错误被静默忽略。

## 相对方案的补充说明

方案第 3 章的目录结构属于指导性描述；字段名、枚举值、路由路径、错误码是强约定，**未做任何改动**。以下是在指导性部分上的补充：

1. **新增 `internal/errs` 包**：方案把「统一响应包装与错误码」放在 `api/response.go`，但 service 层要返回带业务码的错误，而分层规则要求 `handler → service → store` 严格单向，service 不能反向依赖 api。所以把错误码与 `*errs.Error` 抽到独立的 `internal/errs`，`api/response.go` 只负责业务码 → HTTP 状态码的映射与 JSON 包装。
2. **新增 `internal/timex` 包**：方案要求「服务启动时 `time.LoadLocation` 一次，全局复用」「纯日期字段用 TEXT 避免时区歧义」。把时区与时间格式化集中到一个包，避免各处散落 `time.Now()` 的本地时区默认值。系统缺 tzdata 时退回固定 +08:00。
3. **新增 `internal/api/dto.go`**：订单对象的 JSON 视图（`*_yuan`、`*_text` 派生字段、脱敏）集中构造，避免各 handler 重复拼装。
4. **`internal/store/sqlite_stats.go`**：规格、地址簿、统计的 SQL 从 `sqlite.go` 拆出来，单纯是因为一个文件太长。
5. **`internal/service/spec.go`、`address.go`**：规格与地址簿的业务校验需要一个落点，方案的 service 目录只列了 order/stats/orderno 三个文件。

## 若干口径的选择（方案未明确，已在 README 注明）

- `today.revenue` = 当天**实际收到的钱**，按 `payments.paid_at` 计，含退款负数（而不是当天新单的应收）。
- `range.*` 按订单**创建时间**落在区间内统计；`crab_count` = 这些订单明细的 `quantity` 之和。
- `pending.unpaid_amount` **不含已取消的订单**——已取消的欠款不该催。
- `first_pay_time` / `settled_time` 取触发那次重算的**收款时间**（`paid_at`），而不是操作时间，这样补录昨天的收款时间戳是准的。
- `GET /api/specs` 默认只返回启用中的（小程序录单页下拉用），加 `?all=1` 才连停用的一起返回（管理页用）。
- 买家查单的地址脱敏：优先切到第一个「区/县/旗」，没有就切到第一个「市」，再没有就截前 10 个字，上限 20 字。

## 复核清单（方案第 15 章）

| # | 要求 | 落实情况 |
|---|---|---|
| 1 | 金额只用整数分 | 全仓无 `float64` 参与金额计算；分转元只在序列化时做，负数单独处理（`-50` → `"-0.50"`，有测试） |
| 2 | `pay_status` / `paid_amount` 只能由重算函数写入 | 只有 `service.recalcPayment` 写这两个字段；API 入参里根本没有这两个字段 |
| 3 | 发货与收款状态互不约束 | 没有任何「未付款不能发货」的校验，`TestShipDoesNotRequirePayment` 守着 |
| 4 | 明细单价是快照 | `order_items` 不关联 `specs`；`TestSpecPriceChangeDoesNotAffectHistory` 实际验证了改价后历史订单金额不变 |
| 5 | 多表写入在一个事务里，用 BEGIN IMMEDIATE | 建单/改单/记收款/状态流转全部走 `store.WithTx`，DSN `_txlock=immediate` |
| 6 | 列表不返回 items/logs/payments 全量 | 列表返回 `OrderSummaryDTO`，只带 `items_summary` |
| 7 | CSV 导出写 BOM | 写入 `\xEF\xBB\xBF`，有测试 + 冒烟时用 `od` 确认过字节 |
| 8 | 错误对内详细对外简洁 | `errs.Error` 的 `Err` 只进日志；响应只给 `code` + `msg`；panic 堆栈不外泄（有测试） |
| 9 | 改价不影响历史订单，实际验证一次 | 见 #4，接口级测试已覆盖 |
| 10 | 时间全走 Asia/Shanghai | 统一经 `internal/timex`，冒烟输出确认为 `+08:00`（容器本身是 UTC） |

## 买家自助登记（后加的一期）

需求：把「登记」也做成 H5，买家直接在浏览器里填，不占小程序的坑。定下来的形状：

- **不新增表**。登记直接落成 `orders` 里一笔 `source=web` 的待发货单。
  `orders` 加了一列 `source`（`manual` / `web`），schema.sql 改了一处，
  老库靠 `Migrate` 里的 `addedColumns` 查 `pragma_table_info` 后补 `ALTER TABLE`
  （SQLite 没有 `ADD COLUMN IF NOT EXISTS`）。有测试造了一张老表验证升级路径。
- **链接是无状态 token**，`internal/auth/reglink.go`，复用 `AUTH_SECRET`，
  签名前加 `reg.` 域前缀做域分离。服务端不存、撤不回，只有 `REG_LINK_TTL` 兜底。
- **一条链接只落一单不靠记账**：token 里的 `jti` 直接当 `request_id`（`reg:<jti>`），
  撞的是已有的 `uk_orders_request` 唯一索引，复用建单本来就有的幂等分支。
  这条是「不新增表」还能做到一次性的关键。
- **单价绝不收买家传的值**。`resolveRegItems` 只认 `spec_id + quantity`，
  回查 `specs` 表填快照，且只认 `enabled=1` 的。这是这条公开写路径上唯一不能破的规矩，
  `TestRegistrationIgnoresClientPrice` 往请求里塞了 `unit_price=1` / `discount=999999` 守着。
- **手机号一天内查重**，命中返回 40901。顺序上必须排在 jti 幂等**后面**：
  同一条链接重复提交（刷新成功页、连点两下）应该拿回原来那笔单，而不是被查重报「已登记」。
- 公开写接口单独一个限流器（默认 5 次/分钟），不和查单共用配额。

### 过程中的几个判断

1. **重复提示里不回单号。**
   一开始想在「这个手机号今天已经登记过了」后面带上单号方便买家自查，后来撤了：
   链接可能被转发，持链接的人拿任意手机号试，就能反查出别人的单号——
   而单号 + 手机号正好是查单页的两个因子，能看到脱敏后的收货人和物流。
   现在只回一句话，把泄露面收窄到「这个号今天登记过没有」这个布尔值。
   `TestRegistrationDuplicatePhone` 里顺手断言了提示中不出现连续 8 位数字。

2. **登记单会污染统计口径。**
   `range.*` 按创建时间统计，未确认的登记单会被算进营收和蟹数。加 `source` 列就是为这个，
   列表能筛、CSV 多一列「来源」。统计口径本身这一期没改，先让卖家能看见、能筛出来。

3. **两种 token 的域分离。**
   其实不加前缀也串不了（登录 token 解出来 `jti` 为空、登记 token 解出来 `openid` 为空，
   都会被必填校验挡掉），但那是靠字段恰好不重叠兜住的，属于碰运气。
   加一个 `reg.` 前缀让 HMAC 本身就不同，`TestTokenDomainsAreSeparate` 双向守着。

4. **幂等回执要给姓名打码。**
   自查时发现的：链接被转发出去，第二个人填完提交会命中 jti 幂等，
   回执里原样回显第一位买家的**全名**。首次提交回显的是他自己刚填的内容，不用打码；
   幂等命中这条路径上回显的可能是别人的，`ToRegistrationDTO` 因此多收一个 `idem` 参数，
   命中时走 `MaskName`。测试断言了两次提交的姓名不相同且带 `*`。

5. **H5 页加减数量不重建列表。**
   第一版 `bump()` 每次都 `renderSpecs()` 重画整个规格列表，连点时刚按下的那个按钮
   会被换成新的 DOM 节点，焦点和触摸态都丢。改成只改那一行的数字与减号的 disabled。

### 验证

- `gofmt -l` 无输出，`go vet ./...` 通过，`go test -race ./...` 全过。
- 新增测试：登记全链路、买家改价无效、同链接幂等、同手机号查重、坏 token / 过期 token /
  登录 token 冒充、停用规格、未登录不能签链接；store 层的老库升级与手机号查重；
  auth 层的签发校验、过期、篡改、域分离。
- 真浏览器端到端跑过一遍（Chromium + 一个模拟反代的小脚本）：
  选规格 → 填信息 → 提交 → 回执；再提交同一条链接拿回同一笔单（¥420.00 对得上）；
  换链接同手机号被拦；无 token 与坏 token 的兜底文案都对。控制台无报错。
- `miniprogram` 侧 `npm test` 与 `npm run build:weapp` 均通过。

## 本期未实现（方案第 1.3 节明确不含）

微信支付在线收款、商品库存管理、多商家/多店铺、快递面单打印对接、消息推送/订阅消息。
买家侧只做到「自助登记」，线上付款没做。
方案第 9.4 节标为 P2 的乐观锁已顺带实现（`PUT /api/orders/{id}` 可带 `updated_at`，冲突返回 40903）。
