package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Idempotency keys let a client retry a request that creates something (a post,
// an upload, a key) without doing it twice: the first response is stored and a
// repeat with the same key gets that response back. Kept for 24 hours.

const idempotencySchema = `
CREATE TABLE IF NOT EXISTS idempotency (
  key TEXT NOT NULL, key_id INTEGER NOT NULL, method TEXT NOT NULL, path TEXT NOT NULL,
  status INTEGER NOT NULL, body BLOB NOT NULL, content_type TEXT NOT NULL DEFAULT '', at INTEGER NOT NULL,
  PRIMARY KEY (key, key_id));`

// IdemRecord is a stored response.
type IdemRecord struct {
	Method, Path, ContentType string
	Status                    int
	Body                      []byte
}

// IdemGet returns the stored response for a key used by an API key, if any.
func (db *DB) IdemGet(ctx context.Context, key string, keyID int64) (*IdemRecord, error) {
	var r IdemRecord
	err := db.QueryRowContext(ctx, `SELECT method,path,status,body,content_type FROM idempotency WHERE key=? AND key_id=? AND at>?`,
		key, keyID, time.Now().Add(-24*time.Hour).Unix()).Scan(&r.Method, &r.Path, &r.Status, &r.Body, &r.ContentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// IdemPut stores a response and prunes old ones.
func (db *DB) IdemPut(ctx context.Context, key string, keyID int64, r IdemRecord) {
	now := time.Now()
	_, _ = db.ExecContext(ctx, `DELETE FROM idempotency WHERE at<?`, now.Add(-24*time.Hour).Unix())
	_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO idempotency(key,key_id,method,path,status,body,content_type,at) VALUES(?,?,?,?,?,?,?,?)`,
		key, keyID, r.Method, r.Path, r.Status, r.Body, r.ContentType, now.Unix())
}
