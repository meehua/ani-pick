package crawler

import (
	"math"
	"testing"

	"anipick/internal/bgm"
)

func TestConvertComputesTagProportion(t *testing.T) {
	rec := convert(bgm.Subject{
		ID:   42,
		Type: 2,
		Name: "test",
		Tags: []bgm.Tag{
			{Name: "科幻", Count: 1},
			{Name: "原创", Count: 2},
			{Name: "空标签", Count: 0},
		},
		Rating:     bgm.Rating{Score: 8.5, Rank: 12, Total: 345},
		Collection: bgm.Collection{Wish: 10, Collect: 20, Doing: 3, OnHold: 2, Dropped: 1},
		Images:     bgm.ImageSet{Large: "https://lain.bgm.tv/large.jpg", Medium: "https://lain.bgm.tv/medium.jpg"},
		MetaTags:   []string{"TV", "日本"},
	})

	if rec.TagsTotal != 3 {
		t.Fatalf("TagsTotal = %d, 期望 3", rec.TagsTotal)
	}
	if len(rec.Tags) != 2 {
		t.Fatalf("标签数 = %d, 期望 2（跳过 0 票）", len(rec.Tags))
	}
	if math.Abs(rec.Tags[0].Pct-33.33) > 0.01 {
		t.Fatalf("第一个标签占比 = %v", rec.Tags[0].Pct)
	}
	if math.Abs(rec.Tags[1].Pct-66.67) > 0.01 {
		t.Fatalf("第二个标签占比 = %v", rec.Tags[1].Pct)
	}
	if rec.Image != "https://lain.bgm.tv/large.jpg" {
		t.Fatalf("封面应优先大图: %q", rec.Image)
	}
	if rec.Rank != 12 || rec.Score != 8.5 || rec.RatingTotal != 345 {
		t.Fatalf("评分字段异常: %+v", rec)
	}
	if rec.Collect != 20 || rec.Wish != 10 {
		t.Fatalf("收藏字段异常: %+v", rec)
	}
}

func TestNormalizeDate(t *testing.T) {
	cases := map[string]string{
		"2024-03-15":             "2024-03-15",
		"2024-03":                "2024-03-01",
		"2024":                   "2024-01-01",
		"":                       "",
		"  2020-01-02T00:00:00Z": "2020-01-02",
	}
	for in, want := range cases {
		if got := normalizeDate(in); got != want {
			t.Errorf("normalizeDate(%q) = %q, 期望 %q", in, got, want)
		}
	}
}
