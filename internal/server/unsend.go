package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/wa"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// Taking a post back after it went out: delete it for everyone ("unsend") or
// fix its text ("edit"), in every chat it went to, within each app's limits.
//
//   WhatsApp: delete for everyone for about 2.5 days (Status: while it's up,
//   24 hours); edit text for 15 minutes. Photo captions can't be edited.
//   Telegram (your account): delete and edit with no time limit.
//   Telegram bot: delete and edit for 48 hours.
//   Posts sent from Telegram's own queue have new ids we don't know: delete those in Telegram.

// msgOps does the platform calls; tests replace it.
type msgOps interface {
	revoke(ctx context.Context, m store.SentMsg) error
	edit(ctx context.Context, m store.SentMsg, text string) error
}

type liveOps struct{ s *Server }

func (o liveOps) revoke(ctx context.Context, m store.SentMsg) error {
	switch m.Platform {
	case "whatsapp":
		if !o.s.isConnected() {
			return errors.New("WhatsApp isn't connected")
		}
		chat, err := types.ParseJID(m.JID)
		if err != nil {
			return err
		}
		_, err = o.s.WA.SendMessage(ctx, chat, o.s.WA.BuildRevoke(chat, types.EmptyJID, types.MessageID(m.MsgID)))
		return err
	case "telegram":
		id, err := strconv.Atoi(m.MsgID)
		if err != nil {
			return err
		}
		return o.s.TG.Delete(ctx, m.JID, []int{id})
	case "telegram_bot":
		if o.s.Bot == nil {
			return errors.New("the Telegram bot isn't set up")
		}
		id, err := strconv.Atoi(m.MsgID)
		if err != nil {
			return err
		}
		return o.s.Bot.Delete(ctx, m.JID, []int{id})
	}
	return fmt.Errorf("can't delete %s messages", m.Platform)
}

func (o liveOps) edit(ctx context.Context, m store.SentMsg, text string) error {
	switch m.Platform {
	case "whatsapp":
		if !o.s.isConnected() {
			return errors.New("WhatsApp isn't connected")
		}
		chat, err := types.ParseJID(m.JID)
		if err != nil {
			return err
		}
		_, err = o.s.WA.SendMessage(ctx, chat, o.s.WA.BuildEdit(chat, types.MessageID(m.MsgID),
			&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text)}}))
		return err
	case "telegram":
		id, err := strconv.Atoi(m.MsgID)
		if err != nil {
			return err
		}
		return o.s.TG.Edit(ctx, m.JID, id, text)
	case "telegram_bot":
		if o.s.Bot == nil {
			return errors.New("the Telegram bot isn't set up")
		}
		id, err := strconv.Atoi(m.MsgID)
		if err != nil {
			return err
		}
		return o.s.Bot.Edit(ctx, m.JID, id, text, m.Kind != "text")
	}
	return fmt.Errorf("can't edit %s messages", m.Platform)
}

func (s *Server) ops() msgOps {
	if s.msgOps != nil {
		return s.msgOps
	}
	return liveOps{s}
}

// deleteWindow and editWindow: 0 means no limit.
func deleteWindow(m store.SentMsg) time.Duration {
	switch {
	case m.Platform == "telegram":
		return 0
	case m.Platform == "telegram_bot":
		return 48 * time.Hour
	case m.JID == types.StatusBroadcastJID.String():
		return 24 * time.Hour
	case wa.IsChannel(m.JID):
		return 30 * 24 * time.Hour
	}
	return 60 * time.Hour
}

func editWindow(m store.SentMsg) time.Duration {
	switch m.Platform {
	case "telegram":
		return 0
	case "telegram_bot":
		return 48 * time.Hour
	}
	return 15 * time.Minute
}

// ChatResult is what happened (or would happen) in one chat.
type ChatResult struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// deleted, edited, would_delete, would_edit, already_deleted, too_old,
	// cant_edit, not_supported, not_sent, failed
	Result   string `json:"result"`
	Messages int    `json:"messages"`
	Until    int64  `json:"until,omitempty"` // Unix time the window closes (0 = no limit)
	Note     string `json:"note,omitempty"`
}

type takeBackIn struct {
	PostID     int64    `json:"post_id"`
	ScheduleID int64    `json:"schedule_id"`
	Occ        string   `json:"occ"`
	Chats      []string `json:"chats"` // empty = every chat it went to
	Caption    string   `json:"caption"`
	DryRun     bool     `json:"dry_run"`
}

func (s *Server) takeBack(ctx context.Context, in takeBackIn, edit bool) ([]ChatResult, error) {
	if in.ScheduleID == 0 || in.Occ == "" {
		return nil, errors.New("schedule_id and occ are required (from sends list)")
	}
	if edit && strings.TrimSpace(in.Caption) == "" {
		return nil, errors.New("caption is required")
	}
	names := map[string]string{}
	for _, t := range s.DB.Targets(ctx) {
		names[t.JID] = t.Name
	}
	want := map[string]bool{}
	for _, c := range in.Chats {
		want[c] = true
	}
	byChat := map[string][]store.SentMsg{}
	var order []string
	for _, m := range s.DB.SentMsgs(ctx, in.ScheduleID, in.Occ) {
		if len(want) > 0 && !want[m.JID] {
			continue
		}
		if _, ok := byChat[m.JID]; !ok {
			order = append(order, m.JID)
		}
		byChat[m.JID] = append(byChat[m.JID], m)
	}
	var out []ChatResult
	// Chats sent from Telegram's queue (or not sent at all) have no message ids.
	for _, d := range s.DB.DeliveryDetails(ctx, in.ScheduleID, in.Occ) {
		jid := d["jid"]
		if (len(want) > 0 && !want[jid]) || byChat[jid] != nil {
			continue
		}
		r := ChatResult{JID: jid, Name: nz(names[jid], jid), Result: "not_sent", Note: "this chat didn't get it (" + d["state"] + ")"}
		if d["state"] == "unsent" {
			r.Result, r.Note = "already_deleted", ""
		}
		if d["state"] == "sent" && d["via"] == "telegram-queue" {
			r.Result, r.Note = "not_supported", "sent from Telegram's own queue; delete or edit it in Telegram"
		}
		out = append(out, r)
	}
	now := time.Now()
	for _, jid := range order {
		msgs := byChat[jid]
		r := ChatResult{JID: jid, Name: nz(names[jid], jid), Platform: msgs[0].Platform}
		var live []store.SentMsg
		for _, m := range msgs {
			if m.DeletedAt == 0 {
				live = append(live, m)
			}
		}
		if len(live) == 0 {
			r.Result = "already_deleted"
			out = append(out, r)
			continue
		}
		if edit {
			var target *store.SentMsg
			for i := range live {
				if live[i].HasText {
					target = &live[i]
					break
				}
			}
			switch {
			case target == nil:
				r.Result, r.Note = "cant_edit", "this send has no text to edit"
			case target.Platform == "whatsapp" && target.Kind != "text":
				r.Result, r.Note = "cant_edit", "WhatsApp can't change a photo or file caption; unsend it and send a new one"
			default:
				win := editWindow(*target)
				if win > 0 {
					r.Until = target.SentAt.Add(win).Unix()
				}
				r.Messages = 1
				switch {
				case win > 0 && now.After(target.SentAt.Add(win)):
					r.Result, r.Note = "too_old", fmt.Sprintf("edits are only possible for %s after sending", human(win))
				case in.DryRun:
					r.Result = "would_edit"
				default:
					if err := s.ops().edit(ctx, *target, in.Caption); err != nil {
						r.Result, r.Note = "failed", err.Error()
					} else {
						r.Result = "edited"
						s.DB.MarkSentEdited(ctx, *target, in.Caption)
					}
				}
			}
			out = append(out, r)
			continue
		}
		win := deleteWindow(live[0])
		if win > 0 {
			r.Until = live[0].SentAt.Add(win).Unix()
		}
		r.Messages = len(live)
		switch {
		case win > 0 && now.After(live[0].SentAt.Add(win)):
			r.Result, r.Note = "too_old", fmt.Sprintf("deleting for everyone is only possible for %s after sending", human(win))
		case in.DryRun:
			r.Result = "would_delete"
		default:
			var gone []store.SentMsg
			var errs []string
			for _, m := range live {
				if err := s.ops().revoke(ctx, m); err != nil {
					errs = append(errs, err.Error())
					continue
				}
				gone = append(gone, m)
			}
			s.DB.MarkSentDeleted(ctx, gone)
			switch {
			case len(errs) == 0:
				r.Result = "deleted"
				s.DB.SetDeliveryState(ctx, in.ScheduleID, in.Occ, jid, "unsent", "deleted for everyone")
			case len(gone) == 0:
				r.Result, r.Note = "failed", strings.Join(uniq(errs), "; ")
			default:
				r.Result, r.Note = "failed", fmt.Sprintf("deleted %d of %d messages: %s", len(gone), len(live), strings.Join(uniq(errs), "; "))
			}
		}
		out = append(out, r)
	}
	if out == nil {
		out = []ChatResult{}
	}
	return out, nil
}

func human(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%g days", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%g hours", d.Hours())
	}
	return fmt.Sprintf("%g minutes", d.Minutes())
}

func (s *Server) takeBackHandler(edit bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in takeBackIn
		if err := readJSON(r, &in); err != nil {
			failCode(w, 400, "bad_request", err)
			return
		}
		if s.Demo && !in.DryRun {
			failCode(w, 409, "unavailable", errors.New("demo mode: nothing was sent, so there's nothing to take back"))
			return
		}
		res, err := s.takeBack(r.Context(), in, edit)
		if err != nil {
			failCode(w, 422, "invalid", err)
			return
		}
		n := 0
		for _, c := range res {
			if c.Result == "deleted" || c.Result == "edited" {
				n++
			}
		}
		if n > 0 {
			title_ := "a post"
			if p, err := s.DB.Post(r.Context(), in.PostID); err == nil {
				title_ = title(p)
			}
			verb := "deleted for everyone"
			if edit {
				verb = "edited the sent text of"
			}
			s.DB.Log(r.Context(), actorOf(r), fmt.Sprintf("%s %s in %d chats", verb, title_, n))
		}
		writeJSON(w, map[string]any{"dry_run": in.DryRun, "chats": res, "changed": n > 0, "done": n})
	}
}

// pendingSends are sends waiting out the "undo send" pause: due, not sent yet.
func (s *Server) pendingSends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	delay := time.Duration(atoi(s.DB.Setting(ctx, "send_delay"), 0)) * time.Second
	out := []map[string]any{}
	if delay > 0 && !s.Demo {
		posts, _ := s.DB.Posts(ctx, "scheduled")
		now := time.Now()
		for _, o := range store.Expand(posts, now.Add(-delay), now.Add(time.Second)) {
			var left []string
			for _, jid := range o.Targets {
				if !s.DB.Delivered(ctx, o.ScheduleID, o.Occ, jid) && !(tg.IsTelegram(jid) && s.queuedFor(ctx, o.ScheduleID, o.Occ, jid)) {
					left = append(left, jid)
				}
			}
			if len(left) == 0 {
				continue
			}
			t := ""
			for _, p := range posts {
				if p.ID == o.PostID {
					t = title(p)
				}
			}
			sendAt := o.At.Add(delay)
			out = append(out, map[string]any{"post_id": o.PostID, "schedule_id": o.ScheduleID, "occ": o.Occ, "title": t,
				"at": o.At, "send_at": sendAt, "seconds_left": max(0, int(time.Until(sendAt).Seconds())), "chats": len(left)})
		}
	}
	writeJSON(w, map[string]any{"delay_seconds": int(delay.Seconds()), "items": out})
}
