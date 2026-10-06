package model

// FreightBasis 买家要补的运费按哪个金额算：快递原价，还是用券后的实付。
// 每单由卖家自己选，不对买家公开。
type FreightBasis string

const (
	FreightBasisList   FreightBasis = "list"
	FreightBasisActual FreightBasis = "actual"
)

func (b FreightBasis) Valid() bool {
	return b == FreightBasisList || b == FreightBasisActual
}

func (b FreightBasis) Text() string {
	switch b {
	case FreightBasisList:
		return "按原价"
	case FreightBasisActual:
		return "按实付"
	}
	return string(b)
}

// FreightTier 补贴的一档：这单只数不超过 MaxCrabs 时，卖家最多补 SellerCap（分）。
// MaxCrabs 为 0 表示不设上限，只能放在最后一档。
type FreightTier struct {
	MaxCrabs  int
	SellerCap int64
}

// FreightRule 一版运费补贴规则。只给建议值，卖家每单都能改。
type FreightRule struct {
	Version      string
	DefaultBasis FreightBasis
	Tiers        []FreightTier
}

// freightRules 全部历史版本。订单建单时记下当时的版本号，之后一直按那一版算——
// 规则改了只影响之后的新单，老单即使再改运费也不会换规则。
//
// 改规则：在这里加一版新的（别改老版本），再把 currentFreightRuleVer 指过去。
var freightRules = map[string]FreightRule{
	"v1": {
		Version:      "v1",
		DefaultBasis: FreightBasisActual,
		Tiers: []FreightTier{
			{MaxCrabs: 8, SellerCap: 2000},
			{MaxCrabs: 16, SellerCap: 4000},
			{MaxCrabs: 0, SellerCap: 6000},
		},
	},
}

// currentFreightRuleVer 新建订单用的规则版本。
var currentFreightRuleVer = "v1"

// CurrentFreightRuleVer 新建订单用的规则版本。
func CurrentFreightRuleVer() string { return currentFreightRuleVer }

// FreightRuleOf 取某一版规则。版本号不认识（理论上不会出现）就退回当前版。
func FreightRuleOf(ver string) FreightRule {
	if r, ok := freightRules[ver]; ok {
		return r
	}
	return freightRules[currentFreightRuleVer]
}

// AddFreightRuleForTest 测试里模拟「上线了新一版规则」，测试结束自动还原。
func AddFreightRuleForTest(t interface{ Cleanup(func()) }, r FreightRule) {
	prev := currentFreightRuleVer
	freightRules[r.Version] = r
	currentFreightRuleVer = r.Version
	t.Cleanup(func() {
		delete(freightRules, r.Version)
		currentFreightRuleVer = prev
	})
}

// SellerCap 这么多只的一单，卖家最多补多少。
func (r FreightRule) SellerCap(crabs int) int64 {
	for _, t := range r.Tiers {
		if t.MaxCrabs == 0 || crabs <= t.MaxCrabs {
			return t.SellerCap
		}
	}
	if n := len(r.Tiers); n > 0 {
		return r.Tiers[n-1].SellerCap
	}
	return 0
}

// SuggestBuyerFee 买家建议补多少：口径金额超出卖家补贴的部分。
func (r FreightRule) SuggestBuyerFee(basisAmount int64, crabs int) int64 {
	if v := basisAmount - r.SellerCap(crabs); v > 0 {
		return v
	}
	return 0
}
