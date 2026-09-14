package api

import (
	"net/http"

	"crab-order/internal/model"
	"crab-order/internal/service"
)

type addPaymentReq struct {
	Amount    int64           `json:"amount"`
	PayMethod model.PayMethod `json:"pay_method"`
	PaidAt    flexTime        `json:"paid_at"`
	Remark    string          `json:"remark"`
}

// AddPayment POST /api/orders/{id}/payments
// 记一笔收款，金额为负表示退款。实收与收款状态在同一事务内重算。
func (a *API) AddPayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req addPaymentReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}

	o, err := a.orders.AddPayment(r.Context(), service.AddPaymentInput{
		OrderID:   id,
		Amount:    req.Amount,
		PayMethod: req.PayMethod,
		PaidAt:    req.PaidAt.Ptr(),
		Remark:    req.Remark,
		Operator:  OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

// DeletePayment DELETE /api/payments/{id}
// 收款记错了就删掉，软删除后同样重算实收与收款状态。
func (a *API) DeletePayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	o, err := a.orders.DeletePayment(r.Context(), id, OpenIDFrom(r.Context()))
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}
