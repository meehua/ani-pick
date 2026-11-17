package server

import (
	"embed"
	"io/fs"
	"net"
	"net/http"
	"path"
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
	return &Server{
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
		clientIPHeader: strings.TrimSpace(opts.ClientIPHeader),
		trustedCIDRs:   opts.TrustedProxyCIDRs,
		m:              newMetrics(),
	}, nil
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
