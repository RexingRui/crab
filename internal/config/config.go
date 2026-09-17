// Package config 负责环境变量的加载与校验。
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string

	DBPath string

	WechatAppID  string
	WechatSecret string

	AuthSecret   string
	AuthTokenTTL time.Duration
	AdminOpenIDs []string

	// RegLinkTTL 买家登记链接的有效期。链接是无状态 token，发出去撤不回，
	// 只能靠有效期兜底，别设太长。
	RegLinkTTL time.Duration

	Timezone             string
	PublicRateLimit      int
	PublicWriteRateLimit int
}

// IsDev 仅在开发环境开启 CORS 等便利功能。
func (c *Config) IsDev() bool { return c.Env == EnvDev }

// BootstrapMode 为引导模式：ADMIN_OPENIDS 为空时 /api/login 不校验白名单，
// 直接把 openid 返回，方便卖家第一次部署时把自己的 openid 填进配置。
func (c *Config) BootstrapMode() bool { return len(c.AdminOpenIDs) == 0 }

// IsAdmin 判断 openid 是否在管理员白名单中。引导模式下一律放行。
func (c *Config) IsAdmin(openID string) bool {
	if c.BootstrapMode() {
		return true
	}
	for _, id := range c.AdminOpenIDs {
		if id == openID {
			return true
		}
	}
	return false
}

// Load 读取 .env（如果存在）与环境变量并校验。环境变量优先级高于 .env 文件。
func Load(envFile string) (*Config, error) {
	if err := loadDotEnv(envFile); err != nil {
		return nil, err
	}

	c := &Config{
		Env:                  getEnv("ENV", EnvProd),
		HTTPAddr:             getEnv("HTTP_ADDR", ":8080"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		DBPath:               getEnv("DB_PATH", "./data/crab.db"),
		WechatAppID:          getEnv("WECHAT_APPID", ""),
		WechatSecret:         getEnv("WECHAT_SECRET", ""),
		AuthSecret:           getEnv("AUTH_SECRET", ""),
		AdminOpenIDs:         splitAndTrim(getEnv("ADMIN_OPENIDS", "")),
		Timezone:             getEnv("TIMEZONE", "Asia/Shanghai"),
		PublicRateLimit:      getEnvInt("PUBLIC_RATE_LIMIT", 20),
		PublicWriteRateLimit: getEnvInt("PUBLIC_WRITE_RATE_LIMIT", 5),
	}

	ttl, err := time.ParseDuration(getEnv("AUTH_TOKEN_TTL", "720h"))
	if err != nil {
		return nil, fmt.Errorf("AUTH_TOKEN_TTL 格式非法（示例 720h）: %w", err)
	}
	c.AuthTokenTTL = ttl

	regTTL, err := time.ParseDuration(getEnv("REG_LINK_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("REG_LINK_TTL 格式非法（示例 168h）: %w", err)
	}
	c.RegLinkTTL = regTTL

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	// 绝不允许使用默认密钥：密钥泄露等于任何人都能冒充卖家。
	if len(c.AuthSecret) < 32 {
		return errors.New("AUTH_SECRET 必须配置，且至少 32 个字符")
	}
	if c.Env != EnvDev && c.Env != EnvProd {
		return fmt.Errorf("ENV 只能是 %s 或 %s", EnvDev, EnvProd)
	}
	if c.Env == EnvProd && c.WechatSecret == "" {
		return errors.New("ENV=prod 时必须配置 WECHAT_SECRET")
	}
	if c.PublicRateLimit <= 0 {
		return errors.New("PUBLIC_RATE_LIMIT 必须大于 0")
	}
	if c.PublicWriteRateLimit <= 0 {
		return errors.New("PUBLIC_WRITE_RATE_LIMIT 必须大于 0")
	}
	if c.RegLinkTTL <= 0 {
		return errors.New("REG_LINK_TTL 必须大于 0")
	}
	if c.DBPath == "" {
		return errors.New("DB_PATH 不能为空")
	}
	return nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// loadDotEnv 解析简易 .env 文件；文件不存在不是错误。
//
// 进程启动时就存在的环境变量不会被覆盖，方便 systemd / docker 注入生产配置。
// 文件内部重复的键以**后出现的为准**：照文档 `cp .env.example .env` 再往末尾
// 追加一行 AUTH_SECRET=xxx，就该盖掉模板里那行空值。
func loadDotEnv(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	// 只有「进入本函数之前就存在」的变量才压过文件。若直接拿 os.LookupEnv 判断，
	// 本函数自己 Setenv 写进去的值会把文件里后出现的同名键挡掉。
	preset := make(map[string]struct{})
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok {
			preset[k] = struct{}{}
		}
	}

	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return fmt.Errorf("%s 第 %d 行格式非法，应为 KEY=VALUE", path, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// 去掉可能存在的成对引号
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if _, exists := preset[key]; exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	return sc.Err()
}
