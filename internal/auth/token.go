// Package auth 负责自签轻量 token 的签发与校验。
//
// 单管理员场景不引入 JWT 库：
//
//	payload = base64url(JSON{"openid":"...","exp":1789234567})
//	sig     = base64url(HMAC-SHA256(payload, AUTH_SECRET))
//	token   = payload + "." + sig
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"crab-order/internal/errs"
)

// Claims 是 token 里承载的信息。
type Claims struct {
	OpenID string `json:"openid"`
	Exp    int64  `json:"exp"`
}

// Signer 签发与校验 token。
type Signer struct {
	secret []byte
	ttl    time.Duration
}

func NewSigner(secret string, ttl time.Duration) *Signer {
	return &Signer{secret: []byte(secret), ttl: ttl}
}

var enc = base64.RawURLEncoding

func (s *Signer) sign(payload string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(payload))
	return enc.EncodeToString(m.Sum(nil))
}

// Issue 签发 token，返回 token 与过期时间（Unix 秒）。
func (s *Signer) Issue(openID string, now time.Time) (string, int64, error) {
	exp := now.Add(s.ttl).Unix()
	body, err := json.Marshal(Claims{OpenID: openID, Exp: exp})
	if err != nil {
		return "", 0, errs.Internal(err)
	}
	payload := enc.EncodeToString(body)
	return payload + "." + s.sign(payload), exp, nil
}

// ErrInvalidToken token 无效或已过期，统一返回 40100，不向客户端解释具体原因。
var ErrInvalidToken = errs.New(errs.CodeUnauthorized, "登录已失效，请重新登录")

// Verify 校验 token 的签名与有效期。
func (s *Signer) Verify(token string, now time.Time) (*Claims, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || sig == "" {
		return nil, ErrInvalidToken
	}

	// 恒定时间比较，避免签名被逐字节试探出来。
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return nil, ErrInvalidToken
	}

	body, err := enc.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if c.OpenID == "" || c.Exp <= now.Unix() {
		return nil, ErrInvalidToken
	}
	return &c, nil
}
