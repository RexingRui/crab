package service

import (
	"context"
	"errors"
	"testing"

	"crab-order/internal/errs"
	"crab-order/internal/model"
)

func errCode(err error) int {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}

// ========== 纯计算 ==========

func TestCalcAmounts(t *testing.T) {
	items := []model.OrderItem{
		{Quantity: 5, UnitPrice: 8800, Amount: CalcItemAmount(5, 8800)},
		{Quantity: 5, UnitPrice: 6800, Amount: CalcItemAmount(5, 6800)},
	}
	if items[0].Amount != 44000 {
		t.Errorf("明细金额 = %d, want 44000", items[0].Amount)
	}
	goods := CalcGoodsAmount(items)
	if goods != 78000 {
		t.Errorf("货款 = %d, want 78000", goods)
	}
	if got := CalcPayableAmount(goods, 2000, 1000); got != 79000 {
		t.Errorf("应收 = %d, want 79000", got)
	}
	// 优惠等于货款加运费时应收为 0，是合法的（送一单）
	if got := CalcPayableAmount(goods, 2000, 80000); got != 0 {
		t.Errorf("应收 = %d, want 0", got)
	}
}

func TestValidateMoneyDiscountExceeds(t *testing.T) {
	items := []model.OrderItem{{Quantity: 1, UnitPrice: 8800, Amount: 8800}}

	if _, _, err := validateMoney(items, 2000, 10801); errCode(err) != errs.CodeInvalidParam {
		t.Errorf("优惠超过货款+运费应返回 40001，实际 %v", err)
	}
	// 边界：正好等于，允许
	if _, payable, err := validateMoney(items, 2000, 10800); err != nil || payable != 0 {
		t.Errorf("优惠正好抵完应当允许，payable=%d err=%v", payable, err)
	}
	if _, _, err := validateMoney(items, -1, 0); errCode(err) != errs.CodeInvalidParam {
		t.Error("运费为负应返回 40001")
	}
	if _, _, err := validateMoney(items, 0, -1); errCode(err) != errs.CodeInvalidParam {
		t.Error("优惠为负应返回 40001")
	}
}

func TestCreateOrderValidation(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		mutit func(in *CreateOrderInput)
	}{
		{"收货人为空", func(in *CreateOrderInput) { in.ReceiverName = "" }},
		{"手机号格式错误", func(in *CreateOrderInput) { in.Phone = "12345" }},
		{"手机号首位非 1", func(in *CreateOrderInput) { in.Phone = "23800138000" }},
		{"地址过短", func(in *CreateOrderInput) { in.Address = "苏州" }},
		{"明细为空", func(in *CreateOrderInput) { in.Items = nil }},
		{"数量为 0", func(in *CreateOrderInput) { in.Items[0].Quantity = 0 }},
		{"单价为负", func(in *CreateOrderInput) { in.Items[0].UnitPrice = -1 }},
		{"规格克数为 0", func(in *CreateOrderInput) { in.Items[0].SpecGram = 0 }},
		{"性别非法", func(in *CreateOrderInput) { in.Items[0].Gender = "other" }},
		{"单位非法", func(in *CreateOrderInput) { in.Items[0].Unit = "ton" }},
		{"发货日格式错误", func(in *CreateOrderInput) { in.ExpectShipDate = "2026/09/16" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := sampleInput()
			c.mutit(&in)
			if _, _, err := svc.CreateOrder(ctx, in); errCode(err) != errs.CodeInvalidParam {
				t.Errorf("期望 40001，实际 %v", err)
			}
		})
	}
}

func TestCreateOrder(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	o, idempotent, err := svc.CreateOrder(ctx, sampleInput())
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if idempotent {
		t.Error("首次建单不应命中幂等")
	}
	if o.GoodsAmount != 78000 || o.PayableAmount != 79000 || o.PaidAmount != 0 {
		t.Errorf("金额不对: goods=%d payable=%d paid=%d", o.GoodsAmount, o.PayableAmount, o.PaidAmount)
	}
	if o.ShipStatus != model.ShipPending || o.PayStatus != model.PayUnpaid {
		t.Errorf("初始状态不对: ship=%s pay=%s", o.ShipStatus, o.PayStatus)
	}
	if o.OrderNo == "" {
		t.Error("订单号为空")
	}
	if len(o.Items) != 2 {
		t.Fatalf("明细数 = %d, want 2", len(o.Items))
	}
	if len(o.Logs) != 1 || o.Logs[0].Action != model.ActionCreate {
		t.Errorf("应写入一条 create 日志，实际 %+v", o.Logs)
	}
}

// TestCreateOrderIdempotent 同一 request_id 连发两次只产生一笔订单。
func TestCreateOrderIdempotent(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	in := sampleInput()
	in.RequestID = "mp-1726300000-abc123"

	first, idem1, err := svc.CreateOrder(ctx, in)
	if err != nil || idem1 {
		t.Fatalf("首次建单异常: err=%v idempotent=%v", err, idem1)
	}
	second, idem2, err := svc.CreateOrder(ctx, in)
	if err != nil {
		t.Fatalf("重试建单失败: %v", err)
	}
	if !idem2 {
		t.Error("重试应命中幂等")
	}
	if first.ID != second.ID || first.OrderNo != second.OrderNo {
		t.Errorf("幂等应返回同一笔订单，%d/%s vs %d/%s",
			first.ID, first.OrderNo, second.ID, second.OrderNo)
	}

	_, total, err := svc.ListOrders(ctx, model.OrderFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 1 {
		t.Errorf("库里应只有 1 笔订单，实际 %d", total)
	}
}

// ========== 收款 ==========

// TestPaymentPartialToPaidAndBack 定金 → 尾款 → 删除一笔 → 退回 partial。
func TestPaymentPartialToPaidAndBack(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	o, _, err := svc.CreateOrder(ctx, sampleInput())
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	// 定金 400 元
	o, err = svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 40000, Remark: "定金"})
	if err != nil {
		t.Fatalf("记定金失败: %v", err)
	}
	if o.PayStatus != model.PayPartial {
		t.Errorf("收定金后应为 partial，实际 %s", o.PayStatus)
	}
	if o.PaidAmount != 40000 || o.UnpaidAmount() != 39000 {
		t.Errorf("金额不对: paid=%d unpaid=%d", o.PaidAmount, o.UnpaidAmount())
	}
	if o.FirstPayTime == nil {
		t.Error("首次收款应写入 first_pay_time")
	}
	if o.SettledTime != nil {
		t.Error("未付清不应有 settled_time")
	}

	// 尾款 390 元
	o, err = svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 39000, Remark: "尾款"})
	if err != nil {
		t.Fatalf("记尾款失败: %v", err)
	}
	if o.PayStatus != model.PayPaid || o.PaidAmount != 79000 {
		t.Errorf("付清后应为 paid，实际 %s paid=%d", o.PayStatus, o.PaidAmount)
	}
	if o.SettledTime == nil {
		t.Error("付清应写入 settled_time")
	}

	// 删掉尾款这笔（记错了）
	lastPayment := o.Payments[len(o.Payments)-1]
	o, err = svc.DeletePayment(ctx, lastPayment.ID, "oTest")
	if err != nil {
		t.Fatalf("删除收款失败: %v", err)
	}
	if o.PayStatus != model.PayPartial || o.PaidAmount != 40000 {
		t.Errorf("删除尾款后应退回 partial，实际 %s paid=%d", o.PayStatus, o.PaidAmount)
	}
	if o.SettledTime != nil {
		t.Error("退回 partial 后 settled_time 应清空")
	}
	if o.FirstPayTime == nil {
		t.Error("还留着定金，first_pay_time 不应清空")
	}
	if len(o.Payments) != 1 {
		t.Errorf("软删后应只剩 1 条流水，实际 %d", len(o.Payments))
	}
}

// TestRefundRevertsToUnpaid 全额退款后退回 unpaid，两个时间戳都清空。
func TestRefundRevertsToUnpaid(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	o, _, err := svc.CreateOrder(ctx, sampleInput())
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	o, err = svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 79000})
	if err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if o.PayStatus != model.PayPaid {
		t.Fatalf("应为 paid，实际 %s", o.PayStatus)
	}

	o, err = svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: -79000, Remark: "全额退款"})
	if err != nil {
		t.Fatalf("退款失败: %v", err)
	}
	if o.PayStatus != model.PayUnpaid || o.PaidAmount != 0 {
		t.Errorf("全额退款后应为 unpaid，实际 %s paid=%d", o.PayStatus, o.PaidAmount)
	}
	if o.FirstPayTime != nil || o.SettledTime != nil {
		t.Error("退回 unpaid 后 first_pay_time 与 settled_time 都应清空")
	}
	if o.Logs[len(o.Logs)-1].Action != model.ActionRefund {
		t.Errorf("退款应记 refund 日志，实际 %s", o.Logs[len(o.Logs)-1].Action)
	}
}

// TestOverpay 允许超付，未收金额为负。
func TestOverpay(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	o, _, _ := svc.CreateOrder(ctx, sampleInput())
	o, err := svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 80000, Remark: "抹零抹反了"})
	if err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if o.PayStatus != model.PayPaid {
		t.Errorf("超付应为 paid，实际 %s", o.PayStatus)
	}
	if got := o.UnpaidAmount(); got != -1000 {
		t.Errorf("超付未收金额 = %d, want -1000", got)
	}
}

func TestAddPaymentValidation(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	if _, err := svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 0}); errCode(err) != errs.CodeInvalidParam {
		t.Errorf("金额为 0 应返回 40001，实际 %v", err)
	}
	if _, err := svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 100, PayMethod: "btc"}); errCode(err) != errs.CodeInvalidParam {
		t.Errorf("非法收款方式应返回 40001，实际 %v", err)
	}
	if _, err := svc.AddPayment(ctx, AddPaymentInput{OrderID: 99999, Amount: 100}); errCode(err) != errs.CodeNotFound {
		t.Errorf("订单不存在应返回 40400，实际 %v", err)
	}
}

// ========== 改单 ==========

// TestUpdateOrderRecalcPayStatus 改单抬高应收后，原本 paid 的订单要退回 partial。
func TestUpdateOrderRecalcPayStatus(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	o, _, _ := svc.CreateOrder(ctx, sampleInput())
	o, err := svc.AddPayment(ctx, AddPaymentInput{OrderID: o.ID, Amount: 79000})
	if err != nil {
		t.Fatalf("收款失败: %v", err)
	}
	if o.PayStatus != model.PayPaid {
		t.Fatalf("应为 paid，实际 %s", o.PayStatus)
	}

	in := sampleInput()
	in.Items[0].Quantity = 10 // 多买 5 只公蟹，应收变成 1230 元
	updated, err := svc.UpdateOrder(ctx, UpdateOrderInput{
		ID:             o.ID,
		ReceiverName:   in.ReceiverName,
		Phone:          in.Phone,
		Address:        in.Address,
		WechatNick:     in.WechatNick,
		WechatRemark:   in.WechatRemark,
		Items:          in.Items,
		FreightFee:     in.FreightFee,
		Discount:       in.Discount,
		ExpectShipDate: in.ExpectShipDate,
		Remark:         in.Remark,
		Operator:       "oTest",
	})
	if err != nil {
		t.Fatalf("改单失败: %v", err)
	}
	if updated.GoodsAmount != 122000 || updated.PayableAmount != 123000 {
		t.Errorf("改单后金额不对: goods=%d payable=%d", updated.GoodsAmount, updated.PayableAmount)
	}
	if updated.PayStatus != model.PayPartial {
		t.Errorf("应收变大后应退回 partial，实际 %s", updated.PayStatus)
	}
	if updated.SettledTime != nil {
		t.Error("退回 partial 后 settled_time 应清空")
	}
	if updated.PaidAmount != 79000 {
		t.Errorf("实收不应被改单影响，实际 %d", updated.PaidAmount)
	}

	// 改单要留下逐字段的变更日志
	var hasItemsLog, hasPayableLog bool
	for _, l := range updated.Logs {
		if l.Action != model.ActionUpdate {
			continue
		}
		switch l.Field {
		case "items":
			hasItemsLog = true
		case "payable_amount":
			hasPayableLog = true
		}
	}
	if !hasItemsLog || !hasPayableLog {
		t.Errorf("改单应记录 items 与 payable_amount 的变更日志，实际 %+v", updated.Logs)
	}
}

// TestUpdateOrderOptimisticLock 带上过期的 updated_at 会被拒绝。
func TestUpdateOrderOptimisticLock(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	in := sampleInput()
	base := UpdateOrderInput{
		ID: o.ID, ReceiverName: in.ReceiverName, Phone: in.Phone, Address: in.Address,
		Items: in.Items, FreightFee: in.FreightFee, Discount: in.Discount,
		ExpectedUpdatedAt: o.UpdatedAt - 1, // 冒充旧版本
	}
	if _, err := svc.UpdateOrder(ctx, base); errCode(err) != errs.CodeVersionConflict {
		t.Errorf("版本不一致应返回 40903，实际 %v", err)
	}
}

// ========== 状态流转 ==========

func TestShipReceiveFlow(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	// 未发货直接确认收货 → 40902
	if _, err := svc.Receive(ctx, ReceiveInput{ID: o.ID}); errCode(err) != errs.CodeStateConflict {
		t.Errorf("pending 直接收货应返回 40902，实际 %v", err)
	}

	// 发货缺快递信息 → 40001
	if _, err := svc.Ship(ctx, ShipInput{ID: o.ID, TrackingNo: "SF1"}); errCode(err) != errs.CodeInvalidParam {
		t.Errorf("缺 ship_company 应返回 40001，实际 %v", err)
	}

	o, err := svc.Ship(ctx, ShipInput{ID: o.ID, ShipCompany: "顺丰速运", TrackingNo: "SF1234567890"})
	if err != nil {
		t.Fatalf("发货失败: %v", err)
	}
	if o.ShipStatus != model.ShipShipped || o.ShipTime == nil {
		t.Errorf("发货后状态/时间不对: %s %v", o.ShipStatus, o.ShipTime)
	}

	// 重复发货 → 40902
	if _, err := svc.Ship(ctx, ShipInput{ID: o.ID, ShipCompany: "顺丰速运", TrackingNo: "SF1"}); errCode(err) != errs.CodeStateConflict {
		t.Errorf("重复发货应返回 40902，实际 %v", err)
	}

	o, err = svc.Receive(ctx, ReceiveInput{ID: o.ID})
	if err != nil {
		t.Fatalf("确认收货失败: %v", err)
	}
	if o.ShipStatus != model.ShipReceived || o.ReceiveTime == nil {
		t.Errorf("收货后状态/时间不对: %s %v", o.ShipStatus, o.ReceiveTime)
	}

	// 已收货不能再取消
	if _, err := svc.Cancel(ctx, CancelInput{ID: o.ID, Reason: "试试"}); errCode(err) != errs.CodeStateConflict {
		t.Errorf("received 取消应返回 40902，实际 %v", err)
	}
}

// TestShipDoesNotRequirePayment 发货与收款两条线互不约束：未付款也能发货。
func TestShipDoesNotRequirePayment(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	o, err := svc.Ship(ctx, ShipInput{ID: o.ID, ShipCompany: "顺丰速运", TrackingNo: "SF1"})
	if err != nil {
		t.Fatalf("未付款发货应当允许，实际失败: %v", err)
	}
	if o.PayStatus != model.PayUnpaid {
		t.Errorf("发货不应改变收款状态，实际 %s", o.PayStatus)
	}
}

func TestRevertShip(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	o, _ = svc.Ship(ctx, ShipInput{ID: o.ID, ShipCompany: "顺丰速运", TrackingNo: "SF1234567890"})
	o, _ = svc.Receive(ctx, ReceiveInput{ID: o.ID})

	// reason 必填
	if _, err := svc.RevertShip(ctx, RevertShipInput{ID: o.ID, To: model.ShipShipped}); errCode(err) != errs.CodeInvalidParam {
		t.Errorf("缺 reason 应返回 40001，实际 %v", err)
	}
	// received 不能直接回退到 pending
	if _, err := svc.RevertShip(ctx, RevertShipInput{ID: o.ID, To: model.ShipPending, Reason: "点错了"}); errCode(err) != errs.CodeStateConflict {
		t.Errorf("received → pending 应返回 40902，实际 %v", err)
	}

	o, err := svc.RevertShip(ctx, RevertShipInput{ID: o.ID, To: model.ShipShipped, Reason: "点错了"})
	if err != nil {
		t.Fatalf("回退失败: %v", err)
	}
	if o.ShipStatus != model.ShipShipped || o.ReceiveTime != nil {
		t.Errorf("回退到 shipped 应清空收货时间，实际 %s %v", o.ShipStatus, o.ReceiveTime)
	}

	o, err = svc.RevertShip(ctx, RevertShipInput{ID: o.ID, To: model.ShipPending, Reason: "快递没揽收"})
	if err != nil {
		t.Fatalf("回退失败: %v", err)
	}
	if o.ShipStatus != model.ShipPending || o.ShipTime != nil || o.ShipCompany != "" || o.TrackingNo != "" {
		t.Errorf("回退到 pending 应清空发货信息，实际 %+v", o)
	}
	if o.Logs[len(o.Logs)-1].Action != model.ActionRevert {
		t.Error("回退应记 revert 日志")
	}
}

func TestCancelAndRevert(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	o, err := svc.Cancel(ctx, CancelInput{ID: o.ID, Reason: "买家临时不要了"})
	if err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if o.ShipStatus != model.ShipCancelled {
		t.Errorf("应为 cancelled，实际 %s", o.ShipStatus)
	}

	o, err = svc.RevertShip(ctx, RevertShipInput{ID: o.ID, To: model.ShipPending, Reason: "买家又要了"})
	if err != nil {
		t.Fatalf("撤销取消失败: %v", err)
	}
	if o.ShipStatus != model.ShipPending {
		t.Errorf("应回到 pending，实际 %s", o.ShipStatus)
	}
}

// ========== 软删除与查询 ==========

func TestDeleteOrder(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	if err := svc.DeleteOrder(ctx, o.ID, "oTest"); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	if _, err := svc.GetOrder(ctx, o.ID); errCode(err) != errs.CodeNotFound {
		t.Errorf("删除后详情应返回 40400，实际 %v", err)
	}
	_, total, _ := svc.ListOrders(ctx, model.OrderFilter{Page: 1, PageSize: 20})
	if total != 0 {
		t.Errorf("删除后列表应为空，实际 %d", total)
	}
	if err := svc.DeleteOrder(ctx, o.ID, "oTest"); errCode(err) != errs.CodeNotFound {
		t.Errorf("重复删除应返回 40400，实际 %v", err)
	}
}

func TestListOrdersFilter(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()

	a, _, _ := svc.CreateOrder(ctx, sampleInput())

	in := sampleInput()
	in.ReceiverName = "李四"
	in.Phone = "13900139000"
	in.WechatRemark = "邻居"
	in.ExpectShipDate = "2026-09-20"
	b, _, _ := svc.CreateOrder(ctx, in)

	if _, err := svc.Ship(ctx, ShipInput{ID: a.ID, ShipCompany: "顺丰速运", TrackingNo: "SF999"}); err != nil {
		t.Fatalf("发货失败: %v", err)
	}

	cases := []struct {
		name  string
		f     model.OrderFilter
		want  int
		first int64
	}{
		{"按发货状态", model.OrderFilter{ShipStatus: []model.ShipStatus{model.ShipPending}}, 1, b.ID},
		{"按关键字匹配收货人", model.OrderFilter{Keyword: "李四"}, 1, b.ID},
		{"按关键字匹配运单号", model.OrderFilter{Keyword: "SF999"}, 1, a.ID},
		{"按关键字匹配微信备注", model.OrderFilter{Keyword: "邻居"}, 1, b.ID},
		{"按约定发货日", model.OrderFilter{ExpectShipDate: "2026-09-20"}, 1, b.ID},
		{"按发货日区间", model.OrderFilter{ExpectShipDateStart: "2026-09-01", ExpectShipDateEnd: "2026-09-30"}, 2, 0},
		{"按收款状态", model.OrderFilter{PayStatus: []model.PayStatus{model.PayUnpaid}}, 2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.f.Page, c.f.PageSize = 1, 20
			list, total, err := svc.ListOrders(ctx, c.f)
			if err != nil {
				t.Fatalf("列表失败: %v", err)
			}
			if total != c.want {
				t.Fatalf("命中 %d 条，期望 %d", total, c.want)
			}
			if c.first != 0 && list[0].ID != c.first {
				t.Errorf("首条 ID = %d, want %d", list[0].ID, c.first)
			}
			if len(list) > 0 && list[0].ItemsSummary() == "" {
				t.Error("列表项应带 items_summary")
			}
		})
	}
}

func TestListOrdersPaging(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, _, err := svc.CreateOrder(ctx, sampleInput()); err != nil {
			t.Fatalf("建单失败: %v", err)
		}
	}

	list, total, err := svc.ListOrders(ctx, model.OrderFilter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 5 || len(list) != 2 {
		t.Errorf("total=%d len=%d, want 5/2", total, len(list))
	}
}

// TestGetOrderByNo 用单号查详情。
func TestGetOrderByNo(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	got, err := svc.GetOrderByNo(ctx, o.OrderNo)
	if err != nil {
		t.Fatalf("按单号查询失败: %v", err)
	}
	if got.ID != o.ID {
		t.Errorf("ID = %d, want %d", got.ID, o.ID)
	}
	if _, err := svc.GetOrderByNo(ctx, "20990101-001"); errCode(err) != errs.CodeNotFound {
		t.Errorf("不存在的单号应返回 40400，实际 %v", err)
	}
}

// ========== 买家查单 ==========

func TestPublicQuery(t *testing.T) {
	svc, _ := newTestOrderService(t)
	ctx := context.Background()
	o, _, _ := svc.CreateOrder(ctx, sampleInput())

	got, err := svc.PublicQuery(ctx, o.OrderNo, "8000")
	if err != nil {
		t.Fatalf("买家查单失败: %v", err)
	}
	if got.ID != o.ID || len(got.Items) != 2 {
		t.Errorf("返回的订单不对: %+v", got)
	}

	for _, c := range []struct{ no, tail string }{
		{o.OrderNo, "0000"},      // 尾号错
		{"20990101-001", "8000"}, // 单号不存在
		{o.OrderNo, "800"},       // 尾号位数不对
		{"", "8000"},
	} {
		if _, err := svc.PublicQuery(ctx, c.no, c.tail); errCode(err) != errs.CodeNotFound {
			t.Errorf("PublicQuery(%q,%q) 应返回 40400，实际 %v", c.no, c.tail, err)
		}
	}
}
