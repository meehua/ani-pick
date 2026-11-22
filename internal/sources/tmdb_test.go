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

// newTMDBTest 搭一个假 TMDB 上游：校验 api_key 随 query 传递，返回固话样本。
func newTMDBTest(t *testing.T, apiKey string, handler func(t *testing.T, path string, q url.Values) (int, string)) *TMDB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, body := handler(t, r.URL.Path, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	f := newFetcher(srv.Client(), 0, "test")
	return &TMDB{f: f, creds: credStoreWith(apiKey), baseURL: srv.URL}
}

func TestTMDBDiscoverAndConvert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k-test" {
			t.Errorf("api_key 未随请求传递: %s", r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/genre/movie/list":
			_, _ = w.Write([]byte(`{"genres":[{"id":28,"name":"动作"},{"id":878,"name":"科幻"}]}`))
		case "/discover/movie":
			_, _ = w.Write([]byte(`{"page":1,"total_results":2,"results":[
				{"id":603,"title":"黑客帝国","original_title":"The Matrix","overview":"概述",
				 "poster_path":"/abc.jpg","release_date":"1999-03-31",
				 "vote_average":8.2,"vote_count":100,"genre_ids":[28,878],"adult":false},
				{"id":604,"title":"","original_title":"Interstellar","poster_path":"",
				 "release_date":"2014-11-05","vote_average":8.0,"vote_count":50,"genre_ids":[999],"adult":true}
			]}`))
		default:
			t.Errorf("未预期的路径 %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	tr := &TMDB{f: newFetcher(srv.Client(), 0, "test"), creds: credStoreWith("k-test"), baseURL: srv.URL}
	genres := tr.genreNames(context.Background(), "movie")
	if genres[28] != "动作" || genres[878] != "科幻" {
		t.Fatalf("类型字典异常: %v", genres)
	}
	items, err := tr.discover(context.Background(), "movie", 1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("条目数 = %d", len(items))
	}
	rec := tr.convert("movie", items[0], genres)
	if rec.Source != store.SourceTMDB || rec.SourceRef != "movie/603" {
		t.Fatalf("身份异常: %s/%s", rec.Source, rec.SourceRef)
	}
	if rec.Name != "The Matrix" || rec.NameCN != "黑客帝国" {
		t.Fatalf("标题映射异常: %q / %q", rec.Name, rec.NameCN)
	}
	if rec.Date != "1999-03-31" || rec.Score != 8.2 || rec.RatingTotal != 100 {
		t.Fatalf("基础字段异常: %+v", rec)
	}
	if rec.Image != "https://image.tmdb.org/t/p/w500/abc.jpg" {
		t.Fatalf("海报 URL 异常: %q", rec.Image)
	}
	if len(rec.Tags) != 2 || rec.Tags[0].Name != "动作" || rec.Tags[1].Name != "科幻" {
		t.Fatalf("标签映射异常: %+v", rec.Tags)
	}
	if rec.Type != store.TypeMovie || rec.MetaTags == nil || len(rec.MetaTags) != 0 {
		t.Fatalf("类型/元标签异常: type=%d meta=%v", rec.Type, rec.MetaTags)
	}

	// 未知 genre_id 被丢弃；无海报给空；adult 映射 nsfw
	rec2 := tr.convert("movie", items[1], genres)
	if len(rec2.Tags) != 0 || rec2.Image != "" || !rec2.NSFW {
		t.Fatalf("第二条款目归一化异常: %+v", rec2)
	}

	// tv 条目：name/first_air_date/剧集类型
	tv := tr.convert("tv", tmdbDiscoverItem{ID: 1396, Name: "绝命毒师", OriginalName: "Breaking Bad",
		FirstAirDate: "2008-01-20", VoteAverage: 8.9, VoteCount: 30}, genres)
	if tv.Type != store.TypeTVSeries || tv.SourceRef != "tv/1396" || tv.Date != "2008-01-20" ||
		tv.Name != "Breaking Bad" || tv.NameCN != "绝命毒师" {
		t.Fatalf("tv 归一化异常: %+v", tv)
	}
}

func TestTMDBDetailMovie(t *testing.T) {
	tr := newTMDBTest(t, "k-test", func(t *testing.T, path string, q url.Values) (int, string) {
		if path != "/movie/603" {
			t.Errorf("路径异常 %s", path)
		}
		if q.Get("language") != "zh-CN" {
			t.Errorf("详情参数异常: %s", q.Encode())
		}
		return http.StatusOK, `{
			"id":603,"title":"黑客帝国","original_title":"The Matrix","overview":"<br> overview 文本",
			"poster_path":"/abc.jpg","backdrop_path":"/bg.jpg","release_date":"1999-03-31",
			"runtime":136,"vote_average":8.2,"vote_count":100,"adult":false,
			"status":"Released","imdb_id":"tt0133093",
			"genres":[{"id":28,"name":"动作"},{"id":878,"name":"科幻"}],
			"production_countries":[{"iso_3166_1":"US","name":"美国"}],
			"credits":{"cast":[{"name":"Keanu Reeves"},{"name":"Carrie-Anne Moss"}],
				"crew":[{"name":"Lana Wachowski","job":"Director"},{"name":"Lilly Wachowski","job":"Director"},
					{"name":"某编剧","job":"Screenplay"}]}
		}`
	})
	rec, err := tr.FetchRecord(context.Background(), "movie/603")
	if err != nil {
		t.Fatal(err)
	}
	if rec.SourceRef != "movie/603" || rec.Score != 8.2 || rec.RatingTotal != 100 || rec.NameCN != "黑客帝国" {
		t.Fatalf("单条记录基础字段异常: %+v", rec)
	}
	if rec.Image != "https://image.tmdb.org/t/p/w500/abc.jpg" {
		t.Fatalf("海报异常: %q", rec.Image)
	}
	// 简介随抓取入库（详情只读库）：StripText 清理内联标记
	if rec.Summary != "overview 文本" {
		t.Fatalf("简介未清理标签: %q", rec.Summary)
	}
}

func TestTMDBFetchRecordTV(t *testing.T) {
	tr := newTMDBTest(t, "k-test", func(t *testing.T, path string, q url.Values) (int, string) {
		if path != "/tv/1396" {
			t.Errorf("路径异常 %s", path)
		}
		return http.StatusOK, `{
			"id":1396,"name":"绝命毒师","original_name":"Breaking Bad",
			"first_air_date":"2008-01-20","last_air_date":"2013-09-29",
			"episode_run_time":[47],"number_of_seasons":5,"number_of_episodes":62,
			"vote_average":8.9,"vote_count":30,"genres":[{"id":18,"name":"剧情"}],
			"created_by":[{"name":"Vince Gilligan"}],"networks":[{"name":"AMC"}],
			"status":"Ended","credits":{"cast":[{"name":"Bryan Cranston"}]}
		}`
	})
	rec, err := tr.FetchRecord(context.Background(), "tv/1396")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Type != store.TypeTVSeries || rec.NameCN != "绝命毒师" || rec.Name != "Breaking Bad" ||
		rec.Date != "2008-01-20" || rec.Score != 8.9 || rec.Platform != "剧集" {
		t.Fatalf("tv 索引行异常: %+v", rec)
	}
	if len(rec.Tags) != 1 || rec.Tags[0].Name != "剧情" {
		t.Fatalf("标签异常: %+v", rec.Tags)
	}

	if _, err := tr.FetchRecord(context.Background(), "book/1"); err == nil {
		t.Fatal("非法 ref 应报错")
	}
}

func TestTMDBNotConfigured(t *testing.T) {
	tr := NewTMDB(NewCredStore(), nil)
	if tr.Configured() {
		t.Fatal("无 key 应为未配置")
	}
	if _, err := tr.FetchRecord(context.Background(), "movie/1"); err != ErrNotConfigured {
		t.Fatalf("未配置应返回 ErrNotConfigured, 实际 %v", err)
	}
	if err := tr.HarvestFull(&Scope{Ctx: context.Background()}); err != ErrNotConfigured {
		t.Fatalf("未配置抓取应返回 ErrNotConfigured, 实际 %v", err)
	}
}

func TestTMDBUpstreamError(t *testing.T) {
	tr := newTMDBTest(t, "k-test", func(t *testing.T, path string, q url.Values) (int, string) {
		return http.StatusNotFound, `{"status_message":"Not found"}`
	})
	if _, err := tr.FetchRecord(context.Background(), "movie/999"); err != ErrNotFound {
		t.Fatalf("404 应映射 ErrNotFound, 实际 %v", err)
	}
	if _, err := tr.FetchRecord(context.Background(), strings.Repeat("x", 3)); err == nil {
		t.Fatal("非法 ref 应报错")
	}
}

// credStoreWith 构造含单个 TMDB api_key 的凭据快照（测试注入用）。
func credStoreWith(key string) *CredStore {
	c := NewCredStore()
	if key != "" {
		c.Set(map[string]map[string]string{"tmdb": {"api_key": key}})
	}
	return c
}

// credStoreWith2 构造含 v3 key 与 v4 读令牌的凭据快照（双鉴权测试用）。
func credStoreWith2(key, readToken string) *CredStore {
	c := NewCredStore()
	fields := map[string]string{}
	if key != "" {
		fields["api_key"] = key
	}
	if readToken != "" {
		fields["read_token"] = readToken
	}
	if len(fields) > 0 {
		c.Set(map[string]map[string]string{"tmdb": fields})
	}
	return c
}

// TestTMDBDualAuth 钉住双鉴权优先序：有 v4 读令牌走 Bearer 头（凭据不进 URL），
// 只有 v3 key 时走 api_key query 参数；任一存在即视为已配置。
func TestTMDBDualAuth(t *testing.T) {
	var gotAuth, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.Query().Get("api_key")
		_, _ = w.Write([]byte(`{"page":1,"total_results":0,"results":[]}`))
	}))
	defer srv.Close()

	// 只有 v3 key：api_key 随 query 传递，无 Bearer 头
	onlyKey := &TMDB{f: newFetcher(srv.Client(), 0, "test"), creds: credStoreWith("k-v3"), baseURL: srv.URL}
	if !onlyKey.Configured() {
		t.Fatal("有 api_key 应为已配置")
	}
	if _, err := onlyKey.discover(context.Background(), "movie", 1, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "k-v3" || gotAuth != "" {
		t.Fatalf("v3 模式应走 api_key query: query=%q auth=%q", gotQuery, gotAuth)
	}

	// 读令牌 + key：Bearer 优先，api_key 不再进 URL
	both := &TMDB{f: newFetcher(srv.Client(), 0, "test"), creds: credStoreWith2("k-v3", "tok-v4"), baseURL: srv.URL}
	if !both.Configured() {
		t.Fatal("有读令牌应为已配置")
	}
	if _, err := both.discover(context.Background(), "movie", 1, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-v4" || gotQuery != "" {
		t.Fatalf("读令牌模式应走 Bearer 且不带 api_key: query=%q auth=%q", gotQuery, gotAuth)
	}
}

// TestTMDBValidateCredentials 校验器：两种鉴权各验一次成功态与失败态，
// 空凭据直接拒绝（不发请求）。
func TestTMDBValidateCredentials(t *testing.T) {
	var sawAuth, sawQuery string
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawQuery = r.URL.Query().Get("api_key")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer okSrv.Close()
	tr := &TMDB{f: newFetcher(okSrv.Client(), 0, "test"), creds: NewCredStore(), baseURL: okSrv.URL}

	if err := tr.ValidateCredentials(context.Background(), map[string]string{"api_key": " k3 "}); err != nil {
		t.Fatalf("api_key 校验应通过: %v", err)
	}
	if sawQuery != "k3" || sawAuth != "" {
		t.Fatalf("api_key 校验应走 query: query=%q auth=%q", sawQuery, sawAuth)
	}
	if err := tr.ValidateCredentials(context.Background(), map[string]string{"read_token": " tk4 "}); err != nil {
		t.Fatalf("读令牌校验应通过: %v", err)
	}
	if sawAuth != "Bearer tk4" || sawQuery != "" {
		t.Fatalf("读令牌校验应走 Bearer: query=%q auth=%q", sawQuery, sawAuth)
	}

	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_message":"Invalid API key"}`))
	}))
	defer badSrv.Close()
	trBad := &TMDB{f: newFetcher(badSrv.Client(), 0, "test"), creds: NewCredStore(), baseURL: badSrv.URL}
	if err := trBad.ValidateCredentials(context.Background(), map[string]string{"api_key": "bad"}); err == nil {
		t.Fatal("无效凭据应报错")
	}
	if err := tr.ValidateCredentials(context.Background(), map[string]string{}); err == nil {
		t.Fatal("空凭据应直接拒绝")
	}
}
