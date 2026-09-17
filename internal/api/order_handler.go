package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/service"
	"crab-order/internal/timex"
)

// flexTime 兼容两种时间写法：Unix 秒（整数）与 RFC3339 字符串。
type flexTime struct{ ts *int64 }

func (f *flexTime) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` || s == "" {
		return nil
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339, str)
		if err != nil {
			return errs.InvalidParam("时间格式应为 Unix 秒或 RFC3339 字符串")
		}
		ts := t.Unix()
		f.ts = &ts
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return errs.InvalidParam("时间格式应为 Unix 秒或 RFC3339 字符串")
	}
	f.ts = &n
	return nil
}

func (f flexTime) Ptr() *int64 { return f.ts }

type itemReq struct {
	Gender    model.Gender `json:"gender"`
	SpecGram  int          `json:"spec_gram"`
	SpecLabel string       `json:"spec_label"`
	Unit      model.Unit   `json:"unit"`
	Quantity  int          `json:"quantity"`
	UnitPrice int64        `json:"unit_price"`
}

func toItemInputs(in []itemReq) []service.ItemInput {
	out := make([]service.ItemInput, 0, len(in))
	for _, it := range in {
		out = append(out, service.ItemInput{
			Gender:    it.Gender,
			SpecGram:  it.SpecGram,
			SpecLabel: it.SpecLabel,
			Unit:      it.Unit,
			Quantity:  it.Quantity,
			UnitPrice: it.UnitPrice,
		})
	}
	return out
}

type createOrderReq struct {
	RequestID      string    `json:"request_id"`
	ReceiverName   string    `json:"receiver_name"`
	Phone          string    `json:"phone"`
	Address        string    `json:"address"`
	WechatNick     string    `json:"wechat_nick"`
	WechatRemark   string    `json:"wechat_remark"`
	Items          []itemReq `json:"items"`
	FreightFee     int64     `json:"freight_fee"`
	Discount       int64     `json:"discount"`
	ExpectShipDate string    `json:"expect_ship_date"`
	Remark         string    `json:"remark"`
}

// CreateOrder POST /api/orders
func (a *API) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}

	o, idempotent, err := a.orders.CreateOrder(r.Context(), service.CreateOrderInput{
		RequestID:      req.RequestID,
		ReceiverName:   req.ReceiverName,
		Phone:          req.Phone,
		Address:        req.Address,
		WechatNick:     req.WechatNick,
		WechatRemark:   req.WechatRemark,
		Items:          toItemInputs(req.Items),
		FreightFee:     req.FreightFee,
		Discount:       req.Discount,
		ExpectShipDate: req.ExpectShipDate,
		Remark:         req.Remark,
		Operator:       OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}

	dto := ToOrderDTO(o)
	// 幂等命中不是错误：直接返回已有订单，小程序的网络重试不会重复建单。
	dto.Idempotent = idempotent
	OK(w, dto)
}

// ListOrders GET /api/orders
func (a *API) ListOrders(w http.ResponseWriter, r *http.Request) {
	f, err := parseOrderFilter(r, true)
	if err != nil {
		Fail(w, r, err)
		return
	}
	list, total, err := a.orders.ListOrders(r.Context(), f)
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, PageData{
		List:     ToOrderSummaryList(list),
		Total:    total,
		Page:     f.Page,
		PageSize: f.PageSize,
	})
}

// GetOrder GET /api/orders/{id}
func (a *API) GetOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	o, err := a.orders.GetOrder(r.Context(), id)
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

// GetOrderByNo GET /api/orders/by-no/{order_no}
func (a *API) GetOrderByNo(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("order_no")
	if orderNo == "" {
		Fail(w, r, errs.InvalidParam("order_no 必填"))
		return
	}
	o, err := a.orders.GetOrderByNo(r.Context(), orderNo)
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

type updateOrderReq struct {
	ReceiverName   string    `json:"receiver_name"`
	Phone          string    `json:"phone"`
	Address        string    `json:"address"`
	WechatNick     string    `json:"wechat_nick"`
	WechatRemark   string    `json:"wechat_remark"`
	Items          []itemReq `json:"items"`
	FreightFee     int64     `json:"freight_fee"`
	Discount       int64     `json:"discount"`
	ExpectShipDate string    `json:"expect_ship_date"`
	Remark         string    `json:"remark"`
	// UpdatedAt 为可选的乐观锁版本，手机与平板同时操作时用得上。
	UpdatedAt flexTime `json:"updated_at"`
}

// UpdateOrder PUT /api/orders/{id}
func (a *API) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req updateOrderReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	var expected int64
	if p := req.UpdatedAt.Ptr(); p != nil {
		expected = *p
	}

	o, err := a.orders.UpdateOrder(r.Context(), service.UpdateOrderInput{
		ID:                id,
		ReceiverName:      req.ReceiverName,
		Phone:             req.Phone,
		Address:           req.Address,
		WechatNick:        req.WechatNick,
		WechatRemark:      req.WechatRemark,
		Items:             toItemInputs(req.Items),
		FreightFee:        req.FreightFee,
		Discount:          req.Discount,
		ExpectShipDate:    req.ExpectShipDate,
		Remark:            req.Remark,
		ExpectedUpdatedAt: expected,
		Operator:          OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

type shipReq struct {
	ShipCompany string   `json:"ship_company"`
	TrackingNo  string   `json:"tracking_no"`
	ShipTime    flexTime `json:"ship_time"`
}

// ShipOrder POST /api/orders/{id}/ship
func (a *API) ShipOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req shipReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	o, err := a.orders.Ship(r.Context(), service.ShipInput{
		ID:          id,
		ShipCompany: req.ShipCompany,
		TrackingNo:  req.TrackingNo,
		ShipTime:    req.ShipTime.Ptr(),
		Operator:    OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

type receiveReq struct {
	ReceiveTime flexTime `json:"receive_time"`
}

// ReceiveOrder POST /api/orders/{id}/receive
func (a *API) ReceiveOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req receiveReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	o, err := a.orders.Receive(r.Context(), service.ReceiveInput{
		ID:          id,
		ReceiveTime: req.ReceiveTime.Ptr(),
		Operator:    OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

type cancelReq struct {
	Reason string `json:"reason"`
}

// CancelOrder POST /api/orders/{id}/cancel
func (a *API) CancelOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req cancelReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	o, err := a.orders.Cancel(r.Context(), service.CancelInput{
		ID:       id,
		Reason:   req.Reason,
		Operator: OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

type revertShipReq struct {
	To     model.ShipStatus `json:"to"`
	Reason string           `json:"reason"`
}

// RevertShip POST /api/orders/{id}/revert-ship
func (a *API) RevertShip(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	var req revertShipReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	o, err := a.orders.RevertShip(r.Context(), service.RevertShipInput{
		ID:       id,
		To:       req.To,
		Reason:   req.Reason,
		Operator: OpenIDFrom(r.Context()),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, ToOrderDTO(o))
}

// DeleteOrder DELETE /api/orders/{id}
func (a *API) DeleteOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		Fail(w, r, err)
		return
	}
	if err := a.orders.DeleteOrder(r.Context(), id, OpenIDFrom(r.Context())); err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, map[string]any{"id": id})
}

// ExportOrders GET /api/orders/export
func (a *API) ExportOrders(w http.ResponseWriter, r *http.Request) {
	f, err := parseOrderFilter(r, false)
	if err != nil {
		Fail(w, r, err)
		return
	}
	list, _, err := a.orders.ListOrders(r.Context(), f)
	if err != nil {
		Fail(w, r, err)
		return
	}

	filename := "orders_" + timex.DateKey(time.Now().Unix()) + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	// UTF-8 BOM：不写的话 Excel 打开中文必然乱码。
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{
		"单号", "创建时间", "收货人", "手机", "地址", "微信备注", "明细摘要",
		"货款", "运费", "优惠", "应收", "实收", "未收",
		"发货状态", "收款状态", "快递公司", "运单号", "约定发货日", "发货时间", "备注", "来源",
	})
	for _, o := range list {
		shipTime := ""
		if o.ShipTime != nil {
			shipTime = timex.Format(*o.ShipTime)
		}
		_ = cw.Write([]string{
			o.OrderNo,
			timex.Format(o.CreatedAt),
			o.ReceiverName,
			o.Phone,
			o.Address,
			o.WechatRemark,
			o.ItemsSummary(),
			// 金额列输出「元」，方便直接在 Excel 里求和。
			model.FormatYuan(o.GoodsAmount),
			model.FormatYuan(o.FreightFee),
			model.FormatYuan(o.Discount),
			model.FormatYuan(o.PayableAmount),
			model.FormatYuan(o.PaidAmount),
			model.FormatYuan(o.UnpaidAmount()),
			o.ShipStatus.Text(),
			o.PayStatus.Text(),
			o.ShipCompany,
			o.TrackingNo,
			o.ExpectShipDate,
			shipTime,
			o.Remark,
			o.Source.Text(),
		})
	}
}

// ---------- 查询参数解析 ----------

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// parseOrderFilter 解析列表/导出共用的筛选参数。paginate 为 false 时不分页（导出用）。
func parseOrderFilter(r *http.Request, paginate bool) (model.OrderFilter, error) {
	q := r.URL.Query()
	f := model.OrderFilter{
		Keyword:             strings.TrimSpace(q.Get("keyword")),
		ExpectShipDate:      q.Get("expect_ship_date"),
		ExpectShipDateStart: q.Get("expect_ship_date_start"),
		ExpectShipDateEnd:   q.Get("expect_ship_date_end"),
		Sort:                q.Get("sort"),
	}

	for _, v := range splitCSV(q.Get("ship_status")) {
		s := model.ShipStatus(v)
		if !s.Valid() {
			return f, errs.InvalidParam("ship_status 非法：%s", v)
		}
		f.ShipStatus = append(f.ShipStatus, s)
	}
	if v := strings.TrimSpace(q.Get("source")); v != "" {
		src := model.Source(v)
		if !src.Valid() {
			return f, errs.InvalidParam("source 非法，应为 manual/web")
		}
		f.Source = src
	}
	for _, v := range splitCSV(q.Get("pay_status")) {
		s := model.PayStatus(v)
		if !s.Valid() {
			return f, errs.InvalidParam("pay_status 非法：%s", v)
		}
		f.PayStatus = append(f.PayStatus, s)
	}

	for name, dst := range map[string]*string{
		"expect_ship_date":       &f.ExpectShipDate,
		"expect_ship_date_start": &f.ExpectShipDateStart,
		"expect_ship_date_end":   &f.ExpectShipDateEnd,
	} {
		if *dst != "" && !timex.ValidDate(*dst) {
			return f, errs.InvalidParam("%s 格式应为 YYYY-MM-DD", name)
		}
	}

	switch f.Sort {
	case "", model.SortCreatedDesc, model.SortCreatedAsc, model.SortExpectAsc:
	default:
		return f, errs.InvalidParam("sort 非法，应为 created_desc/created_asc/expect_asc")
	}

	var err error
	if f.CreatedStart, err = parseTimeQuery(q.Get("created_start"), "created_start"); err != nil {
		return f, err
	}
	if f.CreatedEnd, err = parseTimeQuery(q.Get("created_end"), "created_end"); err != nil {
		return f, err
	}

	if paginate {
		f.Page, f.PageSize = parsePage(q.Get("page"), q.Get("page_size"))
	}
	return f, nil
}

func parsePage(pageStr, sizeStr string) (int, int) {
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size
}

// parseTimeQuery 接受 Unix 秒或 RFC3339 字符串。
func parseTimeQuery(v, name string) (*int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return &n, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, errs.InvalidParam("%s 应为 Unix 秒或 RFC3339 字符串", name)
	}
	ts := t.Unix()
	return &ts, nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pathID(r *http.Request, name string) (int64, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errs.InvalidParam("%s 非法", name)
	}
	return id, nil
}
