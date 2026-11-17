package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID         int64           `json:"id"`
	Mode       string          `json:"mode"`
	Trigger    string          `json:"trigger"`
	Status     string          `json:"status"`
	Types      []int           `json:"types"`
	Stats      json.RawMessage `json:"stats"`
	Error      string          `json:"error"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}

func (s *Store) CreateRun(ctx context.Context, mode, trigger string, types []int) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO crawl_runs (mode, trigger, status, types, stats)
		VALUES ($1, $2, 'running', $3, '{}'::jsonb) RETURNING id`, mode, trigger, types).Scan(&id)
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

const runColumns = `id, mode, trigger, status, types, stats, error, started_at, finished_at`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Mode, &r.Trigger, &r.Status, &r.Types, &r.Stats, &r.Error, &r.StartedAt, &r.FinishedAt)
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
