// Package store 封装 AniPick 的 PostgreSQL 访问：条目索引、标签、设置、抓取记录与管理会话。
package store

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"anipick/internal/secretbox"
)

var ErrNotFound = errors.New("记录不存在")

type Store struct {
	Pool *pgxpool.Pool
	Box  *secretbox.Box
}

func New(pool *pgxpool.Pool, box *secretbox.Box) *Store {
	return &Store{Pool: pool, Box: box}
}

// TagVote 是某个标签在单个条目中的票数与占比（百分比，0-100）。
type TagVote struct {
	Name  string  `json:"name"`
	Votes int     `json:"votes"`
	Pct   float64 `json:"pct"`
}

// Subject 是索引里的条目主信息；不含简介/角色等详情（详情由前端直连 Bangumi 获取）。
type Subject struct {
	ID          int64     `json:"id"`
	Type        int       `json:"type"`
	Name        string    `json:"name"`
	NameCN      string    `json:"name_cn"`
	Date        string    `json:"date"`
	Platform    string    `json:"platform"`
	Rank        int       `json:"rank"`
	Score       float64   `json:"score"`
	RatingTotal int       `json:"rating_total"`
	Wish        int       `json:"wish"`
	Collect     int       `json:"collect"`
	Doing       int       `json:"doing"`
	OnHold      int       `json:"on_hold"`
	Dropped     int       `json:"dropped"`
	NSFW        bool      `json:"nsfw"`
	MetaTags    []string  `json:"meta_tags"`
	Image       string    `json:"image"`
	TagsTotal   int       `json:"tags_total"`
	Locked      bool      `json:"locked"`
	FetchedAt   time.Time `json:"fetched_at"`
	Tags        []TagVote `json:"tags,omitempty"`
}

// TagStat 是标签候选（按类型热度）。
type TagStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Votes int64  `json:"votes"`
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
