package server

import "sync"

// flightGroup 合并并发的同键回源：缓存失效瞬间涌入的相同请求只放一个进数据库，
// 其余等待首份结果直接返回。2c1g 机器上 DB 查询是稀缺资源，重复回源是纯浪费。
// 调用方负责上下文——闭包内应使用与单个请求解耦的 ctx，避免 leader 取消连坐 follower。
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	body []byte
	err  error
}

// do 合并并发的同键回源。返回值多带 merged：本次调用是否为等待方——等待方直接
// 复用 leader 的结果，未触碰缓存与数据库，性能面板按此统计合并省下的回源次数。
func (g *flightGroup) do(key string, fn func() ([]byte, error)) ([]byte, error, bool) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flightCall{}
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.body, c.err, true
	}
	c := &flightCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	c.body, c.err = fn()
	close(c.done)

	// 完成即移除，map 只在有请求在飞时占用内存，无界风险天然不存在
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	return c.body, c.err, false
}
