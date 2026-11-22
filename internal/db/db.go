// Package db 负责 PostgreSQL 连接与内嵌迁移。
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// PoolConfig 是连接池的启动期规格（基础设施参数，经 config 从环境读取）。
type PoolConfig struct {
	MaxConns          int32         // 最大连接数；≤0 用内置默认（按 1GB 内存机校准）
	MinConns          int32         // 最小空闲连接；≤0 用内置默认
	ConnLifetime      time.Duration // 连接最长存活；0 = 不限
	ConnIdleTime      time.Duration // 空闲回收；0 = 不按空闲回收（既有行为）
	HealthCheckPeriod time.Duration // 健康检查周期；0 用 pgx 内置默认
}

// Open 建立连接池。规格 ≤0 的项使用内置默认值（按 1GB 内存机校准），
// 可经 ANIPICK_DB_MAX_CONNS / ANIPICK_DB_MIN_CONNS 按机器规格调整。
func Open(ctx context.Context, dsn string, pc PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 ANIPICK_DATABASE_URL: %w", err)
	}
	if pc.MaxConns <= 0 {
		pc.MaxConns = 12
	}
	if pc.MinConns <= 0 {
		pc.MinConns = 2
	}
	if pc.MinConns > pc.MaxConns {
		pc.MinConns = pc.MaxConns
	}
	cfg.MaxConns = pc.MaxConns
	cfg.MinConns = pc.MinConns
	cfg.MaxConnLifetime = pc.ConnLifetime
	cfg.MaxConnIdleTime = pc.ConnIdleTime
	cfg.HealthCheckPeriod = pc.HealthCheckPeriod
	// 每条 SQL 的耗时进请求链路累积器（管理端性能面板用）
	cfg.ConnConfig.Tracer = Tracer{}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	return pool, nil
}

// Migrate 按文件名顺序执行未应用的迁移，兼容已有 schema。
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("迁移文件名缺少版本号: %s", entry.Name())
		}

		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("执行迁移 %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, version, entry.Name()); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.Info("数据库迁移完成", "file", entry.Name())
	}
	return nil
}
