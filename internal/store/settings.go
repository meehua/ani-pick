package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

type CrawlSettings struct {
	Types                  []int `json:"types"`
	PageSize               int   `json:"page_size"`
	DelayMS                int   `json:"delay_ms"`
	RequestTimeoutSec      int   `json:"request_timeout_sec"`
	RefreshTTLDays         int   `json:"refresh_ttl_days"`
	RefreshBatch           int   `json:"refresh_batch"`
	IncrementalPages       int   `json:"incremental_pages"`
	IncrementalIntervalMin int   `json:"incremental_interval_min"`
	AutoIncremental        bool  `json:"auto_incremental"`
	AutoRefresh            bool  `json:"auto_refresh"`
	PruneOnFull            bool  `json:"prune_on_full"`
}

type BGMSettings struct {
	Token  string `json:"token"`
	Cookie string `json:"cookie"`
	UA     string `json:"ua"`
}

// CacheSettings 是查询结果缓存参数（管理后台「设置 → 查询缓存」可调）。
// 列表数据只在抓取与条目级操作后变化，失效走显式钩子，TTL 只是兜底。
// TTL 字段用指针：缺省（旧库数据无此字段）→ 默认值；显式 0 → 关闭该层缓存；
// 负值视为未设置。用指针而非 int 是为了区分「没填过」和「填了 0」——否则升级后
// 旧库数据会被解读成 0，缓存全体静默关闭。MaxKeys 是容量而非开关，0 → 默认。
type CacheSettings struct {
	QueryTTL    *int `json:"query_ttl"`     // 搜索页/搜索总数/标签候选/条目详情的进程内缓存 TTL（秒）
	PublicTTL   *int `json:"public_ttl"`    // 公开 GET 响应 Cache-Control max-age（秒，浏览器与 CF 边缘）
	MetaTTL     *int `json:"meta_ttl"`      // /api/meta 首页元信息缓存 TTL（秒）
	MetaTagsTTL *int `json:"meta_tags_ttl"` // /api/meta-tags 元标签候选缓存 TTL（秒）
	OverviewTTL *int `json:"overview_ttl"`  // /api/admin/overview 聚合缓存 TTL（秒）
	MaxKeys     int  `json:"max_keys"`      // 搜索页/标签候选/详情/搜索总数缓存的键上限（内存有界）
}

// CacheSec 解析 TTL 设置指针：nil/负值视为未设置返回 def；0 = 关闭（返回 0）；
// 正值原样返回（Normalize 已钳位下限）。返回 0 时调用方应跳过对应缓存。
func CacheSec(p *int, def int) int {
	if p == nil || *p < 0 {
		return def
	}
	return *p
}

// SecuritySettings 是限流与会话安全参数（管理后台「设置 → 限流与安全」可调）。
// 限流是按 IP 计数的令牌桶（经可信反代时取真实 IP 头）：超过 burst 后按 rate
// 匀速补充。0/缺省视为默认值（兼容旧库数据无此字段）。
type SecuritySettings struct {
	RateLimitRPS   int `json:"rate_limit_rps"`   // 每 IP 常规请求速率（次/秒）
	RateLimitBurst int `json:"rate_limit_burst"` // 每 IP 常规请求突发容量
	LoginRPM       int `json:"login_rpm"`        // 登录尝试稳态速率（次/分钟/IP），防爆破
	LoginBurst     int `json:"login_burst"`      // 登录尝试突发容量
	LoginDelayMS   int `json:"login_delay_ms"`   // 密码错误的响应延迟（毫秒），拉高爆破成本
	SessionTTLDays int `json:"session_ttl_days"` // 管理员会话有效期（天），对新会话生效
	LimiterMaxIPs  int `json:"limiter_max_ips"`  // 限流表 IP 条目硬上限（内存有界）
	MaxPage        int `json:"max_page"`         // 公开搜索最大页码（深分页成本上限）
}

type AppSettings struct {
	Crawl     CrawlSettings    `json:"crawl"`
	BGM       BGMSettings      `json:"bgm"`
	Cache     CacheSettings    `json:"cache"`
	Security  SecuritySettings `json:"security"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// Normalize 裁剪到安全范围，避免管理端填入极端值。
func (s *AppSettings) Normalize() {
	c := &s.Crawl
	var types []int
	for _, t := range c.Types {
		switch t {
		case 1, 2, 3, 4, 6:
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		types = []int{1, 2, 3, 4, 6}
	}
	c.Types = types
	c.PageSize = clamp(c.PageSize, 10, 100)
	c.DelayMS = clamp(c.DelayMS, 50, 10000)
	c.RequestTimeoutSec = clamp(c.RequestTimeoutSec, 5, 120)
	c.RefreshTTLDays = clamp(c.RefreshTTLDays, 1, 3650)
	c.RefreshBatch = clamp(c.RefreshBatch, 0, 5000)
	c.IncrementalPages = clamp(c.IncrementalPages, 1, 1000)
	c.IncrementalIntervalMin = clamp(c.IncrementalIntervalMin, 5, 100000)
	// 缓存 TTL：nil/负值 = 未设置 → 默认；显式 0 = 关闭该层缓存（保留）；
	// 正值钳下限防缓存形同虚设，不设上限——失效靠显式钩子，TTL 只是兜底，
	// 站长可设任意长；上界仅防手滑与 seconds→Duration 溢出（MaxInt32 秒 ≈ 68 年）。
	normTTL := func(p **int, def, floor int) {
		if *p == nil || **p < 0 {
			v := def
			*p = &v
			return
		}
		if **p == 0 {
			return
		}
		**p = clamp(**p, floor, math.MaxInt32)
	}
	normTTL(&s.Cache.QueryTTL, 60, 5)
	normTTL(&s.Cache.PublicTTL, 30, 5)
	normTTL(&s.Cache.MetaTTL, 30, 5)
	normTTL(&s.Cache.MetaTagsTTL, 600, 10)
	normTTL(&s.Cache.OverviewTTL, 3, 1)
	if s.Cache.MaxKeys <= 0 {
		s.Cache.MaxKeys = 256
	}
	s.Cache.MaxKeys = clamp(s.Cache.MaxKeys, 16, 10000)

	// 限流与会话参数：0 = 未设置/恢复默认；钳制既防滥用也防误配把自己锁死
	if s.Security.RateLimitRPS <= 0 {
		s.Security.RateLimitRPS = 30
	}
	if s.Security.RateLimitBurst <= 0 {
		s.Security.RateLimitBurst = 120
	}
	if s.Security.LoginRPM <= 0 {
		s.Security.LoginRPM = 5
	}
	if s.Security.LoginBurst <= 0 {
		s.Security.LoginBurst = 6
	}
	if s.Security.LoginDelayMS <= 0 {
		s.Security.LoginDelayMS = 300
	}
	if s.Security.SessionTTLDays <= 0 {
		s.Security.SessionTTLDays = 7
	}
	if s.Security.LimiterMaxIPs <= 0 {
		s.Security.LimiterMaxIPs = 4096
	}
	if s.Security.MaxPage <= 0 {
		s.Security.MaxPage = 5000
	}
	s.Security.RateLimitRPS = clamp(s.Security.RateLimitRPS, 1, 1000)
	s.Security.RateLimitBurst = clamp(s.Security.RateLimitBurst, 10, 10000)
	s.Security.LoginRPM = clamp(s.Security.LoginRPM, 1, 60)
	s.Security.LoginBurst = clamp(s.Security.LoginBurst, 1, 100)
	s.Security.LoginDelayMS = clamp(s.Security.LoginDelayMS, 0, 2000)
	s.Security.SessionTTLDays = clamp(s.Security.SessionTTLDays, 1, 90)
	s.Security.LimiterMaxIPs = clamp(s.Security.LimiterMaxIPs, 256, 65536)
	s.Security.MaxPage = clamp(s.Security.MaxPage, 100, 100000)
	if s.BGM.UA == "" {
		s.BGM.UA = "AniPick/1.0 (+https://github.com/bangumi; local indexer)"
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// LoadSettings 读取 settings.app；不存在时返回 defaults。
func (s *Store) LoadSettings(ctx context.Context, defaults AppSettings) (AppSettings, error) {
	out := defaults
	out.Normalize()

	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'app'`).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("解析 settings.app: %w", err)
	}
	out.Normalize()
	return out, s.decryptSettings(&out)
}

func (s *Store) SaveSettings(ctx context.Context, in *AppSettings) error {
	in.Normalize()
	in.UpdatedAt = time.Now().UTC()
	stored := *in
	if err := s.encryptSettings(&stored); err != nil {
		return err
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ('app', $1, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, raw)
	return err
}

func (s *Store) encryptSettings(in *AppSettings) error {
	if s.Box == nil {
		return nil
	}
	var err error
	if in.BGM.Token, err = s.Box.Seal(in.BGM.Token); err != nil {
		return err
	}
	if in.BGM.Cookie, err = s.Box.Seal(in.BGM.Cookie); err != nil {
		return err
	}
	return nil
}

func (s *Store) decryptSettings(in *AppSettings) error {
	var err error
	if in.BGM.Token, err = s.Box.Open(in.BGM.Token); err != nil {
		return fmt.Errorf("BGM Token: %w", err)
	}
	if in.BGM.Cookie, err = s.Box.Open(in.BGM.Cookie); err != nil {
		return fmt.Errorf("BGM Cookie: %w", err)
	}
	return nil
}

// Mask 返回脱敏副本，用于管理端回显。
func (s *AppSettings) Mask() AppSettings {
	out := *s
	out.BGM.Token = maskSecret(out.BGM.Token)
	out.BGM.Cookie = maskSecret(out.BGM.Cookie)
	return out
}

func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return "••••"
	}
	return v[:4] + "••••" + v[len(v)-4:]
}
