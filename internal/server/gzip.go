package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"anipick/internal/db"
)

// gzip 相关常量：过小的响应不值得压缩（gzip 头尾开销可能超过收益）。
const minCompressSize = 860

var gzipPool = sync.Pool{
	New: func() any { w := gzip.NewWriter(io.Discard); return w },
}

// gzAssetCache 缓存静态产物的压缩结果：产物随二进制不可变，压缩一次永久复用。
// 弱 CPU 机器上 471KB 主包压一次要 10~30ms，每次回源都压是纯浪费。
var gzAssetCache sync.Map

// assetCompressibleExt 列出可从预压缩缓存受益的静态扩展名。
var assetCompressibleExt = map[string]bool{
	".js": true, ".css": true, ".html": true, ".svg": true,
	".txt": true, ".xml": true, ".json": true, ".map": true,
}

// serveGzippedAsset 用缓存好的 gzip 字节直接应答静态产物；未命中条件返回 false 走原路。
func (s *Server) serveGzippedAsset(w http.ResponseWriter, r *http.Request, name string) bool {
	if r.Method != http.MethodGet || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return false
	}
	ext := path.Ext(name)
	if !assetCompressibleExt[ext] {
		return false
	}
	var gz []byte
	if v, ok := gzAssetCache.Load(name); ok {
		gz = v.([]byte)
	} else {
		raw, err := fs.ReadFile(s.static, name)
		if err != nil {
			return false
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(raw); err != nil {
			return false
		}
		if err := zw.Close(); err != nil {
			return false
		}
		gz = buf.Bytes()
		gzAssetCache.Store(name, gz)
	}
	h := w.Header()
	if ct := mime.TypeByExtension(ext); ct != "" {
		h.Set("Content-Type", ct)
	}
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(len(gz)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(gz)
	return true
}

// compressibleType 判断 Content-Type 是否值得压缩。
// 已压缩格式（图片、字体、视频）与未知类型一律跳过，避免浪费 CPU。
func compressibleType(ct string) bool {
	if ct == "" {
		return false
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/javascript", "application/xml",
		"application/rss+xml", "image/svg+xml":
		return true
	}
	return false
}

func acceptsGzip(ae string) bool {
	for _, part := range strings.Split(ae, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "gzip" || strings.HasPrefix(part, "gzip;") {
			return true
		}
	}
	return false
}

// gzipWriter 包装 ResponseWriter，在首次写入时按需启用 gzip。
// 压缩决策基于已设置的响应头（Content-Type / Content-Length / Content-Range），
// 因此与下游是文件服务还是 JSON 编码无关。
type gzipWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	trace       *db.RequestTrace
	wroteHeader bool
	enabled     bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	switch {
	case h.Get("Content-Encoding") != "":
		// 下游已自行压缩，不叠加
	case h.Get("Content-Range") != "":
		// 范围响应压缩会破坏 Range 语义
	case code == http.StatusNoContent || code == http.StatusNotModified:
	case !compressibleType(h.Get("Content-Type")):
	case h.Get("Content-Length") != "" && smallBody(h.Get("Content-Length")):
	default:
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		g.enabled = true
	}
	g.ResponseWriter.WriteHeader(code)
}

// smallBody 判断声明的响应体是否小于压缩阈值。
func smallBody(contentLength string) bool {
	n, err := strconv.Atoi(contentLength)
	return err == nil && n < minCompressSize
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if !g.enabled {
		return g.ResponseWriter.Write(b)
	}
	if g.gz == nil {
		g.gz = gzipPool.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
	if g.trace != nil {
		t0 := time.Now()
		n, err := g.gz.Write(b)
		g.trace.Gzip.Add(int64(time.Since(t0)))
		return n, err
	}
	return g.gz.Write(b)
}

// Flush 透传给底层（gzip 流同步冲刷），保证流式响应不被吞。
func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// gzipResponse 为可压缩响应启用 gzip。挂在中间件链最内层，
// 让日志与限流看到的是原始请求/最终状态码。
func (s *Server) gzipResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 响应随 Accept-Encoding 变化，对缓存层（含 Cloudflare）声明 Vary
		w.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w, trace: db.TraceFrom(r.Context())}
		defer func() {
			if gw.gz != nil {
				// Close 冲刷剩余字节与校验尾；panic 时也保证流完整
				if gw.trace != nil {
					t0 := time.Now()
					_ = gw.gz.Close()
					gw.trace.Gzip.Add(int64(time.Since(t0)))
				} else {
					_ = gw.gz.Close()
				}
				gw.gz.Reset(io.Discard)
				gzipPool.Put(gw.gz)
			}
		}()
		next.ServeHTTP(gw, r)
	})
}
