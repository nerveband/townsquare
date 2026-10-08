package server

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nerveband/townsquare/internal/contract"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/version"
)

var openapiJSON = contract.OpenAPI()

//go:embed guide.md
var guideMD []byte

// v1 is the public REST API for scripts and AI agents. Same data as the web app,
// with API-key auth, scopes, partial updates and names accepted for targets.
func (s *Server) v1() http.Handler { return s.requireKey(s.idempotent(s.v1Mux())) }

func (s *Server) v1Mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/{$}", s.v1Index)
	m.HandleFunc("GET /api/v1/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openapiJSON)
	})
	m.HandleFunc("GET /api/v1/guide.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write(guideMD)
	})
	m.HandleFunc("GET /api/v1/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, docsHTML)
	})

	m.HandleFunc("GET /api/v1/status", s.v1Status)
	m.HandleFunc("GET /api/v1/update", s.updateState)
	m.HandleFunc("POST /api/v1/update/check", s.updateCheck)
	m.HandleFunc("POST /api/v1/update/install", s.updateInstall)
	m.HandleFunc("GET /api/v1/autostart", s.autostartState)
	m.HandleFunc("PUT /api/v1/autostart", s.setAutostart)
	m.HandleFunc("POST /api/v1/quit", s.quit)
	m.HandleFunc("GET /api/v1/whatsapp", s.whatsappState)
	m.HandleFunc("POST /api/v1/whatsapp/link", s.whatsappLink)
	m.HandleFunc("GET /api/v1/whatsapp/qr.png", s.whatsappQR)
	m.HandleFunc("POST /api/v1/whatsapp/logout", s.whatsappLogout)
	m.HandleFunc("POST /api/v1/telegram/app", s.telegramApp)
	m.HandleFunc("DELETE /api/v1/telegram/app", s.telegramAppReset)
	m.HandleFunc("GET /api/v1/changelog", s.changelogHandler)
	m.HandleFunc("GET /api/v1/config", s.serverConfig)
	m.HandleFunc("PATCH /api/v1/config", s.patchServerConfig)
	m.HandleFunc("POST /api/v1/restart", s.restartServer)
	m.HandleFunc("PUT /api/v1/telegram/bot", s.setBotToken)
	m.HandleFunc("DELETE /api/v1/telegram/bot", s.removeBotToken)
	m.HandleFunc("POST /api/v1/login-link", s.loginLinkHandler)
	m.HandleFunc("POST /api/v1/whatsapp/channels", s.createChannel)
	m.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.DB.Settings(r.Context())) })
	m.HandleFunc("PATCH /api/v1/settings", s.saveSettings)

	m.HandleFunc("GET /api/v1/targets", s.v1Targets)
	m.HandleFunc("POST /api/v1/targets/refresh", s.refreshTargets)
	m.HandleFunc("GET /api/v1/targets/{jid}", s.v1Target)
	m.HandleFunc("PATCH /api/v1/targets/{jid}", s.patchTarget)

	for _, k := range []string{"tag", "client"} {
		plural := k + "s"
		m.HandleFunc("GET /api/v1/"+plural, s.v1ListNamed(k))
		m.HandleFunc("POST /api/v1/"+plural, s.saveNamed(k))
		m.HandleFunc("PATCH /api/v1/"+plural+"/{id}", s.v1PatchNamed(k))
		m.HandleFunc("DELETE /api/v1/"+plural+"/{id}", s.deleteNamed(k))
	}
	m.HandleFunc("GET /api/v1/sets", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.DB.Sets(r.Context())) })
	m.HandleFunc("POST /api/v1/sets", s.v1SaveSet)
	m.HandleFunc("PATCH /api/v1/sets/{id}", s.v1SaveSet)
	m.HandleFunc("DELETE /api/v1/sets/{id}", s.deleteNamed("set"))

	m.HandleFunc("POST /api/v1/media", s.uploadMedia)
	m.HandleFunc("GET /api/v1/media/{id}", s.mediaInfo)
	m.HandleFunc("GET /api/v1/media/{id}/file", s.mediaFile(false))
	m.HandleFunc("GET /api/v1/media/{id}/preview", s.mediaFile(true))

	m.HandleFunc("GET /api/v1/posts", s.v1Posts)
	m.HandleFunc("POST /api/v1/posts", s.v1CreatePost)
	m.HandleFunc("POST /api/v1/posts/preview", s.v1Preview)
	m.HandleFunc("POST /api/v1/posts/bulk", s.bulkPosts)
	m.HandleFunc("GET /api/v1/posts/{id}", s.getPost)
	m.HandleFunc("PATCH /api/v1/posts/{id}", s.v1PatchPost)
	m.HandleFunc("DELETE /api/v1/posts/{id}", s.deletePost)
	m.HandleFunc("GET /api/v1/posts/{id}/sends", s.nextRuns)
	m.HandleFunc("POST /api/v1/posts/{id}/send-now", s.sendNow)
	m.HandleFunc("POST /api/v1/posts/{id}/duplicate", s.duplicatePost)
	m.HandleFunc("POST /api/v1/posts/{id}/pause", s.v1SetStatus("paused"))
	m.HandleFunc("POST /api/v1/posts/{id}/resume", s.v1SetStatus("scheduled"))

	m.HandleFunc("GET /api/v1/sends", s.v1Sends)
	m.HandleFunc("GET /api/v1/sends/deliveries", s.sendDetail)
	m.HandleFunc("POST /api/v1/sends/move", s.moveSend)
	m.HandleFunc("POST /api/v1/sends/copy", s.copySend)
	m.HandleFunc("POST /api/v1/sends/skip", s.skipSend)
	m.HandleFunc("POST /api/v1/sends/unsend", s.takeBackHandler(false))
	m.HandleFunc("POST /api/v1/sends/edit", s.takeBackHandler(true))
	m.HandleFunc("GET /api/v1/sends/pending", s.pendingSends)

	m.HandleFunc("GET /api/v1/stats/summary", s.statsSummary)
	m.HandleFunc("GET /api/v1/stats/summary.txt", s.statsText)
	m.HandleFunc("GET /api/v1/stats/posts/{id}", s.statsPost)
	m.HandleFunc("GET /api/v1/stats/badges", s.statsBadges)
	m.HandleFunc("GET /api/v1/stats/export.csv", s.statsExport)
	m.HandleFunc("POST /api/v1/stats/share", s.statsShare)

	m.HandleFunc("GET /api/v1/changes", s.changes)
	m.HandleFunc("POST /api/v1/undo", s.v1Undo)
	m.HandleFunc("POST /api/v1/redo", s.redo)
	m.HandleFunc("POST /api/v1/test-send", s.testSend)

	m.HandleFunc("GET /api/v1/telegram", s.telegramState)
	m.HandleFunc("POST /api/v1/telegram/login", s.telegramLogin)
	m.HandleFunc("GET /api/v1/telegram/qr.png", s.telegramQR)
	m.HandleFunc("POST /api/v1/telegram/password", s.telegramPassword)
	m.HandleFunc("POST /api/v1/telegram/logout", s.telegramLogout)
	m.HandleFunc("POST /api/v1/telegram/refresh", s.telegramRefresh)
	m.HandleFunc("GET /api/v1/sessions", s.listSessions)
	m.HandleFunc("DELETE /api/v1/sessions/{id}", s.revokeSession)
	m.HandleFunc("GET /api/v1/keys", s.listKeys)
	m.HandleFunc("POST /api/v1/keys", s.createKey)
	m.HandleFunc("DELETE /api/v1/keys/{id}", s.revokeKey)

	m.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		failCode(w, 404, "not_found", fmt.Errorf("no route %s %s. See GET /api/v1/openapi.json", r.Method, r.URL.Path))
	})
	return m
}

func (s *Server) v1Index(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"name": "Townsquare API", "version": "1", "app_version": version.Version,
		"docs": "/api/v1/docs", "openapi": "/api/v1/openapi.json", "guide": "/api/v1/guide.md",
		"auth":      "Authorization: Bearer <key> (scopes: read < write < admin)",
		"resources": []string{"status", "settings", "targets", "tags", "clients", "sets", "media", "posts", "sends", "changes", "undo", "redo", "test-send", "stats", "keys"},
	})
}

func (s *Server) v1Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	set := s.DB.Settings(ctx)
	phone := ""
	if s.WA.Store.ID != nil {
		phone = s.WA.Store.ID.User
	}
	var allowed int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM targets WHERE allowed=1 AND gone=0`).Scan(&allowed)
	u, re := s.DB.CanUndoRedo(ctx)
	out := map[string]any{
		"connected": s.isConnected(), "phone": phone, "timezone": set["timezone"],
		"safe_mode": set["safe_mode"] == "1", "allowlisted_targets": allowed,
		"sent_today": s.DB.SentToday(ctx, set["timezone"]), "daily_cap": atoi(set["daily_cap"], 100),
		"undo": u, "redo": re, "now": time.Now().UTC().Format(time.RFC3339),
		"version": version.Version, "commit": version.Commit, "demo": s.Demo,
	}
	if k := keyOf(r); k != nil {
		out["key"] = map[string]string{"name": k.Name, "scope": k.Scope}
	}
	writeJSON(w, out)
}

// ---------- targets ----------

func (s *Server) v1Targets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ql := strings.ToLower(q.Get("q"))
	out := []store.Target{}
	for _, t := range s.DB.Targets(r.Context()) {
		if t.Gone && q.Get("include_gone") != "true" {
			continue
		}
		if k := q.Get("kind"); k != "" && !contains(strings.Split(k, ","), t.Kind) {
			continue
		}
		if ql != "" && !strings.Contains(strings.ToLower(t.Name+" "+t.Parent), ql) {
			continue
		}
		if c := q.Get("client_id"); c != "" && (t.ClientID == nil || strconv.FormatInt(*t.ClientID, 10) != c) {
			continue
		}
		if a := q.Get("allowed"); a != "" && strconv.FormatBool(t.Allowed) != a {
			continue
		}
		if q.Get("can_send") == "true" && !t.CanSend {
			continue
		}
		out = append(out, t)
	}
	// The body stays a plain list (v1 contract); the full match count is in a header.
	lo, hi := pageBounds(w, r, len(out))
	writeJSON(w, out[lo:hi])
}

func (s *Server) v1Target(w http.ResponseWriter, r *http.Request) {
	jid := r.PathValue("jid")
	for _, t := range s.DB.Targets(r.Context()) {
		if t.JID == jid {
			writeJSON(w, t)
			return
		}
	}
	fail(w, 404, fmt.Errorf("no target %s", jid))
}

// resolveTargets accepts JIDs, exact names (case-insensitive) and the aliases
// "me" (Message yourself) and "status". Ambiguous or unknown names are errors
// that list the closest matches, so an agent never posts to the wrong group.
func (s *Server) resolveTargets(r *http.Request, items []string) ([]string, error) {
	ts := s.DB.Targets(r.Context())
	byJID := map[string]bool{}
	for _, t := range ts {
		if !t.Gone {
			byJID[t.JID] = true
		}
	}
	var out []string
	var problems []string
	for _, raw := range items {
		it := strings.TrimSpace(raw)
		low := strings.ToLower(it)
		if byJID[it] {
			out = append(out, it)
			continue
		}
		var exact, partial []store.Target
		for _, t := range ts {
			if t.Gone {
				continue
			}
			if (low == "me" || low == "self") && t.Kind == "self" || low == "status" && t.Kind == "status" || strings.ToLower(t.Name) == low {
				exact = append(exact, t)
			} else if strings.Contains(strings.ToLower(t.Name), low) {
				partial = append(partial, t)
			}
		}
		switch {
		case len(exact) == 1:
			out = append(out, exact[0].JID)
		case len(exact) > 1:
			problems = append(problems, fmt.Sprintf("%q matches %d chats with that exact name; use a jid: %s", it, len(exact), describe(exact)))
		default:
			sort.Slice(partial, func(i, j int) bool { return partial[i].Members > partial[j].Members })
			hint := "no close matches"
			if len(partial) > 0 {
				hint = "did you mean: " + describe(partial)
			}
			problems = append(problems, fmt.Sprintf("%q is not a known chat (%s)", it, hint))
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return uniq(out), nil
}

func describe(ts []store.Target) string {
	var parts []string
	for i, t := range ts {
		if i == 5 {
			parts = append(parts, fmt.Sprintf("and %d more", len(ts)-5))
			break
		}
		parts = append(parts, fmt.Sprintf("%s [%s, %s]", t.Name, t.Kind, t.JID))
	}
	return strings.Join(parts, ", ")
}

// ---------- tags, clients, sets ----------

func (s *Server) v1ListNamed(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if kind == "tag" {
			writeJSON(w, s.DB.Tags(r.Context()))
		} else {
			writeJSON(w, s.DB.Clients(r.Context()))
		}
	}
}

// v1PatchNamed merges a partial {name?, color?} into the existing tag or client.
func (s *Server) v1PatchNamed(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		var cur *store.Client
		if kind == "client" {
			for _, c := range s.DB.Clients(r.Context()) {
				if c.ID == id {
					c := c
					cur = &c
				}
			}
		} else {
			for _, t := range s.DB.Tags(r.Context()) {
				if t.ID == id {
					cur = &store.Client{ID: t.ID, Name: t.Name, Color: t.Color}
				}
			}
		}
		if cur == nil {
			fail(w, 404, fmt.Errorf("no %s %d", kind, id))
			return
		}
		if !mergeInto(w, r, cur) {
			return
		}
		s.saveNamed(kind)(w, r)
	}
}

func (s *Server) v1SaveSet(w http.ResponseWriter, r *http.Request) {
	cur := &store.Set{}
	if id := pathID(r); id != 0 {
		found := false
		for _, x := range s.DB.Sets(r.Context()) {
			if x.ID == id {
				*cur, found = x, true
			}
		}
		if !found {
			fail(w, 404, fmt.Errorf("no set %d", id))
			return
		}
	}
	if !mergeInto(w, r, cur) {
		return
	}
	if len(cur.JIDs) > 0 {
		jids, err := s.resolveTargets(r, cur.JIDs)
		if err != nil {
			failCode(w, 422, "unknown_target", err)
			return
		}
		cur.JIDs = jids
	}
	setBody(r, cur)
	s.saveSet(w, r)
}

// mergeInto overlays the JSON body onto cur and rewrites the body as the merged object.
func mergeInto(w http.ResponseWriter, r *http.Request, cur any) bool {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, cur); err != nil {
			failCode(w, 400, "bad_json", err)
			return false
		}
	}
	setBody(r, cur)
	return true
}

func setBody(r *http.Request, v any) {
	b, _ := json.Marshal(v)
	r.Body = io.NopCloser(bytes.NewReader(b))
}

// ---------- posts ----------

func (s *Server) v1Posts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var sts []string
	if v := q.Get("status"); v != "" {
		sts = strings.Split(v, ",")
	}
	ps, err := s.DB.Posts(r.Context(), sts...)
	if err != nil {
		fail(w, 500, err)
		return
	}
	ql := strings.ToLower(q.Get("q"))
	out := []*store.Post{}
	for _, p := range ps {
		if v := q.Get("tag_id"); v != "" && (p.TagID == nil || strconv.FormatInt(*p.TagID, 10) != v) {
			continue
		}
		if v := q.Get("client_id"); v != "" && (p.ClientID == nil || strconv.FormatInt(*p.ClientID, 10) != v) {
			continue
		}
		if v := q.Get("target"); v != "" && !contains(p.Targets, v) {
			continue
		}
		if ql != "" && !strings.Contains(strings.ToLower(p.Title+" "+p.Caption), ql) {
			continue
		}
		out = append(out, p)
	}
	lo, hi := pageBounds(w, r, len(out))
	writeJSON(w, withTimes(out[lo:hi]))
}

// PostSummary is a post plus its next and most recent send, for lists and search.
type PostSummary struct {
	*store.Post
	NextAt *time.Time `json:"next_at"`
	LastAt *time.Time `json:"last_at"`
	Sends  int        `json:"upcoming"` // upcoming sends in the next 90 days
}

func withTimes(ps []*store.Post) []PostSummary {
	now := time.Now()
	out := make([]PostSummary, 0, len(ps))
	for _, p := range ps {
		cp := *p
		if cp.Status == "draft" || cp.Status == "archived" {
			out = append(out, PostSummary{Post: p})
			continue
		}
		cp.Status = "scheduled"
		sum := PostSummary{Post: p}
		fut := store.Expand([]*store.Post{&cp}, now, now.AddDate(0, 0, 90))
		if len(fut) > 0 {
			t := fut[0].At
			sum.NextAt, sum.Sends = &t, len(fut)
		}
		if past := store.Expand([]*store.Post{&cp}, now.AddDate(0, 0, -90), now); len(past) > 0 {
			t := past[len(past)-1].At
			sum.LastAt = &t
		}
		out = append(out, sum)
	}
	return out
}

// agentPost is the friendly write shape: everything optional, plus send_at.
type agentPost struct {
	Title    *string           `json:"title"`
	Caption  *string           `json:"caption"`
	Media    *[]int64          `json:"media"`
	Targets  *[]string         `json:"targets"`
	TagID    *json.RawMessage  `json:"tag_id"`
	ClientID *json.RawMessage  `json:"client_id"`
	Status   *string           `json:"status"`
	Schedule *[]store.Schedule `json:"schedules"`
	SendAt   string            `json:"send_at"` // RFC 3339 or local "YYYY-MM-DDTHH:MM"
	TZ       string            `json:"tz"`
	// edit scope for repeating posts (PATCH only)
	Scope      string `json:"scope"`
	ScheduleID int64  `json:"schedule_id"`
	Occ        string `json:"occ"`
	At         string `json:"at"`
	AtTZ       string `json:"at_tz"`
}

func optID(raw *json.RawMessage, cur *int64) *int64 {
	if raw == nil {
		return cur
	}
	if string(*raw) == "null" {
		return nil
	}
	var n int64
	if json.Unmarshal(*raw, &n) != nil {
		return cur
	}
	return &n
}

// apply overlays the agent's fields onto p. It resolves target names and send_at.
func (s *Server) apply(r *http.Request, in *agentPost, p *store.Post) error {
	if in.Title != nil {
		p.Title = *in.Title
	}
	if in.Caption != nil {
		p.Caption = *in.Caption
	}
	if in.Media != nil {
		p.Media = *in.Media
	}
	if in.Targets != nil {
		jids, err := s.resolveTargets(r, *in.Targets)
		if err != nil {
			return err
		}
		p.Targets = jids
	}
	p.TagID = optID(in.TagID, p.TagID)
	p.ClientID = optID(in.ClientID, p.ClientID)
	tz := nz(in.TZ, s.displayTZ(r.Context()))
	if in.Schedule != nil {
		p.Schedules = *in.Schedule
		for i := range p.Schedules {
			if p.Schedules[i].TZ == "" {
				p.Schedules[i].TZ = tz
			}
		}
	}
	if in.SendAt != "" {
		start, err := localIn(in.SendAt, tz)
		if err != nil {
			return err
		}
		p.Schedules = []store.Schedule{{Start: start, TZ: tz}}
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	return nil
}

// localIn turns RFC 3339 or "YYYY-MM-DDTHH:MM" into a local start string in tz.
func localIn(v, tz string) (string, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return "", err
		}
		return t.In(l).Format(store.Layout), nil
	}
	if _, err := store.ParseLocal(v, tz); err == nil {
		return v, nil
	}
	return "", fmt.Errorf("send_at %q: use RFC 3339 (2026-10-14T18:30:00-04:00) or local YYYY-MM-DDTHH:MM with tz", v)
}

func (s *Server) v1CreatePost(w http.ResponseWriter, r *http.Request) {
	var in agentPost
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_json", err)
		return
	}
	p := &store.Post{Status: "draft", Media: []int64{}, Targets: []string{}, Schedules: []store.Schedule{}}
	if err := s.apply(r, &in, p); err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	// API posts are drafts unless "status": "scheduled" is sent, even when they have a time.
	// If a time was given without a status, say so in the response.
	timed := in.Status == nil && len(p.Schedules) > 0
	setBody(r, p)
	if !timed {
		s.createPost(w, r)
		return
	}
	rec := &captureWriter{h: http.Header{}}
	s.createPost(rec, r)
	var body map[string]any
	if rec.code == 0 && json.Unmarshal(rec.buf.Bytes(), &body) == nil {
		body["notice"] = `saved as a draft. It has a time but will not send until you set "status": "scheduled" (PATCH /posts/{id}).`
		writeJSON(w, body)
		return
	}
	for k, v := range rec.h {
		w.Header()[k] = v
	}
	if rec.code != 0 {
		w.WriteHeader(rec.code)
	}
	_, _ = w.Write(rec.buf.Bytes())
}

func (s *Server) v1PatchPost(w http.ResponseWriter, r *http.Request) {
	cur, err := s.DB.Post(r.Context(), pathID(r))
	if err != nil {
		fail(w, 404, err)
		return
	}
	var in agentPost
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_json", err)
		return
	}
	if err := s.apply(r, &in, cur); err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	setBody(r, postIn{Post: *cur, Scope: in.Scope, ScheduleID: in.ScheduleID, Occ: in.Occ, At: in.At, AtTZ: in.AtTZ})
	s.updatePost(w, r)
}

func (s *Server) v1SetStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cur, err := s.DB.Post(r.Context(), pathID(r))
		if err != nil {
			fail(w, 404, err)
			return
		}
		if cur.Status == status { // already there: nothing to do, nothing to undo
			writeJSON(w, map[string]any{"changed": false, "post": cur})
			return
		}
		cur.Status = status
		setBody(r, postIn{Post: *cur})
		s.updatePost(w, r)
	}
}

func (s *Server) v1Preview(w http.ResponseWriter, r *http.Request) {
	var in agentPost
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_json", err)
		return
	}
	p := &store.Post{}
	if err := s.apply(r, &in, p); err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	p.Status = "scheduled"
	if err := validate(p); err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	occ := store.Expand([]*store.Post{p}, time.Now().Add(-time.Minute), time.Now().AddDate(2, 0, 0))
	if len(occ) > 10 {
		occ = occ[:10]
	}
	writeJSON(w, map[string]any{"valid": true, "targets": p.Targets, "next_sends": occ})
}

// ---------- sends, undo ----------

func (s *Server) v1Sends(w http.ResponseWriter, r *http.Request) {
	pid := r.URL.Query().Get("post_id")
	if pid == "" {
		s.sends(w, r)
		return
	}
	rec := &captureWriter{h: http.Header{}}
	s.sends(rec, r)
	var body struct {
		Sends []store.Occurrence `json:"sends"`
		Posts map[string]any     `json:"posts"`
		Media map[string]any     `json:"media"`
	}
	_ = json.Unmarshal(rec.buf.Bytes(), &body)
	out := []store.Occurrence{}
	for _, o := range body.Sends {
		if strconv.FormatInt(o.PostID, 10) == pid {
			out = append(out, o)
		}
	}
	writeJSON(w, map[string]any{"sends": out, "posts": body.Posts, "media": body.Media})
}

type captureWriter struct {
	h    http.Header
	buf  bytes.Buffer
	code int
}

func (c *captureWriter) Header() http.Header         { return c.h }
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(code int)        { c.code = code }

// v1Undo reverts the latest change. Pass {"expect_change": ID} to only undo if
// that change is still the latest, so an agent never undoes someone else's work.
func (s *Server) v1Undo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Expect int64 `json:"expect_change"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in)
	if in.Expect != 0 {
		if latest := s.DB.LatestUndoable(r.Context()); latest != in.Expect {
			u, _ := s.DB.CanUndoRedo(r.Context())
			failCode(w, 409, "conflict", fmt.Errorf("change %d is not the latest; the latest is %d (%q)", in.Expect, latest, u))
			return
		}
	}
	s.undo(w, r)
}

// ---------- keys ----------

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.DB.APIKeys(r.Context()))
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Scope string }
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_json", err)
		return
	}
	k, secret, err := s.DB.CreateAPIKey(r.Context(), strings.TrimSpace(in.Name), nz(in.Scope, "write"))
	if err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), fmt.Sprintf("created API key %s (%s)", k.Name, k.Scope))
	writeJSON(w, map[string]any{"key": k, "secret": secret, "note": "Store the secret now; it is not shown again."})
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := s.DB.RevokeAPIKey(r.Context(), id); err != nil {
		fail(w, 404, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), fmt.Sprintf("revoked API key #%d", id))
	writeJSON(w, map[string]bool{"ok": true})
}

const docsHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Townsquare API</title>
<style>:root{--scalar-color-accent:#128C7E;--scalar-font:"Inter",system-ui,sans-serif}</style></head><body>
<script id="api-reference" data-url="/api/v1/openapi.json" data-configuration='{"theme":"default","hideClientButton":false,"metaData":{"title":"Townsquare API"}}'></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body></html>`

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
