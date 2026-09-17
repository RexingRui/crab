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
var addedColumns = []struct{ table, column, ddl string }{
	{"orders", "source", "ALTER TABLE orders ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'"},
	{"specs", "pack_size", "ALTER TABLE specs ADD COLUMN pack_size INTEGER NOT NULL DEFAULT 0"},
}

// Migrate 建表（全部语句都是 IF NOT EXISTS，可重复执行），再补增量列。
func (s *SQLiteStore) Migrate(ctx context.Context) error {
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
	}
	return nil
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

// seedSpecs 是首次启动时写入的当季价目表。
//
// 价格按**套餐**走：一盒 8 只，默认 4 公 4 母，公母比例由买家在登记页自己调，
// 总数固定 8 只，价格不随比例变。买家可以混着买不同档（自由搭配），
// 所以每一档就是独立的一行，不需要「组合」这种结构。
//
// 只在 specs 表为空时插入。已经跑起来的库不会被覆盖——换价目表要去小程序「设置」页，
// 把旧的档停用、把新的档加上。
var seedSpecs = []model.Spec{
	{SpecLabel: "8只装 母2.5两/公3.5两", SpecGram: 1200, UnitPrice: 18900, PackSize: 8, SortNo: 1},
	{SpecLabel: "8只装 母3.0两/公4.0两", SpecGram: 1400, UnitPrice: 26900, PackSize: 8, SortNo: 2},
	{SpecLabel: "8只装 母3.5两/公4.5两", SpecGram: 1600, UnitPrice: 35900, PackSize: 8, SortNo: 3},
	{SpecLabel: "8只装 母4.0两/公5.0两", SpecGram: 1800, UnitPrice: 43900, PackSize: 8, SortNo: 4},
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
			// 套餐按盒卖、一盒里公母都有，所以是 mixed + box。
			sp.Gender = model.GenderMixed
			sp.Unit = model.UnitBox
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
