package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"crab-order/internal/auth"
	"crab-order/internal/config"
	"crab-order/internal/errs"
	"crab-order/internal/store"
	"crab-order/internal/timex"
	"crab-order/internal/wechat"
)

const (
	testSecret  = "0123456789abcdef0123456789abcdef"
	testAdminID = "oAdmin"
)

// TestMain 默认丢弃日志，免得访问日志淹没测试输出。
// 需要检查日志内容的用例自己 SetDefault 一个带缓冲的 handler。
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	os.Exit(m.Run())
}

type testEnv struct {
	srv   *httptest.Server
	token string
	store *store.SQLiteStore
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "api_test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := st.SeedSpecs(ctx, timex.Now()); err != nil {
		t.Fatalf("seed specs: %v", err)
	}

	cfg := &config.Config{
		Env:                  config.EnvDev,
		HTTPAddr:             ":0",
		DBPath:               "test",
		AuthSecret:           testSecret,
		AuthTokenTTL:         time.Hour,
		AdminOpenIDs:         []string{testAdminID},
		RegLinkTTL:           time.Hour,
		Timezone:             "Asia/Shanghai",
		PublicRateLimit:      20,
		PublicWriteRateLimit: 20,
	}
	signer := auth.NewSigner(cfg.AuthSecret, cfg.AuthTokenTTL)
	a := New(cfg, st, signer, wechat.NewClient("wxtest", "secret"))
	srv := httptest.NewServer(a.Handler())

	token, _, err := signer.Issue(testAdminID, time.Now())
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	t.Cleanup(func() {
		srv.Close()
		a.Close()
		st.Close()
	})
	return &testEnv{srv: srv, token: token, store: st}
}

// call 发起一次带 token 的请求，返回 HTTP 状态码与统一响应体。
func (e *testEnv) call(t *testing.T, method, path string, body any) (int, Response, json.RawMessage) {
	t.Helper()
	return e.callWithToken(t, method, path, body, e.token)
}

func (e *testEnv) callWithToken(t *testing.T, method, path string, body any, token string) (int, Response, json.RawMessage) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("%s %s 响应不是合法 JSON: %s", method, path, raw)
	}
	return resp.StatusCode, Response{Code: envelope.Code, Msg: envelope.Msg}, envelope.Data
}

func (e *testEnv) mustOK(t *testing.T, method, path string, body any) json.RawMessage {
	t.Helper()
	status, resp, data := e.call(t, method, path, body)
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("%s %s 失败: status=%d code=%d msg=%s", method, path, status, resp.Code, resp.Msg)
	}
	return data
}

func decodeOrder(t *testing.T, data json.RawMessage) OrderDTO {
	t.Helper()
	var o OrderDTO
	if err := json.Unmarshal(data, &o); err != nil {
		t.Fatalf("解析订单失败: %v", err)
	}
	return o
}

func sampleOrderBody() map[string]any {
	return map[string]any{
		"receiver_name": "张三",
		"phone":         "13800138000",
		"address":       "江苏省苏州市工业园区xx路88号3栋201",
		"wechat_nick":   "老张",
		"wechat_remark": "同学介绍",
		"items": []map[string]any{
			{"gender": "male", "spec_gram": 225, "spec_label": "4.5两", "unit": "piece", "quantity": 5, "unit_price": 8800},
			{"gender": "female", "spec_gram": 175, "spec_label": "3.5两", "unit": "piece", "quantity": 5, "unit_price": 6800},
		},
		"freight_fee":      2000,
		"discount":         1000,
		"expect_ship_date": "2026-09-16",
		"remark":           "周五之前务必发出",
	}
}

// ========== 用例 ==========

func TestHealthz(t *testing.T) {
	e := newTestEnv(t)
	status, resp, data := e.callWithToken(t, http.MethodGet, "/healthz", nil, "")
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("healthz 失败: status=%d code=%d", status, resp.Code)
	}
	if !strings.Contains(string(data), `"status":"ok"`) {
		t.Errorf("healthz 响应不对: %s", data)
	}
}

// TestOrderLifecycle 建单 → 查详情 → 发货 → 收货 全链路。
func TestOrderLifecycle(t *testing.T) {
	e := newTestEnv(t)

	created := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))
	if created.PayableAmount != 79000 || created.PayableAmountYuan != "790.00" {
		t.Errorf("应收不对: %d / %s", created.PayableAmount, created.PayableAmountYuan)
	}
	if created.GoodsAmount != 78000 || created.ShipStatus != "pending" || created.PayStatus != "unpaid" {
		t.Errorf("建单结果不对: %+v", created)
	}
	if created.Items[0].GenderText != "公" || created.Items[0].UnitText != "只" {
		t.Errorf("中文展示字段不对: %+v", created.Items[0])
	}
	if created.ShipTime != nil || created.SettledTime != nil {
		t.Error("空时间应输出 null")
	}
	if created.Idempotent {
		t.Error("首次建单不应带 idempotent")
	}

	// 查详情
	path := "/api/orders/" + itoa(created.ID)
	detail := decodeOrder(t, e.mustOK(t, http.MethodGet, path, nil))
	if detail.OrderNo != created.OrderNo {
		t.Errorf("单号不一致: %s vs %s", detail.OrderNo, created.OrderNo)
	}

	// 按单号查详情
	byNo := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+created.OrderNo, nil))
	if byNo.ID != created.ID {
		t.Errorf("按单号查到的订单不对: %d", byNo.ID)
	}

	// 列表返回摘要
	listData := e.mustOK(t, http.MethodGet, "/api/orders?page=1&page_size=10", nil)
	var page struct {
		List     []OrderSummaryDTO `json:"list"`
		Total    int               `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"page_size"`
	}
	if err := json.Unmarshal(listData, &page); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if page.Total != 1 || len(page.List) != 1 || page.Page != 1 || page.PageSize != 10 {
		t.Fatalf("列表结构不对: %+v", page)
	}
	if page.List[0].ItemsSummary != "公4.5两×5, 母3.5两×5" {
		t.Errorf("items_summary 不对: %q", page.List[0].ItemsSummary)
	}

	// 发货
	shipped := decodeOrder(t, e.mustOK(t, http.MethodPost, path+"/ship", map[string]any{
		"ship_company": "顺丰速运",
		"tracking_no":  "SF1234567890",
		"ship_time":    nil,
	}))
	if shipped.ShipStatus != "shipped" || shipped.ShipStatusText != "已发货" || shipped.ShipTime == nil {
		t.Errorf("发货结果不对: %+v", shipped)
	}

	// 收货
	received := decodeOrder(t, e.mustOK(t, http.MethodPost, path+"/receive", map[string]any{"receive_time": nil}))
	if received.ShipStatus != "received" || received.ReceiveTime == nil {
		t.Errorf("收货结果不对: %+v", received)
	}
	if len(received.Logs) < 3 {
		t.Errorf("应有 create/ship/receive 三条日志，实际 %d 条", len(received.Logs))
	}
}

// TestIdempotentCreate 同一 request_id 连发两次只产生一笔订单。
func TestIdempotentCreate(t *testing.T) {
	e := newTestEnv(t)

	body := sampleOrderBody()
	body["request_id"] = "mp-1726300000-abc123"

	first := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", body))
	second := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", body))

	if first.ID != second.ID {
		t.Errorf("幂等失败，产生了两笔订单: %d vs %d", first.ID, second.ID)
	}
	if !second.Idempotent {
		t.Error("重试响应应带 idempotent=true")
	}

	listData := e.mustOK(t, http.MethodGet, "/api/orders", nil)
	var page struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(listData, &page)
	if page.Total != 1 {
		t.Errorf("库里应只有 1 笔订单，实际 %d", page.Total)
	}
}

// TestPaymentFlow 部分收款 → partial → 补尾款 → paid → 删一笔 → 退回 partial。
func TestPaymentFlow(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))
	path := "/api/orders/" + itoa(o.ID)

	partial := decodeOrder(t, e.mustOK(t, http.MethodPost, path+"/payments", map[string]any{
		"amount": 40000, "pay_method": "wechat", "remark": "定金",
	}))
	if partial.PayStatus != "partial" || partial.PayStatusText != "部分付款" {
		t.Errorf("应为 partial，实际 %s", partial.PayStatus)
	}
	if partial.UnpaidAmount != 39000 || partial.UnpaidAmountYuan != "390.00" {
		t.Errorf("未收金额不对: %d / %s", partial.UnpaidAmount, partial.UnpaidAmountYuan)
	}
	if partial.FirstPayTime == nil {
		t.Error("应写入 first_pay_time")
	}

	paid := decodeOrder(t, e.mustOK(t, http.MethodPost, path+"/payments", map[string]any{
		"amount": 39000, "remark": "尾款",
	}))
	if paid.PayStatus != "paid" || paid.SettledTime == nil {
		t.Errorf("应为 paid 且有 settled_time，实际 %s %v", paid.PayStatus, paid.SettledTime)
	}
	if len(paid.Payments) != 2 {
		t.Fatalf("应有 2 条收款流水，实际 %d", len(paid.Payments))
	}

	// 删掉尾款
	back := decodeOrder(t, e.mustOK(t, http.MethodDelete,
		"/api/payments/"+itoa(paid.Payments[1].ID), nil))
	if back.PayStatus != "partial" || back.PaidAmount != 40000 {
		t.Errorf("删除尾款后应退回 partial，实际 %s paid=%d", back.PayStatus, back.PaidAmount)
	}
	if back.SettledTime != nil {
		t.Error("退回 partial 后 settled_time 应清空")
	}
}

// TestIllegalTransition pending 直接调 /receive 返回 40902。
func TestIllegalTransition(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))

	status, resp, _ := e.call(t, http.MethodPost, "/api/orders/"+itoa(o.ID)+"/receive", map[string]any{})
	if status != http.StatusConflict || resp.Code != errs.CodeStateConflict {
		t.Fatalf("期望 409/40902，实际 %d/%d", status, resp.Code)
	}
}

// TestRevertShipEndpoint 回退接口。
func TestRevertShipEndpoint(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))
	path := "/api/orders/" + itoa(o.ID)

	e.mustOK(t, http.MethodPost, path+"/ship", map[string]any{
		"ship_company": "顺丰速运", "tracking_no": "SF1234567890",
	})
	reverted := decodeOrder(t, e.mustOK(t, http.MethodPost, path+"/revert-ship", map[string]any{
		"to": "pending", "reason": "点错了",
	}))
	if reverted.ShipStatus != "pending" || reverted.ShipTime != nil || reverted.TrackingNo != "" {
		t.Errorf("回退后应清空发货信息: %+v", reverted)
	}

	// 非法回退目标
	status, resp, _ := e.call(t, http.MethodPost, path+"/revert-ship", map[string]any{
		"to": "received", "reason": "乱点",
	})
	if status != http.StatusConflict || resp.Code != errs.CodeStateConflict {
		t.Errorf("非法回退应返回 409/40902，实际 %d/%d", status, resp.Code)
	}
}

// TestUnauthorized 无 token 访问返回 40100。
func TestUnauthorized(t *testing.T) {
	e := newTestEnv(t)

	for _, c := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/orders"},
		{http.MethodPost, "/api/orders"},
		{http.MethodGet, "/api/stats/dashboard"},
		{http.MethodGet, "/api/specs"},
	} {
		status, resp, _ := e.callWithToken(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized || resp.Code != errs.CodeUnauthorized {
			t.Errorf("%s %s 无 token 期望 401/40100，实际 %d/%d", c.method, c.path, status, resp.Code)
		}
	}

	// 篡改过的 token 同样拒绝
	status, resp, _ := e.callWithToken(t, http.MethodGet, "/api/orders", nil, "forged.token")
	if status != http.StatusUnauthorized || resp.Code != errs.CodeUnauthorized {
		t.Errorf("伪造 token 期望 401/40100，实际 %d/%d", status, resp.Code)
	}
}

// TestForbiddenNonAdmin token 合法但 openid 不在白名单里 → 40300。
func TestForbiddenNonAdmin(t *testing.T) {
	e := newTestEnv(t)
	other, _, err := auth.NewSigner(testSecret, time.Hour).Issue("oStranger", time.Now())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	status, resp, _ := e.callWithToken(t, http.MethodGet, "/api/orders", nil, other)
	if status != http.StatusForbidden || resp.Code != errs.CodeForbidden {
		t.Errorf("期望 403/40300，实际 %d/%d", status, resp.Code)
	}
}

// TestPublicQuery 买家免登录查单：尾号错误返回 40400，正确则返回脱敏数据。
func TestPublicQuery(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))
	e.mustOK(t, http.MethodPost, "/api/orders/"+itoa(o.ID)+"/ship", map[string]any{
		"ship_company": "顺丰速运", "tracking_no": "SF1234567890",
	})

	// 尾号错误
	status, resp, _ := e.callWithToken(t, http.MethodGet,
		"/api/public/orders?order_no="+o.OrderNo+"&phone_tail=0000", nil, "")
	if status != http.StatusNotFound || resp.Code != errs.CodeNotFound {
		t.Fatalf("尾号错误期望 404/40400，实际 %d/%d", status, resp.Code)
	}

	// 单号不存在也是同样的错误，不泄露差异
	_, resp2, _ := e.callWithToken(t, http.MethodGet,
		"/api/public/orders?order_no=20990101-001&phone_tail=8000", nil, "")
	if resp2.Code != resp.Code || resp2.Msg != resp.Msg {
		t.Error("单号不存在与尾号错误应返回完全一致的响应")
	}

	// 正确查询，免 token
	status, resp3, data := e.callWithToken(t, http.MethodGet,
		"/api/public/orders?order_no="+o.OrderNo+"&phone_tail=8000", nil, "")
	if status != http.StatusOK || resp3.Code != errs.CodeOK {
		t.Fatalf("买家查单失败: %d/%d %s", status, resp3.Code, resp3.Msg)
	}
	var pub PublicOrderDTO
	if err := json.Unmarshal(data, &pub); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if pub.ReceiverName != "张*" || pub.Phone != "138****8000" {
		t.Errorf("脱敏不到位: %s / %s", pub.ReceiverName, pub.Phone)
	}
	if pub.Address != "江苏省苏州市工业园区" {
		t.Errorf("地址应只到区县级，实际 %q", pub.Address)
	}
	if strings.Contains(string(data), "amount") || strings.Contains(string(data), "remark") {
		t.Errorf("买家视图不应包含金额与备注: %s", data)
	}
	if pub.TrackingNo != "SF1234567890" || pub.ShipStatusText != "已发货" {
		t.Errorf("物流信息不对: %+v", pub)
	}
}

// TestPublicRateLimit 买家查单按 IP 限流，超出返回 42900。
func TestPublicRateLimit(t *testing.T) {
	e := newTestEnv(t)
	path := "/api/public/orders?order_no=20990101-001&phone_tail=8000"

	limited := false
	for i := 0; i < 25; i++ {
		status, resp, _ := e.callWithToken(t, http.MethodGet, path, nil, "")
		if resp.Code == errs.CodeRateLimited {
			if status != http.StatusTooManyRequests {
				t.Fatalf("限流应返回 HTTP 429，实际 %d", status)
			}
			limited = true
			break
		}
	}
	if !limited {
		t.Error("连续 25 次请求应触发限流（每分钟 20 次）")
	}
}

// TestExportCSV 导出必须带 BOM，金额列为元。
func TestExportCSV(t *testing.T) {
	e := newTestEnv(t)
	e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody())

	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/orders/export", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("CSV 必须以 UTF-8 BOM 开头，否则 Excel 打开中文乱码")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="orders_`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	text := string(body)
	if !strings.Contains(text, "单号") || !strings.Contains(text, "790.00") {
		t.Errorf("CSV 内容不对: %s", text)
	}
}

// TestStatsDashboard 看板与发货计划。
func TestStatsDashboard(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))
	e.mustOK(t, http.MethodPost, "/api/orders/"+itoa(o.ID)+"/payments", map[string]any{"amount": 40000})

	data := e.mustOK(t, http.MethodGet, "/api/stats/dashboard", nil)
	var d struct {
		Today struct {
			NewOrders   int    `json:"new_orders"`
			Revenue     int64  `json:"revenue"`
			RevenueYuan string `json:"revenue_yuan"`
		} `json:"today"`
		Pending struct {
			ToShipCount      int   `json:"to_ship_count"`
			UnpaidOrderCount int   `json:"unpaid_order_count"`
			UnpaidAmount     int64 `json:"unpaid_amount"`
		} `json:"pending"`
		Range struct {
			OrderCount   int   `json:"order_count"`
			PayableTotal int64 `json:"payable_total"`
			CrabCount    int   `json:"crab_count"`
			BySpec       []struct {
				SpecLabel string `json:"spec_label"`
				Quantity  int    `json:"quantity"`
			} `json:"by_spec"`
		} `json:"range"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("解析看板失败: %v", err)
	}
	if d.Today.NewOrders != 1 || d.Today.Revenue != 40000 || d.Today.RevenueYuan != "400.00" {
		t.Errorf("今日统计不对: %+v", d.Today)
	}
	if d.Pending.ToShipCount != 1 || d.Pending.UnpaidOrderCount != 1 || d.Pending.UnpaidAmount != 39000 {
		t.Errorf("待办统计不对: %+v", d.Pending)
	}
	if d.Range.OrderCount != 1 || d.Range.PayableTotal != 79000 || d.Range.CrabCount != 10 {
		t.Errorf("区间统计不对: %+v", d.Range)
	}
	if len(d.Range.BySpec) != 2 {
		t.Errorf("按规格聚合应有 2 项，实际 %d", len(d.Range.BySpec))
	}

	// 发货计划：查约定发货日那天
	planData := e.mustOK(t, http.MethodGet, "/api/stats/ship-plan?date=2026-09-16", nil)
	var plan struct {
		Date  string            `json:"date"`
		List  []OrderSummaryDTO `json:"list"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("解析发货计划失败: %v", err)
	}
	if plan.Date != "2026-09-16" || plan.Total != 1 {
		t.Errorf("发货计划不对: %+v", plan)
	}
}

// TestSpecsAndAddresses 规格价目表与地址簿。
func TestSpecsAndAddresses(t *testing.T) {
	e := newTestEnv(t)

	data := e.mustOK(t, http.MethodGet, "/api/specs", nil)
	var specs struct {
		List  []SpecDTO `json:"list"`
		Total int       `json:"total"`
	}
	if err := json.Unmarshal(data, &specs); err != nil {
		t.Fatalf("解析规格失败: %v", err)
	}
	// 种子价目表是 4 档套餐（8 只装，四个价位）
	if specs.Total != 4 {
		t.Fatalf("种子数据应有 4 档，实际 %d", specs.Total)
	}
	for _, sp := range specs.List {
		if sp.PackSize != 8 || sp.Unit != "box" {
			t.Fatalf("种子档应是 8 只装的盒装，实际 %+v", sp)
		}
	}

	// 新增
	created := e.mustOK(t, http.MethodPost, "/api/specs", map[string]any{
		"gender": "male", "spec_gram": 300, "spec_label": "6.0两", "unit": "piece", "unit_price": 16800,
	})
	var sp SpecDTO
	if err := json.Unmarshal(created, &sp); err != nil {
		t.Fatalf("解析新增规格失败: %v", err)
	}
	if sp.UnitPriceYuan != "168.00" || !sp.Enabled {
		t.Errorf("新增规格不对: %+v", sp)
	}

	// 重复新增 → 40001
	status, resp, _ := e.call(t, http.MethodPost, "/api/specs", map[string]any{
		"gender": "male", "spec_gram": 300, "spec_label": "6.0两", "unit": "piece", "unit_price": 16800,
	})
	if status != http.StatusBadRequest || resp.Code != errs.CodeInvalidParam {
		t.Errorf("重复规格期望 400/40001，实际 %d/%d", status, resp.Code)
	}

	// 停用
	e.mustOK(t, http.MethodDelete, "/api/specs/"+itoa(sp.ID), nil)
	data = e.mustOK(t, http.MethodGet, "/api/specs", nil)
	_ = json.Unmarshal(data, &specs)
	if specs.Total != 4 {
		t.Errorf("停用后默认列表应仍是 4 档，实际 %d", specs.Total)
	}
	data = e.mustOK(t, http.MethodGet, "/api/specs?all=1", nil)
	_ = json.Unmarshal(data, &specs)
	if specs.Total != 5 {
		t.Errorf("all=1 应返回 5 档，实际 %d", specs.Total)
	}

	// 地址簿：下过单之后才有
	e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody())
	data = e.mustOK(t, http.MethodGet, "/api/addresses?keyword=张", nil)
	var addrs struct {
		List  []AddressDTO `json:"list"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(data, &addrs); err != nil {
		t.Fatalf("解析地址簿失败: %v", err)
	}
	if addrs.Total != 1 || addrs.List[0].Phone != "13800138000" || addrs.List[0].OrderCount != 1 {
		t.Errorf("地址簿不对: %+v", addrs)
	}
}

// TestSpecPriceChangeDoesNotAffectHistory 改价只影响新订单，历史订单金额是快照。
func TestSpecPriceChangeDoesNotAffectHistory(t *testing.T) {
	e := newTestEnv(t)
	old := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))

	// 建一档和历史订单明细对得上的规格（公 4.5 两，88 元），再把它涨到 95 元
	created := e.mustOK(t, http.MethodPost, "/api/specs", map[string]any{
		"gender": "male", "spec_gram": 225, "spec_label": "4.5两", "unit": "piece", "unit_price": 8800,
	})
	var target SpecDTO
	if err := json.Unmarshal(created, &target); err != nil {
		t.Fatalf("解析规格失败: %v", err)
	}

	e.mustOK(t, http.MethodPut, "/api/specs/"+itoa(target.ID), map[string]any{
		"gender": target.Gender, "spec_gram": target.SpecGram, "spec_label": target.SpecLabel,
		"unit": target.Unit, "unit_price": 9500, // 88 元涨到 95 元
	})

	again := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/"+itoa(old.ID), nil))
	if again.Items[0].UnitPrice != 8800 || again.PayableAmount != 79000 {
		t.Errorf("改价后历史订单被改动了: unit_price=%d payable=%d",
			again.Items[0].UnitPrice, again.PayableAmount)
	}
}

// TestUpdateAndDeleteOrder 改单与软删。
func TestUpdateAndDeleteOrder(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))
	path := "/api/orders/" + itoa(o.ID)

	body := sampleOrderBody()
	body["receiver_name"] = "张小三"
	body["freight_fee"] = 0
	updated := decodeOrder(t, e.mustOK(t, http.MethodPut, path, body))
	if updated.ReceiverName != "张小三" || updated.PayableAmount != 77000 {
		t.Errorf("改单结果不对: %s payable=%d", updated.ReceiverName, updated.PayableAmount)
	}

	e.mustOK(t, http.MethodDelete, path, nil)
	status, resp, _ := e.call(t, http.MethodGet, path, nil)
	if status != http.StatusNotFound || resp.Code != errs.CodeNotFound {
		t.Errorf("删除后应返回 404/40400，实际 %d/%d", status, resp.Code)
	}
}

// TestInvalidParams 参数校验与未知路由。
func TestInvalidParams(t *testing.T) {
	e := newTestEnv(t)

	body := sampleOrderBody()
	body["phone"] = "12345"
	status, resp, _ := e.call(t, http.MethodPost, "/api/orders", body)
	if status != http.StatusBadRequest || resp.Code != errs.CodeInvalidParam {
		t.Errorf("非法手机号期望 400/40001，实际 %d/%d", status, resp.Code)
	}
	if !strings.Contains(resp.Msg, "phone") {
		t.Errorf("错误信息应指明具体字段，实际 %q", resp.Msg)
	}

	status, resp, _ = e.call(t, http.MethodGet, "/api/orders?ship_status=flying", nil)
	if status != http.StatusBadRequest || resp.Code != errs.CodeInvalidParam {
		t.Errorf("非法筛选值期望 400/40001，实际 %d/%d", status, resp.Code)
	}

	status, resp, _ = e.call(t, http.MethodGet, "/api/nonexistent", nil)
	if status != http.StatusNotFound || resp.Code != errs.CodeNotFound {
		t.Errorf("未知路由期望 404/40400，实际 %d/%d", status, resp.Code)
	}
}

// TestRequestIDHeader 每个响应都带 X-Request-Id。
func TestRequestIDHeader(t *testing.T) {
	e := newTestEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/healthz", nil)
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("响应应带 X-Request-Id")
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
