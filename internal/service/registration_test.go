package service

import (
	"regexp"
	"strconv"
	"testing"

	"crab-order/internal/model"
)

func pack() model.Spec {
	return model.Spec{
		Gender: model.GenderMixed, SpecGram: 1200, SpecLabel: "8只装 母2.5两/公3.5两",
		Unit: model.UnitBox, UnitPrice: 18900, PackSize: 8, Enabled: true,
	}
}

// TestLoosePrice 散买价 = 整盒价摊到只，向上取整到元。
func TestLoosePrice(t *testing.T) {
	cases := []struct{ box, want int64 }{
		{18900, 2400}, // 189/8 = 23.625 → 24
		{26900, 3400}, // 269/8 = 33.625 → 34
		{35900, 4500}, // 359/8 = 44.875 → 45
		{43900, 5500}, // 439/8 = 54.875 → 55
		{16000, 2000}, // 160/8 = 20 整除，不该多跳一元
	}
	for _, c := range cases {
		sp := pack()
		sp.UnitPrice = c.box
		if got := LoosePrice(sp); got != c.want {
			t.Errorf("整盒 %d 分 → 散买 %d 分，期望 %d", c.box, got, c.want)
		}
	}
	// 不是套餐的档没有散买这回事
	piece := model.Spec{Unit: model.UnitPiece, UnitPrice: 8800}
	if got := LoosePrice(piece); got != 8800 {
		t.Errorf("按只档的散买价该是它自己的单价，实际 %d", got)
	}
}

// TestSplitPack 整盒 + 零头的拆法与定价。
func TestSplitPack(t *testing.T) {
	sp := pack()
	loose := LoosePrice(sp) // 2400

	cases := []struct {
		name     string
		n        int
		wantRows int
		wantSum  int64
	}{
		{"整一盒", 8, 1, 18900},
		{"两盒", 16, 1, 37800},
		{"只散买 5 只", 5, 1, 5 * loose},
		{"一盒零 5 只", 13, 2, 18900 + 5*loose},
		{"两盒零 7 只", 23, 2, 37800 + 7*loose},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := splitPack(sp, c.n, nil, 0)
			if err != nil {
				t.Fatalf("拆分失败: %v", err)
			}
			if len(rows) != c.wantRows {
				t.Fatalf("该拆成 %d 条，实际 %d 条: %+v", c.wantRows, len(rows), rows)
			}
			var sum int64
			crabs := 0
			for _, r := range rows {
				sum += CalcItemAmount(r.Quantity, r.UnitPrice)
				if r.Unit == model.UnitBox {
					crabs += r.Quantity * r.PackSize
				} else {
					crabs += r.Quantity
				}
			}
			if sum != c.wantSum {
				t.Errorf("金额 %d，期望 %d", sum, c.wantSum)
			}
			if crabs != c.n {
				t.Errorf("拆完只数对不上：%d，期望 %d", crabs, c.n)
			}
		})
	}
}

// TestSplitPackRejectsShortRemainder 零头不足起订量要拒，并给出最近的两个合法只数。
func TestSplitPackRejectsShortRemainder(t *testing.T) {
	sp := pack()
	for _, n := range []int{1, 4, 9, 12, 20} {
		if _, err := splitPack(sp, n, nil, 0); err == nil {
			t.Errorf("%d 只的零头不足 %d，该被拒", n, RegMinCrabs)
		}
	}
	// 整盒的与零头够的都要放行
	for _, n := range []int{5, 8, 13, 16, 21} {
		if _, err := splitPack(sp, n, nil, 0); err != nil {
			t.Errorf("%d 只该放行: %v", n, err)
		}
	}
}

// TestSplitMaleAddsUp 公母分摊到两条之后，总数要和买家填的一致。
func TestSplitMaleAddsUp(t *testing.T) {
	sp := pack()
	for n := 5; n <= 40; n++ {
		if l := n % sp.PackSize; l > 0 && l < RegMinCrabs {
			continue
		}
		for male := 0; male <= n; male++ {
			m := male
			rows, err := splitPack(sp, n, &m, 0)
			if err != nil {
				t.Fatalf("n=%d male=%d 拆分失败: %v", n, male, err)
			}
			gotMale, gotTotal := 0, 0
			for _, r := range rows {
				a, b := parseRatio(t, r.SpecLabel)
				gotMale += a
				gotTotal += a + b
			}
			if gotMale != male || gotTotal != n {
				t.Fatalf("n=%d male=%d → 分摊成 公%d 共%d", n, male, gotMale, gotTotal)
			}
		}
	}
}

// ratioRe 抓明细快照末尾的「（公X母Y）」。
var ratioRe = regexp.MustCompile(`（公(\d+)母(\d+)）$`)

func parseRatio(t *testing.T, label string) (int, int) {
	t.Helper()
	m := ratioRe.FindStringSubmatch(label)
	if m == nil {
		t.Fatalf("快照里没写公母比例: %q", label)
	}
	male, _ := strconv.Atoi(m[1])
	female, _ := strconv.Atoi(m[2])
	return male, female
}
