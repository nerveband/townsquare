package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerveband/townsquare/internal/store"
)

func TestUnlinkAlertIsOptIn(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{DB: db}
	if msg := s.unlinkAlert(ctx, "WhatsApp"); msg != "" {
		t.Fatalf("alerts must be off by default, got %q", msg)
	}
	put := func(body string) int {
		w := httptest.NewRecorder()
		s.saveSettings(w, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body)))
		return w.Code
	}
	if code := put(`{"alert_unlink":"yes"}`); code != 400 {
		t.Fatalf("bad value accepted: %d", code)
	}
	if code := put(`{"alert_unlink":"1"}`); code != 200 {
		t.Fatalf("turning on: %d", code)
	}
	msg := s.unlinkAlert(ctx, "WhatsApp (Second phone)")
	if !strings.Contains(msg, "WhatsApp (Second phone) was unlinked") || !strings.Contains(msg, "Link WhatsApp") {
		t.Fatalf("alert text: %q", msg)
	}
	s.Demo = true
	if msg := s.unlinkAlert(ctx, "WhatsApp"); msg != "" {
		t.Fatal("demo mode must never alert")
	}
}
