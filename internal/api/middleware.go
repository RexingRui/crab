package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"crab-order/internal/auth"
	"crab-order/internal/errs"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyOpenID
)

// RequestIDFrom 取出当前请求 ID。
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRequestID).(string)
	return v
}

// openIDHolder 让 AccessLog 也能拿到 openid。
//
// AccessLog 在链上位于 Auth 之外，拿不到 Auth 派生出的新 context，
// 所以放一个可写的持有者进去，由 Auth 回填。单个请求内是顺序执行的，没有并发写。
type openIDHolder struct{ value string }

// OpenIDFrom 取出当前登录者的 openid，未登录返回空串。
func OpenIDFrom(ctx context.Context) string {
	h, _ := ctx.Value(ctxKeyOpenID).(*openIDHolder)
	if h == nil {
		return ""
	}
	return h.value
}

// withOpenID 回填 openid：已有持有者就直接写，否则派生一个新的 context。
func withOpenID(ctx context.Context, openID string) context.Context {
	if h, ok := ctx.Value(ctxKeyOpenID).(*openIDHolder); ok {
		h.value = openID
		return ctx
	}
	return context.WithValue(ctx, ctxKeyOpenID, &openIDHolder{value: openID})
}

// Middleware 是标准的处理器装饰器。
type Middleware func(http.Handler) http.Handler

// Chain 按「从外到内」的顺序组合中间件：Chain(h, a, b) 的执行顺序是 a → b → h。
func Chain(h http.Handler, ms ...Middleware) http.Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		h = ms[i](h)
	}
	return h
}

// statusWriter 记录响应状态码，供访问日志使用。
type statusWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

// Flush 让 CSV 导出等流式响应能及时落地。
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Recover 捕获 panic，打日志并返回 50000。堆栈绝不返回给客户端。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic recovered",
					"path", r.URL.Path,
					"request_id", RequestIDFrom(r.Context()),
					"panic", p,
					"stack", string(debug.Stack()))
				Fail(w, r, errs.New(errs.CodeInternal, "服务内部错误"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestID 生成请求 ID，注入 context 与响应 Header。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// AccessLog 记录 method / path / status / 耗时 / requestID / openid。
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}

		// 先放一个空的持有者，Auth 校验通过后会把 openid 填进来。
		ctx := context.WithValue(r.Context(), ctxKeyOpenID, &openIDHolder{})
		r = r.WithContext(ctx)

		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		slog.Info("access",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"cost_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(ctx),
			"openid", OpenIDFrom(ctx),
			"ip", ClientIP(r),
		)
	})
}

// CORS 小程序不走 CORS，只为本地网页调试准备，仅在 ENV=dev 时启用。
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-Id")
		h.Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP 取客户端 IP。生产环境走 Caddy/Nginx 反代，优先信任 X-Forwarded-For 的第一跳。
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	if ip := r.Header.Get("X-Real-Ip"); ip != "" {
		return strings.TrimSpace(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- 限流 ----------

type bucket struct {
	count       int
	windowStart int64 // 所在分钟窗口的起点（Unix 秒）
}

// RateLimiter 是按 IP 的每分钟固定窗口计数器。
// 单机单管理员场景够用，不引入 Redis。
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   int
	now     func() time.Time
	stop    chan struct{}
}

func NewRateLimiter(limit int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		limit:   limit,
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go rl.janitor()
	return rl
}

// Allow 判断该 IP 在当前分钟窗口内是否还有配额。
func (rl *RateLimiter) Allow(ip string) bool {
	window := rl.now().Unix() / 60

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok || b.windowStart != window {
		rl.buckets[ip] = &bucket{count: 1, windowStart: window}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// janitor 定期清掉过期窗口，避免 map 无限增长。
func (rl *RateLimiter) janitor() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-t.C:
			window := rl.now().Unix() / 60
			rl.mu.Lock()
			for ip, b := range rl.buckets {
				if b.windowStart < window {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *RateLimiter) Close() {
	select {
	case <-rl.stop:
	default:
		close(rl.stop)
	}
}

// RateLimit 仅对 /api/public/** 与 /api/login 生效。
// 公开的写请求（买家登记）走更紧的那个配额，不和查单共用。
func RateLimit(rl, wl *RateLimiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter := limiterFor(r, rl, wl); limiter != nil && !limiter.Allow(ClientIP(r)) {
				Fail(w, r, errs.New(errs.CodeRateLimited, "请求过于频繁，请稍后再试"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// limiterFor 挑出这条请求该用哪个配额，不限流的路径返回 nil。
func limiterFor(r *http.Request, rl, wl *RateLimiter) *RateLimiter {
	path := r.URL.Path
	if !rateLimited(path) {
		return nil
	}
	if r.Method != http.MethodGet && strings.HasPrefix(path, "/api/public/") {
		return wl
	}
	return rl
}

func rateLimited(path string) bool {
	return strings.HasPrefix(path, "/api/public/") || path == "/api/login"
}

// authSkipped 列出无需登录的路径。
func authSkipped(path string) bool {
	return path == "/api/login" || path == "/healthz" || strings.HasPrefix(path, "/api/public/")
}

// AdminChecker 由 config 实现，用于随时把不在白名单里的人踢下线。
type AdminChecker interface {
	IsAdmin(openID string) bool
}

// TokenVerifier 由 auth.Signer 实现。
type TokenVerifier interface {
	Verify(token string, now time.Time) (*auth.Claims, error)
}

// Auth 校验 Bearer token，并把 openid 注入 context。
func Auth(v TokenVerifier, admins AdminChecker) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authSkipped(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			token, ok := bearerToken(r)
			if !ok {
				Fail(w, r, errs.New(errs.CodeUnauthorized, "请先登录"))
				return
			}
			c, err := v.Verify(token, time.Now())
			if err != nil {
				Fail(w, r, err)
				return
			}
			// 校验通过后再确认 openid 仍在白名单里，便于随时踢人。
			if !admins.IsAdmin(c.OpenID) {
				Fail(w, r, errs.New(errs.CodeForbidden, "无权限"))
				return
			}

			next.ServeHTTP(w, r.WithContext(withOpenID(r.Context(), c.OpenID)))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}
