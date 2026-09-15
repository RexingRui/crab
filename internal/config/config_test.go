package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setEnv 设置一批环境变量，测试结束自动还原。
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"ENV":           "prod",
		"AUTH_SECRET":   "0123456789abcdef0123456789abcdef",
		"WECHAT_SECRET": "s3cret",
		"ADMIN_OPENIDS": "oA, oB ,",
	}
}

func TestLoadValid(t *testing.T) {
	setEnv(t, validEnv())

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.HTTPAddr != ":8080" || c.DBPath != "./data/crab.db" || c.PublicRateLimit != 20 {
		t.Errorf("默认值不对: %+v", c)
	}
	if c.AuthTokenTTL != 720*time.Hour {
		t.Errorf("AUTH_TOKEN_TTL 默认值 = %v", c.AuthTokenTTL)
	}
	if len(c.AdminOpenIDs) != 2 || c.AdminOpenIDs[0] != "oA" || c.AdminOpenIDs[1] != "oB" {
		t.Errorf("白名单解析不对: %#v", c.AdminOpenIDs)
	}
	if c.BootstrapMode() {
		t.Error("白名单非空时不应进入引导模式")
	}
	if !c.IsAdmin("oA") || c.IsAdmin("oStranger") {
		t.Error("IsAdmin 判断不对")
	}
}

// TestAuthSecretRequired 密钥缺失或过短必须直接报错，绝不允许用默认密钥跑起来。
func TestAuthSecretRequired(t *testing.T) {
	for _, secret := range []string{"", "short", "0123456789abcdef0123456789abcde"} { // 31 字符
		env := validEnv()
		env["AUTH_SECRET"] = secret
		setEnv(t, env)

		if _, err := Load(""); err == nil {
			t.Errorf("AUTH_SECRET=%q 应当启动失败", secret)
		}
	}
}

func TestProdRequiresWechatSecret(t *testing.T) {
	env := validEnv()
	env["WECHAT_SECRET"] = ""
	setEnv(t, env)

	if _, err := Load(""); err == nil {
		t.Error("ENV=prod 且 WECHAT_SECRET 为空时应当启动失败")
	}

	// dev 环境允许不配
	env["ENV"] = "dev"
	setEnv(t, env)
	if _, err := Load(""); err != nil {
		t.Errorf("dev 环境不该要求 WECHAT_SECRET: %v", err)
	}
}

func TestBootstrapMode(t *testing.T) {
	env := validEnv()
	env["ADMIN_OPENIDS"] = ""
	setEnv(t, env)

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.BootstrapMode() {
		t.Error("白名单为空时应进入引导模式")
	}
	// 引导模式下任何 openid 都放行
	if !c.IsAdmin("oAnyone") {
		t.Error("引导模式应放行任意 openid")
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# 注释行

ENV=dev
HTTP_ADDR=":9090"
AUTH_SECRET='0123456789abcdef0123456789abcdef'
export LOG_LEVEL=debug
PUBLIC_RATE_LIMIT=5
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// 已存在的环境变量优先级更高，不应被 .env 覆盖
	t.Setenv("LOG_LEVEL", "warn")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Env != "dev" || c.HTTPAddr != ":9090" || c.PublicRateLimit != 5 {
		t.Errorf(".env 解析不对: %+v", c)
	}
	if c.LogLevel != "warn" {
		t.Errorf("环境变量应优先于 .env，实际 LOG_LEVEL=%q", c.LogLevel)
	}
}

// README 与 deploy/DOCKER.md 都教人 `cp .env.example .env` 之后再
// `echo "AUTH_SECRET=$(openssl rand -hex 32)" >> .env`，而模板里本来就有一行空的
// AUTH_SECRET=。文件内重复键必须以后出现的为准，否则照文档操作起不来。
func TestLoadDotEnvLastDuplicateWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `ENV=dev
AUTH_SECRET=
WECHAT_SECRET=
LOG_LEVEL=info
AUTH_SECRET=0123456789abcdef0123456789abcdef
LOG_LEVEL=debug
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("追加在末尾的 AUTH_SECRET 应该生效: %v", err)
	}
	if c.AuthSecret != "0123456789abcdef0123456789abcdef" {
		t.Errorf("AUTH_SECRET 取值不对: %q", c.AuthSecret)
	}
	if c.LogLevel != "debug" {
		t.Errorf("重复键应以后出现的为准，实际 LOG_LEVEL=%q", c.LogLevel)
	}
}

func TestLoadDotEnvMissingFileIsOK(t *testing.T) {
	setEnv(t, validEnv())
	if _, err := Load(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Errorf(".env 不存在不该报错: %v", err)
	}
}
