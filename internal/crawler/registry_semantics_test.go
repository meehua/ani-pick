package crawler

import (
	"context"
	"testing"

	"anipick/internal/registry"
	"anipick/internal/sources"
	"anipick/internal/store"
)

// fakeHarvester 是登记表语义测试用的最小适配器（只报身份，不做抓取）。
type fakeHarvester struct {
	id string
}

func (f fakeHarvester) Source() string { return f.id }
func (f fakeHarvester) Label() string  { return registry.Label(f.id) }
func (f fakeHarvester) Types() []int {
	return []int{registry.KindCode(f.id, registry.KindAnime)}
}
func (f fakeHarvester) Configured() bool { return true }
func (f fakeHarvester) HarvestFull(*sources.Scope) error {
	return nil
}
func (f fakeHarvester) HarvestIncremental(*sources.Scope) error { return nil }
func (f fakeHarvester) FetchRecord(context.Context, string) (store.Subject, error) {
	return store.Subject{}, nil
}

// TestEnabledHarvesters 设置驱动的抓取分派：停用源被剔除、抓取顺序按
// 设置优先级（登记表默认只在未覆写时生效）。
func TestEnabledHarvesters(t *testing.T) {
	c := New(nil, store.AppSettings{}, nil, []sources.Harvester{
		fakeHarvester{id: registry.SourceAniList},
		fakeHarvester{id: registry.SourceITunes},
		fakeHarvester{id: registry.SourceMAL},
	})

	// 未覆写优先级：登记表默认序 anilist(20) → mal(30)，itunes 停用被剔除
	settings := store.AppSettings{Sources: map[string]store.SourceSetting{
		registry.SourceAniList: {Enabled: true, Priority: 20},
		registry.SourceMAL:     {Enabled: true, Priority: 30},
		registry.SourceITunes:  {Enabled: false, Priority: 50},
	}}
	got := c.enabledHarvesters(settings)
	if len(got) != 2 {
		t.Fatalf("停用源应被剔除，剩 2 个: %d", len(got))
	}
	if got[0].Source() != registry.SourceAniList || got[1].Source() != registry.SourceMAL {
		t.Fatalf("应按默认优先级排序: %s, %s", got[0].Source(), got[1].Source())
	}

	// 覆写优先级：mal 调到 anilist 前面
	override := settings.Sources[registry.SourceMAL]
	override.Priority = 10
	settings.Sources[registry.SourceMAL] = override
	got = c.enabledHarvesters(settings)
	if got[0].Source() != registry.SourceMAL {
		t.Fatalf("设置优先级应生效: 第一个 %s", got[0].Source())
	}
}
