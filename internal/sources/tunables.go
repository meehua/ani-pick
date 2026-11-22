package sources

import (
	"strings"
	"sync"
	"time"

	"anipick/internal/registry"
)

// 运行期可调参数（管理设置保存后由快照链路立刷；0/空 = 登记表默认）。
// 数值默认值全部来自登记表声明与 settings.go 的 Normalize 缺省，这里只存覆写。

// SourceParams 是某来源收获预算的运行期覆写。
type SourceParams struct {
	PageSize   int    // 单页条数（上游硬顶在 store.EffectiveHarvest 已钳）
	FullPages  int    // 全量页数预算
	IncPages   int    // 增量页数预算
	TagMinRank int    // 标签相关度入库门槛
	Region     string // storefront 地区
}

var (
	paramsMu  sync.RWMutex
	sourceOv  = map[string]SourceParams{}
	tagMaxOV  int
	fetchP    fetchParams
	defaultUA = "AniPick/1.0 (+https://github.com/bgm-finder; multi-source indexer)"
)

// fetchParams 是扩展源出站通道的共享参数（原本为 httpx 具名常量）。
type fetchParams struct {
	attempts      int
	backoff       time.Duration
	respLimit     int64
	budget        time.Duration
	retryAfterCap time.Duration
	ua            string
}

func init() {
	fetchP = fetchParams{attempts: 3, backoff: 2 * time.Second, respLimit: 8 << 20,
		budget: 30 * time.Second, retryAfterCap: 30 * time.Second, ua: defaultUA}
	tagMaxOV = 8
}

// SetSourceParams 覆写某来源的收获预算（保存数据源设置后调用；零值字段忽略）。
func SetSourceParams(source string, p SourceParams) {
	paramsMu.Lock()
	defer paramsMu.Unlock()
	sourceOv[source] = p
}

// SetTagMax 覆写扩展源单条目的标签数上限。
func SetTagMax(n int) {
	paramsMu.Lock()
	defer paramsMu.Unlock()
	if n > 0 {
		tagMaxOV = n
	}
}

// SetFetchParams 覆写扩展源出站通道参数（保存设置后调用；零值字段忽略）。
func SetFetchParams(attempts int, backoff time.Duration, respLimit int64, budget, retryAfterCap time.Duration, ua string) {
	paramsMu.Lock()
	defer paramsMu.Unlock()
	if attempts > 0 {
		fetchP.attempts = attempts
	}
	if retryAfterCap > 0 {
		fetchP.retryAfterCap = retryAfterCap
	}
	if backoff > 0 {
		fetchP.backoff = backoff
	}
	if respLimit > 0 {
		fetchP.respLimit = respLimit
	}
	if budget > 0 {
		fetchP.budget = budget
	}
	if strings.TrimSpace(ua) != "" {
		fetchP.ua = strings.TrimSpace(ua)
	}
}

// effectiveParams 合成来源的有效预算：覆写 ?? 登记表默认。
func effectiveParams(spec registry.Source) SourceParams {
	paramsMu.RLock()
	ov := sourceOv[spec.ID]
	paramsMu.RUnlock()
	p := SourceParams{
		PageSize:   spec.Harvest.PageSize,
		FullPages:  spec.Harvest.FullPages,
		IncPages:   spec.Harvest.IncPages,
		TagMinRank: spec.Harvest.TagMinRank,
		Region:     spec.Harvest.Region,
	}
	if ov.PageSize > 0 {
		p.PageSize = ov.PageSize
	}
	if ov.FullPages > 0 {
		p.FullPages = ov.FullPages
	}
	if ov.IncPages > 0 {
		p.IncPages = ov.IncPages
	}
	if ov.TagMinRank > 0 {
		p.TagMinRank = ov.TagMinRank
	}
	if ov.Region != "" {
		p.Region = ov.Region
	}
	return p
}

// currentFetchParams 返回出站通道参数快照（do() 每请求取一次）。
func currentFetchParams() fetchParams {
	paramsMu.RLock()
	defer paramsMu.RUnlock()
	return fetchP
}

// currentTagMax 返回扩展源标签上限快照。
func currentTagMax() int {
	paramsMu.RLock()
	defer paramsMu.RUnlock()
	return tagMaxOV
}
