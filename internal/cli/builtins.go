package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	townsquare "github.com/nerveband/townsquare"
)

// Built-in commands: setup, inspection and agent help. Declared in builtinSpecs
// for `schema`.

var builtinOrder = []string{"doctor", "agenda", "auth", "profiles", "context", "schema", "skills", "feedback", "help"}

var builtinDesc = map[string]string{
	"doctor":   "Check everything an agent needs: server, key, versions, WhatsApp, Telegram, safe mode, what's due",
	"agenda":   "What goes out in the next hours: each send with its post and chats",
	"auth":     "Save or check the API key: auth login | status | logout | local",
	"profiles": "Saved servers: profiles list | get NAME | use NAME | remove NAME",
	"context":  "Which server, profile and key this shell uses, and where each came from",
	"schema":   "Every command as JSON (CLI Spec v0.3); `schema posts list` narrows it; --validate checks it",
	"skills":   "Agent guides shipped with Townsquare: skills list | show [NAME] | path",
	"feedback": "Note CLI friction for the maintainers, kept locally: feedback \"text\" | feedback list",
	"help":     "Help for any command: help posts create",
}

var builtinSpecs = []localSpec{
	{Name: "doctor", Summary: builtinDesc["doctor"] + ". Exits 21 (outcome unhealthy) when a check fails.", Effects: "read_only", Cardinality: "bounded", Examples: []string{"townsquare doctor", "townsquare doctor -o json"}, Errors: []string{"network"}, Outcomes: true},
	{Name: "agenda", Summary: builtinDesc["agenda"], Effects: "read_only", Cardinality: "bounded",
		Args: []Param{{Flag: "--hours", Type: "integer", Desc: "How far ahead (default 24)"}}, Examples: []string{"townsquare agenda --hours 48 -o text"}, Errors: []string{"auth", "network"}},
	{Name: "auth login", Summary: "Save a server and key as a profile. The key comes from stdin or TOWNSQUARE_API_KEY, never a flag.", Effects: "idempotent", Cardinality: "single",
		Args:     []Param{{Flag: "--no-verify", Type: "boolean", Desc: "Save without checking the key against the server. Also uses the global --url (required) and --profile (default: default)."}},
		Examples: []string{"townsquare auth login --url https://townsquare.example.ts.net --profile home < ~/.townsquare/agent-keys/isla-agent.key"}, Errors: []string{"usage", "auth", "network"}},
	{Name: "auth status", Summary: "Check the key against the server without changing anything.", Effects: "read_only", Cardinality: "single", Examples: []string{"townsquare auth status"}, Errors: []string{"auth", "network"}},
	{Name: "auth logout", Summary: "Forget a profile's key.", Effects: "idempotent", Cardinality: "single", Examples: []string{"townsquare auth logout --profile home"}},
	{Name: "auth local", Summary: "On the Townsquare computer: create a key in its database and save the profile \"local\".", Effects: "non_idempotent", Cardinality: "single",
		Args:     []Param{{Flag: "--scope", Type: "string", Enum: []string{"read", "write", "admin"}, Desc: "Default write"}, {Flag: "--name", Type: "string", Desc: "Key name (default cli-local)"}},
		Examples: []string{"townsquare auth local --scope admin"}},
	{Name: "profiles list", Summary: "Saved profiles (no secrets).", Effects: "read_only", Cardinality: "bounded", Examples: []string{"townsquare profiles list -o json"}},
	{Name: "profiles get", Summary: "One profile (no secrets).", Effects: "read_only", Cardinality: "single", Examples: []string{"townsquare profiles get home"}, Errors: []string{"not_found"}},
	{Name: "profiles use", Summary: "Make a profile the default.", Effects: "idempotent", Cardinality: "single", Examples: []string{"townsquare profiles use home"}, Errors: []string{"not_found"}},
	{Name: "profiles remove", Summary: "Delete a profile and its key.", Effects: "idempotent", Cardinality: "single", Examples: []string{"townsquare profiles remove old --yes"}, Errors: []string{"not_found", "confirmation_required"}},
	{Name: "context", Summary: builtinDesc["context"], Effects: "read_only", Cardinality: "single", Examples: []string{"townsquare context -o json"}},
	{Name: "schema", Summary: builtinDesc["schema"], Effects: "read_only", Cardinality: "single",
		Args: []Param{{Flag: "--validate", Type: "boolean", Desc: "Check the schema's own rules (vocabulary, references, examples)"}}, Examples: []string{"townsquare schema posts list", "townsquare schema --validate"}},
	{Name: "skills list", Summary: "Agent guides shipped with this version.", Effects: "read_only", Cardinality: "bounded", Examples: []string{"townsquare skills list"}},
	{Name: "skills show", Summary: "Print an agent guide (SKILL.md).", Effects: "read_only", OutputKind: "opaque", Examples: []string{"townsquare skills show townsquare"}},
	{Name: "skills path", Summary: "Write the guides to a folder and print where (for agents that install skills from files).", Effects: "idempotent", Cardinality: "single", Examples: []string{"townsquare skills path"}},
	{Name: "feedback", Summary: "Record CLI friction locally (~/.config/townsquare/feedback.jsonl). No network.", Effects: "non_idempotent", Cardinality: "single",
		Args: []Param{{Flag: "--command", Type: "string", Desc: "The command it is about"}}, Examples: []string{"townsquare feedback \"posts create rejected --send-at 6pm\" --command 'posts create'"}},
	{Name: "feedback list", Summary: "Show recorded feedback.", Effects: "read_only", Cardinality: "bounded", Examples: []string{"townsquare feedback list"}},
	{Name: "help", Summary: builtinDesc["help"], Effects: "read_only", OutputKind: "opaque", Examples: []string{"townsquare help posts create"}},
}

var builtins = map[string]func(*opts) error{}

func init() {
	builtins["help"] = cmdHelp
	builtins["schema"] = cmdSchema
	builtins["doctor"] = cmdDoctor
	builtins["agenda"] = cmdAgenda
	builtins["auth"] = cmdAuth
	builtins["profiles"] = cmdProfiles
	builtins["context"] = cmdContext
	builtins["skills"] = cmdSkills
	builtins["feedback"] = cmdFeedback
}

// out prints a built-in command's result, honoring --fields, --id-only,
// --count, --max-depth and --transform like remote commands do.
func out(o *opts, v any) error {
	if m, ok := v.(map[string]any); ok {
		if items, ok := m["items"].([]any); ok {
			if o.count {
				return printDoc(os.Stdout, map[string]any{"count": len(items)}, o.resolvedOutput(), isTTY(os.Stdout))
			}
			if o.limitSet || o.offsetSet {
				total := len(items)
				lo := min(o.offset, total)
				hi := total
				if o.limitSet {
					hi = min(lo+o.limit, total)
				}
				items = items[lo:hi]
				lim := 0
				if o.limitSet {
					lim = o.limit
				}
				m = envelope(items, total, lo, lim, true)
				v = m
			}
			for i, it := range items {
				if o.idOnly {
					items[i] = idOf(it)
				} else {
					items[i] = project(it, o.fields)
				}
			}
		} else if len(o.fields) > 0 {
			v = project(m, o.fields)
		}
	}
	if o.maxDepth > 0 {
		v = collapse(v, o.maxDepth)
	}
	if o.transform != "" {
		x, err := transform(v, o.transform)
		if err != nil {
			return err
		}
		v = x
	}
	return printDoc(os.Stdout, v, o.resolvedOutput(), isTTY(os.Stdout))
}

func builtinHelp(o *opts) bool {
	if !o.help {
		return false
	}
	name := strings.Join(o.words, " ")
	for _, s := range builtinSpecs {
		if s.Name == name {
			_ = localHelp(os.Stdout, s)
			return true
		}
	}
	fmt.Fprintf(os.Stdout, "townsquare %s\n\n%s\n\n", o.words[0], builtinDesc[o.words[0]])
	for _, s := range builtinSpecs {
		if strings.HasPrefix(s.Name, o.words[0]+" ") || s.Name == o.words[0] {
			fmt.Fprintf(os.Stdout, "  %-18s %s\n", s.Name, s.Summary)
		}
	}
	return true
}

func cmdHelp(o *opts) error {
	if len(o.words) == 1 {
		return rootHelp(os.Stdout)
	}
	if LocalHelp(o.words[1]) {
		return nil
	}
	o2 := *o
	o2.words, o2.help = o.words[1:], true
	return run(&o2)
}

func checkFlags(o *opts, allowed ...string) error {
	for _, f := range o.flagOrder {
		if !contains(allowed, f) {
			return unknownFlag(f, allowed)
		}
	}
	return nil
}

// ---- auth ----

func cmdAuth(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	verb := ""
	if len(o.words) > 1 {
		verb = o.words[1]
	}
	switch verb {
	case "login":
		if err := checkFlags(o, "--no-verify"); err != nil {
			return err
		}
		name := o.profile
		if name == "" {
			name = "default"
		}
		if o.url == "" {
			return errf("usage", "Example: townsquare auth login --url https://townsquare.example.ts.net --profile home < keyfile", "auth login needs --url")
		}
		key := os.Getenv("TOWNSQUARE_API_KEY")
		if key == "" {
			if isTTY(os.Stdin) {
				return errf("usage", "Pipe it in: townsquare auth login --url URL < keyfile (or set TOWNSQUARE_API_KEY). Keys never go on the command line.", "auth login reads the key from stdin")
			}
			b, _ := io.ReadAll(io.LimitReader(os.Stdin, 4096))
			key = strings.TrimSpace(string(b))
		}
		if !strings.HasPrefix(key, "tsq_") && !strings.HasPrefix(key, "wacal_") {
			return errf("usage", "Keys start with tsq_. Make one in Settings → API keys or with `townsquare apikey create`.", "that doesn't look like a Townsquare API key")
		}
		if o.flags["--no-verify"] == "" {
			if _, err := whoami(o, Resolved{URL: Setting{Value: strings.TrimSuffix(o.url, "/")}, key: key}); err != nil {
				return err
			}
		}
		if err := setProfile(name, o.url, key, true); err != nil {
			return err
		}
		return out(o, map[string]any{"profile": name, "url": strings.TrimSuffix(o.url, "/"), "default": true, "changed": true})
	case "status", "":
		conn := resolveConn(o.profile, o.url, env.LocalURL)
		res := map[string]any{"profile": conn.Profile, "url": conn.URL, "api_key": conn.Key}
		if conn.key == "" {
			res["ok"] = false
			res["hint"] = "No key. Run `townsquare auth local` here, or `townsquare auth login --url URL < keyfile`."
			_ = out(o, res)
			return &Error{Kind: "auth", Message: "no API key configured", Hint: res["hint"].(string)}
		}
		st, err := whoami(o, conn)
		if err != nil {
			return err
		}
		res["ok"] = true
		res["scope"] = scopeOf(st)
		res["server_version"] = st["version"]
		return out(o, res)
	case "logout":
		name := o.profile
		if name == "" {
			name = loadConfig().Default
		}
		if name == "" {
			return out(o, map[string]any{"changed": false})
		}
		err := os.Remove(keyPath(name))
		return out(o, map[string]any{"profile": name, "changed": err == nil})
	case "local":
		if err := checkFlags(o, "--scope", "--name"); err != nil {
			return err
		}
		if env.MakeKey == nil {
			return errf("general", "", "auth local isn't available in this build")
		}
		scope, name := o.flags["--scope"], o.flags["--name"]
		if scope == "" {
			scope = "write"
		}
		if name == "" {
			name = "cli-local"
		}
		if !contains([]string{"read", "write", "admin"}, scope) {
			return errf("usage", "Valid values: read, write, admin.", "invalid --scope %q", scope)
		}
		if o.dryRun {
			return out(o, map[string]any{"dry_run": true, "action": "create API key and save profile local", "key_name": name, "scope": scope,
				"url": env.LocalURL, "validated": "local", "data_dir": env.DataDir})
		}
		key, err := env.MakeKey(name, scope)
		if err != nil {
			return errf("general", "Run it on the computer that runs Townsquare (it needs the data folder "+env.DataDir+").", "couldn't create a key: %v", err)
		}
		if err := setProfile("local", env.LocalURL, key, true); err != nil {
			return err
		}
		return out(o, map[string]any{"profile": "local", "url": env.LocalURL, "scope": scope, "key_name": name, "changed": true})
	}
	return errf("usage", "Use: auth login | status | logout | local", "unknown auth command %q", verb)
}

func whoami(o *opts, conn Resolved) (map[string]any, error) {
	resp, b, err := call(o, &Command{Method: "GET", Effects: "read_only"}, &request{method: "GET", path: "/status", query: url.Values{}}, conn)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fromHTTP(resp.StatusCode, b, "")
	}
	var m map[string]any
	_ = jsonUnmarshal(b, &m)
	return m, nil
}

// ---- profiles and context ----

func cmdProfiles(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	verb := "list"
	if len(o.words) > 1 {
		verb = o.words[1]
	}
	cfg := loadConfig()
	arg := ""
	if len(o.words) > 2 {
		arg = o.words[2]
	}
	switch verb {
	case "list":
		var items []any
		for _, n := range profileNames() {
			items = append(items, map[string]any{"name": n, "url": cfg.Profiles[n].URL, "default": n == cfg.Default, "has_key": readKey(n) != ""})
		}
		return out(o, envelope(items, len(items), 0, 0, false))
	case "get":
		p, ok := cfg.Profiles[arg]
		if !ok {
			return errf("not_found", "See `townsquare profiles list`.", "no profile %q", arg)
		}
		return out(o, map[string]any{"name": arg, "url": p.URL, "default": arg == cfg.Default, "has_key": readKey(arg) != ""})
	case "use":
		if _, ok := cfg.Profiles[arg]; !ok {
			return errf("not_found", "See `townsquare profiles list`.", "no profile %q", arg)
		}
		changed := cfg.Default != arg
		cfg.Default = arg
		if err := saveConfig(cfg); err != nil {
			return err
		}
		return out(o, map[string]any{"default": arg, "changed": changed})
	case "remove":
		if !o.yes {
			return &Error{Kind: "confirmation_required", Message: "removing a profile deletes its saved key", Hint: "Re-run with --yes."}
		}
		if _, ok := cfg.Profiles[arg]; !ok {
			return out(o, map[string]any{"changed": false, "status": "absent"})
		}
		if err := removeProfile(arg); err != nil {
			return err
		}
		return out(o, map[string]any{"removed": arg, "changed": true})
	}
	return errf("usage", "Use: profiles list | get NAME | use NAME | remove NAME --yes", "unknown profiles command %q", verb)
}

func cmdContext(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	conn := resolveConn(o.profile, o.url, env.LocalURL)
	format := Setting{o.output, "default"}
	if o.outputSet {
		format.Source = "flag"
	} else if os.Getenv("TOWNSQUARE_OUTPUT") != "" {
		format.Source = "env"
	}
	return out(o, map[string]any{
		"cli_version": env.Version, "profile": conn.Profile, "url": conn.URL, "api_key": conn.Key,
		"output": format, "timeout": Setting{o.timeout.String(), "flag-or-default"},
		"config_file": filepath.Join(configDir(), "cli.json"), "keys_dir": filepath.Join(configDir(), "keys"),
		"data_dir": env.DataDir, "profiles": profileNames(),
		"precedence": "flag > environment (TOWNSQUARE_URL, TOWNSQUARE_API_KEY, TOWNSQUARE_PROFILE, TOWNSQUARE_OUTPUT) > profile > default",
	})
}

// ---- doctor and agenda ----

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

func cmdDoctor(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	conn := resolveConn(o.profile, o.url, env.LocalURL)
	var cs []check
	add := func(name string, ok bool, detail, hint string) {
		c := check{Name: name, OK: ok, Detail: detail}
		if !ok {
			c.Hint = hint
		}
		cs = append(cs, c)
	}
	if _, err := Commands(); err != nil {
		add("contract", false, err.Error(), "Reinstall Townsquare.")
	} else if errs := validateSchema(); len(errs) > 0 {
		add("contract", false, strings.Join(errs, "; "), "Report this (townsquare feedback).")
	} else {
		add("contract", true, "commands match the built-in API contract", "")
	}
	add("api key", conn.key != "", "from "+conn.Key.Source, "Run `townsquare auth local` here, or `townsquare auth login --url URL < keyfile`.")
	st, err := whoami(&opts{timeout: 10 * time.Second, quiet: true}, conn)
	if err != nil {
		e := asError(err)
		add("server", false, conn.URL.Value+": "+e.Message, e.Hint)
	} else {
		add("server", true, fmt.Sprintf("%s answers (Townsquare %v, key scope %v)", conn.URL.Value, st["version"], scopeOf(st)), "")
		add("versions match", fmt.Sprint(st["version"]) == env.Version, fmt.Sprintf("server %v, CLI %s", st["version"], env.Version), "Run `townsquare update` so the CLI matches the server.")
		add("whatsapp", st["connected"] == true, fmt.Sprintf("connected: %v", st["connected"]), "Run `townsquare whatsapp get`, then `townsquare whatsapp link --wait`.")
		add("safe mode", true, fmt.Sprintf("on: %v (only allowlisted chats receive posts)", st["safe_mode"]), "")
		if tg, err := getJSON(o, conn, "/telegram"); err == nil {
			add("telegram", true, fmt.Sprintf("status: %v", tg["status"]), "")
		}
		if up, err := getJSON(o, conn, "/update"); err == nil {
			latest := up["latest"]
			if latest == nil || latest == "" {
				latest = "not checked yet"
			}
			d := fmt.Sprintf("version %v, latest %v", up["current"], latest)
			if up["staged"] != nil && up["staged"] != "" {
				d += fmt.Sprintf(", %v installs at the next free moment", up["staged"])
			}
			add("updates", up["error"] == nil || up["error"] == "", d, fmt.Sprint(up["error"]))
		}
		if ag, err := agenda(o, conn, 1); err == nil {
			add("next hour", true, fmt.Sprintf("%d sends due in the next hour", len(ag)), "")
		}
	}
	ok := true
	for _, c := range cs {
		ok = ok && c.OK
	}
	res := map[string]any{"ok": ok, "checks": cs}
	defer func() {
		if !ok {
			os.Exit(21) // outcome: unhealthy (the report is on stdout)
		}
	}()
	if o.resolvedOutput() == "text" {
		for _, c := range cs {
			mark := "✓"
			if !c.OK {
				mark = "✗"
			}
			fmt.Printf("%s %-15s %s\n", mark, c.Name, clean(c.Detail))
			if c.Hint != "" {
				fmt.Printf("  → %s\n", c.Hint)
			}
		}
		return nil
	}
	return out(o, res)
}

func scopeOf(st map[string]any) any {
	if k, ok := st["key"].(map[string]any); ok {
		return k["scope"]
	}
	return nil
}

func getJSON(o *opts, conn Resolved, path string) (map[string]any, error) {
	resp, b, err := call(&opts{timeout: 10 * time.Second, quiet: true}, &Command{Method: "GET", Effects: "read_only"}, &request{method: "GET", path: path, query: url.Values{}}, conn)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fromHTTP(resp.StatusCode, b, "")
	}
	var m map[string]any
	return m, jsonUnmarshal(b, &m)
}

func agenda(o *opts, conn Resolved, hours int) ([]any, error) {
	now := time.Now()
	q := url.Values{"from": {now.UTC().Format(time.RFC3339)}, "to": {now.Add(time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)}}
	resp, b, err := call(&opts{timeout: o.timeout, quiet: true}, &Command{Method: "GET", Effects: "read_only"}, &request{method: "GET", path: "/sends", query: q}, conn)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fromHTTP(resp.StatusCode, b, "")
	}
	var d struct {
		Sends []map[string]any          `json:"sends"`
		Posts map[string]map[string]any `json:"posts"`
	}
	if err := jsonUnmarshal(b, &d); err != nil {
		return nil, err
	}
	var items []any
	for _, s := range d.Sends {
		at, _ := time.Parse(time.RFC3339, fmt.Sprint(s["at"]))
		if at.Before(now) || at.After(now.Add(time.Duration(hours)*time.Hour)) {
			continue
		}
		p := d.Posts[fmt.Sprint(s["post_id"])]
		items = append(items, map[string]any{"at": s["at"], "post_id": s["post_id"], "title": p["title"], "status": p["status"],
			"targets": s["targets"], "schedule_id": s["schedule_id"], "occ": s["occ"]})
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i].(map[string]any)["at"]) < fmt.Sprint(items[j].(map[string]any)["at"])
	})
	return items, nil
}

func cmdAgenda(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	if err := checkFlags(o, "--hours"); err != nil {
		return err
	}
	hours := 24
	if h := o.flags["--hours"]; h != "" {
		if _, err := fmt.Sscan(h, &hours); err != nil || hours <= 0 || hours > 24*31 {
			return errf("usage", "Use 1 to 744.", "--hours must be a whole number of hours")
		}
	}
	conn := resolveConn(o.profile, o.url, env.LocalURL)
	if conn.key == "" {
		return &Error{Kind: "auth", Message: "no API key", Hint: "Run `townsquare auth local`."}
	}
	items, err := agenda(o, conn, hours)
	if err != nil {
		return err
	}
	if items == nil {
		items = []any{}
	}
	return out(o, envelope(items, len(items), 0, 0, false))
}

// ---- skills and feedback ----

func cmdSkills(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	verb := "list"
	if len(o.words) > 1 {
		verb = o.words[1]
	}
	switch verb {
	case "list":
		return out(o, envelope([]any{map[string]any{"name": "townsquare", "description": skillDescription(), "show": "townsquare skills show townsquare"}}, 1, 0, 0, false))
	case "show":
		if len(o.words) > 2 && o.words[2] != "townsquare" {
			return errf("not_found", "Available: townsquare.", "no skill %q", o.words[2])
		}
		_, err := os.Stdout.WriteString(townsquare.Skill)
		return err
	case "path":
		dir := filepath.Join(configDir(), "skills", "townsquare")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		p := filepath.Join(dir, "SKILL.md")
		cur, _ := os.ReadFile(p)
		changed := string(cur) != townsquare.Skill
		if changed {
			if err := writeFileAtomic(p, []byte(townsquare.Skill), 0o644); err != nil {
				return err
			}
		}
		return out(o, map[string]any{"path": p, "changed": changed})
	}
	return errf("usage", "Use: skills list | show [NAME] | path", "unknown skills command %q", verb)
}

func skillDescription() string {
	for _, l := range strings.Split(townsquare.Skill, "\n") {
		if strings.HasPrefix(l, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "description:"))
		}
	}
	return ""
}

func cmdFeedback(o *opts) error {
	if builtinHelp(o) {
		return nil
	}
	path := filepath.Join(configDir(), "feedback.jsonl")
	if len(o.words) > 1 && o.words[1] == "list" {
		b, _ := os.ReadFile(path)
		var items []any
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var m map[string]any
			if json.Unmarshal([]byte(l), &m) == nil {
				items = append(items, m)
			}
		}
		if items == nil {
			items = []any{}
		}
		return out(o, envelope(items, len(items), 0, 0, false))
	}
	if err := checkFlags(o, "--command"); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(o.words[1:], " "))
	if text == "" {
		return errf("usage", "Example: townsquare feedback \"--send-at rejected 6pm\" --command 'posts create'", "feedback needs some text")
	}
	if o.dryRun {
		return out(o, map[string]any{"dry_run": true, "action": "append to " + path, "text": text, "validated": "local"})
	}
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	rec := map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "cli_version": env.Version, "text": text, "command": o.flags["--command"]}
	b, _ := json.Marshal(rec)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	f.Close()
	if err != nil {
		return err
	}
	return out(o, map[string]any{"saved": path, "changed": true, "upstream": nil})
}
