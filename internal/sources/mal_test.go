package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"anipick/internal/store"
)

// newMALTest 搭一个假 MAL 上游：校验 X-MAL-CLIENT-ID 头，返回固定样本；
// clientID 为空时用默认的 "cid-test"。
func newMALTest(t *testing.T, clientID string, handler func(t *testing.T, path string, q url.Values, auth string) (int, string)) *MAL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, body := handler(t, r.URL.Path, r.URL.Query(), r.Header.Get("X-MAL-CLIENT-ID"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	if clientID == "" {
		clientID = "cid-test"
	}
	c := NewCredStore()
	c.Set(map[string]map[string]string{"mal": {"client_id": clientID}})
	f := newFetcher(srv.Client(), 0, "test")
	return &MAL{f: f, creds: c, baseURL: srv.URL}
}

// malRankingSample 是榜单单页的固定样本：覆盖双标题集、字段映射与 nsfw 判定。
const malRankingSample = `{"data":[
	{"node":{"id":1,"title":"Cyberpunk Overture","main_picture":{"medium":"/m.jpg","large":"/L.jpg"},
	 "alternative_titles":{"en":"Cyber Overture","ja":"サイバー序曲","synonyms":["CP Overture"]},
	 "start_date":"2020-01-11","end_date":"2020-03-28","synopsis":"<b>概要</b>　本文。",
	 "mean":8.7,"rank":120,"popularity":300,"num_scoring_users":54000,
	 "num_episodes":12,"average_episode_duration":1440,
	 "genres":[{"id":1,"name":"动作"},{"id":2,"name":"科幻"}],
	 "studios":[{"id":11,"name":"Nitro+"}],"media_type":"tv"}},
	{"node":{"id":2,"title":"Hidden Treasure","main_picture":{"large":"/h.jpg"},
	 "alternative_titles":{"en":"HiT","synonyms":["隐藏宝物"]},
	 "start_date":"2015-06-03","synopsis":"简介",
	 "mean":7.1,"rank":900,"popularity":1500,"num_scoring_users":12000,
	 "genres":[{"id":3,"name":"Hentai"}],"studios":[],"media_type":"ova"}}
]}`

func TestMALRankingRequestShape(t *testing.T) {
	var (
		gotAuth   string
		gotQuery  url.Values
		gotPath   string
		pageCalls int
	)
	tr := newMALTest(t, "", func(t *testing.T, path string, q url.Values, auth string) (int, string) {
		gotAuth, gotQuery, gotPath = auth, q, path
		pageCalls++
		return http.StatusOK, `{"data":[]}`
	})
	if !tr.Configured() {
		t.Fatal("有 client_id 应为已配置")
	}

	scope := &Scope{Ctx: context.Background(), Store: store.New(nil, nil),
		Update: func(phase, msg string) {}, Add: func(field string, n int) {}}
	if err := tr.HarvestFull(scope); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "cid-test" {
		t.Fatalf("X-MAL-CLIENT-ID 头未携带或值错误: %q", gotAuth)
	}
	if gotPath != "/anime/ranking" {
		t.Fatalf("发现端点异常: %s", gotPath)
	}
	if gotQuery.Get("ranking_type") != "all" || gotQuery.Get("offset") != "0" {
		t.Fatalf("全量榜单参数异常: %s", gotQuery.Encode())
	}
	if gotQuery.Get("limit") != "500" {
		t.Errorf("limit 应为官方单页上限 500: %s", gotQuery.Get("limit"))
	}
	if gotQuery.Get("fields") == "" || !strings.Contains(gotQuery.Get("fields"), "num_scoring_users") ||
		!strings.Contains(gotQuery.Get("fields"), "alternative_titles") {
		t.Errorf("fields 清单异常: %s", gotQuery.Get("fields"))
	}
	if pageCalls != 1 {
		t.Fatalf("单页预算应只发 1 页请求, 实际 %d", pageCalls)
	}
	// 第 2 页 offset = 单页上限
	if u := rankingURL("https://api.myanimelist.net/v2", "all", 2, 500); !strings.Contains(u, "offset=500") {
		t.Errorf("第 2 页 offset 应为 500: %s", u)
	}
}

// TestMALConvertMapping 字段映射：双标题集喂合并器、评分口径（num_scoring_users
// 而非 num_list_users）、平台词表、nsfw 按 genres 判定、图片取 large。
func TestMALConvertMapping(t *testing.T) {
	tr := newMALTest(t, "", func(t *testing.T, path string, q url.Values, auth string) (int, string) {
		return http.StatusOK, malRankingSample
	})
	var out malRankingResp
	if err := tr.f.getJSONHeaders(context.Background(), tr.baseURL+"/anime/ranking?limit=500", &out, tr.authHeaders()); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 2 {
		t.Fatalf("样本条目数 = %d", len(out.Data))
	}
	rec := tr.convert(out.Data[0].Node)
	if rec.Source != store.SourceMAL || rec.SourceRef != "1" || rec.ExtKey != "1" {
		t.Fatalf("身份异常: %s/%s key=%s", rec.Source, rec.SourceRef, rec.ExtKey)
	}
	if rec.Name != "Cyberpunk Overture" {
		t.Fatalf("name 映射异常: %q", rec.Name)
	}
	if rec.NameCN != "" {
		t.Fatalf("MAL 无中文标题，name_cn 应为空: %q", rec.NameCN)
	}
	// alternative_titles.en/ja/synonyms 全喂给合并器
	if len(rec.AltTitles) != 3 ||
		rec.AltTitles[0] != "Cyber Overture" || rec.AltTitles[1] != "サイバー序曲" || rec.AltTitles[2] != "CP Overture" {
		t.Fatalf("alt_titles 映射异常: %v", rec.AltTitles)
	}
	if rec.Type != store.TypeAnime {
		t.Fatalf("kind anime 应映射类型 2: %d", rec.Type)
	}
	// mean → 10 分制原样；num_scoring_users（非 num_list_users）→ 评分人数
	if rec.Score != 8.7 || rec.RatingTotal != 54000 {
		t.Fatalf("评分映射异常: score=%v total=%d", rec.Score, rec.RatingTotal)
	}
	if rec.Rank != 120 || rec.Date != "2020-01-11" {
		t.Fatalf("排名/日期映射异常: rank=%d date=%q", rec.Rank, rec.Date)
	}
	if rec.Platform != "TV" {
		t.Fatalf("media_type tv 应映射平台 TV: %q", rec.Platform)
	}
	if rec.NSFW {
		t.Fatal("普通条目不应标敏感")
	}
	if rec.Image != "/L.jpg" {
		t.Fatalf("main_picture.large → image: %q", rec.Image)
	}
	if len(rec.Tags) != 2 || rec.Tags[0].Name != "动作" {
		t.Fatalf("genres → tags 映射异常: %+v", rec.Tags)
	}

	nsfwRec := tr.convert(out.Data[1].Node)
	if !nsfwRec.NSFW {
		t.Fatal("Hentai 类型应判定为敏感")
	}
	if nsfwRec.Platform != "OVA" {
		t.Fatalf("media_type ova 应映射平台 OVA: %q", nsfwRec.Platform)
	}
	if nsfwRec.NameCN != "" || nsfwRec.ExtKey != "2" {
		t.Fatalf("敏感条目身份/标题异常: %s %s", nsfwRec.NameCN, nsfwRec.ExtKey)
	}
}

func TestMALIncrementalUsesAiring(t *testing.T) {
	var rankingType string
	tr := newMALTest(t, "", func(t *testing.T, path string, q url.Values, auth string) (int, string) {
		rankingType = q.Get("ranking_type")
		return http.StatusOK, `{"data":[]}`
	})
	scope := &Scope{Ctx: context.Background(), Store: store.New(nil, nil),
		Update: func(phase, msg string) {}, Add: func(field string, n int) {},
		LastIncremental: time.Now()} // 有锚点 = 正常增量走在播榜
	if err := tr.HarvestIncremental(scope); err != nil {
		t.Fatal(err)
	}
	if rankingType != "airing" {
		t.Fatalf("增量应用 airing 榜单: %s", rankingType)
	}

	// 空数据（无锚点）：增量自动升级为全量（总榜），保证首装条目不丢
	rankingType = ""
	scope2 := &Scope{Ctx: context.Background(), Store: store.New(nil, nil),
		Update: func(phase, msg string) {}, Add: func(field string, n int) {}}
	if err := tr.HarvestIncremental(scope2); err != nil {
		t.Fatal(err)
	}
	if rankingType != "all" {
		t.Fatalf("空数据增量应按全量（all 榜）: %s", rankingType)
	}
}

func TestMALDetailAndValidate(t *testing.T) {
	tr := newMALTest(t, "", func(t *testing.T, path string, q url.Values, auth string) (int, string) {
		if !strings.HasPrefix(auth, "cid-") {
			t.Errorf("鉴权头缺失: %q", auth)
		}
		switch path {
		case "/anime/5":
			return http.StatusOK, `{"id":5,"title":"Sample Anime","media_type":"movie",
				"main_picture":{"large":"/s.jpg"},"start_date":"2018-04-07","end_date":"2018-04-07",
				"synopsis":"p","mean":6.5,"num_scoring_users":800,"num_episodes":1,
				"average_episode_duration":5400,
				"alternative_titles":{"en":"Sample EN","ja":"サンプル"},"genres":[{"id":1,"name":"动作"}],
				"studios":[{"id":9,"name":"Studio X"}]}`
		case "/anime/1":
			return http.StatusOK, `{"id":1}`
		default:
			return http.StatusNotFound, `{"message":"not found"}`
		}
	})

	// 详情代理：字段映射进 Detail
	d, err := tr.Detail(context.Background(), "5")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Sample Anime" || d.Platform != "剧场版" || d.Score != 6.5 || d.ScoreCount != 800 {
		t.Fatalf("详情基础字段异常: %+v", d)
	}
	if d.Episodes != 1 || d.Runtime != 90 { // 5400s → 90min
		t.Fatalf("集数/片长映射异常: eps=%d runtime=%d", d.Episodes, d.Runtime)
	}
	if d.EndDate != "2018-04-07" || d.Image != "/s.jpg" {
		t.Fatalf("日期/图片映射异常: %q %q", d.EndDate, d.Image)
	}
	if d.Summary != "p" {
		t.Fatalf("简介清洗异常: %q", d.Summary)
	}
	hasStudio := false
	for _, f := range d.Facts {
		if f.K == "制作" && f.V == "Studio X" {
			hasStudio = true
		}
	}
	if !hasStudio {
		t.Fatalf("facts 缺制作方: %+v", d.Facts)
	}
	if !strings.HasPrefix(d.Link, "https://myanimelist.net/anime/5") {
		t.Fatalf("外链异常: %q", d.Link)
	}
	rec, err := tr.FetchRecord(context.Background(), "5")
	if err != nil {
		t.Fatal(err)
	}
	if rec.SourceRef != "5" || rec.ExtKey != "5" {
		t.Fatalf("单条刷新身份异常: %s/%s", rec.SourceRef, rec.ExtKey)
	}

	// 校验器：200 = 有效
	if err := tr.ValidateCredentials(context.Background(), map[string]string{"client_id": " cid-ok "}); err != nil {
		t.Fatalf("有效 Client ID 校验应通过: %v", err)
	}

	// 校验器：401/400 = 无效（人话 + 建议重新生成）
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Invalid client id","error":"bad_request"}`))
	}))
	defer badSrv.Close()
	trBad := &MAL{f: newFetcher(badSrv.Client(), 0, "test"), creds: NewCredStore(), baseURL: badSrv.URL}
	err = trBad.ValidateCredentials(context.Background(), map[string]string{"client_id": "bad"})
	if err == nil || !strings.Contains(err.Error(), "重新生成") {
		t.Fatalf("无效 Client ID 应给重新生成提示: %v", err)
	}
	if err := tr.ValidateCredentials(context.Background(), map[string]string{}); err == nil {
		t.Fatal("空凭据应直接拒绝")
	}
}

func TestMALNotConfigured(t *testing.T) {
	tr := NewMAL(NewCredStore(), nil)
	if tr.Configured() {
		t.Fatal("无 client_id 应为未配置")
	}
	if _, err := tr.Detail(context.Background(), "1"); err != ErrNotConfigured {
		t.Fatalf("未配置应返回 ErrNotConfigured, 实际 %v", err)
	}
	if err := tr.HarvestFull(&Scope{Ctx: context.Background()}); err != ErrNotConfigured {
		t.Fatalf("未配置抓取应返回 ErrNotConfigured, 实际 %v", err)
	}
}
