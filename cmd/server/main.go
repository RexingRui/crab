// Command server 是大闸蟹订单管理的后端服务。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"crab-order/internal/api"
	"crab-order/internal/auth"
	"crab-order/internal/config"
	"crab-order/internal/store"
	"crab-order/internal/timex"
	"crab-order/internal/wechat"
)

func main() {
	if err := run(); err != nil {
		slog.Error("服务启动失败", "err", err)
		os.Exit(1)
	}
}

func run() error {
	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}

	setupLogger(cfg.LogLevel)

	// 时区只加载一次，全局复用：服务器时区可能是 UTC，业务时间必须走 Asia/Shanghai。
	if err := timex.Init(cfg.Timezone); err != nil {
		slog.Warn("时区加载失败，退回固定 +08:00", "timezone", cfg.Timezone, "err", err)
	}

	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if n, err := st.SeedSpecs(ctx, timex.Now()); err != nil {
		return err
	} else if n > 0 {
		slog.Info("已写入规格价目表种子数据", "count", n)
	}

	if cfg.BootstrapMode() {
		slog.Warn("ADMIN_OPENIDS 为空，已进入引导模式：/api/login 不校验白名单，" +
			"请用小程序登录一次拿到 openid 后填入配置并重启")
	}

	a := api.New(cfg, st, auth.NewSigner(cfg.AuthSecret, cfg.AuthTokenTTL),
		wechat.NewClient(cfg.WechatAppID, cfg.WechatSecret))
	defer a.Close()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("服务已启动", "addr", cfg.HTTPAddr, "env", cfg.Env, "db", cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-stop:
		slog.Info("收到退出信号，开始优雅退出", "signal", sig.String())
	}

	// 给在途请求 10 秒排空，之后再关数据库连接（defer 负责）。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("优雅退出超时，强制关闭", "err", err)
	}
	slog.Info("服务已退出")
	return nil
}

func setupLogger(level string) {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})))
}
