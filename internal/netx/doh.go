// Package netx 提供 DoH 解析 + ECH 的 HTTP 传输层。
//
// 目的：在系统 DNS 被污染或链路抖动时，仍能可靠访问 api.bgm.tv。
//   - 解析：直接向 IP 化的 DoH 入口（1.1.1.1 / 8.8.8.8）查询 A / AAAA，绕开系统 DNS；
//   - SNI：从 HTTPS RR 读取 ech= 配置；上游发布 ECHConfig 时自动启用 ECH
//     （当前 api.bgm.tv 未发布，启用后无需改代码）。
//
// 注意：DoH 入口默认使用 IP 字面量，因此不依赖系统 DNS。
package netx

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// DNS 解析与缓存的默认值（管理设置保存后 SetTunables 覆盖）。
const (
	dohTimeout      = 6 * time.Second
	minCacheTTL     = 60 * time.Second
	defaultCacheTTL = 300 * time.Second
)

type Resolver struct {
	endpoints []string
	enableECH bool
	client    *http.Client

	mu       sync.RWMutex
	addrs    map[string]addrEntry
	ech      map[string]echEntry
	preferEp int

	// 解析超时与缓存 TTL 的运行期值（默认 = 上方常量；管理设置立刷）。
	timeout  time.Duration
	cacheMin time.Duration
	cacheTTL time.Duration
}

// SetTunables 覆盖解析超时与缓存 TTL（保存设置后调用；零值忽略，缓存下限
// 自动钳到不超过默认 TTL）。
func (r *Resolver) SetTunables(timeoutSec, cacheMinSec, cacheTTLSec int) {
	if timeoutSec > 0 {
		r.timeout = time.Duration(timeoutSec) * time.Second
		r.client.Timeout = r.timeout
	}
	if cacheMinSec > 0 {
		r.cacheMin = time.Duration(cacheMinSec) * time.Second
	}
	if cacheTTLSec > 0 {
		r.cacheTTL = time.Duration(cacheTTLSec) * time.Second
	}
	if r.cacheMin > r.cacheTTL {
		r.cacheMin = r.cacheTTL
	}
}

type addrEntry struct {
	addrs   []netip.Addr
	expires time.Time
}

type echEntry struct {
	list    []byte
	expires time.Time
}

type dohAnswer struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		TTL  int    `json:"TTL"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func NewResolver(endpoints []string, enableECH bool) *Resolver {
	if len(endpoints) == 0 {
		endpoints = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/resolve"}
	}
	return &Resolver{
		endpoints: endpoints,
		enableECH: enableECH,
		timeout:   dohTimeout,
		cacheMin:  minCacheTTL,
		cacheTTL:  defaultCacheTTL,
		client: &http.Client{
			Timeout: dohTimeout,
			Transport: &http.Transport{
				Proxy:               nil,
				ForceAttemptHTTP2:   true,
				TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 5 * time.Second,
			},
		},
		addrs: make(map[string]addrEntry),
		ech:   make(map[string]echEntry),
	}
}

func (r *Resolver) Endpoints() []string { return append([]string(nil), r.endpoints...) }
func (r *Resolver) ECHEnabled() bool    { return r.enableECH }

// Resolve 通过 DoH 解析主机名。返回 IPv4 优先。
func (r *Resolver) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}

	r.mu.RLock()
	if e, ok := r.addrs[host]; ok && time.Now().Before(e.expires) {
		r.mu.RUnlock()
		return e.addrs, nil
	}
	r.mu.RUnlock()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		answers  []netip.Addr
		ttl      = defaultCacheTTL
		firstErr error
	)
	for _, qtype := range []int{1, 28} {
		wg.Add(1)
		go func(qtype int) {
			defer wg.Done()
			resp, err := r.query(ctx, host, qtype)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			if resp.TTL > 0 && resp.TTL < ttl {
				ttl = resp.TTL
			}
			for _, a := range resp.Addrs {
				answers = append(answers, a)
			}
		}(qtype)
	}
	wg.Wait()

	if len(answers) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("DoH 解析 %s 失败: %w", host, firstErr)
		}
		return nil, fmt.Errorf("DoH 解析 %s 无结果", host)
	}
	// IPv4 优先，避免无 IPv6 出口时先撞拨号超时。
	sort.SliceStable(answers, func(i, j int) bool { return answers[i].Is4() && !answers[j].Is4() })
	if ttl < r.cacheMin {
		ttl = r.cacheMin
	}
	r.mu.Lock()
	r.addrs[host] = addrEntry{addrs: answers, expires: time.Now().Add(ttl)}
	r.mu.Unlock()
	return answers, nil
}

type queryResult struct {
	Addrs []netip.Addr
	Raw   []string
	TTL   time.Duration
}

func (r *Resolver) query(ctx context.Context, host string, qtype int) (queryResult, error) {
	var lastErr error
	n := len(r.endpoints)
	for i := 0; i < n; i++ {
		idx := (r.preferEp + i) % n
		ep := r.endpoints[idx]
		res, err := r.queryOne(ctx, ep, host, qtype)
		if err == nil {
			r.mu.Lock()
			r.preferEp = idx
			r.mu.Unlock()
			return res, nil
		}
		lastErr = err
	}
	return queryResult{}, lastErr
}

func (r *Resolver) queryOne(ctx context.Context, endpoint, host string, qtype int) (queryResult, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return queryResult{}, err
	}
	q := u.Query()
	q.Set("name", host)
	q.Set("type", fmt.Sprint(qtype))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return queryResult{}, err
	}
	req.Header.Set("Accept", "application/dns-json")

	resp, err := r.client.Do(req)
	if err != nil {
		return queryResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return queryResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return queryResult{}, fmt.Errorf("%s 返回 %d", endpoint, resp.StatusCode)
	}
	var out dohAnswer
	if err := json.Unmarshal(body, &out); err != nil {
		return queryResult{}, err
	}
	if out.Status != 0 {
		return queryResult{}, fmt.Errorf("DoH 状态码 %d", out.Status)
	}

	res := queryResult{TTL: defaultCacheTTL}
	for _, a := range out.Answer {
		if a.Type != qtype {
			continue
		}
		if a.TTL > 0 && time.Duration(a.TTL)*time.Second < res.TTL {
			res.TTL = time.Duration(a.TTL) * time.Second
		}
		res.Raw = append(res.Raw, a.Data)
		if qtype == 1 || qtype == 28 {
			if ip, err := netip.ParseAddr(a.Data); err == nil {
				res.Addrs = append(res.Addrs, ip)
			}
		}
	}
	if (qtype == 1 || qtype == 28) && len(res.Addrs) == 0 {
		return queryResult{}, fmt.Errorf("%s 无 A/AAAA 记录", host)
	}
	if len(res.Raw) == 0 {
		return queryResult{}, fmt.Errorf("%s 无 type=%d 记录", host, qtype)
	}
	return res, nil
}

// ECHConfig 返回主机当前的 ECHConfigList（没有则返回 nil）。
func (r *Resolver) ECHConfig(ctx context.Context, host string) []byte {
	if !r.enableECH {
		return nil
	}
	r.mu.RLock()
	if e, ok := r.ech[host]; ok && time.Now().Before(e.expires) {
		r.mu.RUnlock()
		return e.list
	}
	r.mu.RUnlock()

	resp, err := r.query(ctx, host, 65)
	if err != nil {
		return nil
	}
	var list []byte
	ttl := resp.TTL
	for _, a := range resp.Raw {
		if v := svcParam(a, "ech"); v != "" {
			if b := decodeBase64(v); len(b) > 0 {
				list = b
			}
		}
	}
	if ttl < r.cacheMin {
		ttl = r.cacheMin
	}
	r.mu.Lock()
	r.ech[host] = echEntry{list: list, expires: time.Now().Add(ttl)}
	r.mu.Unlock()
	return list
}

func svcParam(data, key string) string {
	fields := strings.Fields(data)
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || !strings.EqualFold(k, key) {
			continue
		}
		return strings.Trim(v, `"`)
	}
	return ""
}

func decodeBase64(s string) []byte {
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	return nil
}

// DialTLSContext 先 DoH 解析，再按域名完成 TLS（必要时带 ECH）。
func (r *Resolver) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addrs, err := r.Resolve(ctx, host)
	if err != nil || len(addrs) == 0 {
		// DoH 不可用时退回系统解析，保证服务可用。
		d := &net.Dialer{Timeout: 8 * time.Second}
		conn, derr := d.DialContext(ctx, network, addr)
		if derr != nil {
			return nil, derr
		}
		return r.handshake(ctx, conn, host)
	}

	ech := r.ECHConfig(ctx, host)
	var lastErr error
	for _, ip := range addrs {
		target := net.JoinHostPort(ip.String(), port)
		d := &net.Dialer{Timeout: 8 * time.Second}
		conn, derr := d.DialContext(ctx, network, target)
		if derr != nil {
			lastErr = derr
			continue
		}
		tlsConn, herr := r.handshakeWithECH(ctx, conn, host, ech)
		if herr == nil {
			return tlsConn, nil
		}
		_ = conn.Close()
		lastErr = herr

		// 上游声明了 ECH 但拒绝/不支持时，回退普通 TLS，保证可用性。
		if len(ech) > 0 {
			conn2, derr2 := d.DialContext(ctx, network, target)
			if derr2 != nil {
				lastErr = derr2
				continue
			}
			tlsConn2, herr2 := r.handshakeWithECH(ctx, conn2, host, nil)
			if herr2 == nil {
				return tlsConn2, nil
			}
			_ = conn2.Close()
			lastErr = herr2
		}
	}
	return nil, fmt.Errorf("连接 %s 失败: %w", host, lastErr)
}

func (r *Resolver) handshake(ctx context.Context, conn net.Conn, host string) (net.Conn, error) {
	return r.handshakeWithECH(ctx, conn, host, nil)
}

func (r *Resolver) handshakeWithECH(ctx context.Context, conn net.Conn, host string, ech []byte) (net.Conn, error) {
	cfg := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
	if len(ech) > 0 {
		cfg.MinVersion = tls.VersionTLS13
		cfg.EncryptedClientHelloConfigList = ech
	}
	t := tls.Client(conn, cfg)
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := t.HandshakeContext(hctx); err != nil {
		return nil, err
	}
	return t, nil
}

// NewHTTPClient 返回启用 DoH（以及可用时 ECH）的 HTTP 客户端。
func (r *Resolver) NewHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialTLSContext:        r.DialTLSContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 45 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}

type Diagnostics struct {
	Host          string   `json:"host"`
	DoHEnabled    bool     `json:"doh_enabled"`
	ECHEnabled    bool     `json:"ech_enabled"`
	Endpoints     []string `json:"endpoints"`
	Addresses     []string `json:"addresses"`
	ECHAdvertised bool     `json:"ech_advertised"`
	Error         string   `json:"error,omitempty"`
}

func (r *Resolver) Diagnose(ctx context.Context, host string) Diagnostics {
	d := Diagnostics{
		Host:       host,
		DoHEnabled: true,
		ECHEnabled: r.enableECH,
		Endpoints:  r.Endpoints(),
	}
	if addrs, err := r.Resolve(ctx, host); err != nil {
		d.Error = err.Error()
	} else {
		for _, a := range addrs {
			d.Addresses = append(d.Addresses, a.String())
		}
	}
	d.ECHAdvertised = len(r.ECHConfig(ctx, host)) > 0
	return d
}

// NewProxyHTTPClient 返回走显式代理的客户端（代理负责远端 DNS/握手）。
// 仅用于服务端抓取；用户请求不经过服务端。
func NewProxyHTTPClient(proxy *url.URL) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyURL(proxy),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{Transport: tr}
}
