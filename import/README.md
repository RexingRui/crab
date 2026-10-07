# 批量录单

> 通常由 Claude 按 `.claude/skills/order-entry/SKILL.md` 的 SOP 整理好数据后调用本脚本。
>
> **仓库是公开的。** 真实订单（姓名、手机、地址）只放在会话的 scratchpad 或本目录下被忽略的文件里，
> `.gitignore` 已忽略 `import/*.csv` 与 `import/*.json`（模板除外），**绝对不要 `git add -f`**。
> token 只放环境变量，不写进任何文件。

## 用法

```bash
export CRAB_API=http://<服务器地址>
export CRAB_TOKEN=<管理员 token>
python3 scripts/import-orders.py <文件.csv|文件.json> --batch 1007            # 预览，不写库
python3 scripts/import-orders.py <文件.csv|文件.json> --batch 1007 --commit   # 真正录入
```

任何一单有错，整批都不录。中途断了直接重跑同一条命令：同一 `--batch` + `ref` 命中建单幂等，
已建的单按它当前的状态只补没做完的步骤（没收过款才记收款、待发货才发货、已发货才签收），
不会重复记收款。

## 计价

按只计价：**明细金额 = 只数 × 单只价，向上取整到元**，和后端同一个公式。
规格写名字（`母3两`、`公4两(残)`）或价目表 id；单只价不写就用价目表的，
写了就是这一单的临时价（单位元，最多三位小数，比如 `35`、`33.625`）。

## CSV：只建单 + 一笔收款

| 列 | 必填 | 说明 |
|---|---|---|
| ref | ✓ | 本批内的编号，不能重复；和 `--batch` 拼成 request_id |
| receiver_name / phone / address | ✓ | 收货人 / 11 位手机号 / 地址 |
| wechat_nick / wechat_remark | | 微信昵称 / 备注名 |
| items | ✓ | `规格*只数`，多项用 `;` 分隔；临时改价写 `规格*只数@单只价` |
| freight_yuan / discount_yuan | | 买家补的运费 / 优惠，元 |
| paid_yuan / pay_method | | 已收款（元）与方式（微信 / 支付宝 / 现金 / 转账 / 其他，默认微信） |
| expect_ship_date / remark | | 期望发货日 `YYYY-MM-DD` / 备注 |

## JSON：补录历史单（收款时间、发货、运费、签收都带上）

文件是一个数组，每个元素一单。金额都写**元**，时间写北京时间 `YYYY-MM-DD HH:MM[:SS]`：

```json
[{
  "ref": "1",
  "receiver_name": "示例张三", "phone": "13800000000", "address": "江苏省苏州市工业园区示例路1号",
  "wechat_nick": "付款人的微信昵称", "wechat_remark": "", "remark": "",
  "items": [{"spec": "母2.5两", "quantity": 8}, {"spec": "母3两", "quantity": 5, "unit_price": "35"}],
  "freight_fee": "13", "discount": "0",
  "payments": [{"amount": "202", "pay_method": "wechat", "paid_at": "2026-09-21 16:20:17", "remark": "P2"}],
  "ship": {"company": "顺丰", "tracking_no": "SF0000000000000", "time": "2026-09-22 15:44:09",
           "freight_list": "37", "freight_cost": "34"},
  "receive_time": "2026-09-23 11:22:03"
}]
```

- `freight_fee` 是**买家补的运费**，计入应收；`ship.freight_list` / `freight_cost` 是快递原价与实际花的钱，
  只给卖家看。买家补的可以超过实付（券是买来的）。
- `payments` 每笔单独记一条收款，`paid_at` 是实际收到钱的时间；退款写负数。
- 有 `receive_time` 就一并标记签收；没签收的单不写。
- 两个包裹就是两单。
