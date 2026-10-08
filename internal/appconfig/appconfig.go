// Package appconfig holds the few settings Townsquare needs before its database
// opens: where the web app listens and its tailnet name. They live in
// DATA/config.json so the app (and start at login) can run with no flags, and
// agents can change them through the API or `townsquare config`.
package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultListen is the web app's address when nothing is configured.
const DefaultListen = "127.0.0.1:8890"

// Config is DATA/config.json. Empty fields mean the default.
type Config struct {
	Listen    string `json:"listen,omitempty"`    // host:port for the web app and API
	Tailscale string `json:"tailscale,omitempty"` // tailnet machine name ("" = off)
}

func path(dataDir string) string { return filepath.Join(dataDir, "config.json") }

// Load reads DATA/config.json (missing file = defaults).
func Load(dataDir string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("config.json: %w", err)
	}
	return c, nil
}

// ListenAddr is the configured listen address or the default.
func (c Config) ListenAddr() string {
	if c.Listen != "" {
		return c.Listen
	}
	return DefaultListen
}

var tsName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Validate checks the values.
func (c Config) Validate() error {
	if c.Listen != "" {
		host, port, err := net.SplitHostPort(c.Listen)
		if err != nil || port == "" {
			return fmt.Errorf("listen must be host:port, like 127.0.0.1:8890 or 0.0.0.0:8890")
		}
		if host != "" && net.ParseIP(host) == nil && host != "localhost" {
			return fmt.Errorf("listen host must be an IP address or localhost")
		}
	}
	if c.Tailscale != "" && !tsName.MatchString(c.Tailscale) {
		return fmt.Errorf("tailscale must be a machine name: lowercase letters, digits and dashes")
	}
	return nil
}

// Save writes DATA/config.json.
func Save(dataDir string, c Config) error {
	c.Listen, c.Tailscale = strings.TrimSpace(c.Listen), strings.TrimSpace(c.Tailscale)
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := path(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path(dataDir))
}
