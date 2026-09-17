package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"crab-order/internal/errs"
)

// 买家自助登记链接的 token。
//
// 和登录 token 同一套 HMAC 结构，共用 AUTH_SECRET，但签名前给 payload 加了 "reg." 域前缀：
// 两种 token 的签名互不通用，拿登录 token 当登记 token 用签不过，反之亦然。
//
//	payload = base64url(JSON{"jti":"...","iss":"卖家openid","exp":1789234567})
//	sig     = base64url(HMAC-SHA256("reg."+payload, AUTH_SECRET))
//	token   = payload + "." + sig
//
// token 本身是无状态的，服务端不存、也撤不回。「一个链接只能落一单」不靠服务端记账，
// 而是把 jti 当成建单的幂等键（request_id）：同一个链接第二次提交会命中 orders 上的
// 唯一索引，直接返回第一次那笔单，不会重复建单。

const regDomain = "reg."

// RegClaims 是登记链接 token 承载的信息。
type RegClaims struct {
	// JTI 一次性标识，落单时作为 request_id 的一部分。
	JTI string `json:"jti"`
	// Issuer 签发这条链接的卖家 openid，用于操作流水里记清楚是谁发出去的。
	Issuer string `json:"iss"`
	// Remark 卖家生成链接时预填的备注名（如「老张」），买家提交时原样带进订单。
	Remark string `json:"rmk,omitempty"`
	Exp    int64  `json:"exp"`
}

// RegSigner 签发与校验登记链接 token。
type RegSigner struct {
	secret []byte
	ttl    time.Duration
}

func NewRegSigner(secret string, ttl time.Duration) *RegSigner {
	return &RegSigner{secret: []byte(secret), ttl: ttl}
}

func (s *RegSigner) sign(payload string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(regDomain + payload))
	return enc.EncodeToString(m.Sum(nil))
}

// newJTI 生成 16 字节随机标识。用 crypto/rand，不能用可预测的时间戳或自增：
// 猜得到 jti 就等于能抢先把别人的链接用掉。
func newJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errs.Internal(err)
	}
	return enc.EncodeToString(b), nil
}

// Issue 签发一条登记链接 token，返回 token、claims 与过期时间（Unix 秒）。
func (s *RegSigner) Issue(issuer, remark string, now time.Time) (string, *RegClaims, error) {
	jti, err := newJTI()
	if err != nil {
		return "", nil, err
	}
	c := &RegClaims{JTI: jti, Issuer: issuer, Remark: remark, Exp: now.Add(s.ttl).Unix()}
	body, err := json.Marshal(c)
	if err != nil {
		return "", nil, errs.Internal(err)
	}
	payload := enc.EncodeToString(body)
	return payload + "." + s.sign(payload), c, nil
}

// ErrInvalidRegToken 登记链接无效或已过期。买家看得懂的话术，不解释具体原因。
var ErrInvalidRegToken = errs.New(errs.CodeUnauthorized, "登记链接无效或已过期，请找店主重新发一条")

// Verify 校验登记链接 token 的签名与有效期。
func (s *RegSigner) Verify(token string, now time.Time) (*RegClaims, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || sig == "" {
		return nil, ErrInvalidRegToken
	}
	// 恒定时间比较，避免签名被逐字节试探出来。
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return nil, ErrInvalidRegToken
	}
	body, err := enc.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidRegToken
	}
	var c RegClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, ErrInvalidRegToken
	}
	if c.JTI == "" || c.Exp <= now.Unix() {
		return nil, ErrInvalidRegToken
	}
	return &c, nil
}
