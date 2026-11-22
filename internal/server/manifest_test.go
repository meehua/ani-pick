package server

import (
	"context"
	"testing"

	"anipick/internal/crawler"
	"anipick/internal/registry"
	"anipick/internal/sources"
	"anipick/internal/store"
)

// fakeHarvester 实现 crawler.Harvester 的最小适配器（只报身份与配置状态）。
type fakeHarvester struct {
	id         string
	configured bool
}

func (f fakeHarvester) Source() string   { return f.id }
func (f fakeHarvester) Label() string    { return registry.Label(f.id) }
func (f fakeHarvester) Types() []int     { return nil }
func (f fakeHarvester) Configured() bool { return f.configured }
func (f fakeHarvester) HarvestFull(*sources.Scope) error {
	return nil
}
func (f fakeHarvester) HarvestIncremental(*sources.Scope) error { return nil }
func (f fakeHarvester) FetchRecord(context.Context, string) (store.Subject, error) {
	return store.Subject{}, nil
}

// TestSourceManifest 来源登记表 manifest 的下发音义：优先序、停用剔除、
// 配置状态与类型词汇都来自登记表 + 设置快照。
func TestSourceManifest(t *testing.T) {
	st := store.New(nil, nil)
	cw := crawler.New(st, store.AppSettings{}, nil, []sources.Harvester{
		fakeHarvester{id: registry.SourceTMDB, configured: false},
	})
	s := &Server{store: st, crawler: cw, limits: newLimiter()}

	// itunes 停用、mal 优先级提到 bgm 之前
	snap := cfgSnapshot{sources: map[string]store.SourceSetting{
		registry.SourceBGM:     {Enabled: true, Priority: 10},
		registry.SourceAniList: {Enabled: true, Priority: 20},
		registry.SourceMAL:     {Enabled: true, Priority: 5},
		registry.SourceTMDB:    {Enabled: true, Priority: 40},
		registry.SourceITunes:  {Enabled: false, Priority: 50},
	}}
	ms := s.sourceManifest(snap)

	ids := make([]string, 0, len(ms))
	byID := map[string]manifestSource{}
	for _, m := range ms {
		ids = append(ids, m.ID)
		byID[m.ID] = m
	}
	if _, ok := byID[registry.SourceITunes]; ok {
		t.Fatal("停用的来源不应进 manifest")
	}
	want := []string{registry.SourceMAL, registry.SourceBGM, registry.SourceAniList, registry.SourceTMDB}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("manifest 应按有效优先级排序: %v", ids)
		}
	}
	bgm := byID[registry.SourceBGM]
	if !bgm.Native || !bgm.Configured {
		t.Fatalf("原生索引声明异常: %+v", bgm)
	}
	// 配置状态来自适配器（fake 未配置）
	if byID[registry.SourceTMDB].Configured {
		t.Fatal("未配置的 TMDB 应报 configured=false")
	}
	if len(bgm.Types) != 5 {
		t.Fatalf("原生索引类型词汇应 5 条: %+v", bgm.Types)
	}
	if bgm.Types[0].Code != registry.TypeAnime || bgm.Types[0].Label == "" {
		t.Fatalf("类型词条应按登记表展示序排列: %+v", bgm.Types)
	}
}
