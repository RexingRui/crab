package api

import (
	"log/slog"
	"net/http"
	"time"

	"crab-order/internal/errs"
	"crab-order/internal/timex"
)

type loginReq struct {
	Code string `json:"code"`
}

// Login POST /api/login
//
// 小程序 wx.login() 拿到 code，这里用 code2session 换 openid，
// 校验白名单后签发 token。
func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	if req.Code == "" {
		Fail(w, r, errs.InvalidParam("code 必填"))
		return
	}

	sess, err := a.wechat.Code2Session(r.Context(), req.Code)
	if err != nil {
		slog.ErrorContext(r.Context(), "code2session failed",
			"request_id", RequestIDFrom(r.Context()), "err", err)
		Fail(w, r, errs.New(errs.CodeUnauthorized, "微信登录失败，请重试"))
		return
	}

	// 引导模式：ADMIN_OPENIDS 为空时不校验白名单，把 openid 原样返回并打进日志，
	// 方便卖家第一次部署时把自己的 openid 填进配置。配置一旦非空自动关闭。
	if a.cfg.BootstrapMode() {
		slog.Warn("bootstrap mode: 请把下面的 openid 填入 ADMIN_OPENIDS 后重启服务",
			"openid", sess.OpenID)
	} else if !a.cfg.IsAdmin(sess.OpenID) {
		Fail(w, r, errs.New(errs.CodeForbidden, "无权限"))
		return
	}

	token, exp, err := a.signer.Issue(sess.OpenID, time.Now())
	if err != nil {
		Fail(w, r, err)
		return
	}

	OK(w, map[string]any{
		"token":      token,
		"expires_at": timex.Format(exp),
		"openid":     sess.OpenID,
	})
}
