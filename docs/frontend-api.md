# 前端依赖的接口约定

前端按这份约定编码。字段名与后端不一致时，**只改这三个文件**，不要散着改页面：

| 端 | 文件 |
|---|---|
| 小程序 | `miniprogram/src/utils/api.js` |
| Web 后台 | `web/admin/src/api/client.js` |
| 买家查单页 | `web/track/index.html`（页面底部的 `render()`） |

## 通用

- 响应体统一 `{ "code": 0, "msg": "ok", "data": {...} }`，`code != 0` 视为失败。
- **所有金额字段都是「分」为单位的整数**，前端不做浮点运算。
- 需要鉴权的接口带 `Authorization: Bearer <token>`。
- 约定的错误码：`40100` 票据过期（小程序会自动换票重试一次）、`40300` 非管理员
  （小程序据此进入只读演示模式）、`42900` 登录次数超限。

## 订单对象

```jsonc
{
  "id": 1,
  "order_no": "20260914-007",
  "receiver_name": "张三",
  "receiver_phone": "13800138000",
  "receiver_address": "江苏省苏州市…",
  "wechat_note": "老张",
  "items": [
    { "spec_id": 3, "gender": "male", "gender_text": "公", "size": "4.5两",
      "spec_name": "公 4.5两", "unit_price": 8800, "quantity": 5, "amount": 44000 }
  ],
  "items_summary": "公4.5两×5 母3.5两×5",   // 列表页只用这个，不请求明细
  "freight_fee": 2000,
  "discount": 1000,
  "total_amount": 74000,
  "paid_amount": 40000,
  "unpaid_amount": 34000,
  "ship_status": "pending",                  // pending | shipped | received | cancelled
  "ship_status_text": "待发货",              // 有就优先用，前端映射只兜底
  "pay_status": "partial",                   // unpaid | partial | paid
  "pay_status_text": "收了定金",
  "ship_company": "",
  "tracking_no": "",
  "plan_ship_date": "2026-09-16",
  "remark": "",
  "created_at": "2026-09-14 10:42:00",
  "shipped_at": "",
  "received_at": "",
  "payments": [
    { "id": 1, "amount": 40000, "method": "wechat", "method_text": "微信",
      "remark": "定金", "paid_at": "2026-09-14 11:00:00" }
  ],
  "logs": [ { "action": "记了一笔", "detail": "", "created_at": "2026-09-14 10:42:00" } ]
}
```

列表接口可以只返回到 `items_summary` 那一层，`items` / `payments` / `logs` 留给详情。

## 接口清单

### 小程序

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/login` | 入参 `{ code }`，返回 `{ token, openid, expires_at }`。非管理员返回 `40300` |
| GET | `/api/stats/dashboard` | `{ to_ship_count, unpaid_amount, unpaid_count, shipped_count }` |
| GET | `/api/stats/ship-plan?date=today` | `{ list: [订单] }` 今天要发的 |
| GET | `/api/orders` | 查询参数见下，返回 `{ list, total, page, page_size }` |
| GET | `/api/orders/:id` | 单个订单 |
| POST | `/api/orders` | 见「录单入参」 |
| PUT | `/api/orders/:id` | 改单 |
| DELETE | `/api/orders/:id` | 删单 |
| POST | `/api/orders/:id/ship` | `{ ship_company, tracking_no }` |
| POST | `/api/orders/:id/receive` | 确认收货 |
| POST | `/api/orders/:id/revert-ship` | 回退发货状态 |
| POST | `/api/orders/:id/payments` | `{ amount, method, remark }`，`amount` 可为负数（退款） |
| GET/POST/PUT/DELETE | `/api/specs[/:id]` | 价目表增删改查 |
| GET | `/api/orders/export` | CSV，透传筛选条件 |

列表查询参数：`keyword`、`ship_status`、`pay_status`、`created_from`、`created_to`、
`plan_ship_date`、`page`、`page_size`。

录单入参：

```jsonc
{
  "request_id": "abc123-1757800000000-4821",  // 幂等键，见下
  "receiver_name": "张三",
  "receiver_phone": "13800138000",
  "receiver_address": "江苏省苏州市…",
  "wechat_note": "老张",
  "items": [ { "spec_id": 3, "quantity": 5, "unit_price": 8800 } ],
  "freight_fee": 2000,
  "discount": 1000,
  "plan_ship_date": "2026-09-16",
  "remark": ""
}
```

`request_id` 在进入录单页时生成一次（`${openid后6位}-${时间戳}-${随机4位}`），
提交失败重试复用同一个，**提交成功后才换新的**。后端按它做幂等。

金额由后端算，前端页面上的「应收」只是展示用的预估值。

### Web 后台额外需要

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/login` | `{ username, password }` → `{ token, expires_at }`。失败次数超限返回 `42900` |
| POST | `/api/orders/batch-ship` | `{ ship_company, items: [{ order_id, tracking_no }] }` → `{ success, failed: [{ order_id, reason }] }` |
| GET | `/api/stats/summary?from=&to=` | 见下 |

统计返回：

```jsonc
{
  "order_count": 7,
  "total_amount": 526700,
  "paid_amount": 268200,
  "unpaid_amount": 258500,
  "crab_count": 59,
  "by_spec": [ { "gender": "male", "size": "4.5两", "quantity": 19, "amount": 167200 } ],
  "by_day":  [ { "date": "2026-09-14", "amount": 143800 } ]
}
```

### 买家查单页（不鉴权）

`GET /api/public/orders?order_no=&phone_tail=`

只返回下面这些，**不返回任何金额和备注**：

```jsonc
{
  "order_no": "20260914-007",
  "receiver_name": "张*",
  "receiver_phone": "138****8000",
  "receiver_address": "江苏省苏州市工业园区",
  "items_summary": "公4.5两×5 母3.5两×5",
  "ship_status": "shipped",
  "ship_company": "顺丰速运",
  "tracking_no": "SF1234567890",
  "created_at": "2026-09-14 10:42:00",
  "shipped_at": "2026-09-16 09:12:00",
  "received_at": ""
}
```

查不到时返回非 0 code 即可，页面统一提示「没找到这个订单，检查一下订单号和手机后四位」，
不区分是单号错了还是后四位错了。响应头请带 `X-Robots-Tag: noindex`。
