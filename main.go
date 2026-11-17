// AniPick：基于本地 Bangumi 索引的条目检索与发现工具。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"anipick/internal/config"
	"anipick/internal/crawler"
	"anipick/internal/db"
	"anipick/internal/netx"
	"anipick/internal/secretbox"
	"anipick/internal/server"
	"anipick/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("数据库连接失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}

	box, err := secretbox.New(cfg.Secret)
	if err != nil {
		slog.Error("初始化加密失败", "err", err)
		os.Exit(1)
	}
	if box == nil {
		slog.Warn("未配置 ANIPICK_SECRET：数据库内的 Bangumi 凭据将以明文保存")
	}

	st := store.New(pool, box)
	if err := st.EnsureAdminPassword(ctx, cfg.AdminPassword); err != nil {
		slog.Error("初始化管理员密码失败", "err", err)
		os.Exit(1)
	}
	if err := st.MarkStaleRuns(ctx); err != nil {
		slog.Warn("清理中断任务失败", "err", err)
	}
	_ = st.CleanupSessions(ctx)

	defaults := store.AppSettings{
		Crawl: store.CrawlSettings{
			Types:                  cfg.Crawl.Types,
			PageSize:               cfg.Crawl.PageSize,
			DelayMS:                cfg.Crawl.DelayMS,
			RequestTimeoutSec:      cfg.Crawl.RequestTimeoutSec,
			RefreshTTLDays:         cfg.Crawl.RefreshTTLDays,
			RefreshBatch:           cfg.Crawl.RefreshBatch,
			IncrementalPages:       cfg.Crawl.IncrementalPages,
			IncrementalIntervalMin: cfg.Crawl.IncrementalMin,
			AutoIncremental:        cfg.Crawl.AutoIncremental,
			AutoRefresh:            cfg.Crawl.AutoRefresh,
			PruneOnFull:            cfg.Crawl.PruneOnFull,
		},
		BGM: store.BGMSettings{
			Token:  cfg.BGM.Token,
			Cookie: cfg.BGM.Cookie,
			UA:     cfg.BGM.UA,
		},
	}

	// 出站网络：DoH 解析 + 可用时 ECH；仅用于服务端抓取与管理员凭据校验。
	var resolver *netx.Resolver
	var netHTTP *http.Client
	proxyConfigured := false
	if cfg.Net.EgressProxy != "" {
		u, perr := url.Parse(cfg.Net.EgressProxy)
		if perr != nil {
			slog.Error("ANIPICK_EGRESS_PROXY 无效", "err", perr)
			os.Exit(1)
		}
		netHTTP = netx.NewProxyHTTPClient(u)
		proxyConfigured = true
		slog.Info("抓取出口使用代理（仅服务端抓取）", "scheme", u.Scheme, "host", u.Host)
	} else if cfg.Net.DoH {
		resolver = netx.NewResolver(cfg.Net.DoHEndpoints, cfg.Net.ECH)
		netHTTP = resolver.NewHTTPClient()
		slog.Info("DoH 已启用", "endpoints", cfg.Net.DoHEndpoints, "ech", cfg.Net.ECH)
	}

	cw := crawler.New(st, defaults, netHTTP)
	srv, err := server.New(st, cw, defaults, server.Options{
		HTTPClient:  netHTTP,
		Resolver:    resolver,
		EgressProxy: proxyConfigured,
	})
	if err != nil {
		slog.Error("初始化服务失败", "err", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go cw.Scheduler(ctx)

	if resolver != nil {
		go func() {
			dctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			d := resolver.Diagnose(dctx, "api.bgm.tv")
			slog.Info("出站网络诊断",
				"host", d.Host, "addrs", d.Addresses,
				"ech_advertised", d.ECHAdvertised, "err", d.Error)
		}()
	}

	go func() {
		slog.Info("AniPick 已启动", "addr", cfg.Addr, "env", cfg.EnvFile)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("服务异常退出", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("正在关闭…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("关闭 HTTP 服务超时", "err", err)
	}
}
