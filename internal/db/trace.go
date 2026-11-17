// 请求链路计时：pgx tracer 把每条 SQL 的耗时累加进请求上下文里的累积器，
// 供管理端性能面板展示"链路上各部分耗时"（DB / 压缩 / 其余=处理+编码）。
package db

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

type traceKey struct{}

// RequestTrace 汇聚单个请求的各链路耗时（纳秒）。
// DB 由本包 Tracer 累加，Gzip 由 server 的压缩中间件累加。
type RequestTrace struct {
	DB   atomic.Int64
	Gzip atomic.Int64
}

// MillisDB / MillisGzip 返回毫秒。
func (t *RequestTrace) MillisDB() float64   { return float64(t.DB.Load()) / 1e6 }
func (t *RequestTrace) MillisGzip() float64 { return float64(t.Gzip.Load()) / 1e6 }

// WithTrace 在上下文放置累积器；中间件在请求入口调用。
func WithTrace(ctx context.Context) (context.Context, *RequestTrace) {
	t := &RequestTrace{}
	return context.WithValue(ctx, traceKey{}, t), t
}

// TraceFrom 取出累积器；未注入时返回 nil，调用方需 nil 安全。
func TraceFrom(ctx context.Context) *RequestTrace {
	t, _ := ctx.Value(traceKey{}).(*RequestTrace)
	return t
}

type queryStartKey struct{}

// Tracer 实现 pgx 的 QueryTracer：并发安全（累积器是原子的，多查询并行累加）。
type Tracer struct{}

func (Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, time.Now())
}

func (Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	start, ok := ctx.Value(queryStartKey{}).(time.Time)
	if !ok {
		return
	}
	if t := TraceFrom(ctx); t != nil {
		t.DB.Add(int64(time.Since(start)))
	}
}
