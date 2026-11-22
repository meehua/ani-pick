package store

import (
	"context"
	"testing"

	"anipick/internal/registry"
)

// TestScoreSynthesis 评分合成：主行 score/rating_total = 组内各源按权重加权
// 合成（0 权重不参与），各行 raw_* 永远保持来源自己的原始值；
// 权重调整后重算即生效；全无评分 → 0。
func TestScoreSynthesis(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	defer SetScoreWeights(registry.DefaultWeights())

	// 权重覆写：TMDB 50、itunes 0（默认即 0），其余走默认 100
	SetScoreWeights(map[string]int{
		registry.SourceBGM:    100,
		registry.SourceTMDB:   50,
		registry.SourceITunes: 0,
	})

	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{ID: 900001, Type: registry.TypeReal, Name: "合成评分的作品", Date: "2020-01-01", Score: 8.0, RatingTotal: 100},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{Source: registry.SourceTMDB, SourceRef: "tv/3001", Type: registry.TypeTVSeries,
			Name: "合成评分的作品", NameCN: "合成评分的作品", Date: "2020-02-01",
			Score: 6.0, RatingTotal: 50},
	}, 0); err != nil {
		t.Fatal(err)
	}

	var score float64
	var total int
	if err := pool.QueryRow(ctx, `SELECT score::float8, rating_total FROM subjects WHERE id = 900001`).Scan(&score, &total); err != nil {
		t.Fatal(err)
	}
	// (100×8 + 50×6) / 150 = 7.333…，score 列精度 1 位小数（与既有展示口径一致）；
	// 评分人数 = 组内合计 150
	if diff := score - (100*8.0+50*6.0)/150.0; diff < -0.05 || diff > 0.05 {
		t.Fatalf("合成分应为加权平均 7.333: %v", score)
	}
	if total != 150 {
		t.Fatalf("评分人数应为组内合计 150: %d", total)
	}

	// 各源行保持原始值（评分对比行口径）
	var rawA, rawB float64
	var rawTotalA int
	if err := pool.QueryRow(ctx, `SELECT raw_score::float8, raw_rating_total FROM subjects WHERE source='tmdb' AND source_ref='tv/3001'`).Scan(&rawA, &rawTotalA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT raw_score::float8 FROM subjects WHERE id = 900001`).Scan(&rawB); err != nil {
		t.Fatal(err)
	}
	if rawA != 6.0 || rawTotalA != 50 || rawB != 8.0 {
		t.Fatalf("各行 raw_* 应保持来源原始值: tmdb=%v/%d bgm=%v", rawA, rawTotalA, rawB)
	}

	// 全无评分的作品 → 0
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{ID: 900002, Type: registry.TypeBook, Name: "没有评分的作品", Date: "2020-01-01"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT score::float8 FROM subjects WHERE id = 900002`).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score != 0 {
		t.Fatalf("无评分作品合成分应为 0: %v", score)
	}

	// 权重调整后全量重算即生效：TMDB 权重归 0 → 合成分回到 bgm 原始值
	SetScoreWeights(map[string]int{
		registry.SourceBGM:  100,
		registry.SourceTMDB: 0,
	})
	if _, err := st.SynthesizeScores(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT score::float8, rating_total FROM subjects WHERE id = 900001`).Scan(&score, &total); err != nil {
		t.Fatal(err)
	}
	if score != 8.0 {
		t.Fatalf("TMDB 权重归 0 后合成分应为 bgm 原始值 8.0: %v", score)
	}
	if total != 150 {
		t.Fatalf("评分人数合计口径不变: %d", total)
	}
}

// TestScoreSynthesisWritePaths 写路径收口：删除组员、删除主行、Prune 清理后，
// 受影响组的合成评分在同一写路径事务内即时重算，不依赖任何收尾全量扫描；
// 删主行的组重指到剩余最优成员，不悬空。
func TestScoreSynthesisWritePaths(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()
	defer SetScoreWeights(registry.DefaultWeights())
	// bgm 100、tmdb 50：加权合成分与来源原始值可区分
	SetScoreWeights(map[string]int{
		registry.SourceBGM:  100,
		registry.SourceTMDB: 50,
	})

	seed := func(bgmID int64, ref, name string, bgmScore, tmdbScore float64, runID int64) {
		t.Helper()
		if _, _, err := st.UpsertSubjects(ctx, []Subject{
			{ID: bgmID, Type: registry.TypeReal, Name: name, Date: "2020-01-01",
				Score: bgmScore, RatingTotal: 100},
		}, runID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.UpsertSubjects(ctx, []Subject{
			{Source: registry.SourceTMDB, SourceRef: ref, Type: registry.TypeTVSeries,
				Name: name, NameCN: name, Date: "2020-02-01",
				Score: tmdbScore, RatingTotal: 50},
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	mainScore := func(bgmID int64) (float64, int) {
		t.Helper()
		var sc float64
		var total int
		if err := pool.QueryRow(ctx, `SELECT score::float8, rating_total FROM subjects WHERE id = $1`, bgmID).Scan(&sc, &total); err != nil {
			t.Fatal(err)
		}
		return sc, total
	}
	tmdbID := func(ref string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM subjects WHERE source='tmdb' AND source_ref=$1`, ref).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// 组 900101：bgm 8.0/100 + tmdb 6.0/50 → (100×8+50×6)/150 ≈ 7.333，合计 150
	seed(900101, "tv/4001", "写路径收口的作品", 8.0, 6.0, 99)
	if sc, total := mainScore(900101); sc < 7.28 || sc > 7.39 || total != 150 {
		t.Fatalf("组内合成分应为 7.333/150: %v/%d", sc, total)
	}

	// 删组员（tmdb 行）：主行分即时回落到 bgm 原始值
	if err := st.DeleteSubject(ctx, tmdbID("tv/4001")); err != nil {
		t.Fatal(err)
	}
	if sc, total := mainScore(900101); sc != 8.0 || total != 100 {
		t.Fatalf("删组员后主行分应回落 8.0/100: %v/%d", sc, total)
	}

	// 删主行（bgm 900102）：组重指到 tmdb 行，不悬空，分数即 tmdb 原始值
	seed(900102, "tv/4002", "主行删除的作品", 8.0, 6.0, 99)
	if err := st.DeleteSubject(ctx, 900102); err != nil {
		t.Fatal(err)
	}
	var id, wid int64
	var sc float64
	if err := pool.QueryRow(ctx, `SELECT id, work_id, score::float8 FROM subjects WHERE source='tmdb' AND source_ref='tv/4002'`).Scan(&id, &wid, &sc); err != nil {
		t.Fatal(err)
	}
	if id != wid || sc != 6.0 {
		t.Fatalf("删主行后组应重指到 tmdb 行且分数 6.0: id=%d work_id=%d score=%v", id, wid, sc)
	}
	var orphans int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE work_id = 900102`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Fatalf("旧主行 id 不应再被任何行引用: %d", orphans)
	}

	// Prune 清掉过期 bgm 主行：重指 + 重算同事务收口。
	// 900103 的 last_run_id=1 ≠ 清理轮 99 → 过期；900101 已被轮 99 覆盖，不动。
	seed(900103, "tv/4003", "清理重指的作品", 8.0, 6.0, 1)
	deleted, err := st.Prune(ctx, 99, []int{registry.TypeReal})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("应清理 1 行: %d", deleted)
	}
	if err := pool.QueryRow(ctx, `SELECT id, work_id, score::float8 FROM subjects WHERE source='tmdb' AND source_ref='tv/4003'`).Scan(&id, &wid, &sc); err != nil {
		t.Fatal(err)
	}
	if id != wid || sc != 6.0 {
		t.Fatalf("Prune 后组应重指到 tmdb 行且分数 6.0: id=%d work_id=%d score=%v", id, wid, sc)
	}
}

// TestScoreSynthesisChunked 全量重算分块提交：块间切片不撕裂组（每组全部成员
// 随块聚合），全部主行重算正确；重跑幂等。
func TestScoreSynthesisChunked(t *testing.T) {
	st, pool := newScratchStore(t)
	ctx := context.Background()

	old := synthesizeChunkSize
	synthesizeChunkSize = 2
	t.Cleanup(func() { synthesizeChunkSize = old })

	seed := func(id int64, name string, sc float64, total int) {
		t.Helper()
		if _, _, err := st.UpsertSubjects(ctx, []Subject{
			{ID: id, Type: registry.TypeBook, Name: name, Date: "2020-01-01",
				Score: sc, RatingTotal: total},
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	// 5 个单源组 + 1 个双源组（默认权重 bgm/tmdb 各 100）
	names := []string{"分块重算甲", "分块重算乙", "分块重算丙", "分块重算丁", "分块重算戊"}
	for i, sc := range []float64{7.0, 6.5, 8.2, 9.1, 5.5} {
		seed(900201+int64(i), names[i], sc, 100+i)
	}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		// bgm 行用 TypeReal：剧集 kind（= tmdb tv + bgm 三次元）才允许跨源合并
		{ID: 900206, Type: registry.TypeReal, Name: "分块双源作品", Date: "2020-01-01", Score: 9.0, RatingTotal: 200},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpsertSubjects(ctx, []Subject{
		{Source: registry.SourceTMDB, SourceRef: "tv/4006", Type: registry.TypeTVSeries,
			Name: "分块双源作品", NameCN: "分块双源作品", Date: "2020-02-01", Score: 7.0, RatingTotal: 100},
	}, 0); err != nil {
		t.Fatal(err)
	}

	// 破坏主行值后全量重算：6 个组、块大小 2 → 多块，逐组恢复
	if _, err := pool.Exec(ctx, `UPDATE subjects SET score = 0, rating_total = 0 WHERE id BETWEEN 900201 AND 900206`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SynthesizeScores(ctx); err != nil {
		t.Fatal(err)
	}
	expect := map[int64]float64{
		900201: 7.0, 900202: 6.5, 900203: 8.2, 900204: 9.1, 900205: 5.5,
		900206: (9.0 + 7.0) / 2, // 默认权重各 100
	}
	for id, want := range expect {
		var sc float64
		if err := pool.QueryRow(ctx, `SELECT score::float8 FROM subjects WHERE id = $1`, id).Scan(&sc); err != nil {
			t.Fatal(err)
		}
		if sc != want {
			t.Fatalf("组 %d 全量重算后应为 %v: %v", id, want, sc)
		}
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT rating_total FROM subjects WHERE id = 900206`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 300 {
		t.Fatalf("双源组评分人数应为组内合计 300: %d", total)
	}

	// 重跑幂等：值不变、无错误
	if _, err := st.SynthesizeScores(ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range expect {
		var sc float64
		if err := pool.QueryRow(ctx, `SELECT score::float8 FROM subjects WHERE id = $1`, id).Scan(&sc); err != nil {
			t.Fatal(err)
		}
		if sc != want {
			t.Fatalf("重跑幂等被破坏: 组 %d 应为 %v: %v", id, want, sc)
		}
	}
}
