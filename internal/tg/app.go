package tg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Telegram needs an app id (api_id and api_hash from my.telegram.org). Townsquare
// looks for one in this order:
//
//  1. DATA/telegram.app: the user's own (line 1 api_id, line 2 api_hash).
//  2. DATA/telegram.shared.json: Townsquare's shared id, delivered over the air in
//     the signed update manifest, so it can be swapped without a new release.
//  3. The shared id built into release binaries (-ldflags -X ...tg.builtinApp=ID:HASH).

// builtinApp is "api_id:api_hash", set at release build time. Empty in builds from source.
var builtinApp string

// App is one Telegram app id.
type App struct {
	ID   int    `json:"api_id"`
	Hash string `json:"api_hash"`
}

// Valid reports a plausible id and hash.
func (a App) Valid() bool { return a.ID > 0 && len(a.Hash) == 32 }

// ParseApp reads "ID:HASH" or "ID\nHASH".
func ParseApp(s string) (App, error) {
	f := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool { return r == ':' || r == '\n' || r == '\r' })
	if len(f) < 2 {
		return App{}, errors.New("need api_id and api_hash")
	}
	id, err := strconv.Atoi(strings.TrimSpace(f[0]))
	if err != nil {
		return App{}, errors.New("api_id must be a number")
	}
	a := App{ID: id, Hash: strings.TrimSpace(f[1])}
	if !a.Valid() {
		return App{}, errors.New("api_hash must be 32 characters")
	}
	return a, nil
}

// ResolveApp returns the app id to use and where it came from: "own", "shared" or "built-in".
func ResolveApp(dataDir string) (App, string, error) {
	if b, err := os.ReadFile(filepath.Join(dataDir, "telegram.app")); err == nil {
		a, err := ParseApp(string(b))
		if err != nil {
			return App{}, "", errors.New("telegram.app: " + err.Error())
		}
		return a, "own", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return App{}, "", err
	}
	if a, ok := SharedApp(dataDir); ok {
		return a, "shared", nil
	}
	if a, err := ParseApp(builtinApp); err == nil {
		return a, "built-in", nil
	}
	return App{}, "", nil
}

// SharedApp returns the over-the-air shared id, if one was saved.
func SharedApp(dataDir string) (App, bool) {
	var a App
	b, err := os.ReadFile(filepath.Join(dataDir, "telegram.shared.json"))
	if err != nil || json.Unmarshal(b, &a) != nil || !a.Valid() {
		return App{}, false
	}
	return a, true
}

// SaveSharedApp stores a shared id from a verified update manifest. It reports
// whether the id the app would use without its own telegram.app changed.
func SaveSharedApp(dataDir string, a App) (bool, error) {
	if !a.Valid() {
		return false, errors.New("invalid shared Telegram app id")
	}
	before := currentShared(dataDir)
	if cur, ok := SharedApp(dataDir); ok && cur == a {
		return false, nil
	}
	b, _ := json.Marshal(a)
	tmp := filepath.Join(dataDir, "telegram.shared.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, filepath.Join(dataDir, "telegram.shared.json")); err != nil {
		return false, err
	}
	return before != a, nil
}

func currentShared(dataDir string) App {
	if a, ok := SharedApp(dataDir); ok {
		return a
	}
	a, _ := ParseApp(builtinApp)
	return a
}

// SaveOwnApp writes the user's own telegram.app.
func SaveOwnApp(dataDir string, a App) error {
	if !a.Valid() {
		return errors.New("invalid Telegram app id")
	}
	return os.WriteFile(filepath.Join(dataDir, "telegram.app"), []byte(strconv.Itoa(a.ID)+"\n"+a.Hash+"\n"), 0o600)
}

// RemoveOwnApp deletes telegram.app, going back to the shared id.
func RemoveOwnApp(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, "telegram.app"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
