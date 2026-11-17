package netx

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestResolveCachesAndParses(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/dns-json")
		switch r.URL.Query().Get("type") {
		case "1":
			fmt.Fprint(w, `{"Status":0,"Answer":[{"name":"example.test","type":1,"TTL":120,"data":"1.2.3.4"}]}`)
		case "28":
			fmt.Fprint(w, `{"Status":0,"Answer":[{"name":"example.test","type":28,"TTL":120,"data":"2001:db8::1"}]}`)
		default:
			fmt.Fprint(w, `{"Status":0,"Answer":[]}`)
		}
	}))
	defer srv.Close()

	r := NewResolver([]string{srv.URL}, true)
	addrs, err := r.Resolve(context.Background(), "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 2 {
		t.Fatalf("解析结果 = %v", addrs)
	}
	if addrs[0].String() != "1.2.3.4" && addrs[1].String() != "1.2.3.4" {
		t.Fatalf("缺少 IPv4: %v", addrs)
	}

	before := atomic.LoadInt32(&hits)
	if _, err := r.Resolve(context.Background(), "example.test"); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hits) != before {
		t.Fatal("第二次解析未命中缓存")
	}
}

func TestECHConfigParsing(t *testing.T) {
	raw := []byte{0, 1, 2, 3, 4}
	encoded := base64.RawStdEncoding.EncodeToString(raw)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/dns-json")
		fmt.Fprintf(w, `{"Status":0,"Answer":[{"type":65,"TTL":300,"data":"1 . alpn=h2 ipv4hint=1.2.3.4 ech=%s"}]}`, encoded)
	}))
	defer srv.Close()

	r := NewResolver([]string{srv.URL}, true)
	got := r.ECHConfig(context.Background(), "example.test")
	if string(got) != string(raw) {
		t.Fatalf("ECH 配置 = %v, 期望 %v", got, raw)
	}
	if got := r.ECHConfig(context.Background(), "example.test"); string(got) != string(raw) {
		t.Fatal("ECH 缓存结果异常")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("ECH 未命中缓存，请求数 = %d", hits)
	}
}

func TestECHDIsabled(t *testing.T) {
	r := NewResolver([]string{"https://127.0.0.1:1/dns-query"}, false)
	if got := r.ECHConfig(context.Background(), "api.bgm.tv"); got != nil {
		t.Fatalf("关闭 ECH 后仍返回配置: %v", got)
	}
}

func TestSvcParam(t *testing.T) {
	data := `1 . alpn="h2,h3" ipv4hint=1.1.1.1,2.2.2.2 ech="abc="`
	if got := svcParam(data, "ech"); got != "abc=" {
		t.Fatalf("ech = %q", got)
	}
	if got := svcParam(data, "alpn"); got != "h2,h3" {
		t.Fatalf("alpn = %q", got)
	}
	if got := svcParam(data, "missing"); got != "" {
		t.Fatalf("missing = %q", got)
	}
}

func TestDiagnoseReportsNoECH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dns-json")
		switch r.URL.Query().Get("type") {
		case "1":
			fmt.Fprint(w, `{"Status":0,"Answer":[{"type":1,"TTL":60,"data":"104.26.8.23"}]}`)
		case "28":
			fmt.Fprint(w, `{"Status":0,"Answer":[]}`)
		default:
			fmt.Fprint(w, `{"Status":0,"Answer":[{"type":65,"TTL":60,"data":"1 . alpn=h2"}]}`)
		}
	}))
	defer srv.Close()
	r := NewResolver([]string{srv.URL}, true)
	d := r.Diagnose(context.Background(), "api.bgm.tv")
	if d.Error != "" || len(d.Addresses) != 1 || d.ECHAdvertised {
		t.Fatalf("诊断结果异常: %+v", d)
	}
}
