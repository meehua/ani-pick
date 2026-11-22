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
