package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Profiles save where a Townsquare server is and which key to use, so agents
// don't repeat them. Settings live in ~/.config/townsquare/cli.json; keys live
// apart, one file per profile in ~/.config/townsquare/keys/ (mode 600), and are
// never printed.
//
// Precedence, highest first: flags (--url, --profile) > environment
// (TOWNSQUARE_URL, TOWNSQUARE_API_KEY, TOWNSQUARE_PROFILE) > the profile >
// defaults (this computer's Townsquare at the address in its config.json).

// Profile is one saved server.
type Profile struct {
	URL string `json:"url"`
}

type cliConfig struct {
	Default  string             `json:"default,omitempty"`
	Profiles map[string]Profile `json:"profiles"`
}

func configDir() string {
	if d := os.Getenv("TOWNSQUARE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "townsquare")
}

func loadConfig() cliConfig {
	c := cliConfig{Profiles: map[string]Profile{}}
	b, err := os.ReadFile(filepath.Join(configDir(), "cli.json"))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return c
}

func saveConfig(c cliConfig) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeFileAtomic(filepath.Join(configDir(), "cli.json"), append(b, '\n'), 0o600)
}

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}$`)

func keyPath(profile string) string { return filepath.Join(configDir(), "keys", profile) }

func readKey(profile string) string {
	b, err := os.ReadFile(keyPath(profile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveKey(profile, key string) error {
	if err := os.MkdirAll(filepath.Dir(keyPath(profile)), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(keyPath(profile), []byte(key+"\n"), 0o600)
}

// Setting is a resolved value and where it came from.
type Setting struct {
	Value  string `json:"value"`
	Source string `json:"source"` // flag, env, profile, default
}

// Resolved is the connection a command will use.
type Resolved struct {
	Profile Setting `json:"profile"`
	URL     Setting `json:"url"`
	Key     Setting `json:"api_key"` // Value is redacted in output
	key     string
}

func resolveConn(flagProfile, flagURL, localURL string) Resolved {
	cfg := loadConfig()
	var r Resolved
	switch {
	case flagProfile != "":
		r.Profile = Setting{flagProfile, "flag"}
	case os.Getenv("TOWNSQUARE_PROFILE") != "":
		r.Profile = Setting{os.Getenv("TOWNSQUARE_PROFILE"), "env"}
	case cfg.Default != "":
		r.Profile = Setting{cfg.Default, "config"}
	default:
		r.Profile = Setting{"", "default"}
	}
	p, hasProfile := cfg.Profiles[r.Profile.Value]
	switch {
	case flagURL != "":
		r.URL = Setting{flagURL, "flag"}
	case os.Getenv("TOWNSQUARE_URL") != "":
		r.URL = Setting{os.Getenv("TOWNSQUARE_URL"), "env"}
	case hasProfile && p.URL != "":
		r.URL = Setting{p.URL, "profile"}
	default:
		r.URL = Setting{localURL, "default"}
	}
	r.URL.Value = strings.TrimSuffix(r.URL.Value, "/")
	switch {
	case os.Getenv("TOWNSQUARE_API_KEY") != "":
		r.key, r.Key = os.Getenv("TOWNSQUARE_API_KEY"), Setting{"", "env"}
	case r.Profile.Value != "" && readKey(r.Profile.Value) != "":
		r.key, r.Key = readKey(r.Profile.Value), Setting{"", "profile"}
	default:
		r.Key = Setting{"", "none"}
	}
	if r.key != "" {
		r.Key.Value = redact(r.key)
	}
	return r
}

func redact(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:8] + "…"
}

func profileNames() []string {
	cfg := loadConfig()
	var n []string
	for k := range cfg.Profiles {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

func setProfile(name, url, key string, makeDefault bool) error {
	if !profileName.MatchString(name) {
		return errf("usage", "Use lowercase letters, digits, - and _.", "invalid profile name %q", name)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errf("usage", "For example --url https://townsquare.example.ts.net", "--url must start with http:// or https://")
	}
	cfg := loadConfig()
	cfg.Profiles[name] = Profile{URL: strings.TrimSuffix(url, "/")}
	if makeDefault || cfg.Default == "" {
		cfg.Default = name
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if key != "" {
		return saveKey(name, key)
	}
	return nil
}

func removeProfile(name string) error {
	cfg := loadConfig()
	if _, ok := cfg.Profiles[name]; !ok {
		return errf("not_found", "See `townsquare profiles list`.", "no profile %q", name)
	}
	delete(cfg.Profiles, name)
	if cfg.Default == name {
		cfg.Default = ""
	}
	_ = os.Remove(keyPath(name))
	return saveConfig(cfg)
}

func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return errors.New("couldn't write " + path + ": " + err.Error())
	}
	return nil
}
