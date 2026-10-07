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

	// 残蟹要看得出来；改版前的混装老明细不加性别前缀
	mixed := &Order{Items: []OrderItem{
		{Gender: GenderFemale, SpecLabel: "3两", Grade: GradeBroken, Quantity: 2},
		{Gender: GenderMixed, SpecLabel: "8只装 母2.5两/公3.5两", Quantity: 8},
	}}
	if got, want := mixed.ItemsSummary(), "母3两(残)×2, 8只装 母2.5两/公3.5两×8"; got != want {
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

func TestFormatMilliYuan(t *testing.T) {
	cases := []struct {
		milli int64
		want  string
	}{
		{0, "0.00"},
		{23625, "23.625"},
		{35000, "35.00"},
		{33600, "33.60"},
		{1, "0.001"},
		{-23625, "-23.625"},
	}
	for _, c := range cases {
		if got := FormatMilliYuan(c.milli); got != c.want {
			t.Errorf("FormatMilliYuan(%d) = %q, want %q", c.milli, got, c.want)
		}
	}
}

// 按只计价、每行向上取整到元：8 只的整数倍正好落回去年的整盒价，散买的零头进位。
func TestLineAmount(t *testing.T) {
	cases := []struct {
		name  string
		qty   int
		milli int64
		want  int64 // 分
	}{
		{"8 只 2.5 两正好 189", 8, 23625, 18900},
		{"16 只 3 两正好 2 × 269", 16, 33625, 53800},
		{"24 只 3.5 两正好 3 × 359", 24, 44875, 107700},
		{"8 只 4 两正好 439", 8, 54875, 43900},
		{"5 只 2.5 两 118.125 进位到 119", 5, 23625, 11900},
		{"5 只 3 两 168.125 进位到 169", 5, 33625, 16900},
		{"整元不进位", 5, 35000, 17500},
		{"1 厘也进位到 1 元", 1, 1, 100},
		{"单价 0 就是 0", 8, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LineAmount(c.qty, c.milli); got != c.want {
				t.Errorf("LineAmount(%d, %d) = %d, want %d", c.qty, c.milli, got, c.want)
			}
		})
	}
}
