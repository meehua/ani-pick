package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// 列表数据只在抓取与条目级操作后变化，失效走显式钩子，TTL 只是兜底；
// 调小更即时、调大更省 CPU。0/缺省视为默认值（兼容旧库数据无此字段）。
type CacheSettings struct {
	QueryTTL  int `json:"query_ttl"`  // 搜索页/搜索总数/标签候选/条目详情的进程内缓存 TTL（秒）
	PublicTTL int `json:"public_ttl"` // 公开 GET 响应 Cache-Control max-age（秒，浏览器与 CF 边缘）
}

type AppSettings struct {
	Crawl     CrawlSettings `json:"crawl"`
	BGM       BGMSettings   `json:"bgm"`
	Cache     CacheSettings `json:"cache"`
	UpdatedAt time.Time     `json:"updated_at"`
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
	// 缓存参数：0 = 未设置/恢复默认；下限 5s 防止缓存形同虚设，上限 1 天防陈旧失控
	if s.Cache.QueryTTL <= 0 {
		s.Cache.QueryTTL = 60
	}
	if s.Cache.PublicTTL <= 0 {
		s.Cache.PublicTTL = 30
	}
	s.Cache.QueryTTL = clamp(s.Cache.QueryTTL, 5, 86400)
	s.Cache.PublicTTL = clamp(s.Cache.PublicTTL, 5, 86400)
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
