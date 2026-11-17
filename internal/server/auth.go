package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const adminCookie = "anipick_admin"

const sessionTTL = 7 * 24 * time.Hour

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(adminCookie)
		if err != nil || cookie.Value == "" {
			fail(w, http.StatusUnauthorized, "unauthorized", "需要管理员登录", "")
			return
		}
		ok, err := s.store.SessionValid(r.Context(), cookie.Value)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			s.clearAdminCookie(w)
			fail(w, http.StatusUnauthorized, "unauthorized", "登录已过期，请重新登录", "")
			return
		}
		// 写操作校验来源，配合 SameSite=Lax 防跨站请求。
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// 优先用 Sec-Fetch-Site（浏览器会如实标注）；开发代理下 Origin 端口会不同，
			// 因此兜底只比较主机名。
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
				if site != "same-origin" && site != "none" {
					fail(w, http.StatusForbidden, "bad_origin", "请求来源不合法", "")
					return
				}
			} else if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				host := r.Host
				if h, _, e := net.SplitHostPort(host); e == nil {
					host = h
				}
				if err != nil || !strings.EqualFold(u.Hostname(), host) {
					fail(w, http.StatusForbidden, "bad_origin", "请求来源不合法", "")
					return
				}
			}
		}
		next(w, r)
	}
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow("login:"+s.clientIP(r), 0.08, 6) {
		fail(w, http.StatusTooManyRequests, "rate_limited", "尝试过于频繁，请稍后再试", "")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	body.Password = strings.TrimSpace(body.Password)
	if body.Password == "" {
		badRequest(w, "请输入管理员密码")
		return
	}
	ok, err := s.store.VerifyAdminPassword(r.Context(), body.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		time.Sleep(300 * time.Millisecond)
		fail(w, http.StatusUnauthorized, "invalid_password", "密码错误", "")
		return
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		internalError(w, err)
		return
	}
	token := hex.EncodeToString(buf)
	if err := s.store.CreateSession(r.Context(), token, sessionTTL); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminCookie); err == nil && cookie.Value != "" {
		_ = s.store.DeleteSession(r.Context(), cookie.Value)
	}
	s.clearAdminCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func cookieSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) clearAdminCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
