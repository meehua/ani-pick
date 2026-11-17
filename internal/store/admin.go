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

func (s *Store) SessionValid(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE admin_sessions SET last_seen = now()
		WHERE token_hash = $1 AND expires_at > now()`, HashToken(token))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash = $1`, HashToken(token))
	return err
}

func (s *Store) CleanupSessions(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < now()`)
	return err
}

// DeleteAllSessions 用于修改密码后强制所有浏览器重新登录。
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM admin_sessions`)
	return err
}
