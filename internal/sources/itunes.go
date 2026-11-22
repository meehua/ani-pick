package sources

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"anipick/internal/registry"
	"anipick/internal/store"
)

// Apple iTunes 官方公开接口（免密钥）。
//
// 实测结论（2026-11，详见 README「数据源」）：
//   - 中国区电影榜单 RSS 返回空 feed（CN 区不售卖电影），lookup country=CN 恒 0 结果；
//     search 对 movie/tvSeason 实体在所有地区均返回 0（Apple 已下线该类目搜索）；
//     top-tvseasons 等剧集 feed 名称已被 RSS 服务拒绝（Invalid RSS channel name）。
//   - 因此发现通道取台湾区（zh-TW 元数据，华语可读）topmovies 榜单，详情用
//     /lookup?id=...&country=tw；只收获电影（101）。剧集暂无官方发现通道，不做。
//   - lookup/RSS 均已不返回评分字段，本源 score/rating_total 恒 0（登记表 ScoreScale=0）。
const itunesBase = "https://itunes.apple.com"

// topMoviesURL 构造电影榜单地址：地区与条数是运行期设置（默认 tw / 100）。
func (i *ITunes) topMoviesURL(region string, limit int) string {
	return fmt.Sprintf("%s/%s/rss/topmovies/limit=%d/json", i.baseURL, region, limit)
}

// currentRegion 返回当前有效 storefront 地区（设置覆写 ?? 登记表默认 tw）。
func currentRegion() string {
	return effectiveParams(itunesSpec).Region
}

// itunesSpec 是 iTunes 在登记表里的行（节流预算从这取）。
var itunesSpec, _ = registry.Get(registry.SourceITunes)

// ITunes 是 iTunes 电影榜单的适配器。baseURL 可注入以便 httptest 假上游测试。
type ITunes struct {
	f       *fetcher
	baseURL string
}

// NewITunes 构造 iTunes 适配器。
func NewITunes(hc *http.Client) *ITunes {
	return &ITunes{f: newFetcher(hc, specInterval(itunesSpec), itunesSpec.ID), baseURL: itunesBase}
}

func (i *ITunes) Source() string   { return itunesSpec.ID }
func (i *ITunes) Label() string    { return itunesSpec.Label }
func (i *ITunes) Types() []int     { return declaredTypes(itunesSpec) }
func (i *ITunes) Configured() bool { return true }

// itunesEntry 是 RSS feed（feed.entry[]）里用得到的字段（label 型扁平结构）。
type itunesEntry struct {
	ID struct {
		Attributes struct {
			IMID string `json:"im:id"`
		} `json:"attributes"`
	} `json:"id"`
	Name     itunesLabel `json:"im:name"`
	Artist   itunesLabel `json:"im:artist"`
	Summary  itunesLabel `json:"summary"`
	Category struct {
		Attributes struct {
			Label string `json:"label"`
		} `json:"attributes"`
	} `json:"category"`
	Release itunesLabel   `json:"im:releaseDate"`
	Images  []itunesLabel `json:"im:image"`
}

type itunesLabel struct {
	Label string `json:"label"`
}

type itunesFeedResp struct {
	Feed struct {
		Entry []itunesEntry `json:"entry"`
	} `json:"feed"`
}

// itunesLookupResult 是 /lookup 结果里用得到的字段（snake_case）。
type itunesLookupResult struct {
	TrackID       int    `json:"trackId"`
	TrackName     string `json:"trackName"`
	ArtistName    string `json:"artistName"`
	PrimaryGenre  string `json:"primaryGenreName"`
	ReleaseDate   string `json:"releaseDate"`
	Advisory      string `json:"contentAdvisoryRating"`
	LongDesc      string `json:"longDescription"`
	ShortDesc     string `json:"shortDescription"`
	TrackTimeMs   int64  `json:"trackTimeMillis"`
	TrackViewURL  string `json:"trackViewUrl"`
	ArtworkURL100 string `json:"artworkUrl100"`
	Kind          string `json:"kind"`
}

type itunesLookupResp struct {
	Count   int                  `json:"resultCount"`
	Results []itunesLookupResult `json:"results"`
}

// advisoryNSFW 分级里出现以下关键词时视为敏感内容（tw 区分级繁体文案）。
var advisoryNSFW = regexp.MustCompile(`限制|輔15|R-?18|NC-?17|X$`)

// convert 把 RSS 条目归一化。榜单无评分字段（恒 0）；中文名在 im:name。
func (i *ITunes) convert(e itunesEntry) store.Subject {
	image := ""
	if len(e.Images) > 0 {
		image = itunesArtwork(e.Images[len(e.Images)-1].Label) // 末个是最大尺寸
	}
	return store.Subject{
		Source:      itunesSpec.ID,
		SourceRef:   e.ID.Attributes.IMID,
		Type:        registry.KindCode(itunesSpec.ID, registry.KindMovie),
		Name:        e.Name.Label,
		NameCN:      "",
		Date:        NormalizeDate(e.Release.Label),
		Rank:        0,
		Score:       0, // 实测 RSS/lookup 均无评分字段
		RatingTotal: 0,
		NSFW:        false,
		Image:       image,
		MetaTags:    []string{},
		TagsTotal:   0,
		Tags:        truncateTags([]string{e.Category.Attributes.Label}),
	}
}

// HarvestFull 全量收获：整份电影榜单（实测约 100 条）。
func (i *ITunes) HarvestFull(sc *Scope) error {
	sc.fillDefaults(itunesSpec)
	entries, err := i.topMovies(sc)
	if err != nil {
		return err
	}
	return i.write(sc, entries, "全量")
}

// HarvestIncremental 增量收获：榜单与全量同源（一份榜单即全部可得数据）。
func (i *ITunes) HarvestIncremental(sc *Scope) error {
	sc.fillDefaults(itunesSpec)
	entries, err := i.topMovies(sc)
	if err != nil {
		return err
	}
	return i.write(sc, entries, "增量")
}

func (i *ITunes) topMovies(sc *Scope) ([]itunesEntry, error) {
	sc.fillDefaults(itunesSpec)
	ctx := sc.Ctx
	var out itunesFeedResp
	if err := i.f.getJSON(ctx, i.topMoviesURL(sc.Region, sc.PageSize), &out); err != nil {
		return nil, fmt.Errorf("topmovies 榜单: %w", err)
	}
	return out.Feed.Entry, nil
}

func (i *ITunes) write(sc *Scope, entries []itunesEntry, mode string) error {
	if len(entries) == 0 {
		sc.Update("enumerate", "Apple TV：榜单为空")
		return nil
	}
	sc.Add("requests", 1)
	sc.Add("pages", 1)
	recs := make([]store.Subject, 0, len(entries))
	for _, e := range entries {
		if e.ID.Attributes.IMID == "" {
			sc.Add("errors", 1)
			continue
		}
		recs = append(recs, i.convert(e))
	}
	ins, upd, err := sc.Store.UpsertSubjects(sc.Ctx, recs, sc.RunID)
	if err != nil {
		return fmt.Errorf("写入榜单: %w", err)
	}
	sc.Add("inserted", ins)
	sc.Add("updated", upd)
	sc.Update("enumerate", fmt.Sprintf("Apple TV：%s榜单 %d 条（+新增 %d / 更新 %d）", mode, len(recs), ins, upd))
	return nil
}

// FetchRecord 取上游单条记录（/lookup）并归一化为索引行。
func (i *ITunes) FetchRecord(ctx context.Context, ref string) (store.Subject, error) {
	r, err := i.lookup(ctx, ref)
	if err != nil {
		return store.Subject{}, err
	}
	nsfw := advisoryNSFW.MatchString(r.Advisory)
	date := NormalizeDate(r.ReleaseDate)
	summary := r.LongDesc
	if summary == "" {
		summary = r.ShortDesc
	}
	return store.Subject{
		Source:      itunesSpec.ID,
		SourceRef:   strconv.Itoa(r.TrackID),
		Type:        registry.KindCode(itunesSpec.ID, registry.KindMovie),
		Name:        r.TrackName,
		NameCN:      "",
		Date:        date,
		Platform:    "",
		Score:       0,
		RatingTotal: 0,
		NSFW:        nsfw,
		Image:       itunesArtwork(r.ArtworkURL100),
		Summary:     StripText(summary),
		MetaTags:    []string{},
		TagsTotal:   0,
		Tags:        truncateTags([]string{r.PrimaryGenre}),
	}, nil
}

// lookup 按数字 id 取单条记录。
func (i *ITunes) lookup(ctx context.Context, ref string) (itunesLookupResult, error) {
	id, err := strconv.Atoi(strings.TrimSpace(ref))
	if err != nil || id <= 0 {
		return itunesLookupResult{}, fmt.Errorf("非法 iTunes id %q", ref)
	}
	var out itunesLookupResp
	url := fmt.Sprintf("%s/lookup?id=%d&country=%s", i.baseURL, id, currentRegion())
	if err := i.f.getJSON(ctx, url, &out); err != nil {
		return itunesLookupResult{}, err
	}
	if len(out.Results) == 0 {
		return itunesLookupResult{}, ErrNotFound
	}
	return out.Results[0], nil
}
