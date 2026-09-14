// Package wechat 封装微信小程序开放接口，本期只用到 code2session。
package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultEndpoint 见微信官方文档「auth.code2Session」。
const DefaultEndpoint = "https://api.weixin.qq.com/sns/jscode2session"

type Client struct {
	AppID    string
	Secret   string
	Endpoint string
	HTTP     *http.Client
}

func NewClient(appID, secret string) *Client {
	return &Client{
		AppID:    appID,
		Secret:   secret,
		Endpoint: DefaultEndpoint,
		HTTP:     &http.Client{Timeout: 5 * time.Second},
	}
}

// Session 是 code2session 的结果。
type Session struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// Code2Session 用小程序 wx.login() 拿到的 code 换取 openid。
func (c *Client) Code2Session(ctx context.Context, code string) (*Session, error) {
	q := url.Values{}
	q.Set("appid", c.AppID)
	q.Set("secret", c.Secret)
	q.Set("js_code", code)
	q.Set("grant_type", "authorization_code")

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build code2session request: %w", err)
	}

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call code2session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("code2session http %d", resp.StatusCode)
	}

	var s Session
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("decode code2session response: %w", err)
	}
	if s.ErrCode != 0 {
		return nil, fmt.Errorf("code2session errcode=%d errmsg=%s", s.ErrCode, s.ErrMsg)
	}
	if s.OpenID == "" {
		return nil, fmt.Errorf("code2session 未返回 openid")
	}
	return &s, nil
}
