package api

import (
	"net/http"
)

// Dashboard GET /api/stats/dashboard?start=&end=
func (a *API) Dashboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d, err := a.stats.Dashboard(r.Context(), q.Get("start"), q.Get("end"))
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, d)
}

// ShipPlan GET /api/stats/ship-plan?date=
// 某天约定发货、且仍待发货的订单——卖家每天早上最常用的那一屏。
func (a *API) ShipPlan(w http.ResponseWriter, r *http.Request) {
	date, list, err := a.stats.ShipPlan(r.Context(), r.URL.Query().Get("date"))
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, map[string]any{
		"date":  date,
		"list":  ToOrderSummaryList(list),
		"total": len(list),
	})
}
