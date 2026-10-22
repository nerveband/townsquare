package wa

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// zipOf makes a zip holding one program, as the builder's downloads do.
func zipOf(t *testing.T, name, script string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte(script))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// fakeBuilder serves ffmpeg.zip and ffprobe.zip for a pinned build and points the
// installer at it. serve lets a test tamper with what is sent.
func fakeBuilder(t *testing.T, script string, serve func(tool string, zipped []byte) ([]byte, int)) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-ins")
	}
	zips := map[string][]byte{"ffmpeg": zipOf(t, "ffmpeg", script), "ffprobe": zipOf(t, "ffprobe", script)}
	pin := ffPin{OS: "testos", Arch: "testarch", ID: "42_1.0", Sums: map[string]string{"ffmpeg": sum(zips["ffmpeg"]), "ffprobe": sum(zips["ffprobe"])}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tool := strings.TrimSuffix(filepath.Base(r.URL.Path), ".zip")
		if !strings.HasPrefix(r.URL.Path, "/testos/testarch/42_1.0/") || zips[tool] == nil {
			http.NotFound(w, r)
			return
		}
		body, code := zips[tool], 200
		if serve != nil {
			body, code = serve(tool, body)
		}
		w.WriteHeader(code)
		_, _ = w.Write(body)
	}))
	oldPins, oldBase, oldDir := ffPins, ffBaseURL, ManagedFFmpegDir
	ffPins = map[string]ffPin{runtime.GOOS + "/" + runtime.GOARCH: pin}
	ffBaseURL = srv.URL
	ManagedFFmpegDir = filepath.Join(t.TempDir(), "ffmpeg")
	t.Cleanup(func() { srv.Close(); ffPins, ffBaseURL, ManagedFFmpegDir = oldPins, oldBase, oldDir })
}

const okScript = "#!/bin/sh\necho 'ffmpeg version 1.0-test'\n"

func TestInstallFFmpegDownloadsVerifiesAndPrefersIt(t *testing.T) {
	fakeBuilder(t, okScript, nil)
	t.Setenv("TOWNSQUARE_FFMPEG_DIR", "")
	if !NeedsFFmpegInstall() {
		t.Fatal("an empty folder needs the download")
	}
	if err := InstallFFmpeg(context.Background()); err != nil {
		t.Fatal(err)
	}
	if NeedsFFmpegInstall() {
		t.Error("an installed pinned build needs nothing more")
	}
	for _, n := range []string{"ffmpeg", "ffprobe", "NOTICE.txt", "COPYING.GPLv3"} {
		if !isFile(filepath.Join(ManagedFFmpegDir, n)) {
			t.Errorf("%s missing after install", n)
		}
	}
	if p, own := ToolSource("ffmpeg"); p != filepath.Join(ManagedFFmpegDir, "ffmpeg") || !own {
		t.Errorf("the downloaded copy must win over PATH, got %q own=%v", p, own)
	}
	if st := CurrentInstall(); st.State != "idle" || !st.Available {
		t.Errorf("install state = %+v", st)
	}
}

// A file that isn't the pinned build must never be used, and must not replace what works.
func TestInstallFFmpegRefusesWrongChecksum(t *testing.T) {
	fakeBuilder(t, okScript, func(tool string, zipped []byte) ([]byte, int) {
		return zipOf(t, tool, "#!/bin/sh\necho evil\n"), 200
	})
	if err := os.MkdirAll(ManagedFFmpegDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(ManagedFFmpegDir, "ffmpeg")
	if err := os.WriteFile(old, []byte("old copy"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := InstallFFmpeg(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
	if b, _ := os.ReadFile(old); string(b) != "old copy" {
		t.Error("a refused download replaced the existing ffmpeg")
	}
	if NeedsFFmpegInstall() {
		t.Error("a bad checksum is final for this build; don't re-download every 10 minutes")
	}
	if st := CurrentInstall(); st.State != "failed" || st.Error == "" {
		t.Errorf("install state = %+v", st)
	}
}

// The incident class: a build that downloads fine but can't start here. Keep what works.
func TestInstallFFmpegRefusesBuildThatCannotRun(t *testing.T) {
	fakeBuilder(t, "#!/bin/sh\necho 'Library not loaded: libm.so.6' >&2\nexit 127\n", nil)
	err := InstallFFmpeg(context.Background())
	if err == nil || !strings.Contains(err.Error(), "doesn't run") || !strings.Contains(err.Error(), "Library not loaded") {
		t.Fatalf("got %v", err)
	}
	if isFile(filepath.Join(ManagedFFmpegDir, "ffmpeg")) {
		t.Error("a build that can't run was installed")
	}
	if NeedsFFmpegInstall() {
		t.Error("a build that can't run here is final for this build")
	}
}

// A flaky connection is not final: the next check tries again.
func TestInstallFFmpegNetworkFailureIsRetried(t *testing.T) {
	fakeBuilder(t, okScript, func(string, []byte) ([]byte, int) { return []byte("busy"), 503 })
	if err := InstallFFmpeg(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	if !NeedsFFmpegInstall() {
		t.Error("a failed connection should be retried")
	}
	if st := CurrentInstall(); st.State != "failed" {
		t.Errorf("install state = %+v", st)
	}
}
