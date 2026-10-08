// Package autostart turns "start Townsquare when I log in" on and off: a
// LaunchAgent on macOS, a systemd user service on Linux, and a Run key on
// Windows. The entry runs the installed binary with `serve`; that binary hands
// off to any newer downloaded update on start.
package autostart

import (
	"os"
	"path/filepath"
)

// Label is the macOS LaunchAgent label (scripts/deploy.sh uses it too).
const Label = "com.townsquare.server"

// Installed is the path of the installed binary, not a downloaded update it
// handed off to (update.Handoff passes it on in TOWNSQUARE_INSTALLED).
func Installed() string {
	if p := os.Getenv("TOWNSQUARE_INSTALLED"); p != "" {
		return p
	}
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
