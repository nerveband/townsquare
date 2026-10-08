package store

import (
	"context"
	"sort"
	"time"

	"github.com/teambition/rrule-go"
)

const Layout = "2006-01-02T15:04"

// Occurrence is one planned send of a post (a schedule expanded, overrides applied).
type Occurrence struct {
	PostID     int64     `json:"post_id"`
	ScheduleID int64     `json:"schedule_id"`
	Occ        string    `json:"occ"` // original local time, the stable key
	At         time.Time `json:"at"`  // effective time (UTC)
	TZ         string    `json:"tz"`
	Repeating  bool      `json:"repeating"`
	RRule      string    `json:"rrule"`
	Index      int       `json:"index"` // 0-based position in the series
	Edited     bool      `json:"edited"`
	Caption    string    `json:"caption"`
	Media      []int64   `json:"media"`
	Targets    []string  `json:"targets"`
	Status     string    `json:"status"` // post status
	Delivery   string    `json:"delivery"`
	Sent       int       `json:"sent"`
	Failed     int       `json:"failed"`
	Blocked    int       `json:"blocked"`
	Unsent     int       `json:"unsent"` // deleted for everyone afterwards
}

func loc(tz string) *time.Location {
	l, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return l
}

// ParseLocal parses "YYYY-MM-DDTHH:MM" in tz.
func ParseLocal(s, tz string) (time.Time, error) {
	return time.ParseInLocation(Layout, s, loc(tz))
}

// ScheduleTimes returns the raw rule times of s between from and to.
func ScheduleTimes(s Schedule, from, to time.Time) []time.Time {
	l := loc(s.TZ)
	start, err := time.ParseInLocation(Layout, s.Start, l)
	if err != nil {
		return nil
	}
	if s.RRule == "" {
		if !start.Before(from) && start.Before(to) {
			return []time.Time{start}
		}
		return nil
	}
	opt, err := rrule.StrToROptionInLocation(s.RRule, l)
	if err != nil {
		return nil
	}
	opt.Dtstart = start
	if s.Until != "" {
		if u, err := time.ParseInLocation(Layout, s.Until, l); err == nil {
			opt.Until = u
		}
	}
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil
	}
	return r.Between(from, to, true)
}

// Expand lists occurrences of posts with effective times in [from, to).
func Expand(posts []*Post, from, to time.Time) []Occurrence {
	out := []Occurrence{}
	pad := 62 * 24 * time.Hour
	for _, p := range posts {
		if p.Status == "draft" || p.Status == "archived" {
			continue
		}
		for _, s := range p.Schedules {
			l := loc(s.TZ)
			ov := map[string]Override{}
			for _, o := range s.Overrides {
				ov[o.Occ] = o
			}
			var first time.Time
			if st, err := time.ParseInLocation(Layout, s.Start, l); err == nil {
				first = st
			}
			times := ScheduleTimes(s, from.Add(-pad), to.Add(pad))
			idx := 0
			if s.RRule != "" && len(times) > 0 {
				idx = len(ScheduleTimes(s, first, times[0]))
				if idx > 0 {
					idx-- // Between is inclusive of times[0]
				}
			}
			for i, t := range times {
				key := t.In(l).Format(Layout)
				o := Occurrence{PostID: p.ID, ScheduleID: s.ID, Occ: key, At: t.UTC(), TZ: s.TZ, Repeating: s.RRule != "", RRule: s.RRule,
					Index: idx + i, Caption: p.Caption, Media: p.Media, Targets: p.Targets, Status: p.Status}
				if x, ok := ov[key]; ok {
					if x.Skipped {
						continue
					}
					o.Edited = true
					if x.At != "" {
						if at, err := time.ParseInLocation(Layout, x.At, l); err == nil {
							o.At = at.UTC()
						}
					}
					if x.Caption != nil {
						o.Caption = *x.Caption
					}
					if x.Media != nil {
						o.Media = *x.Media
					}
					if x.Targets != nil {
						o.Targets = *x.Targets
					}
				}
				if !o.At.Before(from) && o.At.Before(to) {
					out = append(out, o)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// AttachDeliveries fills delivery counts for each occurrence.
func (db *DB) AttachDeliveries(ctx context.Context, occ []Occurrence) {
	for i := range occ {
		rows, err := db.QueryContext(ctx, `SELECT state, COUNT(*) FROM deliveries WHERE schedule_id=? AND occ=? GROUP BY state`, occ[i].ScheduleID, occ[i].Occ)
		if err != nil {
			continue
		}
		for rows.Next() {
			var st string
			var n int
			_ = rows.Scan(&st, &n)
			switch st {
			case "sent":
				occ[i].Sent = n
			case "failed":
				occ[i].Failed = n
			case "blocked", "missed":
				occ[i].Blocked += n
			case "unsent":
				occ[i].Unsent = n
			}
		}
		rows.Close()
		switch {
		case occ[i].Unsent > 0 && occ[i].Sent == 0:
			occ[i].Delivery = "unsent" // deleted for everyone after it went out
		case occ[i].Failed > 0:
			occ[i].Delivery = "failed"
		case occ[i].Sent > 0 && occ[i].Sent+occ[i].Blocked >= len(occ[i].Targets):
			occ[i].Delivery = "sent"
		case occ[i].Sent > 0:
			occ[i].Delivery = "partial"
		case occ[i].Blocked > 0:
			occ[i].Delivery = "blocked"
		}
	}
}

// Delivered reports whether jid already has a final delivery for this occurrence.
func (db *DB) Delivered(ctx context.Context, scheduleID int64, occ, jid string) bool {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deliveries WHERE schedule_id=? AND occ=? AND jid=?`, scheduleID, occ, jid).Scan(&n)
	return n > 0
}

func (db *DB) RecordDelivery(ctx context.Context, postID, scheduleID int64, occ, jid, state, waID, errMsg string) {
	_, _ = db.ExecContext(ctx, `INSERT INTO deliveries(post_id,schedule_id,occ,jid,state,wa_id,error,at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(schedule_id,occ,jid) DO UPDATE SET state=excluded.state,wa_id=excluded.wa_id,error=excluded.error,at=excluded.at`,
		postID, scheduleID, occ, jid, state, waID, errMsg, time.Now().Unix())
}

// SentToday counts successful deliveries since local midnight in tz.
func (db *DB) SentToday(ctx context.Context, tz string) int {
	now := time.Now().In(loc(tz))
	mid := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deliveries WHERE state='sent' AND at>=?`, mid.Unix()).Scan(&n)
	return n
}

// DeliveryErrors returns failed/blocked rows for an occurrence.
func (db *DB) DeliveryDetails(ctx context.Context, scheduleID int64, occ string) []map[string]string {
	out := []map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT jid,state,error,wa_id FROM deliveries WHERE schedule_id=? AND occ=?`, scheduleID, occ)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var j, s, e, id string
		_ = rows.Scan(&j, &s, &e, &id)
		m := map[string]string{"jid": j, "state": s, "error": e}
		if id == "telegram-queue" {
			m["via"] = "telegram-queue"
		}
		out = append(out, m)
	}
	return out
}
