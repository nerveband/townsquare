package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Key names one entity that a change touched.
type Key struct {
	Type string `json:"type"` // post, tag, client, target, set, setting
	ID   string `json:"id"`
}

func PostKey(id int64) Key { return Key{"post", strconv.FormatInt(id, 10)} }

type item struct {
	Key
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// Change is one row of history.
type Change struct {
	ID       int64  `json:"id"`
	At       int64  `json:"at"`
	Actor    string `json:"actor"`
	Summary  string `json:"summary"`
	Undone   bool   `json:"undone"`
	Undoable bool   `json:"undoable"`
	Redoable bool   `json:"redoable"`
}

// Tx is what mutation callbacks get: the transaction plus typed helpers.
type Tx struct {
	tx  *sql.Tx
	ctx context.Context
	New []Key // entities created inside the callback
}

func (t *Tx) Post(id int64) (*Post, error) { return getPost(t.ctx, t.tx, id) }
func (t *Tx) PutPost(p *Post) error {
	isNew := p.ID == 0
	if err := putPost(t.ctx, t.tx, p); err != nil {
		return err
	}
	if isNew {
		t.New = append(t.New, PostKey(p.ID))
	}
	return nil
}
func (t *Tx) DeletePost(id int64) error { return deletePost(t.ctx, t.tx, id) }
func (t *Tx) Exec(q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, q, args...)
}
func (t *Tx) Created(k Key) { t.New = append(t.New, k) }

// Mutate runs fn in a transaction and records a history entry with before/after
// snapshots of keys (plus anything fn reports as created), so it can be undone.
func (db *DB) Mutate(ctx context.Context, actor, summary string, keys []Key, fn func(*Tx) error) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	sqlTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer sqlTx.Rollback()
	items := make([]item, 0, len(keys))
	for _, k := range keys {
		b, err := snapshot(ctx, sqlTx, k)
		if err != nil {
			return 0, err
		}
		items = append(items, item{Key: k, Before: b})
	}
	t := &Tx{tx: sqlTx, ctx: ctx}
	if err := fn(t); err != nil {
		return 0, err
	}
	for _, k := range t.New {
		items = append(items, item{Key: k, Before: json.RawMessage("null")})
	}
	for i := range items {
		a, err := snapshot(ctx, sqlTx, items[i].Key)
		if err != nil {
			return 0, err
		}
		items[i].After = a
	}
	raw, _ := json.Marshal(items)
	if _, err := sqlTx.ExecContext(ctx, `UPDATE changes SET redoable=0 WHERE redoable=1`); err != nil {
		return 0, err
	}
	res, err := sqlTx.ExecContext(ctx, `INSERT INTO changes(at,actor,summary,items) VALUES(?,?,?,?)`, time.Now().Unix(), actor, summary, string(raw))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, sqlTx.Commit()
}

// Log records a history entry that can't be undone (e.g. the sender's activity).
func (db *DB) Log(ctx context.Context, actor, summary string) {
	_, _ = db.ExecContext(ctx, `INSERT INTO changes(at,actor,summary,items,undoable) VALUES(?,?,?,'[]',0)`, time.Now().Unix(), actor, summary)
}

var ErrNothing = errors.New("nothing to do")

// Undo reverts the latest undoable change.
// Flip is the result of an undo or redo.
type Flip struct {
	Summary string // what was undone or redone
	Target  int64  // the change that was undone or redone
	Entry   int64  // the new history entry recording the undo or redo
}

func (db *DB) Undo(ctx context.Context, actor string) (Flip, error) {
	return db.flip(ctx, actor, `SELECT id,summary,items FROM changes WHERE undone=0 AND undoable=1 ORDER BY id DESC LIMIT 1`, true)
}

// Redo re-applies the most recently undone change, if nothing new happened since.
func (db *DB) Redo(ctx context.Context, actor string) (Flip, error) {
	return db.flip(ctx, actor, `SELECT id,summary,items FROM changes WHERE undone=1 AND redoable=1 ORDER BY id ASC LIMIT 1`, false)
}

func (db *DB) flip(ctx context.Context, actor, query string, undo bool) (Flip, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Flip{}, err
	}
	defer tx.Rollback()
	var id int64
	var summary, raw string
	if err := tx.QueryRowContext(ctx, query).Scan(&id, &summary, &raw); errors.Is(err, sql.ErrNoRows) {
		return Flip{}, ErrNothing
	} else if err != nil {
		return Flip{}, err
	}
	var items []item
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return Flip{}, err
	}
	if undo {
		for i := len(items) - 1; i >= 0; i-- {
			if err := restore(ctx, tx, items[i].Key, items[i].Before); err != nil {
				return Flip{}, err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE changes SET undone=1, redoable=1 WHERE id=?`, id)
	} else {
		for _, it := range items {
			if err := restore(ctx, tx, it.Key, it.After); err != nil {
				return Flip{}, err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE changes SET undone=0, redoable=0 WHERE id=?`, id)
	}
	if err != nil {
		return Flip{}, err
	}
	// Record the undo/redo itself, so history shows who did it and when.
	verb := map[bool]string{true: "undid: ", false: "redid: "}[undo]
	res, err := tx.ExecContext(ctx, `INSERT INTO changes(at,actor,summary,items,undoable) VALUES(?,?,?,'[]',0)`, time.Now().Unix(), actor, verb+summary)
	if err != nil {
		return Flip{}, err
	}
	entry, _ := res.LastInsertId()
	return Flip{Summary: summary, Target: id, Entry: entry}, tx.Commit()
}

func (db *DB) Changes(ctx context.Context, limit int) []Change {
	out := []Change{}
	rows, err := db.QueryContext(ctx, `SELECT id,at,actor,summary,undone,undoable,redoable FROM changes ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c Change
		_ = rows.Scan(&c.ID, &c.At, &c.Actor, &c.Summary, &c.Undone, &c.Undoable, &c.Redoable)
		out = append(out, c)
	}
	return out
}

// CanUndoRedo reports the summaries of the next undo and redo, if any.
func (db *DB) CanUndoRedo(ctx context.Context) (undo, redo string) {
	_ = db.QueryRowContext(ctx, `SELECT summary FROM changes WHERE undone=0 AND undoable=1 ORDER BY id DESC LIMIT 1`).Scan(&undo)
	_ = db.QueryRowContext(ctx, `SELECT summary FROM changes WHERE undone=1 AND redoable=1 ORDER BY id ASC LIMIT 1`).Scan(&redo)
	return
}

// ---------- snapshot / restore per entity type ----------

func snapshot(ctx context.Context, q querier, k Key) (json.RawMessage, error) {
	var v any
	var err error
	switch k.Type {
	case "post":
		id, _ := strconv.ParseInt(k.ID, 10, 64)
		var p *Post
		p, err = getPost(ctx, q, id)
		if errors.Is(err, ErrNotFound) {
			return json.RawMessage("null"), nil
		}
		v = p
	case "tag":
		var t Tag
		err = q.QueryRowContext(ctx, `SELECT id,name,color FROM tags WHERE id=?`, k.ID).Scan(&t.ID, &t.Name, &t.Color)
		v = t
	case "client":
		var c Client
		err = q.QueryRowContext(ctx, `SELECT id,name,color,quiet_start,quiet_end,timezone FROM clients WHERE id=?`, k.ID).Scan(&c.ID, &c.Name, &c.Color, &c.QuietStart, &c.QuietEnd, &c.Timezone)
		v = c
	case "set":
		var s Set
		var j string
		err = q.QueryRowContext(ctx, `SELECT id,name,client_id,jids FROM target_sets WHERE id=?`, k.ID).Scan(&s.ID, &s.Name, &s.ClientID, &j)
		_ = json.Unmarshal([]byte(j), &s.JIDs)
		v = s
	case "target":
		var t Target
		err = q.QueryRowContext(ctx, `SELECT jid,client_id,allowed,starred FROM targets WHERE jid=?`, k.ID).Scan(&t.JID, &t.ClientID, &t.Allowed, &t.Starred)
		v = t
	case "setting":
		var s string
		err = q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, k.ID).Scan(&s)
		v = s
	default:
		return nil, fmt.Errorf("unknown entity %q", k.Type)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage("null"), nil
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func restore(ctx context.Context, q querier, k Key, raw json.RawMessage) error {
	null := len(raw) == 0 || string(raw) == "null"
	switch k.Type {
	case "post":
		id, _ := strconv.ParseInt(k.ID, 10, 64)
		if null {
			return deletePost(ctx, q, id)
		}
		var p Post
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		updated := p.UpdatedAt
		err := putPost(ctx, q, &p)
		_, _ = q.ExecContext(ctx, `UPDATE posts SET updated_at=? WHERE id=?`, updated, p.ID)
		return err
	case "client":
		if null {
			_, err := q.ExecContext(ctx, `DELETE FROM clients WHERE id=?`, k.ID)
			return err
		}
		var c Client
		_ = json.Unmarshal(raw, &c)
		_, err := q.ExecContext(ctx, `INSERT INTO clients(id,name,color,quiet_start,quiet_end,timezone) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,color=excluded.color,quiet_start=excluded.quiet_start,quiet_end=excluded.quiet_end,timezone=excluded.timezone`,
			c.ID, c.Name, c.Color, c.QuietStart, c.QuietEnd, c.Timezone)
		return err
	case "tag":
		table := "tags"
		if null {
			_, err := q.ExecContext(ctx, `DELETE FROM `+table+` WHERE id=?`, k.ID)
			return err
		}
		var t Tag
		_ = json.Unmarshal(raw, &t)
		_, err := q.ExecContext(ctx, `INSERT INTO `+table+`(id,name,color) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,color=excluded.color`, t.ID, t.Name, t.Color)
		return err
	case "set":
		if null {
			_, err := q.ExecContext(ctx, `DELETE FROM target_sets WHERE id=?`, k.ID)
			return err
		}
		var s Set
		_ = json.Unmarshal(raw, &s)
		j, _ := json.Marshal(nonNilS(s.JIDs))
		_, err := q.ExecContext(ctx, `INSERT INTO target_sets(id,name,client_id,jids) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,client_id=excluded.client_id,jids=excluded.jids`, s.ID, s.Name, s.ClientID, string(j))
		return err
	case "target":
		if null {
			return nil
		}
		var t Target
		_ = json.Unmarshal(raw, &t)
		_, err := q.ExecContext(ctx, `UPDATE targets SET client_id=?,allowed=?,starred=? WHERE jid=?`, t.ClientID, t.Allowed, t.Starred, t.JID)
		return err
	case "setting":
		if null {
			return nil
		}
		var s string
		_ = json.Unmarshal(raw, &s)
		_, err := q.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k.ID, s)
		return err
	}
	return fmt.Errorf("unknown entity %q", k.Type)
}
