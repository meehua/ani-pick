package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"anipick/internal/registry"
	"anipick/internal/store"
)

// MyAnimeList 官方 v2 API（api.myanimelist.net/v2，凭 Client ID 鉴权，
// 请求头 X-MAL-CLIENT-ID）。官方未文档化限速：登记表按保守口径声明
// 750ms（下限 400ms），429 由退避重试兜底。
// 发现：/anime/ranking 分页（单页上限 500）；详情：/anime/{id} 带字段清单。
// 只抓 anime（manga 不做）。client_id 存系统设置（与 TMDB key 同一套
// secretbox 链路），未配置时该源整体跳过。
const malBase = "https://api.myanimelist.net/v2"

// malFields 是榜单与详情共用的字段清单（官方按字段返回，缺省只给 id/title）。
const malFields = "id,title,main_picture,alternative_titles,start_date,end_date,synopsis," +
	"mean,rank,popularity,num_scoring_users,num_episodes,average_episode_duration,genres,studios,media_type"

// malSpec 是 MAL 在登记表里的行。
var malSpec, _ = registry.Get(registry.SourceMAL)

// MAL 是 MyAnimeList 官方 v2 的适配器。baseURL 可注入以便 httptest 假上游测试。
type MAL struct {
	f       *fetcher
	creds   *CredStore
	baseURL string
}

// NewMAL 构造 MAL 适配器；凭据快照由 main 装配、server 随设置保存立刷。
// client_id 为空表示未配置（收获跳过、详情代理报未配置）。
func NewMAL(creds *CredStore, hc *http.Client) *MAL {
	return &MAL{f: newFetcher(hc, specInterval(malSpec), malSpec.ID), creds: creds, baseURL: malBase}
}

// clientID 读凭据快照里的当前 Client ID（设置保存后立刷生效）。
func (m *MAL) clientID() string { return m.creds.Get(malSpec.ID, "client_id") }

func (m *MAL) Source() string   { return malSpec.ID }
func (m *MAL) Label() string    { return malSpec.Label }
func (m *MAL) Types() []int     { return declaredTypes(malSpec) }
func (m *MAL) Configured() bool { return m.clientID() != "" }

// authHeaders 每次请求附带的鉴权头（官方 v2 只支持 Client ID 头）。
func (m *MAL) authHeaders() map[string]string {
	return map[string]string{"X-MAL-CLIENT-ID": m.clientID()}
}

// ValidateCredentials 用给定的 Client ID 请求 /v2/anime/1?fields=id 验证有效性
// （校验的是「即将保存」的草稿值或已存值，不经过凭据快照）。
func (m *MAL) ValidateCredentials(ctx context.Context, creds map[string]string) error {
	id := strings.TrimSpace(creds["client_id"])
	if id == "" {
		return errors.New("缺少凭据：请先填写 MyAnimeList 的 Client ID（MAL 设置页 → API 可生成）")
	}
	var out struct {
		ID int `json:"id"`
	}
	u := fmt.Sprintf("%s/anime/1?fields=id", m.baseURL)
	err := m.f.getJSONHeaders(ctx, u, &out, map[string]string{"X-MAL-CLIENT-ID": id})
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, " 401") || strings.Contains(msg, " 400") || strings.Contains(msg, "Invalid") {
		return errors.New("Client ID 无效或已被吊销：请到 MyAnimeList 设置页重新生成后重试")
	}
	return err
}

// malNode 是 /anime/ranking 与 /anime/{id} 共有的条目结构。
type malNode struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	MainPicture struct {
		Medium string `json:"medium"`
		Large  string `json:"large"`
	} `json:"main_picture"`
	AlternativeTitles struct {
		EN       string   `json:"en"`
		JA       string   `json:"ja"`
		Synonyms []string `json:"synonyms"`
	} `json:"alternative_titles"`
	StartDate       string        `json:"start_date"`
	EndDate         string        `json:"end_date"`
	Synopsis        string        `json:"synopsis"`
	Mean            float64       `json:"mean"`
	Rank            int           `json:"rank"`
	Popularity      int           `json:"popularity"`
	NumScoringUsers int           `json:"num_scoring_users"`
	NumEpisodes     int           `json:"num_episodes"`
	AvgEpisodeDur   int           `json:"average_episode_duration"`
	Genres          []malNamedRef `json:"genres"`
	Studios         []malNamedRef `json:"studios"`
	MediaType       string        `json:"media_type"`
}

type malNamedRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type malRankingResp struct {
	Data []struct {
		Node malNode `json:"node"`
	} `json:"data"`
}

// altTitles 汇总 MAL 的各标题变体（英文/日文/同义词），供作品合并匹配的
// 标题启发式使用。
func (n malNode) altTitles() []string {
	var out []string
	if n.AlternativeTitles.EN != "" {
		out = append(out, n.AlternativeTitles.EN)
	}
	if n.AlternativeTitles.JA != "" {
		out = append(out, n.AlternativeTitles.JA)
	}
	out = append(out, n.AlternativeTitles.Synonyms...)
	return out
}

// convert 把 MAL 条目归一化。MAL 无中文标题，name_cn 恒空；
// ext_key 存自己的 MAL id——与 anilist 的 idMal 同一 ID 空间，
// 跨源确定性合并链在写入时即成立（无需事后回填）。
func (m *MAL) convert(n malNode) store.Subject {
	nsfw := false
	for _, g := range n.Genres {
		if strings.EqualFold(g.Name, "Hentai") {
			nsfw = true
		}
	}
	ref := strconv.Itoa(n.ID)
	return store.Subject{
		Source:      malSpec.ID,
		SourceRef:   ref,
		ExtKey:      ref,
		AltTitles:   n.altTitles(),
		Type:        registry.KindCode(malSpec.ID, registry.KindAnime),
		Name:        n.Title,
		NameCN:      "",
		Date:        NormalizeDate(n.StartDate),
		Platform:    registry.AnimePlatform(n.MediaType),
		Rank:        n.Rank,
		Score:       registry.ScoreOf(malSpec.ID, n.Mean),
		RatingTotal: n.NumScoringUsers,
		NSFW:        nsfw,
		Image:       n.MainPicture.Large,
		Summary:     StripText(n.Synopsis),
		MetaTags:    []string{},
		TagsTotal:   0,
		Tags:        truncateTags(genreNames(n.Genres)),
	}
}

// genreNames 提取类型名列表（榜单/详情同用）。
func genreNames(refs []malNamedRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Name)
	}
	return out
}

// HarvestFull 全量收获：/anime/ranking?ranking_type=all 按登记表预算的页数
// （单页 500 条，1 页 = 500 条预算）。
func (m *MAL) HarvestFull(sc *Scope) error {
	sc.fillDefaults(malSpec)
	return m.harvest(sc, "all", sc.FullPages)
}

// HarvestIncremental 增量收获：ranking_type=airing——在播榜单是 MAL 上
// 变化最快的头部（评分/人数随每周放送更新），单页 500 条覆盖当季全部在播。
func (m *MAL) HarvestIncremental(sc *Scope) error {
	sc.fillDefaults(malSpec)
	// 空数据（该源从未完成过增量）按全量预算补齐（总榜）；否则在播榜单——
	// 官方无按日期发现端点，在播榜即新番窗口（预算=增量页数）。
	if sc.LastIncremental.IsZero() {
		return m.harvest(sc, "all", sc.FullPages)
	}
	return m.harvest(sc, "airing", sc.IncPages)
}

// rankingURL 构造榜单分页请求地址（offset = (page-1) × 单页条数；条数是运行期
// 设置，上限 = 官方单页 500）。
func rankingURL(base, rankingType string, page, perPage int) string {
	offset := (page - 1) * perPage
	return fmt.Sprintf("%s/anime/ranking?ranking_type=%s&limit=%d&offset=%d&fields=%s",
		base, rankingType, perPage, offset, malFields)
}

func (m *MAL) harvest(sc *Scope, rankingType string, pages int) error {
	if !m.Configured() {
		return ErrNotConfigured
	}
	perPage := sc.PageSize
	for page := 1; page <= pages; page++ {
		if sc.Ctx.Err() != nil {
			return sc.Ctx.Err()
		}
		var out malRankingResp
		if err := m.f.getJSONHeaders(sc.Ctx, rankingURL(m.baseURL, rankingType, page, perPage), &out, m.authHeaders()); err != nil {
			sc.Add("errors", 1)
			return fmt.Errorf("anime/ranking(%s) 第 %d 页: %w", rankingType, page, err)
		}
		sc.Add("requests", 1)
		sc.Add("pages", 1)
		if len(out.Data) == 0 {
			return nil
		}
		recs := make([]store.Subject, 0, len(out.Data))
		for _, item := range out.Data {
			recs = append(recs, m.convert(item.Node))
		}
		ins, upd, err := sc.Store.UpsertSubjects(sc.Ctx, recs, sc.RunID)
		if err != nil {
			return fmt.Errorf("写入第 %d 页: %w", page, err)
		}
		sc.Add("inserted", ins)
		sc.Add("updated", upd)
		sc.Update("enumerate", fmt.Sprintf("MyAnimeList：第 %d/%d 页（+新增 %d / 更新 %d）", page, pages, ins, upd))
		if len(out.Data) < perPage {
			return nil // 末页不足一整页：榜单到底
		}
	}
	return nil
}

// FetchRecord 取上游单条记录（/anime/{id} 带字段清单）并归一化为索引行。
func (m *MAL) FetchRecord(ctx context.Context, ref string) (store.Subject, error) {
	n, err := m.fetchRecord(ctx, ref)
	if err != nil {
		return store.Subject{}, err
	}
	return m.convert(n), nil
}

// fetchRecord 取上游单条记录；ref 必须是数字 MAL id。
func (m *MAL) fetchRecord(ctx context.Context, ref string) (malNode, error) {
	id, err := strconv.Atoi(strings.TrimSpace(ref))
	if err != nil || id <= 0 {
		return malNode{}, fmt.Errorf("非法 MAL id %q", ref)
	}
	if !m.Configured() {
		return malNode{}, ErrNotConfigured
	}
	var node malNode
	if err := m.f.getJSONHeaders(ctx, fmt.Sprintf("%s/anime/%d?fields=%s", m.baseURL, id, malFields), &node, m.authHeaders()); err != nil {
		return malNode{}, err
	}
	return node, nil
}
