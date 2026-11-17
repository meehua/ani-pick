package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompressibleType(t *testing.T) {
	cases := map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"text/html":                       true,
		"TEXT/PLAIN":                      true,
		"application/javascript":          true,
		"image/svg+xml":                   true,
		"image/png":                       false,
		"font/woff2":                      false,
		"":                                false,
		"application/octet-stream":        false,
	}
	for ct, want := range cases {
		if got := compressibleType(ct); got != want {
			t.Errorf("compressibleType(%q) = %v, want %v", ct, got, want)
		}
	}
}

// 压缩决策矩阵：JSON 压缩、无压缩头直通、Range/已压缩/过小响应跳过。
func TestGzipResponseDecisions(t *testing.T) {
	s := &Server{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(bytes.Repeat([]byte(`{"x":"y"}`), 500))
		case "range":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Range", "bytes 0-99/200")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(make([]byte, 100))
		case "preset":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Encoding", "br")
			_, _ = w.Write(bytes.Repeat([]byte("z"), 2000))
		case "small":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	})
	h := s.gzipResponse(next)

	do := func(url, ae string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if ae != "" {
			req.Header.Set("Accept-Encoding", ae)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := do("/?case=json", "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("可压缩 JSON 应启用 gzip，头=%v", rec.Header())
	}
	rec = do("/?case=json", "")
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("无压缩头的客户端应原样直通")
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal("Vary: Accept-Encoding 必须始终声明（对缓存层）")
	}
	rec = do("/?case=range", "gzip")
	if rec.Header().Get("Content-Encoding") != "" || rec.Code != http.StatusPartialContent {
		t.Fatal("Range 响应不得压缩（破坏 Range 语义）")
	}
	rec = do("/?case=preset", "gzip")
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatal("下游已压缩的响应不得叠加 gzip")
	}
	rec = do("/?case=small", "gzip")
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("声明过小的响应不应压缩")
	}

	// gzip 输出必须可完整解压还原
	rec = do("/?case=json", "gzip")
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	raw, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("读取解压流失败: %v", err)
	}
	// `{"x":"y"}` 共 9 字节 × 500 = 4500（期望值独立于实现手算）
	if len(raw) != 4500 || !bytes.Contains(raw, []byte(`{"x":"y"}`)) {
		t.Fatalf("解压后内容不符: len=%d", len(raw))
	}
}
