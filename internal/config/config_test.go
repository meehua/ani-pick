package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDotEnvAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	// 业务可热改参数（抓取节奏/凭据等）已迁管理后台，env 里不再出现；
	// 这里只用引导期/基础设施参数验证 .env 装配与真实环境变量优先。
	content := "ANIPICK_ADDR=:1111\nANIPICK_DB_CONN_LIFETIME=2h\nANIPICK_DB_MAX_CONNS=24\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANIPICK_ENV_FILE", envPath)
	t.Setenv("ANIPICK_DB_CONN_LIFETIME", "90s")
	t.Setenv("ANIPICK_ADDR", "")
	t.Setenv("ANIPICK_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":1111" {
		t.Fatalf("Addr = %q, 期望 :1111", cfg.Addr)
	}
	// 真实环境变量覆盖 .env：DB 生命周期取 env 的 90s，连接数取 .env 的 24
	if cfg.DbConnLifetime != 90*time.Second {
		t.Fatalf("DbConnLifetime = %v, 期望环境变量覆盖 .env（90s）", cfg.DbConnLifetime)
	}
	if cfg.DbMaxConns != 24 {
		t.Fatalf("DbMaxConns = %d, 期望 .env 提供的 24", cfg.DbMaxConns)
	}
	if cfg.EnvFile == "" {
		t.Fatal(".env 路径未被记录")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ANIPICK_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	for _, k := range []string{
		"ANIPICK_ADDR", "ANIPICK_DATABASE_URL", "DATABASE_URL",
		"ANIPICK_DB_MAX_CONNS", "ANIPICK_DB_CONN_LIFETIME", "ANIPICK_HTTP_WRITE_TIMEOUT",
	} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8787" {
		t.Fatalf("默认监听地址 = %q", cfg.Addr)
	}
	// 基础设施默认值与既有写死行为一致（纯暴露不改行为）
	if cfg.DatabaseURL == "" ||
		cfg.DbConnLifetime != time.Hour ||
		cfg.DbConnIdleTime != 0 ||
		cfg.DbHealthCheckPeriod != 30*time.Second ||
		cfg.HTTPReadHeaderTimeout != 10*time.Second ||
		cfg.HTTPReadTimeout != 30*time.Second ||
		cfg.HTTPWriteTimeout != 120*time.Second ||
		cfg.HTTPIdleTimeout != 90*time.Second {
		t.Fatalf("基础设施默认值异常: %+v", cfg)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("ANIPICK_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("ANIPICK_DB_HEALTH_CHECK_PERIOD", "soon")
	if _, err := Load(); err == nil {
		t.Fatal("期望无效时长返回错误（基础设施参数写错要 Fail Fast）")
	}
}
