package server

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/tgbot"
	"github.com/nerveband/townsquare/internal/wa"
)

// Run is the send loop. It checks for due sends every 15 seconds.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func inQuiet(now time.Time, start, end string) bool {
	if start == "" || end == "" || start == end {
		return false
	}
	hm := now.Format("15:04")
	if start < end {
		return hm >= start && hm < end
	}
	return hm >= start || hm < end // wraps midnight
}

// quietWindow returns the quiet hours that apply to a chat: its client's, else the
// post's client's, else the global setting. Equal start and end means none.
func quietWindow(t store.Target, postClient *int64, clients map[int64]store.Client, set map[string]string) (start, end, tz string) {
	for _, id := range []*int64{t.ClientID, postClient} {
		if id == nil {
			continue
		}
		if c, ok := clients[*id]; ok && c.QuietStart != "" {
			return c.QuietStart, c.QuietEnd, nz(c.Timezone, set["timezone"])
		}
	}
	return set["quiet_start"], set["quiet_end"], set["timezone"]
}

// quietNow describes the quiet window if now is inside it for this chat, else "".
// Your own chats (WhatsApp "Message yourself", Telegram Saved Messages) have no
// quiet hours: reminders to yourself go out at any time.
func quietNow(now time.Time, t store.Target, postClient *int64, clients map[int64]store.Client, set map[string]string) string {
	if t.Kind == "self" {
		return ""
	}
	start, end, tz := quietWindow(t, postClient, clients, set)
	l, err := time.LoadLocation(tz)
	if err != nil {
		l = time.UTC
	}
	if !inQuiet(now.In(l), start, end) {
		return ""
	}
	return start + " to " + end + " " + tz
}

func postClient(posts []*store.Post, id int64) *int64 {
	for _, p := range posts {
		if p.ID == id {
			return p.ClientID
		}
	}
	return nil
}

func (s *Server) tick(ctx context.Context) {
	set := s.DB.Settings(ctx)
	tz := set["timezone"]
	grace := time.Duration(atoi(set["grace_min"], 15)) * time.Minute
	// "Undo send": each send waits this long after its time, so it can still be cancelled.
	delay := time.Duration(atoi(set["send_delay"], 0)) * time.Second
	now := time.Now()
	posts, err := s.DB.Posts(ctx, "scheduled")
	if err != nil {
		log.Println("send loop:", err)
		return
	}
	// Anything older than the grace window that never went out is marked missed.
	for _, o := range store.Expand(posts, now.Add(-48*time.Hour), now.Add(-grace-delay)) {
		for _, jid := range o.Targets {
			if !s.DB.Delivered(ctx, o.ScheduleID, o.Occ, jid) {
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "missed", "", "not sent within the grace window (was Townsquare or WhatsApp offline?)")
			}
		}
	}
	from, to := dueRange(now, grace, delay)
	due := store.Expand(posts, from, to)
	if len(due) == 0 {
		return
	}
	waOK, tgOK := s.isConnected(), s.TG.Ready()
	if !waOK && !tgOK && s.Bot == nil {
		return // retry next tick, until the grace window passes
	}
	clients := map[int64]store.Client{}
	for _, c := range s.DB.Clients(ctx) {
		clients[c.ID] = c
	}
	targets := map[string]store.Target{}
	for _, t := range s.DB.Targets(ctx) {
		targets[t.JID] = t
	}
	gapMin, gapMax := atoi(set["gap_min"], 20), atoi(set["gap_max"], 60)
	if gapMax < gapMin {
		gapMax = gapMin
	}
	first := true
	for _, o := range due {
		var title_ string
		for _, p := range posts {
			if p.ID == o.PostID {
				title_ = title(p)
			}
		}
		prepared := map[string]*wa.Prepared{} // media id + channel flag
		sent, blocked, failed := 0, 0, 0
		for _, jid := range o.Targets {
			if s.DB.Delivered(ctx, o.ScheduleID, o.Occ, jid) {
				continue
			}
			if tg.IsTelegram(jid) && s.queuedFor(ctx, o.ScheduleID, o.Occ, jid) {
				// Handed to Telegram's own queue earlier; Telegram sends it on time.
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "sent", "telegram-queue", "")
				sent++
				continue
			}
			if (tg.IsTelegram(jid) && !tgOK) || (tgbot.IsBot(jid) && s.Bot == nil) || (!tg.IsTelegram(jid) && !tgbot.IsBot(jid) && !waOK) {
				continue // that platform is offline; retry next tick within the grace window
			}
			t, ok := targets[jid]
			switch {
			case !ok || t.Gone:
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "failed", "", "this group or channel is no longer available")
				failed++
				continue
			case set["safe_mode"] == "1" && !t.Allowed:
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "blocked", "", "safe mode: not on the allowlist")
				blocked++
				continue
			case quietNow(now, t, postClient(posts, o.PostID), clients, set) != "":
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "blocked", "", "not sent: inside quiet hours ("+quietNow(now, t, postClient(posts, o.PostID), clients, set)+")")
				blocked++
				continue
			case !t.CanSend:
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "failed", "", "you can't post here (admins only)")
				failed++
				continue
			case s.DB.SentToday(ctx, tz) >= atoi(set["daily_cap"], 100):
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "failed", "", "daily limit reached")
				failed++
				continue
			}
			if !first && t.Kind != "self" { // pacing protects groups; your own chat doesn't need it
				gap := time.Duration(gapMin+rand.IntN(gapMax-gapMin+1)) * time.Second
				select {
				case <-ctx.Done():
					return
				case <-time.After(gap):
				}
			}
			if t.Kind != "self" {
				first = false
			}
			s.mu.Lock()
			s.sending = fmt.Sprintf("%s → %s", title_, t.Name)
			s.mu.Unlock()
			var msgs []store.StatMsg
			id, err := s.deliver(ctx, o, jid, prepared, &msgs)
			s.mu.Lock()
			s.sending = ""
			s.mu.Unlock()
			if err != nil {
				log.Printf("send %q to %s: %v", title_, t.Name, err)
				s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "failed", "", err.Error())
				failed++
				continue
			}
			s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "sent", id, "")
			s.DB.RecordSent(ctx, o.ScheduleID, o.Occ, jid, msgs)
			var stat []store.StatMsg
			for _, m := range msgs {
				if m.Platform != "telegram_bot" {
					stat = append(stat, m)
				}
			}
			s.recordStatSend(ctx, o, t, posts, stat)
			sent++
		}
		if sent+failed+blocked > 0 {
			var parts []string
			if sent > 0 {
				parts = append(parts, fmt.Sprintf("sent to %d", sent))
			}
			if blocked > 0 {
				parts = append(parts, fmt.Sprintf("held back %d (safe mode or quiet hours)", blocked))
			}
			if failed > 0 {
				parts = append(parts, fmt.Sprintf("failed %d", failed))
			}
			s.DB.Log(ctx, "sender", fmt.Sprintf("%s: %s", title_, strings.Join(parts, ", ")))
		}
	}
}

// dueRange is the window of send times that go out now: within the late-send
// grace, and older than the undo-send pause.
func dueRange(now time.Time, grace, delay time.Duration) (time.Time, time.Time) {
	return now.Add(-grace - delay), now.Add(-delay + time.Second)
}

// deliver sends one occurrence to one chat: each media item as its own message
// (caption on the first), or a text message when there is no media.
func (s *Server) deliver(ctx context.Context, o store.Occurrence, jid string, prepared map[string]*wa.Prepared, msgs *[]store.StatMsg) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if msgs == nil {
		msgs = &[]store.StatMsg{}
	}
	if tg.IsTelegram(jid) {
		return s.deliverTelegram(ctx, o, jid, msgs)
	}
	waSend := func(p *wa.Prepared, text, kind string) (string, error) {
		r, err := wa.SendPreparedResp(ctx, s.WA, jid, p, text)
		if err == nil {
			*msgs = append(*msgs, store.StatMsg{Platform: "whatsapp", ID: r.ID, ServerID: int64(r.ServerID), Kind: kind, Text: text != ""})
		}
		return r.ID, err
	}
	if tgbot.IsBot(jid) {
		media, err := s.telegramMedia(ctx, o)
		if err != nil {
			return "", err
		}
		id, err := s.Bot.Send(ctx, jid, o.Caption, media)
		if err == nil && id != "" {
			kind := "text"
			if len(media) > 0 {
				kind = media[0].Kind
			}
			*msgs = append(*msgs, store.StatMsg{Platform: "telegram_bot", ID: id, Kind: kind, Text: true})
		}
		return id, err
	}
	if len(o.Media) == 0 {
		return waSend(nil, o.Caption, "text")
	}
	channel := wa.IsChannel(jid)
	var firstID string
	for i, mid := range o.Media {
		key := strconv.FormatInt(mid, 10) + strconv.FormatBool(channel)
		p := prepared[key]
		if p == nil {
			m, err := s.DB.Media(ctx, mid)
			if err != nil {
				return "", fmt.Errorf("media %d: %w", mid, err)
			}
			desc, err := wa.Describe(ctx, m.Kind, m.Path, m.Mime)
			if err != nil {
				return "", err
			}
			p, err = wa.Upload(ctx, s.WA, m.Kind, desc, filepath.Base(m.Name), channel)
			if err != nil {
				return "", err
			}
			prepared[key] = p
		}
		caption := ""
		if i == 0 && p.Kind != "voice" {
			caption = o.Caption
		}
		id, err := waSend(p, caption, p.Kind)
		if err != nil {
			return firstID, err
		}
		if firstID == "" {
			firstID = id
		}
		if i == 0 && p.Kind == "voice" && o.Caption != "" {
			if _, err := waSend(nil, o.Caption, "text"); err != nil {
				return firstID, err
			}
		}
	}
	return firstID, nil
}

// testSend sends a draft straight to your own "Message yourself" chat.
func (s *Server) testSend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Caption  string  `json:"caption"`
		Media    []int64 `json:"media"`
		Platform string  `json:"platform"` // whatsapp (default) or telegram
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if s.Demo {
		fail(w, 409, fmt.Errorf("demo mode: nothing is sent"))
		return
	}
	if in.Platform == "telegram" {
		if !s.TG.Ready() {
			fail(w, 503, fmt.Errorf("Telegram is not logged in"))
			return
		}
		if _, err := s.deliverTelegram(r.Context(), store.Occurrence{Caption: in.Caption, Media: in.Media}, "tg:self", nil); err != nil {
			fail(w, 500, err)
			return
		}
		s.DB.Log(r.Context(), actorOf(r), "sent a test to Telegram Saved Messages")
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if !s.isConnected() || s.WA.Store.ID == nil {
		fail(w, 503, fmt.Errorf("WhatsApp is not connected"))
		return
	}
	self := s.WA.Store.ID.ToNonAD().String()
	o := store.Occurrence{Caption: in.Caption, Media: in.Media}
	if _, err := s.deliver(r.Context(), o, self, map[string]*wa.Prepared{}, nil); err != nil {
		fail(w, 500, err)
		return
	}
	s.DB.Log(r.Context(), "you", "sent a test to Message yourself")
	writeJSON(w, map[string]bool{"ok": true})
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func nz(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
