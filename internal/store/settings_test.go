package store

import (
	"encoding/json"
	"math"
	"testing"
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
	if s.Cache.MaxKeys != 256 {
		t.Fatalf("MaxKeys 零值应落默认 256，实际 %d", s.Cache.MaxKeys)
	}

	vm, vmt, vo := 60, 1200, 0
	s = AppSettings{Cache: CacheSettings{MetaTTL: &vm, MetaTagsTTL: &vmt, OverviewTTL: &vo, MaxKeys: 512}}
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
	if s.Cache.MaxKeys != 512 {
		t.Fatalf("MaxKeys 512 应保留，实际 %d", s.Cache.MaxKeys)
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
