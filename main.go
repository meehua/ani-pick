// AniPick：基于本站 Bangumi 索引的条目检索与发现工具。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"anipick/internal/config"
	"anipick/internal/crawler"
	"anipick/internal/db"
	"anipick/internal/registry"
	"anipick/internal/secretbox"
	"anipick/internal/server"
	"anipick/internal/sources"
	"anipick/internal/store"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
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

	// 启动序列（连库/迁移/初始化）用独立 context：迁移动辄十几分钟，
	// 不随停止信号取消——收到 SIGTERM 也先把迁移跑完（事务保证不脏），
	// ctx 届时已取消，启动完成即优雅关停；部署方超时强杀兜底。
	bootCtx := context.Background()

	pool, err := db.Open(bootCtx, cfg.DatabaseURL, db.PoolConfig{
		MaxConns:          int32(cfg.DbMaxConns),
		MinConns:          int32(cfg.DbMinConns),
		ConnLifetime:      cfg.DbConnLifetime,
		ConnIdleTime:      cfg.DbConnIdleTime,
		HealthCheckPeriod: cfg.DbHealthCheckPeriod,
	})
	if err != nil {
		slog.Error("数据库连接失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(bootCtx, pool); err != nil {
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
	if err := st.EnsureAdminPassword(bootCtx, cfg.AdminPassword); err != nil {
		slog.Error("初始化管理员密码失败", "err", err)
		os.Exit(1)
	}
	if err := st.MarkStaleRuns(bootCtx); err != nil {
		slog.Warn("清理中断任务失败", "err", err)
	}
	_ = st.CleanupSessions(bootCtx)

	defaults := store.AppSettings{
		// 抓取配置种子（首次启动写入数据库；运行期一律在管理后台「抓取设置」
		// 的 bgm 卡调整，不再提供环境变量）：
		Crawl: store.CrawlSettings{
			Types:             []int{1, 2, 3, 4, 6},
			PageSize:          100,
			RequestTimeoutSec: 40,
			RefreshTTLDays:    30,
			RefreshBatch:      300,
			IncrementalPages:  10,
			AutoIncremental:   true,
			AutoRefresh:       true,
			PruneOnFull:       true,
			// 自动任务调度：增量默认每 6 小时一轮，过期重爬默认每 10 分钟一轮
			//（与旧版本写死行为一致，现均为后台可调的设置项）
			IncrementalSchedule: store.AutoSchedule{Kind: "interval", EveryMin: 360},
			RefreshSchedule:     store.AutoSchedule{Kind: "interval", EveryMin: 10},
			// 任务稳健性 / 请求与重试 / 合并参数（默认推荐值，后台「抓取设置」可调）
			SchedulerTickSec:     120,
			PageFailRetries:      3,
			PageRetryWaitSec:     5,
			RefreshProgressEvery: 20,
			SchedulerLookback:    20,
			StaleRetryCap:        5,
			SourceGapMS:          2000,
			ExtTagMax:            8,
			RequestAttempts:      3,
			RequestBackoffMS:     2000,
			RetryAfterCapSec:     30,
			ResponseLimitMB:      8,
			OutboundTimeoutSec:   30,
			MatchYearWindow:      1,
			RematchBatch:         100,
		},
		BGM: store.BGMSettings{},
		// 出站网络种子（DoH 默认开启；运行期在「抓取设置 → 网络」调整，
		// 代理/DoH/ECH 变更保存后立即重建出站通道）
		Network: store.NetworkSettings{
			DoH:          true,
			ECH:          true,
			DoHEndpoints: []string{"https://dns.alidns.com/resolve", "https://1.1.1.1/dns-query"},
		},
	}
	// 每源请求间隔的种子 = 登记表默认（Sources 不设即取登记表值，下同）。
	defaults.Sources = map[string]store.SourceSetting{
		registry.SourceBGM: {Enabled: true},
	}

	// 扩展数据源适配器（收获 + 详情代理共用同一批实例；顺序即合并运行的执行顺序）。
	// 凭据（TMDB key / MAL Client ID）来自后台设置（secretbox 加密存库），经凭据
	// 快照下发；server 启动与每次保存设置都会立刷快照与出站通道。
	creds := sources.NewCredStore()
	harvesters := []sources.Harvester{
		sources.NewTMDB(creds, nil),
		sources.NewITunes(nil),
		sources.NewMAL(creds, nil),
		sources.NewAniList(nil),
	}

	cw := crawler.New(st, defaults, nil, harvesters)
	srv, err := server.New(st, cw, defaults, server.Options{
		Version:     version,
		Commit:      commit,
		Credentials: creds,
	})
	if err != nil {
		slog.Error("初始化服务失败", "err", err)
		os.Exit(1)
	}

	// HTTP 服务超时（基础设施参数，默认与既有值一致；部署形态不同可经 env 调整）
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	go cw.Scheduler(ctx)

	go func() {
		slog.Info("AniPick 已启动",
			"version", version, "commit", commit, "build_date", buildDate,
			"addr", cfg.Addr, "env", cfg.EnvFile)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("服务异常退出", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("正在关闭…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), srv.ShutdownWait())
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("关闭 HTTP 服务超时", "err", err)
	}
}
