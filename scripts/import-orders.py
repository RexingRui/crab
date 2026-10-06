#!/usr/bin/env python3
"""
批量录单：读一个 CSV，逐行调 POST /api/orders 建单，有已收款的再记一笔收款。

    export CRAB_API=http://<服务器>          # 默认 http://127.0.0.1:8080
    export CRAB_TOKEN=<管理员 token>          # 只放环境变量，别写进任何文件
    python3 scripts/import-orders.py import/2026-10-06.csv --batch 1006          # 预览，不写库
    python3 scripts/import-orders.py import/2026-10-06.csv --batch 1006 --commit # 真正录入

CSV 格式见 import/orders.template.csv 与 import/README.md。只用标准库。

幂等：每行的 request_id = imp-<batch>-<ref>。同一批次重跑，已建过的单会被后端
原样返回（idempotent=true），这时也不会重复记收款。所以中途失败直接重跑即可。
"""
import argparse
import csv
import json
import os
import re
import sys
import urllib.error
import urllib.request
from decimal import Decimal, InvalidOperation

COLUMNS = [
    "ref", "receiver_name", "phone", "address", "wechat_nick", "wechat_remark",
    "items", "freight_yuan", "discount_yuan", "paid_yuan", "pay_method",
    "expect_ship_date", "remark",
]
PAY_METHODS = {"wechat": "wechat", "alipay": "alipay", "cash": "cash", "transfer": "transfer",
               "other": "other", "微信": "wechat", "支付宝": "alipay", "现金": "cash", "转账": "transfer", "其他": "other"}
ITEM_RE = re.compile(r"^(.+?)\s*[*xX×]\s*(\d+)\s*(?:@\s*([\d.]+))?$")


class RowError(Exception):
    pass


def api(method, path, body=None):
    base = os.environ.get("CRAB_API", "http://127.0.0.1:8080").rstrip("/")
    token = os.environ.get("CRAB_TOKEN", "")
    data = json.dumps(body, ensure_ascii=False).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("Authorization", "Bearer " + token)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        try:
            return json.load(e)
        except ValueError:
            return {"code": e.code, "msg": str(e), "data": None}


def yuan_to_fen(s, field):
    s = (s or "").strip()
    if s == "":
        return 0
    try:
        d = Decimal(s)
    except InvalidOperation:
        raise RowError(f"{field} 不是数字：{s}")
    if d != d.quantize(Decimal("0.01")):
        raise RowError(f"{field} 最多两位小数：{s}")
    return int(d * 100)


def read_csv(path):
    raw = open(path, "rb").read()
    for enc in ("utf-8-sig", "gbk"):  # Excel 另存为 CSV 在中文 Windows 上是 GBK
        try:
            text = raw.decode(enc)
            break
        except UnicodeDecodeError:
            continue
    else:
        sys.exit("✗ CSV 编码无法识别，请另存为 UTF-8")
    rows = list(csv.DictReader(text.splitlines()))
    if rows:
        missing = [c for c in ("ref", "receiver_name", "phone", "address", "items") if c not in rows[0]]
        if missing:
            sys.exit(f"✗ CSV 缺少列：{', '.join(missing)}")
    return rows


def find_spec(key, specs):
    key = key.strip()
    if key.isdigit() and int(key) in specs["by_id"]:
        return specs["by_id"][int(key)]
    hits = [s for s in specs["all"] if s["spec_label"] == key]
    if not hits:
        hits = [s for s in specs["all"] if key in s["spec_label"] and s["enabled"]]
    if len(hits) == 1:
        return hits[0]
    if not hits:
        raise RowError(f"找不到规格「{key}」")
    raise RowError(f"规格「{key}」匹配到多个：" + "、".join(f"{s['id']}={s['spec_label']}" for s in hits))


def build(row, specs, batch):
    ref = (row.get("ref") or "").strip()
    if not ref:
        raise RowError("ref 必填")
    phone = (row.get("phone") or "").strip()
    if not re.fullmatch(r"1\d{10}", phone):
        raise RowError(f"手机号不是 11 位：{phone}")

    items, warns = [], []
    for part in re.split(r"[;；\n]", row.get("items") or ""):
        part = part.strip()
        if not part:
            continue
        m = ITEM_RE.match(part)
        if not m:
            raise RowError(f"明细格式不对：「{part}」，应为 规格*数量 或 规格*数量@单价")
        sp = find_spec(m.group(1), specs)
        if not sp["enabled"]:
            warns.append(f"规格 {sp['id']}「{sp['spec_label']}」已停用")
        price = yuan_to_fen(m.group(3), "单价") if m.group(3) else sp["unit_price"]
        items.append({
            "gender": sp["gender"], "spec_gram": sp["spec_gram"], "spec_label": sp["spec_label"],
            "unit": sp["unit"], "quantity": int(m.group(2)), "unit_price": price,
            "pack_size": sp["pack_size"],
        })
    if not items:
        raise RowError("items 为空")

    pm = (row.get("pay_method") or "").strip() or "wechat"
    if pm not in PAY_METHODS:
        raise RowError(f"pay_method 不认识：{pm}")
    pm = PAY_METHODS[pm]

    body = {
        "request_id": f"imp-{batch}-{ref}",
        "receiver_name": row["receiver_name"].strip(),
        "phone": phone,
        "address": row["address"].strip(),
        "wechat_nick": (row.get("wechat_nick") or "").strip(),
        "wechat_remark": (row.get("wechat_remark") or "").strip(),
        "items": items,
        "freight_fee": yuan_to_fen(row.get("freight_yuan"), "运费"),
        "discount": yuan_to_fen(row.get("discount_yuan"), "优惠"),
        "expect_ship_date": (row.get("expect_ship_date") or "").strip(),
        "remark": (row.get("remark") or "").strip(),
    }
    paid = yuan_to_fen(row.get("paid_yuan"), "已收款")
    return body, paid, pm, warns


def fmt(fen):
    return f"{fen / 100:.2f}"


def main():
    ap = argparse.ArgumentParser(description="批量录单")
    ap.add_argument("csv")
    ap.add_argument("--batch", required=True, help="批次名，拼进 request_id；同一批重跑不会重复建单")
    ap.add_argument("--commit", action="store_true", help="真正写入；不加只预览")
    args = ap.parse_args()
    if not re.fullmatch(r"[\w-]+", args.batch):
        sys.exit("✗ --batch 只能用字母数字下划线和横线")
    if not os.environ.get("CRAB_TOKEN"):
        sys.exit("✗ 请先 export CRAB_TOKEN=...")

    r = api("GET", "/api/specs?all=1")
    if r.get("code") != 0:
        sys.exit(f"✗ 拉价目表失败：{r.get('code')} {r.get('msg')}")
    all_specs = r["data"]["list"]
    specs = {"all": all_specs, "by_id": {s["id"]: s for s in all_specs}}

    rows = read_csv(args.csv)
    plans, bad = [], 0
    refs = set()
    for i, row in enumerate(rows, start=2):  # 第 1 行是表头
        try:
            body, paid, pm, warns = build(row, specs, args.batch)
            if body["request_id"] in refs:
                raise RowError(f"ref 重复：{row['ref']}")
            refs.add(body["request_id"])
        except RowError as e:
            bad += 1
            print(f"✗ 第{i}行 {row.get('receiver_name', '')}：{e}")
            continue
        goods = sum(it["quantity"] * it["unit_price"] for it in body["items"])
        payable = goods + body["freight_fee"] - body["discount"]
        summary = "，".join(f"{it['spec_label']}×{it['quantity']}" for it in body["items"])
        print(f"· 第{i}行 [{row['ref']}] {body['receiver_name']} {body['phone'][:3]}****{body['phone'][-4:]}"
              f"  {summary}  应收 {fmt(payable)}" + (f"  已收 {fmt(paid)}" if paid else ""))
        for w in warns:
            print(f"    ⚠ {w}")
        plans.append((i, body, paid, pm, payable))

    total = sum(p[4] for p in plans)
    print(f"\n共 {len(rows)} 行：可录 {len(plans)}，有错 {bad}；合计应收 {fmt(total)}")
    if bad:
        sys.exit("✗ 先改好有错的行再录（整批不动，避免录一半）")
    if not args.commit:
        print("预览完毕，确认无误后加 --commit 真正录入")
        return

    ok = 0
    for i, body, paid, pm, _ in plans:
        r = api("POST", "/api/orders", body)
        if r.get("code") != 0:
            print(f"✗ 第{i}行 建单失败：{r.get('code')} {r.get('msg')}")
            continue
        o = r["data"]
        if r.get("idempotent") or o.get("idempotent"):
            print(f"= 第{i}行 已存在 {o['order_no']}，跳过（不重复记收款）")
            ok += 1
            continue
        line = f"✓ 第{i}行 {o['order_no']} 应收 {o['payable_amount_yuan']}"
        if paid:
            pr = api("POST", f"/api/orders/{o['id']}/payments",
                     {"amount": paid, "pay_method": pm, "remark": f"批量录入 {args.batch}"})
            if pr.get("code") != 0:
                line += f"  ✗ 收款失败：{pr.get('msg')}（订单已建，请手工补记）"
            else:
                line += f"  已收 {fmt(paid)}"
        print(line)
        ok += 1
    print(f"\n完成 {ok}/{len(plans)}")


if __name__ == "__main__":
    main()
