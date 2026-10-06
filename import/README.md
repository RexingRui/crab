# 批量录单

> 通常由 Claude 按 `.claude/skills/order-entry/SKILL.md` 的 SOP 从图片提取后调用本脚本。
>
> **仓库是公开的。** 真实订单（姓名、手机、地址）只放在本目录下的 CSV 里，
> `.gitignore` 已忽略 `import/*.csv`（模板除外），**绝对不要 `git add -f`**。
> token 只放环境变量，不写进任何文件。

## 用法

```bash
cp import/orders.template.csv import/1006.csv   # 用 Excel 或文本编辑器填
export CRAB_API=http://<服务器地址>
export CRAB_TOKEN=<管理员 token>
python3 scripts/import-orders.py import/1006.csv --batch 1006            # 预览，不写库
python3 scripts/import-orders.py import/1006.csv --batch 1006 --commit   # 真正录入
```

任何一行有错，整批都不录。中途网络断了直接重跑同一条命令：同一 `--batch` + `ref`
会命中建单幂等，已建的单原样跳过，也不会重复记收款。

## 列

| 列 | 必填 | 说明 |
|---|---|---|
| ref | ✓ | 本批内的行号/编号，不能重复；和 `--batch` 拼成 request_id |
| receiver_name | ✓ | 收货人 |
| phone | ✓ | 11 位手机号 |
| address | ✓ | 收货地址 |
| wechat_nick / wechat_remark | | 微信昵称 / 备注名 |
| items | ✓ | `规格*数量`，多项用 `;` 分隔。规格写价目表 id（如 `9`）或规格名；临时改价写 `规格*数量@单价元` |
| freight_yuan / discount_yuan | | 运费 / 优惠，单位元 |
| paid_yuan | | 已收款，元；大于 0 时建单后自动记一笔收款 |
| pay_method | | 微信 / 支付宝 / 现金 / 转账 / 其他，默认微信 |
| expect_ship_date | | 期望发货日 `YYYY-MM-DD` |
| remark | | 备注 |

单价不填时按价目表当前价；货款、应收由服务端计算。
