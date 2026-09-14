package model

import "testing"

func TestFormatYuan(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{50, "0.50"},
		{100, "1.00"},
		{8800, "88.00"},
		{88800, "888.00"},
		{79000, "790.00"},
		{-50, "-0.50"}, // 负数的坑：不能输出 "0.-50"
		{-1, "-0.01"},
		{-100, "-1.00"},
		{-39000, "-390.00"},
		{158000000, "1580000.00"},
		{9999999999, "99999999.99"},
	}
	for _, c := range cases {
		if got := FormatYuan(c.cents); got != c.want {
			t.Errorf("FormatYuan(%d) = %q, want %q", c.cents, got, c.want)
		}
	}
}

func TestItemsSummary(t *testing.T) {
	o := &Order{Items: []OrderItem{
		{Gender: GenderMale, SpecLabel: "4.5两", Quantity: 5},
		{Gender: GenderFemale, SpecLabel: "3.5两", Quantity: 5},
	}}
	if got, want := o.ItemsSummary(), "公4.5两×5, 母3.5两×5"; got != want {
		t.Errorf("ItemsSummary() = %q, want %q", got, want)
	}

	empty := &Order{}
	if got := empty.ItemsSummary(); got != "" {
		t.Errorf("空明细应返回空串，实际 %q", got)
	}
}

func TestUnpaidAmount(t *testing.T) {
	o := &Order{PayableAmount: 79000, PaidAmount: 40000}
	if got := o.UnpaidAmount(); got != 39000 {
		t.Errorf("UnpaidAmount() = %d, want 39000", got)
	}
	// 超付时未收为负，前端据此显示「多收 X 元」。
	over := &Order{PayableAmount: 79000, PaidAmount: 80000}
	if got := over.UnpaidAmount(); got != -1000 {
		t.Errorf("超付时 UnpaidAmount() = %d, want -1000", got)
	}
}
