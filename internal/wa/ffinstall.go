package wa

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Townsquare downloads its own static ffmpeg and ffprobe once and uses them before
// anything from PATH or Homebrew. A Homebrew upgrade that breaks the shared libraries
// of the system ffmpeg can then no longer break media uploads. Each download is checked
// against a SHA-256 sum built into this program (so a new ffmpeg ships with a new
// Townsquare release, and a tampered file is refused) and must start before it is used.

//go:embed ffnotice/NOTICE.txt ffnotice/COPYING.GPLv3
var ffNotice embed.FS

// ManagedFFmpegDir is where the downloaded ffmpeg and ffprobe live (<data folder>/ffmpeg).
// "" turns downloading off (demo mode, tests). Set once at start-up.
var ManagedFFmpegDir string

// ffBaseURL is the builder's download root (tests point it at a local server).
var ffBaseURL = "https://ffmpeg.martin-riedl.de/download"

// ffPin names one verified build per platform: the builder's folder and the SHA-256 of
// each zip. To move to a new FFmpeg: change the id and sums (shasum -a 256 the zips),
// update ffnotice/NOTICE.txt, and check the new build runs and links only system libraries.
type ffPin struct {
	OS, Arch, ID string
	Sums         map[string]string // zip SHA-256 per program
}

var ffPins = map[string]ffPin{
	"darwin/arm64": {OS: "macos", Arch: "arm64", ID: "1789931890_9.0.2", Sums: map[string]string{
		"ffmpeg":  "c8ed4c4e6978a03c485edbfe4e0a5dc2380f8a30bba5150531b31b094492d924",
		"ffprobe": "fcbe839537485eaee7a7a8bc5cbc0f90d53617e80943e8a5b2e31cb851197ea6"}},
	"darwin/amd64": {OS: "macos", Arch: "amd64", ID: "1789931006_9.0.2", Sums: map[string]string{
		"ffmpeg":  "7c6b4125b191cbf773832dc51f424cf2b6bb7da43007d1e066f95909e47cacd4",
		"ffprobe": "2322438ed2f6319a691291b247d09c69dcaa3a982460d1f269a7e1af335cfdfd"}},
	"linux/amd64": {OS: "linux", Arch: "amd64", ID: "1789931100_9.0.2", Sums: map[string]string{
		"ffmpeg":  "fa8ecf4abbd290d98f7d188b8649cc6b391ae209a98452be955a15aab1909d7f",
		"ffprobe": "3f428c49070be3d24ec338602b76d412e401ffcb8a5641ef0e729181a232fc32"}},
	"linux/arm64": {OS: "linux", Arch: "arm64", ID: "1789931697_9.0.2", Sums: map[string]string{
		"ffmpeg":  "93a76ae90db5474eecdf951a729857c64f3de23567228d6a7d5e6e8e3cd1021b",
		"ffprobe": "bcbe80fb741c180083327afaf5434812e006b33cacde2016b9aeaf6936128330"}},
}

const (
	maxZipBytes  = 300 << 20 // the biggest zip is about 34 MB
	maxToolBytes = 400 << 20 // the biggest program is about 95 MB
)

// ErrNoFFmpegDownload means this computer has no pinned build (Windows, 32-bit ARM).
var ErrNoFFmpegDownload = errors.New("there is no ffmpeg download for this computer; install ffmpeg yourself " +
	"(Windows: winget install ffmpeg, Linux: sudo apt install ffmpeg)")

// FFmpegInstall says where the download stands.
type FFmpegInstall struct {
	State     string `json:"state"`           // idle, downloading or failed
	Available bool   `json:"available"`       // Townsquare has a download for this computer
	Error     string `json:"error,omitempty"` // why the last try failed
}

var inst struct {
	mu      sync.Mutex
	running bool
	err     string
}

type idMark struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}

func ffPinNow() (ffPin, bool) {
	p, ok := ffPins[runtime.GOOS+"/"+runtime.GOARCH]
	return p, ok
}

func readMark(name string) (m idMark, ok bool) {
	b, err := os.ReadFile(filepath.Join(ManagedFFmpegDir, name))
	return m, err == nil && json.Unmarshal(b, &m) == nil
}

// NeedsFFmpegInstall is true when Townsquare should download its ffmpeg now: it has a
// build for this computer, the folder doesn't hold that build yet, and an earlier try
// didn't fail for good (a corrupt download or a build that can't run here).
func NeedsFFmpegInstall() bool {
	pin, ok := ffPinNow()
	if !ok || ManagedFFmpegDir == "" {
		return false
	}
	if m, ok := readMark("installed.json"); ok && m.ID == pin.ID && isFile(filepath.Join(ManagedFFmpegDir, "ffmpeg")) && isFile(filepath.Join(ManagedFFmpegDir, "ffprobe")) {
		return false
	}
	if m, ok := readMark("failed.json"); ok && m.ID == pin.ID {
		return false
	}
	return true
}

// CurrentInstall reports the download state for status and doctor.
func CurrentInstall() FFmpegInstall {
	pin, ok := ffPinNow()
	st := FFmpegInstall{State: "idle", Available: ok && ManagedFFmpegDir != ""}
	inst.mu.Lock()
	running, lastErr := inst.running, inst.err
	inst.mu.Unlock()
	switch {
	case running:
		st.State = "downloading"
	case lastErr != "":
		st.State, st.Error = "failed", lastErr
	case st.Available:
		if m, ok := readMark("failed.json"); ok && m.ID == pin.ID {
			st.State, st.Error = "failed", m.Error
		}
	}
	return st
}

// InstallFFmpeg downloads, verifies and installs ffmpeg and ffprobe, replacing any
// older copy only once the new one runs. It takes a few seconds to a minute.
func InstallFFmpeg(ctx context.Context) error {
	pin, ok := ffPinNow()
	if !ok {
		return ErrNoFFmpegDownload
	}
	if ManagedFFmpegDir == "" {
		return errors.New("ffmpeg downloads are off in this mode")
	}
	inst.mu.Lock()
	if inst.running {
		inst.mu.Unlock()
		return errors.New("ffmpeg is already downloading; give it a minute")
	}
	inst.running, inst.err = true, ""
	inst.mu.Unlock()
	err := installPin(ctx, pin)
	inst.mu.Lock()
	inst.running = false
	if err != nil {
		inst.err = err.Error()
	}
	inst.mu.Unlock()
	return err
}

func installPin(ctx context.Context, pin ffPin) error {
	dir := ManagedFFmpegDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(dir, "dl-*")) // left behind by an install that was cut off
	for _, o := range old {
		_ = os.RemoveAll(o)
	}
	_ = os.Remove(filepath.Join(dir, "failed.json")) // a new try forgets an old failure
	tmp, err := os.MkdirTemp(dir, "dl-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if err := fetchTool(ctx, pin, tool, tmp); err != nil {
			return err
		}
	}
	// It must start on this computer before it replaces anything.
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, err := exec.CommandContext(sctx, filepath.Join(tmp, tool), "-hide_banner", "-version").CombinedOutput()
		cancel()
		if err != nil {
			msg := fmt.Sprintf("the downloaded %s doesn't run on this computer (%v: %s); Townsquare keeps using the ffmpeg already installed",
				tool, err, strings.TrimSpace(string(out)))
			markFailed(pin, msg)
			return errors.New(msg)
		}
	}
	for _, tool := range []string{"ffprobe", "ffmpeg"} {
		if err := os.Rename(filepath.Join(tmp, tool), filepath.Join(dir, tool)); err != nil {
			return err
		}
	}
	for _, n := range []string{"NOTICE.txt", "COPYING.GPLv3"} {
		if b, err := ffNotice.ReadFile("ffnotice/" + n); err == nil {
			_ = os.WriteFile(filepath.Join(dir, n), b, 0o644)
		}
	}
	return writeMark(filepath.Join(dir, "installed.json"), idMark{ID: pin.ID})
}

func markFailed(pin ffPin, msg string) {
	_ = writeMark(filepath.Join(ManagedFFmpegDir, "failed.json"), idMark{ID: pin.ID, Error: msg})
}

func writeMark(path string, m idMark) error {
	b, _ := json.Marshal(m)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fetchTool downloads one zip, checks its SHA-256 and unpacks the program into tmp.
func fetchTool(ctx context.Context, pin ffPin, tool, tmp string) error {
	want := pin.Sums[tool]
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	url := fmt.Sprintf("%s/%s/%s/%s/%s.zip", ffBaseURL, pin.OS, pin.Arch, pin.ID, tool)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't download %s: %v", tool, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("couldn't download %s: the server answered %s", tool, resp.Status)
	}
	zipPath := filepath.Join(tmp, tool+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxZipBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("couldn't download %s: %v", tool, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); n > maxZipBytes || got != want {
		msg := fmt.Sprintf("the downloaded %s doesn't match the checksum built into Townsquare, so it was not used", tool)
		markFailed(pin, msg)
		return errors.New(msg)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("the downloaded %s isn't a readable zip: %v", tool, err)
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || filepath.Base(zf.Name) != tool {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(tmp, tool), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		n, err := io.Copy(out, io.LimitReader(rc, maxToolBytes+1))
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n > maxToolBytes {
			return fmt.Errorf("the downloaded %s is far bigger than expected, so it was not used", tool)
		}
		_ = os.Remove(zipPath)
		return nil
	}
	return fmt.Errorf("the downloaded zip has no %s in it", tool)
}
