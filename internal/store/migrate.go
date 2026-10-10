package store

import (
	"context"
	_ "embed"
	"fmt"

	"crab-order/internal/model"
)

//go:embed schema.sql
var schemaSQL string

// addedColumns 是建表之后才加进来的列。schema.sql 里的 CREATE TABLE 带 IF NOT EXISTS，
// 对已经建过表的库不会生效，所以增量列得单独补一次。
// SQLite 没有 ADD COLUMN IF NOT EXISTS，只能先查 pragma_table_info 再决定加不加。
// backfill 只在这一列刚加上时跑一次，给老数据补值。
var addedColumns = []struct{ table, column, ddl, backfill string }{
	{"orders", "source", "ALTER TABLE orders ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'", ""},
	{"orders", "freight_list", "ALTER TABLE orders ADD COLUMN freight_list INTEGER", ""},
	{"orders", "freight_cost", "ALTER TABLE orders ADD COLUMN freight_cost INTEGER", ""},
	{"orders", "freight_basis", "ALTER TABLE orders ADD COLUMN freight_basis TEXT NOT NULL DEFAULT ''", ""},
	// 老单一律算第一版规则：上线这版规则之前没有别的规则
	{"orders", "freight_rule_ver", "ALTER TABLE orders ADD COLUMN freight_rule_ver TEXT NOT NULL DEFAULT 'v1'", ""},
	{"orders", "freight_settled_at", "ALTER TABLE orders ADD COLUMN freight_settled_at INTEGER", ""},
	{"order_logs", "amount", "ALTER TABLE order_logs ADD COLUMN amount INTEGER NOT NULL DEFAULT 0", ""},
	{"order_logs", "pay_method", "ALTER TABLE order_logs ADD COLUMN pay_method TEXT NOT NULL DEFAULT ''", ""},
}

// legacyBoxColumns 是按盒计价那一版的规格表 / 明细表上、改版前陆续补进来的列。
// 只在把老库转成按只计价之前补一次，好让转换用的 SQL 能统一读到它们。
var legacyBoxColumns = []struct{ table, column, ddl, backfill string }{
	{"specs", "pack_size", "ALTER TABLE specs ADD COLUMN pack_size INTEGER NOT NULL DEFAULT 0", ""},
	{"order_items", "crab_count", "ALTER TABLE order_items ADD COLUMN crab_count INTEGER NOT NULL DEFAULT 0",
		// 老明细没记每盒几只，按「性别 + 克重 + 单位」回价目表找；找不到的盒按 0 只算。
		`UPDATE order_items SET crab_count = CASE unit
			WHEN 'piece' THEN quantity
			WHEN 'box' THEN quantity * COALESCE((SELECT s.pack_size FROM specs s
				WHERE s.gender = order_items.gender AND s.spec_gram = order_items.spec_gram
				  AND s.unit = 'box'), 0)
			ELSE 0 END`},
}

// Migrate 建表（全部语句都是 IF NOT EXISTS，可重复执行），再补增量列。
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	// 老库的规格表还是按盒的那一版（有 unit 列）：先转成按只计价，
	// 再跑建表语句——新的唯一索引引用了 grade 列，老表上建不起来。
	legacy, err := s.hasColumn(ctx, "specs", "unit")
	if err != nil {
		return err
	}
	if legacy {
		if err := s.migrateBoxToPiece(ctx); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	for _, c := range addedColumns {
		ok, err := s.hasColumn(ctx, c.table, c.column)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := s.db.ExecContext(ctx, c.ddl); err != nil {
			return fmt.Errorf("migrate add column %s.%s: %w", c.table, c.column, err)
		}
		if c.backfill != "" {
			if _, err := s.db.ExecContext(ctx, c.backfill); err != nil {
				return fmt.Errorf("migrate backfill %s.%s: %w", c.table, c.column, err)
			}
		}
	}
	return s.migrateLegacyPayments(ctx)
}

// migrateLegacyPayments 收款流水表已经并进操作流水。老库里还在的 payments 表：
// 把没删的每一笔抄进 order_logs（金额、方式、备注、收款时间），再改名成 payments_legacy 留底。
// 改名之后这一步就不会再跑，可重复执行。
func (s *SQLiteStore) migrateLegacyPayments(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='payments'`).Scan(&n); err != nil {
		return fmt.Errorf("check payments table: %w", err)
	}
	if n == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO order_logs(order_id, action, field, from_value, to_value, operator, remark,
				amount, pay_method, created_at)
			 SELECT order_id, CASE WHEN amount < 0 THEN 'refund' ELSE 'pay' END, 'paid_amount', '', '',
				'', remark, amount, pay_method, paid_at
			 FROM payments WHERE deleted_at IS NULL`); err != nil {
		return fmt.Errorf("copy payments to logs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE payments RENAME TO payments_legacy`); err != nil {
		return fmt.Errorf("rename payments: %w", err)
	}
	return tx.Commit()
}

// migrateBoxToPiece 把按盒计价的老库转成按只计价：
//
//   - 明细表重建：数量换成只数（按盒的 = 盒数 × 每盒只数），单价换成单只价（厘），
//     金额原样保留——历史订单收了多少钱，转换前后一分不变。
//     折不出只数的老明细（按斤、或者查不到每盒几只的盒）数量和单价照旧，只是单位换成厘。
//   - 规格表改名成 specs_legacy 留底，随后建表语句建出新的空表，
//     启动时的 SeedSpecs 会写入按只计价的价目表。
//
// 整个转换在一个事务里，失败就原样回滚。转完规格表就没有 unit 列了，不会再跑第二次。
func (s *SQLiteStore) migrateBoxToPiece(ctx context.Context) error {
	for _, c := range legacyBoxColumns {
		ok, err := s.hasColumn(ctx, c.table, c.column)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := s.db.ExecContext(ctx, c.ddl); err != nil {
			return fmt.Errorf("migrate add column %s.%s: %w", c.table, c.column, err)
		}
		if c.backfill != "" {
			if _, err := s.db.ExecContext(ctx, c.backfill); err != nil {
				return fmt.Errorf("migrate backfill %s.%s: %w", c.table, c.column, err)
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmts := []string{
		`CREATE TABLE order_items_piece (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id         INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			gender           TEXT    NOT NULL,
			spec_gram        INTEGER NOT NULL,
			grade            TEXT    NOT NULL DEFAULT 'normal',
			spec_label       TEXT    NOT NULL,
			quantity         INTEGER NOT NULL,
			unit_price_milli INTEGER NOT NULL,
			amount           INTEGER NOT NULL,
			sort_no          INTEGER NOT NULL DEFAULT 0
		)`,
		// 只数折得出来的：单只价 = 金额 / 只数，四舍五入到厘（金额是分，×10 换成厘）。
		`INSERT INTO order_items_piece(id, order_id, gender, spec_gram, grade, spec_label,
				quantity, unit_price_milli, amount, sort_no)
			SELECT id, order_id, gender, spec_gram, 'normal', spec_label,
				CASE WHEN crab_count > 0 THEN crab_count ELSE quantity END,
				CASE WHEN crab_count > 0 THEN (amount * 10 + crab_count / 2) / crab_count
				     ELSE unit_price * 10 END,
				amount, sort_no
			FROM order_items`,
		`DROP TABLE order_items`,
		`ALTER TABLE order_items_piece RENAME TO order_items`,
		`DROP INDEX IF EXISTS uk_specs`,
		`ALTER TABLE specs RENAME TO specs_legacy`,
	}
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migrate box to piece: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) hasColumn(ctx context.Context, table, column string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("pragma_table_info %s: %w", table, err)
	}
	return n > 0, nil
}

// seedSpecs 是首次启动时写入的当季价目表，按只计价。
//
// 单只价由去年的 8 只混装套餐价摊出来：一盒 4 母 4 公，母 X 两和公 X+1 两同价，
// 189 元一盒 → 23.625 元一只。所以 8 只正好是 189、279、359、439
// （第二档原是 269，10 月起调到 279，单只 34.875 元）。
// 残蟹还没定价，不写种子，定了价在「设置」页加上。
//
// 只在 specs 表为空时插入。已经跑起来的库不会被覆盖——换价目表去小程序「设置」页改。
var seedSpecs = []model.Spec{
	{Gender: model.GenderFemale, SpecGram: 125, SpecLabel: "2.5两", UnitPriceMilli: 23625, SortNo: 1},
	{Gender: model.GenderFemale, SpecGram: 150, SpecLabel: "3两", UnitPriceMilli: 34875, SortNo: 2},
	{Gender: model.GenderFemale, SpecGram: 175, SpecLabel: "3.5两", UnitPriceMilli: 44875, SortNo: 3},
	{Gender: model.GenderFemale, SpecGram: 200, SpecLabel: "4两", UnitPriceMilli: 54875, SortNo: 4},
	{Gender: model.GenderMale, SpecGram: 175, SpecLabel: "3.5两", UnitPriceMilli: 23625, SortNo: 5},
	{Gender: model.GenderMale, SpecGram: 200, SpecLabel: "4两", UnitPriceMilli: 34875, SortNo: 6},
	{Gender: model.GenderMale, SpecGram: 225, SpecLabel: "4.5两", UnitPriceMilli: 44875, SortNo: 7},
	{Gender: model.GenderMale, SpecGram: 250, SpecLabel: "5两", UnitPriceMilli: 54875, SortNo: 8},
}

// SeedSpecs 在 specs 表为空时插入种子数据。已有数据则原样跳过，不覆盖卖家改过的价格。
func (s *SQLiteStore) SeedSpecs(ctx context.Context, now int64) (int, error) {
	inserted := 0
	err := s.WithTx(ctx, func(q Queries) error {
		n, err := q.CountSpecs(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		for _, sp := range seedSpecs {
			sp := sp
			sp.Grade = model.GradeNormal
			sp.Enabled = true
			sp.UpdatedAt = now
			if err := q.InsertSpec(ctx, &sp); err != nil {
				return err
			}
			inserted++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("seed specs: %w", err)
	}
	return inserted, nil
}
