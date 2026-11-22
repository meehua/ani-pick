// Package config 负责基础参数装配：先读 .env，再用真实环境变量覆盖。
//
// 这里只保留两类参数：
//   - 引导期专用：进程怎么起、连哪个库、管理员初始密码、加密密钥；
//   - 基础设施：连接池生命周期/健康检查、HTTP 服务超时——与部署形态绑定，
//     改了要重启，env 是唯一正确归属。
//
// 业务相关的可热改参数（抓取节奏、数据源凭据、出站网络、真实 IP 识别等）
// 全部在管理后台调整并存数据库 settings 表，这里不再提供环境变量。
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 是进程启动所需的全部基础参数。
type Config struct {
	Addr          string
	DatabaseURL   string
	AdminPassword string
	Secret        string // 用于加密数据库内的敏感设置（可选）
	EnvFile       string
	Debug         bool

	// 连接池规格（默认按 1GB 内存机校准）：换机器时按内存调整。
	DbMaxConns int
	DbMinConns int
	// 连接池生命周期与健康检查（基础设施参数；默认值保持既有行为）。
	DbConnLifetime      time.Duration // 连接最长存活；默认 1h
	DbConnIdleTime      time.Duration // 空闲回收；默认 0 = 不按空闲回收（保持既有行为）
	DbHealthCheckPeriod time.Duration // 健康检查周期；默认 30s

	// HTTP 服务超时（默认值与既有写死值一致：纯暴露，便于部署时按反代形态调整）。
	HTTPReadHeaderTimeout time.Duration // 请求头读取超时（慢连接防护）
	HTTPReadTimeout       time.Duration // 请求体读取超时
	HTTPWriteTimeout      time.Duration // 响应写出超时
	HTTPIdleTimeout       time.Duration // 空闲连接超时
}

// Default 返回内置默认值（不读环境）。
func Default() Config {
	return Config{
		Addr:          ":8787",
		DatabaseURL:   "postgres://ani_pick:ani_pick@127.0.0.1:5432/ani_pick?sslmode=disable",
		AdminPassword: "anipick",

		DbMaxConns:            12,
		DbMinConns:            2,
		DbConnLifetime:        time.Hour,
		DbConnIdleTime:        0, // 既有行为：不按空闲回收
		DbHealthCheckPeriod:   30 * time.Second,
		HTTPReadHeaderTimeout: 10 * time.Second,
		HTTPReadTimeout:       30 * time.Second,
		HTTPWriteTimeout:      120 * time.Second,
		HTTPIdleTimeout:       90 * time.Second,
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

	c.DbMaxConns = envInt("ANIPICK_DB_MAX_CONNS", c.DbMaxConns)
	c.DbMinConns = envInt("ANIPICK_DB_MIN_CONNS", c.DbMinConns)

	// 基础设施 duration 参数（Go ParseDuration 格式：90s / 2m / 1h）。
	var err error
	if c.DbConnLifetime, err = envDuration("ANIPICK_DB_CONN_LIFETIME", c.DbConnLifetime); err != nil {
		return c, err
	}
	if c.DbConnIdleTime, err = envDuration("ANIPICK_DB_CONN_IDLE_TIME", c.DbConnIdleTime); err != nil {
		return c, err
	}
	if c.DbHealthCheckPeriod, err = envDuration("ANIPICK_DB_HEALTH_CHECK_PERIOD", c.DbHealthCheckPeriod); err != nil {
		return c, err
	}
	if c.HTTPReadHeaderTimeout, err = envDuration("ANIPICK_HTTP_READ_HEADER_TIMEOUT", c.HTTPReadHeaderTimeout); err != nil {
		return c, err
	}
	if c.HTTPReadTimeout, err = envDuration("ANIPICK_HTTP_READ_TIMEOUT", c.HTTPReadTimeout); err != nil {
		return c, err
	}
	if c.HTTPWriteTimeout, err = envDuration("ANIPICK_HTTP_WRITE_TIMEOUT", c.HTTPWriteTimeout); err != nil {
		return c, err
	}
	if c.HTTPIdleTimeout, err = envDuration("ANIPICK_HTTP_IDLE_TIMEOUT", c.HTTPIdleTimeout); err != nil {
		return c, err
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

// envDuration 解析 Go time.ParseDuration 格式的环境变量（90s / 2m / 1h）；
// 未设置返回默认，设置了解析失败则报错（基础设施参数写错要Fail Fast，不静默吞）。
func envDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d < 0 {
		return def, fmt.Errorf("%s: 无效时长 %q（示例 90s / 2m / 1h）", key, v)
	}
	return d, nil
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
