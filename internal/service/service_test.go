package service

import (
	"context"
	"path/filepath"
	"testing"

	"crab-order/internal/store"
)

// newTestStore 在临时目录里开一个真实的 SQLite 库，用完自动清理。
func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newTestOrderService(t *testing.T) (*OrderService, *store.SQLiteStore) {
	t.Helper()
	st := newTestStore(t)
	return NewOrderService(st), st
}

// sampleInput 一笔常规订单：公 4.5 两 ×5（88 元/只）+ 母 3.5 两 ×5（68 元/只），
// 运费 20 元，优惠 10 元 → 货款 780 元，应收 790 元。
func sampleInput() CreateOrderInput {
	return CreateOrderInput{
		ReceiverName: "张三",
		Phone:        "13800138000",
		Address:      "江苏省苏州市工业园区xx路88号3栋201",
		WechatNick:   "老张",
		WechatRemark: "同学介绍",
		Items: []ItemInput{
			{Gender: "male", SpecGram: 225, SpecLabel: "4.5两", Unit: "piece", Quantity: 5, UnitPrice: 8800},
			{Gender: "female", SpecGram: 175, SpecLabel: "3.5两", Unit: "piece", Quantity: 5, UnitPrice: 6800},
		},
		FreightFee:     2000,
		Discount:       1000,
		ExpectShipDate: "2026-09-16",
		Remark:         "周五之前务必发出",
		Operator:       "oTest",
	}
}
