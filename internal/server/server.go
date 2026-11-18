package server

import (
	"embed"
	"io/fs"
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
	tagMu    sync.Mutex
	tagCache map[string]cachedJSON
	detailMu    sync.Mutex
	detailCache map[string]cachedJSON

	flights flightGroup

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
