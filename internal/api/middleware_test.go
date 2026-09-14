package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAccessLogRecordsOpenID 访问日志必须记下操作人。
// AccessLog 在链上位于 Auth 之外，早期版本因此永远记到空 openid。
func TestAccessLogRecordsOpenID(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(old) })

	e := newTestEnv(t)
	e.mustOK(t, http.MethodGet, "/api/orders", nil)

	var found bool
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec struct {
			Msg       string `json:"msg"`
			Path      string `json:"path"`
			OpenID    string `json:"openid"`
			Status    int    `json:"status"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Msg != "access" || rec.Path != "/api/orders" {
			continue
		}
		found = true
		if rec.OpenID != testAdminID {
			t.Errorf("访问日志的 openid = %q, want %q", rec.OpenID, testAdminID)
		}
		if rec.Status != http.StatusOK || rec.RequestID == "" {
			t.Errorf("访问日志字段不全: %+v", rec)
		}
	}
	if !found {
		t.Fatalf("没有找到 /api/orders 的访问日志，日志内容：%s", buf.String())
	}
}

// TestOpenIDReachesHandler 操作人要能落到 order_logs 里。
func TestOpenIDReachesHandler(t *testing.T) {
	e := newTestEnv(t)
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", sampleOrderBody()))

	if len(o.Logs) == 0 {
		t.Fatal("建单应写一条日志")
	}
	if o.Logs[0].Operator != testAdminID {
		t.Errorf("日志 operator = %q, want %q", o.Logs[0].Operator, testAdminID)
	}
}

func TestRateLimiterWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := NewRateLimiter(3)
	rl.now = func() time.Time { return now }
	t.Cleanup(rl.Close)

	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("第 %d 次请求不该被限流", i+1)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Error("超出配额应被限流")
	}
	// 换个 IP 不受影响
	if !rl.Allow("5.6.7.8") {
		t.Error("不同 IP 之间不应互相影响")
	}
	// 进入下一分钟窗口后恢复
	now = now.Add(time.Minute)
	if !rl.Allow("1.2.3.4") {
		t.Error("新的分钟窗口应恢复配额")
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		header map[string]string
		remote string
		want   string
	}{
		{"直连", nil, "10.0.0.5:54321", "10.0.0.5"},
		{"反代多跳取第一跳", map[string]string{"X-Forwarded-For": "1.2.3.4, 10.0.0.1"}, "10.0.0.1:1", "1.2.3.4"},
		{"反代单跳", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "10.0.0.1:1", "1.2.3.4"},
		{"X-Real-Ip", map[string]string{"X-Real-Ip": "9.9.9.9"}, "10.0.0.1:1", "9.9.9.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.remote
			for k, v := range c.header {
				r.Header.Set(k, v)
			}
			if got := ClientIP(r); got != c.want {
				t.Errorf("ClientIP() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRecoverHidesStack panic 只写日志，不把堆栈返回给客户端。
func TestRecoverHidesStack(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom: secret detail")
	}), Recover, RequestID)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/orders", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":50000`)) {
		t.Errorf("响应体应为 50000：%s", body)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret detail")) ||
		bytes.Contains(rec.Body.Bytes(), []byte("goroutine")) {
		t.Errorf("panic 细节与堆栈不得返回给客户端：%s", body)
	}
	if !bytes.Contains(buf.Bytes(), []byte("secret detail")) {
		t.Error("panic 细节应当写进日志")
	}
}

func TestMaskAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"江苏省苏州市工业园区xx路88号3栋201", "江苏省苏州市工业园区"},
		{"北京市朝阳区建国门外大街1号", "北京市朝阳区"},
		{"上海市浦东新区张江路1号", "上海市浦东新区"},
		{"安徽省xx县城关镇某某村12组", "安徽省xx县"},
		{"苏州市相城区", "苏州市相城区"},
		// 「区」出现在哪里就切到哪里，哪怕它属于详址
		{"没有行政区划字样的地址", "没有行政区"},
		// 完全没有行政区划字样时，退回截取前 10 个字
		{"海外地址一二三四五六七八九十", "海外地址一二三四五六"},
	}
	for _, c := range cases {
		if got := MaskAddress(c.in); got != c.want {
			t.Errorf("MaskAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaskNameAndPhone(t *testing.T) {
	if got := MaskName("张三"); got != "张*" {
		t.Errorf("MaskName(张三) = %q", got)
	}
	if got := MaskName("欧阳修远"); got != "欧***" {
		t.Errorf("MaskName(欧阳修远) = %q", got)
	}
	if got := MaskName("张"); got != "张" {
		t.Errorf("单字姓名不打码，实际 %q", got)
	}
	if got := MaskPhone("13800138000"); got != "138****8000" {
		t.Errorf("MaskPhone = %q", got)
	}
	if got := MaskPhone("123"); got != "****" {
		t.Errorf("非 11 位应整体打码，实际 %q", got)
	}
}
