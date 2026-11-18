package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"anipick/internal/store"
)

// TestFlightGroupDedup 并发同键回源只放一个进 fn，其余等待共享结果。
func TestFlightGroupDedup(t *testing.T) {
	var g flightGroup
	var calls atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := g.do("k", func() ([]byte, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond) // 放大竞态窗口
				return []byte("ok"), nil
			})
			if err != nil {
				t.Errorf("flight 返回错误: %v", err)
			}
			if string(body) != "ok" {
				t.Errorf("flight 返回内容不对: %q", body)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("同键回源应只执行 1 次，实际 %d 次", n)
	}
}

// TestFlightGroupError 错误透传给全部等待者，且完成后清理在飞表。
func TestFlightGroupError(t *testing.T) {
	var g flightGroup
	wantErr := errors.New("boom")
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = g.do("k", func() ([]byte, error) { return nil, wantErr })
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if !errors.Is(err, wantErr) {
			t.Errorf("等待者 %d 应拿到同一错误，实际: %v", i, err)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.calls) != 0 {
		t.Fatalf("完成后在飞表应清空，实际 %d 项", len(g.calls))
	}
}

// TestInvalidateQueryCaches 失效方法清空全部查询结果缓存。
func TestInvalidateQueryCaches(t *testing.T) {
	s, err := New(store.New(nil, nil), nil, store.AppSettings{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.pageCache["p"] = cachedJSON{body: []byte("x")}
	s.tagCache["t"] = cachedJSON{body: []byte("x")}
	s.detailCache["1"] = cachedJSON{body: []byte("x")}
	s.store.InvalidateCounts() // 空表上调用不应 panic

	s.invalidateQueryCaches()
	if len(s.pageCache) != 0 || len(s.tagCache) != 0 || len(s.detailCache) != 0 {
		t.Fatalf("失效后应全部清空: page=%d tag=%d detail=%d",
			len(s.pageCache), len(s.tagCache), len(s.detailCache))
	}
}

// TestInvalidateSubject 只清目标条目的详情缓存。
func TestInvalidateSubject(t *testing.T) {
	s, err := New(store.New(nil, nil), nil, store.AppSettings{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.detailCache["7"] = cachedJSON{body: []byte("x")}
	s.detailCache["8"] = cachedJSON{body: []byte("x")}
	s.invalidateSubject(7)
	if _, ok := s.detailCache["7"]; ok {
		t.Fatal("条目 7 的详情缓存应被清除")
	}
	if _, ok := s.detailCache["8"]; !ok {
		t.Fatal("条目 8 的详情缓存应保留")
	}
}
