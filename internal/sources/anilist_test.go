package sources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"anipick/internal/store"
)

// anilistMediaJSON 构造一条 Media 响应（scoreDistribution 总和 = 1000）。
func anilistMediaJSON(id int, title string, meanScore int) string {
	return `{
		"id":` + strconv.Itoa(id) + `,"idMal":` + strconv.Itoa(id) + `,
		"title":{"romaji":"` + title + `","english":"` + title + ` EN","native":"日本語` + title + `"},
		"coverImage":{"extraLarge":"https://s4.anilist.co/cover_l.jpg"},
		"bannerImage":"https://s4.anilist.co/banner.jpg",
		"startDate":{"year":2013,"month":4,"day":7},
		"endDate":{"year":2013,"month":9},
		"description":"<b>简介</b> 文本",
		"meanScore":` + strconv.Itoa(meanScore) + `,
		"popularity":1080842,"favourites":80859,
		"genres":["Action","Drama"],
		"tags":[{"name":"Kaiju","rank":93,"isMediaSpoiler":false},
			{"name":"Tragedy","rank":89,"isMediaSpoiler":true},
			{"name":"TinyRank","rank":5,"isMediaSpoiler":false},
			{"name":"Revenge","rank":85,"isMediaSpoiler":false}],
		"format":"TV","episodes":25,"duration":24,"status":"FINISHED","countryOfOrigin":"JP",
		"siteUrl":"https://anilist.co/anime/` + strconv.Itoa(id) + `",
		"stats":{"scoreDistribution":[{"score":10,"amount":300},{"score":20,"amount":700}]},
		"studios":{"nodes":[{"name":"Studio X"}]}
	}`
}

func TestAniListConvert(t *testing.T) {
	a := NewAniList(nil)
	var m anilistMedia
	if err := json.Unmarshal([]byte(anilistMediaJSON(16498, "Shingeki", 85)), &m); err != nil {
		t.Fatal(err)
	}
	rec := a.convert(m, 20)
	if rec.Source != store.SourceAniList || rec.SourceRef != "16498" || rec.Type != 2 {
		t.Fatalf("身份/类型异常: %s/%s type=%d", rec.Source, rec.SourceRef, rec.Type)
	}
	if rec.Name != "Shingeki EN" || rec.NameCN != "" {
		t.Fatalf("标题映射异常: %q / %q（AniList 无中文标题）", rec.Name, rec.NameCN)
	}
	if rec.Date != "2013-04-07" {
		t.Fatalf("日期异常: %q", rec.Date)
	}
	if rec.Score != 8.5 {
		t.Fatalf("meanScore 应 ÷10, 实际 %v", rec.Score)
	}
	if rec.RatingTotal != 1000 {
		t.Fatalf("rating_total 应为分布总和 1000, 实际 %d", rec.RatingTotal)
	}
	// scoreDistribution（10 分一档）→ 十档人数：score/10 = 档位
	if rec.RatingDist == nil || len(rec.RatingDist) != 10 || rec.RatingDist[0] != 300 || rec.RatingDist[1] != 700 {
		t.Fatalf("分布应归一为十档 (300,700,0…): %v", rec.RatingDist)
	}
	if rec.Collect != 1080842 || rec.Wish != 80859 {
		t.Fatalf("人气/收藏映射异常: %+v", rec)
	}
	if rec.Image != "https://s4.anilist.co/cover_l.jpg" || rec.Platform != "TV" {
		t.Fatalf("封面/平台异常: %q %q", rec.Image, rec.Platform)
	}
	// 剧透标签排除、rank<20 排除，genres 补在后面
	names := make([]string, 0, len(rec.Tags))
	for _, tag := range rec.Tags {
		names = append(names, tag.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "Kaiju") || !strings.Contains(joined, "Revenge") ||
		strings.Contains(joined, "Tragedy") || strings.Contains(joined, "TinyRank") {
		t.Fatalf("标签过滤异常: %v", names)
	}
	if !strings.Contains(joined, "Action") {
		t.Fatalf("genres 应并入标签候选: %v", names)
	}
}

func TestAniListDetailAndFetch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("AniList 应为 POST, 实际 %s", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatal(err)
		}
		gotQuery = req.Query
		id := 16498
		if v, ok := req.Variables["id"].(float64); ok {
			id = int(v)
		}
		if strings.Contains(req.Query, "Page(") {
			_, _ = w.Write([]byte(`{"data":{"Page":{"pageInfo":{"hasNextPage":false},"media":[` +
				anilistMediaJSON(id, "Shingeki", 85) + `]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"Media":` + anilistMediaJSON(id, "Shingeki", 85) + `}}`))
	}))
	defer srv.Close()
	a := &AniList{f: newFetcher(srv.Client(), 0, "test"), baseURL: srv.URL}

	rec, err := a.FetchRecord(context.Background(), "16498")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Score != 8.5 || rec.RatingTotal != 1000 || rec.Name != "Shingeki EN" {
		t.Fatalf("FetchRecord 异常: %+v", rec)
	}
	// 简介随抓取入库（详情只读库不回上游）：StripText 清理内联标记
	if rec.Summary != "简介 文本" {
		t.Fatalf("简介异常: %q", rec.Summary)
	}
	if !strings.Contains(gotQuery, "Media(id: $id") {
		t.Fatalf("单条查询应按 id 取: %s", gotQuery)
	}
}

func TestAniListGraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Too Many Requests"}]}`))
	}))
	defer srv.Close()
	a := &AniList{f: newFetcher(srv.Client(), 0, "test"), baseURL: srv.URL}
	// GraphQL 层错误体是 200 + errors 字段；media id 为 0 → ErrNotFound 语义
	if _, err := a.FetchRecord(context.Background(), "1"); err != ErrNotFound {
		t.Fatalf("空 Media 应返回 ErrNotFound, 实际 %v", err)
	}
}

// TestAniListIncrementalWindow 真增量钉子：有锚点走开播窗口（startDate_greater），
// 空数据（无锚点）按全量预算（人气榜）补齐。
func TestAniListIncrementalWindow(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		queries = append(queries, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"Page":{"pageInfo":{"hasNextPage":false},"media":[]}}}`))
	}))
	defer srv.Close()
	tr := &AniList{f: newFetcher(srv.Client(), 0, "test"), baseURL: srv.URL}

	scope := &Scope{Ctx: context.Background(), Store: store.New(nil, nil),
		Update: func(phase, msg string) {}, Add: func(field string, n int) {},
		PageSize: 50, FullPages: 10, IncPages: 2, TagMinRank: 20,
		LastIncremental: time.Date(2026, 11, 22, 0, 0, 0, 0, time.Local)}
	if err := tr.HarvestIncremental(scope); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(queries, "\n---\n")
	if !strings.Contains(joined, "startDate_greater") || !strings.Contains(joined, "START_DATE_DESC") {
		t.Fatalf("有锚点应走开播窗口查询: %s", joined)
	}
	if !strings.Contains(joined, "20261122") {
		t.Fatalf("窗口锚点应为 FuzzyDateInt: %s", joined)
	}
	if !strings.Contains(joined, "POPULARITY_DESC") {
		t.Fatalf("开播窗口后应有人气榜保底页: %s", joined)
	}

	// 空数据：只有人气榜全量预算，没有窗口查询
	queries = nil
	scope2 := &Scope{Ctx: context.Background(), Store: store.New(nil, nil),
		Update: func(phase, msg string) {}, Add: func(field string, n int) {},
		PageSize: 50, FullPages: 10, IncPages: 2, TagMinRank: 20}
	if err := tr.HarvestIncremental(scope2); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(queries, "\n---\n")
	if strings.Contains(joined, "startDate_greater") {
		t.Fatalf("空数据增量不应有窗口查询: %s", joined)
	}
	if !strings.Contains(joined, "POPULARITY_DESC") {
		t.Fatalf("空数据增量应按全量（人气榜）: %s", joined)
	}
}
