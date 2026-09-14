package api

import (
	"net/http"

	"crab-order/internal/auth"
	"crab-order/internal/config"
	"crab-order/internal/errs"
	"crab-order/internal/service"
	"crab-order/internal/store"
	"crab-order/internal/wechat"
)

// API 汇总 handler 需要的依赖。handler 只做参数解析与响应包装，不碰 SQL。
type API struct {
	cfg       *config.Config
	orders    *service.OrderService
	stats     *service.StatsService
	specs     *service.SpecService
	addresses *service.AddressService
	signer    *auth.Signer
	wechat    *wechat.Client
	limiter   *RateLimiter
}

func New(cfg *config.Config, st store.Store, signer *auth.Signer, wx *wechat.Client) *API {
	return &API{
		cfg:       cfg,
		orders:    service.NewOrderService(st),
		stats:     service.NewStatsService(st),
		specs:     service.NewSpecService(st),
		addresses: service.NewAddressService(st),
		signer:    signer,
		wechat:    wx,
		limiter:   NewRateLimiter(cfg.PublicRateLimit),
	}
}

// Orders 暴露订单服务，供测试直接调用。
func (a *API) Orders() *service.OrderService { return a.orders }

// Close 释放后台资源。
func (a *API) Close() {
	if a.limiter != nil {
		a.limiter.Close()
	}
}

// Handler 注册路由并按「从外到内」的顺序套上中间件。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// 健康检查，不走鉴权。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		OK(w, map[string]string{"status": "ok"})
	})

	// 登录
	mux.HandleFunc("POST /api/login", a.Login)

	// 订单
	mux.HandleFunc("POST /api/orders", a.CreateOrder)
	mux.HandleFunc("GET /api/orders", a.ListOrders)
	// 字面量路径比通配符更具体，Go 1.22 的 ServeMux 会优先匹配它，不会被 {id} 抢走。
	mux.HandleFunc("GET /api/orders/export", a.ExportOrders)
	mux.HandleFunc("GET /api/orders/by-no/{order_no}", a.GetOrderByNo)
	mux.HandleFunc("GET /api/orders/{id}", a.GetOrder)
	mux.HandleFunc("PUT /api/orders/{id}", a.UpdateOrder)
	mux.HandleFunc("DELETE /api/orders/{id}", a.DeleteOrder)
	mux.HandleFunc("POST /api/orders/{id}/ship", a.ShipOrder)
	mux.HandleFunc("POST /api/orders/{id}/receive", a.ReceiveOrder)
	mux.HandleFunc("POST /api/orders/{id}/cancel", a.CancelOrder)
	mux.HandleFunc("POST /api/orders/{id}/revert-ship", a.RevertShip)

	// 收款
	mux.HandleFunc("POST /api/orders/{id}/payments", a.AddPayment)
	mux.HandleFunc("DELETE /api/payments/{id}", a.DeletePayment)

	// 统计
	mux.HandleFunc("GET /api/stats/dashboard", a.Dashboard)
	mux.HandleFunc("GET /api/stats/ship-plan", a.ShipPlan)

	// 地址簿
	mux.HandleFunc("GET /api/addresses", a.ListAddresses)

	// 规格价目表
	mux.HandleFunc("GET /api/specs", a.ListSpecs)
	mux.HandleFunc("POST /api/specs", a.CreateSpec)
	mux.HandleFunc("PUT /api/specs/{id}", a.UpdateSpec)
	mux.HandleFunc("DELETE /api/specs/{id}", a.DisableSpec)

	// 买家免登录查单
	mux.HandleFunc("GET /api/public/orders", a.PublicQueryOrder)

	// 兜底：未命中的路径也返回统一 JSON，而不是标准库的纯文本 404。
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Fail(w, r, errs.New(errs.CodeNotFound, "接口不存在"))
	})

	// 顺序：Recover → RequestID → AccessLog → CORS(dev) → RateLimit → Auth
	ms := []Middleware{Recover, RequestID, AccessLog}
	if a.cfg.IsDev() {
		ms = append(ms, CORS)
	}
	ms = append(ms, RateLimit(a.limiter), Auth(a.signer, a.cfg))
	return Chain(mux, ms...)
}
