package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/wa"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
)

// After the phone removes the linked device, logout must clear the dead
// session (not say "isn't linked") and the status must read "not linked".
func TestLogoutClearsRemovedDevice(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir, err := os.MkdirTemp("", "ts-wa-removed-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	cli, err := wa.Open(ctx, dir, "ERROR")
	if err != nil {
		t.Fatal(err)
	}
	jid := types.JID{User: "15550000000", Device: 3, Server: types.DefaultUserServer}
	cli.Store.ID = &jid
	cli.Store.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := cli.Store.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cli.Store.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, WA: cli, DataDir: dir}

	w := httptest.NewRecorder()
	s.whatsappLogout(w, httptest.NewRequest("POST", "/api/whatsapp/logout", strings.NewReader("{}")))
	if w.Code != 200 {
		t.Fatalf("logout of a removed device: %d %s", w.Code, w.Body)
	}
	var st map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st["linked"] != false {
		t.Fatalf("still linked: %v", st)
	}
	if cli.Store.Deleted {
		t.Fatal("device still dead after logout")
	}
	// Nothing left to clear now.
	w = httptest.NewRecorder()
	s.whatsappLogout(w, httptest.NewRequest("POST", "/api/whatsapp/logout", strings.NewReader("{}")))
	if w.Code != 409 {
		t.Fatalf("second logout: %d", w.Code)
	}
}
