package sources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"anipick/internal/registry"
	"anipick/internal/store"
	"time"
)

// AniList 官方公开 GraphQL API（https://graphql.anilist.co，免密钥）。
// 全局限速约 90 次/分——节流与抓取页数预算由来源登记表声明。
// 发现：Page(perPage:50) 按热度倒序；详情：同查询按 id 取单条。
// tags 带 rank（0-100 相关度）与 spoiler 标记：rank≥anilistTagMinRank 且非剧透才入库。
const anilistBase = "https://graphql.anilist.co"

// anilistSpec 是 AniList 在登记表里的行（节流/预算/评分换算都从这取）。
var anilistSpec, _ = registry.Get(registry.SourceAniList)

// anilistTypes 是登记表声明的产出类型（排序稳定，供记录与面板使用）。
func anilistTypes() []int { return declaredTypes(anilistSpec) }

// AniList 是 AniList GraphQL 的适配器。baseURL 可注入以便 httptest 假上游测试。
type AniList struct {
	f       *fetcher
	baseURL string
}

// NewAniList 构造 AniList 适配器。
func NewAniList(hc *http.Client) *AniList {
	return &AniList{f: newFetcher(hc, specInterval(anilistSpec), anilistSpec.ID), baseURL: anilistBase}
}

func (a *AniList) Source() string   { return anilistSpec.ID }
func (a *AniList) Label() string    { return anilistSpec.Label }
func (a *AniList) Types() []int     { return anilistTypes() }
func (a *AniList) Configured() bool { return true }

// anilistFields 是媒体对象公共字段（发现与详情共用，一处维护）。
const anilistFields = `
	id idMal
	title { romaji english native }
	synonyms
	coverImage { extraLarge }
	bannerImage
	startDate { year month day }
	endDate { year month day }
	description(asHtml: false)
	meanScore
	genres
	tags { name rank isMediaSpoiler }
	format
	episodes
	duration
	status
	countryOfOrigin
	siteUrl
	stats { scoreDistribution { score amount } }
`

// anilistPageQuery 的单页条数走 $perPage 变量（上游硬顶 50，运行期设置可调小）。
const anilistPageQuery = `
	query ($page: Int, $perPage: Int) {
		Page(page: $page, perPage: $perPage) {
			pageInfo { hasNextPage }
			media(sort: POPULARITY_DESC, type: ANIME, isAdult: false) { ` + anilistFields + ` }
		}
	}`

const anilistMediaQuery = `
	query ($id: Int) {
		Media(id: $id, type: ANIME) { ` + anilistFields + `
			popularity favourites
			studios(isMain: true) { nodes { name } }
		}
	}`

type anilistFuzzyDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type anilistTag struct {
	Name           string `json:"name"`
	Rank           int    `json:"rank"`
	IsMediaSpoiler bool   `json:"isMediaSpoiler"`
}

type anilistScoreDist struct {
	Score  int `json:"score"`
	Amount int `json:"amount"`
}

type anilistMedia struct {
	ID    int `json:"id"`
	IDMal int `json:"idMal"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	CoverImage struct {
		ExtraLarge string `json:"extraLarge"`
	} `json:"coverImage"`
	BannerImage string           `json:"bannerImage"`
	StartDate   anilistFuzzyDate `json:"startDate"`
	EndDate     anilistFuzzyDate `json:"endDate"`
	Description string           `json:"description"`
	Synonyms    []string         `json:"synonyms"`
	MeanScore   int              `json:"meanScore"`
	Popularity  int              `json:"popularity"`
	Favourites  int              `json:"favourites"`
	Genres      []string         `json:"genres"`
	Tags        []anilistTag     `json:"tags"`
	Format      string           `json:"format"`
	Episodes    int              `json:"episodes"`
	Duration    int              `json:"duration"`
	Status      string           `json:"status"`
	Country     string           `json:"countryOfOrigin"`
	SiteURL     string           `json:"siteUrl"`
	Stats       struct {
		ScoreDistribution []anilistScoreDist `json:"scoreDistribution"`
	} `json:"stats"`
	Studios struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"studios"`
}

// ratedCount 是打分人数：scoreDistribution 各档 amount 之和。
func (m anilistMedia) ratedCount() int {
	n := 0
	for _, d := range m.Stats.ScoreDistribution {
		n += d.Amount
	}
	return n
}

// anilistDist 把 scoreDistribution 归一为十档人数（1~10 分各档）。
// AniList 的分档粒度为 10 分一档（score 10~100，2026-11-22 实测），score/10
// 恰为十分制档位；无数据/全 0 返回空数组。
func anilistDist(ds []anilistScoreDist) []int32 {
	out := make([]int32, 10)
	var sum int32
	for _, d := range ds {
		if d.Amount <= 0 || d.Score < 10 || d.Score > 100 {
			continue
		}
		out[d.Score/10-1] += int32(d.Amount)
		sum += int32(d.Amount)
	}
	if sum == 0 {
		return []int32{}
	}
	return out
}

// tagNames 过滤剧透标签并按 rank 门槛截断（门槛是运行期设置，调用方传入）。
func (m anilistMedia) tagNames(minRank int) []string {
	out := make([]string, 0, len(m.Tags))
	for _, t := range m.Tags {
		if t.IsMediaSpoiler || t.Rank < minRank {
			continue
		}
		out = append(out, t.Name)
	}
	return append(out, m.Genres...) // 类型标签优先级低，排在后面由截断兜底
}

// anilistDate 把 fuzzy date 归一成 YYYY-MM-DD（缺月/日补 01）。
func anilistDate(d anilistFuzzyDate) string {
	if d.Year <= 0 {
		return ""
	}
	month, day := d.Month, d.Day
	if month <= 0 {
		month = 1
	}
	if day <= 0 {
		day = 1
	}
	return NormalizeDate(fmt.Sprintf("%04d-%02d-%02d", d.Year, month, day))
}

// altTitles 汇总 AniList 的标题变体（罗马字/日文原名/同义词），供作品合并匹配。
func (m anilistMedia) altTitles() []string {
	var out []string
	if m.Title.Romaji != "" {
		out = append(out, m.Title.Romaji)
	}
	if m.Title.Native != "" {
		out = append(out, m.Title.Native)
	}
	out = append(out, m.Synonyms...)
	return out
}

// convert 把 AniList 媒体归一化。AniList 无中文标题：name 取英文名（缺省罗马字），
// name_cn 恒空。评分换算与平台词表走登记表；ext_key 存 idMal（与 mal 同一 ID 空间，确定性互配）。
func (a *AniList) convert(m anilistMedia, minRank int) store.Subject {
	name := m.Title.English
	if name == "" {
		name = m.Title.Romaji
	}
	score := registry.ScoreOf(anilistSpec.ID, float64(m.MeanScore))
	var extKey string
	if m.IDMal > 0 {
		extKey = strconv.Itoa(m.IDMal)
	}
	return store.Subject{
		Source:      anilistSpec.ID,
		SourceRef:   strconv.Itoa(m.ID),
		ExtKey:      extKey,
		AltTitles:   m.altTitles(),
		Type:        registry.KindCode(anilistSpec.ID, registry.KindAnime),
		Name:        name,
		NameCN:      "",
		Date:        anilistDate(m.StartDate),
		Platform:    registry.AnimePlatform(m.Format),
		Rank:        0, // AniList 热度榜无排名概念
		Score:       score,
		RatingTotal: m.ratedCount(),
		RatingDist:  anilistDist(m.Stats.ScoreDistribution),
		Wish:        m.Favourites,
		Collect:     m.Popularity,
		NSFW:        false, // 发现查询已过滤 isAdult
		Image:       m.CoverImage.ExtraLarge,
		Summary:     StripText(m.Description),
		MetaTags:    []string{},
		TagsTotal:   0,
		Tags:        truncateTags(m.tagNames(minRank)),
	}
}

// HarvestFull 全量抓取：登记表预算的页数（每页 50 条）。
func (a *AniList) HarvestFull(sc *Scope) error {
	sc.fillDefaults(anilistSpec)
	return a.harvest(sc, sc.FullPages)
}

// HarvestIncremental 增量抓取（真增量）：空数据（该源从未完成过增量）按全量
// 预算补齐；否则「上次运行之后开播」的新增窗口（开播日期倒序，预算=增量页数）
// + 人气榜头部 1 页保底。上游无变更流，已有条目的变更靠过期重爬。
func (a *AniList) HarvestIncremental(sc *Scope) error {
	sc.fillDefaults(anilistSpec)
	if sc.LastIncremental.IsZero() {
		return a.harvest(sc, sc.FullPages)
	}
	if err := a.harvestNew(sc); err != nil {
		return err
	}
	return a.harvest(sc, 1) // 人气榜头部保底：榜单前移进榜的新条目兜底
}

// anilistNewQuery 是增量新增窗口查询：开播日期晚于锚点（FuzzyDateInt，如 20261122），
// 按开播日期倒序。注意它只覆盖「开播在窗口内」的条目——先开播后入库的冷门条目
// 靠人气榜保底页与全量补齐，这是官方 API 的能力边界。
const anilistNewQuery = `
	query ($page: Int, $perPage: Int, $since: FuzzyDateInt) {
		Page(page: $page, perPage: $perPage) {
			pageInfo { hasNextPage }
			media(sort: START_DATE_DESC, type: ANIME, isAdult: false, startDate_greater: $since) { ` + anilistFields + ` }
		}
	}`

// fuzzyDate 把锚点时间换成 AniList FuzzyDateInt（YYYYMMDD；>锚点日 语义由上游处理）。
func fuzzyDate(t time.Time) int {
	return t.Year()*10000 + int(t.Month())*100 + t.Day()
}

// harvestNew 跑开播窗口页循环（预算 = 增量页数，空页即止）。
func (a *AniList) harvestNew(sc *Scope) error {
	since := fuzzyDate(sc.LastIncremental)
	for page := 1; page <= sc.IncPages; page++ {
		if sc.Ctx.Err() != nil {
			return sc.Ctx.Err()
		}
		var out struct {
			Data struct {
				Page struct {
					Media []anilistMedia `json:"media"`
				} `json:"Page"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := a.f.postJSON(sc.Ctx, a.baseURL, map[string]any{
			"query":     anilistNewQuery,
			"variables": map[string]any{"page": page, "perPage": sc.PageSize, "since": since},
		}, &out); err != nil {
			sc.Add("errors", 1)
			return fmt.Errorf("开播窗口第 %d 页: %w", page, err)
		}
		sc.Add("requests", 1)
		sc.Add("pages", 1)
		if len(out.Data.Page.Media) == 0 {
			return nil
		}
		recs := make([]store.Subject, 0, len(out.Data.Page.Media))
		for _, m := range out.Data.Page.Media {
			recs = append(recs, a.convert(m, sc.TagMinRank))
		}
		ins, upd, err := sc.Store.UpsertSubjects(sc.Ctx, recs, sc.RunID)
		if err != nil {
			return fmt.Errorf("写入开播窗口第 %d 页: %w", page, err)
		}
		sc.Add("inserted", ins)
		sc.Add("updated", upd)
		sc.Update("enumerate", fmt.Sprintf("AniList：开播窗口第 %d/%d 页（+新增 %d / 更新 %d）", page, sc.IncPages, ins, upd))
	}
	return nil
}

func (a *AniList) harvest(sc *Scope, pages int) error {
	for page := 1; page <= pages; page++ {
		if sc.Ctx.Err() != nil {
			return sc.Ctx.Err()
		}
		var out struct {
			Data struct {
				Page struct {
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
					Media []anilistMedia `json:"media"`
				} `json:"Page"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := a.f.postJSON(sc.Ctx, a.baseURL, map[string]any{
			"query":     anilistPageQuery,
			"variables": map[string]any{"page": page, "perPage": sc.PageSize},
		}, &out); err != nil {
			sc.Add("errors", 1)
			return fmt.Errorf("Page 第 %d 页: %w", page, err)
		}
		sc.Add("requests", 1)
		sc.Add("pages", 1)
		if len(out.Data.Page.Media) == 0 {
			return nil
		}
		recs := make([]store.Subject, 0, len(out.Data.Page.Media))
		for _, m := range out.Data.Page.Media {
			recs = append(recs, a.convert(m, sc.TagMinRank))
		}
		ins, upd, err := sc.Store.UpsertSubjects(sc.Ctx, recs, sc.RunID)
		if err != nil {
			return fmt.Errorf("写入第 %d 页: %w", page, err)
		}
		sc.Add("inserted", ins)
		sc.Add("updated", upd)
		sc.Update("enumerate", fmt.Sprintf("AniList：第 %d/%d 页（+新增 %d / 更新 %d）", page, pages, ins, upd))
		if !out.Data.Page.PageInfo.HasNextPage {
			return nil
		}
	}
	return nil
}

// FetchRecord 取上游单条媒体并归一化为索引行。
func (a *AniList) FetchRecord(ctx context.Context, ref string) (store.Subject, error) {
	m, err := a.media(ctx, ref)
	if err != nil {
		return store.Subject{}, err
	}
	return a.convert(m, effectiveParams(anilistSpec).TagMinRank), nil
}

// media 按 ref（数字 AniList id）取单条媒体。
func (a *AniList) media(ctx context.Context, ref string) (anilistMedia, error) {
	id, err := strconv.Atoi(strings.TrimSpace(ref))
	if err != nil || id <= 0 {
		return anilistMedia{}, fmt.Errorf("非法 AniList id %q", ref)
	}
	var out struct {
		Data struct {
			Media anilistMedia `json:"Media"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := a.f.postJSON(ctx, a.baseURL, map[string]any{
		"query":     anilistMediaQuery,
		"variables": map[string]any{"id": id},
	}, &out); err != nil {
		return anilistMedia{}, err
	}
	if out.Data.Media.ID == 0 {
		return anilistMedia{}, ErrNotFound
	}
	return out.Data.Media, nil
}

// AniListRecord 是一次存量回填的完整载荷：确定性键 + 全部标题变体
// （英文/罗马字/日文原名/同义词——存量行入库早于 alt_titles 抽取时列是空的，
// 必须回填标题，重匹配的启发式才能拿罗马字/日文名对上 bgm）。
type AniListRecord struct {
	ID     int64
	IDMal  int64
	Titles []string
}

// BackfillRecords 批量取一批 AniList 条目的回填载荷。每 50 个 id 一次请求
// （GraphQL id_in）；上游查不到的 id 不出现在结果里。
func (a *AniList) BackfillRecords(ctx context.Context, refs []string) (map[string]AniListRecord, error) {
	out := make(map[string]AniListRecord, len(refs))
	const chunk = 50
	for start := 0; start < len(refs); start += chunk {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		end := start + chunk
		if end > len(refs) {
			end = len(refs)
		}
		intIDs := make([]int, 0, end-start)
		for _, ref := range refs[start:end] {
			if id, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil && id > 0 {
				intIDs = append(intIDs, id)
			}
		}
		if len(intIDs) == 0 {
			continue
		}
		var resp struct {
			Data struct {
				Page struct {
					Media []struct {
						ID    int `json:"id"`
						IDMal int `json:"idMal"`
						Title struct {
							English string `json:"english"`
							Romaji  string `json:"romaji"`
							Native  string `json:"native"`
						} `json:"title"`
						Synonyms []string `json:"synonyms"`
					} `json:"media"`
				} `json:"Page"`
			} `json:"data"`
		}
		query := "query ($ids: [Int]) { Page(perPage: " + strconv.Itoa(chunk) + ") { media(id_in: $ids) { id idMal title { english romaji native } synonyms } } }"
		err := a.f.postJSON(ctx, a.baseURL, map[string]any{
			"query":     query,
			"variables": map[string]any{"ids": intIDs},
		}, &resp)
		if err != nil {
			return out, err
		}
		for _, m := range resp.Data.Page.Media {
			rec := AniListRecord{ID: int64(m.ID), IDMal: int64(m.IDMal)}
			rec.Titles = append(rec.Titles,
				m.Title.English, m.Title.Romaji, m.Title.Native)
			rec.Titles = append(rec.Titles, m.Synonyms...)
			out[strconv.Itoa(m.ID)] = rec
		}
	}
	return out, nil
}
