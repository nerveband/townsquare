package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", "townsquare.service")
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

// Supported reports whether this system can start Townsquare at login.
func Supported() bool { _, err := exec.LookPath("systemctl"); return err == nil }

// Enabled reports whether Townsquare starts at login.
func Enabled() bool {
	return exec.Command("systemctl", "--user", "is-enabled", "--quiet", "townsquare").Run() == nil
}

// Enable writes (or reuses) the systemd user service and enables it for the next login.
func Enable(dataDir string) error {
	bin := Installed()
	if bin == "" {
		return fmt.Errorf("can't find the Townsquare program")
	}
	unit := fmt.Sprintf(`[Unit]
Description=Townsquare: one calendar to schedule all your community posts
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%q --data %q serve
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, bin, dataDir)
	if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath(), []byte(unit), 0o644); err != nil {
		return err
	}
	_ = systemctl("daemon-reload")
	return systemctl("enable", "townsquare")
}

// Disable turns the service off for future logins.
func Disable() error {
	err := systemctl("disable", "townsquare")
	if rmErr := os.Remove(unitPath()); rmErr == nil {
		_ = systemctl("daemon-reload")
	}
	return err
}
