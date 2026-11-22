package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"anipick/internal/crawler"
	"anipick/internal/db"
	"anipick/internal/netx"
	"anipick/internal/registry"
	"anipick/internal/sources"
	"anipick/internal/store"
)

func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

// overviewRunsLimit 是概览面板「最近任务」的显示条数：一次合并运行按源各占
// 一行，条数要覆盖到每个启用源最近一轮（推算下次自动计划也读它）；任务历史的
// 完整浏览走「数据 → 抓取设置」页的任务表，带筛选/排序/分页。
// handleAdminOverview 聚合 6+ 条统计 SQL；前端每 4s 轮询。
// 缓存过期时先回旧数据、后台单飞重算（stale-while-revalidate）：轮询永远
// 不等库，弱机上持续轮询的聚合开销被压到后台每 TTL 至多一次；抓取/设置等
// 动作后的主动失效（invalidateOverview）清空缓存，下一次请求同步实算。
func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot(r.Context())
	if snap.overviewTTL > 0 { // 0 = 关闭 overview 缓存：每次都实算
		s.overviewMu.Lock()
		body := s.overview
		fresh := body.body != nil && time.Since(body.at) < snap.overviewTTL
		stale := body.body != nil && !fresh && !s.overviewRefreshing
		if stale {
			s.overviewRefreshing = true // 同一时刻至多一次后台重算
		}
		s.overviewMu.Unlock()
		if fresh {
			s.m.cacheEvent("overview", "hit")
			writeJSONBytes(w, http.StatusOK, body.body)
			return
		}
		if stale {
			s.m.cacheEvent("overview", "stale")
			// 后台重算沿用触发请求的 DB 计时归因（与搜索单飞同一手法）
			go s.refreshOverview(snap, db.TraceFrom(r.Context()))
			writeJSONBytes(w, http.StatusOK, body.body)
			return
		}
	}
	s.m.cacheEvent("overview", "origin")
	payload, err := s.buildOverview(r.Context(), snap)
	if err != nil {
		internalError(w, err)
		return
	}
	s.overviewMu.Lock()
	s.overview = cachedJSON{at: time.Now(), body: payload}
	s.overviewMu.Unlock()
	writeJSONBytes(w, http.StatusOK, payload)
}

// refreshOverview 后台重算管理总览：请求早已返回，查询用脱离请求的 ctx；
// 完成后替换缓存体并解除单飞标记（失败只丢本次，下次过期再算）。
func (s *Server) refreshOverview(snap cfgSnapshot, tr *db.RequestTrace) {
	ctx, cancel := context.WithTimeout(db.ContextWithTrace(context.Background(), tr), 30*time.Second)
	defer cancel()
	payload, err := s.buildOverview(ctx, snap)
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	if err == nil {
		s.overview = cachedJSON{at: time.Now(), body: payload}
	}
	s.overviewRefreshing = false
}

// buildOverview 聚合管理总览响应体：三路独立查询并行执行缩短耗时
// （省 IO 等待，不增总 CPU），再补过期数与各源新鲜度。
func (s *Server) buildOverview(ctx context.Context, snap cfgSnapshot) ([]byte, error) {
	var (
		stats                          store.Stats
		settings                       store.AppSettings
		runs                           []store.Run
		errStats, errSettings, errRuns error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); stats, errStats = s.store.Stats(ctx) }()
	go func() { defer wg.Done(); settings, errSettings = s.store.LoadSettings(ctx, s.defaults) }()
	go func() { defer wg.Done(); runs, errRuns = s.store.RecentRuns(ctx, snap.overviewRuns) }()
	wg.Wait()
	if errStats != nil {
		return nil, errStats
	}
	if errSettings != nil {
		return nil, errSettings
	}
	if errRuns != nil {
		return nil, errRuns
	}
	// 待重爬口径 = 原生索引超过其有效保留期（源覆盖 ?? 全局默认）的条目数
	nativeTTL := store.EffectiveRefreshTTL(registry.NativeID(), settings.Sources, settings.Crawl.RefreshTTLDays)
	stale, _ := s.store.CountStale(ctx, nativeTTL, settings.Crawl.StaleRetryCap, settings.Crawl.Types)

	// 扩展源清单（管理面板「抓取」面板按源展示配置状态与手动触发按钮）：
	// 按登记表/设置的有效优先级排序，带启用状态与各自的新鲜度（过期条目数，
	// 按各源的有效保留期判定——保留期策略每个源都适用）
	srcCfg := settings.Sources
	allHarvesters := s.crawler.Harvesters()
	staleSources := make([]string, 0, len(allHarvesters)+1)
	staleTTLs := make([]int, 0, len(allHarvesters)+1)
	for _, h := range allHarvesters {
		staleSources = append(staleSources, h.Source())
		staleTTLs = append(staleTTLs, store.EffectiveRefreshTTL(h.Source(), srcCfg, settings.Crawl.RefreshTTLDays))
	}
	staleBySource, _ := s.store.CountStaleBySource(ctx, staleSources, staleTTLs)
	harvesters := make([]map[string]any, 0, len(allHarvesters))
	for _, h := range allHarvesters {
		enabled := true
		if v, ok := srcCfg[h.Source()]; ok {
			enabled = v.Enabled
		}
		harvesters = append(harvesters, map[string]any{
			"source":     h.Source(),
			"label":      h.Label(),
			"types":      h.Types(),
			"configured": h.Configured(),
			"enabled":    enabled,
			"priority":   store.SourcePriority(h.Source(), srcCfg),
			"stale":      staleBySource[h.Source()],
		})
	}
	sort.SliceStable(harvesters, func(i, j int) bool {
		return harvesters[i]["priority"].(int) < harvesters[j]["priority"].(int)
	})
	lastAll, _ := s.store.LastAllSourceRun(ctx, registry.NativeID())

	return json.Marshal(map[string]any{
		"stats":            stats,
		"crawler":          s.crawler.Status(),
		"runs":             runs,
		"stale":            stale,
		"harvesters":       harvesters,
		"last_all_sources": lastAll,
		"settings": map[string]any{
			"crawl": settings.Crawl,
			"bgm": map[string]any{
				"token_configured":  settings.BGM.Token != "",
				"cookie_configured": settings.BGM.Cookie != "",
			},
		},
	})
}

type adminSettingsPayload struct {
	Crawl    store.CrawlSettings    `json:"crawl"`
	Cache    store.CacheSettings    `json:"cache"`
	Security store.SecuritySettings `json:"security"`
	// 未提交 = 保持现有（抓取设置页不编辑该块，用指针区分「没发」与「发零值」）
	Server      *store.ServerSettings          `json:"server,omitempty"`
	Network     store.NetworkSettings          `json:"network"`
	Sources     map[string]store.SourceSetting `json:"sources"`
	Credentials map[string]map[string]string   `json:"credentials"`
	BGM         struct {
		Token       string `json:"token"`
		Cookie      string `json:"cookie"`
		ClearToken  bool   `json:"clear_token"`
		ClearCookie bool   `json:"clear_cookie"`
	} `json:"bgm"`
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	masked := settings.Mask()
	writeJSON(w, http.StatusOK, map[string]any{
		"crawl":       masked.Crawl,
		"cache":       masked.Cache,
		"security":    masked.Security,
		"server":      masked.Server,
		"sources":     masked.Sources,
		"credentials": masked.Credentials,
		"network":     masked.Network,
		"bgm": map[string]any{
			"token":             masked.BGM.Token,
			"cookie":            masked.BGM.Cookie,
			"ua":                masked.BGM.UA,
			"token_configured":  settings.BGM.Token != "",
			"cookie_configured": settings.BGM.Cookie != "",
		},
		"updated_at": settings.UpdatedAt,
	})
}

func (s *Server) handleAdminPutSettings(w http.ResponseWriter, r *http.Request) {
	var payload adminSettingsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&payload); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	// 数据源区块：清单之外的键一律拒绝（登记表是唯一事实源）
	for id := range payload.Sources {
		if !registry.Exists(id) {
			badRequest(w, "未知的来源："+id)
			return
		}
	}
	// 凭据区块：只接受登记表声明的 (来源, 字段)；掩码回显与空值保持现有配置
	for src, fields := range payload.Credentials {
		for k := range fields {
			if !registry.DeclaredCredential(src, k) {
				badRequest(w, "该数据源不接受此凭据字段："+src+"/"+k)
				return
			}
		}
	}
	cur, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	// 真实 IP 可信网段：逐个校验 CIDR 合法性，非法拒绝（错网段会让限流
	// 按代理 IP 计数，等同失效）
	for _, part := range strings.Split(payload.Security.TrustedProxyCIDRs, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(part); err != nil {
			badRequest(w, "可信网段格式错误："+part+"（应为 CIDR，如 173.245.48.0/20）")
			return
		}
	}
	// DoH 入口：逐个校验为 http(s) URL
	for _, ep := range payload.Network.DoHEndpoints {
		if u, err := url.Parse(strings.TrimSpace(ep)); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			badRequest(w, "DoH 入口格式错误："+ep+"（应为 https:// 开头的解析端点）")
			return
		}
	}
	next := cur
	next.Crawl = payload.Crawl
	next.Cache = payload.Cache
	next.Security = payload.Security
	if payload.Server != nil {
		next.Server = *payload.Server
	}
	next.Network = payload.Network
	if payload.Sources != nil {
		next.Sources = payload.Sources
	}
	if payload.Credentials != nil {
		if next.Credentials == nil {
			next.Credentials = map[string]map[string]string{}
		}
		for src, fields := range payload.Credentials {
			for k, val := range fields {
				if val == "" || strings.Contains(val, "•") {
					continue
				}
				if next.Credentials[src] == nil {
					next.Credentials[src] = map[string]string{}
				}
				next.Credentials[src][k] = val
			}
		}
	}

	if payload.BGM.ClearToken {
		next.BGM.Token = ""
	} else if v := strings.TrimSpace(payload.BGM.Token); v != "" && !strings.Contains(v, "•") {
		next.BGM.Token = v
	}
	if payload.BGM.ClearCookie {
		next.BGM.Cookie = ""
	} else if v := strings.TrimSpace(payload.BGM.Cookie); v != "" && !strings.Contains(v, "•") {
		next.BGM.Cookie = v
	}

	if err := s.store.SaveSettings(r.Context(), &next); err != nil {
		internalError(w, err)
		return
	}
	// 保存即立刷运行参数快照（资料序/权重/凭据/每源请求间隔随快照生效），
	// 再丢弃缓存快照让热路径下次请求也读到新值
	s.applySnapshot(next)
	s.invalidateCacheConfig()
	s.invalidateOverview()
	// 评分权重可能已变化：异步全量重算合成评分（不阻塞保存响应），
	// 重算完成后再失效查询缓存，保证公开查询读到合成后的新值。
	go func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.store.SynthesizeScores(sctx); err != nil {
			slog.Error("评分重算失败", "err", err)
			return
		}
		s.invalidateQueryCaches()
		s.invalidateOverview()
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated_at": next.UpdatedAt})
}

// handleAdminTestCredential 按源分发凭据校验：哪些源支持校验由登记表
// Validate 声明（前端按钮的出现同源）。body 可为空（兼容旧调用 = bgm）。
// 扩展源校验用提交的草稿凭据（还没保存也能试），body 未带时用已存值；
// bgm 校验器走既有站长凭据逻辑（已存的 Token/Cookie）。
func (s *Server) handleAdminTestCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source      string            `json:"source"`
		Credentials map[string]string `json:"credentials"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
	}
	source := strings.TrimSpace(body.Source)
	if source == "" {
		source = registry.SourceBGM
	}
	spec, ok := registry.Get(source)
	if !ok || !spec.Validate {
		badRequest(w, "该来源不支持凭据校验")
		return
	}

	if source == registry.SourceBGM {
		s.testBGMCredential(w, r)
		return
	}

	// 扩展源：草稿凭据优先（校验「即将保存」的值），没带就用已存配置
	creds := body.Credentials
	if len(creds) == 0 {
		settings, err := s.store.LoadSettings(r.Context(), s.defaults)
		if err != nil {
			internalError(w, err)
			return
		}
		creds = settings.Credentials[source]
	}
	// 字段按登记表声明裁剪，清单外拒绝
	clean := map[string]string{}
	for _, f := range spec.Credentials {
		if v := strings.TrimSpace(creds[f.Key]); v != "" && !strings.Contains(v, "•") {
			clean[f.Key] = v
		}
	}
	h := s.crawler.Harvester(source)
	v, ok := h.(sources.CredentialValidator)
	if !ok {
		badRequest(w, "该来源不支持凭据校验")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := v.ValidateCredentials(ctx, clean); err != nil {
		fail(w, http.StatusBadGateway, "credential_invalid",
			spec.Label+" 凭据校验失败", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "source": source,
		"mode": map[bool]string{true: "read_token", false: "api_key"}[clean["read_token"] != ""],
	})
}

// testBGMCredential 校验站长凭据：有 Token 用 Token 验身份，没有就验证匿名
// 通道可达（bgm 的 Token/Cookie 存在 BGM 设置区块，不走提交值）。
func (s *Server) testBGMCredential(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	client := s.bgmClient(settings)
	if settings.BGM.Token != "" {
		me, err := client.Me(r.Context())
		if err != nil {
			fail(w, http.StatusBadGateway, "credential_invalid", "凭据校验失败", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "source": registry.SourceBGM, "mode": "token",
			"user_id": me.ID, "username": me.Username, "nickname": me.Nickname,
		})
		return
	}
	sub, err := client.Subject(r.Context(), 2)
	if err != nil {
		fail(w, http.StatusBadGateway, "upstream_unreachable", "无法连接 Bangumi API", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "source": registry.SourceBGM, "mode": "anonymous", "sample": sub.Name,
	})
}

func (s *Server) handleAdminStartCrawl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode   string `json:"mode"`
		Source string `json:"source"` // 指定数据源时按单源抓取执行（模式须在该源声明的能力内）
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	var (
		runID int64
		err   error
	)
	if strings.TrimSpace(body.Source) != "" {
		runID, err = s.crawler.StartSource(body.Mode, strings.TrimSpace(body.Source), "manual")
	} else {
		runID, err = s.crawler.Start(body.Mode, "manual")
	}
	if err != nil {
		if err == crawler.ErrBusy {
			fail(w, http.StatusConflict, "busy", "已有抓取任务在运行", "")
			return
		}
		badRequest(w, err.Error())
		return
	}
	s.invalidateOverview()
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": runID, "status": s.crawler.Status()})
}

func (s *Server) handleAdminCancelCrawl(w http.ResponseWriter, r *http.Request) {
	if !s.crawler.Cancel() {
		fail(w, http.StatusConflict, "not_running", "当前没有正在运行的抓取任务", "")
		return
	}
	s.invalidateOverview()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAdminRematchWorks 重跑作品匹配：对仍自成作品的扩展源行重新归组
// （先补 anilist 的 mal_id 确定性键）。抓取运行期间拒绝（共享同一批行）。
func (s *Server) handleAdminRematchWorks(w http.ResponseWriter, r *http.Request) {
	if s.crawler.Status().Running {
		fail(w, http.StatusConflict, "busy", "已有抓取任务在运行，稍后再重新配对作品", "")
		return
	}
	stats, err := s.crawler.RematchWorks(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "rematch_failed", "重新配对作品失败", err.Error())
		return
	}
	// 分组变化会改变浏览结果与统计口径：全量失效查询缓存
	s.invalidateQueryCaches()
	s.invalidateOverview()
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAdminRuns(w http.ResponseWriter, r *http.Request) {
	q := store.RunQuery{
		Source: r.URL.Query().Get("source"),
		Mode:   r.URL.Query().Get("mode"),
		Status: r.URL.Query().Get("status"),
		Sort:   r.URL.Query().Get("sort"),
		Order:  r.URL.Query().Get("order"),
		Limit:  parseIntParam(r, "limit", 20),
		Offset: parseIntParam(r, "offset", 0),
	}
	runs, total, err := s.store.ListRuns(r.Context(), q)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": runs, "total": total, "limit": q.Limit, "offset": q.Offset,
	})
}

func (s *Server) handleAdminRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "无效的任务 ID")
		return
	}
	run, err := s.store.GetRun(r.Context(), id)
	if err == store.ErrNotFound {
		fail(w, http.StatusNotFound, "not_found", "任务不存在", "")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleAdminSubjects(w http.ResponseWriter, r *http.Request) {
	q := store.SearchQuery{
		Keyword:     r.URL.Query().Get("keyword"),
		Types:       parseIntCSV(r, "types"),
		Tags:        parseCSV(r, "tags"),
		ExcludeTags: parseCSV(r, "exclude"),
		TagMode:     r.URL.Query().Get("tag_mode"),
		PctMin:      parseFloatParam(r, "pct_min"),
		MetaTags:    parseCSV(r, "meta_tags"),
		RankMin:     parseIntParam(r, "rank_min", 0),
		RankMax:     parseIntParam(r, "rank_max", 0),
		NSFW:        "include",
		Sort:        r.URL.Query().Get("sort"),
		Order:       r.URL.Query().Get("order"),
		Page:        parseIntParam(r, "page", 1),
		PageSize:    parseIntParam(r, "page_size", 20),
	}
	if !validText(q.Keyword) || !validTexts(q.Tags) || !validTexts(q.ExcludeTags) || !validTexts(q.MetaTags) {
		badRequest(w, "请求参数含非法字符")
		return
	}
	res, err := s.store.Search(r.Context(), q)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAdminRefreshSubject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	sub, err := s.crawler.RefreshOne(r.Context(), id)
	if err != nil {
		fail(w, http.StatusBadGateway, "refresh_failed", "刷新失败", err.Error())
		return
	}
	s.invalidateSubject(id)
	s.invalidateQueryCaches()
	s.invalidateOverview()
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleAdminDeleteSubject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	if err := s.store.DeleteSubject(r.Context(), id); err != nil {
		if err == store.ErrNotFound {
			fail(w, http.StatusNotFound, "not_found", "条目不存在", "")
			return
		}
		internalError(w, err)
		return
	}
	s.invalidateQueryCaches()
	s.invalidateOverview()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	if len(strings.TrimSpace(body.Next)) < 6 {
		badRequest(w, "新密码至少 6 位")
		return
	}
	ok, err := s.store.VerifyAdminPassword(r.Context(), body.Current)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusUnauthorized, "invalid_password", "当前密码错误", "")
		return
	}
	if err := s.store.SetAdminPassword(r.Context(), strings.TrimSpace(body.Next)); err != nil {
		internalError(w, err)
		return
	}

	// 使旧会话全部失效，并为当前浏览器换发新会话（有效期走系统设置）。
	_ = s.store.DeleteAllSessions(r.Context())
	snap := s.snapshot(r.Context())
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	if err := s.store.CreateSession(r.Context(), token, snap.sessionTTL); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: cookieSecure(r), MaxAge: int(snap.sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAdminNetwork 抓取出口诊断：按当前设置重建一次性 DoH 解析器做 DNS
// 诊断（出站通道本体由 swapOutbound 管理），并用当前出站客户端探测上游。
func (s *Server) handleAdminNetwork(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	net := settings.Network
	out := map[string]any{
		"doh":   net.DoH,
		"ech":   net.DoH && net.ECH,
		"proxy": strings.TrimSpace(net.EgressProxy) != "",
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if net.DoH {
		endpoints := net.DoHEndpoints
		if len(endpoints) == 0 {
			endpoints = []string{"https://dns.alidns.com/resolve", "https://1.1.1.1/dns-query"}
		}
		resolver := netx.NewResolver(endpoints, net.ECH)
		out["diagnostics"] = resolver.Diagnose(ctx, "api.bgm.tv")
	}
	if s.httpClient != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.bgm.tv/v0/subjects/2", nil)
		if err == nil {
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "AniPick/1.0 (+https://github.com/bangumi; local indexer)")
			if resp, derr := s.httpClient.Do(req); derr == nil {
				out["upstream_ok"] = resp.StatusCode == http.StatusOK
				out["upstream_status"] = resp.StatusCode
				_ = resp.Body.Close()
			} else {
				out["upstream_ok"] = false
				out["upstream_error"] = derr.Error()
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
