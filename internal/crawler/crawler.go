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
	"strings"
	"sync"
	"time"

	"anipick/internal/bgm"
	"anipick/internal/store"
)

var ErrBusy = errors.New("已有抓取任务在运行")

type Progress struct {
	Phase     string    `json:"phase"`
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
	httpClient *http.Client

	// OnRunFinished 在每轮抓取收尾时回调（无论 done/failed/canceled），
	// 服务层用它失效查询结果缓存——条目/标签数据只在抓取中变化。
	OnRunFinished func()

	mu     sync.Mutex
	cancel context.CancelFunc
	state  State
}

func New(st *store.Store, defaults store.AppSettings, httpClient *http.Client) *Crawler {
	return &Crawler{st: st, defaults: defaults, httpClient: httpClient}
}

func (c *Crawler) Status() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Start 创建抓取任务并异步执行。mode: full | incremental | refresh
func (c *Crawler) Start(mode, trigger string) (int64, error) {
	if mode != "full" && mode != "incremental" && mode != "refresh" {
		return 0, fmt.Errorf("未知抓取模式 %q", mode)
	}
	settings, err := c.st.LoadSettings(context.Background(), c.defaults)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	if c.state.Running {
		c.mu.Unlock()
		return 0, ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	runID, err := c.st.CreateRun(ctx, mode, trigger, settings.Crawl.Types)
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
		Progress:  Progress{Phase: "start", Message: "准备中", UpdatedAt: time.Now()},
	}
	c.mu.Unlock()

	go c.run(ctx, runID, mode, settings)
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

func (c *Crawler) run(ctx context.Context, runID int64, mode string, settings store.AppSettings) {
	client := c.client(settings)
	prog := Progress{Phase: "start", UpdatedAt: time.Now()}
	status, errMsg := "done", ""

	defer func() {
		if ctx.Err() != nil && status == "done" {
			status = "canceled"
		}
		finished := prog
		finished.Phase = "done"
		finished.UpdatedAt = time.Now()
		_ = c.st.UpdateRunStats(context.Background(), runID, finished)
		_ = c.st.FinishRun(context.Background(), runID, status, errMsg)
		c.mu.Lock()
		c.state.Running = false
		c.state.Progress = finished
		c.cancel = nil
		c.mu.Unlock()
		// 失败/取消同样可能已写入部分条目（增量逐批提交），一律失效查询缓存
		if c.OnRunFinished != nil {
			c.OnRunFinished()
		}
	}()

	update := func(phase, msg string) {
		prog.Phase = phase
		prog.Message = msg
		prog.UpdatedAt = time.Now()
		_ = c.st.UpdateRunStats(context.Background(), runID, prog)
		c.mu.Lock()
		c.state.Progress = prog
		c.mu.Unlock()
	}

	err := func() error {
		switch mode {
		case "full":
			return c.runFull(ctx, client, runID, settings, &prog, update)
		case "incremental":
			return c.runIncremental(ctx, client, runID, settings, &prog, update)
		default:
			if err := c.runRefresh(ctx, client, runID, settings, &prog, update); err != nil {
				return err
			}
			if prog.Refreshed > 0 {
				update("tags", "重算标签热度")
				if err := c.st.RecomputeTagUsage(ctx); err != nil {
					return fmt.Errorf("重算标签热度失败: %w", err)
				}
			}
			update("done", "过期重爬完成")
			return nil
		}
	}()
	if err != nil {
		if ctx.Err() != nil {
			status = "canceled"
			errMsg = "任务已手动停止"
			prog.Message = errMsg
		} else {
			status = "failed"
			errMsg = err.Error()
			prog.Message = "失败: " + errMsg
			slog.Error("抓取失败", "run", runID, "mode", mode, "err", err)
		}
	}
}

func (c *Crawler) client(settings store.AppSettings) *bgm.Client {
	timeout := time.Duration(settings.Crawl.RequestTimeoutSec) * time.Second
	return bgm.NewWithHTTPClient(c.httpClient, settings.BGM.Token, settings.BGM.Cookie, settings.BGM.UA, timeout)
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
	wait := time.Duration(settings.Crawl.DelayMS) * time.Millisecond

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
				if pageFails < 3 {
					pageFails++
					prog.Message = fmt.Sprintf("类型 %d offset %d 拉取失败，重试 %d/3: %v", typ, offset, pageFails, err)
					update("enumerate", prog.Message)
					if !delay(ctx, 5*time.Second) {
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

	update("tags", "重算标签热度")
	if err := c.st.RecomputeTagUsage(ctx); err != nil {
		return fmt.Errorf("重算标签热度失败: %w", err)
	}
	update("done", "全量抓取完成")
	return nil
}

func (c *Crawler) runIncremental(ctx context.Context, client *bgm.Client, runID int64, settings store.AppSettings, prog *Progress, update func(string, string)) error {
	types := settings.Crawl.Types
	pageSize := settings.Crawl.PageSize
	wait := time.Duration(settings.Crawl.DelayMS) * time.Millisecond
	pages := settings.Crawl.IncrementalPages

	for _, typ := range types {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prog.Type = typ
		for page := 0; page < pages; page++ {
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

	update("tags", "重算标签热度")
	if err := c.st.RecomputeTagUsage(ctx); err != nil {
		return fmt.Errorf("重算标签热度失败: %w", err)
	}
	update("done", "增量抓取完成")
	return nil
}

func (c *Crawler) runRefresh(ctx context.Context, client *bgm.Client, runID int64, settings store.AppSettings, prog *Progress, update func(string, string)) error {
	prog.Phase = "refresh"
	update("refresh", "查找过期条目")
	ids, err := c.st.StaleSubjects(ctx, settings.Crawl.RefreshTTLDays, settings.Crawl.RefreshBatch, settings.Crawl.Types)
	if err != nil {
		return fmt.Errorf("查询过期条目失败: %w", err)
	}
	prog.Total = len(ids)
	wait := time.Duration(settings.Crawl.DelayMS) * time.Millisecond
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
		if i%20 == 0 {
			update("refresh", fmt.Sprintf("过期重爬：%d/%d", i+1, len(ids)))
		}
		if !delay(ctx, wait) {
			return ctx.Err()
		}
	}
	update("refresh", fmt.Sprintf("过期重爬完成：%d 条", prog.Refreshed))
	return nil
}

// RefreshOne 立即重爬单个条目（管理端使用）。
func (c *Crawler) RefreshOne(ctx context.Context, id int64) (store.Subject, error) {
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

// Scheduler 周期性触发自动增量/过期重爬；ctx 取消时退出。
func (c *Crawler) Scheduler(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
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
	runs, err := c.st.RecentRuns(ctx, 20)
	if err != nil {
		return
	}
	var lastIncremental, lastAny *store.Run
	for i := range runs {
		r := &runs[i]
		if r.Status == "running" {
			return
		}
		if lastAny == nil {
			lastAny = r
		}
		if lastIncremental == nil && r.Mode == "incremental" && r.Status == "done" {
			lastIncremental = r
		}
	}

	if settings.Crawl.AutoIncremental {
		interval := time.Duration(settings.Crawl.IncrementalIntervalMin) * time.Minute
		if lastIncremental == nil || (lastIncremental.FinishedAt != nil && time.Since(*lastIncremental.FinishedAt) >= interval) {
			if _, err := c.Start("incremental", "auto"); err != nil && !errors.Is(err, ErrBusy) {
				slog.Warn("自动增量抓取启动失败", "err", err)
			}
			return
		}
	}
	if settings.Crawl.AutoRefresh {
		if lastAny == nil || lastAny.FinishedAt == nil || time.Since(*lastAny.FinishedAt) >= 10*time.Minute {
			if _, err := c.Start("refresh", "auto"); err != nil && !errors.Is(err, ErrBusy) {
				slog.Warn("自动过期重爬启动失败", "err", err)
			}
		}
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

	return store.Subject{
		ID:          s.ID,
		Type:        s.Type,
		Name:        s.Name,
		NameCN:      s.NameCN,
		Date:        normalizeDate(s.Date),
		Platform:    s.Platform,
		Rank:        s.Rating.Rank,
		Score:       s.Rating.Score,
		RatingTotal: s.Rating.Total,
		Wish:        s.Collection.Wish,
		Collect:     s.Collection.Collect,
		Doing:       s.Collection.Doing,
		OnHold:      s.Collection.OnHold,
		Dropped:     s.Collection.Dropped,
		NSFW:        s.NSFW,
		MetaTags:    s.MetaTags,
		Image:       image,
		TagsTotal:   votesTotal,
		Tags:        tags,
	}
}

func normalizeDate(s string) string {
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
	// 上游偶有非法日期；非法值直接丢弃，避免整页事务失败。
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}
