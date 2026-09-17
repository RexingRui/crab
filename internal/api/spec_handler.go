package api

import (
	"net/http"

	"crab-order/internal/model"
	"crab-order/internal/service"
)

type specReq struct {
	Gender    model.Gender `json:"gender"`
	SpecGram  int          `json:"spec_gram"`
	SpecLabel string       `json:"spec_label"`
	Unit      model.Unit   `json:"unit"`
	UnitPrice int64        `json:"unit_price"`
	PackSize  int          `json:"pack_size"`
	Enabled   *bool        `json:"enabled"`
	SortNo    int          `json:"sort_no"`
}

func (r specReq) toInput() service.SpecInput {
	return service.SpecInput{
		Gender:    r.Gender,
		SpecGram:  r.SpecGram,
		SpecLabel: r.SpecLabel,
		Unit:      r.Unit,
		UnitPrice: r.UnitPrice,
		PackSize:  r.PackSize,
		Enabled:   r.Enabled,
		SortNo:    r.SortNo,
	}
}

// ListSpecs GET /api/specs?all=1
// 默认只返回启用中的规格（小程序录单页下拉用），all=1 时连停用的一起返回（管理页用）。
func (a *API) ListSpecs(w http.ResponseWriter, r *http.Request) {
	onlyEnabled := r.URL.Query().Get("all") != "1"

	list, err := a.specs.List(r.Context(), onlyEnabled)
	if err != nil {
		Fail(w, r, err)
		return
	}
	out := make([]SpecDTO, 0, len(list))
	for _, s := range list {
		out = append(out, ToSpecDTO(s))
	}
	OK(w, map[string]any{"list": out, "total": len(out)})
}

// CreateSpec POST /api/specs
func (a *API) CreateSpec(w http.ResponseWriter, r *http.Request) {
	var req specReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	s, err := a.specs.Create(r.Context(), req.toInput())
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToSpecDTO(*s))
}

// UpdateSpec PUT /api/specs/{id}
// 改价只影响新订单：历史订单明细里的单价是快照。
func (a *API) UpdateSpec(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req specReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	s, err := a.specs.Update(r.Context(), id, req.toInput())
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToSpecDTO(*s))
}

// DisableSpec DELETE /api/specs/{id}
// 停用而非物理删除。
func (a *API) DisableSpec(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	if err := a.specs.Disable(r.Context(), id); err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, map[string]any{"id": id, "enabled": false})
}
