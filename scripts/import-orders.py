#!/usr/bin/env python3
"""
批量录单：读 CSV 或 JSON，逐单调接口建单；JSON 还能带上收款（含收款时间）、发货（含运费）、签收，
用来补录历史订单。

    export CRAB_API=http://<服务器>          # 默认 http://127.0.0.1:8080
    export CRAB_TOKEN=<管理员 token>          # 只放环境变量，别写进任何文件
    python3 scripts/import-orders.py <文件.csv|文件.json> --batch 1006          # 预览，不写库
    python3 scripts/import-orders.py <文件.csv|文件.json> --batch 1006 --commit # 真正录入

格式见 import/README.md。只用标准库。

计价和后端同一个公式：明细金额 = 只数 × 单只价，向上取整到元。单只价不写就用价目表的。

可以放心重跑：每单的 request_id = imp-<batch>-<ref>，同一批次重跑命中建单幂等，
拿回已经建好的那单，再按它当前的状态只补没做完的步骤（没收过款才记收款、
待发货才发货、已发货才签收），不会重复记收款。
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

PAY_METHODS = {"wechat": "wechat", "alipay": "alipay", "cash": "cash", "transfer": "transfer",
               "other": "other", "微信": "wechat", "支付宝": "alipay", "现金": "cash", "转账": "transfer",
               "其他": "other"}
CSV_ITEM_RE = re.compile(r"^(.+?)\s*[*xX×]\s*(\d+)\s*(?:@\s*([\d.]+))?$")
TIME_RE = re.compile(r"^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})(?::(\d{2}))?$")


class RowError(Exception):
    pass


def api(method, path, body=None):
    base = os.environ.get("CRAB_API", "http://127.0.0.1:8080").rstrip("/")
    data = json.dumps(body, ensure_ascii=False).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("Authorization", "Bearer " + os.environ.get("CRAB_TOKEN", ""))
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


# ---------- 金额与时间 ----------

def to_units(v, field, places, per_yuan):
    """元（字符串或数字）→ 分（places=2）或厘（places=3）。整数运算，拒绝多余小数位。"""
    s = str(v if v is not None else "").strip()
    if s == "":
        return 0
    try:
        d = Decimal(s)
    except InvalidOperation:
        raise RowError(f"{field} 不是数字：{s}")
    q = Decimal(1).scaleb(-places)
    if d != d.quantize(q):
        raise RowError(f"{field} 最多 {places} 位小数：{s}")
    return int(d * per_yuan)


def fen(v, field):
    return to_units(v, field, 2, 100)


def milli(v, field):
    return to_units(v, field, 3, 1000)


def line_amount(qty, unit_milli):
    """和后端 model.LineAmount 同一个公式：只数 × 单只价，向上取整到元，返回分。"""
    return -(-qty * unit_milli // 1000) * 100


def rfc3339(v, field):
    """「2026-09-24 19:33:28」（北京时间）→ RFC3339。空值返回 None。"""
    s = str(v or "").strip()
    if not s:
        return None
    m = TIME_RE.match(s)
    if not m:
        raise RowError(f"{field} 时间格式应为 YYYY-MM-DD HH:MM[:SS]：{s}")
    y, mo, d, h, mi, sec = m.groups()
    return f"{y}-{mo}-{d}T{h}:{mi}:{sec or '00'}+08:00"


def yuan(f):
    return f"{f / 100:.2f}"


# ---------- 读文件 ----------

def read_text(path):
    raw = open(path, "rb").read()
    for enc in ("utf-8-sig", "gbk"):  # Excel 另存为 CSV 在中文 Windows 上是 GBK
        try:
            return raw.decode(enc)
        except UnicodeDecodeError:
            continue
    sys.exit("✗ 文件编码无法识别，请另存为 UTF-8")


def csv_orders(text):
    """CSV 一行一单（只建单 + 一笔收款），转成和 JSON 同样的结构。"""
    rows = list(csv.DictReader(text.splitlines()))
    if rows:
        missing = [c for c in ("ref", "receiver_name", "phone", "address", "items") if c not in rows[0]]
        if missing:
            sys.exit(f"✗ CSV 缺少列：{', '.join(missing)}")
    out = []
    for row in rows:
        items = []
        for part in re.split(r"[;；\n]", row.get("items") or ""):
            part = part.strip()
            if not part:
                continue
            m = CSV_ITEM_RE.match(part)
            if not m:
                items.append({"_bad": part})
                continue
            it = {"spec": m.group(1).strip(), "quantity": int(m.group(2))}
            if m.group(3):
                it["unit_price"] = m.group(3)
            items.append(it)
        o = {k: (row.get(k) or "").strip() for k in
             ("ref", "receiver_name", "phone", "address", "wechat_nick", "wechat_remark",
              "expect_ship_date", "remark")}
        o["items"] = items
        o["freight_fee"] = row.get("freight_yuan") or ""
        o["discount"] = row.get("discount_yuan") or ""
        paid = (row.get("paid_yuan") or "").strip()
        if paid:
            o["payments"] = [{"amount": paid, "pay_method": (row.get("pay_method") or "").strip()}]
        out.append(o)
    return out


# ---------- 校验与换算 ----------

def find_spec(key, specs):
    key = str(key).strip()
    if key.isdigit() and int(key) in specs["by_id"]:
        return specs["by_id"][int(key)]
    hits = [s for s in specs["all"] if s["title"] == key]
    if not hits:
        hits = [s for s in specs["all"] if key in s["title"] and s["enabled"]]
    if len(hits) == 1:
        return hits[0]
    if not hits:
        raise RowError(f"找不到规格「{key}」（写价目表 id，或「母3两」「公4两(残)」这样的名字）")
    raise RowError(f"规格「{key}」匹配到多个：" + "、".join(f"{s['id']}={s['title']}" for s in hits))


def build(o, specs, batch):
    """一单 → 建单请求 + 后续步骤。返回 (plan, warns)。"""
    ref = str(o.get("ref") or "").strip()
    if not ref:
        raise RowError("ref 必填")
    phone = str(o.get("phone") or "").strip()
    if not re.fullmatch(r"1\d{10}", phone):
        raise RowError(f"手机号不是 11 位：{phone}")

    warns, items = [], []
    for it in o.get("items") or []:
        if "_bad" in it:
            raise RowError(f"明细格式不对：「{it['_bad']}」，应为 规格*只数 或 规格*只数@单只价")
        sp = find_spec(it.get("spec"), specs)
        if not sp["enabled"]:
            warns.append(f"规格 {sp['id']}「{sp['title']}」已停用")
        qty = int(it.get("quantity") or 0)
        if qty < 1:
            raise RowError(f"「{sp['title']}」只数要大于 0")
        price = milli(it["unit_price"], "单只价") if it.get("unit_price") not in (None, "") else sp["unit_price_milli"]
        items.append({
            "gender": sp["gender"], "spec_gram": sp["spec_gram"], "grade": sp["grade"],
            "spec_label": sp["spec_label"], "quantity": qty, "unit_price_milli": price,
            "_title": sp["title"],
        })
    if not items:
        raise RowError("items 为空")

    goods = sum(line_amount(i["quantity"], i["unit_price_milli"]) for i in items)
    freight_fee = fen(o.get("freight_fee"), "买家补运费")
    discount = fen(o.get("discount"), "优惠")
    payable = goods + freight_fee - discount
    if payable < 0:
        raise RowError("优惠超过了货款加运费")

    payments = []
    for p in o.get("payments") or []:
        pm = str(p.get("pay_method") or "").strip() or "wechat"
        if pm not in PAY_METHODS:
            raise RowError(f"pay_method 不认识：{pm}")
        amt = fen(p.get("amount"), "收款金额")
        if amt == 0:
            continue
        body = {"amount": amt, "pay_method": PAY_METHODS[pm], "remark": str(p.get("remark") or "")}
        t = rfc3339(p.get("paid_at"), "收款时间")
        if t:
            body["paid_at"] = t
        payments.append(body)
    paid = sum(p["amount"] for p in payments)
    if payments and paid != payable:
        warns.append(f"收款合计 {yuan(paid)} ≠ 应收 {yuan(payable)}")

    ship = None
    s = o.get("ship")
    if s:
        if not s.get("tracking_no"):
            raise RowError("发货要有快递单号")
        ship = {"ship_company": s.get("company") or "顺丰", "tracking_no": s["tracking_no"]}
        t = rfc3339(s.get("time"), "发货时间")
        if t:
            ship["ship_time"] = t
        if s.get("freight_cost") not in (None, ""):
            fr = {"freight_cost": fen(s["freight_cost"], "实付运费"), "freight_basis": "actual",
                  # 买家补的运费已经在建单时定了，这里原样带上，否则后端会按规则重算覆盖掉
                  "freight_fee": freight_fee}
            if s.get("freight_list") not in (None, ""):
                fr["freight_list"] = fen(s["freight_list"], "快递原价")
            ship["freight"] = fr
    receive = rfc3339(o.get("receive_time"), "签收时间")
    if receive and not ship:
        raise RowError("有签收时间却没有发货信息")

    req = {
        "request_id": f"imp-{batch}-{ref}",
        "receiver_name": str(o.get("receiver_name") or "").strip(),
        "phone": phone,
        "address": str(o.get("address") or "").strip(),
        "wechat_nick": str(o.get("wechat_nick") or "").strip(),
        "wechat_remark": str(o.get("wechat_remark") or "").strip(),
        "items": [{k: v for k, v in i.items() if not k.startswith("_")} for i in items],
        "freight_fee": freight_fee,
        "discount": discount,
        "expect_ship_date": str(o.get("expect_ship_date") or "").strip(),
        "remark": str(o.get("remark") or "").strip(),
    }
    plan = {"ref": ref, "req": req, "items": items, "goods": goods, "payable": payable,
            "payments": payments, "paid": paid, "ship": ship, "receive": receive}
    return plan, warns


# ---------- 执行 ----------

def step(label, r):
    if r.get("code") != 0:
        raise RowError(f"{label}失败：{r.get('code')} {r.get('msg')}")
    return r["data"]


def run(plan):
    """建单，再按订单当前状态补齐收款 / 发货 / 签收。重跑时已做过的步骤自动跳过。"""
    r = api("POST", "/api/orders", plan["req"])
    o = step("建单", r)
    resumed = bool(r.get("idempotent") or o.get("idempotent"))
    oid, notes = o["id"], []

    if plan["payments"]:
        if o.get("paid_amount", 0) == 0:
            for p in plan["payments"]:
                o = step("记收款", api("POST", f"/api/orders/{oid}/payments", p))
            notes.append(f"收款 {len(plan['payments'])} 笔")
        elif o.get("paid_amount") != plan["paid"]:
            notes.append(f"⚠ 已有收款 {yuan(o['paid_amount'])}，与计划 {yuan(plan['paid'])} 不符，没动，请人工核对")
    if plan["ship"] and o.get("ship_status") == "pending":
        o = step("发货", api("POST", f"/api/orders/{oid}/ship", plan["ship"]))
        notes.append("已发货")
    if plan["receive"] and o.get("ship_status") == "shipped":
        o = step("签收", api("POST", f"/api/orders/{oid}/receive", {"receive_time": plan["receive"]}))
        notes.append("已签收")
    return o, resumed, notes


def main():
    ap = argparse.ArgumentParser(description="批量录单")
    ap.add_argument("file", help=".csv 或 .json")
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
    if all_specs and "unit_price_milli" not in all_specs[0]:
        sys.exit("✗ 后端还是按盒计价的旧版本，先部署按只计价的版本再录")
    specs = {"all": all_specs, "by_id": {s["id"]: s for s in all_specs}}

    text = read_text(args.file)
    orders = json.loads(text) if args.file.lower().endswith(".json") else csv_orders(text)
    if isinstance(orders, dict):
        orders = orders.get("orders") or []

    plans, bad, refs = [], 0, set()
    for o in orders:
        tag = f"[{o.get('ref', '?')}] {o.get('receiver_name', '')}"
        try:
            plan, warns = build(o, specs, args.batch)
            if plan["ref"] in refs:
                raise RowError("ref 重复")
            refs.add(plan["ref"])
        except RowError as e:
            bad += 1
            print(f"✗ {tag}：{e}")
            continue
        summary = "，".join(f"{i['_title']}×{i['quantity']}" for i in plan["items"])
        extra = []
        if plan["req"]["freight_fee"]:
            extra.append(f"补运费 {yuan(plan['req']['freight_fee'])}")
        if plan["req"]["discount"]:
            extra.append(f"优惠 {yuan(plan['req']['discount'])}")
        if plan["payments"]:
            extra.append(f"收款 {yuan(plan['paid'])}")
        if plan["ship"]:
            fr = plan["ship"].get("freight") or {}
            extra.append("发货" + (f"(实付运费 {yuan(fr['freight_cost'])})" if fr else ""))
        if plan["receive"]:
            extra.append("签收")
        masked = plan["req"]["phone"][:3] + "****" + plan["req"]["phone"][-4:]
        print(f"· {tag} {masked}  {summary}  应收 {yuan(plan['payable'])}  " + "  ".join(extra))
        for w in warns:
            print(f"    ⚠ {w}")
        plans.append(plan)

    total = sum(p["payable"] for p in plans)
    paid = sum(p["paid"] for p in plans)
    print(f"\n共 {len(orders)} 单：可录 {len(plans)}，有错 {bad}；合计应收 {yuan(total)}，收款 {yuan(paid)}")
    if bad:
        sys.exit("✗ 先改好有错的单再录（整批不动，避免录一半）")
    if not args.commit:
        print("预览完毕，确认无误后加 --commit 真正录入")
        return

    ok = 0
    for plan in plans:
        try:
            o, resumed, notes = run(plan)
        except RowError as e:
            print(f"✗ [{plan['ref']}] {e}（重跑同一条命令会从这一步接着做）")
            continue
        head = "= " if resumed else "✓ "
        print(f"{head}[{plan['ref']}] {o['order_no']} 应收 {o['payable_amount_yuan']} 已收 {o['paid_amount_yuan']}"
              + (f"  {'，'.join(notes)}" if notes else ("  已存在，无需补做" if resumed else "")))
        ok += 1
    print(f"\n完成 {ok}/{len(plans)}")


if __name__ == "__main__":
    main()
