package store

import (
	"context"
	"testing"
)

// Syncing one account's chats must never mark another account's chats gone.
func TestAccountsKeepTheirOwnChats(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, err := db.AddAccount(ctx, "whatsapp", "Second phone")
	if err != nil || a.ID < 2 {
		t.Fatalf("add: %+v %v", a, err)
	}
	_ = db.UpsertTargets(ctx, "whatsapp", []Target{{JID: "1@g.us", Kind: "group", Name: "Main", CanSend: true}})
	_ = db.UpsertAccountTargets(ctx, "whatsapp", a.ID, []Target{{JID: "wa@2:1@g.us", Kind: "group", Name: "Main (second)", CanSend: true}})
	// The default account syncs again: its own list doesn't include the extra account's chat.
	_ = db.UpsertTargets(ctx, "whatsapp", []Target{{JID: "1@g.us", Kind: "group", Name: "Main", CanSend: true}})
	got := map[string]Target{}
	for _, x := range db.Targets(ctx) {
		got[x.JID] = x
	}
	if got["wa@2:1@g.us"].Gone || got["wa@2:1@g.us"].Account != a.ID || got["1@g.us"].Account != 0 {
		t.Fatalf("targets: %+v", got)
	}
	// Removing the account marks only its chats gone.
	if err := db.RemoveAccount(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got = map[string]Target{}
	for _, x := range db.Targets(ctx) {
		got[x.JID] = x
	}
	if !got["wa@2:1@g.us"].Gone || got["1@g.us"].Gone || len(db.Accounts(ctx)) != 0 {
		t.Fatalf("after remove: %+v", got)
	}
}
