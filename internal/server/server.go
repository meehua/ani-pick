package server

import (
	"embed"
	"io/fs"
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

	httpClient  *http.Client
	resolver    *netx.Resolver
	egressProxy bool

	cacheMu  sync.Mutex
	metaTags cachedMetaTags
}

// Options 是服务层的可选依赖（网络通道等）。
type Options struct {
	HTTPClient  *http.Client
	Resolver    *netx.Resolver
	EgressProxy bool
	Version     string
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
		store:       st,
		crawler:     cw,
		defaults:    defaults,
		static:      sub,
		limits:      newLimiter(),
		version:     ver,
		httpClient:  opts.HTTPClient,
		resolver:    opts.Resolver,
		egressProxy: opts.EgressProxy,
	}, nil
}

func (s *Server) bgmClient(settings store.AppSettings) *bgm.Client {
	timeout := time.Duration(settings.Crawl.RequestTimeoutSec) * time.Second
	return bgm.NewWithHTTPClient(s.httpClient, settings.BGM.Token, settings.BGM.Cookie, settings.BGM.UA, timeout)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/subjects", s.handleSearch)
	mux.HandleFunc("GET /api/subjects/{id}", s.handleSubjectIndex)
	mux.HandleFunc("GET /api/tags", s.handleTags)
	mux.HandleFunc("GET /api/meta-tags", s.handleMetaTags)

	mux.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("POST /api/admin/logout", s.handleAdminLogout)
	mux.HandleFunc("GET /api/admin/session", s.requireAdmin(s.handleAdminSession))
	mux.HandleFunc("GET /api/admin/overview", s.requireAdmin(s.handleAdminOverview))
	mux.HandleFunc("GET /api/admin/network", s.requireAdmin(s.handleAdminNetwork))
	mux.HandleFunc("GET /api/admin/settings", s.requireAdmin(s.handleAdminGetSettings))
	mux.HandleFunc("PUT /api/admin/settings", s.requireAdmin(s.handleAdminPutSettings))
	mux.HandleFunc("POST /api/admin/password", s.requireAdmin(s.handleAdminPassword))
	mux.HandleFunc("POST /api/admin/credential/test", s.requireAdmin(s.handleAdminTestCredential))
	mux.HandleFunc("POST /api/admin/crawl", s.requireAdmin(s.handleAdminStartCrawl))
	mux.HandleFunc("POST /api/admin/crawl/cancel", s.requireAdmin(s.handleAdminCancelCrawl))
	mux.HandleFunc("GET /api/admin/runs", s.requireAdmin(s.handleAdminRuns))
	mux.HandleFunc("GET /api/admin/runs/{id}", s.requireAdmin(s.handleAdminRun))
	mux.HandleFunc("GET /api/admin/subjects", s.requireAdmin(s.handleAdminSubjects))
	mux.HandleFunc("POST /api/admin/subjects/{id}/refresh", s.requireAdmin(s.handleAdminRefreshSubject))
	mux.HandleFunc("DELETE /api/admin/subjects/{id}", s.requireAdmin(s.handleAdminDeleteSubject))

	mux.Handle("/", s.spa())

	return s.withMiddleware(mux)
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
