// Package config 负责基础参数装配：先读 .env，再用真实环境变量覆盖。
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config 是进程启动所需的全部基础参数。
// 与业务相关的可热改参数（抓取节奏、凭据等）保存在数据库 settings 表里，
// 这里只提供首次启动的种子值与环境级覆盖。
type Config struct {
	Addr          string
	DatabaseURL   string
	AdminPassword string
	Secret        string // 用于加密数据库内的敏感设置（可选）
	BGM           BGM
	Crawl         CrawlDefaults
	Net           Net
	EnvFile       string
	Debug         bool
}

// Net 控制服务端出站网络（仅抓取使用）：DoH 解析与 ECH。
type Net struct {
	DoH          bool
	ECH          bool
	DoHEndpoints []string
	// EgressProxy 仅用于服务端抓取（http/https/socks5），用户请求永不经过服务端。
	EgressProxy string
}

type BGM struct {
	Token  string
	Cookie string
	UA     string
}

// CrawlDefaults 是写入数据库的初始抓取配置。
type CrawlDefaults struct {
	Types             []int
	PageSize          int
	DelayMS           int
	RequestTimeoutSec int
	RefreshTTLDays    int
	RefreshBatch      int
	IncrementalPages  int
	IncrementalMin    int
	AutoIncremental   bool
	AutoRefresh       bool
	PruneOnFull       bool
}

// Default 返回内置默认值（不读环境）。
func Default() Config {
	return Config{
		Addr:          ":8787",
		DatabaseURL:   "postgres://ani_pick:ani_pick@127.0.0.1:5432/ani_pick?sslmode=disable",
		AdminPassword: "anipick",
		BGM: BGM{
			UA: "AniPick/1.0 (+https://github.com/bangumi; local indexer)",
		},
		Net: Net{
			DoH:          true,
			ECH:          true,
			DoHEndpoints: []string{"https://dns.alidns.com/resolve", "https://1.1.1.1/dns-query"},
		},
		Crawl: CrawlDefaults{
			Types:             []int{1, 2, 3, 4, 6},
			PageSize:          100,
			DelayMS:           350,
			RequestTimeoutSec: 40,
			RefreshTTLDays:    30,
			RefreshBatch:      300,
			IncrementalPages:  10,
			IncrementalMin:    360,
			AutoIncremental:   true,
			AutoRefresh:       true,
			PruneOnFull:       true,
		},
	}
}

// Load 读取 .env（若存在）后合并环境变量。
func Load() (Config, error) {
	c := Default()

	envFile := os.Getenv("ANIPICK_ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	envFile, _ = filepath.Abs(envFile)
	if _, err := os.Stat(envFile); err == nil {
		if err := loadDotEnv(envFile); err != nil {
			return c, fmt.Errorf("读取 %s: %w", envFile, err)
		}
		c.EnvFile = envFile
	}

	c.Addr = env("ANIPICK_ADDR", c.Addr)
	c.DatabaseURL = env("ANIPICK_DATABASE_URL", env("DATABASE_URL", c.DatabaseURL))
	c.AdminPassword = env("ANIPICK_ADMIN_PASSWORD", c.AdminPassword)
	c.Secret = env("ANIPICK_SECRET", c.Secret)
	c.Debug = envBool("ANIPICK_DEBUG", c.Debug)

	c.BGM.Token = env("ANIPICK_BGM_TOKEN", c.BGM.Token)
	c.BGM.Cookie = env("ANIPICK_BGM_COOKIE", c.BGM.Cookie)
	c.BGM.UA = env("ANIPICK_BGM_UA", c.BGM.UA)

	if v := os.Getenv("ANIPICK_CRAWL_TYPES"); v != "" {
		types, err := parseTypes(v)
		if err != nil {
			return c, fmt.Errorf("ANIPICK_CRAWL_TYPES: %w", err)
		}
		c.Crawl.Types = types
	}
	c.Crawl.PageSize = envInt("ANIPICK_CRAWL_PAGE_SIZE", c.Crawl.PageSize)
	c.Crawl.DelayMS = envInt("ANIPICK_CRAWL_DELAY_MS", c.Crawl.DelayMS)
	c.Crawl.RequestTimeoutSec = envInt("ANIPICK_BGM_TIMEOUT_SEC", c.Crawl.RequestTimeoutSec)
	c.Crawl.RefreshTTLDays = envInt("ANIPICK_REFRESH_TTL_DAYS", c.Crawl.RefreshTTLDays)
	c.Crawl.RefreshBatch = envInt("ANIPICK_REFRESH_BATCH", c.Crawl.RefreshBatch)
	c.Crawl.IncrementalPages = envInt("ANIPICK_INCREMENTAL_PAGES", c.Crawl.IncrementalPages)
	c.Crawl.IncrementalMin = envInt("ANIPICK_INCREMENTAL_INTERVAL_MIN", c.Crawl.IncrementalMin)
	c.Crawl.AutoIncremental = envBool("ANIPICK_AUTO_INCREMENTAL", c.Crawl.AutoIncremental)
	c.Crawl.AutoRefresh = envBool("ANIPICK_AUTO_REFRESH", c.Crawl.AutoRefresh)
	c.Crawl.PruneOnFull = envBool("ANIPICK_PRUNE_ON_FULL", c.Crawl.PruneOnFull)

	c.Net.EgressProxy = env("ANIPICK_EGRESS_PROXY", c.Net.EgressProxy)
	c.Net.DoH = envBool("ANIPICK_DOH", c.Net.DoH)
	c.Net.ECH = envBool("ANIPICK_ECH", c.Net.ECH)
	if v := env("ANIPICK_DOH_ENDPOINTS", ""); v != "" {
		c.Net.DoHEndpoints = splitList(v)
	}

	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseTypes(s string) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || !ValidType(n) {
			return nil, fmt.Errorf("无效类型 %q（可选 1 书籍 / 2 动画 / 3 音乐 / 4 游戏 / 6 三次元）", part)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("至少需要一个类型")
	}
	return out, nil
}

// ValidType 判断是否为 Bangumi 条目类型。
func ValidType(t int) bool {
	switch t {
	case 1, 2, 3, 4, 6:
		return true
	}
	return false
}

// loadDotEnv 解析 KEY=VALUE 形式的 .env；已存在的真实环境变量优先。
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		// 空值视为未设置，允许 .env 提供默认；非空环境变量优先。
		if existing, exists := os.LookupEnv(key); exists && strings.TrimSpace(existing) != "" {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return sc.Err()
}
