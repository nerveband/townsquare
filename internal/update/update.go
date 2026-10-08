// Package update keeps Townsquare up to date on every kind of install (the Mac
// app, the Windows exe, the Linux .deb and a plain binary).
//
// Each release publishes latest.json (version, and the file name and SHA-256 of
// every platform's binary) and latest.json.sig, an Ed25519 signature made with
// the release key. The public key is built in, so a download is only used when
// the signature and the hash both match.
//
// Updates never touch the installed file (it may be read-only, inside an app
// bundle, or owned by root). The new binary is saved in DATA/bin and recorded in
// DATA/bin/current.json. On start, Handoff runs that newer binary instead of the
// installed one, so the server and every CLI command use the update.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nerveband/townsquare/internal/changelog"
)

// DefaultBase serves the newest release's files.
const DefaultBase = "https://github.com/nerveband/townsquare/releases/latest/download"

// handoffEnv is set on the child so it never hands off again.
const handoffEnv = "TOWNSQUARE_HANDOFF"

// File is one platform's binary in a release.
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Manifest is latest.json.
type Manifest struct {
	Version string            `json:"version"`
	Date    string            `json:"date"`
	Notes   string            `json:"notes"` // release page URL
	Files   map[string]File   `json:"files"`
	Changes []changelog.Entry `json:"changes,omitempty"` // recent CHANGELOG.md sections, newest first
	// Telegram is the shared Telegram app id. Shipping it here (signed) lets it
	// be swapped on every install without a new release.
	Telegram *TelegramApp `json:"telegram,omitempty"`
}

// TelegramApp is a Telegram api_id and api_hash.
type TelegramApp struct {
	ID   int    `json:"api_id"`
	Hash string `json:"api_hash"`
}

// Staged is DATA/bin/current.json: the downloaded binary to run instead of the installed one.
type Staged struct {
	Version string `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}

// State is what the app and API show.
type State struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Available bool   `json:"available"`        // a newer release exists
	Staged    string `json:"staged,omitempty"` // downloaded, used after a restart
	CheckedAt int64  `json:"checked_at,omitempty"`
	Error     string `json:"error,omitempty"`
	Platform  string `json:"platform"`
	Dev       bool   `json:"dev"` // a build from source: updates are shown, never installed automatically
	// Changes are the release notes between this version and Latest, newest first.
	Changes []changelog.Entry `json:"changes,omitempty"`
}

// Updater checks for, downloads and stages releases.
type Updater struct {
	DataDir string
	Current string
	Base    string // DefaultBase unless TOWNSQUARE_UPDATE_URL is set (tests)
	HTTP    *http.Client

	mu    sync.Mutex
	state State
	last  *Manifest
}

// Last is the most recent verified manifest (nil before the first check).
func (u *Updater) Last() *Manifest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}

// New returns an updater for this build.
func New(dataDir, current string) *Updater {
	base := DefaultBase
	if v := os.Getenv("TOWNSQUARE_UPDATE_URL"); v != "" {
		base = strings.TrimSuffix(v, "/")
	}
	u := &Updater{DataDir: dataDir, Current: current, Base: base, HTTP: &http.Client{Timeout: 10 * time.Minute}}
	u.state = State{Current: current, Platform: Platform(), Dev: !IsRelease(current)}
	if st, ok := ReadStaged(dataDir); ok && Newer(st.Version, current) {
		u.state.Staged = st.Version
	}
	return u
}

// State returns a copy of the current state.
func (u *Updater) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// Platform is the release file key for this build, e.g. darwin-arm64, linux-armv7, windows-amd64.
func Platform() string {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	return runtime.GOOS + "-" + arch
}

// Check fetches and verifies latest.json.
func (u *Updater) Check(ctx context.Context) (*Manifest, error) {
	m, err := u.fetchManifest(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.CheckedAt = time.Now().Unix()
	if err != nil {
		u.state.Error = err.Error()
		return nil, err
	}
	u.state.Error = ""
	u.state.Latest, u.state.Notes = m.Version, m.Notes
	u.state.Available = Newer(m.Version, u.Current)
	u.state.Changes = changelog.Between(m.Changes, u.Current, m.Version, Newer)
	u.last = m
	return m, nil
}

func (u *Updater) fetchManifest(ctx context.Context) (*Manifest, error) {
	body, err := u.get(ctx, u.Base+"/latest.json", 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := u.get(ctx, u.Base+"/latest.json.sig", 4096)
	if err != nil {
		return nil, err
	}
	if err := Verify(body, sig); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("latest.json: %w", err)
	}
	if !IsRelease(m.Version) {
		return nil, fmt.Errorf("latest.json: bad version %q", m.Version)
	}
	return &m, nil
}

// Verify checks an Ed25519 signature (base64) over body with the built-in release key.
func Verify(body, sig []byte) error {
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(s) != ed25519.SignatureSize {
		return errors.New("update signature is malformed")
	}
	for _, k := range PublicKeys {
		pk, err := base64.StdEncoding.DecodeString(k)
		if err == nil && len(pk) == ed25519.PublicKeySize && ed25519.Verify(pk, body, s) {
			return nil
		}
	}
	return errors.New("update signature doesn't match the Townsquare release key")
}

// Download fetches this platform's binary from m, checks its hash, and stages it.
// It returns the staged version. Nothing changes until the next start.
func (u *Updater) Download(ctx context.Context, m *Manifest) (string, error) {
	f, ok := m.Files[Platform()]
	if !ok {
		return "", fmt.Errorf("release %s has no build for %s", m.Version, Platform())
	}
	if strings.ContainsAny(f.Name, `/\`) || f.Name == "" || len(f.SHA256) != 64 {
		return "", errors.New("latest.json: bad file entry")
	}
	if IsBad(u.DataDir, m.Version) {
		return "", fmt.Errorf("%s failed to start here before; waiting for a newer release", m.Version)
	}
	if st, ok := ReadStaged(u.DataDir); ok && st.Version == m.Version {
		return st.Version, nil
	}
	dir := filepath.Join(u.DataDir, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := "townsquare-" + m.Version
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.Base+"/"+f.Name, nil)
	resp, err := u.HTTP.Do(req)
	if err != nil {
		tmp.Close()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		tmp.Close()
		return "", fmt.Errorf("download %s: %s", f.Name, resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, 512<<20)); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, f.SHA256) {
		return "", fmt.Errorf("download %s: checksum mismatch", f.Name)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", err
	}
	st := Staged{Version: m.Version, File: name, SHA256: strings.ToLower(f.SHA256)}
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := writeAtomic(filepath.Join(dir, "current.json"), b); err != nil {
		return "", err
	}
	prune(dir, name)
	u.mu.Lock()
	u.state.Staged = m.Version
	u.mu.Unlock()
	return m.Version, nil
}

// Update checks and, if a newer release exists, downloads it. It returns the
// staged version, or "" when already up to date.
func (u *Updater) Update(ctx context.Context) (string, error) {
	m, err := u.Check(ctx)
	if err != nil {
		return "", err
	}
	if !Newer(m.Version, u.Current) {
		return "", nil
	}
	return u.Download(ctx, m)
}

// ReadStaged returns DATA/bin/current.json when it names a file that exists.
func ReadStaged(dataDir string) (Staged, bool) {
	var st Staged
	b, err := os.ReadFile(filepath.Join(dataDir, "bin", "current.json"))
	if err != nil || json.Unmarshal(b, &st) != nil || st.File == "" || strings.ContainsAny(st.File, `/\`) {
		return Staged{}, false
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", st.File)); err != nil {
		return Staged{}, false
	}
	return st, true
}

// StagedPath returns the staged binary when it is newer than current and its hash is intact.
func StagedPath(dataDir, current string) (string, bool) {
	st, ok := ReadStaged(dataDir)
	if !ok || !Newer(st.Version, current) {
		return "", false
	}
	p := filepath.Join(dataDir, "bin", st.File)
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), st.SHA256) {
		return "", false
	}
	return p, true
}

// Handoff runs a newer staged binary in place of this one, if there is one. It
// returns only when there is nothing to hand off to (or starting it failed).
// Builds from source (dev versions) never hand off.
//
// server is true for `serve` (and opening the app). A server that starts a new
// version but never reports healthy (MarkHealthy) twice in a row gets that
// version marked bad and runs the installed one instead, so a broken update
// can't keep Townsquare down and miss sends.
func Handoff(dataDir, current string, server bool) {
	if os.Getenv(handoffEnv) != "" || !IsRelease(current) {
		return
	}
	p, ok := StagedPath(dataDir, current)
	if !ok {
		return
	}
	st, _ := ReadStaged(dataDir)
	if server {
		h := readHealth(dataDir)
		if h.Version != st.Version {
			h = health{Version: st.Version}
		}
		if !h.Healthy && h.Attempts >= 2 {
			markBad(dataDir, st.Version)
			fmt.Fprintln(os.Stderr, "update:", st.Version, "didn't start properly twice; running", current, "instead")
			return
		}
		if !h.Healthy {
			h.Attempts++
			writeHealth(dataDir, h)
		}
	}
	if err := run(p, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "update: couldn't start", p+":", err, "(running the installed version)")
	}
}

type health struct {
	Version  string `json:"version"`
	Attempts int    `json:"attempts"`
	Healthy  bool   `json:"healthy"`
}

func healthPath(dataDir string) string { return filepath.Join(dataDir, "bin", "health.json") }

func readHealth(dataDir string) health {
	var h health
	b, _ := os.ReadFile(healthPath(dataDir))
	_ = json.Unmarshal(b, &h)
	return h
}

func writeHealth(dataDir string, h health) {
	b, _ := json.Marshal(h)
	_ = writeAtomic(healthPath(dataDir), b)
}

// MarkHealthy records that this version's server started and has been running
// fine, so later starts keep using it.
func MarkHealthy(dataDir, version string) {
	h := readHealth(dataDir)
	if h.Version == version && h.Healthy {
		return
	}
	if st, ok := ReadStaged(dataDir); ok && st.Version == version {
		writeHealth(dataDir, health{Version: version, Healthy: true})
	}
}

// markBad stops a version from being used or downloaded again.
func markBad(dataDir, version string) {
	_ = os.Remove(filepath.Join(dataDir, "bin", "current.json"))
	f, err := os.OpenFile(filepath.Join(dataDir, "bin", "bad.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintln(f, version)
		f.Close()
	}
}

// IsBad reports a version that failed to start before.
func IsBad(dataDir, version string) bool {
	b, _ := os.ReadFile(filepath.Join(dataDir, "bin", "bad.txt"))
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == version {
			return true
		}
	}
	return false
}

// Probe runs the staged binary's `version` command and checks it answers with
// the expected version, before the server shuts down to switch to it.
func Probe(dataDir, current string) error {
	p, ok := StagedPath(dataDir, current)
	if !ok {
		return errors.New("no update is ready")
	}
	st, _ := ReadStaged(dataDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "version")
	cmd.Env = append(os.Environ(), handoffEnv+"=1")
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), st.Version) {
		markBad(dataDir, st.Version)
		return fmt.Errorf("%s doesn't run on this computer (%v); skipped it", st.Version, err)
	}
	return nil
}

// Restart replaces this process (same arguments) with the staged update when
// one is ready, else with the installed program. Call it after closing the
// database and listeners.
func Restart(dataDir, current string) error {
	if p, ok := StagedPath(dataDir, current); ok && IsRelease(current) {
		err := run(p, os.Args[1:])
		fmt.Fprintln(os.Stderr, "update: couldn't start", p+":", err, "(restarting the installed version)")
	}
	p := installed()
	if p == "" {
		return errors.New("can't find the Townsquare program")
	}
	return run(p, os.Args[1:])
}

// installed is the path of the program the user installed (kept across handoffs).
func installed() string {
	if p := os.Getenv("TOWNSQUARE_INSTALLED"); p != "" {
		return p
	}
	p, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// prune keeps the new binary and the one before it.
func prune(dir, keep string) {
	ents, _ := os.ReadDir(dir)
	type bin struct {
		name string
		mod  time.Time
	}
	var bins []bin
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "townsquare-v") && e.Name() != keep {
			if i, err := e.Info(); err == nil {
				bins = append(bins, bin{e.Name(), i.ModTime()})
			}
		}
	}
	for i := range bins {
		newer := 0
		for j := range bins {
			if bins[j].mod.After(bins[i].mod) {
				newer++
			}
		}
		if newer >= 1 { // keep only the newest of the old ones
			_ = os.Remove(filepath.Join(dir, bins[i].name))
		}
	}
}

func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "townsquare/"+u.Current)
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", filepath.Base(url), resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

var releaseRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-((?:alpha|beta|rc)\.?\d*))?$`)

// IsRelease reports whether v is a release version (v1.2.3 or v1.2.3-rc.1), not
// a build from source like v0.5.0-23-gabc123 or dev.
func IsRelease(v string) bool { return releaseRE.MatchString(v) }

// Newer reports whether release a is newer than b. A build from source is never
// older than anything, except a release with a higher x.y.z.
func Newer(a, b string) bool {
	pa := releaseRE.FindStringSubmatch(a)
	if pa == nil {
		return false
	}
	pb := releaseRE.FindStringSubmatch(b)
	if pb == nil {
		// b is a dev build "vX.Y.Z-N-gHASH": newer only if a's x.y.z is higher than b's.
		m := regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(b)
		if m == nil {
			return false
		}
		return cmp3(pa[1:4], m[1:4]) > 0
	}
	if c := cmp3(pa[1:4], pb[1:4]); c != 0 {
		return c > 0
	}
	// Same x.y.z: a final release beats a prerelease.
	switch {
	case pa[4] == "" && pb[4] != "":
		return true
	case pa[4] != "" && pb[4] == "":
		return false
	}
	return pa[4] > pb[4]
}

func cmp3(a, b []string) int {
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}
