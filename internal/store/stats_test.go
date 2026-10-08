package store

import (
	"context"
	"testing"
	"time"
)

func TestStatEventsCountOncePerPerson(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sched := time.Now().Add(-2 * time.Hour)
	db.RecordDelivery(ctx, 1, 9, "2026-10-08T06:00", "g@g.us", "sent", "A1", "")
	db.RecordStatSend(ctx, StatSend{PostID: 1, ScheduleID: 9, Occ: "2026-10-08T06:00", Chat: "g@g.us", Platform: "whatsapp", Kind: "image", Members: 10, SchedAt: sched},
		[]StatMsg{{Platform: "whatsapp", ID: "A1", Kind: "image"}, {Platform: "whatsapp", ID: "A2", Kind: "image"}})

	now := time.Now()
	if db.StatEvent(ctx, "whatsapp", "nope", "p1", "", "read", "", now) {
		t.Fatal("unknown message must not match")
	}
	db.StatEvent(ctx, "whatsapp", "A1", "p1", "", "delivered", "", now)
	db.StatEvent(ctx, "whatsapp", "A1", "p1", "", "read", "", now)
	db.StatEvent(ctx, "whatsapp", "A2", "p1", "", "read", "", now) // same person, second photo
	db.StatEvent(ctx, "whatsapp", "A1", "p2", "Sam", "read", "", now)
	db.StatEvent(ctx, "whatsapp", "A1", "p3", "", "react", "❤️", now)
	db.StatEvent(ctx, "whatsapp", "A1", "p3", "", "react", "👍", now) // changed reaction
	db.StatEvent(ctx, "whatsapp", "A1", "p4", "", "react", "👍", now)
	db.StatEvent(ctx, "whatsapp", "A1", "p4", "", "react", "", now) // removed
	db.StatEvent(ctx, "whatsapp", "A1", "p2", "", "reply:R1", "", now)
	db.StatEvent(ctx, "whatsapp", "A1", "p2", "", "reply:R2", "", now)
	db.StatEvent(ctx, "whatsapp", "A1", "p2", "", "reply:R2", "", now) // repeat event

	rows := db.StatRows(ctx, now.Add(-time.Hour), now.Add(time.Hour), 0)
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	r := rows[0]
	if r.Reads != 2 || r.Delivered != 2 || r.Reactions != 1 || r.Emoji["👍"] != 1 || r.Replies != 2 || r.Kind != "image" {
		t.Fatalf("got %+v", r)
	}
	// Names are dropped unless the option is on.
	for _, p := range db.StatPeople(ctx, []int64{r.DeliveryID}) {
		if p.Who != "" {
			t.Fatalf("name stored with option off: %+v", p)
		}
	}
	// Reach marks fill in once their time has passed.
	db.StatSnapshots(ctx, now.Add(2*time.Hour))
	r = db.StatRows(ctx, now.Add(-time.Hour), now.Add(time.Hour), 0)[0]
	if r.R1h == nil || *r.R1h != 2 || r.R24h != nil {
		t.Fatalf("snapshots: 1h=%v 24h=%v", r.R1h, r.R24h)
	}
	// Polled totals (views) take over reach when bigger.
	db.StatPolled(ctx, r.DeliveryID, 7, 1, 0, map[string]int{"🔥": 2})
	r = db.StatRows(ctx, now.Add(-time.Hour), now.Add(time.Hour), 0)[0]
	if r.Reach() != 7 || r.Reactions != 2 {
		t.Fatalf("polled: reach %d reactions %d", r.Reach(), r.Reactions)
	}
}
