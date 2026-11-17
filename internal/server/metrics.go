// 管理端性能监测：按路由聚合响应耗时/大小/状态码，并用 pgx tracer 与
// 压缩中间件各自累加的链路计时还原"总耗时 = DB + 压缩 + 其余（处理+编码）"。
// 全部在内存里，环形缓冲有界，适合 1GB 内存的单节点 VPS。
package server

import (
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"anipick/internal/db"
)

const (
	routeDursCap = 256 // 每路由保留的近期耗时样本数（算 p95 用）
	recentCap    = 256 // 全局近期请求环形缓冲
	// SPA 托管路由的统计键；带扩展名的静态产物请求不进统计
	staticRoute = "GET /"
)

type routeStat struct {
	mu         sync.Mutex
	count      int
	errors     int
	totalDur   time.Duration
	maxDur     time.Duration
	durs       []time.Duration
	dursIdx    int
	totalBytes int64
	totalDB    time.Duration
	totalGzip  time.Duration
}

type reqRecord struct {
	At     time.Time `json:"at"`
	Route  string    `json:"route"`
	Status int       `json:"status"`
	Dur    float64   `json:"ms"`
	DB     float64   `json:"db_ms"`
	Gzip   float64   `json:"gzip_ms"`
	Bytes  int64     `json:"bytes"`
}

type metrics struct {
	started time.Time
	mu      sync.Mutex
	routes  map[string]*routeStat
	recent  []reqRecord
	recentN int
}

func newMetrics() *metrics {
	return &metrics{
		started: time.Now(),
		routes:  map[string]*routeStat{},
		recent:  make([]reqRecord, recentCap),
	}
}

func (m *metrics) record(route string, status int, dur, dbT, gzipT time.Duration, bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs := m.routes[route]
	if rs == nil {
		rs = &routeStat{}
		m.routes[route] = rs
	}
	rs.mu.Lock()
	rs.count++
	if status >= 400 {
		rs.errors++
	}
	rs.totalDur += dur
	if dur > rs.maxDur {
		rs.maxDur = dur
	}
	if len(rs.durs) < routeDursCap {
		rs.durs = append(rs.durs, dur)
	} else {
		rs.durs[rs.dursIdx%routeDursCap] = dur
		rs.dursIdx++
	}
	rs.totalBytes += bytes
	rs.totalDB += dbT
	rs.totalGzip += gzipT
	rs.mu.Unlock()

	m.recent[m.recentN%recentCap] = reqRecord{
		At: time.Now(), Route: route, Status: status,
		Dur: secsMs(dur), DB: secsMs(dbT), Gzip: secsMs(gzipT), Bytes: bytes,
	}
	m.recentN++
}

func secsMs(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// RouteMetrics 是对外输出的单路由聚合。
type RouteMetrics struct {
	Route     string  `json:"route"`
	Count     int     `json:"count"`
	Errors    int     `json:"errors"`
	AvgMs     float64 `json:"avg_ms"`
	P95Ms     float64 `json:"p95_ms"`
	MaxMs     float64 `json:"max_ms"`
	AvgBytes  int64   `json:"avg_bytes"`
	AvgDBMs   float64 `json:"avg_db_ms"`
	AvgGzipMs float64 `json:"avg_gzip_ms"`
}

type metricsSnapshot struct {
	Since   time.Time      `json:"since"`
	UptimeS int64          `json:"uptime_s"`
	Routes  []RouteMetrics `json:"routes"`
	Recent  []reqRecord    `json:"recent"`
}

func (m *metrics) snapshot() metricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := metricsSnapshot{
		Since:   m.started,
		UptimeS: int64(time.Since(m.started).Seconds()),
		Routes:  make([]RouteMetrics, 0, len(m.routes)),
		Recent:  make([]reqRecord, 0, min(m.recentN, recentCap)),
	}
	for route, rs := range m.routes {
		rs.mu.Lock()
		rm := RouteMetrics{
			Route: route, Count: rs.count, Errors: rs.errors,
			AvgMs:     secsMs(rs.totalDur / time.Duration(max(rs.count, 1))),
			MaxMs:     secsMs(rs.maxDur),
			AvgBytes:  rs.totalBytes / int64(max(rs.count, 1)),
			AvgDBMs:   secsMs(rs.totalDB / time.Duration(max(rs.count, 1))),
			AvgGzipMs: secsMs(rs.totalGzip / time.Duration(max(rs.count, 1))),
		}
		if len(rs.durs) > 0 {
			sorted := make([]time.Duration, len(rs.durs))
			copy(sorted, rs.durs)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			rm.P95Ms = secsMs(sorted[int(float64(len(sorted)-1)*0.95)])
		}
		rs.mu.Unlock()
		out.Routes = append(out.Routes, rm)
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Count > out.Routes[j].Count })
	for i := 1; i <= min(m.recentN, recentCap); i++ {
		out.Recent = append(out.Recent, m.recent[(m.recentN-i+recentCap*1000)%recentCap])
	}
	return out
}

func (m *metrics) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = map[string]*routeStat{}
	m.recent = make([]reqRecord, recentCap)
	m.recentN = 0
	m.started = time.Now()
}

// sizeWriter 统计写出字节数（含状态码），供指标聚合使用。
type sizeWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *sizeWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *sizeWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// withMetrics 是挂在每个路由注册处的处理器包装：静态产物请求不进统计
// （一个页面几十个资源会淹没 API 样本）。
func (s *Server) withMetrics(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route == staticRoute && strings.Contains(path.Base(r.URL.Path), ".") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &sizeWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		var dbT, gzT time.Duration
		if trace := db.TraceFrom(r.Context()); trace != nil {
			dbT = time.Duration(trace.DB.Load())
			gzT = time.Duration(trace.Gzip.Load())
		}
		s.m.record(route, sw.status, time.Since(start), dbT, gzT, sw.bytes)
	})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.m.snapshot())
}

func (s *Server) handleAdminMetricsReset(w http.ResponseWriter, r *http.Request) {
	s.m.reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
