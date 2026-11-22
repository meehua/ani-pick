// Package registry 是数据源登记表：每个来源从注册到下线的全部可变语义只在这一处
// 声明——身份、展示文案、主行优先级、条目外链模板、类型词汇与可比对类别、
// 评分换算、详情获取策略、收获节流与预算、配置来源。
// matcher / crawler / server / 前端（经 manifest 下发）一律引用这里；
// 业务代码里不再出现来源专名分支或类型码字面量（适配器解析上游 payload 的
// 逻辑属于业务层，保留在各自文件里，但它们的元数据全部进本登记表）。
// 新增一个源的改动面：登记表加一行 + 适配器 + 测试，三处以内。
package registry

import (
	"sort"
	"strings"
)

// Kind 是站点级分类（双层词表的外层）：浏览筛选/首页类型卡按 kind 展示，
// 作品合并的启发式只在可跨源比对（Mergeable）的 kind 内进行。
// 各源原生类型标签不受 kind 影响（双层词表：原生层在 Types，永不改名）。
type Kind string

const (
	KindAnime Kind = "anime" // 动画（bgm 类型 2 与各动画源的动画条目）
	KindMovie Kind = "movie" // 电影
	KindTV    Kind = "tv"    // 剧集（bgm 三次元作品归入剧集类目）
	KindBook  Kind = "book"  // 书籍
	KindMusic Kind = "music" // 音乐
	KindGame  Kind = "game"  // 游戏
	// KindOther（空串）= 未分类。
	KindOther Kind = ""
)

// mergeKinds 是可跨源比对（参与作品合并）的 kind 集合；
// 书籍/音乐/游戏没有跨源同物，不参与合并。
var mergeKinds = map[Kind]bool{
	KindAnime: true,
	KindMovie: true,
	KindTV:    true,
}

// Mergeable 判断该 kind 是否参与作品合并。
func Mergeable(k Kind) bool { return mergeKinds[k] }

// KindLabels 是站点级分类的中文标签（浏览筛选/首页类型卡）。
// 想改站点的通用类目名，只动这一张表；各源原生标签（Types）不受影响。
var KindLabels = map[Kind]string{
	KindAnime: "动画",
	KindMovie: "电影",
	KindTV:    "剧集",
	KindBook:  "书籍",
	KindMusic: "音乐",
	KindGame:  "游戏",
}

// KindHints 是站点级分类的展示提示（首页类型卡副文案）。
var KindHints = map[Kind]string{
	KindAnime: "TV / 剧场版 / OVA",
	KindMovie: "电影",
	KindTV:    "剧集与电视季",
	KindBook:  "漫画 / 小说 / 画集",
	KindMusic: "专辑 / 单曲 / OST",
	KindGame:  "PC / 主机 / 手游",
}

// KindOrder 是站点级分类的展示顺序。
var KindOrder = []Kind{KindAnime, KindMovie, KindTV, KindBook, KindMusic, KindGame}

// KindLabel 返回站点级分类的中文标签。
func KindLabel(k Kind) string { return KindLabels[k] }

// KindHint 返回站点级分类的展示提示。
func KindHint(k Kind) string { return KindHints[k] }

// ModeLabels 是抓取模式的中文标签（抓取设置页按钮文案），随 manifest 下发。
var ModeLabels = map[string]string{
	"full":                "全量抓取",
	"incremental":         "增量抓取",
	"refresh":             "过期重爬",
	"harvest":             "抓取",
	"harvest_incremental": "增量抓取",
}

// ModeHints 是抓取模式的副文案（按钮提示）。
var ModeHints = map[string]string{
	"full":                "按预算逐页抓到末页",
	"incremental":         "只抓最近更新的页",
	"refresh":             "重爬超过保鲜期的条目",
	"harvest":             "按登记预算抓取",
	"harvest_incremental": "只抓最近更新的页",
}

// QuickLink 是首页「试试」快捷入口（登记表一行可改；经 manifest 下发）。
// To 里的 {year} 占位符由前端替换为去年年份（「近一年」入口的日期基准）。
type QuickLink struct {
	Label string `json:"label"` // 展示文案
	To    string `json:"to"`    // 跳转路径（/browse 的查询参数组合）
}

// QuickLinks 是首页快捷入口清单（顺序即展示顺序）。
var QuickLinks = []QuickLink{
	{Label: "高分口碑", To: "/browse?sort=score&rating_min=50"},
	{Label: "近一年", To: "/browse?date_from={year}-01-01"},
	{Label: "剧场版", To: "/browse?tags=剧场版"},
	{Label: "原创", To: "/browse?tags=原创"},
	{Label: "科幻", To: "/browse?tags=科幻"},
}

// 数据源标识（subjects.source）。bgm 为原生 Bangumi 索引；其余为扩展源，
// 上游记录以 (source, source_ref) 唯一定位，公开 id 由 subject_ext_id_seq 分配。
const (
	SourceBGM     = "bgm"
	SourceTMDB    = "tmdb"
	SourceITunes  = "itunes"
	SourceMAL     = "mal"
	SourceAniList = "anilist"
)

// 条目类型编码：1/2/3/4/6 为原生索引继承的类型，保持不变；
// 扩展源新增 101=电影、102=剧集。动画类扩展源（mal/anilist）并入 2=动画。
const (
	TypeBook     = 1
	TypeAnime    = 2
	TypeMusic    = 3
	TypeGame     = 4
	TypeReal     = 6
	TypeMovie    = 101
	TypeTVSeries = 102
)

// HarvestSpec 收获预算：请求间隔与全量/增量页数（页大小由适配器按上游 API 定）。
// IntervalMS 是默认请求间隔（管理设置可调），IntervalFloorMS 是硬下限
// ——低于下限的设置值按官方限速口径钳回（防止把上游打爆）。
// PageSize/PageSizeMax 是单页条数默认与上游硬顶（0 = 该源无分页参数可调）；
// TagMinRank 是标签相关度入库门槛默认；Region 是 storefront 地区默认。
// 三者均可被管理设置按源覆盖（0/空 = 用这里的默认）。
type HarvestSpec struct {
	IntervalMS      int    // 相邻上游请求的默认间隔（礼貌节流，配合退避重试）
	IntervalFloorMS int    // 请求间隔硬下限（0 = 不设，按官方限速文档定）
	FullPages       int    // 全量收获页数预算（0 = 无分页发现通道，如 iTunes 榜单单请求）
	IncPages        int    // 增量收获页数预算
	PageSize        int    // 单页条数默认（0 = 该源分页大小不可调）
	PageSizeMax     int    // 单页条数上游硬顶（0 = 不可调）
	TagMinRank      int    // 标签相关度入库门槛默认（0 = 该源无相关度词表）
	Region          string // storefront 地区默认（空 = 不适用）
}

// CredField 声明一个数据源凭据字段：设置页照单渲染，清单外拒绝。
// 值存 AppSettings.Credentials（secretbox 加密），运行期经凭据快照下发适配器。
type CredField struct {
	Key    string // 字段键（settings.credentials[source][key]）
	Label  string // 设置页展示名
	Secret bool   // true = 密码框渲染，存库加密
}

// Source 是登记表的一行：一个来源的全部声明式元数据。
type Source struct {
	ID          string         // subjects.source 标识（稳定 id，查询与分组都只用它）
	Label       string         // 中文展示名（徽标/面板/错误文案）
	Native      bool           // 是否原生索引（全站主来源：浏览页 chips 常驻）
	Priority    int            // 资料优先序（越小越优先；管理设置可调）
	Weight      int            // 默认评分权重（0-100，0 = 该源评分不参与合成）
	URLTemplate string         // 条目外链模板，{ref} 为 source_ref 占位
	Types       map[int]string // 该源产出的类型编码 → 原生标签（源资产，永不改名）
	Kinds       map[int]Kind   // 类型编码 → 站点级分类（未声明 = 未分类）
	ScoreScale  float64        // 上游评分满分：换算 10 分制 = 原始分 × 10 ÷ 满分；0 = 源无评分
	Harvest     HarvestSpec    // 抓取节流与页数预算
	Modes       []string       // 该源支持的抓取模式（full/incremental/refresh/harvest/harvest_incremental）
	Validate    bool           // 声明该源支持凭据校验（抓取设置页才渲染「校验凭据」按钮）
	Credentials []CredField    // 需要管理员填写的凭据字段（原生索引凭据走既有凭据区块，不在此列）
}

// TypeRef 引用一个来源的一个类型（匹配组的基本成员；管理设置里的多对多
// 类型匹配用它表达，registry 为叶子包所以类型定义在这里）。
type TypeRef struct {
	Source string `json:"source"`
	Type   int    `json:"type"`
}

// MatchGroupsDefault 返回匹配组的推荐默认值：可跨源比对的作品类（动画/电影/
// 剧集）各成一组，成员 = 登记表声明为该类的全部 (来源, 类型)。书籍/音乐/游戏
// 不进组（自成作品，不跨源配对）。管理设置可在此基础上多对多增删——同一
// (来源, 类型) 允许出现在多个组里（如 TMDB 剧集既在剧集组也可加入动画组）。
func MatchGroupsDefault() [][]TypeRef {
	var groups [][]TypeRef
	for _, k := range KindOrder {
		if !Mergeable(k) {
			continue
		}
		var g []TypeRef
		for _, src := range sources {
			for code, kind := range src.Kinds {
				if kind == k {
					g = append(g, TypeRef{Source: src.ID, Type: code})
				}
			}
		}
		if len(g) >= 2 {
			groups = append(groups, g)
		}
	}
	return groups
}

// sources 是登记表本体。声明顺序无关，语义（优先级/词表）都在行内。
var sources = []Source{
	{
		ID:     SourceBGM,
		Label:  "bgm.tv",
		Native: true,
		// 原生索引是全站主来源：中文资料、标签票数与分面统计都以它为准，
		// 资料优先序默认最高（管理设置可调）。
		Priority:    10,
		Weight:      100,
		URLTemplate: "https://bgm.tv/subject/{ref}",
		Types: map[int]string{
			TypeBook:  "书籍",
			TypeAnime: "动画",
			TypeMusic: "音乐",
			TypeGame:  "游戏",
			TypeReal:  "三次元", // 源原生词表，站点无权改名；站点层归入剧集类目
		},
		Kinds: map[int]Kind{
			TypeBook:  KindBook,
			TypeAnime: KindAnime,
			TypeMusic: KindMusic,
			TypeGame:  KindGame,
			TypeReal:  KindTV, // 三次元真人影视归入站点「剧集」类目，且与 TMDB 剧集同物可比
		},
		ScoreScale: 10,
		// 请求间隔官方无硬性文档，按社区礼貌口径 350ms 起步（下限 100ms 防手滑）。
		Harvest:  HarvestSpec{IntervalMS: 350, IntervalFloorMS: 100},
		Validate: true, // 站长凭据（Token/Cookie）可校验
		Modes:    []string{"full", "incremental", "refresh"},
	},
	{
		ID:     SourceAniList,
		Label:  "AniList",
		Native: false,
		// 动画源里资料最全（罗马字/日文原名/同义词齐），排在 MAL 之前便于
		// 资料面落到英文名、简介与人气数据。
		Priority:    20,
		Weight:      100,
		URLTemplate: "https://anilist.co/anime/{ref}",
		Types:       map[int]string{TypeAnime: "动画"},
		Kinds:       map[int]Kind{TypeAnime: KindAnime},
		ScoreScale:  100, // meanScore 是百分制
		// 官方限速 90 次/分钟：1200ms ≈ 50 次/分留出余量；下限 700ms ≈ 86 次/分。
		Harvest: HarvestSpec{IntervalMS: 1200, IntervalFloorMS: 700, FullPages: 10, IncPages: 2, PageSize: 50, PageSizeMax: 50, TagMinRank: 20},
		Modes:   []string{"harvest", "harvest_incremental"},
	},
	{
		ID:          SourceMAL,
		Label:       "MyAnimeList",
		Native:      false,
		Priority:    30,
		Weight:      100,
		URLTemplate: "https://myanimelist.net/anime/{ref}",
		Types:       map[int]string{TypeAnime: "动画"},
		Kinds:       map[int]Kind{TypeAnime: KindAnime},
		ScoreScale:  10,
		// MyAnimeList 官方 v2 API（api.myanimelist.net/v2，凭 Client ID 鉴权）。
		// 官方未文档化限速：750ms 保守口径（下限 400ms），429 由退避重试兜底。
		// 榜单单页上限 500 条：全量/增量各 1 页（~500 条预算）。
		Harvest:  HarvestSpec{IntervalMS: 750, IntervalFloorMS: 400, FullPages: 1, IncPages: 1, PageSize: 500, PageSizeMax: 500},
		Validate: true, // Client ID 可校验（GET /v2/anime/1?fields=id）
		Modes:    []string{"harvest", "harvest_incremental"},
		// MAL 设置页（API → 随处生成 Client ID）；请求头 X-MAL-CLIENT-ID 携带，
		// 凭据 secretbox 加密存库，未配置时该源整体跳过（与 TMDB 同款降级）。
		Credentials: []CredField{{Key: "client_id", Label: "Client ID", Secret: true}},
	},
	{
		ID:       SourceTMDB,
		Label:    "TMDB",
		Native:   false,
		Priority: 40,
		Weight:   100,
		// TMDB 的 source_ref 自带类别段（"movie/500"、"tv/1234"），直接拼进模板。
		URLTemplate: "https://www.themoviedb.org/{ref}?language=zh-CN",
		Types: map[int]string{
			TypeMovie:    "电影",
			TypeTVSeries: "剧集",
		},
		Kinds: map[int]Kind{
			TypeMovie:    KindMovie,
			TypeTVSeries: KindTV,
		},
		ScoreScale: 10,
		// 带 key 官方限速 50 次/秒：250ms ≈ 4 次/秒已很宽裕；下限 100ms 也仅 10 次/秒。
		Harvest:  HarvestSpec{IntervalMS: 250, IntervalFloorMS: 100, FullPages: 20, IncPages: 5}, // 分页大小上游固定 20/页，不暴露
		Validate: true,                                                                           // 双鉴权凭据（v3 Key / v4 读令牌）可校验
		Modes:    []string{"harvest", "harvest_incremental"},
		// 双鉴权：v3 API Key（query 参数）与 v4 读访问令牌（Authorization: Bearer 头），
		// 有读令牌优先用 Bearer，否则用 api_key。
		Credentials: []CredField{
			{Key: "api_key", Label: "API Key (v3)", Secret: true},
			{Key: "read_token", Label: "读访问令牌 (v4)", Secret: true},
		},
	},
	{
		ID:          SourceITunes,
		Label:       "Apple TV",
		Native:      false,
		Priority:    50,
		URLTemplate: "https://itunes.apple.com/tw/movie/id{ref}",
		Types:       map[int]string{TypeMovie: "电影"},
		Kinds:       map[int]Kind{TypeMovie: KindMovie},
		ScoreScale:  0, // 实测 RSS/lookup 均无评分字段
		// 官方口径约 20 次/分钟：3000ms 即贴住文档限速，下限同值只许更慢。
		Harvest: HarvestSpec{IntervalMS: 3000, IntervalFloorMS: 3000, PageSize: 100, PageSizeMax: 100, Region: "tw"},
		Modes:   []string{"harvest"},
	},
}

var byID = func() map[string]Source {
	m := make(map[string]Source, len(sources))
	for _, s := range sources {
		m[s.ID] = s
	}
	return m
}()

// All 返回登记表全部行（值拷贝，防调用方改坏全局）。
func All() []Source {
	out := make([]Source, len(sources))
	copy(out, sources)
	return out
}

// Get 按标识取登记表行；未知来源返回 false。
func Get(id string) (Source, bool) {
	s, ok := byID[id]
	return s, ok
}

// Exists 判断是否登记过的来源标识（请求入参白名单）。
func Exists(id string) bool {
	_, ok := byID[id]
	return ok
}

// Label 返回来源展示名；未登记的来源原样返回标识（数据里出现未知来源时不至于空屏）。
func Label(id string) string {
	if s, ok := byID[id]; ok {
		return s.Label
	}
	return id
}

// TypeLabel 返回类型编码的中文标签（全站词表：各源同码同义，登记表保证一致）。
// 未登记的编码返回空串，调用方自行退化。
func TypeLabel(code int) string {
	for _, s := range sources {
		if v, ok := s.Types[code]; ok {
			return v
		}
	}
	return ""
}

// KindOf 返回 (来源, 类型编码) 的站点级分类；未声明 = 未分类。
func KindOf(source string, code int) Kind {
	if s, ok := byID[source]; ok {
		return s.Kinds[code]
	}
	return KindOther
}

// ScoreOf 把上游原始评分换算成 10 分制；源无评分或满分非法时返回 0。
func ScoreOf(source string, raw float64) float64 {
	if s, ok := byID[source]; ok && s.ScoreScale > 0 && raw > 0 {
		return raw * 10 / s.ScoreScale
	}
	return 0
}

// KindCode 返回某来源归入指定站点分类的类型编码（适配器写入行类型用，取
// 编码序最小的一个；未声明返回 0）。编码来自登记表的 Kinds 声明。
func KindCode(id string, k Kind) int {
	s, ok := byID[id]
	if !ok {
		return 0
	}
	codes := declaredCodes(s)
	for _, c := range codes {
		if s.Kinds[c] == k {
			return c
		}
	}
	return 0
}

// declaredCodes 返回来源声明的类型编码（升序稳定）。
func declaredCodes(s Source) []int {
	out := make([]int, 0, len(s.Types))
	for code := range s.Types {
		out = append(out, code)
	}
	sort.Ints(out)
	return out
}

// SubjectURL 构造来源条目的官方站点外链（{ref} 换成 source_ref）；
// 未登记来源或模板缺占位符返回空串。
func SubjectURL(id, ref string) string {
	s, ok := byID[id]
	if !ok || ref == "" || !strings.Contains(s.URLTemplate, "{ref}") {
		return ""
	}
	return strings.ReplaceAll(s.URLTemplate, "{ref}", ref)
}

// Priorities 返回登记表默认优先级（source → 越小越优先），供设置缺省合并。
func Priorities() map[string]int {
	m := make(map[string]int, len(sources))
	for _, s := range sources {
		m[s.ID] = s.Priority
	}
	return m
}

// NativeID 返回原生索引来源的标识（合并抓取的主行来源、无 source 字段
// 旧行的归属）；登记表未声明时返回空串。
func NativeID() string {
	for i := range sources {
		if sources[i].Native {
			return sources[i].ID
		}
	}
	return ""
}

// DefaultWeights 返回登记表声明的默认评分权重（source → 0-100，0 = 不参与）。
func DefaultWeights() map[string]int {
	m := make(map[string]int, len(sources))
	for _, s := range sources {
		m[s.ID] = s.Weight
	}
	return m
}

// CredentialsOf 返回来源声明的凭据字段（设置页照单渲染）；未声明返回空。
func CredentialsOf(id string) []CredField {
	if s, ok := byID[id]; ok {
		return s.Credentials
	}
	return nil
}

// DeclaredCredential 判断 (来源, 字段) 是否为登记表声明的凭据字段。
func DeclaredCredential(id, field string) bool {
	for _, f := range CredentialsOf(id) {
		if f.Key == field {
			return true
		}
	}
	return false
}

// TypeOrder 是原生类型编码的全站展示顺序（管理面板数据表列头、兜底排列）。
// 站点层的类目顺序见 KindOrder；原生类型标签见各源 Types（双层词表）。
var TypeOrder = []int{TypeAnime, TypeMovie, TypeTVSeries, TypeBook, TypeGame, TypeMusic, TypeReal}

// ModesOf 返回来源声明的抓取模式清单；未登记的来源返回空。
func ModesOf(id string) []string { return byID[id].Modes }

// HasMode 判断来源是否声明支持某个抓取模式（crawler.start 的入参门）。
func HasMode(id, mode string) bool {
	for _, m := range byID[id].Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// MatchYearWindow 是启发式合并的年份窗口（±N 年，发行日期对齐用）。
const MatchYearWindow = 1

// MatchDateTiebreakDays 是同级打平时「放播日期贴近」的推荐容差（天）：恰有一个
// 打平作品的首播日期落在入站日期 ±N 天内即并入；运行期经系统设置可调。
const MatchDateTiebreakDays = 14

// AnimePlatformWords 把动画源的上游载体类型映射进原生索引的平台词汇
// （动画条目在 bgm 的 platform 取值：TV/剧场版/OVA/WEB/MAD）。
// anilist 与 mal 两个适配器共用这一张词表，避免各写一份。
var AnimePlatformWords = map[string]string{
	"TV":       "TV",
	"TV_SHORT": "TV",
	"MOVIE":    "剧场版",
	"OVA":      "OVA",
	"ONA":      "WEB",
	"SPECIAL":  "其他",
	"MUSIC":    "MAD",
}

// AnimePlatform 查平台词表（入参先做大写归一）；未识别的值原样保留
// （上游新格式不至于丢信息）。
func AnimePlatform(v string) string {
	if w, ok := AnimePlatformWords[strings.ToUpper(strings.TrimSpace(v))]; ok {
		return w
	}
	return v
}
