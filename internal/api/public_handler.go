package api

import (
	"net/http"
	"strings"
)

// PublicQueryOrder GET /api/public/orders?order_no=...&phone_tail=8000
//
// 买家免登录查单：强制「单号 + 手机号后 4 位」双因子匹配，
// 任一不对都统一返回 40400，不区分「单号不存在」和「手机号不对」，避免被枚举。
// 返回数据脱敏，不含金额与备注。
func (a *API) PublicQueryOrder(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	orderNo := strings.TrimSpace(q.Get("order_no"))
	phoneTail := strings.TrimSpace(q.Get("phone_tail"))

	o, err := a.orders.PublicQuery(r.Context(), orderNo, phoneTail)
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToPublicOrderDTO(o))
}
