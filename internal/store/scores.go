package store

// 各源原始评分与作品合成评分的分离与合成：
//   - subjects.raw_score / raw_rating_total：该行所属来源自己的原始值（10 分制
//     归一后），任何后续写入都不再改动它们；
//   - 作品组主行的 score / rating_total：按管理员配置的各源权重加权合成的组级
//     值（Σ(w_i×s_i)/Σ(w_i)，评分人数取组内合计），物化在主行上，检索排序与
//     卡片展示直接走索引。
// 权重快照由 server 在启动与保存设置后注入（SetScoreWeights）。

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

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

// weightArrays 把权重快照展开成并行数组（SQL unnest 参数）。
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

// SynthesizeScores 按当前权重快照对作品组执行全量合成（workIDs 空 = 全部）。
// 返回耗时：抓取收尾/重跑匹配/设置保存后的重算都用它，耗时写日志留档。
func (s *Store) SynthesizeScores(ctx context.Context, workIDs ...int64) (time.Duration, error) {
	start := time.Now()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	if err := synthesizeFor(ctx, tx, workIDs); err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	elapsed := time.Since(start)
	if len(workIDs) == 0 {
		slog.Info("评分合成完成", "耗时", elapsed.String())
	}
	return elapsed, nil
}
