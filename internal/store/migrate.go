package store

import (
	"context"
	_ "embed"
	"fmt"

	"crab-order/internal/model"
)

//go:embed schema.sql
var schemaSQL string

// Migrate 建表（全部语句都是 IF NOT EXISTS，可重复执行）。
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
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
