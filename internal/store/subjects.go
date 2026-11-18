package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const subjectColumns = `s.id, s.type, s.name, s.name_cn, COALESCE(to_char(s.date, 'YYYY-MM-DD'), ''),
	s.platform, s.rank, s.score::float8, s.rating_total, s.wish, s.collect, s.doing, s.on_hold, s.dropped,
	s.nsfw, s.meta_tags, s.image, s.tags_total, s.locked, s.fetched_at`

// 搜索总数缓存：翻页复用同一 count，上限防止关键词型 key 无界增长。
// TTL 只是兜底，抓取结束/删除条目时由 InvalidateCounts 主动失效。
const (
	searchCountTTL     = 60 * time.Second
	searchCountMaxKeys = 256
)

type countEntry struct {
	total int
	at    time.Time
}

func scanSubject(row pgx.Row) (Subject, error) {
	var sub Subject
	err := row.Scan(&sub.ID, &sub.Type, &sub.Name, &sub.NameCN, &sub.Date, &sub.Platform, &sub.Rank,
		&sub.Score, &sub.RatingTotal, &sub.Wish, &sub.Collect, &sub.Doing, &sub.OnHold, &sub.Dropped,
		&sub.NSFW, &sub.MetaTags, &sub.Image, &sub.TagsTotal, &sub.Locked, &sub.FetchedAt)
	return sub, err
}

// UpsertSubjects 在一个事务内写入一页条目（含标签与占比）。
// runID 会写入 last_run_id，供全量抓取结束后清理本轮未覆盖的条目。
func (s *Store) UpsertSubjects(ctx context.Context, recs []Subject, runID int64) (inserted, updated int, err error) {
	if len(recs) == 0 {
		return 0, 0, nil
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	nameSet := map[string]struct{}{}
	for _, rec := range recs {
		for _, t := range rec.Tags {
			if t.Name != "" {
				nameSet[t.Name] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	if len(names) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO tags (name) SELECT unnest($1::text[]) ON CONFLICT (name) DO NOTHING`, names); err != nil {
			return 0, 0, fmt.Errorf("写入标签: %w", err)
		}
	}
	tagIDs := map[string]int64{}
	if len(names) > 0 {
		rows, err := tx.Query(ctx, `SELECT id, name FROM tags WHERE name = ANY($1)`, names)
		if err != nil {
			return 0, 0, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return 0, 0, err
			}
			tagIDs[name] = id
		}
		rows.Close()
		if rows.Err() != nil {
			return 0, 0, rows.Err()
		}
	}

	for _, rec := range recs {
		if rec.MetaTags == nil {
			rec.MetaTags = []string{}
		}
		var isInsert bool
		err := tx.QueryRow(ctx, `
			INSERT INTO subjects (id, type, name, name_cn, date, platform, rank, score, rating_total,
				wish, collect, doing, on_hold, dropped, nsfw, meta_tags, image, tags_total, locked,
				fail_count, last_run_id, fetched_at, updated_at)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::date, $6, $7, $8, $9, $10, $11, $12, $13, $14,
				$15, $16::text[], $17, $18, $19, 0, $20, now(), now())
			ON CONFLICT (id) DO UPDATE SET
				type = EXCLUDED.type, name = EXCLUDED.name, name_cn = EXCLUDED.name_cn,
				date = EXCLUDED.date, platform = EXCLUDED.platform, rank = EXCLUDED.rank,
				score = EXCLUDED.score, rating_total = EXCLUDED.rating_total,
				wish = EXCLUDED.wish, collect = EXCLUDED.collect, doing = EXCLUDED.doing,
				on_hold = EXCLUDED.on_hold, dropped = EXCLUDED.dropped, nsfw = EXCLUDED.nsfw,
				meta_tags = EXCLUDED.meta_tags, image = EXCLUDED.image, tags_total = EXCLUDED.tags_total,
				locked = EXCLUDED.locked, fail_count = 0,
				last_run_id = CASE WHEN EXCLUDED.last_run_id > 0 THEN EXCLUDED.last_run_id ELSE subjects.last_run_id END,
				fetched_at = now(), updated_at = now()
			RETURNING (xmax = 0)`,
			rec.ID, rec.Type, rec.Name, rec.NameCN, rec.Date, rec.Platform, rec.Rank, rec.Score,
			rec.RatingTotal, rec.Wish, rec.Collect, rec.Doing, rec.OnHold, rec.Dropped, rec.NSFW,
			rec.MetaTags, rec.Image, rec.TagsTotal, rec.Locked, runID).Scan(&isInsert)
		if err != nil {
			return inserted, updated, fmt.Errorf("写入条目 %d: %w", rec.ID, err)
		}
		if isInsert {
			inserted++
		} else {
			updated++
		}

		if _, err := tx.Exec(ctx, `DELETE FROM subject_tags WHERE subject_id = $1`, rec.ID); err != nil {
			return inserted, updated, err
		}
		if len(rec.Tags) > 0 {
			ids := make([]int64, 0, len(rec.Tags))
			votes := make([]int32, 0, len(rec.Tags))
			pcts := make([]float64, 0, len(rec.Tags))
			for _, t := range rec.Tags {
				id, ok := tagIDs[t.Name]
				if !ok {
					continue
				}
				ids = append(ids, id)
				votes = append(votes, int32(t.Votes))
				pcts = append(pcts, math.Round(t.Pct*100)/100)
			}
			if len(ids) > 0 {
				if _, err := tx.Exec(ctx, `
					INSERT INTO subject_tags (subject_id, tag_id, votes, pct)
					SELECT $1, u.id, u.votes, u.pct FROM unnest($2::bigint[], $3::int[], $4::numeric[]) AS u(id, votes, pct)
					ON CONFLICT DO NOTHING`, rec.ID, ids, votes, pcts); err != nil {
					return inserted, updated, fmt.Errorf("写入标签占比 %d: %w", rec.ID, err)
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return inserted, updated, err
	}
	return inserted, updated, nil
}

// GetSubject 返回索引中的条目及全部已存标签（按票数降序）。
func (s *Store) GetSubject(ctx context.Context, id int64) (Subject, error) {
	row := s.Pool.QueryRow(ctx, `SELECT `+subjectColumns+` FROM subjects s WHERE s.id = $1`, id)
	sub, err := scanSubject(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return sub, ErrNotFound
		}
		return sub, err
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT t.name, st.votes, st.pct::float8
		FROM subject_tags st JOIN tags t ON t.id = st.tag_id
		WHERE st.subject_id = $1 ORDER BY st.votes DESC, t.name LIMIT 80`, id)
	if err != nil {
		return sub, err
	}
	defer rows.Close()
	for rows.Next() {
		var tv TagVote
		if err := rows.Scan(&tv.Name, &tv.Votes, &tv.Pct); err != nil {
			return sub, err
		}
		sub.Tags = append(sub.Tags, tv)
	}
	return sub, rows.Err()
}

// SearchQuery 是本站索引检索条件。
type SearchQuery struct {
	Keyword     string
	Types       []int
	Tags        []string
	ExcludeTags []string
	TagMode     string
	PctMin      float64 // 百分比 0-100，0 表示不限制
	MetaTags    []string
	DateFrom    string
	DateTo      string
	RankMin     int
	RankMax     int
	ScoreMin    float64
	ScoreMax    float64
	RatingMin   int
	NSFW        string // hide | include | only
	Sort        string
	Order       string
	Page        int
	PageSize    int
}

type SearchResult struct {
	Items    []Subject `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

func (s *Store) tagIDsByNames(ctx context.Context, names []string) ([]int64, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id FROM tags WHERE name = ANY($1)`, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Search 在本站索引上做筛选、排序与分页。
func (s *Store) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	var out SearchResult
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 100 {
		q.PageSize = 20
	}

	var args []any
	ph := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	conds := []string{"TRUE"}

	types := q.Types
	if len(types) == 1 {
		// 单类型用直接等值：type = ANY 形式会让规划器放弃索引序（实测退回
		// Seq Scan + 全量排序，排序索引全部失效），等值则完美命中排序索引
		conds = append(conds, "s.type = "+ph(types[0]))
	} else if len(types) > 0 {
		conds = append(conds, "s.type = ANY("+ph(types)+")")
	}

	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		p := ph("%" + escapeLike(kw) + "%")
		conds = append(conds, `(s.name ILIKE `+p+` ESCAPE '\' OR s.name_cn ILIKE `+p+` ESCAPE '\')`)
	}

	tags := dedupe(q.Tags)
	if len(tags) > 0 {
		// 以 subject_tags 为驱动表：先按 tag_id 取出命中条目，再与 subjects 关联，
		// 避免对每个条目做一次相关子查询。
		ids, err := s.tagIDsByNames(ctx, tags)
		if err != nil {
			return out, err
		}
		if len(ids) < len(tags) {
			out.Page = q.Page
			out.PageSize = q.PageSize
			return out, nil
		}
		pctCond := ""
		if q.PctMin > 0 {
			pctCond = " AND st.pct >= " + ph(q.PctMin)
		}
		idArg := ph(ids)
		if q.TagMode == "or" {
			conds = append(conds, `s.id IN (SELECT st.subject_id FROM subject_tags st
				WHERE st.tag_id = ANY(`+idArg+`)`+pctCond+`)`)
		} else {
			conds = append(conds, `s.id IN (SELECT st.subject_id FROM subject_tags st
				WHERE st.tag_id = ANY(`+idArg+`)`+pctCond+`
				GROUP BY st.subject_id HAVING count(DISTINCT st.tag_id) = `+ph(len(ids))+`)`)
		}
	}

	if ex := dedupe(q.ExcludeTags); len(ex) > 0 {
		ids, err := s.tagIDsByNames(ctx, ex)
		if err != nil {
			return out, err
		}
		if len(ids) > 0 {
			conds = append(conds, `s.id NOT IN (SELECT st.subject_id FROM subject_tags st
				WHERE st.tag_id = ANY(`+ph(ids)+`))`)
		}
	}

	if mt := dedupe(q.MetaTags); len(mt) > 0 {
		conds = append(conds, "s.meta_tags @> "+ph(mt)+"::text[]")
	}

	if q.DateFrom != "" {
		conds = append(conds, "s.date >= "+ph(q.DateFrom)+"::date")
	}
	if q.DateTo != "" {
		conds = append(conds, "s.date <= "+ph(q.DateTo)+"::date")
	}
	if q.RankMin > 0 {
		conds = append(conds, "s.rank >= "+ph(q.RankMin))
	}
	if q.RankMax > 0 {
		conds = append(conds, "s.rank <= "+ph(q.RankMax))
	}
	if q.ScoreMin > 0 {
		conds = append(conds, "s.score >= "+ph(q.ScoreMin))
	}
	if q.ScoreMax > 0 {
		conds = append(conds, "s.score <= "+ph(q.ScoreMax))
	}
	if q.RatingMin > 0 {
		conds = append(conds, "s.rating_total >= "+ph(q.RatingMin))
	}

	switch q.NSFW {
	case "only":
		conds = append(conds, "s.nsfw = TRUE")
	case "include":
	default:
		conds = append(conds, "s.nsfw = FALSE")
	}

	where := strings.Join(conds, " AND ")

	// 分页翻页时总数不变，count 结果按 where+参数缓存 30s，省掉重复的全量计数。
	// 抓取写入导致的 30s 内总数偏差对分页 UI 无感。
	countArgs := args // 快照：limit/offset 尚未追加
	countKey := where + "|" + fmt.Sprint(args...)
	s.countMu.Lock()
	cached, ok := s.countCache[countKey]
	s.countMu.Unlock()
	countCached := ok && time.Since(cached.at) < searchCountTTL

	dir := "ASC"
	if strings.EqualFold(q.Order, "desc") {
		dir = "DESC"
	}
	var orderBy string
	switch q.Sort {
	case "rank":
		orderBy = "(s.rank = 0) ASC, s.rank " + dir
	case "score":
		orderBy = "s.score " + dir + ", s.rating_total DESC"
	case "rating_total":
		orderBy = "s.rating_total " + dir
	case "date":
		orderBy = "s.date " + dir + " NULLS LAST"
	case "collect":
		orderBy = "s.collect " + dir
	case "wish":
		orderBy = "s.wish " + dir
	case "doing":
		orderBy = "s.doing " + dir
	default:
		orderBy = "s.id DESC"
	}
	orderBy += ", s.id ASC"

	limitPh := ph(q.PageSize)
	offsetPh := ph((q.Page - 1) * q.PageSize)

	// 两段式组装：LIMIT/OFFSET 作用于纯索引的 id 窗口子查询，标签组装只对
	// 窗口内的 PageSize 行做 LATERAL——否则深分页时 OFFSET 之前的每一行
	// 都会做一次组装（实测第 5000 页 3.6s 的根因）。
	pageSQL := `
		SELECT ` + subjectColumns + `, COALESCE(tg.tags, '[]'::jsonb)
		FROM (
			SELECT s.id FROM subjects s WHERE ` + where + `
			ORDER BY ` + orderBy + `
			LIMIT ` + limitPh + ` OFFSET ` + offsetPh + `
		) win
		JOIN subjects s ON s.id = win.id
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(jsonb_build_object('name', t.name, 'votes', x.votes, 'pct', x.pct)
				ORDER BY x.votes DESC, t.name) AS tags
			FROM (
				SELECT st.tag_id, st.votes, st.pct::float8 AS pct
				FROM subject_tags st WHERE st.subject_id = s.id
				ORDER BY st.votes DESC LIMIT 10
			) x
			JOIN tags t ON t.id = x.tag_id
		) tg ON TRUE
		ORDER BY ` + orderBy

	var rows pgx.Rows
	var qErr error
	if countCached {
		out.Total = cached.total
		rows, qErr = s.Pool.Query(ctx, pageSQL, args...)
		if qErr != nil {
			return out, qErr
		}
	} else {
		// count 未命中缓存：计数与取页并行执行，省一轮 IO 等待（不增总 CPU）
		var cErr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			cErr = s.Pool.QueryRow(ctx, `SELECT count(*) FROM subjects s WHERE `+where, countArgs...).Scan(&out.Total)
		}()
		rows, qErr = s.Pool.Query(ctx, pageSQL, args...)
		wg.Wait()
		if qErr != nil {
			return out, qErr
		}
		if cErr != nil {
			return out, cErr
		}
		s.countMu.Lock()
		if len(s.countCache) >= searchCountMaxKeys {
			s.countCache = map[string]countEntry{} // 用户输入型 key 无界增长，超限直接换新
		}
		s.countCache[countKey] = countEntry{total: out.Total, at: time.Now()}
		s.countMu.Unlock()
	}
	defer rows.Close()

	out.Items = make([]Subject, 0, q.PageSize)
	for rows.Next() {
		var sub Subject
		var tagsJSON []byte
		if err := rows.Scan(&sub.ID, &sub.Type, &sub.Name, &sub.NameCN, &sub.Date, &sub.Platform, &sub.Rank,
			&sub.Score, &sub.RatingTotal, &sub.Wish, &sub.Collect, &sub.Doing, &sub.OnHold, &sub.Dropped,
			&sub.NSFW, &sub.MetaTags, &sub.Image, &sub.TagsTotal, &sub.Locked, &sub.FetchedAt, &tagsJSON); err != nil {
			return out, err
		}
		if len(tagsJSON) > 0 {
			if err := json.Unmarshal(tagsJSON, &sub.Tags); err != nil {
				return out, err
			}
		}
		out.Items = append(out.Items, sub)
	}
	out.Page = q.Page
	out.PageSize = q.PageSize
	return out, rows.Err()
}

// TagSuggest 返回标签候选；types 为空表示全类型汇总。
func (s *Store) TagSuggest(ctx context.Context, query string, types []int, limit int) ([]TagStat, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	like := "%" + escapeLike(strings.TrimSpace(query)) + "%"

	var rows pgx.Rows
	var err error
	if len(types) == 0 {
		// 全类型汇总直查 tags 上的预聚合列（抓取后随 tag_usage 一并重算），
		// 避免对 tag_usage 做全量 ILIKE 聚合
		rows, err = s.Pool.Query(ctx, `
			SELECT name, subject_count, votes
			FROM tags
			WHERE name ILIKE $1 ESCAPE '\'
			ORDER BY subject_count DESC, name LIMIT $2`, like, limit)
	} else {
		// 带类型：先由 tags 的 trgm GIN 按名称取命中标签（按全类型热度排序，
		// 上限 500 保证热门标签不丢），再按类型前缀聚合 tag_usage——
		// 工作量随"命中的标签数"增长而非随 tag_usage 行数增长，规模免疫
		rows, err = s.Pool.Query(ctx, `
			WITH hit AS (
				SELECT id, name FROM tags
				WHERE name ILIKE $1 ESCAPE '\'
				ORDER BY subject_count DESC, name LIMIT 500
			)
			SELECT h.name, sum(tu.subject_count)::int, sum(tu.votes)::bigint
			FROM hit h JOIN tag_usage tu ON tu.tag_id = h.id AND tu.type = ANY($2)
			GROUP BY h.name ORDER BY 2 DESC, h.name LIMIT $3`, like, types, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TagStat, 0, limit)
	for rows.Next() {
		var st TagStat
		if err := rows.Scan(&st.Name, &st.Count, &st.Votes); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// MetaTagSuggest 返回元标签（TV/剧场版/日本…）候选；查 meta_tag_usage 预聚合表
// （抓取后重算），不再扫 subjects unnest。
func (s *Store) MetaTagSuggest(ctx context.Context, typ int, limit int) ([]TagStat, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	var rows pgx.Rows
	var err error
	if typ == 0 {
		rows, err = s.Pool.Query(ctx, `
			SELECT name, sum(count)::int, 0
			FROM meta_tag_usage
			GROUP BY name ORDER BY 2 DESC, name LIMIT $1`, limit)
	} else {
		rows, err = s.Pool.Query(ctx, `
			SELECT name, count, 0
			FROM meta_tag_usage
			WHERE type = $1
			ORDER BY count DESC, name LIMIT $2`, typ, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TagStat, 0, limit)
	for rows.Next() {
		var st TagStat
		if err := rows.Scan(&st.Name, &st.Count, &st.Votes); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

type Stats struct {
	Total       int            `json:"total"`
	ByType      map[string]int `json:"by_type"`
	Tags        int            `json:"tags"`
	SubjectTags int            `json:"subject_tags"`
	LastFetched *time.Time     `json:"last_fetched"`
	NSFW        int            `json:"nsfw"`
	DBSize      int64          `json:"db_size"`
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	out := Stats{ByType: map[string]int{}}

	// 六条独立统计查询并行执行：省 IO 等待（总 CPU 不变，都是同一批小聚合）
	var wg sync.WaitGroup
	errs := make([]error, 5)
	wg.Add(5)
	go func() {
		defer wg.Done()
		errs[0] = s.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE nsfw) FROM subjects`).Scan(&out.Total, &out.NSFW)
	}()
	go func() {
		defer wg.Done()
		rows, err := s.Pool.Query(ctx, `SELECT type, count(*) FROM subjects GROUP BY type`)
		if err != nil {
			errs[1] = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var t, n int
			if err := rows.Scan(&t, &n); err != nil {
				errs[1] = err
				return
			}
			out.ByType[strconv.Itoa(t)] = n
		}
		errs[1] = rows.Err()
	}()
	go func() {
		defer wg.Done()
		// 展示用计数改取统计信息估算（reltuples 随抓取后的 ANALYZE 刷新）：
		// 精确 count 对 subject_tags 要扫 45 万行（实测 41-96ms），每轮缓存过期都来一次不划算
		errs[2] = s.Pool.QueryRow(ctx, `
			SELECT GREATEST(reltuples::bigint, 0) FROM pg_class WHERE relname = 'tags'`).Scan(&out.Tags)
	}()
	go func() {
		defer wg.Done()
		errs[3] = s.Pool.QueryRow(ctx, `
			SELECT GREATEST(reltuples::bigint, 0) FROM pg_class WHERE relname = 'subject_tags'`).Scan(&out.SubjectTags)
	}()
	go func() {
		defer wg.Done()
		var last *time.Time
		errs[4] = s.Pool.QueryRow(ctx, `SELECT max(fetched_at) FROM subjects`).Scan(&last)
		out.LastFetched = last
	}()
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}
	// 库体积是诊断信息，失败不影响统计可用性
	_ = s.Pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&out.DBSize)
	return out, nil
}

// CountStale 统计超过 TTL 未刷新的条目数量。
func (s *Store) CountStale(ctx context.Context, ttlDays int, types []int) (int, error) {
	if ttlDays < 1 {
		ttlDays = 1
	}
	var n int
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FROM subjects
		WHERE fetched_at < now() - ($1::int * interval '1 day')
			AND NOT locked AND fail_count < 5
			AND ($2::int[] IS NULL OR cardinality($2::int[]) = 0 OR type = ANY($2))`, ttlDays, types).Scan(&n)
	return n, err
}

// StaleSubjects 返回超过 TTL 未刷新的条目 id，优先刷新有排名的。
func (s *Store) StaleSubjects(ctx context.Context, ttlDays, limit int, types []int) ([]int64, error) {
	if ttlDays < 1 {
		ttlDays = 1
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id FROM subjects
		WHERE fetched_at < now() - ($1::int * interval '1 day')
			AND NOT locked AND fail_count < 5
			AND ($2::int[] IS NULL OR cardinality($2::int[]) = 0 OR type = ANY($2))
		ORDER BY (rank = 0) ASC, rank ASC, fetched_at ASC
		LIMIT $3`, ttlDays, types, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkUnavailable 记录上游不可访问（已删除 / 无权限），保留索引并退避。
func (s *Store) MarkUnavailable(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE subjects SET locked = TRUE, fail_count = fail_count + 1, updated_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE subjects SET fail_count = fail_count + 1, updated_at = now() WHERE id = $1`, id)
	return err
}

// Prune 删除本轮全量抓取未覆盖的条目（仅限指定类型）。
func (s *Store) Prune(ctx context.Context, runID int64, types []int) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		DELETE FROM subjects
		WHERE type = ANY($1) AND (last_run_id IS NULL OR last_run_id <> $2)`, types, runID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RecomputeTagUsage 重算标签热度（抓取结束后调用）。
func (s *Store) RecomputeTagUsage(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM tag_usage`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tag_usage (type, tag_id, subject_count, votes)
		SELECT s.type, st.tag_id, count(*)::int, sum(st.votes)::bigint
		FROM subject_tags st JOIN subjects s ON s.id = st.subject_id
		GROUP BY s.type, st.tag_id`); err != nil {
		return err
	}
	// tags 全类型汇总列（TagSuggest 无类型过滤直查本表）
	if _, err := tx.Exec(ctx, `
		UPDATE tags t SET subject_count = u.c, votes = u.v
		FROM (SELECT tag_id, sum(subject_count)::int AS c, sum(votes)::bigint AS v
		      FROM tag_usage GROUP BY tag_id) u
		WHERE t.id = u.tag_id`); err != nil {
		return err
	}
	// 元标签预聚合表（MetaTagSuggest 直查本表）
	if _, err := tx.Exec(ctx, `DELETE FROM meta_tag_usage`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO meta_tag_usage (type, name, count)
		SELECT s.type, m, count(*)::int FROM subjects s, unnest(s.meta_tags) AS m GROUP BY s.type, m`); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// 大批量写入后回收死元组并刷新规划器统计：抓取对 subject_tags 是整表级
	// DELETE+INSERT，不 VACUUM 的话 45 万行（生产 200 万+）会持续膨胀拖慢索引扫描
	for _, t := range []string{"subjects", "subject_tags", "tags"} {
		if _, err := s.Pool.Exec(ctx, `VACUUM ANALYZE `+t); err != nil {
			return err
		}
	}
	if _, err := s.Pool.Exec(ctx, `VACUUM ANALYZE meta_tag_usage`); err != nil {
		return err
	}
	return nil
}

// DeleteSubject 从索引中移除单个条目。
func (s *Store) DeleteSubject(ctx context.Context, id int64) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM subjects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
