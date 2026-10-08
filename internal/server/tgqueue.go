package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	tgapi "github.com/gotd/td/tg"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
)

// Telegram's own scheduled queue. When the "tg_queue_hours" setting is above 0,
// Telegram sends (not stories) due within that many hours are handed to Telegram's
// servers ahead of time, so they go out even if this computer is off. The
// reconciler keeps Telegram's queue in step with the calendar: edits, moves,
// pauses, deletes, undo, safe mode and quiet hours all update or remove what
// was queued. Stories can't be scheduled by Telegram, so they always send live.

const tgQueueSchema = `CREATE TABLE IF NOT EXISTS tg_queue (
  schedule_id INTEGER NOT NULL, occ TEXT NOT NULL, jid TEXT NOT NULL, post_id INTEGER NOT NULL,
  msg_ids TEXT NOT NULL, hash TEXT NOT NULL, run_at INTEGER NOT NULL, PRIMARY KEY (schedule_id, occ, jid))`

type queued struct {
	postID, scheduleID int64
	occ, jid, hash     string
	ids                []int
	runAt              int64
}

func queueKey(scheduleID int64, occ, jid string) string {
	return fmt.Sprintf("%d|%s|%s", scheduleID, occ, jid)
}

func queueHash(o store.Occurrence, jid string) string {
	b, _ := json.Marshal([]any{o.Caption, o.Media, o.At.Unix(), jid})
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

// queuedFor reports whether a send was handed to Telegram's queue.
func (s *Server) queuedFor(ctx context.Context, scheduleID int64, occ, jid string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_queue WHERE schedule_id=? AND occ=? AND jid=?`, scheduleID, occ, jid).Scan(&n)
	return n > 0
}

// reconcileTelegramQueue brings Telegram's scheduled messages in line with the calendar.
func (s *Server) reconcileTelegramQueue(ctx context.Context) {
	if !s.TG.Ready() {
		return
	}
	_, _ = s.DB.ExecContext(ctx, tgQueueSchema)
	set := s.DB.Settings(ctx)
	hours := atoi(set["tg_queue_hours"], 0)
	now := time.Now()

	// What should be in Telegram's queue right now.
	desired := map[string]queued{}
	if hours > 0 {
		posts, _ := s.DB.Posts(ctx, "scheduled")
		targets := map[string]store.Target{}
		for _, t := range s.DB.Targets(ctx) {
			targets[t.JID] = t
		}
		clients := map[int64]store.Client{}
		for _, c := range s.DB.Clients(ctx) {
			clients[c.ID] = c
		}
		for _, o := range store.Expand(posts, now.Add(2*time.Minute), now.Add(time.Duration(hours)*time.Hour)) {
			for _, jid := range o.Targets {
				// Only the first Telegram account uses Telegram's queue; extra accounts send live.
				if !strings.HasPrefix(jid, "tg:") || strings.HasPrefix(jid, "tg:story:") || s.DB.Delivered(ctx, o.ScheduleID, o.Occ, jid) {
					continue
				}
				t, ok := targets[jid]
				if !ok || t.Gone || !t.CanSend || (set["safe_mode"] == "1" && !t.Allowed) ||
					quietNow(o.At, t, postClient(posts, o.PostID), clients, set) != "" {
					continue // these get recorded as held or failed by the send loop at send time
				}
				desired[queueKey(o.ScheduleID, o.Occ, jid)] = queued{postID: o.PostID, scheduleID: o.ScheduleID, occ: o.Occ, jid: jid,
					hash: queueHash(o, jid), runAt: o.At.Unix()}
			}
		}
	}

	// What is queued now (only future items can still be changed).
	have := map[string]queued{}
	rows, err := s.DB.QueryContext(ctx, `SELECT post_id,schedule_id,occ,jid,msg_ids,hash,run_at FROM tg_queue WHERE run_at > ?`, now.Add(30*time.Second).Unix())
	if err != nil {
		return
	}
	for rows.Next() {
		var q queued
		var ids string
		_ = rows.Scan(&q.postID, &q.scheduleID, &q.occ, &q.jid, &ids, &q.hash, &q.runAt)
		_ = json.Unmarshal([]byte(ids), &q.ids)
		have[queueKey(q.scheduleID, q.occ, q.jid)] = q
	}
	rows.Close()

	// Remove what changed or is no longer wanted.
	for k, q := range have {
		if d, ok := desired[k]; ok && d.hash == q.hash {
			continue
		}
		addr, err := tg.Parse(q.jid)
		if err == nil && len(q.ids) > 0 {
			_, err = s.TG.API().MessagesDeleteScheduledMessages(ctx, &tgapi.MessagesDeleteScheduledMessagesRequest{Peer: addr.Peer, ID: q.ids})
		}
		if err != nil {
			log.Printf("telegram queue: remove %s: %v", q.jid, err)
			continue // keep the row so the send loop never double-sends; retry next tick
		}
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM tg_queue WHERE schedule_id=? AND occ=? AND jid=?`, q.scheduleID, q.occ, q.jid)
		delete(have, k)
	}

	// Add what is missing.
	if len(desired) == 0 {
		return
	}
	posts, _ := s.DB.Posts(ctx, "scheduled")
	occs := store.Expand(posts, now.Add(2*time.Minute), now.Add(time.Duration(hours)*time.Hour))
	byKey := map[string]store.Occurrence{}
	for _, o := range occs {
		for _, jid := range o.Targets {
			byKey[queueKey(o.ScheduleID, o.Occ, jid)] = o
		}
	}
	for k, d := range desired {
		if _, ok := have[k]; ok {
			continue
		}
		o := byKey[k]
		ids, err := s.queueTelegram(ctx, o, d.jid)
		if err != nil {
			log.Printf("telegram queue: add %s: %v", d.jid, err)
			continue
		}
		b, _ := json.Marshal(ids)
		_, _ = s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO tg_queue(schedule_id,occ,jid,post_id,msg_ids,hash,run_at) VALUES(?,?,?,?,?,?,?)`,
			d.scheduleID, d.occ, d.jid, d.postID, string(b), d.hash, d.runAt)
	}
}

func (s *Server) queueTelegram(ctx context.Context, o store.Occurrence, jid string) ([]int, error) {
	media, err := s.telegramMedia(ctx, o)
	if err != nil {
		return nil, err
	}
	return s.TG.SendIDs(ctx, jid, o.Caption, media, tg.Options{Schedule: o.At})
}
