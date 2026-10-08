package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/store"
)

type postIn struct {
	store.Post
	Scope      string `json:"scope"` // all, one, future (repeating posts only)
	ScheduleID int64  `json:"schedule_id"`
	Occ        string `json:"occ"`
	At         string `json:"at"`    // for scope one: new local time
	AtTZ       string `json:"at_tz"` // zone At is written in (defaults to the display zone)
}

func title(p *store.Post) string {
	if strings.TrimSpace(p.Title) != "" {
		return p.Title
	}
	c := strings.TrimSpace(strings.Split(p.Caption, "\n")[0])
	c = strings.Trim(c, "*_~ ")
	if len([]rune(c)) > 40 {
		c = string([]rune(c)[:40]) + "…"
	}
	if c == "" {
		return "Untitled post"
	}
	return c
}

func (s *Server) displayTZ(ctx context.Context) string { return s.DB.Setting(ctx, "timezone") }

// toScheduleTZ converts a local time typed in the display zone to the schedule's zone.
func toScheduleTZ(local, fromTZ, toTZ string) (string, error) {
	t, err := store.ParseLocal(local, fromTZ)
	if err != nil {
		return "", fmt.Errorf("bad time %q", local)
	}
	l, err := time.LoadLocation(toTZ)
	if err != nil {
		return "", err
	}
	return t.In(l).Format(store.Layout), nil
}

func validate(p *store.Post) error {
	switch p.Status {
	case "":
		p.Status = "draft"
	case "draft", "scheduled", "paused", "archived":
	default:
		return fmt.Errorf("bad status %q", p.Status)
	}
	for i := range p.Schedules {
		sc := &p.Schedules[i]
		if sc.TZ == "" {
			return errors.New("schedule time zone is required")
		}
		if _, err := store.ParseLocal(sc.Start, sc.TZ); err != nil {
			return fmt.Errorf("bad start time %q", sc.Start)
		}
		sc.RRule = strings.TrimPrefix(strings.TrimSpace(sc.RRule), "RRULE:")
		if sc.RRule != "" && len(store.ScheduleTimes(*sc, time.Now().AddDate(-5, 0, 0), time.Now().AddDate(5, 0, 0))) == 0 {
			return fmt.Errorf("repeat rule %q produces no dates", sc.RRule)
		}
	}
	if p.Status == "scheduled" {
		if len(p.Targets) == 0 {
			return errors.New("pick at least one group or channel")
		}
		if len(p.Schedules) == 0 {
			return errors.New("add a time")
		}
		if strings.TrimSpace(p.Caption) == "" && len(p.Media) == 0 {
			return errors.New("write a message or add media")
		}
	}
	return nil
}

type bulkIn struct {
	IDs      []int64          `json:"ids"`
	Action   string           `json:"action"` // pause, resume, draft, archive, delete, tag, client, add_targets, remove_targets
	TagID    *json.RawMessage `json:"tag_id"`
	ClientID *json.RawMessage `json:"client_id"`
	Targets  []string         `json:"targets"`
}

// bulkPosts applies one action to many posts as a single undoable change.
func (s *Server) bulkPosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in bulkIn
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_json", err)
		return
	}
	if len(in.IDs) == 0 {
		failCode(w, 400, "bad_request", errors.New("ids is required"))
		return
	}
	var targets []string
	if in.Action == "add_targets" || in.Action == "remove_targets" {
		var err error
		if targets, err = s.resolveTargets(r, in.Targets); err != nil || len(targets) == 0 {
			if err == nil {
				err = errors.New("targets is required")
			}
			failCode(w, 422, "unknown_target", err)
			return
		}
	}
	keys := make([]store.Key, 0, len(in.IDs))
	for _, id := range in.IDs {
		keys = append(keys, store.PostKey(id))
	}
	verbs := map[string]string{"pause": "paused", "resume": "resumed", "draft": "moved to drafts", "archive": "archived", "delete": "deleted",
		"tag": "retagged", "client": "changed the client of", "add_targets": "added groups to", "remove_targets": "removed groups from"}
	verb, ok := verbs[in.Action]
	if !ok {
		failCode(w, 400, "bad_request", fmt.Errorf("unknown action %q", in.Action))
		return
	}
	n := 0
	var results []map[string]any // per post: changed, unchanged or not_found
	cid, err := s.DB.Mutate(ctx, actorOf(r), "", keys, func(tx *store.Tx) error {
		results = results[:0]
		for _, id := range in.IDs {
			p, err := tx.Post(id)
			if errors.Is(err, store.ErrNotFound) {
				results = append(results, map[string]any{"id": id, "result": "not_found"})
				continue
			}
			if err != nil {
				return err
			}
			switch in.Action {
			case "delete":
				if err := tx.DeletePost(id); err != nil {
					return err
				}
				n++
				results = append(results, map[string]any{"id": id, "result": "changed"})
				continue
			case "pause":
				if p.Status != "scheduled" {
					results = append(results, map[string]any{"id": id, "result": "unchanged"})
					continue
				}
				p.Status = "paused"
			case "resume":
				if p.Status != "paused" {
					results = append(results, map[string]any{"id": id, "result": "unchanged"})
					continue
				}
				p.Status = "scheduled"
			case "draft":
				p.Status = "draft"
			case "archive":
				p.Status = "archived"
			case "tag":
				p.TagID = optID(in.TagID, nil)
			case "client":
				p.ClientID = optID(in.ClientID, nil)
			case "add_targets":
				p.Targets = uniq(append(p.Targets, targets...))
			case "remove_targets":
				var keep []string
				for _, t := range p.Targets {
					if !contains(targets, t) {
						keep = append(keep, t)
					}
				}
				if len(keep) == 0 && p.Status == "scheduled" {
					return fmt.Errorf("%s would have no groups left; pause it first or add another group", title(p))
				}
				p.Targets = keep
			}
			if err := tx.PutPost(p); err != nil {
				return err
			}
			n++
			results = append(results, map[string]any{"id": id, "result": "changed"})
		}
		return nil
	})
	if err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	summary := fmt.Sprintf("%s %d %s", verb, n, map[bool]string{true: "post", false: "posts"}[n == 1])
	_, _ = s.DB.ExecContext(ctx, `UPDATE changes SET summary=? WHERE id=?`, summary, cid)
	s.mutated(w, r, cid, map[string]any{"summary": summary, "count": n, "results": results, "changed": n > 0})
}

func (s *Server) getPost(w http.ResponseWriter, r *http.Request) {
	p, err := s.DB.Post(r.Context(), pathID(r))
	if err != nil {
		fail(w, 404, err)
		return
	}
	writeJSON(w, p)
}

func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	var in postIn
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	p := in.Post
	p.ID, p.CreatedAt = 0, 0
	for i := range p.Schedules {
		p.Schedules[i].ID = 0
	}
	if err := validate(&p); err != nil {
		fail(w, 400, err)
		return
	}
	verb := "created draft "
	if p.Status == "scheduled" {
		verb = "scheduled "
	}
	cid, err := s.DB.Mutate(r.Context(), actorOf(r), verb+title(&p), nil, func(tx *store.Tx) error { return tx.PutPost(&p) })
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.mutated(w, r, cid, map[string]any{"post": p})
}

func findSchedule(p *store.Post, id int64) *store.Schedule {
	for i := range p.Schedules {
		if p.Schedules[i].ID == id {
			return &p.Schedules[i]
		}
	}
	return nil
}

func setOverride(sc *store.Schedule, o store.Override) {
	for i := range sc.Overrides {
		if sc.Overrides[i].Occ == o.Occ {
			sc.Overrides[i] = o
			return
		}
	}
	sc.Overrides = append(sc.Overrides, o)
}

func getOverride(sc *store.Schedule, occ string) store.Override {
	for _, o := range sc.Overrides {
		if o.Occ == occ {
			return o
		}
	}
	return store.Override{Occ: occ}
}

func sameS(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }
func sameI(a, b []int64) bool  { return fmt.Sprint(a) == fmt.Sprint(b) }

// minusMinute returns the local time one minute before occ, used to end a series.
func minusMinute(occ, tz string) string {
	t, _ := store.ParseLocal(occ, tz)
	return t.Add(-time.Minute).Format(store.Layout)
}

func (s *Server) updatePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	var in postIn
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	cur, err := s.DB.Post(ctx, id)
	if err != nil {
		fail(w, 404, err)
		return
	}
	next := in.Post
	next.ID, next.CreatedAt = id, cur.CreatedAt
	scope := in.Scope
	if scope == "" {
		scope = "all"
	}
	sc := findSchedule(cur, in.ScheduleID)
	if scope != "all" && (sc == nil || sc.RRule == "" || in.Occ == "") {
		scope = "all"
	}
	if scope == "future" && sc != nil && sc.Start == in.Occ {
		scope = "all" // first send: "this and later" is the whole series
	}
	keys := []store.Key{store.PostKey(id)}
	var summary string

	cid, err := s.DB.Mutate(ctx, actorOf(r), "", keys, func(tx *store.Tx) error {
		switch scope {
		case "one":
			o := getOverride(sc, in.Occ)
			if !sameS(next.Targets, cur.Targets) {
				t := append([]string{}, next.Targets...)
				o.Targets = &t
			} else {
				o.Targets = nil
			}
			if next.Caption != cur.Caption {
				c := next.Caption
				o.Caption = &c
			} else {
				o.Caption = nil
			}
			if !sameI(next.Media, cur.Media) {
				m := append([]int64{}, next.Media...)
				o.Media = &m
			} else {
				o.Media = nil
			}
			if in.At != "" {
				at, err := toScheduleTZ(in.At, nz(in.AtTZ, s.displayTZ(ctx)), sc.TZ)
				if err != nil {
					return err
				}
				if at == in.Occ {
					at = ""
				}
				o.At = at
			}
			setOverride(sc, o)
			summary = fmt.Sprintf("edited only the %s send of %s", prettyOcc(in.Occ, sc.TZ), title(cur))
			return tx.PutPost(cur)
		case "future":
			// End the old series just before this send, and start a new post from here.
			old := *sc
			sc.Until = minusMinute(in.Occ, sc.TZ)
			var keep []store.Override
			for _, o := range sc.Overrides {
				if o.Occ < in.Occ {
					keep = append(keep, o)
				}
			}
			sc.Overrides = keep
			if err := tx.PutPost(cur); err != nil {
				return err
			}
			np := next
			np.ID, np.CreatedAt = 0, 0
			ns := old
			ns.ID, ns.Overrides, ns.Start = 0, nil, in.Occ
			if len(next.Schedules) > 0 {
				// Use the edited rule/time from the form for the new series.
				for _, x := range next.Schedules {
					if x.ID == old.ID {
						ns.RRule, ns.TZ, ns.Until = x.RRule, x.TZ, x.Until
						if x.Start != old.Start {
							ns.Start = x.Start
						}
					}
				}
			}
			np.Schedules = []store.Schedule{ns}
			if err := validate(&np); err != nil {
				return err
			}
			summary = fmt.Sprintf("edited %s from %s onward", title(cur), prettyOcc(in.Occ, old.TZ))
			return tx.PutPost(&np)
		default:
			if err := validate(&next); err != nil {
				return err
			}
			summary = describeEdit(cur, &next)
			return tx.PutPost(&next)
		}
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE changes SET summary=? WHERE id=?`, summary, cid)
	p, _ := s.DB.Post(ctx, id)
	s.mutated(w, r, cid, map[string]any{"post": p, "summary": summary})
}

func describeEdit(cur, next *store.Post) string {
	t := title(next)
	switch {
	case cur.Status != next.Status && next.Status == "scheduled" && cur.Status == "draft":
		return "scheduled " + t
	case cur.Status != next.Status && next.Status == "paused":
		return "paused " + t
	case cur.Status != next.Status && next.Status == "scheduled":
		return "resumed " + t
	case cur.Status != next.Status && next.Status == "archived":
		return "archived " + t
	case !sameID(cur.TagID, next.TagID) && cur.Caption == next.Caption && sameS(cur.Targets, next.Targets):
		if next.TagID == nil {
			return "removed the tag from " + t
		}
		return "changed the tag on " + t
	case !sameID(cur.ClientID, next.ClientID) && cur.Caption == next.Caption && sameS(cur.Targets, next.Targets):
		if next.ClientID == nil {
			return "removed the client from " + t
		}
		return "changed the client on " + t
	case !sameS(cur.Targets, next.Targets) && cur.Caption == next.Caption:
		added, removed := diff(cur.Targets, next.Targets)
		var parts []string
		if added > 0 {
			parts = append(parts, fmt.Sprintf("added %d", added))
		}
		if removed > 0 {
			parts = append(parts, fmt.Sprintf("removed %d", removed))
		}
		return fmt.Sprintf("changed groups on %s (%s)", t, strings.Join(parts, ", "))
	}
	return "edited " + t
}

func sameID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func diff(a, b []string) (added, removed int) {
	in := map[string]bool{}
	for _, x := range a {
		in[x] = true
	}
	for _, x := range b {
		if !in[x] {
			added++
		}
		delete(in, x)
	}
	return added, len(in)
}

func prettyOcc(occ, tz string) string {
	t, err := store.ParseLocal(occ, tz)
	if err != nil {
		return occ
	}
	return t.Format("Mon Jan 2")
}

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	cur, err := s.DB.Post(ctx, id)
	if err != nil {
		fail(w, 404, err)
		return
	}
	q := r.URL.Query()
	scope, occ := q.Get("scope"), q.Get("occ")
	sid, _ := strconv.ParseInt(q.Get("schedule_id"), 10, 64)
	sc := findSchedule(cur, sid)
	if sc == nil || sc.RRule == "" || occ == "" {
		scope = "all"
	}
	var summary string
	cid, err := s.DB.Mutate(ctx, actorOf(r), "", []store.Key{store.PostKey(id)}, func(tx *store.Tx) error {
		switch scope {
		case "one":
			o := getOverride(sc, occ)
			o.Skipped = true
			setOverride(sc, o)
			summary = fmt.Sprintf("skipped the %s send of %s", prettyOcc(occ, sc.TZ), title(cur))
			return tx.PutPost(cur)
		case "future":
			if sc.Start != occ {
				sc.Until = minusMinute(occ, sc.TZ)
				summary = fmt.Sprintf("stopped %s from %s onward", title(cur), prettyOcc(occ, sc.TZ))
				return tx.PutPost(cur)
			}
			fallthrough
		default:
			summary = "deleted " + title(cur)
			return tx.DeletePost(id)
		}
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE changes SET summary=? WHERE id=?`, summary, cid)
	s.mutated(w, r, cid, nil)
}

func (s *Server) duplicatePost(w http.ResponseWriter, r *http.Request) {
	cur, err := s.DB.Post(r.Context(), pathID(r))
	if err != nil {
		fail(w, 404, err)
		return
	}
	np := *cur
	np.ID, np.CreatedAt, np.Status = 0, 0, "draft"
	np.Title = title(cur) + " (copy)"
	np.Schedules = []store.Schedule{}
	cid, err := s.DB.Mutate(r.Context(), actorOf(r), "duplicated "+title(cur), nil, func(tx *store.Tx) error { return tx.PutPost(&np) })
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.mutated(w, r, cid, map[string]any{"post": np})
}

// sendNow adds a one-off schedule for right now; the send loop picks it up.
func (s *Server) sendNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cur, err := s.DB.Post(ctx, pathID(r))
	if err != nil {
		fail(w, 404, err)
		return
	}
	tz := s.displayTZ(ctx)
	l, _ := time.LoadLocation(tz)
	now := time.Now().In(l).Format(store.Layout)
	cid, err := s.DB.Mutate(ctx, actorOf(r), "sent "+title(cur)+" now", []store.Key{store.PostKey(cur.ID)}, func(tx *store.Tx) error {
		cur.Schedules = append(cur.Schedules, store.Schedule{Start: now, TZ: tz})
		cur.Status = "scheduled"
		if err := validate(cur); err != nil {
			return err
		}
		return tx.PutPost(cur)
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	p, _ := s.DB.Post(ctx, cur.ID)
	s.mutated(w, r, cid, map[string]any{"post": p})
}

func (s *Server) nextRuns(w http.ResponseWriter, r *http.Request) {
	p, err := s.DB.Post(r.Context(), pathID(r))
	if err != nil {
		fail(w, 404, err)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 50 {
		n = 6
	}
	cp := *p
	cp.Status = "scheduled"
	occ := store.Expand([]*store.Post{&cp}, time.Now().AddDate(0, 0, -30), time.Now().AddDate(2, 0, 0))
	s.DB.AttachDeliveries(r.Context(), occ)
	// keep up to 2 past sends for context, then n upcoming
	var past, future []store.Occurrence
	for _, o := range occ {
		if o.At.Before(time.Now()) {
			past = append(past, o)
		} else if len(future) < n {
			future = append(future, o)
		}
	}
	if len(past) > 2 {
		past = past[len(past)-2:]
	}
	writeJSON(w, append(past, future...))
}

// preview lists the next sends of an unsaved post, for the composer.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var p store.Post
	if err := readJSON(r, &p); err != nil {
		fail(w, 400, err)
		return
	}
	p.Status = "scheduled"
	for i := range p.Schedules {
		p.Schedules[i].RRule = strings.TrimPrefix(strings.TrimSpace(p.Schedules[i].RRule), "RRULE:")
	}
	occ := store.Expand([]*store.Post{&p}, time.Now().Add(-time.Minute), time.Now().AddDate(2, 0, 0))
	if len(occ) > 8 {
		occ = occ[:8]
	}
	writeJSON(w, occ)
}

// ---------- calendar ----------

func parseWhen(v string, def time.Time) time.Time {
	if v == "" {
		return def
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t
	}
	return def
}

func (s *Server) sends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	from := parseWhen(q.Get("from"), time.Now().AddDate(0, 0, -7))
	to := parseWhen(q.Get("to"), time.Now().AddDate(0, 0, 35))
	posts, err := s.DB.Posts(ctx)
	if err != nil {
		fail(w, 500, err)
		return
	}
	occ := store.Expand(posts, from, to)
	s.DB.AttachDeliveries(ctx, occ)
	pm := map[int64]*store.Post{}
	media := map[int64]*store.Media{}
	for _, p := range posts {
		pm[p.ID] = p
	}
	for _, o := range occ {
		for _, id := range o.Media {
			if _, ok := media[id]; !ok {
				if m, err := s.DB.Media(ctx, id); err == nil {
					media[id] = m
				}
			}
		}
	}
	used := map[int64]*store.Post{}
	for _, o := range occ {
		used[o.PostID] = pm[o.PostID]
	}
	writeJSON(w, map[string]any{"sends": occ, "posts": used, "media": media})
}

func (s *Server) sendDetail(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(r.URL.Query().Get("schedule_id"), 10, 64)
	writeJSON(w, s.DB.DeliveryDetails(r.Context(), sid, r.URL.Query().Get("occ")))
}

type sendRef struct {
	PostID     int64  `json:"post_id"`
	ScheduleID int64  `json:"schedule_id"`
	Occ        string `json:"occ"`
	To         string `json:"to"`    // local "YYYY-MM-DDTHH:MM" in the display time zone
	Scope      string `json:"scope"` // one (default) or all
	TZ         string `json:"tz"`    // zone "to" is written in (defaults to the display zone)
}

func (s *Server) moveSend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in sendRef
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	cur, err := s.DB.Post(ctx, in.PostID)
	if err != nil {
		fail(w, 404, err)
		return
	}
	sc := findSchedule(cur, in.ScheduleID)
	if sc == nil {
		fail(w, 404, errors.New("schedule not found"))
		return
	}
	to, err := toScheduleTZ(in.To, nz(in.TZ, s.displayTZ(ctx)), sc.TZ)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var summary string
	cid, err := s.DB.Mutate(ctx, actorOf(r), "", []store.Key{store.PostKey(cur.ID)}, func(tx *store.Tx) error {
		switch {
		case sc.RRule == "":
			sc.Start = to
			summary = fmt.Sprintf("moved %s to %s", title(cur), prettyOcc(to, sc.TZ))
		case in.Scope == "all":
			// Shift the whole series by the same amount, overrides included.
			eff := getOverride(sc, in.Occ).At
			if eff == "" {
				eff = in.Occ
			}
			a, _ := store.ParseLocal(eff, sc.TZ)
			b, _ := store.ParseLocal(to, sc.TZ)
			d := b.Sub(a)
			shift := func(v string) string {
				if v == "" {
					return v
				}
				t, err := store.ParseLocal(v, sc.TZ)
				if err != nil {
					return v
				}
				return t.Add(d).Format(store.Layout)
			}
			sc.Start, sc.Until = shift(sc.Start), shift(sc.Until)
			for i := range sc.Overrides {
				sc.Overrides[i].Occ, sc.Overrides[i].At = shift(sc.Overrides[i].Occ), shift(sc.Overrides[i].At)
			}
			days := int(d.Round(time.Hour).Hours() / 24)
			summary = fmt.Sprintf("moved every send of %s by %+d days", title(cur), days)
			if days == 0 {
				summary = fmt.Sprintf("moved every send of %s to %s", title(cur), b.Format("3:04 PM"))
			}
			// The rule's weekday must follow the move.
			if strings.Contains(sc.RRule, "BYDAY=") && days%7 != 0 {
				sc.RRule = shiftByDay(sc.RRule, days)
			}
		default:
			o := getOverride(sc, in.Occ)
			o.At = to
			if to == in.Occ {
				o.At = ""
			}
			setOverride(sc, o)
			summary = fmt.Sprintf("moved only the %s send of %s to %s", prettyOcc(in.Occ, sc.TZ), title(cur), prettyOcc(to, sc.TZ))
		}
		return tx.PutPost(cur)
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE changes SET summary=? WHERE id=?`, summary, cid)
	s.mutated(w, r, cid, map[string]any{"summary": summary})
}

var weekdays = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}

func shiftByDay(rule string, days int) string {
	parts := strings.Split(rule, ";")
	for i, p := range parts {
		if !strings.HasPrefix(p, "BYDAY=") {
			continue
		}
		var out []string
		for _, d := range strings.Split(strings.TrimPrefix(p, "BYDAY="), ",") {
			pre, wd := d[:len(d)-2], d[len(d)-2:]
			for j, w := range weekdays {
				if w == wd {
					wd = weekdays[((j+days)%7+7)%7]
					break
				}
			}
			out = append(out, pre+wd)
		}
		parts[i] = "BYDAY=" + strings.Join(out, ",")
	}
	return strings.Join(parts, ";")
}

func (s *Server) copySend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in sendRef
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	cur, err := s.DB.Post(ctx, in.PostID)
	if err != nil {
		fail(w, 404, err)
		return
	}
	tz := nz(in.TZ, s.displayTZ(ctx))
	if _, err := store.ParseLocal(in.To, tz); err != nil {
		fail(w, 400, err)
		return
	}
	np := *cur
	np.ID, np.CreatedAt = 0, 0
	if sc := findSchedule(cur, in.ScheduleID); sc != nil {
		o := getOverride(sc, in.Occ)
		if o.Caption != nil {
			np.Caption = *o.Caption
		}
		if o.Targets != nil {
			np.Targets = *o.Targets
		}
		if o.Media != nil {
			np.Media = *o.Media
		}
	}
	np.Status = "scheduled"
	np.Schedules = []store.Schedule{{Start: in.To, TZ: tz}}
	summary := fmt.Sprintf("copied %s to %s", title(cur), prettyOcc(in.To, tz))
	cid, err := s.DB.Mutate(ctx, actorOf(r), summary, nil, func(tx *store.Tx) error { return tx.PutPost(&np) })
	if err != nil {
		fail(w, 400, err)
		return
	}
	s.mutated(w, r, cid, map[string]any{"summary": summary, "post": np})
}

func (s *Server) skipSend(w http.ResponseWriter, r *http.Request) {
	var in sendRef
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	r.URL.RawQuery = fmt.Sprintf("scope=one&schedule_id=%d&occ=%s", in.ScheduleID, in.Occ)
	r.SetPathValue("id", strconv.FormatInt(in.PostID, 10))
	s.deletePost(w, r)
}

// ---------- history ----------

func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	cs := s.DB.Changes(r.Context(), 1000)
	lo, hi := pageBounds(w, r, len(cs))
	if r.URL.Query().Get("limit") == "" && r.URL.Query().Get("offset") == "" {
		hi = min(len(cs), 200) // the web app's history panel
	}
	writeJSON(w, cs[lo:hi])
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	f, err := s.DB.Undo(r.Context(), actorOf(r))
	if errors.Is(err, store.ErrNothing) {
		fail(w, 409, errors.New("nothing to undo"))
		return
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.mutated(w, r, f.Entry, map[string]any{"summary": "Undid: " + f.Summary, "undid": f.Target})
}

func (s *Server) redo(w http.ResponseWriter, r *http.Request) {
	f, err := s.DB.Redo(r.Context(), actorOf(r))
	if errors.Is(err, store.ErrNothing) {
		fail(w, 409, errors.New("nothing to redo"))
		return
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.mutated(w, r, f.Entry, map[string]any{"summary": "Redid: " + f.Summary, "redid": f.Target})
}
