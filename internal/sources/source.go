// Package sources 实现扩展数据源（tmdb/itunes/mal/anilist）的收获与详情适配器。
//
// 每个 harvester 负责「发现」（热门榜单分页枚举）与「归一化」：上游记录统一映射为
// store.Subject（含标签，votes/pct 恒为 0——扩展源不做票数统计），身份 (source,
// source_ref) 的解析由 store.UpsertSubjects 收口；上游详情统一映射为 Detail，
// 由服务端详情代理端点输出（上游 key 与限速不出服务端）。
// 出站复用 netx 通道（调用方注入 *http.Client），节流与退避重试在本包统一实现。
package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"anipick/internal/registry"
	"anipick/internal/store"
)

// ErrNotConfigured 表示该源缺少必要配置（如 TMDB API key）。
var ErrNotConfigured = errors.New("该数据源未配置")

// ErrNotFound 表示上游不存在这条记录（404）。
var ErrNotFound = errors.New("上游条目不存在")

// Scope 是一次收获执行的运行环境：由 crawler 组装，harvester 只面向本结构工作。
type Scope struct {
	Ctx   context.Context
	Store *store.Store
	RunID int64
	// Update 报告当前阶段与消息（进度面板展示）。
	Update func(phase, message string)
	// Add 累加进度计数：requests / pages / inserted / updated / errors。
	Add func(field string, n int)
	// HTTP 是出站客户端（netx DoH/ECH 通道；测试注入 httptest 客户端）。
	HTTP *http.Client
	// 收获预算（登记表默认 + 管理设置覆写后的有效值；crawler 构建 Scope 时填入，
	// 测试构造 Scope 时零值 = 适配器经 fillDefaults 回落登记表默认）。
	PageSize   int    // 单页条数
	FullPages  int    // 全量页数预算
	IncPages   int    // 增量页数预算
	TagMinRank int    // 标签相关度入库门槛（AniList 消费）
	Region     string // storefront 地区（Apple TV 消费）
	// LastIncremental 是该源最近一次完成增量的运行时间（crawler 查库填入）。
	// 零值 = 该源还没跑过增量（空数据）——适配器把本轮增量升级为全量预算，
	// 保证首次安装/清空后条目不丢。
	LastIncremental time.Time
}

// fillDefaults 把 Scope 里零值的预算回落到登记表默认（适配器收获入口调用）。
func (sc *Scope) fillDefaults(spec registry.Source) {
	if sc.PageSize <= 0 {
		sc.PageSize = spec.Harvest.PageSize
	}
	if sc.FullPages <= 0 {
		sc.FullPages = spec.Harvest.FullPages
	}
	if sc.IncPages <= 0 {
		sc.IncPages = spec.Harvest.IncPages
	}
	if sc.TagMinRank <= 0 {
		sc.TagMinRank = spec.Harvest.TagMinRank
	}
	if sc.Region == "" {
		sc.Region = spec.Harvest.Region
	}
}

// Harvester 是一个扩展数据源的适配器。
type Harvester interface {
	// Source 是 subjects.source 标识。
	Source() string
	// Label 是展示名（管理面板/日志）。
	Label() string
	// Types 是该源写入的条目类型（crawl_runs.types 记录用）。
	Types() []int
	// Configured 是否可用；不可用时收获按「已跳过」记录。
	Configured() bool
	// HarvestFull 全量收获：按各源预算枚举发现列表并逐批写入。
	HarvestFull(sc *Scope) error
	// HarvestIncremental 增量收获：只取发现列表头部若干页。
	HarvestIncremental(sc *Scope) error
	// FetchRecord 取上游单条记录并归一化为索引行（管理端单条刷新用）。
	FetchRecord(ctx context.Context, ref string) (store.Subject, error)
}

// Detailer 是详情代理：按 source_ref 取上游详情并归一化。
// 所有 Harvester 同时实现 Detailer（服务端按来源分派）。
type Detailer interface {
	Detail(ctx context.Context, ref string) (*Detail, error)
}

// CredentialValidator 是凭据校验器：用给定（草稿或已存）凭据向上游发一次
// 轻量请求验证有效性。哪些源声明了校验能力由登记表 Validate 决定；
// 服务端按此渲染校验按钮与分派校验端点。
type CredentialValidator interface {
	ValidateCredentials(ctx context.Context, creds map[string]string) error
}

// Fact 是详情页信息栏的一行（k=名称，v=值）。
type Fact struct {
	K string `json:"k"`
	V string `json:"v"`
}

// Detail 是归一化后的条目详情（服务端代理端点 /api/subjects/{id}/detail 的响应体）。
// 字段是各源共有的超集；上游没有的部分留空，前端沿用缺省退化逻辑。
type Detail struct {
	Source     string   `json:"source"`
	SourceRef  string   `json:"source_ref"`
	Title      string   `json:"title"`
	TitleCN    string   `json:"title_cn,omitempty"`
	Platform   string   `json:"platform,omitempty"`
	Date       string   `json:"date,omitempty"`
	EndDate    string   `json:"end_date,omitempty"`
	Image      string   `json:"image,omitempty"`
	Backdrop   string   `json:"backdrop,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Score      float64  `json:"score,omitempty"`       // 已归一到 10 分制
	ScoreCount int      `json:"score_count,omitempty"` // 评分人数
	Episodes   int      `json:"episodes,omitempty"`
	Runtime    int      `json:"runtime,omitempty"` // 分钟（电影为片长，剧集为单集）
	Tags       []string `json:"tags,omitempty"`
	Facts      []Fact   `json:"facts,omitempty"`
	Link       string   `json:"link,omitempty"`
}

// truncateTags 去空、去重并截断标签（扩展源 votes/pct 恒为 0，只保留名字）。
// 上限是运行期设置（ext_tag_max，SetTagMax 立刷）。
func truncateTags(names []string) []store.TagVote {
	max := currentTagMax()
	seen := map[string]struct{}{}
	out := make([]store.TagVote, 0, max)
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, store.TagVote{Name: n})
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

// NormalizeDate 把上游日期归一成 YYYY-MM-DD；只有年份/年月时补 01-01/01，
// 非法值返回空（上游偶有脏数据，丢字段好过让整页写入失败）。
func NormalizeDate(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case len(s) >= 10:
		s = s[:10]
	case len(s) == 7:
		s += "-01"
	case len(s) == 4:
		s += "-01-01"
	default:
		return ""
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// StripText 清理上游简介里的内联标记（HTML 标签、换行冗余），保留纯文本。
// AniList/MAL 的 description 常带 <br>/<i> 与 Markdown 残留。
func StripText(s string) string {
	if s == "" {
		return ""
	}
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// declaredTypes 排序登记表声明的产出类型编码（适配器 Types() 用）。
func declaredTypes(s registry.Source) []int {
	out := make([]int, 0, len(s.Types))
	for code := range s.Types {
		out = append(out, code)
	}
	sort.Ints(out)
	return out
}

// specInterval 登记表声明的请求节流间隔（礼貌限速，配合退避重试）。
func specInterval(s registry.Source) time.Duration {
	return time.Duration(s.Harvest.IntervalMS) * time.Millisecond
}

// itunesArtworkSize 把 mzstatic 缩略图 URL 里的尺寸段换成大图
// （https://…/115x170bb.png → /500x750bb.png；扩展名为 png/jpg 均保留）。
var itunesArtworkRe = regexp.MustCompile(`/\d+x\d+bb(\.\w+)?$`)

func itunesArtwork(url string) string {
	if url == "" {
		return ""
	}
	return itunesArtworkRe.ReplaceAllString(url, "/500x750bb$1")
}

// SubjectURL 构造一个来源行的官方站点外链（详情页多源对比行用）。
// ref 为 subjects.source_ref；模板与解析规则在来源登记表，未登记来源返回空串。
func SubjectURL(source, ref string) string {
	if source == registry.SourceTMDB {
		// TMDB 的 ref 自带类别段（"movie/500"、"tv/1234"），先校验再进模板
		if _, _, err := tmdbParseRef(ref); err != nil {
			return ""
		}
	}
	return registry.SubjectURL(source, ref)
}

// humanize 数字人性化（面板/详情文本用，万为级）。
func humanize(n int) string {
	if n >= 10000 {
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	}
	return strconv.Itoa(n)
}
