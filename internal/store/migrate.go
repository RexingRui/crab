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

// seedSpecs 是首次启动时写入的当季参考价，仅在 specs 表为空时插入。
var seedSpecs = []model.Spec{
	{Gender: model.GenderMale, SpecLabel: "2.5两", SpecGram: 125, UnitPrice: 3800, SortNo: 1},
	{Gender: model.GenderMale, SpecLabel: "3.5两", SpecGram: 175, UnitPrice: 5800, SortNo: 2},
	{Gender: model.GenderMale, SpecLabel: "4.5两", SpecGram: 225, UnitPrice: 8800, SortNo: 3},
	{Gender: model.GenderMale, SpecLabel: "5.5两", SpecGram: 275, UnitPrice: 13800, SortNo: 4},
	{Gender: model.GenderFemale, SpecLabel: "2.0两", SpecGram: 100, UnitPrice: 3200, SortNo: 5},
	{Gender: model.GenderFemale, SpecLabel: "2.5两", SpecGram: 125, UnitPrice: 4200, SortNo: 6},
	{Gender: model.GenderFemale, SpecLabel: "3.0两", SpecGram: 150, UnitPrice: 5800, SortNo: 7},
	{Gender: model.GenderFemale, SpecLabel: "3.5两", SpecGram: 175, UnitPrice: 7800, SortNo: 8},
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
			sp.Unit = model.UnitPiece
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
