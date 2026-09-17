-- 把价目表换成当季的 4 档 8 只装套餐。
--
-- 什么时候用：种子价目表只在 specs 表为空时写入，已经跑起来的库不会被覆盖，
-- 所以老库升级后要手动换一次。正常入口是小程序「设置」页，这个 SQL 是
-- 「小程序用不了」时的后路。
--
-- 怎么跑（容器化部署）：
--   cd /opt/crab-order
--   docker compose exec -T api sqlite3 /data/crab.db < scripts/price-packs.sql
--   docker compose exec -T api sqlite3 /data/crab.db \
--     "SELECT id,spec_label,unit,unit_price,pack_size,enabled FROM specs ORDER BY sort_no;"
--
-- 跑之前先确认 pack_size 这一列已经在了（后端升级时自动补的）：
--   docker compose exec -T api sqlite3 /data/crab.db "PRAGMA table_info(specs);"
--
-- 安全性：只停用旧档、不物理删除，历史订单的明细是快照，金额不受任何影响。
-- 可重复执行：撞唯一索引时改成更新，跑第二遍不会报错也不会插重复。

BEGIN IMMEDIATE;

-- 1) 先把现有启用中的档全停掉。必须排在插入前面，否则会把刚加的四档一起停掉。
UPDATE specs SET enabled = 0, updated_at = strftime('%s', 'now') WHERE enabled = 1;

-- 2) 四档套餐。unit_price 是整盒价（分），spec_gram 是整盒克重，pack_size 是一盒几只。
--    公母比例不在这儿存——那是买家每一单自己定的。
INSERT INTO specs (gender, spec_gram, spec_label, unit, unit_price, pack_size, enabled, sort_no, updated_at)
VALUES
    ('mixed', 1200, '8只装 母2.5两/公3.5两',      'box', 18900, 8, 1, 1, strftime('%s', 'now')),
    ('mixed', 1400, '8只装 母3.0两/公4.0两',      'box', 26900, 8, 1, 2, strftime('%s', 'now')),
    ('mixed', 1600, '8只装 母3.5两/公4.5两',      'box', 35900, 8, 1, 3, strftime('%s', 'now')),
    ('mixed', 1800, '8只装 母4.0两/公5.0两',      'box', 43900, 8, 1, 4, strftime('%s', 'now'))
ON CONFLICT(gender, spec_gram, unit) DO UPDATE SET
    spec_label = excluded.spec_label,
    unit_price = excluded.unit_price,
    pack_size  = excluded.pack_size,
    enabled    = 1,
    sort_no    = excluded.sort_no,
    updated_at = excluded.updated_at;

COMMIT;
