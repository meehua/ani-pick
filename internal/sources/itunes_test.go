package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anipick/internal/store"
)

func TestITunesRSSConvert(t *testing.T) {
	i := NewITunes(nil)
	rec := i.convert(itunesEntry{
		ID: struct {
			Attributes struct {
				IMID string `json:"im:id"`
			} `json:"attributes"`
		}{Attributes: struct {
			IMID string `json:"im:id"`
		}{IMID: "521035514"}},
		Name:    itunesLabel{Label: "飢餓遊戲 The Hunger Games"},
		Artist:  itunesLabel{Label: "Gary Ross"},
		Summary: itunesLabel{Label: "剧情简介"},
		Release: itunesLabel{Label: "2012-03-23T00:00:00-07:00"},
		Images: []itunesLabel{
			{Label: "https://is1-ssl.mzstatic.com/img/40x60bb.png"},
			{Label: "https://is1-ssl.mzstatic.com/img/115x170bb.png"},
		},
		Category: struct {
			Attributes struct {
				Label string `json:"label"`
			} `json:"attributes"`
		}{Attributes: struct {
			Label string `json:"label"`
		}{Label: "動作與歷險"}},
	})
	if rec.Source != store.SourceITunes || rec.SourceRef != "521035514" || rec.Type != store.TypeMovie {
		t.Fatalf("身份/类型异常: %s/%s type=%d", rec.Source, rec.SourceRef, rec.Type)
	}
	if rec.Name != "飢餓遊戲 The Hunger Games" || rec.Date != "2012-03-23" {
		t.Fatalf("标题/日期异常: %q %q", rec.Name, rec.Date)
	}
	if rec.Image != "https://is1-ssl.mzstatic.com/img/500x750bb.png" {
		t.Fatalf("封面应取最大尺寸并升级: %q", rec.Image)
	}
	if len(rec.Tags) != 1 || rec.Tags[0].Name != "動作與歷險" {
		t.Fatalf("标签应为分类名: %+v", rec.Tags)
	}
	if rec.Score != 0 || rec.RatingTotal != 0 || rec.TagsTotal != 0 {
		t.Fatalf("榜单无评分字段, 应全 0: %+v", rec)
	}
}

func TestITunesFeedHarvestConvert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/rss/topmovies/limit=100/json") {
			t.Errorf("路径异常 %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"feed":{"entry":[
			{"id":{"attributes":{"im:id":"521035514"}},"im:name":{"label":"飢餓遊戲"},
			 "im:artist":{"label":"Gary Ross"},"im:releaseDate":{"label":"2012-03-23T00:00:00-07:00"},
			 "im:image":[{"label":"https://is1-ssl.mzstatic.com/img/100x100bb.jpg"}],
			 "category":{"attributes":{"label":"動作與歷險"}}},
			{"id":{"attributes":{"im:id":"6822007631"}},"im:name":{"label":"奧德賽"},
			 "im:releaseDate":{"label":"2026-07-17T00:00:00-07:00"},
			 "im:image":[{"label":"https://is1-ssl.mzstatic.com/img/115x170bb.png"}],
			 "category":{"attributes":{"label":"動作歷險"}}}
		]}}`))
	}))
	defer srv.Close()
	i := &ITunes{f: newFetcher(srv.Client(), 0, "test"), baseURL: srv.URL}
	entries, err := i.topMovies(&Scope{Ctx: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("榜单条目数 = %d", len(entries))
	}
	rec := i.convert(entries[0])
	if rec.SourceRef != "521035514" || rec.Image != "https://is1-ssl.mzstatic.com/img/500x750bb.jpg" {
		t.Fatalf("归一化异常: %+v", rec)
	}
}

func TestITunesLookupDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" {
			t.Errorf("路径异常 %s", r.URL.Path)
		}
		if r.URL.Query().Get("country") != "tw" {
			t.Errorf("lookup 地区应 tw: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"resultCount":1,"results":[{
			"trackId":6822007631,"trackName":"奧德賽","artistName":"Christopher Nolan",
			"primaryGenreName":"動作與歷險","releaseDate":"2026-07-17T07:00:00Z",
			"contentAdvisoryRating":"輔12級","longDescription":"<p>剧情描述</p>",
			"trackTimeMillis":10356750,
			"trackViewUrl":"https://itunes.apple.com/tw/movie/id6822007631",
			"artworkUrl100":"https://is1-ssl.mzstatic.com/img/100x100bb.jpg","kind":"feature-movie"
		}]}`))
	}))
	defer srv.Close()
	i := &ITunes{f: newFetcher(srv.Client(), 0, "test"), baseURL: srv.URL}

	rec, err := i.FetchRecord(context.Background(), "6822007631")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "奧德賽" || rec.Date != "2026-07-17" || rec.Type != store.TypeMovie {
		t.Fatalf("FetchRecord 异常: %+v", rec)
	}
	if rec.NSFW {
		t.Fatal("輔12級不应判敏感")
	}

	// 简介随抓取入库（详情只读库）；外链由登记表模板拼，不入库
	if rec.Summary != "剧情描述" {
		t.Fatalf("简介异常: %q", rec.Summary)
	}
	if _, err := i.FetchRecord(context.Background(), "0"); err == nil {
		t.Fatal("非法 id 应报错")
	}
}

func TestITunesLookupEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resultCount":0,"results":[]}`))
	}))
	defer srv.Close()
	i := &ITunes{f: newFetcher(srv.Client(), 0, "test"), baseURL: srv.URL}
	if _, err := i.FetchRecord(context.Background(), "485191790"); err != ErrNotFound {
		t.Fatalf("lookup 0 结果应映射 ErrNotFound, 实际 %v", err)
	}
}

func TestITunesAdvisoryNSFW(t *testing.T) {
	if !advisoryNSFW.MatchString("限制級") {
		t.Fatal("限制級应判敏感")
	}
	if advisoryNSFW.MatchString("普遍級") {
		t.Fatal("普遍級不应判敏感")
	}
}
