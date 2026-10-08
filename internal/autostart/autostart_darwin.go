package autostart

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// Enabled reports whether Townsquare starts at login.
func Enabled() bool { _, err := os.Stat(plistPath()); return err == nil }

// Supported reports whether this system can start Townsquare at login.
func Supported() bool { return true }

// Enable writes the LaunchAgent. It takes effect at the next login, so it never
// starts a second copy next to the one already running.
func Enable(dataDir string) error {
	bin := Installed()
	if bin == "" {
		return fmt.Errorf("can't find the Townsquare program")
	}
	home, _ := os.UserHomeDir()
	esc := html.EscapeString
	args := []string{bin, "--data", dataDir, "serve"} // address and tailnet come from config.json
	var b strings.Builder
	for _, a := range args {
		b.WriteString("<string>" + esc(a) + "</string>")
	}
	log := filepath.Join(dataDir, "serve.log")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + Label + `</string>
  <key>ProgramArguments</key><array>` + b.String() + `</array>
  <key>EnvironmentVariables</key><dict>
    <key>HOME</key><string>` + esc(home) + `</string>
    <key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>` + esc(log) + `</string>
  <key>StandardErrorPath</key><string>` + esc(log) + `</string>
</dict></plist>
`
	if err := os.MkdirAll(filepath.Dir(plistPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath(), []byte(plist), 0o644); err != nil {
		return err
	}
	// Clear any "disabled" override left by launchctl, so it runs at login.
	_ = exec.Command("launchctl", "enable", fmt.Sprintf("gui/%d/%s", os.Getuid(), Label)).Run()
	return nil
}

// Disable removes the LaunchAgent. If launchd is running Townsquare right now,
// it keeps running until you log out or stop it.
func Disable() error {
	err := os.Remove(plistPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
