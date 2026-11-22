package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID         int64           `json:"id"`
	Mode       string          `json:"mode"`
	Source     string          `json:"source"`
	Trigger    string          `json:"trigger"`
	Status     string          `json:"status"`
	Types      []int           `json:"types"`
	Stats      json.RawMessage `json:"stats"`
	Error      string          `json:"error"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}

// CreateRun 新建运行记录；source 标注该行属于哪个数据源
// （合并运行里 bgm 与各扩展源各占一行，管理面板按来源展示最近状态）。
func (s *Store) CreateRun(ctx context.Context, mode, trigger, source string, types []int) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO crawl_runs (mode, trigger, source, status, types, stats)
		VALUES ($1, $2, $3, 'running', $4, '{}'::jsonb) RETURNING id`, mode, trigger, source, types).Scan(&id)
	return id, err
}

func (s *Store) UpdateRunStats(ctx context.Context, id int64, stats any) error {
	raw, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE crawl_runs SET stats = $2 WHERE id = $1`, id, raw)
	return err
}

func (s *Store) FinishRun(ctx context.Context, id int64, status, errMsg string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE crawl_runs SET status = $2, error = $3, finished_at = now() WHERE id = $1`, id, status, errMsg)
	return err
}

const runColumns = `id, mode, source, trigger, status, types, stats, error, started_at, finished_at`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Mode, &r.Source, &r.Trigger, &r.Status, &r.Types, &r.Stats, &r.Error, &r.StartedAt, &r.FinishedAt)
	return r, err
}

func (s *Store) RecentRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+runColumns+` FROM crawl_runs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastRun 返回最近一次指定模式与触发方式的运行（合并运行每个来源各占一行，
// 这里取的是整轮——同一轮多行 started_at 相同，取哪行语义一致）；没有则返回 nil。
// 调度器用它做定时触发的锚点：daily/every_days 判断“本轮时隙是否已经跑过”。
func (s *Store) LastRun(ctx context.Context, mode, trigger string) (*Run, error) {
	var r Run
	err := s.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM crawl_runs
		WHERE mode = $1 AND trigger = $2 ORDER BY started_at DESC, id DESC LIMIT 1`, mode, trigger).Scan(
		&r.ID, &r.Mode, &r.Source, &r.Trigger, &r.Status, &r.Types, &r.Stats, &r.Error, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// LastSourceRun 返回某来源最近一次完成的增量运行（合并增量的源行与手动
// 单源增量都算锚点；没有则返回 nil = 该源还没跑过增量，属空数据）。
// 真增量用它做时间窗：下次增量只发现「上次运行之后新增」的条目。
func (s *Store) LastSourceRun(ctx context.Context, source string) (*Run, error) {
	var r Run
	err := s.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM crawl_runs
		WHERE source = $1 AND mode IN ('incremental', 'harvest_incremental') AND status = 'done'
		ORDER BY started_at DESC LIMIT 1`, source).Scan(
		&r.ID, &r.Mode, &r.Source, &r.Trigger, &r.Status, &r.Types, &r.Stats, &r.Error, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// RunQuery 是任务历史的查询条件（管理端任务表的筛选/排序/分页）。
// Source/Mode/Status 空串 = 不过滤；Sort 白名单见 runsSortColumns；
// Limit/Offset 是页大小与页起点（返回值同时带总数）。
type RunQuery struct {
	Source string
	Mode   string
	Status string
	Sort   string
	Order  string
	Limit  int
	Offset int
}

// runsSortColumns 是任务表可排序字段的白名单（防注入：动态拼接只允许这里列出的列名）。
var runsSortColumns = map[string]string{
	"started_at": "started_at",
	"mode":       "mode",
	"source":     "source",
	"status":     "status",
}

// ListRuns 按条件查询任务历史并返回总数（分页用）。
func (s *Store) ListRuns(ctx context.Context, q RunQuery) ([]Run, int, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 20
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	col, ok := runsSortColumns[q.Sort]
	if !ok {
		col = "started_at"
	}
	order := "DESC"
	if q.Order == "asc" {
		order = "ASC"
	}
	where := `WHERE TRUE`
	args := []any{}
	if q.Source != "" {
		args = append(args, q.Source)
		where += fmt.Sprintf(` AND source = $%d`, len(args))
	}
	if q.Mode != "" {
		args = append(args, q.Mode)
		where += fmt.Sprintf(` AND mode = $%d`, len(args))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where += fmt.Sprintf(` AND status = $%d`, len(args))
	}

	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM crawl_runs `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := s.Pool.Query(ctx,
		`SELECT `+runColumns+` FROM crawl_runs `+where+
			fmt.Sprintf(` ORDER BY %s %s, id DESC LIMIT $%d OFFSET $%d`, col, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (s *Store) GetRun(ctx context.Context, id int64) (Run, error) {
	r, err := scanRun(s.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM crawl_runs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// MarkStaleRuns 把进程重启后遗留的 running 记录标记为中断。
func (s *Store) MarkStaleRuns(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE crawl_runs SET status = 'interrupted', error = '进程重启，任务中断', finished_at = now()
		WHERE status = 'running'`)
	return err
}

// LastAllSourceRun 返回最近一次合并运行（覆盖全部启用中来源）的开始时间。
// 合并运行里才会出现扩展源的 full/incremental/refresh 行（手动单源抓取用
// harvest* 模式、原生源单抓是 combined=false），因此扩展源行是合并运行的
// 锚；没有任何扩展源行时退回原生主流程最近一次 full/incremental 行
// （只启用了原生源时，「全源」就是它自己）。
func (s *Store) LastAllSourceRun(ctx context.Context, native string) (*time.Time, error) {
	var t *time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT max(started_at) FROM crawl_runs
		WHERE mode IN ('full', 'incremental', 'refresh') AND source <> $1`, native).Scan(&t)
	if err != nil {
		return nil, err
	}
	if t != nil {
		return t, nil
	}
	err = s.Pool.QueryRow(ctx, `
		SELECT max(started_at) FROM crawl_runs
		WHERE mode IN ('full', 'incremental') AND source = $1`, native).Scan(&t)
	return t, err
}
