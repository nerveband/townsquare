package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nerveband/townsquare/internal/wa"
)

func TestAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := allowed(dir, wa.Target{JID: "1@s.whatsapp.net"}); err == nil {
		t.Fatal("empty allowlist must block")
	}
	_ = os.WriteFile(filepath.Join(dir, "allow.txt"), []byte("1@s.whatsapp.net\n"), 0o600)
	if err := allowed(dir, wa.Target{JID: "1@s.whatsapp.net"}); err != nil {
		t.Fatal(err)
	}
	if err := allowed(dir, wa.Target{JID: "123-456@g.us"}); err == nil {
		t.Fatal("unlisted group must block")
	}
}
