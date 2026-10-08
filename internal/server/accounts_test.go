package server

import (
	"context"
	"os"
	"testing"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/wa"
)

// Each chat posts from the account it belongs to.
func TestChatsRouteToTheirAccount(t *testing.T) {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "ts-accounts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	main, err := wa.Open(ctx, dir, "ERROR")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, WA: main, DataDir: dir}
	a, _ := db.AddAccount(ctx, "whatsapp", "Second phone")
	s.StartAccounts(ctx, "ERROR")
	t.Cleanup(s.CloseAccounts)
	x := s.extra(a.ID)
	if x == nil || x.wa == nil || x.wa == main {
		t.Fatal("extra account not opened with its own session")
	}
	if c, _ := s.waFor("120363000000000102@g.us"); c != main {
		t.Fatal("default chat should use the first account")
	}
	if c, ok := s.waFor("wa@2:120363000000000102@g.us"); c != x.wa || ok {
		t.Fatalf("extra chat: client %v ok %v (not linked, so not ready)", c == x.wa, ok)
	}
	if c, _ := s.waFor("wa@9:1@g.us"); c != nil {
		t.Fatal("unknown account should have no client")
	}
	if s.tgFor("tg@9:ch:1:2") != nil || s.ready("tg@9:ch:1:2") || s.ready("tgbot:5") {
		t.Fatal("missing accounts must not be ready")
	}
	if s.ready("wa@2:1@g.us") {
		t.Fatal("an unlinked extra account must not be ready")
	}
}
