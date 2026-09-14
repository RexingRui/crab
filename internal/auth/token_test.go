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
