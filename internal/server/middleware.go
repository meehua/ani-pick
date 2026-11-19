package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"anipick/internal/db"
)

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return s.recoverer(s.securityHeaders(s.generalLimit(s.traceMiddleware(s.logRequests(s.gzipResponse(next))))))
}

// traceMiddleware 在请求入口放置链路计时累积器（DB 由 pgx tracer 累加、
// gzip 由压缩中间件累加），供管理端性能面板还原每请求的耗时构成。
func (s *Server) traceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := db.WithTrace(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
				"dur", time.Since(start).Round(time.Millisecond).String())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "path", r.URL.Path, "recover", rec)
				fail(w, http.StatusInternalServerError, "panic", "服务内部错误", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data: https:; "+
					"connect-src 'self' https:; "+
					"font-src 'self' data:; object-src 'none'; base-uri 'self'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

// limiter 是极简的令牌桶：按 IP 限制请求速率。
type limiter struct {
	mu      sync.Mutex
	entries map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter() *limiter {
	return &limiter{entries: map[string]*bucket{}}
}

// clientIP 返回请求来源 IP。配置了 clientIPHeader（如经 Cloudflare/反代暴露时
// 取 CF-Connecting-IP）且 TCP 对端在可信网段内时优先使用；否则回退 TCP 对端地址。
// 教训（源自 Morikusa 06-security）：XFF 类头可被伪造绕过限流，只有连接来自
// 可信反代网段才读头；未配置可信网段时对头完全不信。
func (s *Server) clientIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if s.clientIPHeader != "" && s.trustedProxy(remote) {
		if v := r.Header.Get(s.clientIPHeader); v != "" {
			// 兼容 X-Forwarded-For 这类逗号列表头，取第一个（最接近客户端的）
			if i := strings.IndexByte(v, ','); i >= 0 {
				v = v[:i]
			}
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	return remote
}

// trustedProxy 判断 TCP 对端是否属于配置的可信反代网段。
func (s *Server) trustedProxy(remote string) bool {
	if len(s.trustedCIDRs) == 0 {
		return false
	}
	ip := net.ParseIP(remote)
	if ip == nil {
		return false
	}
	for _, c := range s.trustedCIDRs {
		if c.Contains(ip) {
			return true
		}
	}
	return false
}

// limiterEntriesMax 是限流表条目上限的兜底默认值（1GB 机器内存红线：进程内
// 缓存必须带上限）；实际取值走系统设置（SecuritySettings.limiter_max_ips），
// 经 allow 的 maxEntries 参数注入。新 IP 插入前腾位：先清 10 分钟未活跃的
// 条目，仍满则强制淘汰最久未活跃的——独特 IP 洪泛（经可信反代头的伪造值或
// 真实 IPv6 潮汐）能把条目数推满，必须有硬界。O(n) 扫描只在表满时触发；
// 被淘汰的 IP 会拿到满桶，属限流精度换内存安全的取舍。
const limiterEntriesMax = 4096

func (l *limiter) allow(key string, rate, burst float64, maxEntries int) bool {
	if maxEntries <= 0 {
		maxEntries = limiterEntriesMax
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= maxEntries {
			for k, e := range l.entries {
				if now.Sub(e.last) > 10*time.Minute {
					delete(l.entries, k)
				}
			}
			for len(l.entries) >= maxEntries {
				oldestKey := ""
				oldest := now
				for k, e := range l.entries {
					if k == key {
						continue // 不挤掉刚要建立的桶
					}
					if e.last.Before(oldest) {
						oldestKey, oldest = k, e.last
					}
				}
				if oldestKey == "" {
					break
				}
				delete(l.entries, oldestKey)
			}
		}
		b = &bucket{tokens: burst, last: now}
		l.entries[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (s *Server) generalLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// 速率/突发/IP 上限走系统设置（限流与安全），60s 快照 + 保存后立刷
		snap := s.snapshot(r.Context())
		if !s.limits.allow(s.clientIP(r), snap.rateRPS, snap.rateBurst, snap.limiterMaxIPs) {
			fail(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}
