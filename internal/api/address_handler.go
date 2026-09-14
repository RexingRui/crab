package api

import (
	"net/http"
	"strconv"
	"strings"
)

// ListAddresses GET /api/addresses?keyword=&limit=20
// 地址簿从历史订单聚合，不单独建客户表。
func (a *API) ListAddresses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	list, err := a.addresses.List(r.Context(), strings.TrimSpace(q.Get("keyword")), limit)
	if err != nil {
		Fail(w, r, err)
		return
	}

	out := make([]AddressDTO, 0, len(list))
	for _, item := range list {
		out = append(out, ToAddressDTO(item))
	}
	OK(w, map[string]any{"list": out, "total": len(out)})
}
