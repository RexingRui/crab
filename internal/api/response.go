package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"crab-order/internal/errs"
)

// Response 是全局统一响应结构。
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

// PageData 是列表类接口固定的 data 结构。
type PageData struct {
	List     any `json:"list"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// httpStatusOf 把业务码映射到 HTTP 状态码。
func httpStatusOf(code int) int {
	switch code {
	case errs.CodeOK:
		return http.StatusOK
	case errs.CodeInvalidParam:
		return http.StatusBadRequest
	case errs.CodeUnauthorized:
		return http.StatusUnauthorized
	case errs.CodeForbidden:
		return http.StatusForbidden
	case errs.CodeNotFound:
		return http.StatusNotFound
	case errs.CodeIdempotent, errs.CodeStateConflict, errs.CodeVersionConflict:
		return http.StatusConflict
	case errs.CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, body Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write response failed", "err", err)
	}
}

// OK 返回成功响应。
func OK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, Response{Code: errs.CodeOK, Msg: "ok", Data: data})
}

// Fail 返回失败响应。
// 对外只给简洁的业务提示，内部原因（SQL、堆栈）只写日志。
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *errs.Error
	if !errors.As(err, &e) {
		e = errs.Internal(err)
	}
	if e.Code == errs.CodeInternal {
		slog.ErrorContext(r.Context(), "request failed",
			"path", r.URL.Path, "request_id", RequestIDFrom(r.Context()), "err", err)
	} else {
		slog.DebugContext(r.Context(), "request rejected",
			"path", r.URL.Path, "code", e.Code, "msg", e.Msg, "err", err)
	}
	writeJSON(w, httpStatusOf(e.Code), Response{Code: e.Code, Msg: e.Msg, Data: nil})
}

// decodeJSON 解析请求体。请求体为空时不报错，由各接口自己的必填校验兜底。
func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, http.ErrBodyNotAllowed) {
			return nil
		}
		if err.Error() == "EOF" {
			return nil
		}
		return errs.InvalidParam("请求体不是合法 JSON")
	}
	return nil
}
