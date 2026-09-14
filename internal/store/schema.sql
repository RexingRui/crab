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

    remark            TEXT    NOT NULL DEFAULT '',
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

-- ========== 订单明细 ==========
CREATE TABLE IF NOT EXISTS order_items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    gender      TEXT    NOT NULL,                       -- male/female/mixed
    spec_gram   INTEGER NOT NULL,                       -- 规格克数，4.5两=225
    spec_label  TEXT    NOT NULL,                       -- 展示用："4.5两"
    unit        TEXT    NOT NULL DEFAULT 'piece',       -- piece/box/jin
    quantity    INTEGER NOT NULL,
    unit_price  INTEGER NOT NULL,                       -- 单价快照（分）
    amount      INTEGER NOT NULL,                       -- = quantity * unit_price
    sort_no     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_items_order ON order_items(order_id);

-- ========== 收款流水 ==========
CREATE TABLE IF NOT EXISTS payments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    amount      INTEGER NOT NULL,                       -- 分；退款记负数
    pay_method  TEXT    NOT NULL DEFAULT 'wechat',      -- wechat/alipay/cash/transfer/other
    paid_at     INTEGER NOT NULL,
    remark      TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    deleted_at  INTEGER
);
CREATE INDEX IF NOT EXISTS idx_payments_order ON payments(order_id, deleted_at);

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
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_logs_order ON order_logs(order_id, created_at);

-- ========== 规格价目表（快速录单用） ==========
CREATE TABLE IF NOT EXISTS specs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    gender      TEXT    NOT NULL,
    spec_gram   INTEGER NOT NULL,
    spec_label  TEXT    NOT NULL,
    unit        TEXT    NOT NULL DEFAULT 'piece',
    unit_price  INTEGER NOT NULL,                       -- 当季参考价
    enabled     INTEGER NOT NULL DEFAULT 1,
    sort_no     INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_specs ON specs(gender, spec_gram, unit);

-- ========== 订单号日序列 ==========
CREATE TABLE IF NOT EXISTS order_seq (
    date_key    TEXT PRIMARY KEY,                       -- YYYYMMDD
    seq         INTEGER NOT NULL
);
