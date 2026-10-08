package autostart

import (
	"fmt"
	"os/exec"
	"strings"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// Supported reports whether this system can start Townsquare at login.
func Supported() bool { return true }

// Enabled reports whether Townsquare starts at login.
func Enabled() bool { return exec.Command("reg", "query", runKey, "/v", "Townsquare").Run() == nil }

// Enable adds a Run entry for the next login.
func Enable(dataDir, listen string) error {
	bin := Installed()
	if bin == "" {
		return fmt.Errorf("can't find the Townsquare program")
	}
	cmd := fmt.Sprintf(`"%s" --data "%s" serve --listen %s`, bin, dataDir, listen)
	out, err := exec.Command("reg", "add", runKey, "/v", "Townsquare", "/t", "REG_SZ", "/d", cmd, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg add: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Disable removes the Run entry.
func Disable() error {
	if !Enabled() {
		return nil
	}
	out, err := exec.Command("reg", "delete", runKey, "/v", "Townsquare", "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg delete: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
