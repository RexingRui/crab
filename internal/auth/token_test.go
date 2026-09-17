package auth

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestIssueAndVerify(t *testing.T) {
	s := NewSigner(testSecret, 720*time.Hour)
	now := time.Unix(1_700_000_000, 0)

	token, exp, err := s.Issue("oXxxxx", now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if want := now.Add(720 * time.Hour).Unix(); exp != want {
		t.Errorf("exp = %d, want %d", exp, want)
	}

	c, err := s.Verify(token, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.OpenID != "oXxxxx" {
		t.Errorf("openid = %q, want oXxxxx", c.OpenID)
	}
}

func TestVerifyExpired(t *testing.T) {
	s := NewSigner(testSecret, time.Hour)
	now := time.Unix(1_700_000_000, 0)

	token, _, err := s.Issue("oXxxxx", now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := s.Verify(token, now.Add(time.Hour+time.Second)); err == nil {
		t.Fatal("过期 token 应当校验失败")
	}
	// 恰好到期也算过期
	if _, err := s.Verify(token, now.Add(time.Hour)); err == nil {
		t.Fatal("到期时刻的 token 应当校验失败")
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	s := NewSigner(testSecret, time.Hour)
	now := time.Unix(1_700_000_000, 0)
	token, _, _ := s.Issue("oXxxxx", now)

	payload, sig, _ := strings.Cut(token, ".")

	// 改签名
	bad := []byte(sig)
	if bad[0] == 'A' {
		bad[0] = 'B'
	} else {
		bad[0] = 'A'
	}
	if _, err := s.Verify(payload+"."+string(bad), now); err == nil {
		t.Error("签名被篡改后应当校验失败")
	}

	// 改 payload（想把自己改成别人）
	forged, _, _ := s.Issue("oOther", now)
	forgedPayload, _, _ := strings.Cut(forged, ".")
	if _, err := s.Verify(forgedPayload+"."+sig, now); err == nil {
		t.Error("payload 被替换后应当校验失败")
	}
}

func TestVerifyMalformed(t *testing.T) {
	s := NewSigner(testSecret, time.Hour)
	now := time.Unix(1_700_000_000, 0)

	for _, token := range []string{"", ".", "abc", "abc.", ".abc", "a.b.c", "!!!.???"} {
		if _, err := s.Verify(token, now); err == nil {
			t.Errorf("畸形 token %q 应当校验失败", token)
		}
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token, _, _ := NewSigner(testSecret, time.Hour).Issue("oXxxxx", now)

	other := NewSigner("ffffffffffffffffffffffffffffffff", time.Hour)
	if _, err := other.Verify(token, now); err == nil {
		t.Error("换密钥后应当校验失败")
	}
}

// ---------- 登记链接 token ----------

// TestRegSignerIssueVerify 签发的登记链接能验过，jti 每条都不一样。
func TestRegSignerIssueVerify(t *testing.T) {
	s := NewRegSigner(testSecret, time.Hour)
	now := time.Now()

	tk, claims, err := s.Issue("oSeller", "老张", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := s.Verify(tk, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.JTI != claims.JTI || got.Issuer != "oSeller" || got.Remark != "老张" {
		t.Fatalf("claims 对不上: %+v", got)
	}

	// jti 是一次性标识，两条链接不能撞。撞了就等于两个买家共用一个幂等键。
	_, other, err := s.Issue("oSeller", "", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if other.JTI == claims.JTI {
		t.Fatal("两条链接的 jti 重复了")
	}
}

// TestRegSignerExpired 过期的链接验不过。
func TestRegSignerExpired(t *testing.T) {
	s := NewRegSigner(testSecret, time.Hour)
	now := time.Now()
	tk, _, err := s.Issue("oSeller", "", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := s.Verify(tk, now.Add(2*time.Hour)); err == nil {
		t.Fatal("过期的链接被放行了")
	}
}

// TestRegSignerTampered 改一个字节就验不过。
func TestRegSignerTampered(t *testing.T) {
	s := NewRegSigner(testSecret, time.Hour)
	now := time.Now()
	tk, _, err := s.Issue("oSeller", "", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	for _, bad := range []string{tk + "x", strings.Replace(tk, ".", ".x", 1), "", "abc"} {
		if _, err := s.Verify(bad, now); err == nil {
			t.Fatalf("被改过的 token 验过了: %q", bad)
		}
	}
}

// TestTokenDomainsAreSeparate 两种 token 互相当不了对方用。
// 签名前给登记链接加了 "reg." 域前缀，就是为了这个：
// 卖家的登录 token 一旦泄露，也不该顺手变成一条能建单的登记链接。
func TestTokenDomainsAreSeparate(t *testing.T) {
	now := time.Now()
	login := NewSigner(testSecret, time.Hour)
	reg := NewRegSigner(testSecret, time.Hour)

	loginToken, _, err := login.Issue("oSeller", now)
	if err != nil {
		t.Fatalf("issue login: %v", err)
	}
	if _, err := reg.Verify(loginToken, now); err == nil {
		t.Fatal("登录 token 被当成登记链接放行了")
	}

	regToken, _, err := reg.Issue("oSeller", "", now)
	if err != nil {
		t.Fatalf("issue reg: %v", err)
	}
	if _, err := login.Verify(regToken, now); err == nil {
		t.Fatal("登记链接被当成登录 token 放行了")
	}
}
