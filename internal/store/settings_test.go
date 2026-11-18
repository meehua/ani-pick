package store

import "testing"

// TestNormalizeCacheDefaults 缓存参数：0/缺省落默认值，越界钳位。
func TestNormalizeCacheDefaults(t *testing.T) {
	// 旧库数据无 cache 字段：零值应落默认
	var s AppSettings
	s.Normalize()
	if s.Cache.QueryTTL != 60 || s.Cache.PublicTTL != 30 {
		t.Fatalf("零值应落默认 60/30，实际 %d/%d", s.Cache.QueryTTL, s.Cache.PublicTTL)
	}

	// 合法值原样保留
	s = AppSettings{Cache: CacheSettings{QueryTTL: 300, PublicTTL: 15}}
	s.Normalize()
	if s.Cache.QueryTTL != 300 || s.Cache.PublicTTL != 15 {
		t.Fatalf("合法值应保留，实际 %d/%d", s.Cache.QueryTTL, s.Cache.PublicTTL)
	}

	// 负值与越界钳位
	s = AppSettings{Cache: CacheSettings{QueryTTL: -1, PublicTTL: 999999}}
	s.Normalize()
	if s.Cache.QueryTTL != 60 {
		t.Fatalf("负值应落默认 60，实际 %d", s.Cache.QueryTTL)
	}
	if s.Cache.PublicTTL != 86400 {
		t.Fatalf("越界上限应钳到 86400，实际 %d", s.Cache.PublicTTL)
	}

	// 下限：低于 5s 的合法正值钳到下限，5 保留
	s = AppSettings{Cache: CacheSettings{QueryTTL: 2, PublicTTL: 5}}
	s.Normalize()
	if s.Cache.QueryTTL != 5 || s.Cache.PublicTTL != 5 {
		t.Fatalf("2s 应钳到下限 5、5s 应保留，实际 %d/%d", s.Cache.QueryTTL, s.Cache.PublicTTL)
	}
}
