package server

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	tgapi "github.com/gotd/td/tg"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/wa"
)

// ---------- collecting ----------

// recordStatSend links a successful send to its messages so engagement can be counted.
func (s *Server) recordStatSend(ctx context.Context, o store.Occurrence, t store.Target, posts []*store.Post, msgs []store.StatMsg) {
	if len(msgs) == 0 || t.Kind == "self" {
		return
	}
	st := store.StatSend{PostID: o.PostID, ScheduleID: o.ScheduleID, Occ: o.Occ, Chat: t.JID, Platform: t.Platform,
		Kind: "text", Members: t.Members, SchedAt: o.At}
	for _, p := range posts {
		if p.ID == o.PostID {
			st.TagID, st.ClientID = p.TagID, p.ClientID
		}
	}
	if len(o.Media) > 0 {
		if m, err := s.DB.Media(ctx, o.Media[0]); err == nil {
			st.Kind = m.Kind
			if len(o.Media) > 1 && (m.Kind == "image" || m.Kind == "video") {
				st.Kind = "album"
			}
		}
	}
	s.DB.RecordStatSend(ctx, st, msgs)
}

// onWAStat handles WhatsApp receipts, reactions and replies to our messages.
func (s *Server) onWAStat(evt any) {
	ctx := context.Background()
	switch e := evt.(type) {
	case *events.Receipt:
		if e.IsFromMe {
			return
		}
		what := map[types.ReceiptType]string{types.ReceiptTypeDelivered: "delivered", types.ReceiptTypeRead: "read", types.ReceiptTypePlayed: "played"}[e.Type]
		if what == "" {
			return
		}
		person := e.Sender.ToNonAD()
		who := s.waWho(ctx, person, "")
		for _, id := range e.MessageIDs {
			s.statEventRetry(ctx, id, person.String(), who, what, "", e.Timestamp)
		}
	case *events.Message:
		if e.Info.IsFromMe || e.Message == nil {
			return
		}
		person := e.Info.Sender.ToNonAD()
		m := e.Message
		var react *waE2E.ReactionMessage
		if r := m.GetReactionMessage(); r != nil {
			react = r
		} else if m.GetEncReactionMessage() != nil {
			if r, err := s.WA.DecryptReaction(ctx, e); err == nil {
				react = r
			}
		}
		if react != nil {
			s.statEventRetry(ctx, react.GetKey().GetID(), person.String(), s.waWho(ctx, person, e.Info.PushName), "react", react.GetText(), e.Info.Timestamp)
			return
		}
		if ci := contextInfo(m); ci != nil && ci.GetStanzaID() != "" {
			s.DB.StatEvent(ctx, "whatsapp", ci.GetStanzaID(), person.String(), s.waWho(ctx, person, e.Info.PushName), "reply:"+e.Info.ID, "", e.Info.Timestamp)
		}
	}
}

// statEventRetry retries once a few seconds later: a receipt can arrive before the
// send is recorded.
func (s *Server) statEventRetry(ctx context.Context, id, person, who, what, emoji string, at time.Time) {
	if s.DB.StatEvent(ctx, "whatsapp", id, person, who, what, emoji, at) || time.Since(at) > 2*time.Minute {
		return
	}
	go func() {
		time.Sleep(5 * time.Second)
		s.DB.StatEvent(ctx, "whatsapp", id, person, who, what, emoji, at)
	}()
}

// waWho is a person's display name, only when "who read it" lists are on.
func (s *Server) waWho(ctx context.Context, j types.JID, push string) string {
	if s.DB.Setting(ctx, "stats_people") != "1" {
		return ""
	}
	name := push
	if name == "" && s.WA != nil && s.WA.Store != nil && s.WA.Store.Contacts != nil {
		if c, err := s.WA.Store.Contacts.GetContact(ctx, j); err == nil {
			name = nz(c.FullName, nz(c.FirstName, c.PushName))
		}
	}
	num := ""
	if j.Server == types.DefaultUserServer {
		num = "+" + j.User
	}
	switch {
	case name != "" && num != "":
		return name + " · " + num
	case name != "":
		return name
	case num != "":
		return num
	}
	return "Someone (hidden number)"
}

func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		return m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		return m.GetStickerMessage().GetContextInfo()
	}
	return nil
}

// RunStats keeps engagement numbers fresh: reach marks every minute, polled
// platforms every 15 minutes, member counts and platform insights daily.
func (s *Server) RunStats(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	n, lastDay := 0, ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		s.DB.StatSnapshots(ctx, now)
		if n%15 == 1 {
			s.pollStats(ctx, now)
		}
		n++
		day := now.In(loc(s.displayTZ(ctx))).Format("2006-01-02")
		if day != lastDay && n > 2 {
			lastDay = day
			s.dailyStats(ctx, day)
		}
	}
}

func loc(tz string) *time.Location {
	l, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return l
}

func (s *Server) dailyStats(ctx context.Context, day string) {
	if s.isConnected() {
		if err := s.syncTargets(ctx); err != nil {
			log.Println("stats: targets:", err)
		}
	}
	members := map[string]int{}
	for _, t := range s.DB.Targets(ctx) {
		if t.Members > 0 && !t.Gone {
			members[t.JID] = t.Members
		}
	}
	s.DB.StatMembers(ctx, day, members)
	s.DB.StatPrune(ctx, time.Now())
	s.telegramInsights(ctx)
}

func (s *Server) pollStats(ctx context.Context, now time.Time) {
	byChat := map[string][]store.PollTarget{}
	for _, p := range s.DB.StatsToPoll(ctx, now) {
		byChat[p.Chat] = append(byChat[p.Chat], p)
	}
	for chat, ps := range byChat {
		var err error
		switch {
		case wa.IsChannel(chat):
			err = s.pollWAChannel(ctx, chat, ps)
		case tg.IsTelegram(chat):
			err = s.pollTelegram(ctx, chat, ps)
		}
		if err != nil {
			log.Printf("stats: %s: %v", chat, err)
		}
	}
}

func (s *Server) pollWAChannel(ctx context.Context, chat string, ps []store.PollTarget) error {
	if !s.isConnected() {
		return nil
	}
	j, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	since := ps[0].SentAt
	for _, p := range ps {
		if p.SentAt.Before(since) {
			since = p.SentAt
		}
	}
	// The plain message list carries view and reaction counts and answers
	// reliably; the "updates" query often times out, so it is only a fallback.
	upd, err := s.WA.GetNewsletterMessages(ctx, j, &whatsmeow.GetNewsletterMessagesParams{Count: 100})
	if err != nil {
		upd, err = s.WA.GetNewsletterMessageUpdates(ctx, j, &whatsmeow.GetNewsletterUpdatesParams{Count: 100, Since: since.Add(-time.Minute)})
	}
	if err != nil {
		return err
	}
	byServer := map[int64]*types.NewsletterMessage{}
	byID := map[string]*types.NewsletterMessage{}
	for _, m := range upd {
		byServer[int64(m.MessageServerID)] = m
		byID[m.MessageID] = m
	}
	for _, p := range ps {
		views, emoji, found := 0, map[string]int{}, false
		for _, m := range p.Msgs {
			nm := byServer[m.ServerID]
			if nm == nil {
				nm = byID[m.ID]
			}
			if nm == nil {
				continue
			}
			found = true
			views = max(views, nm.ViewsCount)
			for e, c := range nm.ReactionCounts {
				emoji[e] += c
			}
		}
		if found {
			s.DB.StatPolled(ctx, p.DeliveryID, views, 0, 0, emoji)
		} else {
			s.DB.StatMarkPolled(ctx, p.DeliveryID)
		}
	}
	return nil
}

func (s *Server) pollTelegram(ctx context.Context, chat string, ps []store.PollTarget) error {
	if !s.TG.Ready() {
		return nil
	}
	api := s.TG.API()
	addr, err := tg.Parse(chat)
	if err != nil {
		return err
	}
	var ids []int
	for _, p := range ps {
		for _, m := range p.Msgs {
			if n, err := strconv.Atoi(m.ID); err == nil {
				ids = append(ids, n)
			}
		}
	}
	type counts struct {
		views, forwards, replies int
		emoji                    map[string]int
	}
	got := map[int]*counts{}
	get := func(id int) *counts {
		if got[id] == nil {
			got[id] = &counts{emoji: map[string]int{}}
		}
		return got[id]
	}
	for i := 0; i < len(ids); i += 100 {
		chunk := ids[i:min(i+100, len(ids))]
		if addr.Story {
			r, err := api.StoriesGetStoriesViews(ctx, &tgapi.StoriesGetStoriesViewsRequest{Peer: addr.Peer, ID: chunk})
			if err != nil {
				return err
			}
			for k, v := range r.Views {
				if k < len(chunk) {
					c := get(chunk[k])
					c.views, c.forwards = v.ViewsCount, v.ForwardsCount
					addReactions(c.emoji, v.Reactions)
				}
			}
			continue
		}
		if _, ok := addr.Peer.(*tgapi.InputPeerChannel); ok {
			if r, err := api.MessagesGetMessagesViews(ctx, &tgapi.MessagesGetMessagesViewsRequest{Peer: addr.Peer, ID: chunk}); err == nil {
				for k, v := range r.Views {
					if k < len(chunk) {
						c := get(chunk[k])
						c.views, c.forwards = v.Views, v.Forwards
						c.replies = v.Replies.Replies
					}
				}
			}
		}
		if u, err := api.MessagesGetMessagesReactions(ctx, &tgapi.MessagesGetMessagesReactionsRequest{Peer: addr.Peer, ID: chunk}); err == nil {
			if up, ok := u.(*tgapi.Updates); ok {
				for _, x := range up.Updates {
					if mr, ok := x.(*tgapi.UpdateMessageReactions); ok {
						addReactions(get(mr.MsgID).emoji, mr.Reactions.Results)
					}
				}
			}
		}
	}
	for _, p := range ps {
		var views, fwd, rep int
		emoji, found := map[string]int{}, false
		for _, m := range p.Msgs {
			n, _ := strconv.Atoi(m.ID)
			c := got[n]
			if c == nil {
				continue
			}
			found = true
			views, fwd, rep = max(views, c.views), max(fwd, c.forwards), max(rep, c.replies)
			for e, k := range c.emoji {
				emoji[e] += k
			}
		}
		if found {
			s.DB.StatPolled(ctx, p.DeliveryID, views, fwd, rep, emoji)
		} else {
			s.DB.StatMarkPolled(ctx, p.DeliveryID)
		}
	}
	return nil
}

func addReactions(into map[string]int, rs []tgapi.ReactionCount) {
	for _, r := range rs {
		key := "★"
		switch v := r.Reaction.(type) {
		case *tgapi.ReactionEmoji:
			key = v.Emoticon
		case *tgapi.ReactionPaid:
			key = "⭐"
		}
		into[key] += r.Count
	}
}

// telegramInsights saves Telegram's own channel summary for channels we post to.
// Telegram only offers it for larger channels; others are skipped quietly.
func (s *Server) telegramInsights(ctx context.Context) {
	if !s.TG.Ready() {
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT s.chat FROM stat_sends s JOIN targets t ON t.jid=s.chat
		WHERE s.platform='telegram' AND t.kind='channel' AND s.sent_at > ?`, time.Now().AddDate(0, 0, -90).Unix())
	if err != nil {
		return
	}
	var chats []string
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		chats = append(chats, c)
	}
	rows.Close()
	for _, chat := range chats {
		addr, err := tg.Parse(chat)
		if err != nil {
			continue
		}
		pc, ok := addr.Peer.(*tgapi.InputPeerChannel)
		if !ok {
			continue
		}
		st, err := s.TG.API().StatsGetBroadcastStats(ctx, &tgapi.StatsGetBroadcastStatsRequest{Channel: &tgapi.InputChannel{ChannelID: pc.ChannelID, AccessHash: pc.AccessHash}})
		if err != nil {
			continue
		}
		pv := func(v tgapi.StatsAbsValueAndPrev) map[string]float64 {
			return map[string]float64{"current": v.Current, "previous": v.Previous}
		}
		notif := 0.0
		if st.EnabledNotifications.Total > 0 {
			notif = st.EnabledNotifications.Part / st.EnabledNotifications.Total
		}
		s.DB.StatInsight(ctx, chat, map[string]any{
			"followers": pv(st.Followers), "views_per_post": pv(st.ViewsPerPost), "shares_per_post": pv(st.SharesPerPost),
			"reactions_per_post": pv(st.ReactionsPerPost), "views_per_story": pv(st.ViewsPerStory), "notifications_on": notif,
			"period_from": st.Period.MinDate, "period_to": st.Period.MaxDate,
		})
	}
}

// ---------- reporting ----------

type statFilter struct {
	from, to, prevFrom time.Time
	days               int
	tz                 string
	platforms          map[string]bool
	clients, tags      map[int64]bool // key 0 = none
	chats              map[string]bool
}

func (s *Server) parseStatFilter(r *http.Request) statFilter {
	q := r.URL.Query()
	f := statFilter{tz: s.displayTZ(r.Context())}
	f.days = atoi(q.Get("days"), 30)
	if f.days < 1 || f.days > 366 {
		f.days = 30
	}
	l := loc(f.tz)
	now := time.Now().In(l)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, l).AddDate(0, 0, 1)
	if v := q.Get("to"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, l); err == nil {
			end = t.AddDate(0, 0, 1)
		}
	}
	f.to, f.from = end, end.AddDate(0, 0, -f.days)
	f.prevFrom = f.from.AddDate(0, 0, -f.days)
	list := func(k string) []string {
		var out []string
		for _, v := range strings.Split(q.Get(k), ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	ids := func(k string) map[int64]bool {
		vs := list(k)
		if len(vs) == 0 {
			return nil
		}
		m := map[int64]bool{}
		for _, v := range vs {
			n, _ := strconv.ParseInt(v, 10, 64)
			m[n] = true
		}
		return m
	}
	if vs := list("platform"); len(vs) > 0 {
		f.platforms = map[string]bool{}
		for _, v := range vs {
			f.platforms[v] = true
			if v == "telegram" {
				f.platforms["telegram_bot"] = true
			}
		}
	}
	f.clients, f.tags = ids("client"), ids("tag")
	if vs := list("chat"); len(vs) > 0 {
		f.chats = map[string]bool{}
		for _, v := range vs {
			f.chats[v] = true
		}
	}
	return f
}

func idOr0(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func platformOfJID(jid string) string {
	switch {
	case strings.HasPrefix(jid, "tgbot:"):
		return "telegram_bot"
	case tg.IsTelegram(jid):
		return "telegram"
	}
	return "whatsapp"
}

func (f statFilter) keep(platform, chat string, tag, client *int64) bool {
	if f.platforms != nil && !f.platforms[platform] {
		return false
	}
	if f.chats != nil && !f.chats[chat] {
		return false
	}
	if f.tags != nil && !f.tags[idOr0(tag)] {
		return false
	}
	if f.clients != nil && !f.clients[idOr0(client)] {
		return false
	}
	return true
}

func (s *Server) statRows(ctx context.Context, f statFilter, from, to time.Time) []store.StatRow {
	var out []store.StatRow
	for _, r := range s.DB.StatRows(ctx, from, to, 0) {
		if f.keep(r.Platform, r.Chat, r.TagID, r.ClientID) {
			out = append(out, r)
		}
	}
	return out
}

func rate(r store.StatRow) (float64, bool) {
	if r.Members <= 0 {
		return 0, false
	}
	return math.Min(1, float64(r.Reach())/float64(r.Members)), true
}

type tiles struct {
	Posts         int     `json:"posts"`
	Sends         int     `json:"sends"`
	OnTime        float64 `json:"on_time"`
	Reach         int     `json:"reach"`
	ReadRate      float64 `json:"read_rate"`
	Reactions     int     `json:"reactions"`
	Replies       int     `json:"replies"`
	Forwards      int     `json:"forwards"`
	ReactionsPost float64 `json:"reactions_per_post"`
	RepliesPost   float64 `json:"replies_per_post"`
}

func computeTiles(rows []store.StatRow) tiles {
	var t tiles
	posts := map[string]bool{}
	ontime, nrate := 0, 0
	sum := 0.0
	for _, r := range rows {
		posts[fmt.Sprintf("%d|%s", r.ScheduleID, r.Occ)] = true
		if r.SentAt-r.SchedAt <= 120 {
			ontime++
		}
		t.Reach += r.Reach()
		t.Reactions += r.Reactions
		t.Replies += r.Replies
		t.Forwards += r.Forwards
		if v, ok := rate(r); ok {
			sum += v
			nrate++
		}
	}
	t.Posts, t.Sends = len(posts), len(rows)
	if len(rows) > 0 {
		t.OnTime = float64(ontime) / float64(len(rows))
	}
	if nrate > 0 {
		t.ReadRate = sum / float64(nrate)
	}
	if t.Posts > 0 {
		t.ReactionsPost = float64(t.Reactions) / float64(t.Posts)
		t.RepliesPost = float64(t.Replies) / float64(t.Posts)
	}
	return t
}

type group struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Color     string  `json:"color,omitempty"`
	Sends     int     `json:"sends"`
	ReadRate  float64 `json:"read_rate"`
	Reach     float64 `json:"reach"`
	Reactions float64 `json:"reactions"`
	Replies   float64 `json:"replies"`
	RateN     int     `json:"rate_sends"` // sends with a known member count (read rate needs it)
}

func groupBy(rows []store.StatRow, key func(store.StatRow) (string, string, string)) []group {
	m := map[string]*group{}
	for _, r := range rows {
		k, label, color := key(r)
		g := m[k]
		if g == nil {
			g = &group{Key: k, Label: label, Color: color}
			m[k] = g
		}
		g.Sends++
		g.Reach += float64(r.Reach())
		g.Reactions += float64(r.Reactions)
		g.Replies += float64(r.Replies)
		if v, ok := rate(r); ok {
			g.ReadRate += v
			g.RateN++
		}
	}
	out := make([]group, 0, len(m))
	for _, g := range m {
		if g.RateN > 0 {
			g.ReadRate /= float64(g.RateN)
		}
		n := float64(g.Sends)
		g.Reach, g.Reactions, g.Replies = g.Reach/n, g.Reactions/n, g.Replies/n
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sends > out[j].Sends })
	return out
}

var kindLabels = map[string]string{"text": "Text", "image": "Photo", "album": "Photo album", "video": "Video", "voice": "Voice note", "audio": "Audio", "document": "File"}

// statsSummary powers the stats board (UI and /api/v1/stats/summary).
func (s *Server) statsSummary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.buildSummary(r.Context(), s.parseStatFilter(r)))
}

func (s *Server) buildSummary(ctx context.Context, f statFilter) map[string]any {
	l := loc(f.tz)
	rows := s.statRows(ctx, f, f.from, f.to)
	prev := s.statRows(ctx, f, f.prevFrom, f.from)
	targets := map[string]store.Target{}
	for _, t := range s.DB.Targets(ctx) {
		targets[t.JID] = t
	}
	posts, _ := s.DB.Posts(ctx)
	postByID := map[int64]*store.Post{}
	for _, p := range posts {
		postByID[p.ID] = p
	}
	days := make([]string, f.days)
	dayIdx := map[string]int{}
	for i := range days {
		days[i] = f.from.AddDate(0, 0, i).Format("2006-01-02")
		dayIdx[days[i]] = i
	}
	dayOf := func(unix int64) string { return time.Unix(unix, 0).In(l).Format("2006-01-02") }

	type dayRow struct {
		Day     string `json:"day"`
		Reach   int    `json:"reach"`
		Posts   int    `json:"posts"`
		Sent    int    `json:"sent"`
		Held    int    `json:"held"`
		Failed  int    `json:"failed"`
		Missed  int    `json:"missed"`
		Members int    `json:"members"`
	}
	daily := make([]dayRow, len(days))
	for i, d := range days {
		daily[i].Day = d
	}
	seenPost := map[string]bool{}
	for _, r := range rows {
		i, ok := dayIdx[dayOf(r.SentAt)]
		if !ok {
			continue
		}
		daily[i].Reach += r.Reach()
		k := fmt.Sprintf("%d|%s", r.ScheduleID, r.Occ)
		if !seenPost[k] {
			seenPost[k] = true
			daily[i].Posts++
		}
	}
	for _, d := range s.DB.DeliveryLog(ctx, f.from, f.to) {
		var tag, client *int64
		if p := postByID[d.PostID]; p != nil {
			tag, client = p.TagID, p.ClientID
		}
		if !f.keep(platformOfJID(d.Jid), d.Jid, tag, client) {
			continue
		}
		i, ok := dayIdx[dayOf(d.At)]
		if !ok {
			continue
		}
		switch d.State {
		case "sent":
			daily[i].Sent++
		case "blocked":
			daily[i].Held++
		case "failed":
			daily[i].Failed++
		case "missed":
			daily[i].Missed++
		}
	}

	// Member growth for chats in view (the chats posted to, or the chat filter).
	chatSet := map[string]bool{}
	for _, r := range rows {
		chatSet[r.Chat] = true
	}
	for c := range f.chats {
		chatSet[c] = true
	}
	hist := s.DB.MemberHistory(ctx, days[0], days[len(days)-1])
	growth := 0
	for c := range chatSet {
		h := hist[c]
		first, last, fd, ld := 0, 0, "", ""
		for d, n := range h {
			if fd == "" || d < fd {
				fd, first = d, n
			}
			if ld == "" || d > ld {
				ld, last = d, n
			}
			if i, ok := dayIdx[d]; ok {
				daily[i].Members += n
			}
		}
		growth += last - first
	}

	// Read speed: share of members reached after 1h, 6h, 24h, 7d.
	speed := map[string]any{}
	for _, m := range []struct {
		k string
		v func(store.StatRow) *int
	}{{"1h", func(r store.StatRow) *int { return r.R1h }}, {"6h", func(r store.StatRow) *int { return r.R6h }},
		{"24h", func(r store.StatRow) *int { return r.R24h }}, {"7d", func(r store.StatRow) *int { return r.R7d }}} {
		sum, n := 0.0, 0
		for _, r := range rows {
			if v := m.v(r); v != nil && r.Members > 0 {
				sum += math.Min(1, float64(*v)/float64(r.Members))
				n++
			}
		}
		val := 0.0
		if n > 0 {
			val = sum / float64(n)
		}
		speed[m.k] = map[string]any{"rate": val, "sends": n}
	}

	// Best time: average 1-hour reach rate by weekday and hour scheduled.
	type cell struct {
		Rate  float64 `json:"rate"`
		Sends int     `json:"sends"`
	}
	heat := make([][]cell, 7)
	for i := range heat {
		heat[i] = make([]cell, 24)
	}
	for _, r := range rows {
		if r.Members <= 0 {
			continue
		}
		v := r.R1h
		if v == nil {
			continue
		}
		t := time.Unix(r.SchedAt, 0).In(l)
		d := (int(t.Weekday()) + 6) % 7
		c := &heat[d][t.Hour()]
		c.Rate += math.Min(1, float64(*v)/float64(r.Members))
		c.Sends++
	}
	for d := range heat {
		for h := range heat[d] {
			if heat[d][h].Sends > 0 {
				heat[d][h].Rate /= float64(heat[d][h].Sends)
			}
		}
	}

	tagMap, clientMap := map[int64]store.Tag{}, map[int64]store.Client{}
	for _, t := range s.DB.Tags(ctx) {
		tagMap[t.ID] = t
	}
	for _, c := range s.DB.Clients(ctx) {
		clientMap[c.ID] = c
	}
	byKind := groupBy(rows, func(r store.StatRow) (string, string, string) { return r.Kind, nz(kindLabels[r.Kind], r.Kind), "" })
	byTag := groupBy(rows, func(r store.StatRow) (string, string, string) {
		if t, ok := tagMap[idOr0(r.TagID)]; ok {
			return strconv.FormatInt(t.ID, 10), t.Name, t.Color
		}
		return "0", "No tag", "#667781"
	})
	byClient := groupBy(rows, func(r store.StatRow) (string, string, string) {
		if c, ok := clientMap[idOr0(r.ClientID)]; ok {
			return strconv.FormatInt(c.ID, 10), c.Name, c.Color
		}
		return "0", "No client", "#667781"
	})
	byPlatform := groupBy(rows, func(r store.StatRow) (string, string, string) {
		p := map[string]string{"whatsapp": "WhatsApp", "telegram": "Telegram", "telegram_bot": "Telegram bot"}[r.Platform]
		return r.Platform, nz(p, r.Platform), ""
	})

	// Chats: totals plus a daily reach trend.
	chats := map[string]*statChat{}
	for _, r := range rows {
		c := chats[r.Chat]
		if c == nil {
			t := targets[r.Chat]
			c = &statChat{JID: r.Chat, Name: nz(t.Name, r.Chat), Platform: r.Platform, Kind: t.Kind, Members: nz0(t.Members, r.Members), Trend: make([]int, len(days))}
			chats[r.Chat] = c
		}
		c.Sends++
		c.Reach += r.Reach()
		c.Reacts += r.Reactions
		c.Replies += r.Replies
		if v, ok := rate(r); ok {
			c.ReadRate += v
			c.rateN++
		}
		if i, ok := dayIdx[dayOf(r.SentAt)]; ok {
			c.Trend[i] += r.Reach()
		}
	}
	chatList := make([]*statChat, 0, len(chats))
	for _, c := range chats {
		if c.rateN > 0 {
			c.ReadRate /= float64(c.rateN)
		}
		h := hist[c.JID]
		fd, ld := "", ""
		for d := range h {
			if fd == "" || d < fd {
				fd = d
			}
			if ld == "" || d > ld {
				ld = d
			}
		}
		if fd != "" {
			c.Growth = h[ld] - h[fd]
		}
		chatList = append(chatList, c)
	}
	sort.Slice(chatList, func(i, j int) bool { return chatList[i].Reach > chatList[j].Reach })

	emoji := map[string]int{}
	for _, r := range rows {
		for e, n := range r.Emoji {
			emoji[e] += n
		}
	}
	type em struct {
		Emoji string `json:"emoji"`
		Count int    `json:"count"`
	}
	var emojis []em
	for e, n := range emoji {
		emojis = append(emojis, em{e, n})
	}
	sort.Slice(emojis, func(i, j int) bool { return emojis[i].Count > emojis[j].Count })
	if len(emojis) > 12 {
		emojis = emojis[:12]
	}

	insights := map[string]any{}
	for c, d := range s.DB.Insights(ctx) {
		if (f.chats == nil || f.chats[c]) && (f.platforms == nil || f.platforms["telegram"]) {
			insights[c] = map[string]any{"name": nz(targets[c].Name, c), "data": d}
		}
	}

	cur, pv := computeTiles(rows), computeTiles(prev)
	return map[string]any{
		"from": f.from.Format("2006-01-02"), "to": f.to.AddDate(0, 0, -1).Format("2006-01-02"), "days": f.days, "tz": f.tz,
		"tiles": cur, "previous": pv, "member_growth": growth,
		"daily": daily, "speed": speed, "heatmap": heat,
		"by_kind": byKind, "by_tag": byTag, "by_client": byClient, "by_platform": byPlatform,
		"chats": chatList, "emoji": emojis, "insights": insights,
		"names":          s.DB.Setting(ctx, "stats_people") == "1",
		"tracking_since": s.trackingSince(ctx),
	}
}

type statChat struct {
	JID      string  `json:"jid"`
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	Kind     string  `json:"kind"`
	Members  int     `json:"members"`
	Sends    int     `json:"sends"`
	Reach    int     `json:"reach"`
	ReadRate float64 `json:"read_rate"`
	Reacts   int     `json:"reactions"`
	Replies  int     `json:"replies"`
	Growth   int     `json:"growth"`
	Trend    []int   `json:"trend"`
	rateN    int
}

func nz0(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func (s *Server) trackingSince(ctx context.Context) int64 {
	var at int64
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MIN(sent_at),0) FROM stat_sends`).Scan(&at)
	return at
}

// statsPost is the per-post panel: each send's numbers, reads over 48 hours, and
// (when the option is on) who read, reacted or replied.
func (s *Server) statsPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	q := r.URL.Query()
	rows := s.DB.StatRows(ctx, time.Now().AddDate(0, 0, -400), time.Now().Add(time.Hour), id)
	if sid := q.Get("schedule_id"); sid != "" {
		var keep []store.StatRow
		for _, x := range rows {
			if strconv.FormatInt(x.ScheduleID, 10) == sid && (q.Get("occ") == "" || x.Occ == q.Get("occ")) {
				keep = append(keep, x)
			}
		}
		rows = keep
	}
	targets := map[string]store.Target{}
	for _, t := range s.DB.Targets(ctx) {
		targets[t.JID] = t
	}
	type sendOut struct {
		store.StatRow
		Name     string  `json:"name"`
		ReadRate float64 `json:"read_rate"`
		Reach    int     `json:"reach"`
	}
	var sends []sendOut
	var dids []int64
	sentAt := map[int64]int64{}
	emoji := map[string]int{}
	tot := map[string]int{}
	for _, x := range rows {
		rr, _ := rate(x)
		sends = append(sends, sendOut{StatRow: x, Name: nz(targets[x.Chat].Name, x.Chat), ReadRate: rr, Reach: x.Reach()})
		dids = append(dids, x.DeliveryID)
		sentAt[x.DeliveryID] = x.SentAt
		for e, n := range x.Emoji {
			emoji[e] += n
		}
		tot["reach"] += x.Reach()
		tot["reads"] += x.Reads
		tot["views"] += x.Views
		tot["reactions"] += x.Reactions
		tot["replies"] += x.Replies
		tot["forwards"] += x.Forwards
		tot["members"] += x.Members
		tot["delivered"] += x.Delivered
	}
	curve := make([]int, 48)
	names := s.DB.Setting(ctx, "stats_people") == "1"
	var people []store.StatPerson
	for _, p := range s.DB.StatPeople(ctx, dids) {
		if p.What == "read" || p.What == "played" {
			h := int((p.At - sentAt[p.DeliveryID]) / 3600)
			if h >= 0 && h < 48 {
				curve[h]++
			}
		}
		if names && p.Who != "" && p.What != "delivered" {
			people = append(people, p)
		}
	}
	for i := 1; i < len(curve); i++ {
		curve[i] += curve[i-1]
	}
	rate := 0.0
	if tot["members"] > 0 {
		rate = math.Min(1, float64(tot["reach"])/float64(tot["members"]))
	}
	writeJSON(w, map[string]any{"sends": sends, "totals": tot, "read_rate": rate, "curve": curve, "emoji": emoji, "names": names, "people": people})
}

// statsBadges returns small per-send numbers for calendar cards, keyed "schedule_id|occ".
func (s *Server) statsBadges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := parseWhen(q.Get("from"), time.Now().AddDate(0, 0, -7))
	to := parseWhen(q.Get("to"), time.Now().AddDate(0, 0, 1))
	out := map[string]map[string]int{}
	for _, x := range s.DB.StatRows(r.Context(), from, to.Add(24*time.Hour), 0) {
		k := fmt.Sprintf("%d|%s", x.ScheduleID, x.Occ)
		if out[k] == nil {
			out[k] = map[string]int{}
		}
		out[k]["reach"] += x.Reach()
		out[k]["reactions"] += x.Reactions
		out[k]["replies"] += x.Replies
		out[k]["members"] += x.Members
	}
	writeJSON(w, out)
}

// summaryText is a short plain-text report (for copying or sending to a chat).
func (s *Server) summaryText(ctx context.Context, f statFilter) string {
	sum := s.buildSummary(ctx, f)
	t := sum["tiles"].(tiles)
	p := sum["previous"].(tiles)
	pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
	delta := func(a, b float64) string {
		if b == 0 {
			return ""
		}
		d := (a - b) / b * 100
		if math.Abs(d) < 1 {
			return " (same as before)"
		}
		sign := "+"
		if d < 0 {
			sign = ""
		}
		return fmt.Sprintf(" (%s%.0f%%)", sign, d)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "*Townsquare stats: last %d days* (%s to %s)\n\n", f.days, sum["from"], sum["to"])
	fmt.Fprintf(&b, "• Posts: %d, sent to %d chats, %s on time\n", t.Posts, t.Sends, pct(t.OnTime))
	fmt.Fprintf(&b, "• Reach: %d%s\n", t.Reach, delta(float64(t.Reach), float64(p.Reach)))
	fmt.Fprintf(&b, "• Read rate: %s\n", pct(t.ReadRate))
	fmt.Fprintf(&b, "• Reactions: %d, replies: %d\n", t.Reactions, t.Replies)
	if g := sum["member_growth"].(int); g != 0 {
		fmt.Fprintf(&b, "• Members: %+d\n", g)
	}
	if cs := sum["chats"].([]*statChat); len(cs) > 0 {
		b.WriteString("\n*Top chats*\n")
		for i, c := range cs {
			if i == 3 {
				break
			}
			fmt.Fprintf(&b, "%d. %s: %d reached, %s read\n", i+1, c.Name, c.Reach, pct(c.ReadRate))
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Server) statsText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.summaryText(r.Context(), s.parseStatFilter(r)) + "\n"))
}

// statsShare sends the text summary to one chat right away. "me" (default) is your
// own chat. With safe mode on, only allowlisted chats can receive it.
func (s *Server) statsShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		To string `json:"to"`
	}
	_ = readJSON(r, &in)
	to := nz(in.To, "me")
	if to == "me" {
		if !s.isConnected() || s.WA.Store.ID == nil {
			failCode(w, 503, "unavailable", errors.New("WhatsApp isn't connected"))
			return
		}
		to = s.WA.Store.ID.ToNonAD().String()
	}
	var tgt *store.Target
	for _, t := range s.DB.Targets(ctx) {
		if t.JID == to {
			t := t
			tgt = &t
		}
	}
	if tgt == nil {
		fail(w, 404, fmt.Errorf("unknown chat %q", to))
		return
	}
	if s.DB.Setting(ctx, "safe_mode") == "1" && !tgt.Allowed {
		failCode(w, 403, "not_allowed", errors.New("safe mode is on and this chat isn't on the allowlist"))
		return
	}
	text := s.summaryText(ctx, s.parseStatFilter(r))
	if _, err := s.deliver(ctx, store.Occurrence{Caption: text}, to, map[string]*wa.Prepared{}, nil); err != nil {
		fail(w, 502, err)
		return
	}
	s.DB.Log(ctx, actorOf(r), "sent a stats summary to "+tgt.Name)
	writeJSON(w, map[string]any{"ok": true, "sent_to": tgt.Name})
}

// statsExport downloads one row per send as CSV.
func (s *Server) statsExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := s.parseStatFilter(r)
	rows := s.statRows(ctx, f, f.from, f.to)
	targets := map[string]string{}
	for _, t := range s.DB.Targets(ctx) {
		targets[t.JID] = t.Name
	}
	titles := map[int64]string{}
	if posts, err := s.DB.Posts(ctx); err == nil {
		for _, p := range posts {
			titles[p.ID] = title(p)
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="townsquare-stats-%s-to-%s.csv"`, f.from.Format("2006-01-02"), f.to.AddDate(0, 0, -1).Format("2006-01-02")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"sent_at", "post", "chat", "platform", "kind", "members", "delivered", "reads", "views", "reach", "read_rate", "reactions", "replies", "forwards", "reach_1h", "reach_24h"})
	l := loc(f.tz)
	ptr := func(p *int) string {
		if p == nil {
			return ""
		}
		return strconv.Itoa(*p)
	}
	for _, x := range rows {
		rr, _ := rate(x)
		_ = cw.Write([]string{time.Unix(x.SentAt, 0).In(l).Format("2006-01-02 15:04"), titles[x.PostID], nz(targets[x.Chat], x.Chat), x.Platform, x.Kind,
			strconv.Itoa(x.Members), strconv.Itoa(x.Delivered), strconv.Itoa(x.Reads), strconv.Itoa(x.Views), strconv.Itoa(x.Reach()),
			fmt.Sprintf("%.3f", rr), strconv.Itoa(x.Reactions), strconv.Itoa(x.Replies), strconv.Itoa(x.Forwards), ptr(x.R1h), ptr(x.R24h)})
	}
	cw.Flush()
}
