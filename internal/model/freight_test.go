package model

import "testing"

// TestFreightRulesWellFormed 规则写死在代码里，改的时候容易手滑：
// 档位必须按只数递增、最后一档不设上限，默认口径合法，当前版本存在。
func TestFreightRulesWellFormed(t *testing.T) {
	if _, ok := freightRules[currentFreightRuleVer]; !ok {
		t.Fatalf("当前版本 %s 不在规则表里", currentFreightRuleVer)
	}
	for ver, r := range freightRules {
		if r.Version != ver {
			t.Errorf("%s: Version 字段写成了 %s", ver, r.Version)
		}
		if !r.DefaultBasis.Valid() {
			t.Errorf("%s: 默认口径 %q 非法", ver, r.DefaultBasis)
		}
		if len(r.Tiers) == 0 {
			t.Errorf("%s: 没有档位", ver)
			continue
		}
		prev := 0
		for i, tier := range r.Tiers {
			last := i == len(r.Tiers)-1
			if last != (tier.MaxCrabs == 0) {
				t.Errorf("%s: 只有最后一档能不设上限（第 %d 档 MaxCrabs=%d）", ver, i+1, tier.MaxCrabs)
			}
			if !last && tier.MaxCrabs <= prev {
				t.Errorf("%s: 第 %d 档只数没有递增", ver, i+1)
			}
			if tier.SellerCap < 0 {
				t.Errorf("%s: 第 %d 档补贴为负", ver, i+1)
			}
			prev = tier.MaxCrabs
		}
	}
}

func TestFreightSuggest(t *testing.T) {
	r := FreightRule{Tiers: []FreightTier{{15, 2000}, {23, 4000}, {0, 6000}}}
	cases := []struct {
		crabs int
		cost  int64
		want  int64
	}{
		{8, 1800, 0},     // 没超过 20，卖家全包
		{8, 2600, 600},   // 超出 6 元买家补
		{16, 6000, 2000}, // 16 只运费 60：卖家补 40，买家补 20
		{20, 4500, 500},  // 20 只落在第二档
		{40, 9000, 3000}, // 超过最后一个上限，按最后一档
		{0, 2500, 500},   // 全是按斤的，只数为 0，按第一档
	}
	for _, c := range cases {
		if got := r.SuggestBuyerFee(c.cost, c.crabs); got != c.want {
			t.Errorf("%d 只、运费 %d：买家补 %d，期望 %d", c.crabs, c.cost, got, c.want)
		}
	}
}

// TestFreightRuleV1Tiers v1 的分档边界：15 只以内补 20，16-23 只补 40，24 只及以上补 60。
func TestFreightRuleV1Tiers(t *testing.T) {
	r := FreightRuleOf("v1")
	for crabs, want := range map[int]int64{
		1: 2000, 8: 2000, 15: 2000,
		16: 4000, 17: 4000, 23: 4000,
		24: 6000, 40: 6000,
	} {
		if got := r.SellerCap(crabs); got != want {
			t.Errorf("%d 只：卖家最多补 %d，期望 %d", crabs, got, want)
		}
	}
}
