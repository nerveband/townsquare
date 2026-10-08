package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const sessionSchema = `
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT, hash TEXT NOT NULL UNIQUE, device TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL, revoked_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS login_tokens (
  hash TEXT PRIMARY KEY, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, used_at INTEGER NOT NULL DEFAULT 0);`

// Session is a signed-in browser. The cookie holds the secret; only its hash is stored.
type Session struct {
	ID        int64  `json:"id"`
	Device    string `json:"device"`
	CreatedAt int64  `json:"created_at"`
	LastSeen  int64  `json:"last_seen"`
	RevokedAt int64  `json:"revoked_at"`
	Current   bool   `json:"current"`
}

func secret(prefix string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// NewLoginToken makes a one-time sign-in token valid for ttl.
func (db *DB) NewLoginToken(ctx context.Context, ttl time.Duration) (string, error) {
	tok, err := secret("wl_")
	if err != nil {
		return "", err
	}
	now := time.Now()
	_, _ = db.ExecContext(ctx, `DELETE FROM login_tokens WHERE expires_at < ?`, now.Add(-24*time.Hour).Unix())
	_, err = db.ExecContext(ctx, `INSERT INTO login_tokens(hash,created_at,expires_at) VALUES(?,?,?)`, hashKey(tok), now.Unix(), now.Add(ttl).Unix())
	return tok, err
}

// LastLoginToken returns when the most recent token was made (for rate limiting).
func (db *DB) LastLoginToken(ctx context.Context) time.Time {
	var at int64
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(created_at),0) FROM login_tokens`).Scan(&at)
	return time.Unix(at, 0)
}

var ErrBadToken = errors.New("this sign-in link is invalid, used or expired; ask for a new one")

// UseLoginToken redeems a token once and returns a new session secret.
func (db *DB) UseLoginToken(ctx context.Context, tok, device string) (string, error) {
	now := time.Now().Unix()
	res, err := db.ExecContext(ctx, `UPDATE login_tokens SET used_at=? WHERE hash=? AND used_at=0 AND expires_at>=?`, now, hashKey(tok), now)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", ErrBadToken
	}
	sec, err := secret("ws_")
	if err != nil {
		return "", err
	}
	if len(device) > 200 {
		device = device[:200]
	}
	_, err = db.ExecContext(ctx, `INSERT INTO sessions(hash,device,created_at,last_seen) VALUES(?,?,?,?)`, hashKey(sec), device, now, now)
	return sec, err
}

// LookupSession returns the session id for a cookie secret, or ErrNotFound.
func (db *DB) LookupSession(ctx context.Context, sec string) (int64, error) {
	var id, last, revoked int64
	err := db.QueryRowContext(ctx, `SELECT id,last_seen,revoked_at FROM sessions WHERE hash=?`, hashKey(sec)).Scan(&id, &last, &revoked)
	if errors.Is(err, sql.ErrNoRows) || revoked != 0 {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if now := time.Now().Unix(); now-last > 300 {
		_, _ = db.ExecContext(ctx, `UPDATE sessions SET last_seen=? WHERE id=?`, now, id)
	}
	return id, nil
}

func (db *DB) Sessions(ctx context.Context) []Session {
	out := []Session{}
	rows, err := db.QueryContext(ctx, `SELECT id,device,created_at,last_seen,revoked_at FROM sessions WHERE revoked_at=0 ORDER BY last_seen DESC`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s Session
		_ = rows.Scan(&s.ID, &s.Device, &s.CreatedAt, &s.LastSeen, &s.RevokedAt)
		out = append(out, s)
	}
	return out
}

func (db *DB) RevokeSession(ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
