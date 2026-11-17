// Package bgm 是 AniPick 使用的最小 Bangumi API 客户端：
// 只负责列表枚举与单条刷新（仅服务端抓取使用）。
package bgm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	BaseURL = "https://api.bgm.tv"
)

var (
	ErrNotFound     = errors.New("条目不存在或无权访问")
	ErrUnauthorized = errors.New("Bangumi 凭据无效或已过期")
)

type Client struct {
	http   *http.Client
	Token  string
	Cookie string
	UA     string
}

func New(token, cookie, ua string, timeout time.Duration) *Client {
	return NewWithHTTPClient(nil, token, cookie, ua, timeout)
}

// NewWithHTTPClient 允许注入自定义 transport（例如 DoH + ECH），
// 只覆盖超时，连接池与 TLS 配置保持共享。
func NewWithHTTPClient(hc *http.Client, token, cookie, ua string, timeout time.Duration) *Client {
	if ua == "" {
		ua = "AniPick/1.0 (+https://github.com/bangumi; local indexer)"
	}
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	client := hc
	if client == nil {
		client = &http.Client{}
	}
	cp := *client
	cp.Timeout = timeout
	return &Client{
		http:   &cp,
		Token:  token,
		Cookie: cookie,
		UA:     ua,
	}
}

type ImageSet struct {
	Large  string `json:"large"`
	Common string `json:"common"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
	Grid   string `json:"grid"`
}

type Rating struct {
	Rank  int            `json:"rank"`
	Total int            `json:"total"`
	Score float64        `json:"score"`
	Count map[string]int `json:"count"`
}

type Collection struct {
	Wish    int `json:"wish"`
	Collect int `json:"collect"`
	Doing   int `json:"doing"`
	OnHold  int `json:"on_hold"`
	Dropped int `json:"dropped"`
}

type Tag struct {
	Name       string `json:"name"`
	Count      int    `json:"count"`
	TotalCount int    `json:"total_count"`
}

type Subject struct {
	ID            int64      `json:"id"`
	Type          int        `json:"type"`
	Name          string     `json:"name"`
	NameCN        string     `json:"name_cn"`
	Date          string     `json:"date"`
	Platform      string     `json:"platform"`
	Image         string     `json:"image"`
	Images        ImageSet   `json:"images"`
	NSFW          bool       `json:"nsfw"`
	Locked        bool       `json:"locked"`
	MetaTags      []string   `json:"meta_tags"`
	Tags          []Tag      `json:"tags"`
	Rating        Rating     `json:"rating"`
	Collection    Collection `json:"collection"`
	TotalEpisodes int        `json:"total_episodes"`
}

type SubjectList struct {
	Data   []Subject `json:"data"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

func (c *Client) ListSubjects(ctx context.Context, typ int, sort string, limit, offset int) (*SubjectList, error) {
	if sort == "" {
		sort = "date"
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	q := url.Values{}
	q.Set("type", strconv.Itoa(typ))
	q.Set("sort", sort)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))

	var out SubjectList
	if err := c.getJSON(ctx, "/v0/subjects?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Subject(ctx context.Context, id int64) (*Subject, error) {
	var out Subject
	if err := c.getJSON(ctx, "/v0/subjects/"+strconv.FormatInt(id, 10), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// Me 需要有效 token，用于管理端校验凭据。
func (c *Client) Me(ctx context.Context) (*User, error) {
	var out User
	if err := c.getJSON(ctx, "/v0/me", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.UA)
		req.Header.Set("Accept", "application/json")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		if c.Cookie != "" {
			req.Header.Set("Cookie", c.Cookie)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求 Bangumi: %w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			if err := json.Unmarshal(body, dst); err != nil {
				return fmt.Errorf("解析 Bangumi 响应: %w", err)
			}
			return nil
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return ErrUnauthorized
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("Bangumi 返回 %d: %s", resp.StatusCode, snippet(body))
			if wait := retryAfter(resp.Header); wait > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
			continue
		default:
			return fmt.Errorf("Bangumi 返回 %d: %s", resp.StatusCode, snippet(body))
		}
	}
	if lastErr == nil {
		lastErr = errors.New("Bangumi 请求失败")
	}
	return lastErr
}

func retryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n > 30 {
			n = 30
		}
		return time.Duration(n) * time.Second
	}
	return 0
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
