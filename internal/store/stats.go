package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Engagement stats. Every sent message is recorded (stat_msgs) and linked to its
// delivery (stat_sends: one post occurrence to one chat). Live events (WhatsApp
// receipts, reactions, replies) land in stat_people, one row per person and kind,
// so repeats are counted once; the totals on stat_sends are recomputed from it.
// Platforms that only give totals (WhatsApp channels, Telegram) are polled and
// their numbers written straight to stat_sends. People are stored as a salted hash;
// names are kept only when the "stats_people" setting is on, and person rows are
// pruned after 30 days (the totals stay).

const statSchema = `
CREATE TABLE IF NOT EXISTS stat_sends (
  delivery_id INTEGER PRIMARY KEY, post_id INTEGER NOT NULL, schedule_id INTEGER NOT NULL, occ TEXT NOT NULL,
  chat TEXT NOT NULL, platform TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'text',
  tag_id INTEGER, client_id INTEGER, members INTEGER NOT NULL DEFAULT 0,
  sched_at INTEGER NOT NULL, sent_at INTEGER NOT NULL,
  delivered INTEGER NOT NULL DEFAULT 0, reads INTEGER NOT NULL DEFAULT 0, played INTEGER NOT NULL DEFAULT 0,
  views INTEGER NOT NULL DEFAULT 0, forwards INTEGER NOT NULL DEFAULT 0, replies INTEGER NOT NULL DEFAULT 0,
  reactions INTEGER NOT NULL DEFAULT 0, emoji TEXT NOT NULL DEFAULT '{}',
  r1h INTEGER, r6h INTEGER, r24h INTEGER, r7d INTEGER,
  polled_at INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS stat_sends_sent ON stat_sends(sent_at);
CREATE INDEX IF NOT EXISTS stat_sends_post ON stat_sends(post_id);
CREATE TABLE IF NOT EXISTS stat_msgs (
  platform TEXT NOT NULL, msg_id TEXT NOT NULL, chat TEXT NOT NULL, server_id INTEGER NOT NULL DEFAULT 0,
  delivery_id INTEGER NOT NULL, kind TEXT NOT NULL DEFAULT 'text', PRIMARY KEY (platform, msg_id, chat));
CREATE INDEX IF NOT EXISTS stat_msgs_id ON stat_msgs(platform, msg_id);
CREATE INDEX IF NOT EXISTS stat_msgs_delivery ON stat_msgs(delivery_id);
CREATE TABLE IF NOT EXISTS stat_people (
  delivery_id INTEGER NOT NULL, person TEXT NOT NULL, what TEXT NOT NULL, emoji TEXT NOT NULL DEFAULT '',
  who TEXT NOT NULL DEFAULT '', at INTEGER NOT NULL, PRIMARY KEY (delivery_id, person, what));
CREATE TABLE IF NOT EXISTS stat_members (chat TEXT NOT NULL, day TEXT NOT NULL, members INTEGER NOT NULL, PRIMARY KEY (chat, day));
CREATE TABLE IF NOT EXISTS stat_insights (chat TEXT PRIMARY KEY, data TEXT NOT NULL, updated_at INTEGER NOT NULL);
`

// StatMsg is one message Townsquare sent (a post with 3 photos is 3 messages).
type StatMsg struct {
	Platform string
	ID       string
	ServerID int64 // WhatsApp channel server id
	Kind     string
	Text     bool // carries the caption or text (not stored with stats)
}

// StatSend describes a delivery for stats.
type StatSend struct {
	PostID, ScheduleID int64
	Occ, Chat          string
	Platform, Kind     string
	TagID, ClientID    *int64
	Members            int
	SchedAt            time.Time
}

// RecordStatSend links a successful delivery and its messages for engagement stats.
func (db *DB) RecordStatSend(ctx context.Context, s StatSend, msgs []StatMsg) {
	if len(msgs) == 0 {
		return
	}
	var did int64
	if db.QueryRowContext(ctx, `SELECT id FROM deliveries WHERE schedule_id=? AND occ=? AND jid=?`, s.ScheduleID, s.Occ, s.Chat).Scan(&did) != nil {
		return
	}
	now := time.Now().Unix()
	_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO stat_sends(delivery_id,post_id,schedule_id,occ,chat,platform,kind,tag_id,client_id,members,sched_at,sent_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, did, s.PostID, s.ScheduleID, s.Occ, s.Chat, s.Platform, s.Kind, s.TagID, s.ClientID, s.Members, s.SchedAt.Unix(), now, now)
	for _, m := range msgs {
		_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO stat_msgs(platform,msg_id,chat,server_id,delivery_id,kind) VALUES(?,?,?,?,?,?)`,
			m.Platform, m.ID, s.Chat, m.ServerID, did, m.Kind)
	}
}

// statSalt is a random per-install value so stored person hashes can't be matched to numbers.
func (db *DB) statSalt(ctx context.Context) string {
	v := db.Setting(ctx, "stats_salt")
	if v != "" {
		return v
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	v = hex.EncodeToString(b)
	_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key,value) VALUES('stats_salt',?)`, v)
	return db.Setting(ctx, "stats_salt")
}

func (db *DB) personHash(ctx context.Context, id string) string {
	h := sha256.Sum256([]byte(db.statSalt(ctx) + "|" + id))
	return hex.EncodeToString(h[:12])
}

// StatEvent records one person's receipt, reaction or reply to a message we sent.
// what: delivered, read, played, react (emoji "" removes it), or reply:<message id>.
// It reports whether the message was one of ours.
func (db *DB) StatEvent(ctx context.Context, platform, msgID, person, who, what, emoji string, at time.Time) bool {
	var did int64
	if db.QueryRowContext(ctx, `SELECT delivery_id FROM stat_msgs WHERE platform=? AND msg_id=? LIMIT 1`, platform, msgID).Scan(&did) != nil {
		return false
	}
	if db.Setting(ctx, "stats_people") != "1" {
		who = ""
	}
	p := db.personHash(ctx, person)
	switch {
	case what == "react" && emoji == "":
		_, _ = db.ExecContext(ctx, `DELETE FROM stat_people WHERE delivery_id=? AND person=? AND what='react'`, did, p)
	case what == "react":
		_, _ = db.ExecContext(ctx, `INSERT INTO stat_people(delivery_id,person,what,emoji,who,at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(delivery_id,person,what) DO UPDATE SET emoji=excluded.emoji, who=excluded.who, at=excluded.at`, did, p, what, emoji, who, at.Unix())
	default:
		// First time wins, so "read at" stays the moment they first read it.
		_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO stat_people(delivery_id,person,what,emoji,who,at) VALUES(?,?,?,?,?,?)`, did, p, what, emoji, who, at.Unix())
		if who != "" {
			_, _ = db.ExecContext(ctx, `UPDATE stat_people SET who=? WHERE delivery_id=? AND person=? AND who=''`, who, did, p)
		}
	}
	db.recount(ctx, did)
	return true
}

// recount refreshes live totals from stat_people. A read implies delivered, a
// voice note play implies read.
func (db *DB) recount(ctx context.Context, did int64) {
	var delivered, reads, played, replies, reactions int
	_ = db.QueryRowContext(ctx, `SELECT
		COUNT(DISTINCT CASE WHEN what IN ('delivered','read','played') THEN person END),
		COUNT(DISTINCT CASE WHEN what IN ('read','played') THEN person END),
		COUNT(DISTINCT CASE WHEN what='played' THEN person END),
		SUM(CASE WHEN what LIKE 'reply:%' THEN 1 ELSE 0 END),
		SUM(CASE WHEN what='react' THEN 1 ELSE 0 END)
		FROM stat_people WHERE delivery_id=?`, did).Scan(&delivered, &reads, &played, &replies, &reactions)
	emoji := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT emoji, COUNT(*) FROM stat_people WHERE delivery_id=? AND what='react' GROUP BY emoji`, did)
	if err == nil {
		for rows.Next() {
			var e string
			var n int
			_ = rows.Scan(&e, &n)
			emoji[e] = n
		}
		rows.Close()
	}
	ej, _ := json.Marshal(emoji)
	// Live counters never go down because of pruning: keep the max of stored and recounted.
	_, _ = db.ExecContext(ctx, `UPDATE stat_sends SET delivered=MAX(delivered,?), reads=MAX(reads,?), played=MAX(played,?),
		replies=MAX(replies,?), reactions=?, emoji=?, updated_at=? WHERE delivery_id=?`,
		delivered, reads, played, replies, reactions, string(ej), time.Now().Unix(), did)
}

// StatPolled stores totals from platforms that report counts (WhatsApp channels, Telegram).
func (db *DB) StatPolled(ctx context.Context, did int64, views, forwards, replies int, emoji map[string]int) {
	n := 0
	for _, c := range emoji {
		n += c
	}
	ej, _ := json.Marshal(emoji)
	now := time.Now().Unix()
	_, _ = db.ExecContext(ctx, `UPDATE stat_sends SET views=MAX(views,?), forwards=MAX(forwards,?), replies=MAX(replies,?),
		reactions=?, emoji=?, polled_at=?, updated_at=? WHERE delivery_id=?`, views, forwards, replies, n, string(ej), now, now, did)
}

// StatMarkPolled records a poll attempt with nothing to update.
func (db *DB) StatMarkPolled(ctx context.Context, did int64) {
	_, _ = db.ExecContext(ctx, `UPDATE stat_sends SET polled_at=? WHERE delivery_id=?`, time.Now().Unix(), did)
}

// PollTarget is a delivery to refresh from a platform that reports totals.
type PollTarget struct {
	DeliveryID int64
	Chat       string
	SentAt     time.Time
	Msgs       []StatMsg
}

// StatsToPoll lists deliveries on polled platforms that are due: every 15 minutes
// for 48 hours, then daily up to 30 days.
func (db *DB) StatsToPoll(ctx context.Context, now time.Time) []PollTarget {
	rows, err := db.QueryContext(ctx, `SELECT s.delivery_id, s.chat, s.sent_at, m.platform, m.msg_id, m.server_id, m.kind
		FROM stat_sends s JOIN stat_msgs m ON m.delivery_id=s.delivery_id
		WHERE s.sent_at > ? AND ((s.platform='telegram' AND s.chat != 'tg:self') OR s.chat LIKE '%@newsletter')
		  AND ((s.sent_at > ? AND s.polled_at < ?) OR s.polled_at < ?)
		ORDER BY s.delivery_id`,
		now.Add(-30*24*time.Hour).Unix(), now.Add(-48*time.Hour).Unix(), now.Add(-15*time.Minute).Unix(), now.Add(-24*time.Hour).Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []PollTarget
	for rows.Next() {
		var did, sent, sid int64
		var chat string
		var m StatMsg
		_ = rows.Scan(&did, &chat, &sent, &m.Platform, &m.ID, &sid, &m.Kind)
		m.ServerID = sid
		if len(out) == 0 || out[len(out)-1].DeliveryID != did {
			out = append(out, PollTarget{DeliveryID: did, Chat: chat, SentAt: time.Unix(sent, 0)})
		}
		out[len(out)-1].Msgs = append(out[len(out)-1].Msgs, m)
	}
	return out
}

// StatSnapshots fills the 1 hour, 6 hour, 24 hour and 7 day reach marks once they pass.
func (db *DB) StatSnapshots(ctx context.Context, now time.Time) {
	for col, d := range map[string]time.Duration{"r1h": time.Hour, "r6h": 6 * time.Hour, "r24h": 24 * time.Hour, "r7d": 7 * 24 * time.Hour} {
		_, _ = db.ExecContext(ctx, `UPDATE stat_sends SET `+col+`=MAX(reads,views) WHERE `+col+` IS NULL AND sent_at <= ?`, now.Add(-d).Unix())
	}
}

// StatMembers saves today's member counts.
func (db *DB) StatMembers(ctx context.Context, day string, members map[string]int) {
	for chat, n := range members {
		_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO stat_members(chat,day,members) VALUES(?,?,?)`, chat, day, n)
	}
}

// StatInsight stores a platform's own channel summary (Telegram broadcast stats).
func (db *DB) StatInsight(ctx context.Context, chat string, data any) {
	b, _ := json.Marshal(data)
	_, _ = db.ExecContext(ctx, `INSERT OR REPLACE INTO stat_insights(chat,data,updated_at) VALUES(?,?,?)`, chat, string(b), time.Now().Unix())
}

// StatPrune drops per-person rows older than 30 days. Totals stay.
func (db *DB) StatPrune(ctx context.Context, now time.Time) {
	_, _ = db.ExecContext(ctx, `DELETE FROM stat_people WHERE at < ?`, now.Add(-30*24*time.Hour).Unix())
}

// StatClearNames removes stored names (used when the "who read it" option is turned off).
func (db *DB) StatClearNames(ctx context.Context) {
	_, _ = db.ExecContext(ctx, `UPDATE stat_people SET who='' WHERE who != ''`)
}

// StatRow is one delivery with its engagement.
type StatRow struct {
	DeliveryID int64          `json:"delivery_id"`
	PostID     int64          `json:"post_id"`
	ScheduleID int64          `json:"schedule_id"`
	Occ        string         `json:"occ"`
	Chat       string         `json:"chat"`
	Platform   string         `json:"platform"`
	Kind       string         `json:"kind"`
	TagID      *int64         `json:"tag_id"`
	ClientID   *int64         `json:"client_id"`
	Members    int            `json:"members"`
	SchedAt    int64          `json:"sched_at"`
	SentAt     int64          `json:"sent_at"`
	Delivered  int            `json:"delivered"`
	Reads      int            `json:"reads"`
	Played     int            `json:"played"`
	Views      int            `json:"views"`
	Forwards   int            `json:"forwards"`
	Replies    int            `json:"replies"`
	Reactions  int            `json:"reactions"`
	Emoji      map[string]int `json:"emoji"`
	R1h        *int           `json:"reach_1h"`
	R6h        *int           `json:"reach_6h"`
	R24h       *int           `json:"reach_24h"`
	R7d        *int           `json:"reach_7d"`
}

// Reach is the number of people who saw it: readers, or views where only views exist.
func (r StatRow) Reach() int {
	if r.Views > r.Reads {
		return r.Views
	}
	return r.Reads
}

// StatRows returns deliveries sent in [from, to). postID 0 means all posts.
func (db *DB) StatRows(ctx context.Context, from, to time.Time, postID int64) []StatRow {
	q := `SELECT delivery_id,post_id,schedule_id,occ,chat,platform,kind,tag_id,client_id,members,sched_at,sent_at,
		delivered,reads,played,views,forwards,replies,reactions,emoji,r1h,r6h,r24h,r7d FROM stat_sends WHERE sent_at >= ? AND sent_at < ?`
	args := []any{from.Unix(), to.Unix()}
	if postID != 0 {
		q += ` AND post_id = ?`
		args = append(args, postID)
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY sent_at`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []StatRow
	for rows.Next() {
		var r StatRow
		var ej string
		var tag, client sql.NullInt64
		var r1, r6, r24, r7 sql.NullInt64
		_ = rows.Scan(&r.DeliveryID, &r.PostID, &r.ScheduleID, &r.Occ, &r.Chat, &r.Platform, &r.Kind, &tag, &client, &r.Members,
			&r.SchedAt, &r.SentAt, &r.Delivered, &r.Reads, &r.Played, &r.Views, &r.Forwards, &r.Replies, &r.Reactions, &ej, &r1, &r6, &r24, &r7)
		if tag.Valid {
			r.TagID = &tag.Int64
		}
		if client.Valid {
			r.ClientID = &client.Int64
		}
		r.R1h, r.R6h, r.R24h, r.R7d = nullInt(r1), nullInt(r6), nullInt(r24), nullInt(r7)
		_ = json.Unmarshal([]byte(ej), &r.Emoji)
		out = append(out, r)
	}
	return out
}

func nullInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// DeliveryLogRow is one delivery outcome (sent, blocked, failed, missed).
type DeliveryLogRow struct {
	PostID int64
	Jid    string
	State  string
	At     int64
}

// DeliveryLog returns delivery outcomes in [from, to).
func (db *DB) DeliveryLog(ctx context.Context, from, to time.Time) []DeliveryLogRow {
	rows, err := db.QueryContext(ctx, `SELECT post_id, jid, state, at FROM deliveries WHERE at >= ? AND at < ?`, from.Unix(), to.Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []DeliveryLogRow
	for rows.Next() {
		var r DeliveryLogRow
		_ = rows.Scan(&r.PostID, &r.Jid, &r.State, &r.At)
		out = append(out, r)
	}
	return out
}

// MemberHistory returns member counts per chat per day in [fromDay, toDay].
func (db *DB) MemberHistory(ctx context.Context, fromDay, toDay string) map[string]map[string]int {
	out := map[string]map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT chat, day, members FROM stat_members WHERE day >= ? AND day <= ?`, fromDay, toDay)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c, d string
		var n int
		_ = rows.Scan(&c, &d, &n)
		if out[c] == nil {
			out[c] = map[string]int{}
		}
		out[c][d] = n
	}
	return out
}

// Insights returns stored platform summaries by chat.
func (db *DB) Insights(ctx context.Context) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	rows, err := db.QueryContext(ctx, `SELECT chat, data FROM stat_insights`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c, d string
		_ = rows.Scan(&c, &d)
		out[c] = json.RawMessage(d)
	}
	return out
}

// StatPerson is one person's interaction (names only when the option is on).
type StatPerson struct {
	DeliveryID int64  `json:"delivery_id"`
	Who        string `json:"who"`
	What       string `json:"what"`
	Emoji      string `json:"emoji,omitempty"`
	At         int64  `json:"at"`
}

// StatPeople returns interactions for deliveries. Without names it returns only
// times and kinds (for the reads-over-time curve).
func (db *DB) StatPeople(ctx context.Context, dids []int64) []StatPerson {
	if len(dids) == 0 {
		return nil
	}
	ph := strings.Repeat("?,", len(dids))
	args := make([]any, len(dids))
	for i, d := range dids {
		args[i] = d
	}
	rows, err := db.QueryContext(ctx, `SELECT delivery_id, who, what, emoji, at FROM stat_people WHERE delivery_id IN (`+ph[:len(ph)-1]+`) ORDER BY at`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []StatPerson
	for rows.Next() {
		var p StatPerson
		_ = rows.Scan(&p.DeliveryID, &p.Who, &p.What, &p.Emoji, &p.At)
		if strings.HasPrefix(p.What, "reply:") {
			p.What = "reply"
		}
		out = append(out, p)
	}
	return out
}
