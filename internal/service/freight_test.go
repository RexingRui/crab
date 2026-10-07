package service

import (
	"context"
	"errors"
	"testing"

	"crab-order/internal/errs"
	"crab-order/internal/model"
)

func ptr(v int64) *int64 { return &v }

// sixteenCrabs 16 只母 3 两（33.625 元/只），货款 538 元，建单时不填运费。
func sixteenCrabs() CreateOrderInput {
	in := sampleInput()
	in.Items = []ItemInput{{
		Gender: model.GenderFemale, SpecGram: 150, SpecLabel: "3两",
		Quantity: 16, UnitPriceMilli: 33625,
	}}
	in.FreightFee = 0
	in.Discount = 0
	return in
}

func mustCode(t *testing.T, err error, code int) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("期望错误码 %d，实际 %v", code, err)
	}
}

// TestFreightFlow 货款先付清 → 运费待定 → 发货填运费 → 买家待补 → 补齐。
func TestFreightFlow(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	o, _, err := svc.CreateOrder(ctx, sixteenCrabs())
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if o.FreightRuleVer != model.CurrentFreightRuleVer() {
		t.Fatalf("建单没记下规则版本: %q", o.FreightRuleVer)
	}
	o, _ = svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 53800})
	if o.PayStatus != model.PayPaid || !o.FreightPending() {
		t.Fatalf("货款付清后应是 paid 且运费待定: %s pending=%v", o.PayStatus, o.FreightPending())
	}

	// 发货时一起填：16 只、实付 60，按实付算 → 卖家补 40，买家补 20
	o, err = svc.Ship(ctx, ShipInput{
		ID: o.ID, ShipCompany: "顺丰", TrackingNo: "SF1",
		Freight: &FreightInput{FreightList: ptr(7500), FreightCost: ptr(6000)},
	})
	if err != nil {
		t.Fatalf("发货失败: %v", err)
	}
	if o.FreightFee != 2000 || o.FreightBasis != model.FreightBasisActual {
		t.Fatalf("买家应补 20（按实付）: fee=%d basis=%s", o.FreightFee, o.FreightBasis)
	}
	if o.FreightSellerPart() != 4000 {
		t.Fatalf("卖家承担应为 40，实际 %d", o.FreightSellerPart())
	}
	if o.PayableAmount != 55800 || o.PayStatus != model.PayPartial || o.UnpaidAmount() != 2000 {
		t.Fatalf("应收/状态不对: payable=%d status=%s unpaid=%d", o.PayableAmount, o.PayStatus, o.UnpaidAmount())
	}
	if o.FreightPending() {
		t.Fatal("填了运费就不该再是待定")
	}

	o, _ = svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 2000, Remark: "补运费"})
	if o.PayStatus != model.PayPaid {
		t.Fatalf("补齐后应为 paid，实际 %s", o.PayStatus)
	}
}

// TestFreightBasisAndOverride 口径按单选；卖家可以直接改买家承担的金额。
func TestFreightBasisAndOverride(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sixteenCrabs())

	// 按原价 80 算：80 - 40 = 40
	o, err := svc.SetFreight(ctx, SetFreightInput{ID: o.ID, FreightInput: FreightInput{
		FreightList: ptr(8000), FreightCost: ptr(6000), Basis: model.FreightBasisList,
	}})
	if err != nil {
		t.Fatalf("填运费失败: %v", err)
	}
	if o.FreightFee != 4000 || o.FreightSellerPart() != 2000 {
		t.Fatalf("按原价应买家补 40、卖家实际担 20: fee=%d seller=%d", o.FreightFee, o.FreightSellerPart())
	}

	// 熟客免补
	o, err = svc.SetFreight(ctx, SetFreightInput{ID: o.ID, FreightInput: FreightInput{
		FreightList: ptr(8000), FreightCost: ptr(6000), Basis: model.FreightBasisList, BuyerFee: ptr(0),
	}})
	if err != nil {
		t.Fatalf("改买家承担失败: %v", err)
	}
	if o.FreightFee != 0 || o.PayableAmount != 53800 {
		t.Fatalf("免补后 fee=%d payable=%d", o.FreightFee, o.PayableAmount)
	}

	_, err = svc.SetFreight(ctx, SetFreightInput{ID: o.ID, FreightInput: FreightInput{
		FreightCost: ptr(6000), Basis: model.FreightBasisList,
	}})
	mustCode(t, err, errs.CodeInvalidParam) // 按原价却没填原价

	// 买家补的超过快递实付是正常的：券是卖家花钱买的，用大额券寄的单实付很低，
	// 买家照常补运费（比如 16 只原价 58、实付 40，买家补了 60）
	o, err = svc.SetFreight(ctx, SetFreightInput{ID: o.ID, FreightInput: FreightInput{
		FreightList: ptr(5800), FreightCost: ptr(4000), BuyerFee: ptr(6000),
	}})
	if err != nil {
		t.Fatalf("买家补的超过实付应当允许: %v", err)
	}
	if o.FreightFee != 6000 || o.PayableAmount != 59800 || o.FreightSellerPart() != -2000 {
		t.Fatalf("fee=%d payable=%d seller=%d", o.FreightFee, o.PayableAmount, o.FreightSellerPart())
	}

	_, err = svc.SetFreight(ctx, SetFreightInput{ID: o.ID, FreightInput: FreightInput{
		FreightCost: ptr(6000), BuyerFee: ptr(-1),
	}})
	mustCode(t, err, errs.CodeInvalidParam) // 买家承担为负
}

// TestFreightRuleChangeOnlyAffectsNewOrders 规则换了新版本，老单仍按建单时的版本给建议值。
func TestFreightRuleChangeOnlyAffectsNewOrders(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	old, _, _ := svc.CreateOrder(ctx, sixteenCrabs())

	// 上线新规则：不管几只，卖家一律只补 10 元
	model.AddFreightRuleForTest(t, model.FreightRule{
		Version: "v-test", DefaultBasis: model.FreightBasisActual,
		Tiers: []model.FreightTier{{MaxCrabs: 0, SellerCap: 1000}},
	})
	in := sixteenCrabs()
	in.Phone = "13800138001"
	fresh, _, _ := svc.CreateOrder(ctx, in)

	freight := FreightInput{FreightCost: ptr(6000)}
	old, _ = svc.SetFreight(ctx, SetFreightInput{ID: old.ID, FreightInput: freight})
	fresh, _ = svc.SetFreight(ctx, SetFreightInput{ID: fresh.ID, FreightInput: freight})

	if old.FreightRuleVer != "v1" || old.FreightFee != 2000 {
		t.Fatalf("老单应仍按 v1：买家补 20，实际 ver=%s fee=%d", old.FreightRuleVer, old.FreightFee)
	}
	if fresh.FreightRuleVer != "v-test" || fresh.FreightFee != 5000 {
		t.Fatalf("新单应按新规则：买家补 50，实际 ver=%s fee=%d", fresh.FreightRuleVer, fresh.FreightFee)
	}
}

// TestSettleFreight 结算标记：没填运费的不能标，能撤销，筛选跟着变。
func TestSettleFreight(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	a, _, _ := svc.CreateOrder(ctx, sixteenCrabs())
	in := sixteenCrabs()
	in.Phone = "13800138002"
	b, _, _ := svc.CreateOrder(ctx, in)
	svc.SetFreight(ctx, SetFreightInput{ID: a.ID, FreightInput: FreightInput{FreightCost: ptr(6000)}})

	count := func(f string) int {
		_, n, err := svc.ListOrders(ctx, model.OrderFilter{Freight: f})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return n
	}
	if count(model.FreightFilterPending) != 1 || count(model.FreightFilterUnsettled) != 1 {
		t.Fatalf("待定/未结应各 1 单")
	}

	_, err := svc.SettleFreight(ctx, []int64{a.ID, b.ID}, true, "oTest")
	mustCode(t, err, errs.CodeStateConflict)
	if count(model.FreightFilterSettled) != 0 {
		t.Fatal("整批被拒，不该有任何一单标上已结")
	}

	n, err := svc.SettleFreight(ctx, []int64{a.ID}, true, "oTest")
	if err != nil || n != 1 {
		t.Fatalf("标记已结失败: n=%d err=%v", n, err)
	}
	if count(model.FreightFilterSettled) != 1 || count(model.FreightFilterUnsettled) != 0 {
		t.Fatal("标记后筛选不对")
	}
	if n, _ := svc.SettleFreight(ctx, []int64{a.ID}, true, "oTest"); n != 0 {
		t.Fatal("重复标记不该再算一次")
	}
	if n, _ := svc.SettleFreight(ctx, []int64{a.ID}, false, "oTest"); n != 1 || count(model.FreightFilterUnsettled) != 1 {
		t.Fatal("撤销失败")
	}
}
