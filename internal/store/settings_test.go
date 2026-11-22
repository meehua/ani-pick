package store

import (
	"encoding/json"
	"math"
	"testing"

	"anipick/internal/registry"
)

// cacheInt 取出指针字段的值（nil 视为 0 以便断言）。
func cacheInt(p *int) int {
	if p == nil {
		return -1 // 未设置，用 -1 与显式 0 区分
	}
	return *p
}

// TestNormalizeCacheDefaults 缓存 TTL：nil/负值落默认值，显式 0 = 关闭，正值钳下限无上限。
func TestNormalizeCacheDefaults(t *testing.T) {
	// 旧库数据无 cache 字段：nil 应落默认（不是 0=关闭！）
	var s AppSettings
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != 60 {
		t.Fatalf("nil 应落默认 60，实际 %d", v)
	}
	if v := cacheInt(s.Cache.PublicTTL); v != 30 {
		t.Fatalf("nil 应落默认 30，实际 %d", v)
	}

	// 合法值原样保留
	v300, v15 := 300, 15
	s = AppSettings{Cache: CacheSettings{QueryTTL: &v300, PublicTTL: &v15}}
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != 300 {
		t.Fatalf("300 应保留，实际 %d", v)
	}
	if v := cacheInt(s.Cache.PublicTTL); v != 15 {
		t.Fatalf("15 应保留，实际 %d", v)
	}

	// 显式 0 = 关闭该层缓存，不得被改写成默认
	v0 := 0
	s = AppSettings{Cache: CacheSettings{QueryTTL: &v0, PublicTTL: &v0}}
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != 0 {
		t.Fatalf("显式 0 应保留为 0（关闭），实际 %d", v)
	}
	if v := cacheInt(s.Cache.PublicTTL); v != 0 {
		t.Fatalf("显式 0 应保留为 0（关闭），实际 %d", v)
	}

	// 负值视为未设置 → 默认；大值不设上限原样保留
	vn, vbig := -1, 999999
	s = AppSettings{Cache: CacheSettings{QueryTTL: &vn, PublicTTL: &vbig}}
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != 60 {
		t.Fatalf("负值应落默认 60，实际 %d", v)
	}
	if v := cacheInt(s.Cache.PublicTTL); v != 999999 {
		t.Fatalf("999999 不应被钳位，实际 %d", v)
	}

	// 下限：低于 5s 的合法正值钳到下限，5 保留
	v2, v5 := 2, 5
	s = AppSettings{Cache: CacheSettings{QueryTTL: &v2, PublicTTL: &v5}}
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != 5 {
		t.Fatalf("2s 应钳到下限 5，实际 %d", v)
	}
	if v := cacheInt(s.Cache.PublicTTL); v != 5 {
		t.Fatalf("5s 应保留，实际 %d", v)
	}

	// 防溢出硬边界：天文数字钳到 MaxInt32 秒
	vmax := math.MaxInt64
	s = AppSettings{Cache: CacheSettings{QueryTTL: &vmax}}
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != math.MaxInt32 {
		t.Fatalf("溢出值应钳到 MaxInt32，实际 %d", v)
	}
}

// TestNormalizeCacheJSONRoundTrip 旧库 JSON（无 cache 字段）解出 nil → 默认；
// 显式 0 经保存/读取往返后仍为 0（关闭不被吞掉）。
func TestNormalizeCacheJSONRoundTrip(t *testing.T) {
	// 旧行：没有 cache 字段
	var s AppSettings
	if err := json.Unmarshal([]byte(`{"crawl":{},"bgm":{}}`), &s); err != nil {
		t.Fatal(err)
	}
	s.Normalize()
	if v := cacheInt(s.Cache.QueryTTL); v != 60 {
		t.Fatalf("旧库数据应落默认 60，实际 %d", v)
	}

	// 新行：显式 0 保存后再读回，仍为 0
	v0 := 0
	in := AppSettings{Cache: CacheSettings{QueryTTL: &v0, PublicTTL: &v0}}
	in.Normalize()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AppSettings
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	out.Normalize()
	if v := cacheInt(out.Cache.QueryTTL); v != 0 {
		t.Fatalf("显式 0 往返后应仍为 0（关闭），实际 %d", v)
	}
}

// TestNormalizeCacheNewFields 新增缓存字段：MaxKeys 0/缺省落默认（容量非开关），
// TTL 指针语义与 Query/Public 一致（nil→默认、0=关闭）。
func TestNormalizeCacheNewFields(t *testing.T) {
	var s AppSettings
	s.Normalize()
	if v := cacheInt(s.Cache.MetaTTL); v != 30 {
		t.Fatalf("nil MetaTTL 应落默认 30，实际 %d", v)
	}
	if v := cacheInt(s.Cache.MetaTagsTTL); v != 600 {
		t.Fatalf("nil MetaTagsTTL 应落默认 600，实际 %d", v)
	}
	if v := cacheInt(s.Cache.OverviewTTL); v != 3 {
		t.Fatalf("nil OverviewTTL 应落默认 3，实际 %d", v)
	}
	if v := cacheInt(s.Cache.ExtDetailTTL); v != 600 {
		t.Fatalf("nil ExtDetailTTL 应落默认 600，实际 %d", v)
	}
	if s.Cache.MaxKeys != 256 {
		t.Fatalf("MaxKeys 零值应落默认 256，实际 %d", s.Cache.MaxKeys)
	}

	vm, vmt, vo, ved := 60, 1200, 0, -1
	s = AppSettings{Cache: CacheSettings{MetaTTL: &vm, MetaTagsTTL: &vmt, OverviewTTL: &vo, ExtDetailTTL: &ved, MaxKeys: 512}}
	s.Normalize()
	if v := cacheInt(s.Cache.MetaTTL); v != 60 {
		t.Fatalf("60 应保留，实际 %d", v)
	}
	if v := cacheInt(s.Cache.MetaTagsTTL); v != 1200 {
		t.Fatalf("1200 应保留，实际 %d", v)
	}
	if v := cacheInt(s.Cache.OverviewTTL); v != 0 {
		t.Fatalf("显式 0 应保留为 0（关闭），实际 %d", v)
	}
	if v := cacheInt(s.Cache.ExtDetailTTL); v != 600 {
		t.Fatalf("负值应视作未设置回落默认 600，实际 %d", v)
	}
	if s.Cache.MaxKeys != 512 {
		t.Fatalf("MaxKeys 512 应保留，实际 %d", s.Cache.MaxKeys)
	}
}

// TestNormalizeSchedules 自动任务调度：旧数据升级（无 kind → interval 用旧间隔）、
// 越界钳位、增量调度向旧字段的镜像（旧二进制回滚可读）。
func TestNormalizeSchedules(t *testing.T) {
	// 旧库 JSON：没有 schedule 字段，只有旧的增量间隔字段
	var s AppSettings
	s.Crawl.IncrementalIntervalMin = 360
	s.Normalize()
	if s.Crawl.IncrementalSchedule.Kind != "interval" || s.Crawl.IncrementalSchedule.EveryMin != 360 {
		t.Fatalf("旧数据应升级为 interval/360，实际 %+v", s.Crawl.IncrementalSchedule)
	}
	if s.Crawl.RefreshSchedule.Kind != "interval" || s.Crawl.RefreshSchedule.EveryMin != 10 {
		t.Fatalf("旧数据过期重爬应升级为 interval/10（旧写死值），实际 %+v", s.Crawl.RefreshSchedule)
	}

	// 显式 interval：下限钳 1；every_days：天数与时刻钳位
	s = AppSettings{Crawl: CrawlSettings{
		IncrementalSchedule: AutoSchedule{Kind: "interval", EveryMin: 0},
		RefreshSchedule:     AutoSchedule{Kind: "every_days", AtMinute: 2000, EveryDays: 0},
	}}
	s.Normalize()
	if s.Crawl.IncrementalSchedule.EveryMin != 1 {
		t.Fatalf("interval 间隔下限应为 1，实际 %d", s.Crawl.IncrementalSchedule.EveryMin)
	}
	if s.Crawl.RefreshSchedule.AtMinute != 1439 || s.Crawl.RefreshSchedule.EveryDays != 1 {
		t.Fatalf("every_days 应钳到 1439/1，实际 %+v", s.Crawl.RefreshSchedule)
	}

	// daily 保留原值；未知 kind 回落 interval
	s = AppSettings{Crawl: CrawlSettings{
		IncrementalSchedule: AutoSchedule{Kind: "daily", AtMinute: 180},
		RefreshSchedule:     AutoSchedule{Kind: "bogus"},
	}}
	s.Normalize()
	if s.Crawl.IncrementalSchedule.Kind != "daily" || s.Crawl.IncrementalSchedule.AtMinute != 180 {
		t.Fatalf("daily 应保留，实际 %+v", s.Crawl.IncrementalSchedule)
	}
	if s.Crawl.RefreshSchedule.Kind != "interval" {
		t.Fatalf("未知 kind 应回落 interval，实际 %s", s.Crawl.RefreshSchedule.Kind)
	}

	// 增量调度镜像回旧字段（interval 时），daily 时不覆盖旧字段
	s = AppSettings{Crawl: CrawlSettings{IncrementalSchedule: AutoSchedule{Kind: "interval", EveryMin: 90}}}
	s.Normalize()
	if s.Crawl.IncrementalIntervalMin != 90 {
		t.Fatalf("interval 调度应镜像到旧字段 90，实际 %d", s.Crawl.IncrementalIntervalMin)
	}
}

// TestNormalizeTaskParams 任务稳健性/请求重试/服务器与面板参数：0/未填 = 默认推荐值，
// 越界钳位；每源预算覆写按登记表硬顶钳位、地区只收两位代码。
func TestNormalizeTaskParams(t *testing.T) {
	var s AppSettings
	s.Normalize()
	c := s.Crawl
	if c.SchedulerTickSec != 120 || c.PageFailRetries != 3 || c.PageRetryWaitSec != 5 ||
		c.RefreshProgressEvery != 20 || c.SchedulerLookback != 20 || c.StaleRetryCap != 5 ||
		c.SourceGapMS != 2000 || c.ExtTagMax != 8 || c.RequestAttempts != 3 ||
		c.RequestBackoffMS != 2000 || c.RetryAfterCapSec != 30 || c.ResponseLimitMB != 8 ||
		c.OutboundTimeoutSec != 30 || c.MatchYearWindow != 1 || c.RematchBatch != 100 {
		t.Fatalf("任务参数缺省落推荐值: %+v", c)
	}
	sv := s.Server
	if sv.SessionCacheTTLSec != 60 || sv.SessionSeenGapSec != 600 || sv.SessionCacheMax != 512 ||
		sv.MinCompressBytes != 860 || sv.SnapshotTTLSec != 60 || sv.PerfSampleCap != 256 ||
		sv.RecentReqCap != 256 || sv.OverviewRuns != 24 || sv.ShutdownWaitSec != 10 ||
		sv.AdminPollSec != 4 || sv.PageCacheMax != 12 {
		t.Fatalf("服务器与面板参数缺省落推荐值: %+v", sv)
	}
	nw := s.Network
	if nw.DoHTimeoutSec != 6 || nw.DnsCacheMinSec != 60 || nw.DnsCacheTTLSec != 300 || nw.OutboundUA != "" {
		t.Fatalf("网络参数缺省落推荐值: %+v", nw)
	}
	if s.Cache.MetaTagsMaxKeys != 64 {
		t.Fatalf("元标签缓存键上限缺省 64: %d", s.Cache.MetaTagsMaxKeys)
	}

	// 越界钳位 + 每源预算与地区校验
	s = AppSettings{
		Crawl: CrawlSettings{
			SchedulerTickSec: 9999, RequestAttempts: 99, MatchYearWindow: 9, ExtTagMax: -3,
		},
		Server: ServerSettings{AdminPollSec: 0, PageCacheMax: 999},
		Network: NetworkSettings{
			DoHTimeoutSec: 999, DnsCacheMinSec: 5, DnsCacheTTLSec: 999999, OutboundUA: "  ",
		},
		Sources: map[string]SourceSetting{
			// anilist 硬顶 50 / itunes 默认地区 tw
			"anilist": {PageSize: 999},
			"itunes":  {Region: "CN", FullPages: 999999},
		},
	}
	s.Normalize()
	if c := s.Crawl; c.SchedulerTickSec != 600 || c.RequestAttempts != 10 || c.MatchYearWindow != 2 || c.ExtTagMax != 8 {
		t.Fatalf("越界钳位不符: %+v", c)
	}
	if sv := s.Server; sv.AdminPollSec != 4 || sv.PageCacheMax != 50 {
		t.Fatalf("服务器面板钳位不符: %+v", sv)
	}
	if nw := s.Network; nw.DoHTimeoutSec != 30 || nw.DnsCacheMinSec != 10 || nw.DnsCacheTTLSec != 86400 {
		t.Fatalf("网络钳位不符: %+v", nw)
	}
	if v := s.Sources["anilist"]; v.PageSize != 50 {
		t.Fatalf("单页条数应钳到上游硬顶 50: %d", v.PageSize)
	}
	if v := s.Sources["itunes"]; v.Region != "cn" || v.FullPages != 5000 {
		t.Fatalf("地区应小写化、页数应钳顶: %+v", v)
	}
}

// TestNormalizeMatchGroups 匹配组：nil 落登记表推荐默认（按可配对 kind 分组）；
// 非法成员（未登记来源/未声明类型）丢弃；组内去重；单成员组淘汰；多对多保留。
func TestNormalizeMatchGroups(t *testing.T) {
	var s AppSettings
	s.Normalize()
	if len(s.Crawl.MatchGroups) == 0 {
		t.Fatal("nil 匹配组应落登记表推荐默认")
	}
	// 推荐默认：动画组必含 bgm/anilist/mal 三家动画
	found := map[registry.TypeRef]bool{}
	for _, g := range s.Crawl.MatchGroups {
		for _, m := range g {
			found[m] = true
		}
	}
	for _, want := range []registry.TypeRef{
		{Source: "bgm", Type: 2}, {Source: "anilist", Type: 2}, {Source: "mal", Type: 2},
	} {
		if !found[want] {
			t.Fatalf("默认动画组缺成员 %v", want)
		}
	}

	// 多对多 + 非法成员清理：tmdb 剧集(102) 同时进剧集组与动画组；垃圾成员剔除
	s = AppSettings{Crawl: CrawlSettings{MatchGroups: [][]registry.TypeRef{
		{{Source: "bgm", Type: 6}, {Source: "tmdb", Type: 102}},
		{{Source: "bgm", Type: 2}, {Source: "tmdb", Type: 102}, {Source: "tmdb", Type: 102}, {Source: "nope", Type: 1}, {Source: "tmdb", Type: 999}},
	}}}
	s.Normalize()
	if len(s.Crawl.MatchGroups) != 2 {
		t.Fatalf("应保留 2 组（单成员组淘汰）: %+v", s.Crawl.MatchGroups)
	}
	g2 := s.Crawl.MatchGroups[1]
	if len(g2) != 2 {
		t.Fatalf("组内应去重并剔除非法成员: %+v", g2)
	}
	if g2[0].Source != "bgm" || g2[1].Source != "tmdb" || g2[1].Type != 102 {
		t.Fatalf("多对多成员不符: %+v", g2)
	}
}

// TestNormalizeSecurity 限流与会话参数：0/缺省落默认，越界钳位（防滥用也防误配锁死）。
func TestNormalizeSecurity(t *testing.T) {
	var s AppSettings
	s.Normalize()
	sec := s.Security
	if sec.RateLimitRPS != 30 || sec.RateLimitBurst != 120 || sec.LoginRPM != 5 ||
		sec.LoginBurst != 6 || sec.LoginDelayMS != 300 || sec.SessionTTLDays != 7 ||
		sec.LimiterMaxIPs != 4096 || sec.MaxPage != 5000 {
		t.Fatalf("零值应落默认，实际 %+v", sec)
	}

	s = AppSettings{Security: SecuritySettings{
		RateLimitRPS: 50, RateLimitBurst: 200, LoginRPM: 10, LoginBurst: 10,
		LoginDelayMS: 500, SessionTTLDays: 14, LimiterMaxIPs: 8192, MaxPage: 10000}}
	s.Normalize()
	sec = s.Security
	if sec.RateLimitRPS != 50 || sec.RateLimitBurst != 200 || sec.LoginRPM != 10 ||
		sec.LoginBurst != 10 || sec.LoginDelayMS != 500 || sec.SessionTTLDays != 14 ||
		sec.LimiterMaxIPs != 8192 || sec.MaxPage != 10000 {
		t.Fatalf("合法值应保留，实际 %+v", sec)
	}

	s = AppSettings{Security: SecuritySettings{
		RateLimitRPS: 0, RateLimitBurst: 1, LoginRPM: 1000, LoginBurst: -5,
		LoginDelayMS: 999999, SessionTTLDays: 0, LimiterMaxIPs: 1, MaxPage: 999999999}}
	s.Normalize()
	sec = s.Security
	if sec.RateLimitRPS != 30 || sec.RateLimitBurst != 10 || sec.LoginRPM != 60 ||
		sec.LoginBurst != 6 || sec.LoginDelayMS != 2000 || sec.SessionTTLDays != 7 ||
		sec.LimiterMaxIPs != 256 || sec.MaxPage != 100000 {
		t.Fatalf("非正值应落默认、越界应钳位，实际 %+v", sec)
	}
}

// TestEffectiveInterval 每源请求间隔：0 = 登记表默认；正值可比默认更快，
// 但不得低于登记表按官方限速口径声明的下限；interval_ms 越界在 Normalize 钳位。
func TestEffectiveInterval(t *testing.T) {
	settings := map[string]SourceSetting{}
	// 未填 = 各源登记表默认
	for id, want := range map[string]int{
		registry.SourceBGM:     350,
		registry.SourceAniList: 1200,
		registry.SourceMAL:     750,
		registry.SourceTMDB:    250,
		registry.SourceITunes:  3000,
	} {
		if got := EffectiveInterval(id, settings); got != want {
			t.Errorf("EffectiveInterval(%s) 默认 = %d, 期望 %d", id, got, want)
		}
	}
	// 正值覆写生效（可比默认更快，比如 tmdb 调到 150）
	fast := map[string]SourceSetting{"tmdb": {IntervalMS: 150}}
	if got := EffectiveInterval(registry.SourceTMDB, fast); got != 150 {
		t.Errorf("覆写 150 应生效, 实际 %d", got)
	}
	// 低于下限按官方口径钳回：mal 下限 400、itunes 下限 = 默认 3000
	clampSet := map[string]SourceSetting{
		registry.SourceMAL:    {IntervalMS: 50},
		registry.SourceITunes: {IntervalMS: 500},
	}
	if got := EffectiveInterval(registry.SourceMAL, clampSet); got != 400 {
		t.Errorf("mal 50 应钳到下限 400, 实际 %d", got)
	}
	if got := EffectiveInterval(registry.SourceITunes, clampSet); got != 3000 {
		t.Errorf("itunes 500 应钳到下限 3000, 实际 %d", got)
	}
	// Normalize：interval_ms 上限 60s 防手滑，0 保留（= 用默认）
	s := AppSettings{Sources: map[string]SourceSetting{
		"bgm": {IntervalMS: 999999}, "tmdb": {IntervalMS: 0}}}
	s.Normalize()
	if got := s.Sources["bgm"].IntervalMS; got != 60000 {
		t.Errorf("越界间隔应钳到 60000, 实际 %d", got)
	}
	if got := s.Sources["tmdb"].IntervalMS; got != 0 {
		t.Errorf("显式 0 应保留（= 用默认）, 实际 %d", got)
	}
	if got := EffectiveInterval("unknown-source", settings); got != 0 {
		t.Errorf("未登记来源应返回 0, 实际 %d", got)
	}
}

func TestEffectiveRefreshTTL(t *testing.T) {
	const global = 30
	settings := map[string]SourceSetting{}
	// 未填覆盖 = 全局默认（保留期策略每个源都适用，不只原生索引）
	if got := EffectiveRefreshTTL(registry.SourceBGM, settings, global); got != global {
		t.Errorf("未覆盖应回落全局默认 %d, 实际 %d", global, got)
	}
	if got := EffectiveRefreshTTL(registry.SourceMAL, settings, global); got != global {
		t.Errorf("未覆盖应回落全局默认 %d, 实际 %d", global, got)
	}
	// 源覆盖生效：mal 设 7 天 → 7；bgm 未覆盖 → 仍 30
	cover := map[string]SourceSetting{
		registry.SourceMAL: {RefreshTTLDays: 7},
		registry.SourceBGM: {},
	}
	if got := EffectiveRefreshTTL(registry.SourceMAL, cover, global); got != 7 {
		t.Errorf("覆盖 7 应生效, 实际 %d", got)
	}
	if got := EffectiveRefreshTTL(registry.SourceBGM, cover, global); got != global {
		t.Errorf("bgm 覆盖 0 应回落全局默认 %d, 实际 %d", global, got)
	}
	// Normalize：源覆盖钳 0–3650，0 保留（= 用全局默认）
	s := AppSettings{Sources: map[string]SourceSetting{
		"mal": {RefreshTTLDays: 99999}, "bgm": {RefreshTTLDays: 0}}}
	s.Normalize()
	if got := s.Sources["mal"].RefreshTTLDays; got != 3650 {
		t.Errorf("越界保留期应钳到 3650, 实际 %d", got)
	}
	if got := s.Sources["bgm"].RefreshTTLDays; got != 0 {
		t.Errorf("显式 0 应保留（= 用全局默认）, 实际 %d", got)
	}
}
