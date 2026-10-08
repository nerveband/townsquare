package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.6.1", "v0.6.0", true},
		{"v0.6.0", "v0.6.0", false},
		{"v0.6.0", "v0.6.1", false},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v1.0.0-rc.2", true},
		{"v1.0.0-rc.2", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.1", "v1.0.0", false},
		{"v0.6.0", "v0.5.0-23-g659bbb5", true},
		{"v0.5.0", "v0.5.0-23-g659bbb5", false},
		{"v0.6.0", "dev", false},
		{"garbage", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if IsRelease("v0.5.0-23-g659bbb5") || IsRelease("dev") || !IsRelease("v0.6.0") {
		t.Fatal("IsRelease")
	}
}

// fakeRelease serves a signed latest.json and one binary, like GitHub's latest/download.
func fakeRelease(t *testing.T, version string, bin []byte, priv ed25519.PrivateKey) *httptest.Server {
	sum := sha256.Sum256(bin)
	name := "townsquare-" + version + "-" + Platform()
	m := Manifest{Version: version, Files: map[string]File{Platform(): {Name: name, SHA256: hex.EncodeToString(sum[:])}}}
	body, _ := json.Marshal(m)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			_, _ = w.Write(body)
		case "/latest.json.sig":
			_, _ = w.Write([]byte(sig))
		case "/" + name:
			_, _ = w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
}

func testKey(t *testing.T) ed25519.PrivateKey {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := PublicKeys
	PublicKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	t.Cleanup(func() { PublicKeys = old })
	return priv
}

func TestUpdateStagesVerifiedBinary(t *testing.T) {
	priv := testKey(t)
	bin := []byte("#!/bin/sh\necho new\n")
	srv := fakeRelease(t, "v0.7.0", bin, priv)
	defer srv.Close()
	dir := t.TempDir()
	u := New(dir, "v0.6.0")
	u.Base = srv.URL
	v, err := u.Update(context.Background())
	if err != nil || v != "v0.7.0" {
		t.Fatalf("Update = %q, %v", v, err)
	}
	p, ok := StagedPath(dir, "v0.6.0")
	if !ok || filepath.Base(p) != "townsquare-v0.7.0"+exeSuffix() {
		t.Fatalf("StagedPath = %q, %v", p, ok)
	}
	if _, ok := StagedPath(dir, "v0.7.0"); ok {
		t.Fatal("same version must not hand off")
	}
	if st := u.State(); !st.Available || st.Staged != "v0.7.0" || st.Latest != "v0.7.0" {
		t.Fatalf("state %+v", st)
	}
	// A tampered binary is never used.
	_ = os.WriteFile(p, []byte("evil"), 0o755)
	if _, ok := StagedPath(dir, "v0.6.0"); ok {
		t.Fatal("tampered binary accepted")
	}
	// Already newest: nothing to do.
	u2 := New(t.TempDir(), "v0.7.0")
	u2.Base = srv.URL
	if v, err := u2.Update(context.Background()); err != nil || v != "" {
		t.Fatalf("up to date: %q, %v", v, err)
	}
}

func TestBadSignatureRejected(t *testing.T) {
	testKey(t)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	srv := fakeRelease(t, "v0.7.0", []byte("x"), other)
	defer srv.Close()
	u := New(t.TempDir(), "v0.6.0")
	u.Base = srv.URL
	if _, err := u.Update(context.Background()); err == nil {
		t.Fatal("unsigned release accepted")
	}
	if u.State().Error == "" {
		t.Fatal("error not reported")
	}
}

func exeSuffix() string {
	if Platform()[:7] == "windows" {
		return ".exe"
	}
	return ""
}

func TestBadVersionIsSkipped(t *testing.T) {
	priv := testKey(t)
	srv := fakeRelease(t, "v0.7.0", []byte("x"), priv)
	defer srv.Close()
	dir := t.TempDir()
	u := New(dir, "v0.6.0")
	u.Base = srv.URL
	if _, err := u.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	// "x" is not a program: the probe fails, marks it bad, and it is never downloaded again.
	if err := Probe(dir, "v0.6.0"); err == nil {
		t.Fatal("probe passed for a broken binary")
	}
	if _, ok := StagedPath(dir, "v0.6.0"); ok || !IsBad(dir, "v0.7.0") {
		t.Fatal("broken version still staged")
	}
	if _, err := u.Update(context.Background()); err == nil {
		t.Fatal("bad version downloaded again")
	}
}
