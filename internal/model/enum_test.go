package model

import "testing"

var allShipStatuses = []ShipStatus{ShipPending, ShipShipped, ShipReceived, ShipCancelled}

// TestCanTransitShip 覆盖 4×4 全部组合：只有方案 5.1 列出的四条正向流转合法。
func TestCanTransitShip(t *testing.T) {
	allowed := map[ShipStatus]map[ShipStatus]bool{
		ShipPending: {ShipShipped: true, ShipCancelled: true},
		ShipShipped: {ShipReceived: true, ShipCancelled: true},
	}

	for _, from := range allShipStatuses {
		for _, to := range allShipStatuses {
			want := allowed[from][to]
			if got := CanTransitShip(from, to); got != want {
				t.Errorf("CanTransitShip(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// TestCanRevertShip 覆盖 4×4 全部组合：只有三条回退路径合法。
func TestCanRevertShip(t *testing.T) {
	allowed := map[ShipStatus]map[ShipStatus]bool{
		ShipShipped:   {ShipPending: true},
		ShipReceived:  {ShipShipped: true},
		ShipCancelled: {ShipPending: true},
	}

	for _, from := range allShipStatuses {
		for _, to := range allShipStatuses {
			want := allowed[from][to]
			if got := CanRevertShip(from, to); got != want {
				t.Errorf("CanRevertShip(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// TestShipTransitionNoSelfLoop 原地不动也算非法流转。
func TestShipTransitionNoSelfLoop(t *testing.T) {
	for _, s := range allShipStatuses {
		if CanTransitShip(s, s) {
			t.Errorf("CanTransitShip(%s, %s) 不应允许", s, s)
		}
		if CanRevertShip(s, s) {
			t.Errorf("CanRevertShip(%s, %s) 不应允许", s, s)
		}
	}
}

func TestCalcPayStatus(t *testing.T) {
	cases := []struct {
		name    string
		paid    int64
		payable int64
		want    PayStatus
	}{
		{"未付款", 0, 79000, PayUnpaid},
		{"退款退到负数也算未付款", -100, 79000, PayUnpaid},
		{"定金", 40000, 79000, PayPartial},
		{"付清", 79000, 79000, PayPaid},
		{"超付", 80000, 79000, PayPaid},
		{"应收为 0 且未收钱", 0, 0, PayUnpaid},
		{"应收为 0 但收了钱", 100, 0, PayPaid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CalcPayStatus(c.paid, c.payable); got != c.want {
				t.Errorf("CalcPayStatus(%d, %d) = %s, want %s", c.paid, c.payable, got, c.want)
			}
		})
	}
}

func TestEnumValidAndText(t *testing.T) {
	if !ShipPending.Valid() || ShipStatus("unknown").Valid() {
		t.Error("ShipStatus.Valid 判断错误")
	}
	if !PayPartial.Valid() || PayStatus("unknown").Valid() {
		t.Error("PayStatus.Valid 判断错误")
	}
	if !GenderMale.Valid() || Gender("x").Valid() {
		t.Error("Gender.Valid 判断错误")
	}
	if !UnitPiece.Valid() || Unit("x").Valid() {
		t.Error("Unit.Valid 判断错误")
	}
	if !PayMethodWechat.Valid() || PayMethod("btc").Valid() {
		t.Error("PayMethod.Valid 判断错误")
	}
	if GenderMale.Text() != "公" || GenderFemale.Text() != "母" {
		t.Error("Gender.Text 翻译错误")
	}
	if UnitPiece.Text() != "只" || ShipShipped.Text() != "已发货" {
		t.Error("Text 翻译错误")
	}
}
