package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExpandDSTAndOverrides(t *testing.T) {
	p := &Post{ID: 1, Status: "scheduled", Caption: "hi", Targets: []string{"a"}, Schedules: []Schedule{{
		ID: 7, Start: "2026-03-04T18:30", TZ: "America/New_York", RRule: "FREQ=WEEKLY;BYDAY=WE",
		Overrides: []Override{{Occ: "2026-03-18T18:30", At: "2026-03-19T09:00"}, {Occ: "2026-03-25T18:30", Skipped: true}},
	}}}
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	occ := Expand([]*Post{p}, from, from.AddDate(0, 0, 31))
	ny, _ := time.LoadLocation("America/New_York")
	var got []string
	for _, o := range occ {
		got = append(got, o.At.In(ny).Format(Layout))
	}
	want := []string{"2026-03-04T18:30", "2026-03-11T18:30", "2026-03-19T09:00"} // DST starts Mar 8; 25th skipped
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if occ[2].Index != 2 || !occ[2].Edited {
		t.Fatalf("index/edited wrong: %+v", occ[2])
	}
	if occ[0].At.Hour() != 23 || occ[1].At.Hour() != 22 { // EST vs EDT in UTC
		t.Fatalf("DST not respected: %v %v", occ[0].At, occ[1].At)
	}
}

func TestMutateUndoRedo(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p := &Post{Title: "Flyer", Caption: "v1", Status: "scheduled", Targets: []string{"g1"},
		Schedules: []Schedule{{Start: "2026-05-06T18:30", TZ: "America/New_York", RRule: "FREQ=WEEKLY"}}}
	if _, err := db.Mutate(ctx, "you", "create", nil, func(tx *Tx) error { return tx.PutPost(p) }); err != nil {
		t.Fatal(err)
	}
	sid := p.Schedules[0].ID
	if _, err := db.Mutate(ctx, "you", "edit", []Key{PostKey(p.ID)}, func(tx *Tx) error {
		cur, _ := tx.Post(p.ID)
		cur.Caption = "v2"
		cur.Targets = []string{"g1", "g2"}
		return tx.PutPost(cur)
	}); err != nil {
		t.Fatal(err)
	}
	cur, _ := db.Post(ctx, p.ID)
	if cur.Caption != "v2" || len(cur.Targets) != 2 || cur.Schedules[0].ID != sid {
		t.Fatalf("edit not applied or schedule id changed: %+v", cur)
	}
	if f, err := db.Undo(ctx, "you"); err != nil || f.Summary != "edit" || f.Entry == 0 || f.Target == 0 {
		t.Fatal(f, err)
	}
	if h := db.Changes(ctx, 1); len(h) != 1 || h[0].Summary != "undid: edit" {
		t.Fatalf("undo not recorded in history: %+v", h)
	}
	cur, _ = db.Post(ctx, p.ID)
	if cur.Caption != "v1" || len(cur.Targets) != 1 {
		t.Fatalf("undo failed: %+v", cur)
	}
	if _, err := db.Redo(ctx, "you"); err != nil {
		t.Fatal(err)
	}
	cur, _ = db.Post(ctx, p.ID)
	if cur.Caption != "v2" {
		t.Fatalf("redo failed: %+v", cur)
	}
	// Undo twice removes the post entirely; redo brings it back.
	_, _ = db.Undo(ctx, "you")
	_, _ = db.Undo(ctx, "you")
	if _, err := db.Post(ctx, p.ID); err != ErrNotFound {
		t.Fatalf("want post gone after undoing create, got %v", err)
	}
	_, _ = db.Redo(ctx, "you")
	if cur, _ = db.Post(ctx, p.ID); cur == nil || cur.Caption != "v1" {
		t.Fatalf("redo create failed: %+v", cur)
	}
	// A new change clears the redo stack.
	_, _ = db.Mutate(ctx, "you", "other", []Key{PostKey(p.ID)}, func(tx *Tx) error { return nil })
	if _, err := db.Redo(ctx, "you"); err != ErrNothing {
		t.Fatalf("redo should be cleared, got %v", err)
	}
}

func TestMigrateIgnoresJIDsAndRepairsHugeIDs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, _ = db.ExecContext(ctx, `INSERT INTO changes(at,actor,summary,items) VALUES(1,'you','x',?)`,
		`[{"type":"target","id":"120363000000000447@g.us","before":null,"after":null},{"type":"post","id":"120363000000000448","before":null,"after":{"id":120363000000000448,"schedules":[{"id":120363000000000449,"start":"2026-01-01T09:00","tz":"UTC"}]}}]`)
	_, _ = db.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=120363000000000447`)
	db.Close()
	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var seq int64
	_ = db.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name='posts'`).Scan(&seq)
	if seq >= maxSafeID || seq < 2 {
		t.Fatalf("posts sequence not repaired: %d", seq)
	}
	var items string
	_ = db.QueryRowContext(ctx, `SELECT items FROM changes`).Scan(&items)
	if strings.Contains(items, `"id":120363000000000448`) || !strings.Contains(items, "120363000000000447@g.us") {
		t.Fatalf("history not renumbered correctly: %s", items)
	}
}
