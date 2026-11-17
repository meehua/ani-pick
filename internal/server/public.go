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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "db": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": "up", "time": time.Now()})
}

// 公开 GET 端点的浏览器/CDN 缓存时长；列表类数据只在抓取后变化，30s 陈旧无感。
const publicCacheTTL = 30 * time.Second

// searchPageCacheTTL 是搜索页结果的进程内缓存：并发访客的相同视图（首页/浏览页
// 默认列表等）共享同一份结果字节，省掉重复查询与 JSON 编码。
const (
	searchPageCacheTTL = 5 * time.Second
	searchPageMaxKeys  = 256
)

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
	if q.Page > 5000 {
		q.Page = 5000
	}
	key := searchCacheKey(q)
	s.pageMu.Lock()
	if e, ok := s.pageCache[key]; ok && time.Since(e.at) < searchPageCacheTTL {
		body := e.body
		s.pageMu.Unlock()
		writeJSONTTL(w, http.StatusOK, body, publicCacheTTL)
		return
	}
	s.pageMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	res, err := s.store.Search(ctx, q)
	if err != nil {
		internalError(w, err)
		return
	}
	payload, err := json.Marshal(res)
	if err != nil {
		internalError(w, err)
		return
	}
	s.pageMu.Lock()
	if len(s.pageCache) >= searchPageMaxKeys {
		s.pageCache = map[string]cachedJSON{} // 查询组合无界，超限整体换新
	}
	s.pageCache[key] = cachedJSON{at: time.Now(), body: payload}
	s.pageMu.Unlock()
	writeJSONTTL(w, http.StatusOK, payload, publicCacheTTL)
}

func (s *Server) handleSubjectIndex(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	sub, err := s.store.GetSubject(r.Context(), id)
	if err == store.ErrNotFound {
		fail(w, http.StatusNotFound, "not_indexed", "本站索引中没有这个条目", "可在管理后台手动刷新，或等待抓取任务覆盖")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.TagSuggest(r.Context(), r.URL.Query().Get("q"), parseIntCSV(r, "types"), parseIntParam(r, "limit", 30))
	if err != nil {
		internalError(w, err)
		return
	}
	payload, err := json.Marshal(map[string]any{"items": tags})
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSONTTL(w, http.StatusOK, payload, publicCacheTTL)
}

type cachedMetaTags struct {
	at   time.Time
	data []store.TagStat
}

// metaTagsTTL 是元标签候选的缓存时长；条目上限用于淘汰长期不用的参数组合。
const (
	metaTagsTTL      = 10 * time.Minute
	metaTagsMaxKeys  = 64
	metaCacheTTL     = 30 * time.Second
	overviewCacheTTL = 3 * time.Second
)

func (s *Server) handleMetaTags(w http.ResponseWriter, r *http.Request) {
	typ := parseIntParam(r, "type", 0)
	limit := parseIntParam(r, "limit", 40)
	key := strconv.Itoa(typ) + ":" + strconv.Itoa(limit)

	s.cacheMu.Lock()
	if e, ok := s.metaTags[key]; ok && time.Since(e.at) < metaTagsTTL {
		data := e.data
		s.cacheMu.Unlock()
		writeJSONTTL(w, http.StatusOK, mustMarshalItems(data), publicCacheTTL)
		return
	}
	s.cacheMu.Unlock()

	items, err := s.store.MetaTagSuggest(r.Context(), typ, limit)
	if err != nil {
		internalError(w, err)
		return
	}
	s.cacheMu.Lock()
	if len(s.metaTags) >= metaTagsMaxKeys {
		// 参数组合有限，整体重置比逐键清理更简单
		s.metaTags = map[string]cachedMetaTags{}
	}
	s.metaTags[key] = cachedMetaTags{at: time.Now(), data: items}
	s.cacheMu.Unlock()
	writeJSONTTL(w, http.StatusOK, mustMarshalItems(items), publicCacheTTL)
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
	s.metaMu.Lock()
	body, fresh := s.metaBody, time.Since(s.metaBody.at) < metaCacheTTL
	s.metaMu.Unlock()
	if fresh && body.body != nil {
		writeJSONTTL(w, http.StatusOK, body.body, publicCacheTTL)
		return
	}

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
	s.metaMu.Lock()
	s.metaBody = cachedJSON{at: time.Now(), body: payload}
	s.metaMu.Unlock()
	writeJSONTTL(w, http.StatusOK, payload, publicCacheTTL)
}
