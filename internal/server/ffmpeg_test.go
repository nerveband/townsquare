package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The incident: a Homebrew upgrade left ffmpeg unable to start. The endpoint behind
// `townsquare doctor` must report that with ffmpeg's own error text, and the server
// must remember it for status.
func TestFFmpegEndpointReportsBrokenBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		script := "#!/bin/sh\necho 'dyld: Library not loaded: libx265.215.dylib' >&2\nexit 134\n"
		if err := os.WriteFile(filepath.Join(dir, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TOWNSQUARE_FFMPEG_DIR", dir)

	s := &Server{}
	if s.ffmpegLast() != nil {
		t.Fatal("no check has run yet")
	}
	rec := httptest.NewRecorder()
	s.ffmpegNow(rec, httptest.NewRequest("GET", "/api/v1/ffmpeg", nil))
	var got struct {
		OK    bool   `json:"ok"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.OK || got.Path != filepath.Join(dir, "ffmpeg") || !strings.Contains(got.Error, "Library not loaded") {
		t.Fatalf("want red with the binary path and dyld error, got %+v", got)
	}
	if last := s.ffmpegLast(); last == nil || last.OK {
		t.Fatalf("status should remember the failed check, got %+v", last)
	}
}
