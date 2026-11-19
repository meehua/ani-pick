package server

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"anipick/internal/bgm"
	"anipick/internal/crawler"
	"anipick/internal/netx"
	"anipick/internal/store"
)

//go:embed all:dist
var distFS embed.FS

type Server struct {
	store    *store.Store
	crawler  *crawler.Crawler
	defaults store.AppSettings
	static   fs.FS
	limits   *limiter
	version  string

	httpClient     *http.Client
	resolver       *netx.Resolver
	egressProxy    bool
	clientIPHeader string
	trustedCIDRs   []*net.IPNet

	cacheMu  sync.Mutex
	metaTags map[string]cachedMetaTags

	metaMu     sync.Mutex
	metaBody   cachedJSON
	overviewMu sync.Mutex
	overview   cachedJSON

	pageMu    sync.Mutex
	pageCache map[string]cachedJSON

	// 标签候选与条目详情的结果缓存（数据只在抓取/管理操作后变化）
	tagMu       sync.Mutex
	tagCache    map[string]cachedJSON
	detailMu    sync.Mutex
	detailCache map[string]cachedJSON

	flights flightGroup

	// 运行参数快照（系统设置 → 查询缓存/限流与安全）：启动时读一次，运行期 60s
	// 惰性刷新、保存设置后立刷——公开热路径不因设置引入每请求查询。
	cfgMu sync.Mutex
	cfgAt time.Time
	cfg   cfgSnapshot

	m *metrics
}

// cachedJSON 是带 TTL 的响应体缓存，命中时跳过查询与 JSON 编码。
type cachedJSON struct {
	at   time.Time
	body []byte
}

// Options 是服务层的可选依赖（网络通道等）。
type Options struct {
	HTTPClient        *http.Client
	Resolver          *netx.Resolver
	EgressProxy       bool
	Version           string
	ClientIPHeader    string
	TrustedProxyCIDRs []*net.IPNet
}

func New(st *store.Store, cw *crawler.Crawler, defaults store.AppSettings, opts Options) (*Server, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	ver := opts.Version
	if ver == "" {
		ver = "dev"
	}
	s := &Server{
		store:          st,
		crawler:        cw,
		defaults:       defaults,
		static:         sub,
		limits:         newLimiter(),
		version:        ver,
		httpClient:     opts.HTTPClient,
		resolver:       opts.Resolver,
		egressProxy:    opts.EgressProxy,
		metaTags:       map[string]cachedMetaTags{},
		pageCache:      map[string]cachedJSON{},
		tagCache:       map[string]cachedJSON{},
		detailCache:    map[string]cachedJSON{},
		clientIPHeader: strings.TrimSpace(opts.ClientIPHeader),
		trustedCIDRs:   opts.TrustedProxyCIDRs,
		m:              newMetrics(),
	}
	// 启动即同步一次运行参数（缓存/限流/会话）；失败先落默认值，运行期惰性重试
	def := defaults
	def.Normalize()
	s.applySnapshot(def)
	if st.Pool != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if settings, err := st.LoadSettings(sctx, defaults); err != nil {
			slog.Warn("读取运行参数失败，使用默认值", "err", err)
		} else {
			s.applySnapshot(settings)
		}
		cancel()
	}
	s.cfgAt = time.Now()
	// 抓取收尾时失效公开查询缓存（增量逐批写入，失败/取消也要失效）
	if cw != nil {
		cw.OnRunFinished = s.invalidateQueryCaches
	}
	return s, nil
}

func (s *Server) bgmClient(settings store.AppSettings) *bgm.Client {
	timeout := time.Duration(settings.Crawl.RequestTimeoutSec) * time.Second
	return bgm.NewWithHTTPClient(s.httpClient, settings.BGM.Token, settings.BGM.Cookie, settings.BGM.UA, timeout)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// reg = 注册 + 性能统计包装（按注册模式聚合耗时/大小/状态码）
	reg := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.withMetrics(pattern, h))
	}

	reg("GET /api/health", s.handleHealth)
	reg("GET /api/meta", s.handleMeta)
	reg("GET /api/subjects", s.handleSearch)
	reg("GET /api/subjects/{id}", s.handleSubjectIndex)
	reg("GET /api/tags", s.handleTags)
	reg("GET /api/meta-tags", s.handleMetaTags)

	reg("POST /api/admin/login", s.handleAdminLogin)
	reg("POST /api/admin/logout", s.handleAdminLogout)
	reg("GET /api/admin/session", s.requireAdmin(s.handleAdminSession))
	reg("GET /api/admin/overview", s.requireAdmin(s.handleAdminOverview))
	reg("GET /api/admin/network", s.requireAdmin(s.handleAdminNetwork))
	reg("GET /api/admin/settings", s.requireAdmin(s.handleAdminGetSettings))
	reg("PUT /api/admin/settings", s.requireAdmin(s.handleAdminPutSettings))
	reg("POST /api/admin/password", s.requireAdmin(s.handleAdminPassword))
	reg("POST /api/admin/credential/test", s.requireAdmin(s.handleAdminTestCredential))
	reg("POST /api/admin/crawl", s.requireAdmin(s.handleAdminStartCrawl))
	reg("POST /api/admin/crawl/cancel", s.requireAdmin(s.handleAdminCancelCrawl))
	reg("GET /api/admin/runs", s.requireAdmin(s.handleAdminRuns))
	reg("GET /api/admin/runs/{id}", s.requireAdmin(s.handleAdminRun))
	reg("GET /api/admin/subjects", s.requireAdmin(s.handleAdminSubjects))
	reg("POST /api/admin/subjects/{id}/refresh", s.requireAdmin(s.handleAdminRefreshSubject))
	reg("DELETE /api/admin/subjects/{id}", s.requireAdmin(s.handleAdminDeleteSubject))
	reg("GET /api/admin/metrics", s.requireAdmin(s.handleAdminMetrics))
	reg("POST /api/admin/metrics/reset", s.requireAdmin(s.handleAdminMetricsReset))

	mux.Handle("/", s.withMetrics(staticRoute, s.spa()))

	return s.withMiddleware(mux)
}

// invalidateOverview 使管理端 overview 的响应缓存失效（抓取/设置等动作后调用）。
func (s *Server) invalidateOverview() {
	s.overviewMu.Lock()
	s.overview = cachedJSON{}
	s.overviewMu.Unlock()
}

// invalidateQueryCaches 失效全部公开查询结果缓存（抓取结束/管理端删改条目后调用）。
// 搜索页/总数/标签候选/详情缓存的数据只在抓取与条目级操作后变化，
// 靠显式失效保持新鲜，TTL 只作兜底，因此可以放到 60s 省下弱机上的重复查询。
func (s *Server) invalidateQueryCaches() {
	s.pageMu.Lock()
	s.pageCache = map[string]cachedJSON{}
	s.pageMu.Unlock()
	s.tagMu.Lock()
	s.tagCache = map[string]cachedJSON{}
	s.tagMu.Unlock()
	s.detailMu.Lock()
	s.detailCache = map[string]cachedJSON{}
	s.detailMu.Unlock()
	s.store.InvalidateCounts()
}

// invalidateSubject 失效单个条目的详情缓存（管理端刷新/删除后调用）。
func (s *Server) invalidateSubject(id int64) {
	s.detailMu.Lock()
	delete(s.detailCache, strconv.FormatInt(id, 10))
	s.detailMu.Unlock()
}

// 快照刷新周期；实际参数存系统设置（AppSettings.Cache/Security），
// 管理后台「设置 → 查询缓存 / 限流与安全」可调。
const cacheCfgRefreshTTL = 60 * time.Second

// cfgSnapshot 是公开热路径用的运行参数快照，避免每请求读设置。
type cfgSnapshot struct {
	queryTTL      time.Duration // 搜索页/总数/标签候选/详情共用
	publicTTL     time.Duration // 公开 GET 响应 Cache-Control max-age
	metaTTL       time.Duration // /api/meta
	metaTagsTTL   time.Duration // /api/meta-tags
	overviewTTL   time.Duration // /api/admin/overview
	maxKeys       int           // 搜索页/标签候选/详情/搜索总数缓存的键上限
	rateRPS       float64       // 每 IP 常规请求速率（次/秒）
	rateBurst     float64       // 每 IP 常规请求突发容量
	loginRate     float64       // 登录尝试稳态速率（次/秒）
	loginBurst    int           // 登录尝试突发容量
	loginDelay    time.Duration // 密码错误的响应延迟
	sessionTTL    time.Duration // 管理员会话有效期
	limiterMaxIPs int           // 限流表 IP 条目硬上限
	maxPage       int           // 公开搜索最大页码
}

// applySnapshot 落运行参数快照并同步 store 的 count 缓存 TTL 与键上限。
// TTL 为 0 表示关闭对应缓存，各消费方按 <=0 跳过缓存读写。
func (s *Server) applySnapshot(settings store.AppSettings) {
	c, sec := settings.Cache, settings.Security
	s.cfg = cfgSnapshot{
		queryTTL:      time.Duration(store.CacheSec(c.QueryTTL, 60)) * time.Second,
		publicTTL:     time.Duration(store.CacheSec(c.PublicTTL, 30)) * time.Second,
		metaTTL:       time.Duration(store.CacheSec(c.MetaTTL, 30)) * time.Second,
		metaTagsTTL:   time.Duration(store.CacheSec(c.MetaTagsTTL, 600)) * time.Second,
		overviewTTL:   time.Duration(store.CacheSec(c.OverviewTTL, 3)) * time.Second,
		maxKeys:       c.MaxKeys,
		rateRPS:       float64(sec.RateLimitRPS),
		rateBurst:     float64(sec.RateLimitBurst),
		loginRate:     float64(sec.LoginRPM) / 60,
		loginBurst:    sec.LoginBurst,
		loginDelay:    time.Duration(sec.LoginDelayMS) * time.Millisecond,
		sessionTTL:    time.Duration(sec.SessionTTLDays) * 24 * time.Hour,
		limiterMaxIPs: sec.LimiterMaxIPs,
		maxPage:       sec.MaxPage,
	}
	s.store.SetCountTTL(s.cfg.queryTTL)
	s.store.SetCountCap(s.cfg.maxKeys)
}

// snapshot 返回当前运行参数：快照 60s 内复用，过期才读一次设置；
// 读失败沿用上一次取值（无取值时保留默认），不阻塞公开热路径。
func (s *Server) snapshot(ctx context.Context) cfgSnapshot {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if time.Since(s.cfgAt) > cacheCfgRefreshTTL {
		if settings, err := s.store.LoadSettings(ctx, s.defaults); err == nil {
			s.applySnapshot(settings)
		} else {
			slog.Warn("刷新运行参数失败，沿用上一次取值", "err", err)
		}
		s.cfgAt = time.Now()
	}
	return s.cfg
}

// invalidateCacheConfig 丢弃设置快照，下一次请求重新读取（保存设置后调用）。
func (s *Server) invalidateCacheConfig() {
	s.cfgMu.Lock()
	s.cfgAt = time.Time{}
	s.cfgMu.Unlock()
}

// hasDotDot 判断路径是否含 .. 段（穿越语义）。
func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func (s *Server) spa() http.Handler {
	fileServer := http.FileServer(http.FS(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// 未匹配的 API 路径直接 404，绝不回退到 SPA。
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		// 解码后路径含 .. 段的一律 404，不落 SPA 兜底：编码穿越（%2e%2e/%2f 等
		// 变体）会绕过 mux 的路径规整落进本 handler，r.URL.Path 已百分号解码。
		if hasDotDot(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if _, err := fs.Stat(s.static, name); err == nil {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				// 静态产物压缩结果已缓存：直接回 gzip 字节，绕过逐次压缩
				if s.serveGzippedAsset(w, r, name) {
					return
				}
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// 有扩展名的资源缺失时返回 404，避免把 HTML 当 JS 返回
		if strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
