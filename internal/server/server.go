package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"anipick/internal/bgm"
	"anipick/internal/crawler"
	"anipick/internal/netx"
	"anipick/internal/registry"
	"anipick/internal/sources"
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
	commit   string
	creds    *sources.CredStore // 数据源凭据快照（后台设置，保存立刷）

	// 出站通道：baseHTTPClient 是启动期通道（DoH/直连）；appliedProxy 是当前
	// 出站通道状态：设置（代理/DoH/ECH）签名变化才重建客户端。
	httpClient *http.Client
	outboundMu sync.Mutex
	appliedNet string
	// 真实 IP 识别（随设置快照立刷；clientIP 每请求读这里）。
	ipMu           sync.RWMutex
	ipHeader       string
	ipTrustedCIDRs []*net.IPNet

	cacheMu  sync.Mutex
	metaTags map[string]cachedMetaTags

	metaMu             sync.Mutex
	metaBody           cachedJSON
	metaRefreshing     bool // 过期后台重算进行中（单飞，防止并发重算压库）
	overviewMu         sync.Mutex
	overview           cachedJSON
	overviewRefreshing bool // 同上：overview 过期后台重算进行中

	pageMu    sync.Mutex
	pageCache map[string]cachedJSON

	// 评分全量重算的并发合并：同一时刻只跑一轮，进行中的保存只置待重算标记，
	// 当前轮结束后补算一轮（分块按当前权重快照逐块读，中途改的权重需要补算）。
	synthMu     sync.Mutex
	synthQueued atomic.Bool

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
	Version string
	Commit  string
	// Credentials 是数据源凭据快照（main 装配；server 随设置加载/保存立刷）。
	Credentials *sources.CredStore
}

// buildID 返回组合版本标识 "版本号-提交哈希"，页脚展示、前端一致性检测共用。
// 提交哈希缺省（本地裸构建 main.commit="none"）时退化为仅版本号。
// ShutdownWait 返回设置里的 HTTP 优雅停机等待（main 关闭 http server 时用）。
func (s *Server) ShutdownWait() time.Duration {
	if s.cfg.shutdownWait > 0 {
		return s.cfg.shutdownWait
	}
	return 10 * time.Second
}

func (s *Server) buildID() string {
	if s.commit == "" || s.commit == "none" {
		return s.version
	}
	return s.version + "-" + s.commit
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
		store:       st,
		crawler:     cw,
		defaults:    defaults,
		static:      sub,
		limits:      newLimiter(),
		version:     ver,
		commit:      opts.Commit,
		creds:       opts.Credentials,
		metaTags:    map[string]cachedMetaTags{},
		pageCache:   map[string]cachedJSON{},
		tagCache:    map[string]cachedJSON{},
		detailCache: map[string]cachedJSON{},
		m:           newMetrics(),
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
	reg("POST /api/admin/works/rematch", s.requireAdmin(s.handleAdminRematchWorks))
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

// invalidateSubject 失效单个条目的详情缓存（索引详情与扩展源代理详情，管理端刷新/删除后调用）。
func (s *Server) invalidateSubject(id int64) {
	s.detailMu.Lock()
	delete(s.detailCache, strconv.FormatInt(id, 10))
	delete(s.detailCache, "ext:"+strconv.FormatInt(id, 10))
	s.detailMu.Unlock()
}

// 快照刷新周期；实际参数存系统设置（AppSettings.Cache/Security），
// 管理后台「设置 → 查询缓存 / 限流与安全」可调。
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
	// 服务器与面板参数（原为代码常量，全部后台可调）
	minCompressBytes int           // 小响应不压缩阈值（字节）
	snapshotTTL      time.Duration // 设置快照兜底刷新周期
	overviewRuns     int           // 管理总览最近任务条数
	shutdownWait     time.Duration // HTTP 优雅停机等待
	// sources 是数据源管理设置快照（启用/优先级），来源 manifest 共用
	sources map[string]store.SourceSetting
}

// applySnapshot 落运行参数快照并同步 store 的 count 缓存 TTL 与键上限。
// TTL 为 0 表示关闭对应缓存，各消费方按 <=0 跳过缓存读写。
func (s *Server) applySnapshot(settings store.AppSettings) {
	c, sec, sv := settings.Cache, settings.Security, settings.Server
	s.cfg = cfgSnapshot{
		queryTTL:         time.Duration(store.CacheSec(c.QueryTTL, 60)) * time.Second,
		publicTTL:        time.Duration(store.CacheSec(c.PublicTTL, 30)) * time.Second,
		metaTTL:          time.Duration(store.CacheSec(c.MetaTTL, 30)) * time.Second,
		metaTagsTTL:      time.Duration(store.CacheSec(c.MetaTagsTTL, 600)) * time.Second,
		overviewTTL:      time.Duration(store.CacheSec(c.OverviewTTL, 3)) * time.Second,
		maxKeys:          c.MaxKeys,
		rateRPS:          float64(sec.RateLimitRPS),
		rateBurst:        float64(sec.RateLimitBurst),
		loginRate:        float64(sec.LoginRPM) / 60,
		loginBurst:       sec.LoginBurst,
		loginDelay:       time.Duration(sec.LoginDelayMS) * time.Millisecond,
		sessionTTL:       time.Duration(sec.SessionTTLDays) * 24 * time.Hour,
		limiterMaxIPs:    sec.LimiterMaxIPs,
		maxPage:          sec.MaxPage,
		minCompressBytes: sv.MinCompressBytes,
		snapshotTTL:      time.Duration(sv.SnapshotTTLSec) * time.Second,
		overviewRuns:     sv.OverviewRuns,
		shutdownWait:     time.Duration(sv.ShutdownWaitSec) * time.Second,
		sources:          settings.Sources,
	}
	s.store.SetCountTTL(s.cfg.queryTTL)
	s.store.SetCountCap(s.cfg.maxKeys)
	// 请求重试与响应体积立刷（bgm 客户端与扩展源通道同一口径，原为代码常量）
	cr := settings.Crawl
	bgm.SetRetryPolicy(cr.RequestAttempts, time.Duration(cr.RequestBackoffMS)*time.Millisecond,
		time.Duration(cr.RetryAfterCapSec)*time.Second, cr.ResponseLimitMB)
	sources.SetFetchParams(cr.RequestAttempts, time.Duration(cr.RequestBackoffMS)*time.Millisecond,
		int64(cr.ResponseLimitMB)<<20, time.Duration(cr.OutboundTimeoutSec)*time.Second,
		time.Duration(cr.RetryAfterCapSec)*time.Second, settings.Network.OutboundUA)
	sources.SetTagMax(cr.ExtTagMax)
	// 每源收获预算立刷（页数/单页条数/标签门槛/地区；适配器详情路径读这层快照）
	for _, spec := range registry.All() {
		ps, fp, ip, tr, rg := store.EffectiveHarvest(spec.ID, settings.Sources)
		sources.SetSourceParams(spec.ID, sources.SourceParams{
			PageSize: ps, FullPages: fp, IncPages: ip, TagMinRank: tr, Region: rg,
		})
	}
	// 合并判定与会话缓存参数立刷（原为登记表常量与 store 内常量）
	store.SetMatchTunables(cr.MatchYearWindow, cr.RematchBatch, cr.MatchDateDays)
	store.SetMatchTypeGroups(cr.MatchGroups)
	store.SetSessionCacheParams(sv.SessionCacheTTLSec, sv.SessionSeenGapSec, sv.SessionCacheMax)
	// 评分合成参数立刷（块大小/单块与整体超时，原为代码常量）
	store.SetScoreSynthTunables(sv.SynthChunkSize,
		time.Duration(sv.SynthChunkTimeoutSec)*time.Second,
		time.Duration(sv.SynthOverallTimeoutSec)*time.Second)
	// 性能面板样本上限立刷（环形缓冲收缩，丢最旧样本）
	s.m.SetCaps(sv.PerfSampleCap, sv.RecentReqCap)
	// 数据源设置立刷：资料序进匹配器，评分权重进合成，凭据进适配器
	store.SetSourcePriorities(prioritiesOf(settings.Sources))
	store.SetScoreWeights(weightsOf(settings.Sources))
	if s.creds != nil {
		s.creds.Set(settings.Credentials)
	}
	// 每源请求间隔立刷：登记表默认 + 设置覆写（下限按官方限速口径钳），
	// 时间槽是动态槽位，下一次请求即生效，无需重建客户端。
	for _, spec := range registry.All() {
		sources.SetInterval(spec.ID, time.Duration(store.EffectiveInterval(spec.ID, settings.Sources))*time.Millisecond)
	}
	// 真实 IP 识别立刷（解析失败的 CIDR 丢弃并记日志；保存入口已拒绝非法值）
	s.ipMu.Lock()
	s.ipHeader = strings.TrimSpace(settings.Security.ClientIPHeader)
	cidrs := make([]*net.IPNet, 0, 8)
	for _, part := range strings.Split(settings.Security.TrustedProxyCIDRs, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, c, err := net.ParseCIDR(part); err == nil {
			cidrs = append(cidrs, c)
		} else {
			slog.Warn("可信网段解析失败，忽略", "cidr", part, "err", err)
		}
	}
	s.ipTrustedCIDRs = cidrs
	s.ipMu.Unlock()
	// 出站通道设置立刷（代理/DoH/ECH 任一变化即重建，bgm 抓取与扩展源抓取共用）
	s.swapOutbound(settings.Network)
}

// swapOutbound 按设置重建出站客户端并下发到抓取器与各适配器：
// 代理优先，其次 DoH/ECH 解析通道，都没有则直连。
// 设置签名（地址+开关+端点）未变化时是空操作；非法代理地址保留当前通道并记日志。
func (s *Server) swapOutbound(net store.NetworkSettings) {
	proxy := strings.TrimSpace(net.EgressProxy)
	sig := fmt.Sprintf("proxy=%s|doh=%t|ech=%t|ep=%s", proxy, net.DoH, net.ECH, strings.Join(net.DoHEndpoints, ","))
	s.outboundMu.Lock()
	defer s.outboundMu.Unlock()
	if sig == s.appliedNet {
		return
	}
	kind := "直连"
	var hc *http.Client
	switch {
	case proxy != "":
		u, err := url.Parse(proxy)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
			slog.Error("出站代理地址无效，沿用当前通道", "proxy", proxy, "err", err)
			return
		}
		hc = netx.NewProxyHTTPClient(u)
		kind = "代理"
	case net.DoH:
		endpoints := net.DoHEndpoints
		if len(endpoints) == 0 {
			endpoints = []string{"https://dns.alidns.com/resolve", "https://1.1.1.1/dns-query"}
		}
		r := netx.NewResolver(endpoints, net.ECH)
		r.SetTunables(net.DoHTimeoutSec, net.DnsCacheMinSec, net.DnsCacheTTLSec)
		hc = r.NewHTTPClient()
		kind = "DoH"
	default:
		hc = &http.Client{}
	}
	s.appliedNet = sig
	s.httpClient = hc
	sources.SwapOutbound(hc)
	if s.crawler != nil {
		s.crawler.SetHTTPClient(hc)
	}
	slog.Info("抓取出站通道已切换", "mode", kind, "doh", net.DoH, "ech", net.ECH)
}

// weightsOf 抽出设置里的有效评分权重（合成快照只认数值）。
func weightsOf(sources map[string]store.SourceSetting) map[string]int {
	out := make(map[string]int, len(sources))
	for id := range sources {
		out[id] = store.EffectiveWeight(id, sources)
	}
	return out
}

// prioritiesOf 抽出设置里的优先级覆写（匹配器快照只认数值）。
func prioritiesOf(sources map[string]store.SourceSetting) map[string]int {
	out := make(map[string]int, len(sources))
	for id, v := range sources {
		out[id] = v.Priority
	}
	return out
}

// snapshot 返回当前运行参数：快照 60s 内复用，过期才读一次设置；
// 读失败沿用上一次取值（无取值时保留默认），不阻塞公开热路径。
func (s *Server) snapshot(ctx context.Context) cfgSnapshot {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if time.Since(s.cfgAt) > s.cfg.snapshotTTL {
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
// 数据源 manifest 随设置变化，meta 缓存一并丢弃。
func (s *Server) invalidateCacheConfig() {
	s.cfgMu.Lock()
	s.cfgAt = time.Time{}
	s.cfgMu.Unlock()
	s.metaMu.Lock()
	s.metaBody = cachedJSON{}
	s.metaMu.Unlock()
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
