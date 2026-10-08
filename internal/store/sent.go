package store

import (
	"context"
	"time"
)

// Every message Townsquare sends is recorded here, so a send can later be
// deleted for everyone ("unsent") or its text edited, in every chat it went to.

const sentSchema = `
CREATE TABLE IF NOT EXISTS sent_msgs (
  id INTEGER PRIMARY KEY, schedule_id INTEGER NOT NULL, occ TEXT NOT NULL, jid TEXT NOT NULL,
  platform TEXT NOT NULL, msg_id TEXT NOT NULL, server_id INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL DEFAULT '',
  has_text INTEGER NOT NULL DEFAULT 0, sent_at INTEGER NOT NULL,
  deleted_at INTEGER NOT NULL DEFAULT 0, edited_at INTEGER NOT NULL DEFAULT 0, text TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS sent_msgs_send ON sent_msgs(schedule_id, occ);`

// SentMsg is one message in one chat.
type SentMsg struct {
	ID         int64     `json:"-"`
	ScheduleID int64     `json:"schedule_id"`
	Occ        string    `json:"occ"`
	JID        string    `json:"jid"`
	Platform   string    `json:"platform"`
	MsgID      string    `json:"msg_id"`
	ServerID   int64     `json:"server_id,omitempty"`
	Kind       string    `json:"kind"`
	HasText    bool      `json:"has_text"` // carries the caption or text (the one to edit)
	SentAt     time.Time `json:"sent_at"`
	DeletedAt  int64     `json:"deleted_at,omitempty"`
	EditedAt   int64     `json:"edited_at,omitempty"`
	Text       string    `json:"text,omitempty"` // the text as last edited
}

// RecordSent stores the messages of one delivery.
func (db *DB) RecordSent(ctx context.Context, scheduleID int64, occ, jid string, msgs []StatMsg) {
	now := time.Now().Unix()
	for _, m := range msgs {
		_, _ = db.ExecContext(ctx, `INSERT INTO sent_msgs(schedule_id,occ,jid,platform,msg_id,server_id,kind,has_text,sent_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			scheduleID, occ, jid, m.Platform, m.ID, m.ServerID, m.Kind, m.Text, now)
	}
}

// SentMsgs returns the messages of a send in every chat. Sends made before
// this table existed fall back to the stats record or the delivery's first id.
func (db *DB) SentMsgs(ctx context.Context, scheduleID int64, occ string) []SentMsg {
	var out []SentMsg
	rows, err := db.QueryContext(ctx, `SELECT id,schedule_id,occ,jid,platform,msg_id,server_id,kind,has_text,sent_at,deleted_at,edited_at,text
		FROM sent_msgs WHERE schedule_id=? AND occ=? ORDER BY id`, scheduleID, occ)
	if err == nil {
		for rows.Next() {
			var m SentMsg
			var at int64
			if rows.Scan(&m.ID, &m.ScheduleID, &m.Occ, &m.JID, &m.Platform, &m.MsgID, &m.ServerID, &m.Kind, &m.HasText, &at, &m.DeletedAt, &m.EditedAt, &m.Text) == nil {
				m.SentAt = time.Unix(at, 0)
				out = append(out, m)
			}
		}
		rows.Close()
	}
	have := map[string]bool{}
	for _, m := range out {
		have[m.JID] = true
	}
	// Older sends: one row per delivery, plus the stats copy of every message id.
	rows, err = db.QueryContext(ctx, `SELECT d.jid, d.wa_id, d.at, COALESCE(m.platform,''), COALESCE(m.msg_id,''), COALESCE(m.server_id,0), COALESCE(m.kind,'')
		FROM deliveries d LEFT JOIN stat_sends s ON s.delivery_id=d.id LEFT JOIN stat_msgs m ON m.delivery_id=d.id
		WHERE d.schedule_id=? AND d.occ=? AND d.state IN ('sent','unsent') ORDER BY d.id, m.rowid`, scheduleID, occ)
	if err != nil {
		return out
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var jid, waID, platform, msgID, kind string
		var at, serverID int64
		if rows.Scan(&jid, &waID, &at, &platform, &msgID, &serverID, &kind) != nil || have[jid] {
			continue
		}
		if msgID == "" {
			msgID = waID
		}
		if msgID == "" || waID == "telegram-queue" || seen[jid+"|"+msgID] {
			continue
		}
		seen[jid+"|"+msgID] = true
		if platform == "" {
			platform = "whatsapp"
			if len(jid) > 3 && jid[:3] == "tg:" {
				platform = "telegram"
			}
		}
		out = append(out, SentMsg{ScheduleID: scheduleID, Occ: occ, JID: jid, Platform: platform, MsgID: msgID, ServerID: serverID,
			Kind: kind, HasText: msgID == waID, SentAt: time.Unix(at, 0)})
	}
	return out
}

// MarkSentDeleted records that messages were deleted for everyone. Legacy
// rows (no id) are inserted so a repeat knows they're gone.
func (db *DB) MarkSentDeleted(ctx context.Context, msgs []SentMsg) {
	now := time.Now().Unix()
	for _, m := range msgs {
		if m.ID == 0 {
			_, _ = db.ExecContext(ctx, `INSERT INTO sent_msgs(schedule_id,occ,jid,platform,msg_id,server_id,kind,has_text,sent_at,deleted_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				m.ScheduleID, m.Occ, m.JID, m.Platform, m.MsgID, m.ServerID, m.Kind, m.HasText, m.SentAt.Unix(), now)
			continue
		}
		_, _ = db.ExecContext(ctx, `UPDATE sent_msgs SET deleted_at=? WHERE id=?`, now, m.ID)
	}
}

// MarkSentEdited records a new text for a message.
func (db *DB) MarkSentEdited(ctx context.Context, m SentMsg, text string) {
	now := time.Now().Unix()
	if m.ID == 0 {
		_, _ = db.ExecContext(ctx, `INSERT INTO sent_msgs(schedule_id,occ,jid,platform,msg_id,server_id,kind,has_text,sent_at,edited_at,text) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			m.ScheduleID, m.Occ, m.JID, m.Platform, m.MsgID, m.ServerID, m.Kind, true, m.SentAt.Unix(), now, text)
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE sent_msgs SET edited_at=?, text=? WHERE id=?`, now, text, m.ID)
}

// SetDeliveryState changes a delivery's state (for example to "unsent").
func (db *DB) SetDeliveryState(ctx context.Context, scheduleID int64, occ, jid, state, note string) {
	_, _ = db.ExecContext(ctx, `UPDATE deliveries SET state=?, error=? WHERE schedule_id=? AND occ=? AND jid=?`, state, note, scheduleID, occ, jid)
}
