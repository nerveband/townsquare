package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nerveband/townsquare/internal/store"
)

type fakeOps struct {
	revoked []string
	edited  map[string]string
	failOn  string
}

func (f *fakeOps) revoke(_ context.Context, m store.SentMsg) error {
	if m.MsgID == f.failOn {
		return errors.New("boom")
	}
	f.revoked = append(f.revoked, m.JID+"/"+m.MsgID)
	return nil
}

func (f *fakeOps) edit(_ context.Context, m store.SentMsg, text string) error {
	if f.edited == nil {
		f.edited = map[string]string{}
	}
	f.edited[m.JID+"/"+m.MsgID] = text
	return nil
}

func TestUnsendAndEdit(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ops := &fakeOps{}
	s := &Server{DB: db, msgOps: ops}
	const sid, occ = int64(7), "2026-10-08T16:00"
	wag, tgc, old := "120363000000000102@g.us", "tg:ch:1:2", "120363000000000103@g.us"
	for _, d := range []struct {
		jid  string
		msgs []store.StatMsg
	}{
		{wag, []store.StatMsg{{Platform: "whatsapp", ID: "W1", Kind: "image", Text: true}, {Platform: "whatsapp", ID: "W2", Kind: "image"}}},
		{tgc, []store.StatMsg{{Platform: "telegram", ID: "11", Kind: "text", Text: true}}},
		{old, []store.StatMsg{{Platform: "whatsapp", ID: "W3", Kind: "text", Text: true}}},
	} {
		db.RecordDelivery(ctx, 1, sid, occ, d.jid, "sent", d.msgs[0].ID, "")
		db.RecordSent(ctx, sid, occ, d.jid, d.msgs)
	}
	db.RecordDelivery(ctx, 1, sid, occ, "tg:ch:3:4", "sent", "telegram-queue", "")
	db.RecordDelivery(ctx, 1, sid, occ, "120363000000000104@g.us", "blocked", "", "safe mode")
	// The third chat's send is 3 days old: past WhatsApp's delete window.
	_, _ = db.ExecContext(ctx, `UPDATE sent_msgs SET sent_at=? WHERE jid=?`, time.Now().Add(-72*time.Hour).Unix(), old)

	res := func(rs []ChatResult) map[string]string {
		m := map[string]string{}
		for _, r := range rs {
			m[r.JID] = r.Result
		}
		return m
	}
	// Dry run: nothing is touched.
	rs, err := s.takeBack(ctx, takeBackIn{ScheduleID: sid, Occ: occ, DryRun: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := res(rs)
	if got[wag] != "would_delete" || got[tgc] != "would_delete" || got[old] != "too_old" || got["tg:ch:3:4"] != "not_supported" ||
		got["120363000000000104@g.us"] != "not_sent" || len(ops.revoked) != 0 {
		t.Fatalf("dry run: %v, revoked %v", got, ops.revoked)
	}
	// Edit: WhatsApp photo captions can't be edited; Telegram text can.
	rs, _ = s.takeBack(ctx, takeBackIn{ScheduleID: sid, Occ: occ, Caption: "Fixed typo"}, true)
	got = res(rs)
	if got[wag] != "cant_edit" || got[tgc] != "edited" || got[old] != "too_old" || ops.edited[tgc+"/11"] != "Fixed typo" {
		t.Fatalf("edit: %v %v", got, ops.edited)
	}
	// Unsend, with one message failing: the chat reports it, the rest are deleted.
	ops.failOn = "W2"
	rs, _ = s.takeBack(ctx, takeBackIn{ScheduleID: sid, Occ: occ}, false)
	got = res(rs)
	if got[wag] != "failed" || got[tgc] != "deleted" {
		t.Fatalf("unsend: %v", got)
	}
	// Retry: only what's left is sent; then everything reports already_deleted.
	ops.failOn = ""
	rs, _ = s.takeBack(ctx, takeBackIn{ScheduleID: sid, Occ: occ, Chats: []string{wag, tgc}}, false)
	got = res(rs)
	if got[wag] != "deleted" || got[tgc] != "already_deleted" {
		t.Fatalf("retry: %v", got)
	}
	if n := len(ops.revoked); n != 3 { // W1, 11, then W2
		t.Fatalf("revoked %v", ops.revoked)
	}
	for _, d := range db.DeliveryDetails(ctx, sid, occ) {
		if (d["jid"] == wag || d["jid"] == tgc) && d["state"] != "unsent" {
			t.Fatalf("delivery %s state %s", d["jid"], d["state"])
		}
	}
}

func TestSendDelayHoldsSends(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{DB: db}
	at := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Minute)
	if _, err := db.ExecContext(ctx, `INSERT INTO posts(title,caption,media,targets,status,created_at,updated_at) VALUES('Digest','x','[]','["1@g.us"]','scheduled',0,0)`); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO schedules(post_id,start,tz,rrule,until) VALUES(1,?,'UTC','','')`, at.Format("2006-01-02T15:04"))
	_, _ = db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('send_delay','120') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	rec := httptestGet(t, s.pendingSends, "/api/v1/sends/pending")
	items, _ := rec["items"].([]any)
	if rec["delay_seconds"] != float64(120) || len(items) != 1 {
		t.Fatalf("pending: %v", rec)
	}
	left := items[0].(map[string]any)["seconds_left"].(float64)
	if left < 30 || left > 120 {
		t.Fatalf("seconds_left %v", left)
	}
	// The send loop leaves it alone until the pause is over.
	posts, _ := db.Posts(ctx, "scheduled")
	now := time.Now()
	from, to := dueRange(now, 15*time.Minute, 120*time.Second)
	if len(store.Expand(posts, from, to)) != 0 {
		t.Fatal("due during the pause")
	}
	from, to = dueRange(now.Add(2*time.Minute), 15*time.Minute, 120*time.Second)
	if len(store.Expand(posts, from, to)) != 1 {
		t.Fatal("not due after the pause")
	}
	from, to = dueRange(now, 15*time.Minute, 0)
	if len(store.Expand(posts, from, to)) != 1 {
		t.Fatal("without a pause it is due right away")
	}
}
