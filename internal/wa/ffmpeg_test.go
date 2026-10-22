package wa

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeTools writes ffmpeg and ffprobe shell scripts into a new folder and returns it.
func fakeTools(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-ins")
	}
	dir := t.TempDir()
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The incident: ffmpeg is installed but dies at start because a library it needs changed.
func TestCheckFFmpegBrokenBinaryIsRed(t *testing.T) {
	dir := fakeTools(t, `echo "dyld[1]: Library not loaded: /opt/homebrew/opt/x265/lib/libx265.215.dylib" >&2; exit 134`)
	t.Setenv("TOWNSQUARE_FFMPEG_DIR", dir)
	st := CheckFFmpeg(context.Background())
	if st.OK {
		t.Fatal("a binary that can't start must fail the check")
	}
	if st.Path != filepath.Join(dir, "ffmpeg") {
		t.Errorf("path = %q, want the broken binary in %s", st.Path, dir)
	}
	if !strings.Contains(st.Error, "Library not loaded") {
		t.Errorf("error should carry ffmpeg's own words, got %q", st.Error)
	}
}

// ffmpeg starts and says its version but can't encode (missing codec): still red.
func TestCheckFFmpegVersionOKButEncodeFails(t *testing.T) {
	dir := fakeTools(t, `case "$*" in *-version*) echo "ffmpeg version 9.9.9 test";; *) echo "Unknown encoder 'libx264'" >&2; exit 1;; esac`)
	t.Setenv("TOWNSQUARE_FFMPEG_DIR", dir)
	st := CheckFFmpeg(context.Background())
	if st.OK || st.Version != "9.9.9" {
		t.Fatalf("want a red check that still reports the version, got %+v", st)
	}
	if !strings.Contains(st.Error, "Unknown encoder") {
		t.Errorf("error = %q", st.Error)
	}
}

// The copy that ships with the app beats PATH and Homebrew. Only a missing copy falls back.
func TestToolPrefersBundled(t *testing.T) {
	dir := fakeTools(t, `exit 0`)
	onPath := fakeTools(t, `exit 0`)
	t.Setenv("PATH", onPath+string(os.PathListSeparator)+os.Getenv("PATH"))

	t.Setenv("TOWNSQUARE_FFMPEG_DIR", dir)
	if p, bundled := ToolSource("ffmpeg"); p != filepath.Join(dir, "ffmpeg") || !bundled {
		t.Errorf("with a bundled copy: %q bundled=%v, want %s", p, bundled, dir)
	}

	t.Setenv("TOWNSQUARE_FFMPEG_DIR", t.TempDir()) // nothing in it
	if p, bundled := ToolSource("ffmpeg"); p != filepath.Join(onPath, "ffmpeg") || bundled {
		t.Errorf("without a bundled copy: %q bundled=%v, want the PATH one %s", p, bundled, onPath)
	}
}

func TestCheckFFmpegWorks(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil && Tool("ffmpeg") == "" {
		t.Skip("ffmpeg not installed")
	}
	st := CheckFFmpeg(context.Background())
	if !st.OK {
		t.Fatalf("real ffmpeg failed the check: %+v", st)
	}
	if st.Path == "" || st.Version == "" || st.ProbePath == "" {
		t.Errorf("a green check names the binary and version: %+v", st)
	}
}
