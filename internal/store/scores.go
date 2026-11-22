package store

// 各源原始评分与作品合成评分的分离与合成：
//   - subjects.raw_score / raw_rating_total：该行所属来源自己的原始值（10 分制
//     归一后），任何后续写入都不再改动它们；
//   - 作品组主行的 score / rating_total：按管理员配置的各源权重加权合成的组级
//     值（Σ(w_i×s_i)/Σ(w_i)，评分人数取组内合计），物化在主行上，检索排序与
//     卡片展示直接走索引。
// 权重快照由 server 在启动与保存设置后注入（SetScoreWeights）。
//
// 收口规则（裁决台账 2026-11-22）：一切改评分输入的写路径在各自事务内对受影响
// 组即时重算（UpsertSubjects / Prune / DeleteSubject）；全量重算只剩「重新配对
// 作品」与「设置保存」两个入口，分块提交，同一时刻只跑一轮（并发合并见 server）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"anipick/internal/registry"
)

// scoreWeights 是评分合成权重的运行期快照（source → 0-100，0 = 不参与）。
var scoreWeights atomic.Value // map[string]int

func init() {
	SetScoreWeights(registry.DefaultWeights())
}

// SetScoreWeights 更新评分权重快照（server 保存设置后调用）；
// 只接受登记过的来源，未声明/非法值回落登记表默认。
func SetScoreWeights(w map[string]int) {
	def := registry.DefaultWeights()
	out := make(map[string]int, len(def))
	for id, d := range def {
		out[id] = d
	}
	for id, v := range w {
		if registry.Exists(id) && v >= 0 && v <= 100 {
			out[id] = v
		}
	}
	scoreWeights.Store(out)
}

func scoreWeightsSnapshot() map[string]int {
	v, _ := scoreWeights.Load().(map[string]int)
	return v
}

// weightArrays 把权重快照展开成并行数组（SQL unnest 参数）；每次合成调用时
// 重新读取快照——分块全量重算跑到一半改权重，后续块自动用新值。
func weightArrays() ([]string, []float64) {
	w := scoreWeightsSnapshot()
	srcs := make([]string, 0, len(w))
	vals := make([]float64, 0, len(w))
	for _, s := range registry.All() {
		srcs = append(srcs, s.ID)
		vals = append(vals, float64(w[s.ID]))
	}
	return srcs, vals
}

// synthTunables 是评分合成全量重算的运行期参数（原为代码常量，现由管理设置
// 立刷：块大小/单块与整体超时的取舍随机器规格而异——生产 169k 行单事务实测
// 7 分半，2 分钟超时必死，故按 work_id 分页、每块独立事务提交）。
type synthTunables struct {
	chunkSize      int
	chunkTimeout   time.Duration
	overallTimeout time.Duration
}

var synthParams = func() atomic.Value {
	v := atomic.Value{}
	v.Store(synthTunables{chunkSize: 2000, chunkTimeout: 2 * time.Minute, overallTimeout: 30 * time.Minute})
	return v
}()

// SetScoreSynthTunables 立刷评分合成参数（零值/负值字段忽略，保留原值；
// 保存设置后由快照链路调用）。
func SetScoreSynthTunables(chunkSize int, chunkTimeout, overallTimeout time.Duration) {
	cur := synthParams.Load().(synthTunables)
	if chunkSize > 0 {
		cur.chunkSize = chunkSize
	}
	if chunkTimeout > 0 {
		cur.chunkTimeout = chunkTimeout
	}
	if overallTimeout > 0 {
		cur.overallTimeout = overallTimeout
	}
	synthParams.Store(cur)
}

// SynthesizeOverallTimeout 返回全量重算的整体上限（重新配对作品 / 设置保存用
// 它包外层 context）；每块独立提交，整体超时只是兜底。
func SynthesizeOverallTimeout() time.Duration {
	return synthParams.Load().(synthTunables).overallTimeout
}

// SQLSTATE：死锁牺牲品 / 序列化冲突——块级重试可解的竞态类错误。
const (
	sqlStateDeadlock      = "40P01"
	sqlStateSerialization = "40001"
)

// synthesizeStmt 是合成语句：组内 raw_score>0 的源按权重加权平均，
// 评分人数取组内合计；只写主行（id = work_id），值未变的行不产生写入。
const synthesizeStmt = `
	WITH w AS (SELECT * FROM unnest($1::text[], $2::real[]) AS t(src, weight)),
	agg AS (
		SELECT s.work_id AS wid,
			sum(CASE WHEN s.raw_score > 0 THEN w.weight * s.raw_score ELSE 0 END)::real AS wsum,
			sum(CASE WHEN s.raw_score > 0 THEN w.weight ELSE 0 END)::real AS wden,
			sum(s.raw_rating_total)::int AS total,
			(CASE WHEN sum(CASE WHEN s.raw_score > 0 THEN w.weight ELSE 0 END) > 0
				THEN sum(CASE WHEN s.raw_score > 0 THEN w.weight * s.raw_score ELSE 0 END)::real
					/ sum(CASE WHEN s.raw_score > 0 THEN w.weight ELSE 0 END)::real
				ELSE 0::real END)::real AS score
		FROM subjects s LEFT JOIN w ON w.src = s.source
		WHERE (:COND:)
		GROUP BY s.work_id
	)
	UPDATE subjects p SET score = agg.score, rating_total = agg.total
	FROM agg
	WHERE p.id = p.work_id AND p.work_id = agg.wid
		AND (p.score IS DISTINCT FROM agg.score OR p.rating_total IS DISTINCT FROM agg.total)`

// synthesizeFor 在给定事务内对指定作品组执行合成（workIDs 空 = 全部作品）。
func synthesizeFor(ctx context.Context, tx pgxTx, workIDs []int64) error {
	srcs, vals := weightArrays()
	args := []any{srcs, vals}
	stmt := synthesizeStmt
	if len(workIDs) > 0 {
		stmt = strings.Replace(stmt, ":COND:", "s.work_id = ANY($3)", 1)
		args = append(args, workIDs)
	} else {
		stmt = strings.Replace(stmt, ":COND:", "TRUE", 1)
	}
	if _, err := tx.Exec(ctx, stmt, args...); err != nil {
		return fmt.Errorf("评分合成: %w", err)
	}
	return nil
}

// SynthesizeScores 按当前权重快照对全部作品组执行全量合成。
// 入口只剩重新配对作品与设置保存（写路径都在各自事务内按组收口）；
// 按 work_id 分页分块、块完成即提交：每组取全部成员聚合保证完整，
// 竞态失败重试一次、仍失败记日志跳过留待下轮重算覆盖。耗时写日志留档。
func (s *Store) SynthesizeScores(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	tp := synthParams.Load().(synthTunables)
	blocks, failed := 0, 0
	var last int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		pctx, cancel := context.WithTimeout(ctx, tp.chunkTimeout)
		ids, err := s.nextSynthesizeChunk(pctx, last, tp.chunkSize)
		if err != nil {
			cancel()
			return 0, fmt.Errorf("评分合成分页（work_id>%d）: %w", last, err)
		}
		if len(ids) == 0 {
			cancel()
			break
		}
		if err := s.synthesizeChunk(pctx, ids); err != nil {
			failed++
			slog.Error("评分合成分块失败，跳过留待下轮重算",
				"块", blocks+1, "起始组", ids[0], "err", err)
		}
		cancel()
		last = ids[len(ids)-1]
		blocks++
	}
	elapsed := time.Since(start)
	slog.Info("评分合成完成", "耗时", elapsed.String(), "块", blocks, "失败块", failed)
	return elapsed, nil
}

// synthesizePageStmt 全量重算的分页查询：按 work_id 有序去重取下一块
// （subjects_work_idx 有序扫描 + 去重，LIMIT 截断有界）；work_id=0 的迁移前
// 旧行自成一组、无主行可写，直接排除。
const synthesizePageStmt = `
	SELECT DISTINCT work_id FROM subjects
	WHERE work_id > $1
	ORDER BY work_id
	LIMIT $2`

// nextSynthesizeChunk 取下一块作品组 id（有序去重，last 为上一块末尾，
// size 为每块组数）。
func (s *Store) nextSynthesizeChunk(ctx context.Context, last int64, size int) ([]int64, error) {
	rows, err := s.Pool.Query(ctx, synthesizePageStmt, last, size)
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

// synthesizeChunk 单块合成：独立事务，提交即落盘，长跑不占长事务。
// 死锁/序列化冲突（并发写路径与全量重算撞行锁）重试一次，其余错误直接返回。
func (s *Store) synthesizeChunk(ctx context.Context, ids []int64) error {
	for attempt := 0; ; attempt++ {
		err := s.synthesizeChunkOnce(ctx, ids)
		if err == nil || attempt > 0 || ctx.Err() != nil || !retryableSQLErr(err) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Store) synthesizeChunkOnce(ctx context.Context, ids []int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := synthesizeFor(ctx, tx, ids); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// retryableSQLErr 判定块级重试可解的竞态类 SQL 错误（按 SQLSTATE 判型，
// 不做错误文本匹配）。
func retryableSQLErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		(pgErr.Code == sqlStateDeadlock || pgErr.Code == sqlStateSerialization)
}
