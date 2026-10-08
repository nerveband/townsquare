// Command manifest writes dist/latest.json for a release: each platform's binary
// and SHA-256, the recent CHANGELOG.md sections, and the shared Telegram app id.
//
//	go run ./tools/manifest VERSION DIST [TELEGRAM_APP_FILE]
//
// Sign the result with tools/sign. See internal/update.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	townsquare "github.com/nerveband/townsquare"
	"github.com/nerveband/townsquare/internal/changelog"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/update"
)

func main() {
	if len(os.Args) < 3 {
		fail("usage: manifest VERSION DIST [TELEGRAM_APP_FILE]")
	}
	v, dist := os.Args[1], os.Args[2]
	if !update.IsRelease(v) {
		fail("not a release version: " + v)
	}
	m := update.Manifest{Version: v, Date: time.Now().Format("2006-01-02"),
		Notes: "https://github.com/nerveband/townsquare/releases/tag/" + v, Files: map[string]update.File{}}
	for _, plat := range []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64", "linux-armv7", "windows-amd64"} {
		name := "townsquare-" + v + "-" + plat
		if plat == "windows-amd64" {
			name += ".exe"
		}
		b, err := os.ReadFile(filepath.Join(dist, name))
		if err != nil {
			fail(err.Error())
		}
		sum := sha256.Sum256(b)
		m.Files[plat] = update.File{Name: name, SHA256: hex.EncodeToString(sum[:])}
	}
	es := changelog.Parse(townsquare.Changelog)
	if len(es) == 0 || es[0].Version != v {
		fail("CHANGELOG.md's newest release section must be " + v)
	}
	if len(es) > 10 {
		es = es[:10]
	}
	m.Changes = es
	if len(os.Args) > 3 {
		b, err := os.ReadFile(os.Args[3])
		if err != nil {
			fail(err.Error())
		}
		a, err := tg.ParseApp(string(b))
		if err != nil {
			fail("telegram app file: " + err.Error())
		}
		m.Telegram = &update.TelegramApp{ID: a.ID, Hash: a.Hash}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dist, "latest.json"), append(b, '\n'), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Println("wrote", filepath.Join(dist, "latest.json"))
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "manifest:", msg)
	os.Exit(1)
}
