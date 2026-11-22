package registry

import "testing"

// 登记表是词表正源：站点分类标签、源原生标签与抓取能力清单的 invariant
// 在这里钉住——改词表先过这组机器检查。
func TestKindVocab(t *testing.T) {
	if len(KindOrder) != len(KindLabels) {
		t.Fatalf("KindOrder 与 KindLabels 数量不一致: %d vs %d", len(KindOrder), len(KindLabels))
	}
	for _, k := range KindOrder {
		if KindLabels[k] == "" {
			t.Errorf("分类 %s 缺中文标签", k)
		}
		if KindHints[k] == "" {
			t.Errorf("分类 %s 缺副文案", k)
		}
	}
}

func TestNativeVocabUntouchable(t *testing.T) {
	// 源原生词表站点无权改名：bgm 类型 6 的原生标签是「三次元」，
	// 站点层只是把它归入剧集类目（两种表达并存，互不覆盖）。
	s, ok := Get(SourceBGM)
	if !ok {
		t.Fatal("缺原生索引行")
	}
	if s.Types[TypeReal] != "三次元" {
		t.Errorf("bgm 类型 6 原生标签被改动: %q", s.Types[TypeReal])
	}
	if s.Kinds[TypeReal] != KindTV {
		t.Errorf("bgm 类型 6 应归入剧集类目: %q", s.Kinds[TypeReal])
	}
	if KindLabel(KindTV) != "剧集" {
		t.Errorf("站点剧集类目标签: %q", KindLabel(KindTV))
	}
}

func TestModeDeclarations(t *testing.T) {
	// 每个登记过的源都要声明抓取模式；原生索引三模式，扩展源 harvest 系。
	want := map[string][]string{
		SourceBGM:     {"full", "incremental", "refresh"},
		SourceAniList: {"harvest", "harvest_incremental"},
		SourceMAL:     {"harvest", "harvest_incremental"},
		SourceTMDB:    {"harvest", "harvest_incremental"},
		SourceITunes:  {"harvest"},
	}
	for id, modes := range want {
		if ModesOf(id) == nil {
			t.Errorf("来源 %s 未声明抓取模式", id)
			continue
		}
		for _, m := range modes {
			if !HasMode(id, m) {
				t.Errorf("来源 %s 缺模式 %s", id, m)
			}
			if ModeLabels[m] == "" {
				t.Errorf("模式 %s 缺按钮文案", m)
			}
		}
	}
	if HasMode(SourceITunes, "harvest_incremental") {
		t.Error("iTunes 无增量通道，不应声明 harvest_incremental")
	}
	if HasMode("unknown", "full") {
		t.Error("未登记来源不应支持任何模式")
	}
}
