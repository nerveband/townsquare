package store

import (
	"context"
	"time"
)

// Accounts beyond the first WhatsApp and Telegram account. The first ones are
// implicit (account 0) and keep their sessions where they always were; each
// extra account keeps its own session in DATA/accounts/<id>/.

const accountSchema = `
CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT, platform TEXT NOT NULL, label TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL);`

// Account is an extra linked account.
type Account struct {
	ID        int64  `json:"id"`
	Platform  string `json:"platform"` // whatsapp, telegram
	Label     string `json:"label"`
	CreatedAt int64  `json:"created_at"`
}

// Accounts lists the extra accounts, oldest first.
func (db *DB) Accounts(ctx context.Context) []Account {
	out := []Account{}
	rows, err := db.QueryContext(ctx, `SELECT id,platform,label,created_at FROM accounts ORDER BY id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a Account
		if rows.Scan(&a.ID, &a.Platform, &a.Label, &a.CreatedAt) == nil {
			out = append(out, a)
		}
	}
	return out
}

// AddAccount records a new extra account. Ids start at 2 so they never look like the default.
func (db *DB) AddAccount(ctx context.Context, platform, label string) (Account, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, _ = db.ExecContext(ctx, `INSERT INTO sqlite_sequence(name,seq) SELECT 'accounts',1 WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name='accounts')`)
	a := Account{Platform: platform, Label: label, CreatedAt: time.Now().Unix()}
	res, err := db.ExecContext(ctx, `INSERT INTO accounts(platform,label,created_at) VALUES(?,?,?)`, platform, label, a.CreatedAt)
	if err != nil {
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

// RenameAccount changes an account's label.
func (db *DB) RenameAccount(ctx context.Context, id int64, label string) error {
	_, err := db.ExecContext(ctx, `UPDATE accounts SET label=? WHERE id=?`, label, id)
	return err
}

// RemoveAccount forgets an account; its chats are marked gone (posts keep them, sends to them fail clearly).
func (db *DB) RemoveAccount(ctx context.Context, id int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, err := db.ExecContext(ctx, `UPDATE targets SET gone=1 WHERE account=?`, id); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
	return err
}
