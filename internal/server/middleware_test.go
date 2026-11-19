package server

import (
	"fmt"
	"testing"
	"time"
)

// TestLimiterHardCap 独特 IP 洪泛会把限流表推满，必须强制淘汰兜底，
// 条目数不允许超过注入的上限（0 取兜底默认 limiterEntriesMax）。
func TestLimiterHardCap(t *testing.T) {
	l := newLimiter()
	for i := 0; i < limiterEntriesMax+8; i++ {
		l.allow(fmt.Sprintf("10.0.0.%d", i), 1, 100, limiterEntriesMax)
	}
	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()
	if n > limiterEntriesMax {
		t.Fatalf("超上限后应强制淘汰，实际 %d > %d", n, limiterEntriesMax)
	}
}

// TestLimiterCustomCap 上限作为参数注入（来自系统设置 limiter_max_ips）。
func TestLimiterCustomCap(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 20; i++ {
		l.allow(fmt.Sprintf("10.0.2.%d", i), 1, 100, 8)
	}
	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()
	if n > 8 {
		t.Fatalf("自定义上限 8 应生效，实际 %d", n)
	}
}

// TestLimiterStaleEviction 表满触发腾位时先清 10 分钟未活跃的条目。
func TestLimiterStaleEviction(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	l.mu.Lock()
	for i := 0; i < limiterEntriesMax; i++ {
		l.entries[fmt.Sprintf("10.0.1.%d", i)] = &bucket{tokens: 1, last: now.Add(-11 * time.Minute)}
	}
	l.mu.Unlock()
	if !l.allow("new-ip", 1, 100, limiterEntriesMax) {
		t.Fatal("清完过期条目后新 IP 应放行")
	}
	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()
	if n != 1 {
		t.Fatalf("过期条目应被清空、只剩新条目，实际 %d", n)
	}
}

func TestLimiterRateLimit(t *testing.T) {
	l := newLimiter()
	passed := 0
	for i := 0; i < 10; i++ {
		if l.allow("same-ip", 0.5, 3, 0) {
			passed++
		}
	}
	if passed != 3 {
		t.Fatalf("burst=3 应恰好放行 3 次，实际 %d", passed)
	}
}
