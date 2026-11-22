package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"anipick/internal/db"
	"anipick/internal/registry"
	"anipick/internal/sources"
	"anipick/internal/store"
)

// errDirectOnly 标记请求的是详情策略为「浏览器直连」的来源（不经服务端代理）。
var errDirectOnly = errors.New("该来源详情由浏览器直连获取")

func parseIntParam(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func parseFloatParam(r *http.Request, key string) float64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseCSV(r *http.Request, key string) []string {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseIntCSV(r *http.Request, key string) []int {
	var out []int
	for _, p := range parseCSV(r, key) {
		n, err := strconv.Atoi(p)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

// validText 校验用户可控文本：必须是合法 UTF-8 且不含 NUL——Postgres 会拒绝
// 这两类输入，放行会让查询直接变 500。校验必须在触碰缓存与数据库之前完成。
func validText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, '\x00')
}

func validTexts(ss []string) bool {
	for _, s := range ss {
		if !validText(s) {
			return false
		}
	}
	return true
}

// validDate 校验日期参数（YYYY-MM-DD）。宽松写法（如 zzz、2020-02-30）会打到
// SQL 的 ::date 转换报错变 500，必须在 handler 侧拦成 400。
func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "db": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": "up", "time": time.Now()})
}

// 公开 GET 端点的浏览器/CDN 缓存与进程内查询缓存的默认时长；实际值存系统设置
// （AppSettings.Cache，管理后台「设置 → 查询缓存」可调），server 侧持有 60s 快照。
// 列表类数据只在抓取后变化，失效走显式钩子（invalidateQueryCaches），TTL 仅兜底。

// searchPageMaxKeys 是搜索页结果缓存的键上限（查询组合无界，超限整体换新）；
// TTL 走系统设置（AppSettings.Cache.query_ttl）。
const searchPageMaxKeys = 256

// searchCacheKey 生成与查询语义一一对应的缓存键：列表参数排序去重、关键词去空白，
// 使参数顺序不同的等价查询命中同一份缓存。
func searchCacheKey(q store.SearchQuery) string {
	types := append([]int(nil), q.Types...)
	sort.Ints(types)
	norm := func(in []string) []string {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(in))
		for _, v := range in {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
		sort.Strings(out)
		return out
	}
	return fmt.Sprintf("%s|%v|%v|%v|%s|%v|%s|%s|%d|%d|%g|%g|%d|%s|%s|%s|%s|%d|%d",
		strings.TrimSpace(q.Keyword), types, norm(q.Tags), norm(q.ExcludeTags), q.TagMode,
		norm(q.MetaTags), q.DateFrom, q.DateTo, q.RankMin, q.RankMax,
		q.ScoreMin, q.ScoreMax, q.RatingMin, q.NSFW, q.Source, q.Sort, q.Order, q.Page, q.PageSize)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source != "" && !store.ValidSource(source) {
		badRequest(w, "未知的数据来源，可选 "+strings.Join(store.AllSources(), " / "))
		return
	}
	q := store.SearchQuery{
		Keyword:     r.URL.Query().Get("keyword"),
		Source:      source,
		Types:       parseIntCSV(r, "types"),
		Tags:        parseCSV(r, "tags"),
		ExcludeTags: parseCSV(r, "exclude"),
		TagMode:     r.URL.Query().Get("tag_mode"),
		PctMin:      parseFloatParam(r, "pct_min"),
		MetaTags:    parseCSV(r, "meta_tags"),
		DateFrom:    r.URL.Query().Get("date_from"),
		DateTo:      r.URL.Query().Get("date_to"),
		RankMin:     parseIntParam(r, "rank_min", 0),
		RankMax:     parseIntParam(r, "rank_max", 0),
		ScoreMin:    parseFloatParam(r, "score_min"),
		ScoreMax:    parseFloatParam(r, "score_max"),
		RatingMin:   parseIntParam(r, "rating_min", 0),
		NSFW:        r.URL.Query().Get("nsfw"),
		Sort:        r.URL.Query().Get("sort"),
		Order:       r.URL.Query().Get("order"),
		Page:        parseIntParam(r, "page", 1),
		PageSize:    parseIntParam(r, "page_size", 24),
	}
	// 入参校验前置：非法输入直接 400，不进缓存也不打库
	if !validText(q.Keyword) || !validTexts(q.Tags) || !validTexts(q.ExcludeTags) || !validTexts(q.MetaTags) {
		badRequest(w, "请求参数含非法字符")
		return
	}
	if !validDate(q.DateFrom) || !validDate(q.DateTo) {
		badRequest(w, "日期格式应为 YYYY-MM-DD")
		return
	}
	snap := s.snapshot(r.Context())
	if q.Page > snap.maxPage {
		q.Page = snap.maxPage
	}
	key := searchCacheKey(q)
	if snap.queryTTL > 0 { // 0 = 关闭搜索缓存：每次都回源
		s.pageMu.Lock()
		if e, ok := s.pageCache[key]; ok && time.Since(e.at) < snap.queryTTL {
			body := e.body
			s.pageMu.Unlock()
			s.m.cacheEvent("search", "hit")
			writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
			return
		}
		s.pageMu.Unlock()
	}

	// 缓存未命中：经单飞合并并发的同键回源（TTL 到期瞬间的重复请求只查一次库）。
	// 查询用独立超时的后台 ctx——leader 客户端断开不应让等待中的 follower 一起失败；
	// 请求链路的 DB 计时累积器挂到后台 ctx，性能面板的 DB 归属不丢（follower 同享）。
	body, err, merged := s.flights.do("search:"+key, func() ([]byte, error) {
		tr := db.TraceFrom(r.Context())
		ctx, cancel := context.WithTimeout(db.ContextWithTrace(context.Background(), tr), 25*time.Second)
		defer cancel()
		res, err := s.store.Search(ctx, q)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}
		if snap.queryTTL > 0 {
			s.pageMu.Lock()
			if len(s.pageCache) >= snap.maxKeys {
				s.pageCache = map[string]cachedJSON{} // 查询组合无界，超限整体换新
			}
			s.pageCache[key] = cachedJSON{at: time.Now(), body: payload}
			s.pageMu.Unlock()
		}
		return payload, nil
	})
	if merged {
		s.m.cacheEvent("search", "merged")
	} else {
		s.m.cacheEvent("search", "origin")
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
}

// subjectSourceRow 是详情响应里作品组的一个来源行（多源评分对比/简介补缺用）。
// Name/Platform/Date/Image 供页面区分同组多行（如 AniList 的剧场版与 TV 各一行）。
type subjectSourceRow struct {
	ID          int64   `json:"id"`
	Source      string  `json:"source"`
	Score       float64 `json:"score"`
	RatingTotal int     `json:"rating_total"`
	URL         string  `json:"url"`
	Name        string  `json:"name,omitempty"`
	NameCN      string  `json:"name_cn,omitempty"`
	Platform    string  `json:"platform,omitempty"`
	Date        string  `json:"date,omitempty"`
	Image       string  `json:"image,omitempty"`
}

// subjectIndexResponse 是 /api/subjects/{id} 的响应：索引行 + 作品组的来源清单。
type subjectIndexResponse struct {
	store.Subject
	Sources []subjectSourceRow `json:"sources"`
}

// toSourceRows 把组内来源行映射成响应结构（外链在各源的官方站点）。
func toSourceRows(group []store.GroupSource) []subjectSourceRow {
	rows := make([]subjectSourceRow, 0, len(group))
	for _, g := range group {
		rows = append(rows, subjectSourceRow{
			ID:          g.ID,
			Source:      g.Source,
			Score:       g.Score,
			RatingTotal: g.RatingTotal,
			URL:         sources.SubjectURL(g.Source, g.SourceRef),
			Name:        g.Name,
			NameCN:      g.NameCN,
			Platform:    g.Platform,
			Date:        g.Date,
			Image:       g.Image,
		})
	}
	return rows
}

func (s *Server) handleSubjectIndex(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	snap := s.snapshot(r.Context())
	key := strconv.FormatInt(id, 10)
	if snap.queryTTL > 0 { // 0 = 关闭详情缓存：每次都回源
		s.detailMu.Lock()
		if e, ok := s.detailCache[key]; ok && time.Since(e.at) < snap.queryTTL {
			body := e.body
			s.detailMu.Unlock()
			s.m.cacheEvent("detail", "hit")
			writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
			return
		}
		s.detailMu.Unlock()
	}

	// 单飞合并同 id 并发回源；404 不缓存——条目可能随下次抓取入库
	body, err, merged := s.flights.do("detail:"+key, func() ([]byte, error) {
		tr := db.TraceFrom(r.Context())
		ctx, cancel := context.WithTimeout(db.ContextWithTrace(context.Background(), tr), 10*time.Second)
		defer cancel()
		sub, err := s.store.GetSubject(ctx, id)
		if err != nil {
			return nil, err
		}
		// 作品组的全部来源行（一次查询取齐禁 N+1）：多源评分对比行 + 简介补缺
		group, err := s.store.GroupSources(ctx, id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(subjectIndexResponse{Subject: sub, Sources: toSourceRows(group)})
	})
	if merged {
		s.m.cacheEvent("detail", "merged")
	} else {
		s.m.cacheEvent("detail", "origin")
	}
	if err == store.ErrNotFound {
		fail(w, http.StatusNotFound, "not_indexed", "本站索引中没有这个条目", "可在管理后台手动刷新，或等待抓取任务覆盖")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if snap.queryTTL > 0 {
		s.detailMu.Lock()
		if len(s.detailCache) >= snap.maxKeys {
			s.detailCache = map[string]cachedJSON{} // id 空间无界，超限整体换新
		}
		s.detailCache[key] = cachedJSON{at: time.Now(), body: body}
		s.detailMu.Unlock()
	}
	writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
}

// handleSubjectExtDetail 是扩展源（tmdb/itunes/mal/anilist）的详情代理端点。
// 上游 API key（TMDB）与共享出口限速都留在服务端；bgm 详情由浏览器直连，不经此端点。
// 结果缓存复用 detailCache（键加 ext: 前缀），失效与索引详情一致：
// 抓取收尾全量清空、管理端刷新/删除按 id 失效（invalidateSubject）。
func (s *Server) handleSubjectExtDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	snap := s.snapshot(r.Context())
	key := "ext:" + strconv.FormatInt(id, 10)
	if snap.queryTTL > 0 { // 0 = 关闭详情缓存：每次都回源
		s.detailMu.Lock()
		if e, ok := s.detailCache[key]; ok && time.Since(e.at) < snap.queryTTL {
			body := e.body
			s.detailMu.Unlock()
			s.m.cacheEvent("ext-detail", "hit")
			writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
			return
		}
		s.detailMu.Unlock()
	}

	body, err, merged := s.flights.do("extdetail:"+key, func() ([]byte, error) {
		// 先读索引行确定来源与 source_ref（脱离请求的后台 ctx，超时显式给足）
		tr := db.TraceFrom(r.Context())
		ctx, cancel := context.WithTimeout(db.ContextWithTrace(context.Background(), tr), 10*time.Second)
		defer cancel()
		sub, err := s.store.GetSubject(ctx, id)
		if err != nil {
			return nil, err
		}
		if sub.Source == "" {
			return nil, errDirectOnly
		}
		// 详情获取策略由来源登记表声明：直连来源（原生索引）不经服务端代理
		if src, ok := registry.Get(sub.Source); !ok || src.Detail != registry.DetailProxy {
			return nil, errDirectOnly
		}
		detailer, ok := s.detailers[sub.Source]
		if !ok {
			return nil, fmt.Errorf("未知数据源 %q", sub.Source)
		}
		// 上游请求显式超时：长动作不继承默认超时
		uctx, ucancel := context.WithTimeout(db.ContextWithTrace(context.Background(), tr), 20*time.Second)
		defer ucancel()
		d, err := detailer.Detail(uctx, sub.SourceRef)
		if err != nil {
			return nil, err
		}
		return json.Marshal(d)
	})
	if merged {
		s.m.cacheEvent("ext-detail", "merged")
	} else {
		s.m.cacheEvent("ext-detail", "origin")
	}
	switch {
	case err == nil:
	case errors.Is(err, errDirectOnly):
		fail(w, http.StatusBadRequest, "bgm_direct_only",
			"该来源条目详情由浏览器直连获取，不经服务端代理", "前端详情页会按条目来源自动分派")
		return
	case errors.Is(err, sources.ErrNotConfigured):
		fail(w, http.StatusServiceUnavailable, "source_not_configured",
			"该数据源未配置，无法获取详情", "在管理后台「设置 → 数据源」填写对应凭据后重试")
		return
	case errors.Is(err, sources.ErrNotFound):
		fail(w, http.StatusBadGateway, "upstream_not_found",
			"上游已不存在这条记录", "")
		return
	case err == store.ErrNotFound:
		fail(w, http.StatusNotFound, "not_indexed", "本站索引中没有这个条目", "")
		return
	default:
		slog.Error("扩展源详情代理失败", "err", err)
		fail(w, http.StatusBadGateway, "upstream_error",
			"上游详情获取失败", "稍后重试；详情结果已缓存，命中后不再回源")
		return
	}
	if snap.queryTTL > 0 {
		s.detailMu.Lock()
		if len(s.detailCache) >= snap.maxKeys {
			s.detailCache = map[string]cachedJSON{} // id 空间无界，超限整体换新
		}
		s.detailCache[key] = cachedJSON{at: time.Now(), body: body}
		s.detailMu.Unlock()
	}
	writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	types := parseIntCSV(r, "types")
	limit := parseIntParam(r, "limit", 30)
	if !validText(q) {
		badRequest(w, "请求参数含非法字符")
		return
	}
	snap := s.snapshot(r.Context())
	key := tagCacheKey(q, types, limit)

	if snap.queryTTL > 0 { // 0 = 关闭标签候选缓存：每次都回源
		s.tagMu.Lock()
		if e, ok := s.tagCache[key]; ok && time.Since(e.at) < snap.queryTTL {
			body := e.body
			s.tagMu.Unlock()
			s.m.cacheEvent("tags", "hit")
			writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
			return
		}
		s.tagMu.Unlock()
	}

	// 标签候选是搜索框逐键请求的高频端点（每键一次查询），必须进程内缓存；
	// 数据只在抓取后随 RecomputeTagUsage 变化，失效挂抓取收尾钩子，TTL 兜底。
	body, err, merged := s.flights.do("tags:"+key, func() ([]byte, error) {
		tr := db.TraceFrom(r.Context())
		ctx, cancel := context.WithTimeout(db.ContextWithTrace(context.Background(), tr), 10*time.Second)
		defer cancel()
		tags, err := s.store.TagSuggest(ctx, q, types, limit)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(map[string]any{"items": tags})
		if err != nil {
			return nil, err
		}
		if snap.queryTTL > 0 {
			s.tagMu.Lock()
			if len(s.tagCache) >= snap.maxKeys {
				s.tagCache = map[string]cachedJSON{} // 输入型 key 无界增长，超限整体换新
			}
			s.tagCache[key] = cachedJSON{at: time.Now(), body: payload}
			s.tagMu.Unlock()
		}
		return payload, nil
	})
	if merged {
		s.m.cacheEvent("tags", "merged")
	} else {
		s.m.cacheEvent("tags", "origin")
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSONTTL(w, http.StatusOK, body, snap.publicTTL)
}

// tagCacheKey 归一化标签候选查询：关键词去空白、类型排序，等价请求共享同一份缓存。
func tagCacheKey(q string, types []int, limit int) string {
	sort.Ints(types)
	return fmt.Sprintf("%s|%v|%d", q, types, limit)
}

type cachedMetaTags struct {
	at   time.Time
	data []store.TagStat
}

// 进程内缓存的键上限（内存有界）；各缓存 TTL 走系统设置（查询缓存），不在此写死。
const metaTagsMaxKeys = 64

func (s *Server) handleMetaTags(w http.ResponseWriter, r *http.Request) {
	typ := parseIntParam(r, "type", 0)
	limit := parseIntParam(r, "limit", 40)
	key := strconv.Itoa(typ) + ":" + strconv.Itoa(limit)
	snap := s.snapshot(r.Context())

	if snap.metaTagsTTL > 0 { // 0 = 关闭元标签缓存：每次都回源
		s.cacheMu.Lock()
		if e, ok := s.metaTags[key]; ok && time.Since(e.at) < snap.metaTagsTTL {
			data := e.data
			s.cacheMu.Unlock()
			s.m.cacheEvent("meta-tags", "hit")
			writeJSONTTL(w, http.StatusOK, mustMarshalItems(data), snap.publicTTL)
			return
		}
		s.cacheMu.Unlock()
	}
	s.m.cacheEvent("meta-tags", "origin")

	items, err := s.store.MetaTagSuggest(r.Context(), typ, limit)
	if err != nil {
		internalError(w, err)
		return
	}
	if snap.metaTagsTTL > 0 {
		s.cacheMu.Lock()
		if len(s.metaTags) >= metaTagsMaxKeys {
			// 参数组合有限，整体重置比逐键清理更简单
			s.metaTags = map[string]cachedMetaTags{}
		}
		s.metaTags[key] = cachedMetaTags{at: time.Now(), data: items}
		s.cacheMu.Unlock()
	}
	writeJSONTTL(w, http.StatusOK, mustMarshalItems(items), snap.publicTTL)
}

// mustMarshalItems 序列化标签候选列表；TagStat 字段全是基础类型，不会失败。
func mustMarshalItems(items []store.TagStat) []byte {
	body, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		// 理论上不可达；兜底空列表，避免调用方处理错误分支
		return []byte(`{"items":[]}`)
	}
	return body
}

// manifestType 是来源 manifest 里的一个类型词条（登记表词表的下发形态）。
// Label 是该源的原生标签（源资产，站点不改名）；Kind 是站点级分类（外层词表）。
type manifestType struct {
	Code  int    `json:"code"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"` // 站点级分类（kind 层渲染与合并匹配用）
}

// manifestCred 是来源 manifest 里的一个凭据字段声明（设置页照单渲染）。
type manifestCred struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
}

// manifestSource 是来源登记表下发给前端的公开清单行：
// 徽标文案、详情获取策略、优先序、类型词汇、抓取能力、凭据声明、配置状态全部以这里为准。
type manifestSource struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Native          bool   `json:"native"`
	Priority        int    `json:"priority"`
	Detail          string `json:"detail"` // proxy=服务端代理 | direct=浏览器直连
	Configured      bool   `json:"configured"`
	Validate        bool   `json:"validate"`                 // 登记表声明支持凭据校验（前端才渲染校验按钮）
	IntervalDefault int    `json:"interval_default"`         // 请求间隔默认值（ms，官方限速口径）
	IntervalFloor   int    `json:"interval_floor,omitempty"` // 请求间隔硬下限（ms）
	// 收获预算声明（0 = 该源无此参数；前端照单渲染每源预算输入并写默认值提示）
	FullPages   int            `json:"full_pages,omitempty"`
	IncPages    int            `json:"inc_pages,omitempty"`
	PageSize    int            `json:"page_size,omitempty"`     // 单页条数默认
	PageSizeMax int            `json:"page_size_max,omitempty"` // 单页条数上游硬顶
	TagMinRank  int            `json:"tag_min_rank,omitempty"`  // 标签相关度门槛默认
	Region      string         `json:"region,omitempty"`        // storefront 地区默认
	Types       []manifestType `json:"types"`
	Modes       []manifestMode `json:"modes,omitempty"` // 该源支持的抓取模式（抓取设置页按此渲染按钮组）
	Credentials []manifestCred `json:"credentials,omitempty"`
}

// manifestKind 是站点级分类词表的下发形态（双层词表的外层）。
type manifestKind struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"` // 首页类型卡副文案
}

// manifestMode 是来源支持的一个抓取模式（登记表能力清单的下发形态）。
type manifestMode struct {
	Mode  string `json:"mode"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// manifestKinds 组装站点级分类词表（登记表 KindOrder 顺序）。
func manifestKinds() []manifestKind {
	out := make([]manifestKind, 0, len(registry.KindOrder))
	for _, k := range registry.KindOrder {
		out = append(out, manifestKind{Kind: string(k), Label: registry.KindLabel(k), Hint: registry.KindHint(k)})
	}
	return out
}

// sourceManifest 组装来源公开清单：登记表全量行按有效优先级排序，
// 只含设置启用中的源；类型词条按登记表展示序排列。
func (s *Server) sourceManifest(snap cfgSnapshot) []manifestSource {
	configured := map[string]bool{}
	for _, h := range s.crawler.Harvesters() {
		configured[h.Source()] = h.Configured()
	}
	out := make([]manifestSource, 0)
	for _, src := range registry.All() {
		if v, ok := snap.sources[src.ID]; ok && !v.Enabled {
			continue // 停用的源不进 manifest
		}
		ms := manifestSource{
			ID:       src.ID,
			Label:    src.Label,
			Native:   src.Native,
			Priority: store.SourcePriority(src.ID, snap.sources),
			Detail:   string(src.Detail),
			Validate: src.Validate,
			// 每源请求间隔的默认与下限（登记表按官方限速口径声明），
			// 展开区的间隔输入用默认值做 placeholder、下限做提示。
			IntervalDefault: src.Harvest.IntervalMS,
			IntervalFloor:   src.Harvest.IntervalFloorMS,
			FullPages:       src.Harvest.FullPages,
			IncPages:        src.Harvest.IncPages,
			PageSize:        src.Harvest.PageSize,
			PageSizeMax:     src.Harvest.PageSizeMax,
			TagMinRank:      src.Harvest.TagMinRank,
			Region:          src.Harvest.Region,
			// 声明了凭据字段的源看适配器配置状态（凭据是否已填）；其余恒可用
			Configured: len(src.Credentials) == 0 || configured[src.ID],
		}
		for _, f := range src.Credentials {
			ms.Credentials = append(ms.Credentials, manifestCred{Key: f.Key, Label: f.Label, Secret: f.Secret})
		}
		seen := map[int]struct{}{}
		for _, code := range registry.TypeOrder {
			label, ok := src.Types[code]
			if !ok {
				continue
			}
			seen[code] = struct{}{}
			ms.Types = append(ms.Types, manifestType{Code: code, Label: label, Kind: string(src.Kinds[code])})
		}
		// 登记表声明了但不在全局展示序里的编码，按编码序补在后面
		for _, code := range sortedCodes(src.Types) {
			if _, ok := seen[code]; ok {
				continue
			}
			ms.Types = append(ms.Types, manifestType{Code: code, Label: src.Types[code], Kind: string(src.Kinds[code])})
		}
		for _, m := range src.Modes {
			ms.Modes = append(ms.Modes, manifestMode{Mode: m, Label: registry.ModeLabels[m], Hint: registry.ModeHints[m]})
		}
		out = append(out, ms)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// sortedCodes 类型编码升序（manifest 兜底段用）。
func sortedCodes(m map[int]string) []int {
	out := make([]int, 0, len(m))
	for code := range m {
		out = append(out, code)
	}
	sort.Ints(out)
	return out
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot(r.Context())
	if snap.metaTTL > 0 { // 0 = 关闭 meta 缓存：每次都实算
		s.metaMu.Lock()
		body, fresh := s.metaBody, time.Since(s.metaBody.at) < snap.metaTTL
		s.metaMu.Unlock()
		if fresh && body.body != nil {
			s.m.cacheEvent("meta", "hit")
			writeJSONTTL(w, http.StatusOK, body.body, snap.publicTTL)
			return
		}
	}
	s.m.cacheEvent("meta", "origin")

	stats, err := s.store.Stats(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	payload, err := json.Marshal(map[string]any{
		"name":    "AniPick",
		"version": s.version,
		"commit":  s.commit,
		"build":   s.buildID(),
		"stats":   stats,
		"crawler": s.crawler.Status(),
		// 来源登记表的公开清单：前端词表/徽标/详情分派/筛选 chips 全部由它驱动
		"sources": s.sourceManifest(snap),
		// 站点级分类词表（双层词表的外层）：浏览 chips/首页类型卡按它渲染
		"kinds": manifestKinds(),
		// 首页快捷入口（登记表 QuickLinks 下发，{year} 占位符由前端替换）
		"quick_links": registry.QuickLinks,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	if snap.metaTTL > 0 {
		s.metaMu.Lock()
		s.metaBody = cachedJSON{at: time.Now(), body: payload}
		s.metaMu.Unlock()
	}
	writeJSONTTL(w, http.StatusOK, payload, snap.publicTTL)
}
