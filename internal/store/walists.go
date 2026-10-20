package store

import (
	"context"
	"sort"
)

// WhatsApp lists (Business labels are lists too), read from WhatsApp and never
// edited here. Chat ids are stored in target form (wa@N:... for extra accounts).
// gen marks the last full sync that saw a row, so a resync can drop the rest.
const waListSchema = `
CREATE TABLE IF NOT EXISTS wa_lists (
  account INTEGER NOT NULL, id TEXT NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL,
  color INTEGER NOT NULL DEFAULT 0, ord INTEGER NOT NULL DEFAULT 0, gen INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (account, id));
CREATE TABLE IF NOT EXISTS wa_list_chats (
  account INTEGER NOT NULL, list TEXT NOT NULL, jid TEXT NOT NULL, gen INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (account, list, jid));
`

// WAList is one WhatsApp list with the Townsquare chats in it.
type WAList struct {
	ID      string   `json:"id"`
	Account int64    `json:"account"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // favorites, custom, label
	Color   int      `json:"color"`
	JIDs    []string `json:"jids"`
}

// PutWAList adds, renames or (deleted) removes a list.
func (db *DB) PutWAList(ctx context.Context, account int64, id, name, kind string, color, order int, deleted bool, gen int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if deleted {
		if _, err := db.ExecContext(ctx, `DELETE FROM wa_list_chats WHERE account=? AND list=?`, account, id); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `DELETE FROM wa_lists WHERE account=? AND id=?`, account, id)
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO wa_lists(account,id,name,kind,color,ord,gen) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(account,id) DO UPDATE SET name=excluded.name,kind=excluded.kind,color=excluded.color,ord=excluded.ord,gen=MAX(gen,excluded.gen)`,
		account, id, name, kind, color, order, gen)
	return err
}

// SetWAListChat adds a chat to a list or takes it out.
func (db *DB) SetWAListChat(ctx context.Context, account int64, list, jid string, in bool, gen int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if !in {
		_, err := db.ExecContext(ctx, `DELETE FROM wa_list_chats WHERE account=? AND list=? AND jid=?`, account, list, jid)
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO wa_list_chats(account,list,jid,gen) VALUES(?,?,?,?)
		ON CONFLICT(account,list,jid) DO UPDATE SET gen=MAX(gen,excluded.gen)`, account, list, jid, gen)
	return err
}

// ReplaceWAList sets a list and exactly these chats (WhatsApp sends Favorites whole).
func (db *DB) ReplaceWAList(ctx context.Context, account int64, id, name, kind string, order int, jids []string, gen int64) error {
	if err := db.PutWAList(ctx, account, id, name, kind, 0, order, false, gen); err != nil {
		return err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM wa_list_chats WHERE account=? AND list=?`, account, id); err != nil {
		return err
	}
	for _, j := range jids {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO wa_list_chats(account,list,jid,gen) VALUES(?,?,?,?)`, account, id, j, gen); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SweepWALists drops what a full sync (gen) didn't see: lists and chats WhatsApp no longer has.
func (db *DB) SweepWALists(ctx context.Context, account int64, gen int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, err := db.ExecContext(ctx, `DELETE FROM wa_list_chats WHERE account=? AND gen<?`, account, gen); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM wa_lists WHERE account=? AND gen<?`, account, gen)
	return err
}

// SetPinned marks a chat as pinned (or not) in WhatsApp.
func (db *DB) SetPinned(ctx context.Context, jid string, pinned bool) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.ExecContext(ctx, `UPDATE targets SET pinned=? WHERE jid=?`, pinned, jid)
	return err
}

// WALists lists every WhatsApp list with the chats Townsquare knows (not gone),
// Favorites first, then in WhatsApp's order.
func (db *DB) WALists(ctx context.Context) ([]WAList, error) {
	rows, err := db.QueryContext(ctx, `SELECT account,id,name,kind,color FROM wa_lists ORDER BY account, kind!='favorites', ord, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	out := []WAList{}
	idx := map[[2]any]int{}
	for rows.Next() {
		l := WAList{JIDs: []string{}}
		if err := rows.Scan(&l.Account, &l.ID, &l.Name, &l.Kind, &l.Color); err != nil {
			rows.Close()
			return nil, err
		}
		idx[[2]any{l.Account, l.ID}] = len(out)
		out = append(out, l)
	}
	rows.Close()
	rows, err = db.QueryContext(ctx, `SELECT c.account,c.list,c.jid FROM wa_list_chats c JOIN targets t ON t.jid=c.jid AND t.gone=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var acct int64
		var list, jid string
		if err := rows.Scan(&acct, &list, &jid); err != nil {
			return nil, err
		}
		if i, ok := idx[[2]any{acct, list}]; ok {
			out[i].JIDs = append(out[i].JIDs, jid)
		}
	}
	for i := range out {
		sort.Strings(out[i].JIDs)
	}
	return out, rows.Err()
}
