package tg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAppOrder(t *testing.T) {
	dir := t.TempDir()
	old := builtinApp
	t.Cleanup(func() { builtinApp = old })
	h := func(c byte) string {
		b := make([]byte, 32)
		for i := range b {
			b[i] = c
		}
		return string(b)
	}

	builtinApp = ""
	if a, src, err := ResolveApp(dir); err != nil || a.Valid() || src != "" {
		t.Fatalf("nothing: %+v %q %v", a, src, err)
	}
	builtinApp = "111:" + h('a')
	if a, src, _ := ResolveApp(dir); a.ID != 111 || src != "built-in" {
		t.Fatalf("built-in: %+v %q", a, src)
	}
	// A new shared id over the air replaces the built-in one.
	changed, err := SaveSharedApp(dir, App{ID: 222, Hash: h('b')})
	if err != nil || !changed {
		t.Fatalf("save shared: %v %v", changed, err)
	}
	if changed, _ := SaveSharedApp(dir, App{ID: 222, Hash: h('b')}); changed {
		t.Fatal("same id reported as changed")
	}
	if a, src, _ := ResolveApp(dir); a.ID != 222 || src != "shared" {
		t.Fatalf("shared: %+v %q", a, src)
	}
	// The user's own id wins.
	if err := SaveOwnApp(dir, App{ID: 333, Hash: h('c')}); err != nil {
		t.Fatal(err)
	}
	if a, src, _ := ResolveApp(dir); a.ID != 333 || src != "own" {
		t.Fatalf("own: %+v %q", a, src)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "telegram.app")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("telegram.app mode %v", fi.Mode().Perm())
	}
	_ = RemoveOwnApp(dir)
	if _, src, _ := ResolveApp(dir); src != "shared" {
		t.Fatalf("after reset: %q", src)
	}
	if _, err := ParseApp("12:short"); err == nil {
		t.Fatal("short hash accepted")
	}
}
