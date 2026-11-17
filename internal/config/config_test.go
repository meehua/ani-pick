package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "ANIPICK_ADDR=:1111\nANIPICK_CRAWL_TYPES=1,2\nANIPICK_BGM_TOKEN=from_file\n" +
		"ANIPICK_REFRESH_TTL_DAYS=7\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANIPICK_ENV_FILE", envPath)
	t.Setenv("ANIPICK_BGM_TOKEN", "from_env")
	t.Setenv("ANIPICK_ADDR", "")
	t.Setenv("ANIPICK_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ANIPICK_CRAWL_TYPES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":1111" {
		t.Fatalf("Addr = %q, 期望 :1111", cfg.Addr)
	}
	if cfg.BGM.Token != "from_env" {
		t.Fatalf("Token = %q, 期望环境变量覆盖 .env", cfg.BGM.Token)
	}
	if len(cfg.Crawl.Types) != 2 || cfg.Crawl.Types[0] != 1 || cfg.Crawl.Types[1] != 2 {
		t.Fatalf("Types = %v", cfg.Crawl.Types)
	}
	if cfg.Crawl.RefreshTTLDays != 7 {
		t.Fatalf("RefreshTTLDays = %d", cfg.Crawl.RefreshTTLDays)
	}
	if cfg.EnvFile == "" {
		t.Fatal(".env 路径未被记录")
	}
}

func TestLoadRejectsInvalidTypes(t *testing.T) {
	t.Setenv("ANIPICK_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("ANIPICK_CRAWL_TYPES", "2,9")
	if _, err := Load(); err == nil {
		t.Fatal("期望无效类型返回错误")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ANIPICK_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	for _, k := range []string{
		"ANIPICK_ADDR", "ANIPICK_DATABASE_URL", "DATABASE_URL", "ANIPICK_CRAWL_TYPES",
		"ANIPICK_CRAWL_PAGE_SIZE", "ANIPICK_REFRESH_TTL_DAYS", "ANIPICK_AUTO_INCREMENTAL",
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
	if cfg.DatabaseURL == "" || cfg.Crawl.PageSize != 100 {
		t.Fatalf("默认值异常: %+v", cfg)
	}
}
