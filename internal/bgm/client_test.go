package bgm

import (
	"encoding/json"
	"testing"
)

// TestAliases 信息栏「别名」提取：{v} 列表与字符串两种形态、去空白去重、无键返回空。
// 别名常含英文名/罗马字（鬼滅の刃 的别名带 "Demon Slayer: Kimetsu no Yaiba"），
// 是扩展源标题启发式命中的关键证据面。
func TestAliases(t *testing.T) {
	var s Subject

	withList := []byte(`{
		"id": 245665, "type": 2, "name": "鬼滅の刃",
		"infobox": [
			{"key": "中文名", "value": "鬼灭之刃"},
			{"key": "别名", "value": [
				{"v": "Demon Slayer: Kimetsu no Yaiba"},
				{"v": "鬼滅の刃 竈門炭治郎 立志編"},
				{"v": ""},
				{"v": "Demon Slayer: Kimetsu no Yaiba"}
			]},
			{"key": "放送开始", "value": "2019年4月6日"}
		]
	}`)
	if err := json.Unmarshal(withList, &s); err != nil {
		t.Fatal(err)
	}
	aliases := s.Aliases()
	want := []string{"Demon Slayer: Kimetsu no Yaiba", "鬼滅の刃 竈門炭治郎 立志編"}
	if len(aliases) != len(want) {
		t.Fatalf("别名应去空去重得到 %v: %v", want, aliases)
	}
	for i := range want {
		if aliases[i] != want[i] {
			t.Fatalf("别名[%d] 应为 %q: %q", i, want[i], aliases[i])
		}
	}

	withString := []byte(`{"id": 1, "infobox": [{"key": "别名", "value": "K-ON!"}]}`)
	if err := json.Unmarshal(withString, &s); err != nil {
		t.Fatal(err)
	}
	if got := s.Aliases(); len(got) != 1 || got[0] != "K-ON!" {
		t.Fatalf("字符串形态别名应提取: %v", got)
	}

	noAlias := []byte(`{"id": 1, "infobox": [{"key": "话数", "value": "13"}]}`)
	if err := json.Unmarshal(noAlias, &s); err != nil {
		t.Fatal(err)
	}
	if got := s.Aliases(); got != nil {
		t.Fatalf("无别名键应返回空: %v", got)
	}
}
