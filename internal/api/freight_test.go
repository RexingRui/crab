package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"crab-order/internal/errs"
)

// TestFreightAPI 发货带运费 → 详情带建议所需字段 → 未结筛选 → 批量结清 → 看板汇总。
func TestFreightAPI(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	body := sampleOrderBody()
	body["freight_fee"] = 0
	body["discount"] = 0
	body["items"] = []map[string]any{{
		"gender": sp.Gender, "spec_gram": sp.SpecGram, "spec_label": sp.SpecLabel,
		"quantity": 16, "unit_price_milli": sp.UnitPriceMilli,
	}}
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", body))
	if !o.FreightPending || o.FreightSellerCap != 4000 || o.CrabCount != 16 || o.FreightDefaultBasis != "actual" {
		t.Fatalf("新单的运费字段不对: %+v", o.FreightDTO)
	}
	path := "/api/orders/" + itoa(o.ID)

	shipped := decodeOrder(t, e.mustOK(t, http.MethodPost, path+"/ship", map[string]any{
		"ship_company": "顺丰", "tracking_no": "SF100",
		"freight": map[string]any{"freight_list": 7500, "freight_cost": 6000},
	}))
	if shipped.FreightFee != 2000 || shipped.FreightSeller != 4000 || shipped.FreightPending {
		t.Fatalf("发货填运费后: fee=%d seller=%d pending=%v", shipped.FreightFee, shipped.FreightSeller, shipped.FreightPending)
	}

	// 重量复核后加价，改按原价算，并手动定买家补 30
	changed := decodeOrder(t, e.mustOK(t, http.MethodPut, path+"/freight", map[string]any{
		"freight_list": 9000, "freight_cost": 7000, "freight_basis": "list", "freight_fee": 3000,
	}))
	if changed.FreightFee != 3000 || changed.FreightBasis != "list" || changed.FreightSeller != 4000 {
		t.Fatalf("改运费后: %+v fee=%d", changed.FreightDTO, changed.FreightFee)
	}

	var page struct {
		List  []OrderSummaryDTO `json:"list"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(e.mustOK(t, http.MethodGet, "/api/orders?freight=unsettled", nil), &page); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if page.Total != 1 || page.List[0].FreightCost == nil || *page.List[0].FreightCost != 7000 {
		t.Fatalf("未结筛选不对: %+v", page)
	}
	if status, resp, _ := e.call(t, http.MethodGet, "/api/orders?freight=bogus", nil); resp.Code != errs.CodeInvalidParam {
		t.Fatalf("非法 freight 参数应 40001: %d/%d", status, resp.Code)
	}

	e.mustOK(t, http.MethodPost, "/api/orders/freight-settle", map[string]any{"ids": []int64{o.ID}, "settled": true})
	detail := decodeOrder(t, e.mustOK(t, http.MethodGet, path, nil))
	if detail.FreightSettledAt == nil {
		t.Fatal("标记已结后应有 freight_settled_at")
	}

	var d struct {
		Freight struct {
			CostTotal      int64 `json:"cost_total"`
			BuyerTotal     int64 `json:"buyer_total"`
			SellerTotal    int64 `json:"seller_total"`
			SavedTotal     int64 `json:"saved_total"`
			UnsettledCount int   `json:"unsettled_count"`
		} `json:"freight"`
	}
	if err := json.Unmarshal(e.mustOK(t, http.MethodGet, "/api/stats/dashboard", nil), &d); err != nil {
		t.Fatalf("解析看板失败: %v", err)
	}
	f := d.Freight
	if f.CostTotal != 7000 || f.BuyerTotal != 3000 || f.SellerTotal != 4000 || f.SavedTotal != 2000 || f.UnsettledCount != 0 {
		t.Fatalf("看板运费汇总不对: %+v", f)
	}
}

// TestFreightHiddenFromBuyer 买家查单和登记回执里不能出现原价、实付、卖家补贴、口径。
func TestFreightHiddenFromBuyer(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	_, _, reg := e.register(t, registerBody(e.regLink(t, ""), sp.ID, PackHint, "13900139050"))
	var r RegistrationDTO
	if err := json.Unmarshal(reg, &r); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+r.OrderNo, nil))
	e.mustOK(t, http.MethodPut, "/api/orders/"+itoa(o.ID)+"/freight", map[string]any{
		"freight_list": 3000, "freight_cost": 2500,
	})

	_, _, pub := e.callWithToken(t, http.MethodGet,
		"/api/public/orders?order_no="+r.OrderNo+"&phone_tail=9050", nil, "")
	if !strings.Contains(string(pub), r.OrderNo) {
		t.Fatalf("买家查单没查到这一单: %s", pub)
	}
	for name, raw := range map[string]json.RawMessage{"回执": reg, "查单": pub} {
		for _, key := range []string{"freight_list", "freight_cost", "freight_seller", "freight_basis", "freight_seller_cap"} {
			if strings.Contains(string(raw), `"`+key+`"`) {
				t.Errorf("买家看到的%s里出现了 %s: %s", name, key, raw)
			}
		}
	}
}
