package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testServer() *Server {
	return &Server{
		static: fstest.MapFS{
			"index.html":    &fstest.MapFile{Data: []byte("<html>AniPick</html>")},
			"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
			"theme-boot.js": &fstest.MapFile{Data: []byte("boot")},
		},
		limits: newLimiter(),
	}
}

func TestSPAFallback(t *testing.T) {
	h := testServer().spa()

	req := httptest.NewRequest(http.MethodGet, "/subject/123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "AniPick") {
		t.Fatalf("fallback 未返回 index.html: %q", string(body))
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") == "" {
		t.Fatalf("静态资源响应异常: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺失资源应 404，实际 %d", rec.Code)
	}
}

// TestSPATraversalRejected 编码穿越（%2e%2e/%2f 变体）会绕过 mux 的路径规整
// 落进 spa handler，此前会兜底回 200 index.html；应一律 404。
func TestSPATraversalRejected(t *testing.T) {
	h := testServer().spa()
	for _, target := range []string{
		"/..%2f..%2fetc%2fpasswd",
		"/%2e%2e/%2e%2e/etc/passwd",
		"/assets/..%2f..%2fmain.go",
		"/a/../../etc/passwd",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 应 404，实际 %d", target, rec.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := testServer()
	h := s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, key := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy"} {
		if rec.Header().Get(key) == "" {
			t.Errorf("缺少安全响应头 %s", key)
		}
	}
}
