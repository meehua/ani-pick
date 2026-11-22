package sources

import (
	"testing"

	"anipick/internal/registry"
	"anipick/internal/store"
)

func TestNormalizeDate(t *testing.T) {
	cases := map[string]string{
		"2024-03-15":               "2024-03-15",
		"2024-03":                  "2024-03-01",
		"2024":                     "2024-01-01",
		"":                         "",
		"2024-13-01":               "", // 非法月
		"  2020-01-02T00:00:00Z  ": "2020-01-02",
		"2026-07-17T07:00:00Z":     "2026-07-17",
	}
	for in, want := range cases {
		if got := NormalizeDate(in); got != want {
			t.Errorf("NormalizeDate(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestStripText(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"<br>第一行<br/>第二行":      "第一行 第二行",
		"<i>斜体</i> 与 &amp; 符号": "斜体 与 & 符号",
		"多个   空格\t与\n换行":       "多个 空格 与 换行",
		"纯文本保持原样":              "纯文本保持原样",
	}
	for in, want := range cases {
		if got := StripText(in); got != want {
			t.Errorf("StripText(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestAnimePlatformWords(t *testing.T) {
	cases := map[string]string{
		"TV":       "TV",
		"tv_short": "TV",
		"MOVIE":    "剧场版",
		"OVA":      "OVA",
		"ONA":      "WEB",
		"SPECIAL":  "其他",
		"MUSIC":    "MAD",
		"未识别的值":    "未识别的值",
	}
	for in, want := range cases {
		if got := registry.AnimePlatform(in); got != want {
			t.Errorf("AnimePlatform(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestTruncateTags(t *testing.T) {
	names := []string{"动作", "", "科幻", "动作", "悬疑", "喜剧", "爱情", "恐怖", "纪录", "战争"}
	tags := truncateTags(names)
	if len(tags) != 8 {
		t.Fatalf("标签数 = %d, 期望 %d", len(tags), 8)
	}
	if tags[0].Name != "动作" || tags[1].Name != "科幻" {
		t.Fatalf("去重/顺序异常: %+v", tags)
	}
	for _, tag := range tags {
		if tag.Votes != 0 || tag.Pct != 0 {
			t.Fatalf("扩展源标签票数/占比应为 0: %+v", tag)
		}
	}
	if got := truncateTags([]string{"", "  "}); len(got) != 0 {
		t.Fatalf("空名标签应被过滤: %+v", got)
	}
}

func TestITunesArtwork(t *testing.T) {
	cases := map[string]string{
		"https://is1-ssl.mzstatic.com/.../115x170bb.png": "https://is1-ssl.mzstatic.com/.../500x750bb.png",
		"https://is1-ssl.mzstatic.com/.../100x100bb.jpg": "https://is1-ssl.mzstatic.com/.../500x750bb.jpg",
		"": "",
	}
	for in, want := range cases {
		if got := itunesArtwork(in); got != want {
			t.Errorf("itunesArtwork(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestValidSourcesRoundtrip(t *testing.T) {
	for _, s := range store.AllSources() {
		if !store.ValidSource(s) {
			t.Errorf("ValidSource(%q) 应为 true", s)
		}
	}
	if store.ValidSource("nope") {
		t.Error("ValidSource(nope) 应为 false")
	}
	if store.SourceLabel(store.SourceTMDB) != "TMDB" {
		t.Errorf("TMDB 展示名异常: %q", store.SourceLabel(store.SourceTMDB))
	}
}
