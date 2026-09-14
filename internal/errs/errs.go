// Package errs 定义全局业务错误码与错误类型。
//
// 放在独立的包而不是 api 包里，是因为 service 层需要返回带业务码的错误，
// 而分层规则要求 handler → service → store 严格单向，service 不能反向依赖 api。
package errs

import "fmt"

// 业务错误码（强约定，不得改动）
const (
	CodeOK              = 0     // 成功
	CodeInvalidParam    = 40001 // 参数校验失败
	CodeUnauthorized    = 40100 // 未登录 / token 无效或过期
	CodeForbidden       = 40300 // 无权限
	CodeNotFound        = 40400 // 资源不存在
	CodeIdempotent      = 40901 // 幂等冲突
	CodeStateConflict   = 40902 // 状态流转非法
	CodeVersionConflict = 40903 // 数据已被修改（乐观锁冲突）
	CodeRateLimited     = 42900 // 请求过于频繁
	CodeInternal        = 50000 // 服务内部错误
)

// Error 带业务码的错误。Msg 是给客户端看的简洁信息，不得包含 SQL 或堆栈。
type Error struct {
	Code int
	Msg  string
	Err  error // 内部原因，只写日志，不返回给客户端
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("code=%d msg=%s cause=%v", e.Code, e.Msg, e.Err)
	}
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Msg)
}

func (e *Error) Unwrap() error { return e.Err }

// New 构造一个业务错误。
func New(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

// Newf 构造一个带格式化信息的业务错误。
func Newf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Wrap 在业务错误上附加内部原因。
func Wrap(code int, msg string, err error) *Error {
	return &Error{Code: code, Msg: msg, Err: err}
}

// 常用快捷构造

func InvalidParam(format string, args ...any) *Error {
	return Newf(CodeInvalidParam, format, args...)
}

func NotFound(what string) *Error {
	return New(CodeNotFound, what+"不存在")
}

func StateConflict(format string, args ...any) *Error {
	return Newf(CodeStateConflict, format, args...)
}

func Internal(err error) *Error {
	return Wrap(CodeInternal, "服务内部错误", err)
}

// ErrNotFound 是 store 层统一的「查无此记录」哨兵错误。
var ErrNotFound = New(CodeNotFound, "资源不存在")
