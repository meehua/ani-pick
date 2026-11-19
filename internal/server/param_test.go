package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSearchInputValidation 复现：非法日期与含 NUL 的文本曾直接打到 SQL 的
// ::date 转换/文本参数上变成 500。校验前置在触碰缓存与数据库之前，
// bare Server（nil store）即可测 400 分支。
func TestSearchInputValidation(t *testing.T) {
	s := &Server{limits: newLimiter()}
	cases := []struct {
		name  string
		query string
	}{
		{"非法日期", "date_from=zzz"},
		{"不存在的日历日", "date_to=2020-02-30"},
		{"keyword 含 NUL", "keyword=a%00b"},
		{"tags 含 NUL", "tags=a%00b"},
		{"exclude 含 NUL", "exclude=a%00b"},
		{"meta_tags 含 NUL", "meta_tags=a%00b"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/subjects?"+c.query, nil)
		rec := httptest.NewRecorder()
		s.handleSearch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: 应 400，实际 %d", c.name, rec.Code)
		}
	}
}

func TestTagsInputValidation(t *testing.T) {
	s := &Server{limits: newLimiter()}
	req := httptest.NewRequest(http.MethodGet, "/api/tags?q=a%00b", nil)
	rec := httptest.NewRecorder()
	s.handleTags(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("tags q 含 NUL 应 400，实际 %d", rec.Code)
	}
}

func TestAdminSubjectsInputValidation(t *testing.T) {
	s := &Server{limits: newLimiter()}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/subjects?keyword=a%00b", nil)
	rec := httptest.NewRecorder()
	s.handleAdminSubjects(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("管理端搜索 keyword 含 NUL 应 400，实际 %d", rec.Code)
	}
}

// 合法输入不应被误伤：合法日期与普通文本应通过校验（后续打到 store 才需要库）。
// 这里只验证校验函数本身，避免在单测里连库。
func TestValidationHelpers(t *testing.T) {
	if !validText("魔法少女") || !validText("") {
		t.Error("合法文本被误判")
	}
	if validText("a\x00b") {
		t.Error("NUL 未被拦下")
	}
	if validText(string([]byte{0xff, 0xfe})) {
		t.Error("非法 UTF-8 未被拦下")
	}
	if !validDate("2024-01-01") || validDate("2024-13-01") || validDate("zzz") || validDate("2024-1-1") {
		t.Error("日期校验边界不符预期")
	}
}
