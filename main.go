// CheckSpam：接收文本，调用大模型审核涉政/色情/违禁/暴恐/广告。
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"checkspam/internal/checker"
	"checkspam/internal/server"
)

func main() {
	baseURL := os.Getenv("CHECKSPAM_BASE_URL")
	if baseURL == "" {
		slog.Error("未设置环境变量 CHECKSPAM_BASE_URL（大模型 base_url）")
		os.Exit(1)
	}
	model := envOr("CHECKSPAM_MODEL", "default")
	apiKey := os.Getenv("CHECKSPAM_API_KEY")
	addr := envOr("CHECKSPAM_ADDR", ":8080")

	timeout := 60 * time.Second
	if v, err := strconv.Atoi(os.Getenv("CHECKSPAM_TIMEOUT_SEC")); err == nil && v > 0 {
		timeout = time.Duration(v) * time.Second
	}

	client := checker.New(checker.Config{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		Timeout: timeout,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:    addr,
		Handler: server.New(ctx, client),
	}

	go func() {
		<-ctx.Done()
		slog.Info("收到退出信号，开始关闭")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("优雅关闭失败", "err", err)
		}
	}()

	slog.Info("服务启动", "addr", addr, "model", model, "base_url", baseURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP 服务异常退出", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
