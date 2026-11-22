package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"anipick/internal/registry"
	"anipick/internal/store"
	"time"
)

// TMDB v3 API（官方公开接口，需 API key）。key 只在服务端使用，绝不进浏览器。
// 发现：/discover/movie、/discover/tv（按热度倒序，每页 20 条）；
// 详情：/movie/{id}、/tv/{id}（append_to_response=credits）；类型名：/genre/*/list。
const (
	tmdbBase      = "https://api.themoviedb.org/3"
	tmdbImageBase = "https://image.tmdb.org/t/p"
)

// tmdbSpec 是 TMDB 在登记表里的行（节流/预算/评分换算从这取）。
var tmdbSpec, _ = registry.Get(registry.SourceTMDB)

// TMDB 是 TMDB v3 的适配器。baseURL 可注入以便 httptest 假上游测试。
// API Key 来自凭据快照（后台设置，secretbox 加密存库），不再走环境变量。
type TMDB struct {
	f       *fetcher
	creds   *CredStore
	baseURL string
}

// NewTMDB 构造 TMDB 适配器；凭据快照由 main 装配、server 随设置保存立刷。
// key 为空表示未配置（收获跳过、详情代理报未配置）。
func NewTMDB(creds *CredStore, hc *http.Client) *TMDB {
	return &TMDB{f: newFetcher(hc, specInterval(tmdbSpec), tmdbSpec.ID), creds: creds, baseURL: tmdbBase}
}

// apiKey 读凭据快照里的当前 v3 key（设置保存后立刷生效）。
func (t *TMDB) apiKey() string { return t.creds.Get(tmdbSpec.ID, "api_key") }

// readToken 读凭据快照里的 v4 读访问令牌（可选；有它优先走 Bearer）。
func (t *TMDB) readToken() string { return t.creds.Get(tmdbSpec.ID, "read_token") }

func (t *TMDB) Source() string { return tmdbSpec.ID }
func (t *TMDB) Label() string  { return tmdbSpec.Label }
func (t *TMDB) Types() []int   { return declaredTypes(tmdbSpec) }

// Configured 双鉴权任一存在即可用（v3 API Key 或 v4 读访问令牌）。
func (t *TMDB) Configured() bool { return t.apiKey() != "" || t.readToken() != "" }

// authURL 组装带鉴权的请求地址：有读令牌时走 Bearer（Authorization 头，
// 不把凭据放进 URL），否则用 v3 api_key query 参数——与 TMDB 设置页
// 颁发的两种凭据一一对应。
func (t *TMDB) authURL(path string, q url.Values) (string, map[string]string) {
	if tok := t.readToken(); tok != "" {
		return t.baseURL + path + "?" + q.Encode(), map[string]string{"Authorization": "Bearer " + tok}
	}
	q.Set("api_key", t.apiKey())
	return t.baseURL + path + "?" + q.Encode(), nil
}

// tmdbDiscoverItem 是 /discover/movie 与 /discover/tv 共有的返回字段
// （movie 用 title/original_title/release_date，tv 用 name/original_name/first_air_date）。
type tmdbDiscoverItem struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Name          string  `json:"name"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
	GenreIDs      []int   `json:"genre_ids"`
	Adult         bool    `json:"adult"`
}

type tmdbDiscoverResp struct {
	Page    int                `json:"page"`
	Results []tmdbDiscoverItem `json:"results"`
	Total   int                `json:"total_results"`
	TotalP  int                `json:"total_pages"`
}

type tmdbGenreList struct {
	Genres []tmdbGenre `json:"genres"`
}

type tmdbGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// discover 一次分页请求；kind = movie | tv。
// discover 构造发现查询：since 非零时带上映/开播日期下限（真增量窗口，
// 人气序下头部即新片）；零值 = 全量语义（纯人气序）。
func (t *TMDB) discover(ctx context.Context, kind string, page int, since time.Time) ([]tmdbDiscoverItem, error) {
	if !t.Configured() {
		return nil, ErrNotConfigured
	}
	q := url.Values{}
	q.Set("language", "zh-CN")
	q.Set("sort_by", "popularity.desc")
	q.Set("include_adult", "false")
	q.Set("page", strconv.Itoa(page))
	if !since.IsZero() {
		d := since.Format("2006-01-02")
		if kind == "movie" {
			q.Set("primary_release_date.gte", d)
		} else {
			q.Set("first_air_date.gte", d)
		}
	}
	u, headers := t.authURL("/discover/"+kind, q)
	var out tmdbDiscoverResp
	if err := t.f.getJSONHeaders(ctx, u, &out, headers); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// genreNames 取一次 zh-CN 类型字典（收获每轮每 kind 只请求一次；失败非致命，标签留空）。
func (t *TMDB) genreNames(ctx context.Context, kind string) map[int]string {
	out := map[int]string{}
	if !t.Configured() {
		return out
	}
	q := url.Values{}
	q.Set("language", "zh-CN")
	u, headers := t.authURL("/genre/"+kind+"/list", q)
	var list tmdbGenreList
	if err := t.f.getJSONHeaders(ctx, u, &list, headers); err != nil {
		return out
	}
	for _, g := range list.Genres {
		out[g.ID] = g.Name
	}
	return out
}

// HarvestFull 全量收获：电影 + 剧集各按登记表预算的页数。
func (t *TMDB) HarvestFull(sc *Scope) error {
	sc.fillDefaults(tmdbSpec)
	return t.harvest(sc, sc.FullPages, false)
}

// HarvestIncremental 增量收获：电影 + 剧集各头部（登记表预算）页数。
// HarvestIncremental 增量收获（真增量）：空数据（该源从未完成过增量）按全量
// 预算补齐；否则 discover 带上映/开播日期窗口（≥上次运行日，人气序）——
// 头部即新片，预算=增量页数。上游无变更流，已有条目的变更靠过期重爬。
func (t *TMDB) HarvestIncremental(sc *Scope) error {
	sc.fillDefaults(tmdbSpec)
	if sc.LastIncremental.IsZero() {
		return t.harvest(sc, sc.FullPages, false)
	}
	return t.harvest(sc, sc.IncPages, true)
}

func (t *TMDB) harvest(sc *Scope, pagesPerKind int, window bool) error {
	if !t.Configured() {
		return ErrNotConfigured
	}
	for _, kind := range []string{"movie", "tv"} {
		if sc.Ctx.Err() != nil {
			return sc.Ctx.Err()
		}
		if err := t.harvestKind(sc, kind, pagesPerKind, window); err != nil {
			return err
		}
	}
	return nil
}

func (t *TMDB) harvestKind(sc *Scope, kind string, pages int, window bool) error {
	genres := t.genreNames(sc.Ctx, kind)
	since := time.Time{}
	if window {
		since = sc.LastIncremental
	}
	for page := 1; page <= pages; page++ {
		if sc.Ctx.Err() != nil {
			return sc.Ctx.Err()
		}
		items, err := t.discover(sc.Ctx, kind, page, since)
		if err != nil {
			sc.Add("errors", 1)
			return fmt.Errorf("discover %s 第 %d 页: %w", kind, page, err)
		}
		sc.Add("requests", 1)
		sc.Add("pages", 1)
		if len(items) == 0 {
			return nil
		}
		recs := make([]store.Subject, 0, len(items))
		for _, it := range items {
			recs = append(recs, t.convert(kind, it, genres))
		}
		ins, upd, err := sc.Store.UpsertSubjects(sc.Ctx, recs, sc.RunID)
		if err != nil {
			return fmt.Errorf("写入 %s 第 %d 页: %w", kind, page, err)
		}
		sc.Add("inserted", ins)
		sc.Add("updated", upd)
		sc.Update("enumerate", fmt.Sprintf("TMDB %s：第 %d/%d 页（+新增 %d / 更新 %d）", kind, page, pages, ins, upd))
	}
	return nil
}

// convert 把 discover 条目归一化。zh-CN 请求下 title/name 是本地化标题
// （多为中文，无中文翻译时与原名相同），original_* 是原始标题。
func (t *TMDB) convert(kind string, it tmdbDiscoverItem, genres map[int]string) store.Subject {
	ref := kind + "/" + strconv.Itoa(it.ID)
	name := it.OriginalTitle
	nameCN := it.Title
	typ := registry.KindCode(tmdbSpec.ID, registry.KindMovie)
	date := it.ReleaseDate
	platform := ""
	if kind == "tv" {
		name = it.OriginalName
		nameCN = it.Name
		typ = registry.KindCode(tmdbSpec.ID, registry.KindTV)
		date = it.FirstAirDate
		platform = "剧集"
	}
	names := make([]string, 0, len(it.GenreIDs))
	for _, id := range it.GenreIDs {
		if g, ok := genres[id]; ok {
			names = append(names, g)
		}
	}
	alt := []string{name, nameCN}
	return store.Subject{
		Source:      tmdbSpec.ID,
		SourceRef:   ref,
		Type:        typ,
		Name:        name,
		NameCN:      nameCN,
		AltTitles:   alt,
		Date:        NormalizeDate(date),
		Platform:    platform,
		Rank:        0, // TMDB 无全站排名概念，排名列不参与
		Score:       registry.ScoreOf(tmdbSpec.ID, it.VoteAverage),
		RatingTotal: it.VoteCount,
		NSFW:        it.Adult,
		Image:       tmdbPoster(it.PosterPath),
		Summary:     StripText(it.Overview),
		MetaTags:    []string{},
		TagsTotal:   0, // 扩展源标签无票数，tags_total 恒 0
		Tags:        truncateTags(names),
	}
}

func tmdbPoster(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageBase + "/w500" + path
}

func tmdbBackdrop(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageBase + "/w780" + path
}

// ---------- 详情（服务端代理） ----------

type tmdbGenreRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbCrew struct {
	Name      string `json:"name"`
	Job       string `json:"job"`
	Character string `json:"character"`
}

type tmdbCredits struct {
	Cast []tmdbCrew `json:"cast"`
	Crew []tmdbCrew `json:"crew"`
}

type tmdbCountry struct {
	ISO  string `json:"iso_3166_1"`
	Name string `json:"name"`
}

type tmdbNetwork struct {
	Name string `json:"name"`
}

type tmdbCreator struct {
	Name string `json:"name"`
}

// tmdbDetailItem 是 /movie/{id} 与 /tv/{id}（append_to_response=credits）的合并形状。
type tmdbDetailItem struct {
	ID               int            `json:"id"`
	Title            string         `json:"title"`
	OriginalTitle    string         `json:"original_title"`
	Name             string         `json:"name"`
	OriginalName     string         `json:"original_name"`
	Overview         string         `json:"overview"`
	Tagline          string         `json:"tagline"`
	PosterPath       string         `json:"poster_path"`
	BackdropPath     string         `json:"backdrop_path"`
	ReleaseDate      string         `json:"release_date"`
	FirstAirDate     string         `json:"first_air_date"`
	LastAirDate      string         `json:"last_air_date"`
	Runtime          int            `json:"runtime"`
	EpisodeRunTime   []int          `json:"episode_run_time"`
	NumberOfSeasons  int            `json:"number_of_seasons"`
	NumberOfEpisodes int            `json:"number_of_episodes"`
	Genres           []tmdbGenreRef `json:"genres"`
	VoteAverage      float64        `json:"vote_average"`
	VoteCount        int            `json:"vote_count"`
	Adult            bool           `json:"adult"`
	Status           string         `json:"status"`
	IMDbID           string         `json:"imdb_id"`
	ProductionC      []tmdbCountry  `json:"production_countries"`
	CreatedBy        []tmdbCreator  `json:"created_by"`
	Networks         []tmdbNetwork  `json:"networks"`
	Credits          tmdbCredits    `json:"credits"`
}

// ValidateCredentials 用给定的凭据请求 /3/authentication 验证有效性
// （校验的是「即将保存」的草稿值或已存值，不经过凭据快照）。
// 鉴权优先序与正式请求一致：有读令牌走 Bearer，否则用 api_key。
func (t *TMDB) ValidateCredentials(ctx context.Context, creds map[string]string) error {
	apiKey, readToken := creds["api_key"], creds["read_token"]
	apiKey, readToken = strings.TrimSpace(apiKey), strings.TrimSpace(readToken)
	if apiKey == "" && readToken == "" {
		return errors.New("缺少凭据：请先填写 API Key (v3) 或读访问令牌 (v4)")
	}
	q := url.Values{}
	q.Set("language", "zh-CN")
	var headers map[string]string
	if readToken != "" {
		headers = map[string]string{"Authorization": "Bearer " + readToken}
	} else {
		q.Set("api_key", apiKey)
	}
	var out struct {
		Success bool `json:"success"`
	}
	if err := t.f.getJSONHeaders(ctx, t.baseURL+"/authentication?"+q.Encode(), &out, headers); err != nil {
		return err
	}
	if !out.Success {
		return errors.New("TMDB 返回 success=false：凭据无效")
	}
	return nil
}

// FetchRecord 取上游单条记录并归一化为索引行（管理端单条刷新）。
func (t *TMDB) FetchRecord(ctx context.Context, ref string) (store.Subject, error) {
	if !t.Configured() {
		return store.Subject{}, ErrNotConfigured
	}
	kind, id, err := tmdbParseRef(ref)
	if err != nil {
		return store.Subject{}, err
	}
	q := url.Values{}
	q.Set("language", "zh-CN")
	u, headers := t.authURL("/"+kind+"/"+id, q)
	var it tmdbDetailItem
	if err := t.f.getJSONHeaders(ctx, u, &it, headers); err != nil {
		return store.Subject{}, err
	}

	isTV := kind == "tv"
	title := it.Title
	original := it.OriginalTitle
	date := NormalizeDate(it.ReleaseDate)
	platform := ""
	if isTV {
		title = it.Name
		original = it.OriginalName
		date = NormalizeDate(it.FirstAirDate)
		platform = "剧集"
	}

	tagNames := make([]string, 0, len(it.Genres))
	for _, g := range it.Genres {
		tagNames = append(tagNames, g.Name)
	}

	rec := store.Subject{
		Source:      tmdbSpec.ID,
		SourceRef:   ref,
		Type:        registry.KindCode(tmdbSpec.ID, registry.KindMovie),
		Name:        original,
		NameCN:      title,
		AltTitles:   []string{original, title},
		Date:        date,
		Platform:    platform,
		Score:       registry.ScoreOf(tmdbSpec.ID, it.VoteAverage),
		RatingTotal: it.VoteCount,
		NSFW:        it.Adult,
		Image:       tmdbPoster(it.PosterPath),
		Summary:     StripText(it.Overview),
		MetaTags:    []string{},
		TagsTotal:   0,
		Tags:        truncateTags(tagNames),
	}
	if isTV {
		rec.Type = registry.KindCode(tmdbSpec.ID, registry.KindTV)
	}
	return rec, nil
}

// tmdbParseRef 拆出 kind 与数字 id（ref 形如 "movie/123"）。
func tmdbParseRef(ref string) (string, string, error) {
	kind, id, ok := strings.Cut(ref, "/")
	if !ok || (kind != "movie" && kind != "tv") {
		return "", "", fmt.Errorf("非法 TMDB 引用 %q（应为 movie/{id} 或 tv/{id}）", ref)
	}
	if _, err := strconv.Atoi(id); err != nil {
		return "", "", fmt.Errorf("非法 TMDB id %q", id)
	}
	return kind, id, nil
}
