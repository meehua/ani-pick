package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type adminRecord struct {
	PasswordHash string    `json:"password_hash"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// EnsureAdminPassword 在数据库尚未保存管理员密码时，用配置里的初始密码写入。
func (s *Store) EnsureAdminPassword(ctx context.Context, initial string) error {
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'admin'`).Scan(&raw)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if initial == "" {
		initial = "anipick"
	}
	return s.SetAdminPassword(ctx, initial)
}

func (s *Store) SetAdminPassword(ctx context.Context, plain string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(adminRecord{PasswordHash: string(hash), UpdatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ('admin', $1, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, raw)
	return err
}

func (s *Store) VerifyAdminPassword(ctx context.Context, plain string) (bool, error) {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'admin'`).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var rec adminRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return false, err
	}
	if rec.PasswordHash == "" {
		return false, nil
	}
	return bcrypt.CompareHashAndPassword([]byte(rec.PasswordHash), []byte(plain)) == nil, nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateSession(ctx context.Context, token string, ttl time.Duration) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO admin_sessions (token_hash, expires_at) VALUES ($1, now() + ($2::int * interval '1 second'))
		ON CONFLICT (token_hash) DO UPDATE SET expires_at = EXCLUDED.expires_at, last_seen = now()`,
		HashToken(token), int(ttl.Seconds()))
	return err
}

// 会话校验缓存的参数：TTL 内重复校验零 DB 开销；last_seen 落库节流，消除每请求写放大。
const (
	sessionCacheTTL     = time.Minute
	sessionSeenWriteGap = 10 * time.Minute
	sessionCacheMax     = 512
)

type sessionCacheEntry struct {
	validUntil time.Time
	lastWrite  time.Time
}

// SessionValid 校验管理员会话。有效期在进程内缓存 1 分钟，
// 命中时不查库；last_seen 至多每 10 分钟落一次库（读操作不再产生写 IO）。
func (s *Store) SessionValid(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	hash := HashToken(token)
	now := time.Now()

	s.sessMu.Lock()
	if e, ok := s.sessCache[hash]; ok && now.Before(e.validUntil) {
		s.sessMu.Unlock()
		return true, nil
	}
	if len(s.sessCache) > sessionCacheMax {
		// 顺手清理过期项，缓存只按活跃会话数增长（单管理员场景实际只有个位数）
		for k, e := range s.sessCache {
			if now.After(e.validUntil) {
				delete(s.sessCache, k)
			}
		}
	}
	s.sessMu.Unlock()

	var ok int
	err := s.Pool.QueryRow(ctx, `
		SELECT 1 FROM admin_sessions
		WHERE token_hash = $1 AND expires_at > now()`, hash).Scan(&ok)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.sessMu.Lock()
			delete(s.sessCache, hash)
			s.sessMu.Unlock()
			return false, nil
		}
		return false, err
	}

	needWrite := false
	s.sessMu.Lock()
	e := s.sessCache[hash]
	e.validUntil = now.Add(sessionCacheTTL)
	if e.lastWrite.IsZero() || now.Sub(e.lastWrite) >= sessionSeenWriteGap {
		e.lastWrite = now
		needWrite = true
	}
	s.sessCache[hash] = e
	s.sessMu.Unlock()

	if needWrite {
		// last_seen 仅作运营观测，写失败不影响会话有效性
		_, _ = s.Pool.Exec(ctx, `UPDATE admin_sessions SET last_seen = now() WHERE token_hash = $1`, hash)
	}
	return true, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	hash := HashToken(token)
	s.sessMu.Lock()
	delete(s.sessCache, hash)
	s.sessMu.Unlock()
	_, err := s.Pool.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash = $1`, hash)
	return err
}

func (s *Store) CleanupSessions(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < now()`)
	return err
}

// DeleteAllSessions 用于修改密码后强制所有浏览器重新登录。
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	s.sessMu.Lock()
	s.sessCache = map[string]sessionCacheEntry{}
	s.sessMu.Unlock()
	_, err := s.Pool.Exec(ctx, `DELETE FROM admin_sessions`)
	return err
}
