package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"anipick/internal/crawler"
	"anipick/internal/store"
)

func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	settings, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	runs, err := s.store.RecentRuns(r.Context(), 12)
	if err != nil {
		internalError(w, err)
		return
	}
	stale, _ := s.store.CountStale(r.Context(), settings.Crawl.RefreshTTLDays, settings.Crawl.Types)

	writeJSON(w, http.StatusOK, map[string]any{
		"stats":   stats,
		"crawler": s.crawler.Status(),
		"runs":    runs,
		"stale":   stale,
		"settings": map[string]any{
			"crawl": settings.Crawl,
			"bgm": map[string]any{
				"token_configured":  settings.BGM.Token != "",
				"cookie_configured": settings.BGM.Cookie != "",
			},
		},
	})
}

type adminSettingsPayload struct {
	Crawl store.CrawlSettings `json:"crawl"`
	BGM   struct {
		Token       string `json:"token"`
		Cookie      string `json:"cookie"`
		ClearToken  bool   `json:"clear_token"`
		ClearCookie bool   `json:"clear_cookie"`
	} `json:"bgm"`
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	masked := settings.Mask()
	writeJSON(w, http.StatusOK, map[string]any{
		"crawl": masked.Crawl,
		"bgm": map[string]any{
			"token":             masked.BGM.Token,
			"cookie":            masked.BGM.Cookie,
			"ua":                masked.BGM.UA,
			"token_configured":  settings.BGM.Token != "",
			"cookie_configured": settings.BGM.Cookie != "",
		},
		"updated_at": settings.UpdatedAt,
	})
}

func (s *Server) handleAdminPutSettings(w http.ResponseWriter, r *http.Request) {
	var payload adminSettingsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&payload); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	cur, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	next := cur
	next.Crawl = payload.Crawl

	if payload.BGM.ClearToken {
		next.BGM.Token = ""
	} else if v := strings.TrimSpace(payload.BGM.Token); v != "" && !strings.Contains(v, "•") {
		next.BGM.Token = v
	}
	if payload.BGM.ClearCookie {
		next.BGM.Cookie = ""
	} else if v := strings.TrimSpace(payload.BGM.Cookie); v != "" && !strings.Contains(v, "•") {
		next.BGM.Cookie = v
	}

	if err := s.store.SaveSettings(r.Context(), &next); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated_at": next.UpdatedAt})
}

func (s *Server) handleAdminTestCredential(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.LoadSettings(r.Context(), s.defaults)
	if err != nil {
		internalError(w, err)
		return
	}
	client := s.bgmClient(settings)
	if settings.BGM.Token != "" {
		me, err := client.Me(r.Context())
		if err != nil {
			fail(w, http.StatusBadGateway, "credential_invalid", "凭据校验失败", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "mode": "token", "user_id": me.ID, "username": me.Username, "nickname": me.Nickname,
		})
		return
	}
	sub, err := client.Subject(r.Context(), 2)
	if err != nil {
		fail(w, http.StatusBadGateway, "upstream_unreachable", "无法连接 Bangumi API", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "mode": "anonymous", "sample": sub.Name,
	})
}

func (s *Server) handleAdminStartCrawl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	runID, err := s.crawler.Start(body.Mode, "manual")
	if err != nil {
		if err == crawler.ErrBusy {
			fail(w, http.StatusConflict, "busy", "已有抓取任务在运行", "")
			return
		}
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": runID, "status": s.crawler.Status()})
}

func (s *Server) handleAdminCancelCrawl(w http.ResponseWriter, r *http.Request) {
	if !s.crawler.Cancel() {
		fail(w, http.StatusConflict, "not_running", "当前没有正在运行的抓取任务", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.RecentRuns(r.Context(), parseIntParam(r, "limit", 30))
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *Server) handleAdminRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "无效的任务 ID")
		return
	}
	run, err := s.store.GetRun(r.Context(), id)
	if err == store.ErrNotFound {
		fail(w, http.StatusNotFound, "not_found", "任务不存在", "")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleAdminSubjects(w http.ResponseWriter, r *http.Request) {
	q := store.SearchQuery{
		Keyword:     r.URL.Query().Get("keyword"),
		Types:       parseIntCSV(r, "types"),
		Tags:        parseCSV(r, "tags"),
		ExcludeTags: parseCSV(r, "exclude"),
		TagMode:     r.URL.Query().Get("tag_mode"),
		PctMin:      parseFloatParam(r, "pct_min"),
		MetaTags:    parseCSV(r, "meta_tags"),
		RankMin:     parseIntParam(r, "rank_min", 0),
		RankMax:     parseIntParam(r, "rank_max", 0),
		NSFW:        "include",
		Sort:        r.URL.Query().Get("sort"),
		Order:       r.URL.Query().Get("order"),
		Page:        parseIntParam(r, "page", 1),
		PageSize:    parseIntParam(r, "page_size", 20),
	}
	res, err := s.store.Search(r.Context(), q)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAdminRefreshSubject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	sub, err := s.crawler.RefreshOne(r.Context(), id)
	if err != nil {
		fail(w, http.StatusBadGateway, "refresh_failed", "刷新失败", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleAdminDeleteSubject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "无效的条目 ID")
		return
	}
	if err := s.store.DeleteSubject(r.Context(), id); err != nil {
		if err == store.ErrNotFound {
			fail(w, http.StatusNotFound, "not_found", "条目不存在", "")
			return
		}
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	if len(strings.TrimSpace(body.Next)) < 6 {
		badRequest(w, "新密码至少 6 位")
		return
	}
	ok, err := s.store.VerifyAdminPassword(r.Context(), body.Current)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusUnauthorized, "invalid_password", "当前密码错误", "")
		return
	}
	if err := s.store.SetAdminPassword(r.Context(), strings.TrimSpace(body.Next)); err != nil {
		internalError(w, err)
		return
	}

	// 使旧会话全部失效，并为当前浏览器换发新会话。
	_ = s.store.DeleteAllSessions(r.Context())
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	if err := s.store.CreateSession(r.Context(), token, sessionTTL); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: cookieSecure(r), MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminNetwork(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"doh":   s.resolver != nil,
		"ech":   s.resolver != nil && s.resolver.ECHEnabled(),
		"proxy": s.egressProxy,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if s.resolver != nil {
		out["diagnostics"] = s.resolver.Diagnose(ctx, "api.bgm.tv")
	}
	if s.httpClient != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.bgm.tv/v0/subjects/2", nil)
		if err == nil {
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "AniPick/1.0 (+https://github.com/bangumi; local indexer)")
			if resp, derr := s.httpClient.Do(req); derr == nil {
				out["upstream_ok"] = resp.StatusCode == http.StatusOK
				out["upstream_status"] = resp.StatusCode
				_ = resp.Body.Close()
			} else {
				out["upstream_ok"] = false
				out["upstream_error"] = derr.Error()
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
