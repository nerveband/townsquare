// Package store is Townsquare's app database: posts, schedules, targets, tags,
// clients, deliveries and the change log that powers undo, redo and history.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	mu sync.Mutex // serialises writes and undo/redo
}

const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS clients (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, color TEXT NOT NULL DEFAULT '#128C7E');
CREATE TABLE IF NOT EXISTS tags (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, color TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS targets (
  jid TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, parent TEXT NOT NULL DEFAULT '',
  can_send INTEGER NOT NULL DEFAULT 0, members INTEGER NOT NULL DEFAULT 0,
  client_id INTEGER, allowed INTEGER NOT NULL DEFAULT 0, starred INTEGER NOT NULL DEFAULT 0,
  gone INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS target_sets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, client_id INTEGER, jids TEXT NOT NULL DEFAULT '[]');
CREATE TABLE IF NOT EXISTS media (
  id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, name TEXT NOT NULL, path TEXT NOT NULL, mime TEXT NOT NULL,
  width INTEGER, height INTEGER, seconds INTEGER, thumb TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS posts (
  id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL DEFAULT '', caption TEXT NOT NULL DEFAULT '',
  media TEXT NOT NULL DEFAULT '[]', targets TEXT NOT NULL DEFAULT '[]',
  tag_id INTEGER, client_id INTEGER, status TEXT NOT NULL DEFAULT 'draft',
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS schedules (
  id INTEGER PRIMARY KEY AUTOINCREMENT, post_id INTEGER NOT NULL, start TEXT NOT NULL, tz TEXT NOT NULL,
  rrule TEXT NOT NULL DEFAULT '', until TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS schedules_post ON schedules(post_id);
CREATE TABLE IF NOT EXISTS overrides (
  schedule_id INTEGER NOT NULL, occ TEXT NOT NULL, at TEXT NOT NULL DEFAULT '', skipped INTEGER NOT NULL DEFAULT 0,
  caption TEXT, media TEXT, targets TEXT, PRIMARY KEY (schedule_id, occ));
CREATE TABLE IF NOT EXISTS deliveries (
  id INTEGER PRIMARY KEY, post_id INTEGER NOT NULL, schedule_id INTEGER NOT NULL, occ TEXT NOT NULL, jid TEXT NOT NULL,
  state TEXT NOT NULL, wa_id TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', at INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS deliveries_once ON deliveries(schedule_id, occ, jid);
CREATE TABLE IF NOT EXISTS changes (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, actor TEXT NOT NULL, summary TEXT NOT NULL,
  items TEXT NOT NULL, undone INTEGER NOT NULL DEFAULT 0, redoable INTEGER NOT NULL DEFAULT 0, undoable INTEGER NOT NULL DEFAULT 1);
`

var defaults = map[string]string{
	"timezone":       "America/New_York",
	"safe_mode":      "1",
	"gap_min":        "20",
	"gap_max":        "60",
	"daily_cap":      "100",
	"quiet_start":    "22:00",
	"quiet_end":      "07:00",
	"grace_min":      "15",
	"tg_queue_hours": "0",
	"stats_people":   "0",
}

var defaultTags = [][2]string{
	{"Announcements", "#128C7E"}, {"Reminders", "#7C6BF0"}, {"Events", "#D9962B"}, {"Media", "#2E9BD1"}, {"Status", "#1FAF57"},
}

func Open(dataDir string) (*DB, error) {
	dsn := "file:" + filepath.Join(dataDir, "app.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(1)
	if _, err := sdb.Exec(schema + apiKeySchema + sessionSchema + statSchema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	db := &DB{DB: sdb}
	addColumns(sdb, "targets", map[string]string{"platform": "TEXT NOT NULL DEFAULT 'whatsapp'"})
	addColumns(sdb, "clients", map[string]string{
		"quiet_start": "TEXT NOT NULL DEFAULT ''", "quiet_end": "TEXT NOT NULL DEFAULT ''", "timezone": "TEXT NOT NULL DEFAULT ''"})
	if err := migrateIDs(sdb); err != nil {
		return nil, fmt.Errorf("migrate ids: %w", err)
	}
	for k, v := range defaults {
		_, _ = sdb.Exec(`INSERT OR IGNORE INTO settings(key,value) VALUES(?,?)`, k, v)
	}
	var n int
	_ = sdb.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&n)
	if n == 0 {
		for _, t := range defaultTags {
			_, _ = sdb.Exec(`INSERT INTO tags(name,color) VALUES(?,?)`, t[0], t[1])
		}
	}
	return db, nil
}

func (db *DB) Settings(ctx context.Context) map[string]string {
	out := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT key,value FROM settings`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		_ = rows.Scan(&k, &v)
		out[k] = v
	}
	return out
}

func (db *DB) Setting(ctx context.Context, key string) string {
	var v string
	if db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v) != nil {
		return defaults[key]
	}
	return v
}

func (db *DB) SettingInt(ctx context.Context, key string) int {
	n, _ := strconv.Atoi(db.Setting(ctx, key))
	return n
}

// migrateIDs makes ids never reuse: deleted posts live on in history and can be
// restored by undo, so a new post must never take an old id. Older databases are
// rebuilt with AUTOINCREMENT, and every sequence is bumped past ids seen in history.
// maxSafeID keeps ids exact in JavaScript (2^53).
const maxSafeID = 1 << 53

func migrateIDs(db *sql.DB) error {
	tables := []string{"clients", "tags", "target_sets", "media", "posts", "schedules"}
	for _, t := range tables {
		var ddl string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, t).Scan(&ddl); err != nil {
			return err
		}
		if strings.Contains(ddl, "AUTOINCREMENT") {
			continue
		}
		newDDL := strings.Replace(ddl, "id INTEGER PRIMARY KEY,", "id INTEGER PRIMARY KEY AUTOINCREMENT,", 1)
		newDDL = strings.Replace(newDDL, "CREATE TABLE "+t, "CREATE TABLE "+t+"_new", 1)
		stmts := []string{newDDL, "INSERT INTO " + t + "_new SELECT * FROM " + t, "DROP TABLE " + t, "ALTER TABLE " + t + "_new RENAME TO " + t}
		for _, q := range stmts {
			if _, err := db.Exec(q); err != nil {
				return fmt.Errorf("%s: %w", t, err)
			}
		}
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS schedules_post ON schedules(post_id)`)
	// Highest numeric entity id mentioned in history (posts, tags, clients, sets and
	// the schedules inside post snapshots). Target keys are JIDs and must be ignored.
	hi := int64(0)
	rows, err := db.Query(`SELECT items FROM changes`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var raw string
		_ = rows.Scan(&raw)
		var items []struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Before json.RawMessage `json:"before"`
			After  json.RawMessage `json:"after"`
		}
		if json.Unmarshal([]byte(raw), &items) != nil {
			continue
		}
		for _, it := range items {
			if it.Type == "target" || it.Type == "setting" {
				continue
			}
			if n, err := strconv.ParseInt(it.ID, 10, 64); err == nil && n > hi && n < maxSafeID {
				hi = n
			}
			for _, snap := range []json.RawMessage{it.Before, it.After} {
				var p struct{ Schedules []struct{ ID int64 } }
				if it.Type == "post" && json.Unmarshal(snap, &p) == nil {
					for _, sc := range p.Schedules {
						if sc.ID > hi && sc.ID < maxSafeID {
							hi = sc.ID
						}
					}
				}
			}
		}
	}
	rows.Close()
	if err := renumberHugeIDs(db, &hi); err != nil {
		return err
	}
	var ds int64
	_ = db.QueryRow(`SELECT COALESCE(MAX(schedule_id),0) FROM deliveries`).Scan(&ds)
	if ds > hi {
		hi = ds
	}
	for _, t := range tables {
		var cur, max int64
		_ = db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM ` + t).Scan(&max)
		_ = db.QueryRow(`SELECT COALESCE(seq,0) FROM sqlite_sequence WHERE name=?`, t).Scan(&cur)
		want := hi
		if max > want {
			want = max
		}
		if cur >= maxSafeID {
			cur = -1 // repair a sequence pushed out of range by an older migration
		}
		if want != cur && (want > cur || cur == -1) {
			if cur <= 0 {
				_, _ = db.Exec(`DELETE FROM sqlite_sequence WHERE name=?`, t)
				_, _ = db.Exec(`INSERT INTO sqlite_sequence(name,seq) VALUES(?,?)`, t, want)
			} else {
				_, _ = db.Exec(`UPDATE sqlite_sequence SET seq=? WHERE name=?`, want, t)
			}
		}
	}
	return nil
}

// renumberHugeIDs fixes post and schedule ids at or above 2^53 (created by an
// older migration bug) in the history, so undo never restores an id that
// JavaScript can't represent exactly.
func renumberHugeIDs(db *sql.DB, hi *int64) error {
	rows, err := db.Query(`SELECT id, items FROM changes`)
	if err != nil {
		return err
	}
	type row struct {
		id    int64
		items string
	}
	var all []row
	for rows.Next() {
		var r row
		_ = rows.Scan(&r.id, &r.items)
		all = append(all, r)
	}
	rows.Close()
	remap := map[string]int64{}
	next := func(old string) int64 {
		if n, ok := remap[old]; ok {
			return n
		}
		*hi++
		remap[old] = *hi
		return *hi
	}
	for _, r := range all {
		dec := json.NewDecoder(strings.NewReader(r.items))
		dec.UseNumber()
		var items []map[string]any
		if dec.Decode(&items) != nil {
			continue
		}
		for _, it := range items {
			if it["type"] != "post" {
				continue
			}
			if id, _ := it["id"].(string); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil && n >= maxSafeID {
					next(id)
				}
			}
			for _, k := range []string{"before", "after"} {
				snap, _ := it[k].(map[string]any)
				scheds, _ := snap["schedules"].([]any)
				for _, sc := range scheds {
					m, _ := sc.(map[string]any)
					if n, ok := m["id"].(json.Number); ok {
						if v, err := n.Int64(); err == nil && v >= maxSafeID {
							next(n.String())
						}
					}
				}
			}
		}
	}
	if len(remap) == 0 {
		return nil
	}
	for _, r := range all {
		out := r.items
		for old, n := range remap {
			ns := strconv.FormatInt(n, 10)
			out = strings.ReplaceAll(out, `"id":"`+old+`"`, `"id":"`+ns+`"`)
			out = strings.ReplaceAll(out, `"id":`+old+`,`, `"id":`+ns+`,`)
			out = strings.ReplaceAll(out, `"id":`+old+`}`, `"id":`+ns+`}`)
		}
		if out != r.items {
			if _, err := db.Exec(`UPDATE changes SET items=? WHERE id=?`, out, r.id); err != nil {
				return err
			}
		}
	}
	for old, n := range remap {
		_, _ = db.Exec(`UPDATE deliveries SET schedule_id=? WHERE schedule_id=?`, n, old)
		_, _ = db.Exec(`UPDATE deliveries SET post_id=? WHERE post_id=?`, n, old)
	}
	return nil
}

// addColumns adds missing columns (idempotent schema evolution).
func addColumns(db *sql.DB, table string, cols map[string]string) {
	have := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		have[n] = true
	}
	rows.Close()
	for name, ddl := range cols {
		if !have[name] {
			_, _ = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + ddl)
		}
	}
}
