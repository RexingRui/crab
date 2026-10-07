package service

import (
	"context"
	"testing"

	"crab-order/internal/model"
)

// TestCheckMinCrabs 起订量卡整单，不卡每档。
func TestCheckMinCrabs(t *testing.T) {
	// 一档 1 只 + 另一档 4 只 = 5 只，该放行——逐档卡就会把这种正常搭配挡住
	five := []ItemInput{{Quantity: 1}, {Quantity: 4}}
	if err := checkMinCrabs(five); err != nil {
		t.Fatalf("凑够 5 只该放行: %v", err)
	}
	// 一共 4 只 → 拒
	if err := checkMinCrabs([]ItemInput{{Quantity: 4}}); err == nil {
		t.Fatal("一共 4 只该被拒")
	}
}

// TestResolveRegItems 买家选的规格按价目表当前值落成明细：一档一行、按只计价，
// 单价、性别、克重、品相都取自价目表。
func TestResolveRegItems(t *testing.T) {
	svc, st := newTestOrderService(t)
	ctx := context.Background()

	sp := model.Spec{Gender: model.GenderFemale, SpecGram: 150, Grade: model.GradeBroken,
		SpecLabel: "3两", UnitPriceMilli: 20000, Enabled: true, UpdatedAt: 1}
	if err := st.InsertSpec(ctx, &sp); err != nil {
		t.Fatalf("insert spec: %v", err)
	}
	off := model.Spec{Gender: model.GenderFemale, SpecGram: 200, SpecLabel: "4两",
		Grade: model.GradeNormal, UnitPriceMilli: 54875, Enabled: false, UpdatedAt: 1}
	if err := st.InsertSpec(ctx, &off); err != nil {
		t.Fatalf("insert spec: %v", err)
	}

	items, err := svc.resolveRegItems(ctx, []RegistrationItemInput{{SpecID: sp.ID, Quantity: 5}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("一档该是一行，实际 %d 行", len(items))
	}
	got := items[0]
	if got.Gender != model.GenderFemale || got.SpecGram != 150 || got.Grade != model.GradeBroken ||
		got.Quantity != 5 || got.UnitPriceMilli != 20000 {
		t.Fatalf("明细没按价目表填: %+v", got)
	}

	if _, err := svc.resolveRegItems(ctx, []RegistrationItemInput{{SpecID: off.ID, Quantity: 5}}); err == nil {
		t.Fatal("停用的规格不该能选")
	}
	if _, err := svc.resolveRegItems(ctx, []RegistrationItemInput{{SpecID: sp.ID, Quantity: 0}}); err == nil {
		t.Fatal("0 只不该能选")
	}
}
