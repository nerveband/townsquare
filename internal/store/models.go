package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type querier interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Client groups posts and chats. Its quiet hours, if set, replace the global ones
// for chats assigned to it (QuietStart == QuietEnd means "no quiet hours").
type Client struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	QuietStart string `json:"quiet_start"` // "HH:MM", "" = use the global setting
	QuietEnd   string `json:"quiet_end"`
	Timezone   string `json:"timezone"` // zone for the quiet hours, "" = the app's zone
}

type Target struct {
	JID      string `json:"jid"`
	Platform string `json:"platform"` // whatsapp, telegram
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Parent   string `json:"parent"`
	CanSend  bool   `json:"can_send"`
	Members  int    `json:"members"`
	ClientID *int64 `json:"client_id"`
	Allowed  bool   `json:"allowed"`
	Starred  bool   `json:"starred"`
	Gone     bool   `json:"gone"`
}

type Set struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	ClientID *int64   `json:"client_id"`
	JIDs     []string `json:"jids"`
}

type Media struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Path    string `json:"-"`
	Mime    string `json:"mime"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Seconds int    `json:"seconds"`
	Thumb   string `json:"-"`
}

type Override struct {
	Occ     string    `json:"occ"`
	At      string    `json:"at,omitempty"` // new local time "YYYY-MM-DDTHH:MM", empty = unchanged
	Skipped bool      `json:"skipped,omitempty"`
	Caption *string   `json:"caption,omitempty"`
	Media   *[]int64  `json:"media,omitempty"`
	Targets *[]string `json:"targets,omitempty"`
}

type Schedule struct {
	ID        int64      `json:"id"`
	Start     string     `json:"start"` // local "YYYY-MM-DDTHH:MM" in TZ
	TZ        string     `json:"tz"`
	RRule     string     `json:"rrule"` // "" = once, else RFC 5545 RRULE body, e.g. FREQ=WEEKLY;BYDAY=WE
	Until     string     `json:"until,omitempty"`
	Overrides []Override `json:"overrides"`
}

type Post struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Caption   string     `json:"caption"`
	Media     []int64    `json:"media"`
	Targets   []string   `json:"targets"`
	TagID     *int64     `json:"tag_id"`
	ClientID  *int64     `json:"client_id"`
	Status    string     `json:"status"` // draft, scheduled, paused, archived
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	Schedules []Schedule `json:"schedules"`
}

var ErrNotFound = errors.New("not found")

// ---------- posts ----------

func getPost(ctx context.Context, q querier, id int64) (*Post, error) {
	p := &Post{}
	var media, targets string
	err := q.QueryRowContext(ctx, `SELECT id,title,caption,media,targets,tag_id,client_id,status,created_at,updated_at FROM posts WHERE id=?`, id).
		Scan(&p.ID, &p.Title, &p.Caption, &media, &targets, &p.TagID, &p.ClientID, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(media), &p.Media)
	_ = json.Unmarshal([]byte(targets), &p.Targets)
	rows, err := q.QueryContext(ctx, `SELECT id,start,tz,rrule,until FROM schedules WHERE post_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s Schedule
		_ = rows.Scan(&s.ID, &s.Start, &s.TZ, &s.RRule, &s.Until)
		s.Overrides = []Override{}
		p.Schedules = append(p.Schedules, s)
	}
	rows.Close()
	for i := range p.Schedules {
		orows, err := q.QueryContext(ctx, `SELECT occ,at,skipped,caption,media,targets FROM overrides WHERE schedule_id=? ORDER BY occ`, p.Schedules[i].ID)
		if err != nil {
			return nil, err
		}
		for orows.Next() {
			var o Override
			var cap, med, tg sql.NullString
			_ = orows.Scan(&o.Occ, &o.At, &o.Skipped, &cap, &med, &tg)
			if cap.Valid {
				o.Caption = &cap.String
			}
			if med.Valid {
				var m []int64
				_ = json.Unmarshal([]byte(med.String), &m)
				o.Media = &m
			}
			if tg.Valid {
				var t []string
				_ = json.Unmarshal([]byte(tg.String), &t)
				o.Targets = &t
			}
			p.Schedules[i].Overrides = append(p.Schedules[i].Overrides, o)
		}
		orows.Close()
	}
	if p.Schedules == nil {
		p.Schedules = []Schedule{}
	}
	if p.Media == nil {
		p.Media = []int64{}
	}
	if p.Targets == nil {
		p.Targets = []string{}
	}
	return p, nil
}

// putPost writes the whole post aggregate, replacing schedules and overrides.
// Schedule IDs are preserved so deliveries keep pointing at the right rows.
func putPost(ctx context.Context, q querier, p *Post) error {
	now := time.Now().Unix()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	media, _ := json.Marshal(nonNilI(p.Media))
	targets, _ := json.Marshal(nonNilS(p.Targets))
	if p.ID == 0 {
		res, err := q.ExecContext(ctx, `INSERT INTO posts(title,caption,media,targets,tag_id,client_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			p.Title, p.Caption, string(media), string(targets), p.TagID, p.ClientID, p.Status, p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
	} else {
		_, err := q.ExecContext(ctx, `INSERT INTO posts(id,title,caption,media,targets,tag_id,client_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title,caption=excluded.caption,media=excluded.media,targets=excluded.targets,
			tag_id=excluded.tag_id,client_id=excluded.client_id,status=excluded.status,updated_at=excluded.updated_at`,
			p.ID, p.Title, p.Caption, string(media), string(targets), p.TagID, p.ClientID, p.Status, p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return err
		}
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM overrides WHERE schedule_id IN (SELECT id FROM schedules WHERE post_id=?)`, p.ID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM schedules WHERE post_id=?`, p.ID); err != nil {
		return err
	}
	for i := range p.Schedules {
		s := &p.Schedules[i]
		if s.ID == 0 {
			res, err := q.ExecContext(ctx, `INSERT INTO schedules(post_id,start,tz,rrule,until) VALUES(?,?,?,?,?)`, p.ID, s.Start, s.TZ, s.RRule, s.Until)
			if err != nil {
				return err
			}
			s.ID, _ = res.LastInsertId()
		} else if _, err := q.ExecContext(ctx, `INSERT INTO schedules(id,post_id,start,tz,rrule,until) VALUES(?,?,?,?,?,?)`, s.ID, p.ID, s.Start, s.TZ, s.RRule, s.Until); err != nil {
			return err
		}
		for _, o := range s.Overrides {
			var med, tg any
			if o.Media != nil {
				b, _ := json.Marshal(*o.Media)
				med = string(b)
			}
			if o.Targets != nil {
				b, _ := json.Marshal(*o.Targets)
				tg = string(b)
			}
			if _, err := q.ExecContext(ctx, `INSERT INTO overrides(schedule_id,occ,at,skipped,caption,media,targets) VALUES(?,?,?,?,?,?,?)`,
				s.ID, o.Occ, o.At, o.Skipped, o.Caption, med, tg); err != nil {
				return err
			}
		}
	}
	return nil
}

func deletePost(ctx context.Context, q querier, id int64) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM overrides WHERE schedule_id IN (SELECT id FROM schedules WHERE post_id=?)`, id); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM schedules WHERE post_id=?`, id); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `DELETE FROM posts WHERE id=?`, id)
	return err
}

func (db *DB) Post(ctx context.Context, id int64) (*Post, error) { return getPost(ctx, db, id) }

// Posts returns all posts with the given statuses (all if none given).
func (db *DB) Posts(ctx context.Context, statuses ...string) ([]*Post, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,status FROM posts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var st string
		_ = rows.Scan(&id, &st)
		if len(statuses) == 0 || contains(statuses, st) {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []*Post{}
	for _, id := range ids {
		p, err := getPost(ctx, db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------- simple entities ----------

func (db *DB) Tags(ctx context.Context) []Tag {
	out := []Tag{}
	rows, err := db.QueryContext(ctx, `SELECT id,name,color FROM tags ORDER BY id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t Tag
		_ = rows.Scan(&t.ID, &t.Name, &t.Color)
		out = append(out, t)
	}
	return out
}

func (db *DB) Clients(ctx context.Context) []Client {
	out := []Client{}
	rows, err := db.QueryContext(ctx, `SELECT id,name,color,quiet_start,quiet_end,timezone FROM clients ORDER BY name`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c Client
		_ = rows.Scan(&c.ID, &c.Name, &c.Color, &c.QuietStart, &c.QuietEnd, &c.Timezone)
		out = append(out, c)
	}
	return out
}

func (db *DB) Sets(ctx context.Context) []Set {
	out := []Set{}
	rows, err := db.QueryContext(ctx, `SELECT id,name,client_id,jids FROM target_sets ORDER BY name`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s Set
		var j string
		_ = rows.Scan(&s.ID, &s.Name, &s.ClientID, &j)
		_ = json.Unmarshal([]byte(j), &s.JIDs)
		out = append(out, s)
	}
	return out
}

func (db *DB) Targets(ctx context.Context) []Target {
	out := []Target{}
	rows, err := db.QueryContext(ctx, `SELECT jid,platform,kind,name,parent,can_send,members,client_id,allowed,starred,gone FROM targets ORDER BY
		CASE kind WHEN 'self' THEN 0 WHEN 'status' THEN 1 WHEN 'announce' THEN 2 WHEN 'channel' THEN 3 WHEN 'group' THEN 4 ELSE 5 END, name COLLATE NOCASE`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t Target
		_ = rows.Scan(&t.JID, &t.Platform, &t.Kind, &t.Name, &t.Parent, &t.CanSend, &t.Members, &t.ClientID, &t.Allowed, &t.Starred, &t.Gone)
		out = append(out, t)
	}
	return out
}

// UpsertTarget adds or updates one chat (used by the Telegram bot as chats appear).
func (db *DB) UpsertTarget(ctx context.Context, platform string, t Target) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.ExecContext(ctx, `INSERT INTO targets(jid,platform,kind,name,parent,can_send,members,updated_at,gone) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(jid) DO UPDATE SET platform=excluded.platform,kind=excluded.kind,name=excluded.name,can_send=excluded.can_send,
		updated_at=excluded.updated_at,gone=excluded.gone`, t.JID, platform, t.Kind, t.Name, t.Parent, t.CanSend, t.Members, time.Now().Unix(), t.Gone)
	return err
}

// UpsertTargets refreshes one platform's chats but keeps local fields (client, allowed,
// starred). Chats of that platform that no longer appear are marked gone.
func (db *DB) UpsertTargets(ctx context.Context, platform string, ts []Target) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `UPDATE targets SET gone=1 WHERE platform=?`, platform); err != nil {
		return err
	}
	for _, t := range ts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO targets(jid,platform,kind,name,parent,can_send,members,updated_at,gone) VALUES(?,?,?,?,?,?,?,?,0)
			ON CONFLICT(jid) DO UPDATE SET platform=excluded.platform,kind=excluded.kind,name=excluded.name,parent=excluded.parent,can_send=excluded.can_send,
			members=excluded.members,updated_at=excluded.updated_at,gone=0`, t.JID, platform, t.Kind, t.Name, t.Parent, t.CanSend, t.Members, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) Media(ctx context.Context, id int64) (*Media, error) {
	m := &Media{}
	err := db.QueryRowContext(ctx, `SELECT id,kind,name,path,mime,COALESCE(width,0),COALESCE(height,0),COALESCE(seconds,0),thumb FROM media WHERE id=?`, id).
		Scan(&m.ID, &m.Kind, &m.Name, &m.Path, &m.Mime, &m.Width, &m.Height, &m.Seconds, &m.Thumb)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (db *DB) AddMedia(ctx context.Context, m *Media) error {
	res, err := db.ExecContext(ctx, `INSERT INTO media(kind,name,path,mime,width,height,seconds,thumb,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		m.Kind, m.Name, m.Path, m.Mime, m.Width, m.Height, m.Seconds, m.Thumb, time.Now().Unix())
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func nonNilI(x []int64) []int64 {
	if x == nil {
		return []int64{}
	}
	return x
}

func nonNilS(x []string) []string {
	if x == nil {
		return []string{}
	}
	return x
}
