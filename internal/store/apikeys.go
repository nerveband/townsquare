package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const apiKeySchema = `
CREATE TABLE IF NOT EXISTS api_keys (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, scope TEXT NOT NULL, prefix TEXT NOT NULL, hash TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL, last_used_at INTEGER NOT NULL DEFAULT 0, revoked_at INTEGER NOT NULL DEFAULT 0);`

// APIKey is a credential for /api/v1. The secret is only shown once, at creation.
type APIKey struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Scope      string `json:"scope"` // read, write, admin
	Prefix     string `json:"prefix"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	RevokedAt  int64  `json:"revoked_at"`
}

var Scopes = map[string]int{"read": 1, "write": 2, "admin": 3}

func hashKey(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// CreateAPIKey returns the stored key and the secret (tsq_...).
func (db *DB) CreateAPIKey(ctx context.Context, name, scope string) (*APIKey, string, error) {
	if _, ok := Scopes[scope]; !ok {
		return nil, "", errors.New("scope must be read, write or admin")
	}
	if name == "" {
		return nil, "", errors.New("name is required")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	secret := "tsq_" + hex.EncodeToString(b)
	k := &APIKey{Name: name, Scope: scope, Prefix: secret[:12], CreatedAt: time.Now().Unix()}
	res, err := db.ExecContext(ctx, `INSERT INTO api_keys(name,scope,prefix,hash,created_at) VALUES(?,?,?,?,?)`, k.Name, k.Scope, k.Prefix, hashKey(secret), k.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	k.ID, _ = res.LastInsertId()
	return k, secret, nil
}

// LookupAPIKey finds an active key by its secret and records the use.
func (db *DB) LookupAPIKey(ctx context.Context, secret string) (*APIKey, error) {
	k := &APIKey{}
	err := db.QueryRowContext(ctx, `SELECT id,name,scope,prefix,created_at,last_used_at,revoked_at FROM api_keys WHERE hash=?`, hashKey(secret)).
		Scan(&k.ID, &k.Name, &k.Scope, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && k.RevokedAt != 0) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if now-k.LastUsedAt > 60 {
		_, _ = db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=?`, now, k.ID)
	}
	return k, nil
}

func (db *DB) APIKeys(ctx context.Context) []APIKey {
	out := []APIKey{}
	rows, err := db.QueryContext(ctx, `SELECT id,name,scope,prefix,created_at,last_used_at,revoked_at FROM api_keys ORDER BY id DESC`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k APIKey
		_ = rows.Scan(&k.ID, &k.Name, &k.Scope, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
		out = append(out, k)
	}
	return out
}

func (db *DB) RevokeAPIKey(ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LatestUndoable returns the id of the change Undo would revert (0 if none).
func (db *DB) LatestUndoable(ctx context.Context) int64 {
	var id int64
	_ = db.QueryRowContext(ctx, `SELECT id FROM changes WHERE undone=0 AND undoable=1 ORDER BY id DESC LIMIT 1`).Scan(&id)
	return id
}
