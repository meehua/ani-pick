package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"anipick/internal/registry"
)

// subjectColumns 是条目查询的列清单（source_ref 为身份内部字段：入扫描、JSON 不外泄）。
// score/rating_total 是作品组合成值（主行上物化）；raw_* 是该行所属来源的原始值。
const subjectColumns = `s.id, s.source, s.source_ref, s.type, s.name, s.name_cn, COALESCE(to_char(s.date, 'YYYY-MM-DD'), ''),
	s.platform, s.rank, s.score::float8, s.rating_total, s.raw_score::float8, s.raw_rating_total, s.rating_dist, s.wish, s.collect, s.doing, s.on_hold, s.dropped,
	s.nsfw, s.meta_tags, s.image, s.summary, s.tags_total, s.locked, s.fetched_at`

// 搜索总数缓存：翻页复用同一 count，上限防止关键词型 key 无界增长。
// TTL 只是兜底，抓取结束/删除条目时由 InvalidateCounts 主动失效；
// 取值可经系统设置调整（server 注入 SetCountTTL），常量为内置默认。
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
	err := row.Scan(&sub.ID, &sub.Source, &sub.SourceRef, &sub.Type, &sub.Name, &sub.NameCN, &sub.Date, &sub.Platform, &sub.Rank,
		&sub.Score, &sub.RatingTotal, &sub.RawScore, &sub.RawRatingTotal, &sub.RatingDist, &sub.Wish, &sub.Collect, &sub.Doing, &sub.OnHold, &sub.Dropped,
		&sub.NSFW, &sub.MetaTags, &sub.Image, &sub.Summary, &sub.TagsTotal, &sub.Locked, &sub.FetchedAt)
	return sub, err
}

// resolveIDs 把一批记录解析成最终写入 id 与所属组（身份读写的唯一收口）：
//   - bgm（Source 为空视为 bgm）：id = 来源主键，source_ref 回填为 id 文本；
//   - 扩展源：先按 (source, source_ref) 查既有行，缺失的从 subject_ext_id_seq 取号。
//
// 返回 ids（写入 id）与 workIDs（既有行所属作品组；新行为 0，由调用方走匹配器）。
func resolveIDs(ctx context.Context, tx pgx.Tx, recs []Subject) ([]int64, []int64, error) {
	ids := make([]int64, len(recs))
	workIDs := make([]int64, len(recs))
	refsBySource := map[string][]string{}
	for i := range recs {
		if recs[i].Source == "" {
			recs[i].Source = SourceBGM
		}
		if recs[i].SourceRef == "" && recs[i].Source == SourceBGM {
			recs[i].SourceRef = strconv.FormatInt(recs[i].ID, 10)
		}
		if recs[i].SourceRef == "" {
			return nil, nil, fmt.Errorf("条目缺少 source_ref: source=%s name=%q", recs[i].Source, recs[i].Name)
		}
		if recs[i].Source == SourceBGM {
			id, err := strconv.ParseInt(recs[i].SourceRef, 10, 64)
			if err != nil || id <= 0 {
				return nil, nil, fmt.Errorf("bgm 条目 id 非法: %q", recs[i].SourceRef)
			}
			ids[i] = id
		} else {
			ids[i] = -1
			refsBySource[recs[i].Source] = append(refsBySource[recs[i].Source], recs[i].SourceRef)
		}
	}
	// 扩展源既有身份：一次 IN 查询取回（批量组装，禁循环逐条查）
	for source, refs := range refsBySource {
		rows, err := tx.Query(ctx, `SELECT source_ref, id, work_id FROM subjects WHERE source = $1 AND source_ref = ANY($2)`,
			source, refs)
		if err != nil {
			return nil, nil, err
		}
		type existing struct {
			id   int64
			work int64
		}
		found := map[string]existing{}
		for rows.Next() {
			var ref string
			var e existing
			if err := rows.Scan(&ref, &e.id, &e.work); err != nil {
				rows.Close()
				return nil, nil, err
			}
			found[ref] = e
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		for i := range recs {
			if ids[i] < 0 && recs[i].Source == source {
				if e, ok := found[recs[i].SourceRef]; ok {
					ids[i] = e.id
					workIDs[i] = e.work // 已存在的行保持原组
					if e.work == 0 {
						workIDs[i] = e.id // 迁移前的 0 值自愈：自成一组
					}
				}
			}
		}
	}
	// bgm 行的既有组：一次 IN 查询（新 bgm 行 workIDs 保持 0，走匹配器）
	var bgmIdx []int
	for i := range recs {
		if recs[i].Source == SourceBGM {
			bgmIdx = append(bgmIdx, i)
		}
	}
	if len(bgmIdx) > 0 {
		bgmIDs := make([]int64, 0, len(bgmIdx))
		for _, i := range bgmIdx {
			bgmIDs = append(bgmIDs, ids[i])
		}
		rows, err := tx.Query(ctx, `SELECT id, work_id FROM subjects WHERE id = ANY($1)`, bgmIDs)
		if err != nil {
			return nil, nil, err
		}
		work := map[int64]int64{}
		for rows.Next() {
			var id, w int64
			if err := rows.Scan(&id, &w); err != nil {
				rows.Close()
				return nil, nil, err
			}
			work[id] = w
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		for _, i := range bgmIdx {
			if w, ok := work[ids[i]]; ok {
				workIDs[i] = w
				if w == 0 {
					workIDs[i] = ids[i] // 迁移前的 0 值自愈：自成一组
				}
			}
		}
	}

	// 仍缺的：从扩展序列批量取号（1e10 起，与 bgm id 空间隔离）
	missing := 0
	for i := range recs {
		if ids[i] < 0 {
			missing++
		}
	}
	if missing > 0 {
		rows, err := tx.Query(ctx, `SELECT nextval('subject_ext_id_seq') FROM generate_series(1, $1)`, missing)
		if err != nil {
			return nil, nil, err
		}
		seq := make([]int64, 0, missing)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, nil, err
			}
			seq = append(seq, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		n := 0
		for i := range recs {
			if ids[i] < 0 {
				ids[i] = seq[n]
				n++
			}
		}
	}
	return ids, workIDs, nil
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

	ids, workIDs, err := resolveIDs(ctx, tx, recs)
	if err != nil {
		return 0, 0, err
	}

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

	// 每条入站记录的归属组：既有行保持原组；新行走匹配器（② ext_key → ③ 启发式），
	// 拿不准就自成作品。新行并入的组在写入后统一重排资料序；
	// 所有涉及到的组在写入后重算合成评分。
	joined := map[int64]struct{}{}  // 新行并入的组：需要重排资料序
	touched := map[int64]struct{}{} // 涉及到的组：需要重算合成评分
	for i, rec := range recs {
		if rec.MetaTags == nil {
			rec.MetaTags = []string{}
		}
		if rec.AltTitles == nil {
			rec.AltTitles = []string{}
		}
		if workIDs[i] == 0 {
			m := matchRecord{
				Source: rec.Source,
				Ref:    rec.SourceRef,
				Name:   rec.Name,
				ExtKey: rec.ExtKey,
				Type:   rec.Type,
				Date:   rec.Date,
				Titles: TitleSetOf(rec.Name, rec.NameCN, rec.AltTitles),
			}
			w, ambiguous, merr := matchWork(ctx, tx, m)
			if merr != nil {
				return inserted, updated, fmt.Errorf("作品匹配 %s/%s: %w", rec.Source, rec.SourceRef, merr)
			}
			// 拿不准就拒绝：自成作品（matchWork 内已记警告日志）
			_ = ambiguous
			if w > 0 {
				joined[w] = struct{}{}
			}
			workIDs[i] = w // 0 = 独立新作品，work_id = 自己的 id
			if workIDs[i] == 0 {
				workIDs[i] = ids[i]
			}
		}
		touched[workIDs[i]] = struct{}{}
		var isInsert bool
		// rating_dist 列 NOT NULL：nil 归一为空数组（空 = 该源无分档数据）
		dist := rec.RatingDist
		if dist == nil {
			dist = []int32{}
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO subjects (id, source, source_ref, work_id, ext_key, alt_titles, type, name, name_cn, date,
				platform, rank, score, rating_total, raw_score, raw_rating_total, rating_dist, wish, collect, doing, on_hold, dropped,
				nsfw, meta_tags, image, summary, tags_total, locked, fail_count, last_run_id, fetched_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6::text[], $7, $8, $9, NULLIF($10, '')::date, $11, $12, $13, $14, $15, $16, $17::integer[],
				$18, $19, $20, $21, $22, $23, $24::text[], $25, $26, $27, $28, 0, $29, now(), now())
			ON CONFLICT (id) DO UPDATE SET
				source = EXCLUDED.source, source_ref = EXCLUDED.source_ref,
				ext_key = EXCLUDED.ext_key, alt_titles = EXCLUDED.alt_titles,
				-- work_id 不随 upsert 改写：既有行保持已归的组（自愈分支见 CASE）
				work_id = CASE WHEN subjects.work_id = 0 THEN EXCLUDED.work_id ELSE subjects.work_id END,
				type = EXCLUDED.type, name = EXCLUDED.name, name_cn = EXCLUDED.name_cn,
				date = EXCLUDED.date, platform = EXCLUDED.platform, rank = EXCLUDED.rank,
				-- raw_* 永远保存该来源自己的原始值；score 先落回原始值，
				-- 属于作品组主行的部分由本次写入收尾的合成重算覆盖
				score = EXCLUDED.raw_score, rating_total = EXCLUDED.raw_rating_total,
				raw_score = EXCLUDED.raw_score, raw_rating_total = EXCLUDED.raw_rating_total, rating_dist = EXCLUDED.rating_dist,
				wish = EXCLUDED.wish, collect = EXCLUDED.collect, doing = EXCLUDED.doing,
				on_hold = EXCLUDED.on_hold, dropped = EXCLUDED.dropped, nsfw = EXCLUDED.nsfw,
				meta_tags = EXCLUDED.meta_tags, image = EXCLUDED.image, summary = EXCLUDED.summary, tags_total = EXCLUDED.tags_total,
				locked = EXCLUDED.locked, fail_count = 0,
				last_run_id = CASE WHEN EXCLUDED.last_run_id > 0 THEN EXCLUDED.last_run_id ELSE subjects.last_run_id END,
				fetched_at = now(), updated_at = now()
			RETURNING (xmax = 0)`,
			ids[i], rec.Source, rec.SourceRef, workIDs[i], rec.ExtKey, rec.AltTitles,
			rec.Type, rec.Name, rec.NameCN, rec.Date, rec.Platform,
			rec.Rank, rec.Score, rec.RatingTotal, rec.Score, rec.RatingTotal, dist, rec.Wish, rec.Collect, rec.Doing, rec.OnHold,
			rec.Dropped, rec.NSFW, rec.MetaTags, rec.Image, rec.Summary, rec.TagsTotal, rec.Locked, runID).Scan(&isInsert)
		if err != nil {
			return inserted, updated, fmt.Errorf("写入条目 %d: %w", ids[i], err)
		}
		if isInsert {
			inserted++
		} else {
			updated++
		}

		// 标签写入使用解析后的最终 id（扩展源 rec.ID 为 0，身份由 ids[i] 承载）
		if _, err := tx.Exec(ctx, `DELETE FROM subject_tags WHERE subject_id = $1`, ids[i]); err != nil {
			return inserted, updated, err
		}
		if len(rec.Tags) > 0 {
			tagIDs2 := make([]int64, 0, len(rec.Tags))
			votes := make([]int32, 0, len(rec.Tags))
			pcts := make([]float64, 0, len(rec.Tags))
			for _, t := range rec.Tags {
				id, ok := tagIDs[t.Name]
				if !ok {
					continue
				}
				tagIDs2 = append(tagIDs2, id)
				votes = append(votes, int32(t.Votes))
				pcts = append(pcts, math.Round(t.Pct*100)/100)
			}
			if len(tagIDs2) > 0 {
				if _, err := tx.Exec(ctx, `
					INSERT INTO subject_tags (subject_id, tag_id, votes, pct)
					SELECT $1, u.id, u.votes, u.pct FROM unnest($2::bigint[], $3::int[], $4::numeric[]) AS u(id, votes, pct)
					ON CONFLICT DO NOTHING`, ids[i], tagIDs2, votes, pcts); err != nil {
					return inserted, updated, fmt.Errorf("写入标签占比 %d: %w", ids[i], err)
				}
			}
		}
	}

	// 新行并入既有组后重排资料序：更高优先级源（如 bgm）加入时整组 work_id 单条更新
	for w := range joined {
		if _, err := recomputeGroupPrimary(ctx, tx, w); err != nil {
			return inserted, updated, fmt.Errorf("重排作品 %d: %w", w, err)
		}
	}
	// 涉及到的组重算合成评分（主行 score/rating_total = 组内各源按权重加权）
	groups := make([]int64, 0, len(touched))
	for w := range touched {
		groups = append(groups, w)
	}
	if err := synthesizeFor(ctx, tx, groups); err != nil {
		return inserted, updated, err
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
	Source      string // 数据来源（bgm/tmdb/itunes/mal/anilist），空 = 全部来源
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

	// 作品分组塌缩：每组只出主行（work_id = 组内主行 id；0 视为自成作品）。
	// 标签/类型/评分/日期等筛选全部作用在主行字段上，语义可预期。
	conds := []string{"(s.work_id = 0 OR s.id = s.work_id)"}

	// 来源筛选：语义为「该作品组内含此来源的记录」（EXISTS 半连接）；
	// count 缓存键由 where+args 组成，source 参与其中，不同来源各存各的总数。
	if q.Source != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM subjects g WHERE g.work_id = s.work_id AND g.source = "+ph(q.Source)+")")
	}

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

	// 分页翻页时总数不变，count 结果按 where+参数缓存（系统设置可调，默认 60s，
	// 0 = 关闭），省掉重复的全量计数。抓取写入导致的 TTL 内总数偏差对分页 UI 无感。
	countArgs := args // 快照：limit/offset 尚未追加
	countKey := where + "|" + fmt.Sprint(args...)
	countTTL := time.Duration(s.countTTL.Load())
	var cached countEntry
	var ok bool
	if countTTL > 0 {
		s.countMu.Lock()
		cached, ok = s.countCache[countKey]
		s.countMu.Unlock()
	}
	countCached := ok && time.Since(cached.at) < countTTL

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
		if countTTL > 0 { // 0 = 关闭 count 缓存：每次都实算，不落缓存
			s.countMu.Lock()
			if len(s.countCache) >= int(s.countCap.Load()) {
				s.countCache = map[string]countEntry{} // 用户输入型 key 无界增长，超限直接换新
			}
			s.countCache[countKey] = countEntry{total: out.Total, at: time.Now()}
			s.countMu.Unlock()
		}
	}
	defer rows.Close()

	out.Items = make([]Subject, 0, q.PageSize)
	for rows.Next() {
		var sub Subject
		var tagsJSON []byte
		if err := rows.Scan(&sub.ID, &sub.Source, &sub.SourceRef, &sub.Type, &sub.Name, &sub.NameCN, &sub.Date, &sub.Platform, &sub.Rank,
			&sub.Score, &sub.RatingTotal, &sub.RawScore, &sub.RawRatingTotal, &sub.RatingDist, &sub.Wish, &sub.Collect, &sub.Doing, &sub.OnHold, &sub.Dropped,
			&sub.NSFW, &sub.MetaTags, &sub.Image, &sub.Summary, &sub.TagsTotal, &sub.Locked, &sub.FetchedAt, &tagsJSON); err != nil {
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

// SourceMatchStat 是单个扩展源的多源匹配度：行数与已并入多源作品组的行数
// （组内除自身外还有别的来源行）；匹配率由消费方换算。bgm 是锚点源不统计。
type SourceMatchStat struct {
	Rows   int `json:"rows"`
	Merged int `json:"merged"`
}

type Stats struct {
	Total       int                        `json:"total"`
	ByType      map[string]int             `json:"by_type"`
	BySource    map[string]int             `json:"by_source"`
	SourceMatch map[string]SourceMatchStat `json:"source_match"`
	Tags        int                        `json:"tags"`
	SubjectTags int                        `json:"subject_tags"`
	LastFetched *time.Time                 `json:"last_fetched"`
	NSFW        int                        `json:"nsfw"`
	DBSize      int64                      `json:"db_size"`
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	out := Stats{ByType: map[string]int{}, BySource: map[string]int{}, SourceMatch: map[string]SourceMatchStat{}}

	// 六条独立统计查询并行执行：省 IO 等待（总 CPU 不变，都是同一批小聚合）
	var wg sync.WaitGroup
	errs := make([]error, 5)
	wg.Add(6) // 来源计数不计入 errs，失败静默
	go func() {
		defer wg.Done()
		// 总数/敏感数/类型计数一次拿齐，口径 = 作品数（塌缩到主行；扩展源行
		// 与主行同属一个作品，不重复计）。塌缩条件已放进部分索引谓词
		//（0008 subjects_work_type_idx），引用列全部在索引内——
		// 仅索引扫描出结果；旧写法两处 OR 查询各整表扫一遍（生产 169k 行）。
		rows, err := s.Pool.Query(ctx, `
			SELECT type, count(*), count(*) FILTER (WHERE nsfw) FROM subjects
			WHERE work_id = 0 OR id = work_id GROUP BY type`)
		if err != nil {
			errs[0] = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var t, n, nsfwN int
			if err := rows.Scan(&t, &n, &nsfwN); err != nil {
				errs[0] = err
				return
			}
			out.ByType[strconv.Itoa(t)] = n
			out.Total += n
			out.NSFW += nsfwN
		}
		errs[0] = rows.Err()
	}()
	go func() {
		defer wg.Done()
		// 来源计数口径 = 覆盖作品数（DISTINCT work_id；0 值行按自身 id 计）。
		// 引用列随附在 0008 subjects_source_work_idx 内，仅索引扫描。
		rows, err := s.Pool.Query(ctx, `
			SELECT source, count(DISTINCT CASE WHEN work_id = 0 THEN id ELSE work_id END)
			FROM subjects GROUP BY source`)
		if err != nil {
			return // 来源计数是展示性信息，失败不影响统计可用性
		}
		defer rows.Close()
		for rows.Next() {
			var src string
			var n int
			if err := rows.Scan(&src, &n); err != nil {
				return
			}
			out.BySource[src] = n
		}
	}()
	go func() {
		defer wg.Done()
		// 展示用计数改取统计信息估算（reltuples 随抓取后的 ANALYZE 刷新）：
		// 精确 count 对 subject_tags 要扫 45 万行（实测 41-96ms），每轮缓存过期都来一次不划算
		errs[1] = s.Pool.QueryRow(ctx, `
			SELECT GREATEST(reltuples::bigint, 0) FROM pg_class WHERE relname = 'tags'`).Scan(&out.Tags)
	}()
	go func() {
		defer wg.Done()
		errs[2] = s.Pool.QueryRow(ctx, `
			SELECT GREATEST(reltuples::bigint, 0) FROM pg_class WHERE relname = 'subject_tags'`).Scan(&out.SubjectTags)
	}()
	go func() {
		defer wg.Done()
		var last *time.Time
		errs[3] = s.Pool.QueryRow(ctx, `SELECT max(fetched_at) FROM subjects`).Scan(&last)
		out.LastFetched = last
	}()
	go func() {
		defer wg.Done()
		// 多源匹配度：扩展源行中已并入多源作品组（组内还有别的来源行）的比例。
		// 只扫扩展源行（每源几百到几千行），EXISTS 走 work_id 索引；bgm 是锚点源。
		// work_id=0 的迁移前旧行不算并入。
		rows, err := s.Pool.Query(ctx, `
			SELECT s.source, count(*),
				count(*) FILTER (WHERE s.work_id > 0 AND EXISTS (
					SELECT 1 FROM subjects x WHERE x.work_id = s.work_id AND x.id <> s.id))
			FROM subjects s WHERE s.source <> $1 GROUP BY s.source`, SourceBGM)
		if err != nil {
			errs[4] = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var src string
			var sm SourceMatchStat
			if err := rows.Scan(&src, &sm.Rows, &sm.Merged); err != nil {
				errs[4] = err
				return
			}
			out.SourceMatch[src] = sm
		}
		errs[4] = rows.Err()
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

// CountNativeType 统计原生索引某类型的条目数（增量判空：某类型在库为空时，
// 本轮增量对该类型自动按全量预算跑到底，保证空数据/首次安装不漏条目）。
func (s *Store) CountNativeType(ctx context.Context, typ int) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*)::int FROM subjects WHERE source = $1 AND type = $2`,
		registry.SourceBGM, typ).Scan(&n)
	return n, err
}

// CountStale 统计超过 TTL 未刷新的条目数量（只算原生索引：扩展源靠重新抓取保持新鲜；
// 来源维度由登记表常量参数传入，SQL 里不出现来源名字面量）。
// retryCap 是单条连续失败多少次后暂时跳过（fail_count 由 MarkFailed 累加）。
func (s *Store) CountStale(ctx context.Context, ttlDays, retryCap int, types []int) (int, error) {
	if ttlDays < 1 {
		ttlDays = 1
	}
	var n int
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FROM subjects
		WHERE source = $1
			AND fetched_at < now() - ($2::int * interval '1 day')
			AND NOT locked AND fail_count < $3
			AND ($4::int[] IS NULL OR cardinality($4::int[]) = 0 OR type = ANY($4))`,
		registry.SourceBGM, ttlDays, retryCap, types).Scan(&n)
	return n, err
}

// StaleSubjects 返回超过 TTL 未刷新的原生索引条目 id，优先刷新有排名的。
// StaleSubjects 列出超过 TTL 未刷新的条目 id（retryCap = 单条连续失败跳过阈值）。
func (s *Store) StaleSubjects(ctx context.Context, ttlDays, retryCap, limit int, types []int) ([]int64, error) {
	if ttlDays < 1 {
		ttlDays = 1
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id FROM subjects
		WHERE source = $1
			AND fetched_at < now() - ($2::int * interval '1 day')
			AND NOT locked AND fail_count < $3
			AND ($4::int[] IS NULL OR cardinality($4::int[]) = 0 OR type = ANY($4))
		ORDER BY (rank = 0) ASC, rank ASC, fetched_at ASC
		LIMIT $5`, registry.SourceBGM, ttlDays, retryCap, types, limit)
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

// CountStaleBySource 按源统计超过各自保留期的条目数（一次查询取齐，禁 N+1）。
// 每个源带自己的有效保留期（源覆盖值 ?? 全局默认），来源清单由登记表声明
// 的启用中来源构成，SQL 里不出现来源名字面量。扩展源不看 types/fail_count
// （它们的保鲜方式是整体重抓，行数本身就是「该不该重抓」的判定依据）。
func (s *Store) CountStaleBySource(ctx context.Context, sources []string, ttls []int) (map[string]int, error) {
	if len(sources) == 0 || len(sources) != len(ttls) {
		return map[string]int{}, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT t.src, count(*)::int
		FROM unnest($1::text[], $2::int[]) AS t(src, ttl)
		JOIN subjects s ON s.source = t.src
		WHERE s.fetched_at < now() - make_interval(days => t.ttl)
		GROUP BY t.src`, sources, ttls)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int, len(sources))
	for rows.Next() {
		var src string
		var n int
		if err := rows.Scan(&src, &n); err != nil {
			return nil, err
		}
		out[src] = n
	}
	return out, rows.Err()
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

// pruneStalePred 全量清理的旧条目判定：受影响组收集与 DELETE 共用同一谓词，
// 两处口径不许漂移。
const pruneStalePred = `source = $1 AND type = ANY($2) AND (last_run_id IS NULL OR last_run_id <> $3)`

// Prune 删除本轮全量抓取未覆盖的条目（仅限指定类型）。
// 只清 bgm：type=2 的动画如今混有 mal/anilist 行，必须按 source 隔离，
// 否则 bgm 全量清理会把扩展源动画一并删掉。
// 若被清理的 bgm 行是多源作品组的主行（锚点），先把组重排到最优的扩展源行上
// （作品组由扩展源继续存活），再执行删除；删除改变组内成员构成，受影响组
// 在同一事务内重算合成评分。
func (s *Store) Prune(ctx context.Context, runID int64, types []int) (int64, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 待清理且锚定了扩展源的 bgm 主行 → 其组解散到最优扩展源行
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT s.work_id FROM subjects s
		WHERE s.`+pruneStalePred+`
			AND EXISTS (SELECT 1 FROM subjects x WHERE x.work_id = s.work_id AND x.source <> $1)`,
		registry.SourceBGM, types, runID)
	if err != nil {
		return 0, err
	}
	var anchored []int64
	var reanchored []int64
	for rows.Next() {
		var w int64
		if err := rows.Scan(&w); err != nil {
			rows.Close()
			return 0, err
		}
		anchored = append(anchored, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, w := range anchored {
		// 主行即将删除：把组重指到最优的扩展源成员（按登记表优先序动态生成
		// 排序数组——新增源不再改这条 SQL），再连同旧主行一起删
		var newPrimary int64
		if err := tx.QueryRow(ctx, `
			SELECT id FROM subjects
			WHERE work_id = $1 AND source <> $2
			ORDER BY array_position($3::text[], source) NULLS LAST, id
			LIMIT 1`, w, registry.SourceBGM, extSourceOrder()).Scan(&newPrimary); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE subjects SET work_id = $1 WHERE work_id = $2`, newPrimary, w); err != nil {
			return 0, err
		}
		reanchored = append(reanchored, newPrimary)
	}

	// 受影响组收集（先于删除取组）：被删行所在组的合成评分随删除变化
	rows, err = tx.Query(ctx, `SELECT DISTINCT work_id FROM subjects WHERE `+pruneStalePred,
		registry.SourceBGM, types, runID)
	if err != nil {
		return 0, err
	}
	affected := reanchored
	for rows.Next() {
		var w int64
		if err := rows.Scan(&w); err != nil {
			rows.Close()
			return 0, err
		}
		affected = append(affected, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	tag, err := tx.Exec(ctx, `DELETE FROM subjects WHERE `+pruneStalePred,
		registry.SourceBGM, types, runID)
	if err != nil {
		return 0, err
	}
	// 删除后的组员构成上重算合成评分（重指后旧组 id 已无行，合成自动跳过）
	if err := synthesizeFor(ctx, tx, affected); err != nil {
		return 0, fmt.Errorf("清理后重算合成评分: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// extSourceOrder 返回扩展源标识按当前优先级快照排列的数组（越小越靠前），
// bgm 全量清理重指主行时的排序基准；未登记的来源排最后（NULLS LAST）。
// 优先级读 SetSourcePriorities 维护的运行期快照（登记表默认 + 管理设置覆写），
// 与 GroupSourceSort/主行选择同一份序——设置里调序，重指基准跟着变。
func extSourceOrder() []string {
	ids := make([]string, 0, len(registry.All()))
	for _, s := range registry.All() {
		if !s.Native {
			ids = append(ids, s.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return sourceRankOf(ids[i]) < sourceRankOf(ids[j]) })
	return ids
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
	// 元标签预聚合表（MetaTagSuggest 直查本表）。元标签是原生索引专属概念
	//（扩展源 meta_tags 恒为空数组），聚合按来源维度圈定（登记表常量参数传入，
	// 不写类型码或来源名字面量）。
	if _, err := tx.Exec(ctx, `DELETE FROM meta_tag_usage`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO meta_tag_usage (type, name, count)
		SELECT s.type, m, count(*)::int FROM subjects s, unnest(s.meta_tags) AS m
		WHERE s.source = $1 GROUP BY s.type, m`, registry.SourceBGM); err != nil {
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

// DeleteSubject 从索引中移除单个条目。被删行若为组主行，整组重指到剩余最优
// 成员（组继续存活，不悬空）；受影响组在同一事务内重算合成评分。
func (s *Store) DeleteSubject(ctx context.Context, id int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var workID int64
	err = tx.QueryRow(ctx, `SELECT work_id FROM subjects WHERE id = $1`, id).Scan(&workID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM subjects WHERE id = $1`, id); err != nil {
		return err
	}
	// 空组（单人组删空）安全返回原值；删主行时整组重指到剩余最优成员
	primary, err := recomputeGroupPrimary(ctx, tx, workID)
	if err != nil {
		return fmt.Errorf("重排作品组 %d: %w", workID, err)
	}
	if err := synthesizeFor(ctx, tx, []int64{primary}); err != nil {
		return fmt.Errorf("删除后重算合成评分: %w", err)
	}
	return tx.Commit(ctx)
}
