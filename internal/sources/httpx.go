package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 出站通道参数（重试/退避/限长/超时/UA）原为具名常量，现为运行期设置：
// 默认值在 tunables.go init，管理设置保存后 SetFetchParams 立刷。

// fetcher 是带节流与退避重试的 JSON 出站通道，同一 harvester 内共享一个实例：
// 任意两次请求之间至少间隔 interval（各源按官方/礼貌限速配置，
// mal 750ms/请求（官方未文档化）、anilist 约 90 次/分、itunes 3 秒/请求。
// 出站客户端可运行期更换（后台改出站代理后 SwapOutbound 立刷）。
type fetcher struct {
	client   atomic.Pointer[http.Client]
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

// liveFetchers 登记全部存活的 fetcher：出站代理变更时统一换新客户端；
// fetchersBySource 按来源索引：每源请求间隔设置保存后运行期更新时间槽。
var (
	outboundMu       sync.Mutex
	liveFetchers     []*fetcher
	fetchersBySource = map[string]*fetcher{}
	outboundRoute    atomic.Value // *http.Client（当前出站客户端，新构建的 fetcher 继承）
)

func newFetcher(client *http.Client, interval time.Duration, source string) *fetcher {
	if client == nil {
		client = &http.Client{}
	}
	if cur := currentOutbound(); cur != nil {
		client = cur
	}
	f := &fetcher{interval: interval}
	f.client.Store(client)
	outboundMu.Lock()
	liveFetchers = append(liveFetchers, f)
	fetchersBySource[source] = f
	outboundMu.Unlock()
	return f
}

func currentOutbound() *http.Client {
	v, _ := outboundRoute.Load().(*http.Client)
	return v
}

// SwapOutbound 更换所有 fetcher 的出站客户端（保存出站代理设置后调用）。
func SwapOutbound(client *http.Client) {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	outboundRoute.Store(client)
	for _, f := range liveFetchers {
		f.client.Store(client)
	}
}

// SetInterval 运行期更新某个来源的请求间隔（保存数据源设置后由快照链路调用）。
// 时间槽本来就是动态计算的，改完下一次 wait() 即生效，无需重建客户端。
func SetInterval(source string, interval time.Duration) {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	if f := fetchersBySource[source]; f != nil {
		f.mu.Lock()
		f.interval = interval
		f.mu.Unlock()
	}
}

// wait 保证请求间隔：抢占下一个时间槽后按需等待（并发调用自动串行）。
func (f *fetcher) wait(ctx context.Context) error {
	f.mu.Lock()
	d := time.Until(f.next)
	if d < 0 {
		d = 0
	}
	f.next = time.Now().Add(f.interval)
	f.mu.Unlock()
	if d == 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// getJSONHeaders 与 getJSON 相同，但附加自定义请求头（如 TMDB 的 Bearer 鉴权）。
func (f *fetcher) getJSONHeaders(ctx context.Context, url string, dst any, headers map[string]string) error {
	return f.do(ctx, http.MethodGet, url, nil, dst, headers)
}

func (f *fetcher) getJSON(ctx context.Context, url string, dst any) error {
	return f.do(ctx, http.MethodGet, url, nil, dst, nil)
}

func (f *fetcher) postJSON(ctx context.Context, url string, payload any, dst any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return f.do(ctx, http.MethodPost, url, raw, dst, nil)
}

// do 执行一次 JSON 请求：节流 + 退避重试（429/5xx/网络错误），404 直接返回
// ErrNotFound，其余非 2xx 返回错误。响应体限长读取；headers 为附加请求头
// （如 TMDB Bearer），可为 nil。重试次数/退避/限长/超时/UA 都是运行期设置
// （SetFetchParams 立刷），每请求取一次快照。
func (f *fetcher) do(ctx context.Context, method, url string, payload []byte, dst any, headers map[string]string) error {
	var lastErr error
	var after time.Duration // Retry-After 驱动的本轮重试等待；0 用默认退避
	fp := currentFetchParams()
	for attempt := 0; attempt < fp.attempts; attempt++ {
		if attempt > 0 {
			wait := fp.backoff * time.Duration(attempt*attempt)
			if after > 0 {
				wait = after
			}
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if err := f.wait(ctx); err != nil {
			return err
		}

		rctx, cancel := context.WithTimeout(ctx, fp.budget)
		req, err := http.NewRequestWithContext(rctx, method, url, payloadReader(payload))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("User-Agent", fp.ua)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := f.client.Load().Do(req)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("请求上游失败: %w", err)
			after = 0
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, fp.respLimit))
		resp.Body.Close()
		cancel()
		if readErr != nil {
			lastErr = fmt.Errorf("读取上游响应失败: %w", readErr)
			after = 0
			continue
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if dst == nil {
				return nil
			}
			if err := json.Unmarshal(body, dst); err != nil {
				return fmt.Errorf("解析上游响应: %w", err)
			}
			return nil
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("上游返回 %d: %s", resp.StatusCode, snippet(body))
			after = parseRetryAfter(resp.Header.Get("Retry-After"), fp.retryAfterCap)
			continue
		default:
			return fmt.Errorf("上游返回 %d: %s", resp.StatusCode, snippet(body))
		}
	}
	if lastErr == nil {
		lastErr = errors.New("上游请求失败")
	}
	return lastErr
}

func payloadReader(payload []byte) io.Reader {
	if payload == nil {
		return nil
	}
	return strings.NewReader(string(payload))
}

func parseRetryAfter(v string, cap time.Duration) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	d := time.Duration(n) * time.Second
	if d > cap {
		d = cap
	}
	return d
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= 300 {
		return s
	}
	return s[:300] + "…"
}
