// Package crawler 负责后台索引抓取。
//
// 三种模式：
//   - full        全量干净抓取：逐类型顺序枚举全部条目，结束后清理本轮未覆盖的旧条目
//   - incremental 增量抓取：各类型最新若干页 + 一批过期条目重爬
//   - refresh     过期重爬：只处理超过 TTL 的条目
package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"anipick/internal/bgm"
	"anipick/internal/registry"
	"anipick/internal/sources"
	"anipick/internal/store"
)

var ErrBusy = errors.New("已有抓取任务在运行")

type Progress struct {
	Phase     string    `json:"phase"`
	Source    string    `json:"source,omitempty"` // 当前阶段的数据来源（合并运行随阶段切换）
	Type      int       `json:"type"`
	Offset    int       `json:"offset"`
	Total     int       `json:"total"`
	Pages     int       `json:"pages"`
	Requests  int       `json:"requests"`
	Inserted  int       `json:"inserted"`
	Updated   int       `json:"updated"`
	Refreshed int       `json:"refreshed"`
	Errors    int       `json:"errors"`
	Skipped   int       `json:"skipped"`
	Pruned    int       `json:"pruned"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

type State struct {
	Running   bool      `json:"running"`
	RunID     int64     `json:"run_id"`
	Mode      string    `json:"mode"`
	Trigger   string    `json:"trigger"`
	StartedAt time.Time `json:"started_at"`
	Progress  Progress  `json:"progress"`
}

type Crawler struct {
	st         *store.Store
	defaults   store.AppSettings
	httpClient atomic.Pointer[http.Client] // 出站客户端（出站代理设置保存后运行期换新）
	harvesters []sources.Harvester

	// OnRunFinished 在每轮抓取收尾时回调（无论 done/failed/canceled），
	// 服务层用它失效查询结果缓存——条目/标签数据只在抓取中变化。
	OnRunFinished func()

	mu     sync.Mutex
	cancel context.CancelFunc
	state  State

	// tickSec 是调度器检查周期（秒）的设置快照；tick 每轮刷新，Scheduler 用。
	tickSec atomic.Int64
}

// New 装配抓取器。harvesters 是扩展源适配器注册表（合并运行在 bgm 阶段后
// 串行跑各源；顺序即注册顺序）。
func New(st *store.Store, defaults store.AppSettings, httpClient *http.Client, harvesters []sources.Harvester) *Crawler {
	c := &Crawler{st: st, defaults: defaults, harvesters: harvesters}
	c.httpClient.Store(httpClient)
	return c
}

// SetHTTPClient 运行期更换出站客户端（server 保存出站代理设置后调用），
// bgm 抓取与扩展源收获的后续请求随即走新通道。
func (c *Crawler) SetHTTPClient(hc *http.Client) {
	c.httpClient.Store(hc)
}

// Harvester 按来源标识查注册表（凭据校验端点按源分派用）。
func (c *Crawler) Harvester(source string) sources.Harvester {
	for _, h := range c.harvesters {
		if h.Source() == source {
			return h
		}
	}
	return nil
}

// Harvesters 返回已注册的扩展源适配器（管理面板展示配置状态用）。
func (c *Crawler) Harvesters() []sources.Harvester { return c.harvesters }

func (c *Crawler) Status() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Start 创建合并抓取任务并异步执行。mode: full | incremental | refresh
// （原生索引主行先行，随后各扩展源逐个抓取、各自一行运行记录）。
// 调度器自动任务用；面板上的手动抓取走 StartSource（每个源只抓自己）。
func (c *Crawler) Start(mode, trigger string) (int64, error) {
	if mode != "full" && mode != "incremental" && mode != "refresh" {
		return 0, fmt.Errorf("未知抓取模式 %q", mode)
	}
	return c.start(mode, trigger, registry.NativeID(), true)
}

// StartSource 手动抓取单个数据源：模式必须在该源的登记表能力清单里
// （bgm 声明 full/incremental/refresh；扩展源声明 harvest/harvest_incremental，
// iTunes 这类无增量的源只声明 harvest）——面板按钮组按同一份声明渲染，
// 后端这里是同一份清单的门。
func (c *Crawler) StartSource(mode, source, trigger string) (int64, error) {
	spec, ok := registry.Get(source)
	if !ok {
		return 0, fmt.Errorf("未知数据源 %q", source)
	}
	if !registry.HasMode(source, mode) {
		return 0, fmt.Errorf("%s 不支持「%s」", spec.Label, registry.ModeLabels[mode])
	}
	if !spec.Native {
		// 扩展源：凭据已配置且设置启用中才能抓
		h := c.Harvester(source)
		if h == nil {
			return 0, fmt.Errorf("未知数据源 %q", source)
		}
		if !h.Configured() {
			return 0, fmt.Errorf("%s 未配置，无法抓取", spec.Label)
		}
		settings, err := c.st.LoadSettings(context.Background(), c.defaults)
		if err != nil {
			return 0, err
		}
		if !settings.Sources[source].Enabled {
			return 0, fmt.Errorf("%s 已在设置中停用，无法抓取", spec.Label)
		}
	}
	return c.start(mode, trigger, source, false)
}

// enabledHarvesters 返回设置里启用中的扩展源，按登记表优先级排序
// （设置可调优先级；同级保持注册顺序）。
func (c *Crawler) enabledHarvesters(settings store.AppSettings) []sources.Harvester {
	out := make([]sources.Harvester, 0, len(c.harvesters))
	for _, h := range c.harvesters {
		if settings.Sources[h.Source()].Enabled {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return store.SourcePriority(out[i].Source(), settings.Sources) <
			store.SourcePriority(out[j].Source(), settings.Sources)
	})
	return out
}

func (c *Crawler) start(mode, trigger, source string, combined bool) (int64, error) {
	settings, err := c.st.LoadSettings(context.Background(), c.defaults)
	if err != nil {
		return 0, err
	}
	types := settings.Crawl.Types
	if src, ok := registry.Get(source); !ok || !src.Native {
		// 扩展源（或未知来源）：类型按登记表声明，不混用原生索引的抓取类型
		types = c.Harvester(source).Types()
	}

	c.mu.Lock()
	if c.state.Running {
		c.mu.Unlock()
		return 0, ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	runID, err := c.st.CreateRun(ctx, mode, trigger, source, types)
	if err != nil {
		cancel()
		c.mu.Unlock()
		return 0, err
	}
	c.cancel = cancel
	c.state = State{
		Running:   true,
		RunID:     runID,
		Mode:      mode,
		Trigger:   trigger,
		StartedAt: time.Now(),
		Progress:  Progress{Phase: "start", Source: source, Message: "准备中", UpdatedAt: time.Now()},
	}
	c.mu.Unlock()

	go c.run(ctx, runID, mode, trigger, source, settings, combined)
	return runID, nil
}

func (c *Crawler) Cancel() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.Running || c.cancel == nil {
		return false
	}
	c.cancel()
	c.state.Progress.Message = "正在停止…"
	return true
}

func (c *Crawler) run(ctx context.Context, runID int64, mode, trigger, source string, settings store.AppSettings, combined bool) {
	client := c.client(settings)
	prog := &Progress{Phase: "start", Source: source, UpdatedAt: time.Now()}
	extTouched := false // 扩展源是否写入过条目（决定收尾是否重算标签热度）

	defer func() {
		c.mu.Lock()
		c.state.Running = false
		c.cancel = nil
		c.mu.Unlock()
		// 失败/取消同样可能已写入部分条目（增量逐批提交），一律失效查询缓存；
		// 评分合成不在这里做：写入/清理/删除各写路径已在自身事务内按组收口
		if c.OnRunFinished != nil {
			c.OnRunFinished()
		}
	}()

	// updateRow 把指定行的进度写入 DB 并同步到状态面板。
	updateRow := func(rowID int64, p *Progress, phase, msg string) {
		p.Phase = phase
		if msg != "" {
			p.Message = msg
		}
		p.UpdatedAt = time.Now()
		_ = c.st.UpdateRunStats(context.Background(), rowID, p)
		c.mu.Lock()
		c.state.Progress = *p
		c.mu.Unlock()
	}
	update := func(phase, msg string) { updateRow(runID, prog, phase, msg) }

	// finishRow 落一行运行的终态（done/failed/canceled/skipped）。
	finishRow := func(rowID int64, p *Progress, status, errMsg string) {
		if ctx.Err() != nil && status == "done" {
			status = "canceled"
		}
		fp := *p
		fp.Phase = "done"
		fp.UpdatedAt = time.Now()
		_ = c.st.UpdateRunStats(context.Background(), rowID, fp)
		_ = c.st.FinishRun(context.Background(), rowID, status, errMsg)
	}

	// 单源抓取（扩展源手动按钮：harvest=全量预算 / harvest_incremental=增量预算）：
	// runID 即该源自己的运行行。
	if mode == "harvest" || mode == "harvest_incremental" {
		h := c.Harvester(source)
		err := c.harvestOne(ctx, h, runID, mode == "harvest_incremental", settings, prog, update)
		status, errMsg := "done", ""
		switch {
		case err == nil:
			update("done", h.Label()+" 抓取完成")
		case errors.Is(err, sources.ErrNotConfigured):
			status, errMsg = "skipped", h.Label()+" 未配置，已跳过"
			prog.Message = errMsg
		case ctx.Err() != nil:
			status, errMsg = "canceled", "任务已手动停止"
			prog.Message = errMsg
		default:
			status, errMsg = "failed", err.Error()
			prog.Message = "失败: " + errMsg
			slog.Error("抓取失败", "run", runID, "source", source, "err", err)
		}
		finishRow(runID, prog, status, errMsg)
		if prog.Inserted+prog.Updated > 0 {
			update("tags", "重算标签热度")
			if err := c.st.RecomputeTagUsage(ctx); err != nil {
				slog.Error("重算标签热度失败", "run", runID, "err", err)
			}
		}
		return
	}

	// 合并运行：bgm 主行先行。
	var bgmErr error
	switch mode {
	case "full":
		bgmErr = c.runFull(ctx, client, runID, settings, prog, update)
	case "incremental":
		bgmErr = c.runIncremental(ctx, client, runID, settings, prog, update)
	default: // refresh：原生索引过期重爬 + 有过期条目的扩展源各自重抓
		bgmErr = c.runRefresh(ctx, client, runID, settings, prog, update)
	}
	bgmStatus, bgmMsg := "done", ""
	switch {
	case bgmErr == nil:
	case ctx.Err() != nil:
		bgmStatus, bgmMsg = "canceled", "任务已手动停止"
		prog.Message = bgmMsg
	default:
		bgmStatus, bgmMsg = "failed", bgmErr.Error()
		prog.Message = "失败: " + bgmMsg
		slog.Error("bgm 抓取失败", "run", runID, "mode", mode, "err", bgmErr)
	}
	finishRow(runID, prog, bgmStatus, bgmMsg)
	if ctx.Err() != nil {
		return // 取消：不再跑扩展源
	}

	// 扩展源逐个抓取（仅合并运行）：按登记表优先序、只抓取设置启用中的源；
	// 每个源独立运行行与计数，失败记录后继续下一个源。
	// 手动单源运行（combined=false）只抓自己声明的模式，不带动别的源。
	// 过期重爬合并运行时，扩展源也纳入新鲜度管理：只重抓条目已超过各自
	// 有效保留期（源覆盖 ?? 全局默认）的源，没过期的跳过、不建运行行。
	if combined && (mode == "full" || mode == "incremental" || mode == "refresh") {
		var failed []string
		staleBySource := map[string]int{}
		if mode == "refresh" {
			hs := c.enabledHarvesters(settings)
			srcs := make([]string, 0, len(hs))
			ttls := make([]int, 0, len(hs))
			for _, h := range hs {
				srcs = append(srcs, h.Source())
				ttls = append(ttls, store.EffectiveRefreshTTL(h.Source(), settings.Sources, settings.Crawl.RefreshTTLDays))
			}
			stale, serr := c.st.CountStaleBySource(ctx, srcs, ttls)
			if serr != nil {
				slog.Warn("统计各源过期条目失败，本轮跳过扩展源重抓", "err", serr)
			}
			staleBySource = stale
		}
		for i, h := range c.enabledHarvesters(settings) {
			if ctx.Err() != nil {
				break
			}
			if mode == "refresh" && staleBySource[h.Source()] == 0 {
				continue // 未过期：该源不需要重抓
			}
			if i > 0 && !delay(ctx, time.Duration(settings.Crawl.SourceGapMS)*time.Millisecond) {
				break
			}
			rowProg := &Progress{Phase: "start", Source: h.Source()}
			rowID, rerr := c.st.CreateRun(ctx, mode, trigger, h.Source(), h.Types())
			if rerr != nil {
				slog.Warn("创建数据源运行记录失败", "source", h.Source(), "err", rerr)
				continue
			}
			updateRow(rowID, rowProg, "start", h.Label()+" 准备中")
			err := c.harvestOne(ctx, h, rowID, mode == "incremental", settings, rowProg,
				func(phase, msg string) { updateRow(rowID, rowProg, phase, msg) })
			status, errMsg := "done", ""
			switch {
			case err == nil:
				updateRow(rowID, rowProg, "done", h.Label()+" 抓取完成")
				extTouched = extTouched || rowProg.Inserted+rowProg.Updated > 0
			case errors.Is(err, sources.ErrNotConfigured):
				status, errMsg = "skipped", h.Label()+" 未配置，已跳过"
				rowProg.Message = errMsg
			case ctx.Err() != nil:
				status, errMsg = "canceled", "任务已手动停止"
				rowProg.Message = errMsg
			default:
				status, errMsg = "failed", err.Error()
				rowProg.Message = "失败: " + errMsg
				failed = append(failed, h.Label())
				slog.Error("抓取失败", "run", rowID, "source", h.Source(), "err", err)
			}
			finishRow(rowID, rowProg, status, errMsg)
			if status == "skipped" {
				updateRow(rowID, rowProg, "done", errMsg)
			}
		}
		if len(failed) > 0 {
			prog.Message = "部分来源失败: " + strings.Join(failed, "、")
		}
	}

	// 标签热度统一在运行末尾重算一次（原生索引阶段与扩展源抓取都动过 subject_tags）。
	touched := extTouched || prog.Inserted+prog.Updated+prog.Refreshed > 0
	if touched {
		snap := Progress{Phase: "tags", Source: registry.NativeID(), Message: "重算标签热度", UpdatedAt: time.Now()}
		c.mu.Lock()
		c.state.Progress = snap
		c.mu.Unlock()
		if err := c.st.RecomputeTagUsage(ctx); err != nil {
			slog.Error("重算标签热度失败", "run", runID, "err", err)
			prog.Message = "重算标签热度失败: " + err.Error()
		}
	}
	c.mu.Lock()
	c.state.Progress = Progress{Phase: "done", Source: prog.Source, Message: prog.Message, UpdatedAt: time.Now()}
	c.mu.Unlock()
}

// harvestOne 执行单个扩展源的抓取（incremental=false 用全量预算）。
func (c *Crawler) harvestOne(ctx context.Context, h sources.Harvester, rowID int64, incremental bool,
	settings store.AppSettings, prog *Progress, update func(string, string)) error {
	scope := &sources.Scope{
		Ctx:   ctx,
		Store: c.st,
		RunID: rowID,
		Update: func(phase, msg string) {
			update(phase, msg)
		},
		Add: func(field string, n int) {
			switch field {
			case "requests":
				prog.Requests += n
			case "pages":
				prog.Pages += n
			case "inserted":
				prog.Inserted += n
			case "updated":
				prog.Updated += n
			case "errors":
				prog.Errors += n
			}
		},
		HTTP: c.httpClient.Load(),
	}
	// 收获预算：登记表默认 + 管理设置按源覆写（单页条数/页数/标签门槛/地区）
	ps, fp, ip, tr, rg := store.EffectiveHarvest(h.Source(), settings.Sources)
	scope.PageSize, scope.FullPages, scope.IncPages, scope.TagMinRank, scope.Region = ps, fp, ip, tr, rg
	if incremental {
		// 增量时间窗锚点：该源最近一次完成的增量运行；查不到 = 空数据，
		// 适配器自动把本轮升级为全量预算（条目不能丢）
		if last, err := c.st.LastSourceRun(ctx, h.Source()); err == nil && last != nil {
			scope.LastIncremental = last.StartedAt
		}
	}
	if !h.Configured() {
		return sources.ErrNotConfigured
	}
	if incremental {
		return h.HarvestIncremental(scope)
	}
	return h.HarvestFull(scope)
}

func (c *Crawler) client(settings store.AppSettings) *bgm.Client {
	timeout := time.Duration(settings.Crawl.RequestTimeoutSec) * time.Second
	return bgm.NewWithHTTPClient(c.httpClient.Load(), settings.BGM.Token, settings.BGM.Cookie, settings.BGM.UA, timeout)
}

func delay(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (c *Crawler) runFull(ctx context.Context, client *bgm.Client, runID int64, settings store.AppSettings, prog *Progress, update func(string, string)) error {
	types := settings.Crawl.Types
	pageSize := settings.Crawl.PageSize
	wait := time.Duration(store.EffectiveInterval(registry.SourceBGM, settings.Sources)) * time.Millisecond

	for _, typ := range types {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prog.Type = typ
		prog.Offset = 0
		prog.Total = 0
		offset := 0
		total := -1
		pageFails := 0
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			page, err := client.ListSubjects(ctx, typ, "date", pageSize, offset)
			if err != nil {
				if errors.Is(err, bgm.ErrUnauthorized) {
					return fmt.Errorf("Bangumi 凭据无效，请检查管理后台的访问凭据")
				}
				prog.Errors++
				if pageFails < settings.Crawl.PageFailRetries {
					pageFails++
					prog.Message = fmt.Sprintf("类型 %d offset %d 拉取失败，重试 %d/%d: %v", typ, offset, pageFails, settings.Crawl.PageFailRetries, err)
					update("enumerate", prog.Message)
					if !delay(ctx, time.Duration(settings.Crawl.PageRetryWaitSec)*time.Second) {
						return ctx.Err()
					}
					continue
				}
				// 同一页多次失败后跳过，并记为 skipped（本次不做清理，避免误删）
				prog.Skipped++
				pageFails = 0
				prog.Message = fmt.Sprintf("类型 %d offset %d 已跳过: %v", typ, offset, err)
				update("enumerate", prog.Message)
				offset += pageSize
				continue
			}
			pageFails = 0
			prog.Requests++
			prog.Pages++
			if total < 0 {
				total = page.Total
				prog.Total = total
			}
			if len(page.Data) == 0 {
				break
			}
			recs := convertAll(page.Data)
			ins, upd, err := c.st.UpsertSubjects(ctx, recs, runID)
			if err != nil {
				return fmt.Errorf("写入索引失败: %w", err)
			}
			prog.Inserted += ins
			prog.Updated += upd
			offset += len(page.Data)
			prog.Offset = offset
			update("enumerate", fmt.Sprintf("类型 %d：%d/%d", typ, offset, total))
			if offset >= total {
				break
			}
			if !delay(ctx, wait) {
				return ctx.Err()
			}
		}
	}

	if settings.Crawl.PruneOnFull && prog.Skipped == 0 {
		update("prune", "清理本轮未覆盖的条目")
		pruned, err := c.st.Prune(ctx, runID, types)
		if err != nil {
			return fmt.Errorf("清理旧条目失败: %w", err)
		}
		prog.Pruned = int(pruned)
	}

	// 标签热度由 run() 在运行末尾统一重算（合并运行还要跑扩展源抓取）
	update("done", "bgm 全量抓取完成")
	return nil
}

func (c *Crawler) runIncremental(ctx context.Context, client *bgm.Client, runID int64, settings store.AppSettings, prog *Progress, update func(string, string)) error {
	types := settings.Crawl.Types
	pageSize := settings.Crawl.PageSize
	wait := time.Duration(store.EffectiveInterval(registry.SourceBGM, settings.Sources)) * time.Millisecond
	pages := settings.Crawl.IncrementalPages

	for _, typ := range types {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prog.Type = typ
		// 空类型判空：该类型在库为 0（首次安装/清空后）时，本轮增量对该类型
		// 自动按全量跑到底（跑完即止，不触发全量清理），保证条目不丢
		typePages := pages
		if n, err := c.st.CountNativeType(ctx, typ); err == nil && n == 0 {
			typePages = 0 // 0 = 不设页数上限，直到空页为止
			update("enumerate", fmt.Sprintf("类型 %d 在库为空，本轮按全量补齐", typ))
		}
		for page := 0; typePages <= 0 || page < typePages; page++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			list, err := client.ListSubjects(ctx, typ, "date", pageSize, page*pageSize)
			if err != nil {
				prog.Errors++
				prog.Message = fmt.Sprintf("类型 %d 第 %d 页失败: %v", typ, page, err)
				update("enumerate", prog.Message)
				break
			}
			prog.Requests++
			prog.Pages++
			if len(list.Data) == 0 {
				break
			}
			ins, upd, err := c.st.UpsertSubjects(ctx, convertAll(list.Data), runID)
			if err != nil {
				return err
			}
			prog.Inserted += ins
			prog.Updated += upd
			update("enumerate", fmt.Sprintf("类型 %d：最新 %d 条", typ, (page+1)*pageSize))
			if !delay(ctx, wait) {
				return ctx.Err()
			}
		}
	}

	if err := c.runRefresh(ctx, client, runID, settings, prog, update); err != nil {
		return err
	}

	// 标签热度由 run() 在运行末尾统一重算
	update("done", "bgm 增量抓取完成")
	return nil
}

func (c *Crawler) runRefresh(ctx context.Context, client *bgm.Client, runID int64, settings store.AppSettings, prog *Progress, update func(string, string)) error {
	prog.Phase = "refresh"
	update("refresh", "查找过期条目")
	// 保留期读该源的有效值（源覆盖 ?? 全局默认）——新鲜度判定统一走这里
	ttl := store.EffectiveRefreshTTL(registry.NativeID(), settings.Sources, settings.Crawl.RefreshTTLDays)
	ids, err := c.st.StaleSubjects(ctx, ttl, settings.Crawl.StaleRetryCap, settings.Crawl.RefreshBatch, settings.Crawl.Types)
	if err != nil {
		return fmt.Errorf("查询过期条目失败: %w", err)
	}
	prog.Total = len(ids)
	wait := time.Duration(store.EffectiveInterval(registry.SourceBGM, settings.Sources)) * time.Millisecond
	for i, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sub, err := client.Subject(ctx, id)
		if err != nil {
			prog.Errors++
			if errors.Is(err, bgm.ErrNotFound) || errors.Is(err, bgm.ErrUnauthorized) {
				_ = c.st.MarkUnavailable(ctx, id)
			} else {
				_ = c.st.MarkFailed(ctx, id)
			}
			if !delay(ctx, wait) {
				return ctx.Err()
			}
			continue
		}
		prog.Requests++
		prog.Refreshed++
		prog.Offset = i + 1
		if _, _, err := c.st.UpsertSubjects(ctx, convertAll([]bgm.Subject{*sub}), 0); err != nil {
			return err
		}
		if i%settings.Crawl.RefreshProgressEvery == 0 {
			update("refresh", fmt.Sprintf("过期重爬：%d/%d", i+1, len(ids)))
		}
		if !delay(ctx, wait) {
			return ctx.Err()
		}
	}
	update("refresh", fmt.Sprintf("过期重爬完成：%d 条", prog.Refreshed))
	return nil
}

// RematchWorks 重跑作品匹配（管理端使用）：先对存量扩展源行做完整回填
// （确定性键 + 候选标题集——存量行入库早于 alt_titles 抽取时列是空的，
// 不回填标题则启发式只能拿主名硬碰，命中率极低），再全量重归组。
// 只有 anilist 需要回填（idMal 是异源键）；mal 行写入时 ext_key 自带
// 自己的 MAL id，无回填路径。
func (c *Crawler) RematchWorks(ctx context.Context) (store.RematchStats, error) {
	if err := c.backfillRecords(ctx, store.SourceAniList); err != nil {
		return store.RematchStats{}, err
	}
	stats, err := c.st.RematchWorks(ctx)
	if err != nil {
		return stats, err
	}
	// 组归并/重排后按当前权重重算合成评分：收尾动作不随请求取消
	//（生产 169k 行全量重算实测 7 分半），独立 context 给足整体超时
	sctx, scancel := context.WithTimeout(context.Background(), store.SynthesizeOverallTimeout)
	defer scancel()
	if _, err := c.st.SynthesizeScores(sctx); err != nil {
		return stats, err
	}
	return stats, nil
}

// backfillRecords 对某来源缺回填的行走适配器的 BackfillRecords 并落库。
func (c *Crawler) backfillRecords(ctx context.Context, source string) error {
	refs, err := c.st.ExtRefsMissingBackfill(ctx, source)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	slog.Info("回填存量行的确定性键与标题集", "source", source, "rows", len(refs))

	var recs map[string]store.BackfillRecord
	switch source {
	case store.SourceAniList:
		h, ok := c.Harvester(source).(*sources.AniList)
		if !ok {
			return fmt.Errorf("anilist 适配器不可用")
		}
		raw, err := h.BackfillRecords(ctx, refs)
		if err != nil {
			return err
		}
		recs = make(map[string]store.BackfillRecord, len(raw))
		for ref, r := range raw {
			key := ""
			if r.IDMal > 0 {
				key = strconv.FormatInt(r.IDMal, 10)
			}
			recs[ref] = store.BackfillRecord{ExtKey: key, AltTitles: r.Titles}
		}
	default:
		return fmt.Errorf("来源 %s 无回填路径", source)
	}

	n, err := c.st.SetBackfill(ctx, source, recs)
	if err != nil {
		return err
	}
	slog.Info("存量回填完成", "source", source, "upstream", len(recs), "updated", n)
	return nil
}

// RefreshOne 立即重爬单个条目（管理端使用）：bgm 走抓取客户端，
// 扩展源走各自适配器的单条记录接口，身份仍由 UpsertSubjects 收口。
func (c *Crawler) RefreshOne(ctx context.Context, id int64) (store.Subject, error) {
	cur, err := c.st.GetSubject(ctx, id)
	if err != nil {
		return store.Subject{}, err
	}
	if cur.Source != "" && cur.Source != registry.NativeID() {
		// 扩展源走各自适配器的单条记录接口（简介随载荷入库）
		return c.refreshExtOne(ctx, cur)
	}

	settings, err := c.st.LoadSettings(ctx, c.defaults)
	if err != nil {
		return store.Subject{}, err
	}
	client := c.client(settings)
	sub, err := client.Subject(ctx, id)
	if err != nil {
		return store.Subject{}, err
	}
	recs := convertAll([]bgm.Subject{*sub})
	if _, _, err := c.st.UpsertSubjects(ctx, recs, 0); err != nil {
		return store.Subject{}, err
	}
	return c.st.GetSubject(ctx, id)
}

// refreshExtOne 用对应适配器刷新一条扩展源条目。
func (c *Crawler) refreshExtOne(ctx context.Context, cur store.Subject) (store.Subject, error) {
	h := c.Harvester(cur.Source)
	if h == nil {
		return store.Subject{}, fmt.Errorf("未知数据源 %q", cur.Source)
	}
	if !h.Configured() {
		return store.Subject{}, fmt.Errorf("%s 未配置，无法刷新", h.Label())
	}
	rec, err := h.FetchRecord(ctx, cur.SourceRef)
	if err != nil {
		if errors.Is(err, sources.ErrNotFound) {
			_ = c.st.MarkUnavailable(ctx, cur.ID)
		}
		return store.Subject{}, err
	}
	if rec.ID == 0 {
		rec.ID = cur.ID // 保持公开 id 稳定（身份解析按 (source, source_ref) 也能命中）
	}
	if _, _, err := c.st.UpsertSubjects(ctx, []store.Subject{rec}, 0); err != nil {
		return store.Subject{}, err
	}
	return c.st.GetSubject(ctx, cur.ID)
}

// 调度器与抓取循环的可调参数全部来自管理设置（AppSettings.Crawl）：
// 检查周期/页重试/播报节奏/回看条数/源间间隔，tick 每轮读设置并刷新快照。
const defaultSchedulerTickSec = 120 // 设置缺省时的兜底（首次 tick 前的检查周期）

// Scheduler 周期性触发自动增量/过期重爬；ctx 取消时退出。
// 检查周期是运行期设置（crawler_tick_sec），每轮 tick 后按最新设置重排下次检查。
func (c *Crawler) Scheduler(ctx context.Context) {
	ticker := time.NewTicker(c.currentTick())
	defer ticker.Stop()
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
			ticker.Reset(c.currentTick())
		}
	}
}

// currentTick 返回当前设置的调度检查周期（设置快照未就绪时用缺省值）。
func (c *Crawler) currentTick() time.Duration {
	if v := c.tickSec.Load(); v > 0 {
		return time.Duration(v) * time.Second
	}
	return defaultSchedulerTickSec * time.Second
}

func (c *Crawler) tick(ctx context.Context) {
	if c.Status().Running {
		return
	}
	settings, err := c.st.LoadSettings(ctx, c.defaults)
	if err != nil {
		slog.Warn("读取抓取配置失败", "err", err)
		return
	}
	c.tickSec.Store(int64(settings.Crawl.SchedulerTickSec))
	runs, err := c.st.RecentRuns(ctx, settings.Crawl.SchedulerLookback)
	if err != nil {
		return
	}
	var lastIncrementalDone, lastAnyDone *store.Run
	for i := range runs {
		r := &runs[i]
		if r.Status == "running" {
			return
		}
		if r.Status != "done" {
			continue
		}
		if lastAnyDone == nil {
			lastAnyDone = r
		}
		if lastIncrementalDone == nil && r.Mode == "incremental" {
			lastIncrementalDone = r
		}
	}
	now := time.Now()

	// 自动增量：interval 看上轮增量结束；daily/every_days 看上次自动增量的开始时间
	if settings.Crawl.AutoIncremental {
		if c.autoTaskDue(ctx, now, settings.Crawl.IncrementalSchedule, "incremental", lastIncrementalDone) {
			if _, err := c.Start("incremental", "auto"); err != nil && !errors.Is(err, ErrBusy) {
				slog.Warn("自动增量抓取启动失败", "err", err)
			}
			return
		}
	}
	if settings.Crawl.AutoRefresh {
		// 过期重爬的 interval 口径沿用旧版：距「任意一轮」结束的防抖窗口，
		// 人工跑过的任务也占用窗口；daily/every_days 只认自动触发的时隙
		if c.autoTaskDue(ctx, now, settings.Crawl.RefreshSchedule, "refresh", lastAnyDone) {
			if _, err := c.Start("refresh", "auto"); err != nil && !errors.Is(err, ErrBusy) {
				slog.Warn("自动过期重爬启动失败", "err", err)
			}
		}
	}
}

// autoTaskDue 判断一个自动任务此刻是否到期。lastDone 是该任务在 interval 口径下
// 的锚点（对应运行结束时间）；daily/every_days 口径下另外查上次自动触发的运行。
func (c *Crawler) autoTaskDue(ctx context.Context, now time.Time, s store.AutoSchedule, mode string, lastDone *store.Run) bool {
	switch s.Kind {
	case "daily", "every_days":
		lastFire, err := c.st.LastRun(ctx, mode, "auto")
		if err != nil {
			return false // 查不到锚点宁可不跑，下轮再判
		}
		everyDays := 1
		if s.Kind == "every_days" {
			everyDays = s.EveryDays
		}
		var anchor *time.Time
		if lastFire != nil {
			anchor = &lastFire.StartedAt
		}
		return dailyDue(now, everyDays, s.AtMinute, anchor)
	default:
		var finished *time.Time
		if lastDone != nil {
			finished = lastDone.FinishedAt
		}
		return intervalDue(now, finished, s.EveryMin)
	}
}

func convertAll(in []bgm.Subject) []store.Subject {
	out := make([]store.Subject, 0, len(in))
	for _, s := range in {
		out = append(out, convert(s))
	}
	return out
}

func convert(s bgm.Subject) store.Subject {
	var votesTotal int
	tags := make([]store.TagVote, 0, len(s.Tags))
	for _, t := range s.Tags {
		if t.Name == "" || t.Count <= 0 {
			continue
		}
		votesTotal += t.Count
		tags = append(tags, store.TagVote{Name: t.Name, Votes: t.Count})
	}
	for i := range tags {
		if votesTotal > 0 {
			tags[i].Pct = math.Round(float64(tags[i].Votes)/float64(votesTotal)*10000) / 100
		}
	}

	image := s.Images.Large
	if image == "" {
		image = s.Image
	}
	if image == "" {
		image = s.Images.Common
	}

	// ratingDist 把 rating.count（键 "1".."10"）归一为十档人数；全 0 视为无分档数据
	var distSum int32
	ratingDist := make([]int32, 10)
	for i := 1; i <= 10; i++ {
		n := int32(s.Rating.Count[strconv.Itoa(i)])
		ratingDist[i-1] = n
		distSum += n
	}
	if distSum == 0 {
		ratingDist = []int32{}
	}

	return store.Subject{
		ID:          s.ID,
		Type:        s.Type,
		Name:        s.Name,
		NameCN:      s.NameCN,
		Date:        sources.NormalizeDate(s.Date),
		Platform:    s.Platform,
		Rank:        s.Rating.Rank,
		Score:       s.Rating.Score,
		RatingTotal: s.Rating.Total,
		AltTitles:   s.Aliases(),
		RatingDist:  ratingDist,
		Wish:        s.Collection.Wish,
		Collect:     s.Collection.Collect,
		Doing:       s.Collection.Doing,
		OnHold:      s.Collection.OnHold,
		Dropped:     s.Collection.Dropped,
		NSFW:        s.NSFW,
		MetaTags:    s.MetaTags,
		Image:       image,
		Summary:     s.Summary,
		TagsTotal:   votesTotal,
		Tags:        tags,
	}
}
