package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"anipick/internal/registry"
)

// regionRe 校验每源设置里的 storefront 地区代码（两位字母，如 tw / jp）。
var regionRe = regexp.MustCompile(`^[a-z]{2}$`)

// AutoSchedule 描述自动任务（自动增量 / 自动过期重爬）的触发方式，三种形式：
// interval 固定间隔（距上次运行结束至少隔 EveryMin 分钟）、daily 每天定时、
// every_days 每隔 EveryDays 天定时（都在当天 AtMinute 分触发，服务器本地时间）。
// Kind 为空 = 旧库数据没有此字段 → 由 Normalize 按旧字段升级为 interval。
type AutoSchedule struct {
	Kind      string `json:"kind"`       // interval | daily | every_days
	EveryMin  int    `json:"every_min"`  // interval：间隔分钟（下限 1）
	AtMinute  int    `json:"at_minute"`  // daily/every_days：当天 0 点起的分钟数（0..1439）
	EveryDays int    `json:"every_days"` // every_days：每隔几天（下限 1）
}

type CrawlSettings struct {
	Types             []int `json:"types"`
	PageSize          int   `json:"page_size"`
	RequestTimeoutSec int   `json:"request_timeout_sec"`
	RefreshTTLDays    int   `json:"refresh_ttl_days"`
	RefreshBatch      int   `json:"refresh_batch"`
	IncrementalPages  int   `json:"incremental_pages"`
	// 旧字段：增量间隔的原始存储位。调度模型（IncrementalSchedule）上线后只作
	// 双向镜像——新值从 schedule 折算回来写给它（旧二进制回滚时仍读到合理值），
	// 权威判定永远以 IncrementalSchedule 为准。
	IncrementalIntervalMin int          `json:"incremental_interval_min"`
	IncrementalSchedule    AutoSchedule `json:"incremental_schedule"`
	RefreshSchedule        AutoSchedule `json:"refresh_schedule"`
	AutoIncremental        bool         `json:"auto_incremental"`
	AutoRefresh            bool         `json:"auto_refresh"`
	PruneOnFull            bool         `json:"prune_on_full"`
	// 调度器与任务稳健性（原为代码常量，全部后台可调；缺省 = 默认推荐值）
	SchedulerTickSec     int `json:"scheduler_tick_sec"`     // 调度器检查是否到点的周期（秒）
	PageFailRetries      int `json:"page_fail_retries"`      // 页拉取连续失败重试次数，超过整页跳过
	PageRetryWaitSec     int `json:"page_retry_wait_sec"`    // 页重试间隔（秒）
	RefreshProgressEvery int `json:"refresh_progress_every"` // 过期重爬每处理多少条播报一次进度
	SchedulerLookback    int `json:"scheduler_lookback"`     // 调度器回看最近任务条数（找上一轮结束）
	StaleRetryCap        int `json:"stale_retry_cap"`        // 过期条目连续失败多少次后暂时跳过
	SourceGapMS          int `json:"source_gap_ms"`          // 合并运行中源与源的间隔（毫秒）
	ExtTagMax            int `json:"ext_tag_max"`            // 扩展源单条目最多入库标签数
	// 请求与重试（bgm 客户端与扩展源出站通道共用同一口径）
	RequestAttempts    int `json:"request_attempts"`     // 同一请求的重试次数（429/5xx/网络错）
	RequestBackoffMS   int `json:"request_backoff_ms"`   // 重试退避基数（毫秒，按平方递增）
	RetryAfterCapSec   int `json:"retry_after_cap_sec"`  // 上游 429 Retry-After 的等待封顶（秒）
	ResponseLimitMB    int `json:"response_limit_mb"`    // 单响应体积上限（MB，防异常响应撑爆内存）
	OutboundTimeoutSec int `json:"outbound_timeout_sec"` // 扩展源单请求超时（秒；bgm 用抓取设置里的请求超时）
	// 合并与配对
	MatchYearWindow int `json:"match_year_window"` // 跨源合并的年份容差（±年，改小更严）
	RematchBatch    int `json:"rematch_batch"`     // 重新配对的分批大小（条）
	// MatchGroups 是多对多类型匹配组：同组成员之间允许跨源配对；同一 (来源,
	// 类型) 可属于多个组。nil = 未设置（旧库数据）→ 归一化时落登记表推荐默认。
	MatchGroups [][]registry.TypeRef `json:"match_groups"`
}

type BGMSettings struct {
	Token  string `json:"token"`
	Cookie string `json:"cookie"`
	UA     string `json:"ua"`
}

// CacheSettings 是查询结果缓存参数（管理后台「设置 → 查询缓存」可调）。
// 列表数据只在抓取与条目级操作后变化，失效走显式钩子，TTL 只是兜底。
// TTL 字段用指针：缺省（旧库数据无此字段）→ 默认值；显式 0 → 关闭该层缓存；
// 负值视为未设置。用指针而非 int 是为了区分「没填过」和「填了 0」——否则升级后
// 旧库数据会被解读成 0，缓存全体静默关闭。MaxKeys 是容量而非开关，0 → 默认。
type CacheSettings struct {
	QueryTTL        *int `json:"query_ttl"`          // 搜索页/搜索总数/标签候选/条目详情的进程内缓存 TTL（秒）
	PublicTTL       *int `json:"public_ttl"`         // 公开 GET 响应 Cache-Control max-age（秒，浏览器与 CF 边缘）
	MetaTTL         *int `json:"meta_ttl"`           // /api/meta 首页元信息缓存 TTL（秒）
	MetaTagsTTL     *int `json:"meta_tags_ttl"`      // /api/meta-tags 元标签候选缓存 TTL（秒）
	OverviewTTL     *int `json:"overview_ttl"`       // /api/admin/overview 聚合缓存 TTL（秒）
	ExtDetailTTL    *int `json:"ext_detail_ttl"`     // /api/subjects/{id}/detail 扩展源代理详情缓存 TTL（秒）
	MaxKeys         int  `json:"max_keys"`           // 搜索页/标签候选/详情/搜索总数缓存的键上限（内存有界）
	MetaTagsMaxKeys int  `json:"meta_tags_max_keys"` // 元标签候选缓存的键上限（内存有界），0 = 默认
}

// CacheSec 解析 TTL 设置指针：nil/负值视为未设置返回 def；0 = 关闭（返回 0）；
// 正值原样返回（Normalize 已钳位下限）。返回 0 时调用方应跳过对应缓存。
func CacheSec(p *int, def int) int {
	if p == nil || *p < 0 {
		return def
	}
	return *p
}

// SecuritySettings 是限流与会话安全参数（管理后台「设置 → 限流与安全」可调）。
// 限流是按 IP 计数的令牌桶（经可信反代时取真实 IP 头）：超过 burst 后按 rate
// 匀速补充。0/缺省视为默认值（兼容旧库数据无此字段）。
// ClientIPHeader / TrustedProxyCIDRs 是真实 IP 识别：只有 TCP 对端在可信网段内
// 才采信头，防伪造绕过限流；随设置快照立刷，逐请求生效。
type SecuritySettings struct {
	RateLimitRPS      int    `json:"rate_limit_rps"`      // 每 IP 常规请求速率（次/秒）
	RateLimitBurst    int    `json:"rate_limit_burst"`    // 每 IP 常规请求突发容量
	LoginRPM          int    `json:"login_rpm"`           // 登录尝试稳态速率（次/分钟/IP），防爆破
	LoginBurst        int    `json:"login_burst"`         // 登录尝试突发容量
	LoginDelayMS      int    `json:"login_delay_ms"`      // 密码错误的响应延迟（毫秒），拉高爆破成本
	SessionTTLDays    int    `json:"session_ttl_days"`    // 管理员会话有效期（天），对新会话生效
	LimiterMaxIPs     int    `json:"limiter_max_ips"`     // 限流表 IP 条目硬上限（内存有界）
	MaxPage           int    `json:"max_page"`            // 公开搜索最大页码（深分页成本上限）
	ClientIPHeader    string `json:"client_ip_header"`    // 客户端真实 IP 请求头（如 CF-Connecting-IP）；空 = 用 TCP 对端
	TrustedProxyCIDRs string `json:"trusted_proxy_cidrs"` // 可信反代网段（逗号分隔 CIDR）；空 = 不读头
}

// SourceSetting 是每个数据源的管理设置（管理后台「设置 → 数据源」可调）。
// 可调语义本身在来源登记表（internal/registry）声明，这里只存管理员覆写：
// Enabled 关掉后该源不进站点清单、抓取跳过、浏览页筛选消失（已入库的行保留）；
// Priority 是资料优先序（越小越优先），0 = 用登记表默认；
// Weight 是评分权重（0-100），指针缺省 = 登记表默认，显式 0 = 不参与合成
// ——用指针区分「没填过」和「填了 0」，否则旧库数据会被解读成 0；
// IntervalMS 是该源的请求间隔（毫秒），0 = 用登记表默认（每个源的最佳值），
// 正值不允许低于登记表声明的下限（EffectiveInterval 负责钳位）；
// RefreshTTLDays 是该源的保留期覆盖（天），0 = 用全局默认（EffectiveRefreshTTL
// 负责取值）——新鲜度策略每个源都适用，不只原生索引。
type SourceSetting struct {
	Enabled        bool `json:"enabled"`
	Priority       int  `json:"priority"`
	Weight         *int `json:"weight"`
	IntervalMS     int  `json:"interval_ms"`
	RefreshTTLDays int  `json:"refresh_ttl_days"`
	// 收获预算覆写（0/空 = 用登记表默认；上限按登记表声明的上游硬顶钳位）
	PageSize   int    `json:"page_size"`    // 单页条数（仅登记表声明可调的源生效）
	FullPages  int    `json:"full_pages"`   // 全量页数预算
	IncPages   int    `json:"inc_pages"`    // 增量页数预算
	TagMinRank int    `json:"tag_min_rank"` // 标签相关度入库门槛（仅 AniList 消费）
	Region     string `json:"region"`       // storefront 地区两位代码（仅 Apple TV 消费）
}

// SourcePriority 返回来源在给定设置下的有效优先级（设置覆写优先，
// 未填/缺省回落登记表默认；未登记来源垫底）。
func SourcePriority(source string, settings map[string]SourceSetting) int {
	if v, ok := settings[source]; ok && v.Priority > 0 {
		return v.Priority
	}
	if s, ok := registry.Get(source); ok {
		return s.Priority
	}
	return 1 << 30
}

// EffectiveWeight 返回来源在给定设置下的有效评分权重（nil/负 = 登记表默认；
// 显式 0 = 不参与合成）。
func EffectiveWeight(source string, settings map[string]SourceSetting) int {
	if v, ok := settings[source]; ok && v.Weight != nil && *v.Weight >= 0 {
		return *v.Weight
	}
	if s, ok := registry.Get(source); ok {
		return s.Weight
	}
	return 0
}

// EffectiveInterval 返回来源在给定设置下的有效请求间隔（毫秒）：
// 设置 0/未填 = 登记表默认（每个源的最佳值）；正值允许比默认更快，
// 但不允许低于登记表声明的官方限速下限（低于按下限钳回）。
func EffectiveInterval(source string, settings map[string]SourceSetting) int {
	spec, ok := registry.Get(source)
	if !ok {
		return 0
	}
	v := 0
	if s, ok := settings[source]; ok {
		v = s.IntervalMS
	}
	if v <= 0 {
		v = spec.Harvest.IntervalMS
	}
	if floor := spec.Harvest.IntervalFloorMS; floor > 0 && v < floor {
		v = floor
	}
	return v
}

// EffectiveRefreshTTL 返回来源的有效保留期（天）：源覆盖值 > 0 用覆盖值，
// 否则用全局默认（新鲜度策略对每个源都适用，过期重爬与各源的新鲜度判定
// 统一从这里取值）。globalTTL 已经过 Normalize 钳位（1–3650）。
func EffectiveRefreshTTL(source string, settings map[string]SourceSetting, globalTTL int) int {
	if s, ok := settings[source]; ok && s.RefreshTTLDays > 0 {
		return s.RefreshTTLDays
	}
	return globalTTL
}

// EffectiveHarvest 返回来源在给定设置下的有效收获预算（登记表默认 + 管理覆写）：
// 页数 0 = 登记表默认；单页条数钳在登记表声明的上游硬顶内；
// 标签门槛 0 = 登记表默认；地区空 = 登记表默认。
func EffectiveHarvest(source string, settings map[string]SourceSetting) (pageSize, fullPages, incPages, tagMinRank int, region string) {
	spec, _ := registry.Get(source)
	pageSize, fullPages, incPages, tagMinRank, region =
		spec.Harvest.PageSize, spec.Harvest.FullPages, spec.Harvest.IncPages, spec.Harvest.TagMinRank, spec.Harvest.Region
	v, ok := settings[source]
	if !ok {
		return
	}
	if v.PageSize > 0 && spec.Harvest.PageSizeMax > 0 {
		if v.PageSize > spec.Harvest.PageSizeMax {
			pageSize = spec.Harvest.PageSizeMax
		} else {
			pageSize = v.PageSize
		}
	}
	if v.FullPages > 0 {
		fullPages = v.FullPages
	}
	if v.IncPages > 0 {
		incPages = v.IncPages
	}
	if v.TagMinRank > 0 {
		tagMinRank = v.TagMinRank
	}
	if v.Region != "" {
		region = v.Region
	}
	return
}

// NetworkSettings 是服务端抓取出口的运行配置（管理后台「抓取设置 → 网络」可调）。
// EgressProxy 只用于服务端抓取（http/https/socks5），访客请求不经过它；
// DoH/ECH/DoHEndpoints 是出站 DNS 解析通道。
// 任一变化保存后立即重建出站客户端，bgm 抓取与各扩展源抓取随即走新通道。
type NetworkSettings struct {
	EgressProxy  string   `json:"egress_proxy"`  // 出站代理；空 = 直连或 DoH
	DoH          bool     `json:"doh"`           // 出站走 DoH 解析（代理优先于 DoH）
	ECH          bool     `json:"ech"`           // TLS ECH（DoH 启用时生效）
	DoHEndpoints []string `json:"doh_endpoints"` // DoH 入口清单；空 = 内置默认
	// DNS 与出站身份（原为代码常量，后台可调；缺省 = 默认推荐值）
	DoHTimeoutSec  int    `json:"doh_timeout_sec"`   // DoH 解析超时（秒）
	DnsCacheMinSec int    `json:"dns_cache_min_sec"` // DNS 结果最短缓存（秒）
	DnsCacheTTLSec int    `json:"dns_cache_ttl_sec"` // DNS 结果默认缓存（秒）
	OutboundUA     string `json:"outbound_ua"`       // 扩展源出站请求的 User-Agent；空 = 内置默认
}

// ServerSettings 是服务器运行参数（管理后台「设置 → 服务器与面板」可调）。
// 原为代码常量；缺省（旧库数据无此字段）= 默认推荐值。
type ServerSettings struct {
	SessionCacheTTLSec int `json:"session_cache_ttl_sec"` // 会话校验进程内缓存 TTL（秒）
	SessionSeenGapSec  int `json:"session_seen_gap_sec"`  // 会话 last_seen 落库节流（秒）
	SessionCacheMax    int `json:"session_cache_max"`     // 会话缓存条目上限（内存有界）
	MinCompressBytes   int `json:"min_compress_bytes"`    // 小于该字节数的响应不压缩
	SnapshotTTLSec     int `json:"snapshot_ttl_sec"`      // 设置快照的兜底刷新周期（秒）
	PerfSampleCap      int `json:"perf_sample_cap"`       // 性能面板每路由耗时样本上限
	RecentReqCap       int `json:"recent_req_cap"`        // 近期请求环形缓冲上限
	OverviewRuns       int `json:"overview_runs"`         // 管理总览显示的最近任务条数
	ShutdownWaitSec    int `json:"shutdown_wait_sec"`     // HTTP 优雅停机等待（秒）
	AdminPollSec       int `json:"admin_poll_sec"`        // 管理面板轮询间隔（秒，页面隐藏时自动暂停）
	PageCacheMax       int `json:"page_cache_max"`        // 前端「返回」缓存的页面数，0 = 关闭
}

type AppSettings struct {
	Crawl       CrawlSettings                `json:"crawl"`
	BGM         BGMSettings                  `json:"bgm"`
	Cache       CacheSettings                `json:"cache"`
	Security    SecuritySettings             `json:"security"`
	Network     NetworkSettings              `json:"network"`
	Server      ServerSettings               `json:"server"`
	Sources     map[string]SourceSetting     `json:"sources"`
	Credentials map[string]map[string]string `json:"credentials"`
	UpdatedAt   time.Time                    `json:"updated_at"`
}

// Normalize 裁剪到安全范围，避免管理端填入极端值。
func (s *AppSettings) Normalize() {
	c := &s.Crawl
	// 抓取类型只允许原生索引在登记表里声明的类型编码
	native, _ := registry.Get(registry.SourceBGM)
	declared := map[int]struct{}{}
	var defTypes []int
	for code := range native.Types {
		declared[code] = struct{}{}
		defTypes = append(defTypes, code)
	}
	sort.Ints(defTypes)
	var types []int
	for _, t := range c.Types {
		if _, ok := declared[t]; ok {
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		types = defTypes
	}
	c.Types = types
	c.PageSize = clamp(c.PageSize, 10, 100)
	c.RequestTimeoutSec = clamp(c.RequestTimeoutSec, 5, 120)
	c.RefreshTTLDays = clamp(c.RefreshTTLDays, 1, 3650)
	c.RefreshBatch = clamp(c.RefreshBatch, 0, 5000)
	c.IncrementalPages = clamp(c.IncrementalPages, 1, 1000)
	c.IncrementalIntervalMin = clamp(c.IncrementalIntervalMin, 5, 100000)
	// 自动任务调度：Kind 空 = 旧库数据 → 升级为 interval（增量用旧间隔字段，
	// 过期重爬用旧写死值 10 分钟）；显式 interval 钳下限 1 分钟；daily/every_days
	// 钳时间与天数范围。归一化后把增量调度镜像回旧字段（旧二进制回滚可读）。
	const refreshGapLegacy = 10 // 旧版本的过期重爬防抖窗口（分钟），仅作升级缺省
	normSchedule := func(sc *AutoSchedule, legacyMin int) {
		switch sc.Kind {
		case "interval":
			sc.EveryMin = clamp(sc.EveryMin, 1, 100000)
		case "daily":
			sc.AtMinute = clamp(sc.AtMinute, 0, 1439)
		case "every_days":
			sc.AtMinute = clamp(sc.AtMinute, 0, 1439)
			sc.EveryDays = clamp(sc.EveryDays, 1, 3650)
		default:
			sc.Kind = "interval"
			sc.EveryMin = legacyMin
		}
		sc.AtMinute = clamp(sc.AtMinute, 0, 1439)
	}
	normSchedule(&c.IncrementalSchedule, c.IncrementalIntervalMin)
	normSchedule(&c.RefreshSchedule, refreshGapLegacy)
	if c.IncrementalSchedule.Kind == "interval" {
		c.IncrementalIntervalMin = clamp(c.IncrementalSchedule.EveryMin, 5, 100000)
	}
	// 缓存 TTL：nil/负值 = 未设置 → 默认；显式 0 = 关闭该层缓存（保留）；
	// 正值钳下限防缓存形同虚设，不设上限——失效靠显式钩子，TTL 只是兜底，
	// 站长可设任意长；上界仅防手滑与 seconds→Duration 溢出（MaxInt32 秒 ≈ 68 年）。
	normTTL := func(p **int, def, floor int) {
		if *p == nil || **p < 0 {
			v := def
			*p = &v
			return
		}
		if **p == 0 {
			return
		}
		**p = clamp(**p, floor, math.MaxInt32)
	}
	normTTL(&s.Cache.QueryTTL, 60, 5)
	normTTL(&s.Cache.PublicTTL, 30, 5)
	normTTL(&s.Cache.MetaTTL, 30, 5)
	normTTL(&s.Cache.MetaTagsTTL, 600, 10)
	normTTL(&s.Cache.OverviewTTL, 3, 1)
	// 扩展源详情回源要请求上游（秒级延迟），默认缓存 10 分钟——评分/标题变化
	// 慢，且管理端刷新/抓取收尾会主动失效；下限与详情缓存一致 5s
	normTTL(&s.Cache.ExtDetailTTL, 600, 5)
	if s.Cache.MaxKeys <= 0 {
		s.Cache.MaxKeys = 256
	}
	s.Cache.MaxKeys = clamp(s.Cache.MaxKeys, 16, 10000)
	if s.Cache.MetaTagsMaxKeys <= 0 {
		s.Cache.MetaTagsMaxKeys = 64
	}
	s.Cache.MetaTagsMaxKeys = clamp(s.Cache.MetaTagsMaxKeys, 8, 4096)

	// 任务稳健性与合并参数：0/未填 = 默认推荐值，正值钳安全范围
	if c.SchedulerTickSec <= 0 {
		c.SchedulerTickSec = 120
	}
	c.SchedulerTickSec = clamp(c.SchedulerTickSec, 30, 600)
	if c.PageFailRetries <= 0 {
		c.PageFailRetries = 3
	}
	c.PageFailRetries = clamp(c.PageFailRetries, 0, 10)
	if c.PageRetryWaitSec <= 0 {
		c.PageRetryWaitSec = 5
	}
	c.PageRetryWaitSec = clamp(c.PageRetryWaitSec, 1, 60)
	if c.RefreshProgressEvery <= 0 {
		c.RefreshProgressEvery = 20
	}
	c.RefreshProgressEvery = clamp(c.RefreshProgressEvery, 1, 500)
	if c.SchedulerLookback <= 0 {
		c.SchedulerLookback = 20
	}
	c.SchedulerLookback = clamp(c.SchedulerLookback, 5, 200)
	if c.StaleRetryCap <= 0 {
		c.StaleRetryCap = 5
	}
	c.StaleRetryCap = clamp(c.StaleRetryCap, 0, 20)
	if c.SourceGapMS <= 0 {
		c.SourceGapMS = 2000
	}
	c.SourceGapMS = clamp(c.SourceGapMS, 100, 60000)
	if c.ExtTagMax <= 0 {
		c.ExtTagMax = 8
	}
	c.ExtTagMax = clamp(c.ExtTagMax, 0, 30)
	if c.RequestAttempts <= 0 {
		c.RequestAttempts = 3
	}
	c.RequestAttempts = clamp(c.RequestAttempts, 1, 10)
	if c.RequestBackoffMS <= 0 {
		c.RequestBackoffMS = 2000
	}
	c.RequestBackoffMS = clamp(c.RequestBackoffMS, 100, 30000)
	if c.RetryAfterCapSec <= 0 {
		c.RetryAfterCapSec = 30
	}
	c.RetryAfterCapSec = clamp(c.RetryAfterCapSec, 5, 300)
	if c.ResponseLimitMB <= 0 {
		c.ResponseLimitMB = 8
	}
	c.ResponseLimitMB = clamp(c.ResponseLimitMB, 1, 64)
	if c.OutboundTimeoutSec <= 0 {
		c.OutboundTimeoutSec = 30
	}
	c.OutboundTimeoutSec = clamp(c.OutboundTimeoutSec, 5, 120)
	if c.MatchYearWindow <= 0 {
		c.MatchYearWindow = 1
	}
	c.MatchYearWindow = clamp(c.MatchYearWindow, 0, 2)
	if c.RematchBatch <= 0 {
		c.RematchBatch = 100
	}
	c.RematchBatch = clamp(c.RematchBatch, 10, 1000)
	// 匹配组：nil = 未设置 → 登记表推荐默认；成员校验（来源存在且声明过该
	// 类型）、组内去重、淘汰空组——非法成员静默丢弃（与来源区块同口径）。
	if c.MatchGroups == nil {
		c.MatchGroups = registry.MatchGroupsDefault()
	}
	cleanGroups := make([][]registry.TypeRef, 0, len(c.MatchGroups))
	for _, g := range c.MatchGroups {
		seen := map[registry.TypeRef]struct{}{}
		cg := make([]registry.TypeRef, 0, len(g))
		for _, m := range g {
			spec, ok := registry.Get(m.Source)
			if !ok {
				continue
			}
			if _, declared := spec.Types[m.Type]; !declared {
				continue
			}
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			cg = append(cg, m)
		}
		if len(cg) >= 2 {
			cleanGroups = append(cleanGroups, cg)
		}
	}
	c.MatchGroups = cleanGroups
	// 每源收获预算覆写：0 = 登记表默认；正值钳 [1, 登记表声明的上游硬顶]
	// （无硬顶声明的源如 TMDB 分页固定，页数覆写仍可用）；地区只收两位代码。
	for id, v := range s.Sources {
		spec, ok := registry.Get(id)
		if !ok {
			continue
		}
		if v.PageSize < 0 {
			v.PageSize = 0
		}
		if v.PageSize > 0 && spec.Harvest.PageSizeMax > 0 {
			v.PageSize = clamp(v.PageSize, 1, spec.Harvest.PageSizeMax)
		}
		v.FullPages = clamp(v.FullPages, 0, 5000)
		v.IncPages = clamp(v.IncPages, 0, 1000)
		if v.TagMinRank < 0 {
			v.TagMinRank = 0
		}
		v.Region = strings.ToLower(strings.TrimSpace(v.Region))
		if v.Region != "" && !regionRe.MatchString(v.Region) {
			v.Region = ""
		}
		s.Sources[id] = v
	}

	// 限流与会话参数：0 = 未设置/恢复默认；钳制既防滥用也防误配把自己锁死
	if s.Security.RateLimitRPS <= 0 {
		s.Security.RateLimitRPS = 30
	}
	if s.Security.RateLimitBurst <= 0 {
		s.Security.RateLimitBurst = 120
	}
	if s.Security.LoginRPM <= 0 {
		s.Security.LoginRPM = 5
	}
	if s.Security.LoginBurst <= 0 {
		s.Security.LoginBurst = 6
	}
	if s.Security.LoginDelayMS <= 0 {
		s.Security.LoginDelayMS = 300
	}
	if s.Security.SessionTTLDays <= 0 {
		s.Security.SessionTTLDays = 7
	}
	if s.Security.LimiterMaxIPs <= 0 {
		s.Security.LimiterMaxIPs = 4096
	}
	if s.Security.MaxPage <= 0 {
		s.Security.MaxPage = 5000
	}
	s.Security.RateLimitRPS = clamp(s.Security.RateLimitRPS, 1, 1000)
	s.Security.RateLimitBurst = clamp(s.Security.RateLimitBurst, 10, 10000)
	s.Security.LoginRPM = clamp(s.Security.LoginRPM, 1, 60)
	s.Security.LoginBurst = clamp(s.Security.LoginBurst, 1, 100)
	s.Security.LoginDelayMS = clamp(s.Security.LoginDelayMS, 0, 2000)
	s.Security.SessionTTLDays = clamp(s.Security.SessionTTLDays, 1, 90)
	s.Security.LimiterMaxIPs = clamp(s.Security.LimiterMaxIPs, 256, 65536)
	s.Security.MaxPage = clamp(s.Security.MaxPage, 100, 100000)

	// DNS 与出站身份：0/未填 = 默认推荐值
	if s.Network.DoHTimeoutSec <= 0 {
		s.Network.DoHTimeoutSec = 6
	}
	s.Network.DoHTimeoutSec = clamp(s.Network.DoHTimeoutSec, 1, 30)
	if s.Network.DnsCacheMinSec <= 0 {
		s.Network.DnsCacheMinSec = 60
	}
	s.Network.DnsCacheMinSec = clamp(s.Network.DnsCacheMinSec, 10, 3600)
	if s.Network.DnsCacheTTLSec <= 0 {
		s.Network.DnsCacheTTLSec = 300
	}
	s.Network.DnsCacheTTLSec = clamp(s.Network.DnsCacheTTLSec, 30, 86400)
	s.Network.OutboundUA = strings.TrimSpace(s.Network.OutboundUA)
	if len(s.Network.OutboundUA) > 200 {
		s.Network.OutboundUA = s.Network.OutboundUA[:200]
	}

	// 服务器与面板参数：0/未填 = 默认推荐值（会话缓存只允许更保守，防止
	// 误配把「缓存 + 落库节流」这条性能红线关掉）
	sv := &s.Server
	if sv.SessionCacheTTLSec <= 0 {
		sv.SessionCacheTTLSec = 60
	}
	sv.SessionCacheTTLSec = clamp(sv.SessionCacheTTLSec, 5, 600)
	if sv.SessionSeenGapSec <= 0 {
		sv.SessionSeenGapSec = 600
	}
	sv.SessionSeenGapSec = clamp(sv.SessionSeenGapSec, 30, 3600)
	if sv.SessionCacheMax <= 0 {
		sv.SessionCacheMax = 512
	}
	sv.SessionCacheMax = clamp(sv.SessionCacheMax, 64, 65536)
	if sv.MinCompressBytes <= 0 {
		sv.MinCompressBytes = 860
	}
	sv.MinCompressBytes = clamp(sv.MinCompressBytes, 200, 65536)
	if sv.SnapshotTTLSec <= 0 {
		sv.SnapshotTTLSec = 60
	}
	sv.SnapshotTTLSec = clamp(sv.SnapshotTTLSec, 5, 600)
	if sv.PerfSampleCap <= 0 {
		sv.PerfSampleCap = 256
	}
	sv.PerfSampleCap = clamp(sv.PerfSampleCap, 32, 4096)
	if sv.RecentReqCap <= 0 {
		sv.RecentReqCap = 256
	}
	sv.RecentReqCap = clamp(sv.RecentReqCap, 32, 4096)
	if sv.OverviewRuns <= 0 {
		sv.OverviewRuns = 24
	}
	sv.OverviewRuns = clamp(sv.OverviewRuns, 5, 100)
	if sv.ShutdownWaitSec <= 0 {
		sv.ShutdownWaitSec = 10
	}
	sv.ShutdownWaitSec = clamp(sv.ShutdownWaitSec, 1, 120)
	if sv.AdminPollSec <= 0 {
		sv.AdminPollSec = 4
	}
	sv.AdminPollSec = clamp(sv.AdminPollSec, 2, 60)
	if sv.PageCacheMax <= 0 {
		sv.PageCacheMax = 12
	}
	sv.PageCacheMax = clamp(sv.PageCacheMax, 0, 50)

	// 数据源区块：登记表声明的源全部补默认（启用 + 优先序 + 评分权重）；
	// 清单之外的键丢弃——登记表是唯一事实源，设置只允许覆写已声明的源。
	def := registry.All()
	clean := make(map[string]SourceSetting, len(def))
	for _, d := range def {
		v, ok := s.Sources[d.ID]
		if !ok {
			v = SourceSetting{Enabled: true, Priority: d.Priority}
		}
		if v.Priority <= 0 {
			v.Priority = d.Priority // 0/负值 = 未填，回落登记表默认
		}
		if v.Weight == nil || *v.Weight < 0 {
			w := d.Weight
			v.Weight = &w
		} else {
			w := clamp(*v.Weight, 0, 100)
			v.Weight = &w
		}
		// 请求间隔：0 = 登记表默认；上限 60s 防手滑（下限在 EffectiveInterval
		// 按官方限速口径钳，这里不钳让 0 的「用默认」语义保持干净）。
		v.IntervalMS = clamp(v.IntervalMS, 0, 60000)
		// 保留期覆盖：0 = 用全局默认（EffectiveRefreshTTL 取值）。
		v.RefreshTTLDays = clamp(v.RefreshTTLDays, 0, 3650)
		clean[d.ID] = v
	}
	s.Sources = clean

	// 凭据区块：只保留登记表声明的 (来源, 字段) 组合，清单外拒绝在管理端
	// 入口做硬校验，这里兜底丢弃，避免脏键进库。
	if s.Credentials != nil {
		cleanCred := map[string]map[string]string{}
		for src, fields := range s.Credentials {
			if !registry.Exists(src) {
				continue
			}
			for k, val := range fields {
				if registry.DeclaredCredential(src, k) && val != "" {
					if cleanCred[src] == nil {
						cleanCred[src] = map[string]string{}
					}
					cleanCred[src][k] = val
				}
			}
		}
		s.Credentials = cleanCred
	}

	if s.BGM.UA == "" {
		s.BGM.UA = "AniPick/1.0 (+https://github.com/bangumi; local indexer)"
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// LoadSettings 读取 settings.app；不存在时返回 defaults。
func (s *Store) LoadSettings(ctx context.Context, defaults AppSettings) (AppSettings, error) {
	out := defaults
	out.Normalize()

	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'app'`).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("解析 settings.app: %w", err)
	}
	out.Normalize()
	return out, s.decryptSettings(&out)
}

func (s *Store) SaveSettings(ctx context.Context, in *AppSettings) error {
	in.Normalize()
	in.UpdatedAt = time.Now().UTC()
	stored := *in
	if err := s.encryptSettings(&stored); err != nil {
		return err
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ('app', $1, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, raw)
	return err
}

func (s *Store) encryptSettings(in *AppSettings) error {
	if s.Box == nil {
		return nil
	}
	var err error
	if in.BGM.Token, err = s.Box.Seal(in.BGM.Token); err != nil {
		return err
	}
	if in.BGM.Cookie, err = s.Box.Seal(in.BGM.Cookie); err != nil {
		return err
	}
	for src, fields := range in.Credentials {
		for k, val := range fields {
			if val == "" {
				continue
			}
			sealed, serr := s.Box.Seal(val)
			if serr != nil {
				return fmt.Errorf("%s/%s: %w", src, k, serr)
			}
			fields[k] = sealed
		}
	}
	return nil
}

func (s *Store) decryptSettings(in *AppSettings) error {
	var err error
	if in.BGM.Token, err = s.Box.Open(in.BGM.Token); err != nil {
		return fmt.Errorf("BGM Token: %w", err)
	}
	if in.BGM.Cookie, err = s.Box.Open(in.BGM.Cookie); err != nil {
		return fmt.Errorf("BGM Cookie: %w", err)
	}
	for src, fields := range in.Credentials {
		for k, val := range fields {
			if val == "" {
				continue
			}
			plain, oerr := s.Box.Open(val)
			if oerr != nil {
				return fmt.Errorf("%s/%s: %w", src, k, oerr)
			}
			fields[k] = plain
		}
	}
	return nil
}

// Mask 返回脱敏副本，用于管理端回显。
func (s *AppSettings) Mask() AppSettings {
	out := *s
	out.BGM.Token = maskSecret(out.BGM.Token)
	out.BGM.Cookie = maskSecret(out.BGM.Cookie)
	if out.Credentials != nil {
		masked := make(map[string]map[string]string, len(out.Credentials))
		for src, fields := range out.Credentials {
			mf := make(map[string]string, len(fields))
			for k, val := range fields {
				mf[k] = maskSecret(val)
			}
			masked[src] = mf
		}
		out.Credentials = masked
	}
	return out
}

func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return "••••"
	}
	return v[:4] + "••••" + v[len(v)-4:]
}
