-- ========== 订单主表 ==========
CREATE TABLE IF NOT EXISTS orders (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    order_no          TEXT    NOT NULL UNIQUE,          -- 业务单号 20260914-007
    request_id        TEXT,                             -- 客户端幂等键

    -- 收货信息
    receiver_name     TEXT    NOT NULL,
    phone             TEXT    NOT NULL,
    address           TEXT    NOT NULL,                 -- 完整地址（省市区+详址）

    -- 买家身份（用于和微信聊天窗口对应）
    wechat_nick       TEXT    NOT NULL DEFAULT '',      -- 微信昵称
    wechat_remark     TEXT    NOT NULL DEFAULT '',      -- 自己给对方打的备注名

    -- 金额（单位：分）
    goods_amount      INTEGER NOT NULL DEFAULT 0,       -- 货款 = sum(items.amount)
    freight_fee       INTEGER NOT NULL DEFAULT 0,       -- 运费
    discount          INTEGER NOT NULL DEFAULT 0,       -- 优惠（正数，表示减免额）
    payable_amount    INTEGER NOT NULL DEFAULT 0,       -- 应收 = goods + freight - discount
    paid_amount       INTEGER NOT NULL DEFAULT 0,       -- 实收 = sum(payments.amount)

    -- 状态
    ship_status       TEXT    NOT NULL DEFAULT 'pending',
    pay_status        TEXT    NOT NULL DEFAULT 'unpaid',

    -- 物流
    ship_company      TEXT    NOT NULL DEFAULT '',
    tracking_no       TEXT    NOT NULL DEFAULT '',

    -- 时间
    expect_ship_date  TEXT,                             -- 约定发货日 YYYY-MM-DD
    ship_time         INTEGER,
    receive_time      INTEGER,
    first_pay_time    INTEGER,                          -- 首次收款时间
    settled_time      INTEGER,                          -- 付清时间

    -- 运费（只给卖家看；freight_fee 是买家承担的那部分）
    freight_list      INTEGER,                          -- 快递原价（分）
    freight_cost      INTEGER,                          -- 用券后的实付（分），NULL = 运费待定
    freight_basis     TEXT    NOT NULL DEFAULT '',      -- 买家补多少按 list(原价) / actual(实付) 算
    freight_rule_ver  TEXT    NOT NULL DEFAULT 'v1',    -- 建单时的补贴规则版本
    freight_settled_at INTEGER,                         -- 和快递结清的时间

    remark            TEXT    NOT NULL DEFAULT '',
    source            TEXT    NOT NULL DEFAULT 'manual',  -- manual=卖家录入 / web=买家自助登记
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    deleted_at        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_orders_ship   ON orders(ship_status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_orders_pay    ON orders(pay_status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_orders_phone  ON orders(phone);
CREATE INDEX IF NOT EXISTS idx_orders_expect ON orders(expect_ship_date, ship_status);
CREATE INDEX IF NOT EXISTS idx_orders_ctime  ON orders(created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uk_orders_request ON orders(request_id)
    WHERE request_id IS NOT NULL AND request_id != '';

-- ========== 订单明细（一律按只记） ==========
CREATE TABLE IF NOT EXISTS order_items (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id         INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    gender           TEXT    NOT NULL,                  -- male/female（改版前的老明细可能是 mixed）
    spec_gram        INTEGER NOT NULL,                  -- 规格克数，3两=150
    grade            TEXT    NOT NULL DEFAULT 'normal', -- normal 正常 / broken 残蟹
    spec_label       TEXT    NOT NULL,                  -- 展示用："3两"
    quantity         INTEGER NOT NULL,                  -- 只数
    unit_price_milli INTEGER NOT NULL,                  -- 单只价快照（厘，0.001 元）
    amount           INTEGER NOT NULL,                  -- 分，= 只数 × 单只价，向上取整到元
    sort_no          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_items_order ON order_items(order_id);

-- ========== 操作流水 ==========
CREATE TABLE IF NOT EXISTS order_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL,
    action      TEXT    NOT NULL,                       -- create/update/ship/receive/pay/refund/cancel/delete/revert
    field       TEXT    NOT NULL DEFAULT '',            -- ship_status / pay_status / ...
    from_value  TEXT    NOT NULL DEFAULT '',
    to_value    TEXT    NOT NULL DEFAULT '',
    operator    TEXT    NOT NULL DEFAULT '',            -- openid 或 'system'
    remark      TEXT    NOT NULL DEFAULT '',
    amount      INTEGER NOT NULL DEFAULT 0,             -- 收款/退款这一笔的金额（分），其他动作为 0
    pay_method  TEXT    NOT NULL DEFAULT '',            -- 收款方式，只有收款/退款才有
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_logs_order ON order_logs(order_id, created_at);

-- ========== 规格价目表（按只计价，没有「盒」） ==========
CREATE TABLE IF NOT EXISTS specs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    gender           TEXT    NOT NULL,                  -- male/female
    spec_gram        INTEGER NOT NULL,
    grade            TEXT    NOT NULL DEFAULT 'normal', -- normal 正常 / broken 残蟹
    spec_label       TEXT    NOT NULL,
    unit_price_milli INTEGER NOT NULL,                  -- 当季单只价（厘，0.001 元）
    enabled          INTEGER NOT NULL DEFAULT 1,
    sort_no          INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_specs_piece ON specs(gender, spec_gram, grade);

-- ========== 订单号日序列 ==========
CREATE TABLE IF NOT EXISTS order_seq (
    date_key    TEXT PRIMARY KEY,                       -- YYYYMMDD
    seq         INTEGER NOT NULL
);
