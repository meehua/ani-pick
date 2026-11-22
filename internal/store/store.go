// Package store 封装 AniPick 的 PostgreSQL 访问：条目索引、标签、设置、抓取记录与管理会话。
package store

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"anipick/internal/registry"
	"anipick/internal/secretbox"
)

var ErrNotFound = errors.New("记录不存在")

// 数据源标识与类型编码的定义在来源登记表（internal/registry）里：
// 标识/优先级/词表/详情策略等全部可变语义只在那一张表声明，
// 这里保留别名让既有调用方无需改引用。
const (
	SourceBGM     = registry.SourceBGM
	SourceTMDB    = registry.SourceTMDB
	SourceITunes  = registry.SourceITunes
	SourceMAL     = registry.SourceMAL
	SourceAniList = registry.SourceAniList

	TypeMovie    = registry.TypeMovie
	TypeTVSeries = registry.TypeTVSeries
	TypeAnime    = registry.TypeAnime
)

// AllSources 是全部登记过的来源（含原生索引）。
func AllSources() []string {
	all := registry.All()
	ids := make([]string, 0, len(all))
	for _, s := range all {
		ids = append(ids, s.ID)
	}
	return ids
}

// SourceLabel 返回来源展示名（登记表）。
func SourceLabel(source string) string { return registry.Label(source) }

// ValidSource 判断是否为登记过的来源标识（请求入参白名单）。
func ValidSource(s string) bool { return registry.Exists(s) }

type Store struct {
	Pool *pgxpool.Pool
	Box  *secretbox.Box

	// 进程内缓存：会话有效性（避免每管理请求一次 DB 写）、搜索总数（避免分页重复 count）。
	sessMu     sync.Mutex
	sessCache  map[string]sessionCacheEntry
	countMu    sync.Mutex
	countCache map[string]countEntry
	// countTTL 是搜索总数缓存 TTL，server 按系统设置注入（SetCountTTL），0 前取内置默认。
	countTTL atomic.Int64
	// countCap 是搜索总数缓存键上限，server 按系统设置注入（SetCountCap），0 前取内置默认。
	countCap atomic.Int64
}

func New(pool *pgxpool.Pool, box *secretbox.Box) *Store {
	st := &Store{
		Pool:       pool,
		Box:        box,
		sessCache:  map[string]sessionCacheEntry{},
		countCache: map[string]countEntry{},
	}
	st.countTTL.Store(int64(searchCountTTL))
	st.countCap.Store(int64(searchCountMaxKeys))
	return st
}

// SetCountTTL 设置搜索总数缓存 TTL（server 从系统设置同步）：
// 0 = 关闭 count 缓存（Search 跳过读写）；负值恢复内置默认。
func (s *Store) SetCountTTL(d time.Duration) {
	if d < 0 {
		d = searchCountTTL
	}
	s.countTTL.Store(int64(d))
}

// SetCountCap 设置搜索总数缓存键上限（server 从系统设置同步）；≤0 恢复内置默认。
func (s *Store) SetCountCap(n int) {
	if n <= 0 {
		n = searchCountMaxKeys
	}
	s.countCap.Store(int64(n))
}

// InvalidateCounts 清空搜索总数缓存（抓取结束/删除条目后调用），
// 避免 60s TTL 内新条目不进总数。
func (s *Store) InvalidateCounts() {
	s.countMu.Lock()
	s.countCache = map[string]countEntry{}
	s.countMu.Unlock()
}

// TagVote 是某个标签在单个条目中的票数与占比（百分比，0-100）。
type TagVote struct {
	Name  string  `json:"name"`
	Votes int     `json:"votes"`
	Pct   float64 `json:"pct"`
}

// Subject 是索引里的条目主信息；不含简介/角色等详情（bgm 详情由前端直连
// Bangumi 获取，其余来源经服务端详情代理端点获取）。
// Subject 是索引里一行条目（每个来源一行）。score/rating_total 在主行上
// 是作品组合成值；RawScore/RawRatingTotal 是该来源自己的原始值（评分对比行用）。
type Subject struct {
	ID     int64  `json:"id"`
	Source string `json:"source"`
	// SourceRef 是上游记录的原始 id 文本（如 "movie/123"）；身份内部字段，不外泄。
	SourceRef string `json:"-"`
	// WorkID 是作品分组 id（= 组内主行 id）；分组内部字段，不外泄。
	WorkID int64 `json:"-"`
	// ExtKey 是跨源确定性键（mal/anilist 的 MAL id）；分组内部字段，不外泄。
	ExtKey string `json:"-"`
	// AltTitles 是匹配用候选标题集（anilist 的 romaji/native、mal 的各变体等）；
	// 只服务作品合并匹配，不参与检索展示，不外泄。
	AltTitles   []string `json:"-"`
	Type        int      `json:"type"`
	Name        string   `json:"name"`
	NameCN      string   `json:"name_cn"`
	Date        string   `json:"date"`
	Platform    string   `json:"platform"`
	Rank        int      `json:"rank"`
	Score       float64  `json:"score"`
	RatingTotal int      `json:"rating_total"`
	// RawScore / RawRatingTotal 是本行所属来源自己的原始评分（评分对比行用，
	// 主行上的 Score 是作品组合成值）；组内字段，不外泄。
	RawScore       float64 `json:"-"`
	RawRatingTotal int     `json:"-"`
	// RatingDist 是本行的评分分布（1~10 分十档人数；空 = 该源无分档数据）。
	// 组内字段，经详情响应 sources 数组外发。
	RatingDist []int32  `json:"-"`
	Wish       int      `json:"wish"`
	Collect    int      `json:"collect"`
	Doing      int      `json:"doing"`
	OnHold     int      `json:"on_hold"`
	Dropped    int      `json:"dropped"`
	NSFW       bool     `json:"nsfw"`
	MetaTags   []string `json:"meta_tags"`
	Image      string   `json:"image"`
	// Summary 是条目简介（抓取时随上游载荷入库；详情页只读库不回上游）。
	// 存量行在下次抓取前为空串——字段恒输出，消费方不需要判 undefined。
	Summary   string    `json:"summary"`
	TagsTotal int       `json:"tags_total"`
	Locked    bool      `json:"locked"`
	FetchedAt time.Time `json:"fetched_at"`
	Tags      []TagVote `json:"tags,omitempty"`
}

// TagStat 是标签候选（按类型热度）。
type TagStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Votes int64  `json:"votes"`
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
