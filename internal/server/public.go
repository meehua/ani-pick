package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"anipick/internal/db"
	"anipick/internal/store"
)

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
	return fmt.Sprintf("%s|%v|%v|%v|%s|%v|%s|%s|%d|%d|%g|%g|%d|%s|%s|%s|%d|%d",
		strings.TrimSpace(q.Keyword), types, norm(q.Tags), norm(q.ExcludeTags), q.TagMode,
		norm(q.MetaTags), q.DateFrom, q.DateTo, q.RankMin, q.RankMax,
		q.ScoreMin, q.ScoreMax, q.RatingMin, q.NSFW, q.Sort, q.Order, q.Page, q.PageSize)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := store.SearchQuery{
		Keyword:     r.URL.Query().Get("keyword"),
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
		return json.Marshal(sub)
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
		"stats":   stats,
		"crawler": s.crawler.Status(),
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
