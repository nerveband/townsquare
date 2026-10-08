package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	townsquare "github.com/nerveband/townsquare"
)

func repo(p string) string { return filepath.Join("..", "..", p) }

// The CLI is generated from the contract; every operation must become a
// command, and the schema must pass its own rules (vocabulary, references,
// examples that parse).
func TestSchemaFollowsTheContract(t *testing.T) {
	cs, err := Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) < 60 {
		t.Fatalf("only %d remote commands", len(cs))
	}
	for _, c := range cs {
		if c.Effects != "read_only" && c.Effects != "idempotent" && c.Effects != "non_idempotent" {
			t.Errorf("%s: effects %q isn't declared", c.Name, c.Effects)
		}
		if c.OutputKind == "data" && c.Cardinality == "" {
			t.Errorf("%s: no cardinality", c.Name)
		}
		if c.Cardinality == "unbounded" && !c.Paginated && c.Name != "targets refresh" {
			t.Errorf("%s: unbounded but not paged on the server", c.Name)
		}
	}
	if errs := validateSchema(); len(errs) > 0 {
		t.Fatalf("schema problems:\n%s", strings.Join(errs, "\n"))
	}
}

func TestDocsAreGenerated(t *testing.T) {
	md, err := Markdown()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(repo("docs/cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != md {
		t.Fatal("docs/cli.md is out of date: run `make spec`")
	}
}

var cmdRef = regexp.MustCompile("townsquare ((?:[a-z][a-z-]*)(?: [a-z][a-z-]*){0,3})")

// Every `townsquare ...` command named in the skill and docs must exist.
func TestDocsNameRealCommands(t *testing.T) {
	cs, _ := Commands()
	known := map[string]bool{}
	for _, c := range cs {
		known[c.Name] = true
	}
	for _, s := range builtinSpecs {
		known[s.Name] = true
		known[strings.Fields(s.Name)[0]] = true
	}
	for n := range localSpecs {
		known[n] = true
	}
	files := []string{"skills/townsquare/SKILL.md", "README.md", "AGENTS.md", "internal/server/guide.md", "docs/releasing.md"}
	for _, f := range files {
		b, err := os.ReadFile(repo(f))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range cmdRef.FindAllStringSubmatch(string(b), -1) {
			words := strings.Fields(m[1])
			ok := false
			for n := len(words); n > 0; n-- {
				if known[strings.Join(words[:n], " ")] {
					ok = true
					break
				}
			}
			if !ok && !strings.HasPrefix(m[1], "is ") && !strings.HasPrefix(m[1], "on ") && words[0] != "was" && words[0] != "a" && words[0] != "app" && words[0] != "server" {
				t.Errorf("%s mentions `townsquare %s`, which isn't a command", f, m[1])
			}
		}
	}
	// Code blocks in the skill must parse as real invocations.
	for _, line := range strings.Split(townsquare.Skill, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
		if !strings.HasPrefix(line, "townsquare ") || strings.Contains(line, "`") {
			continue
		}
		if i := strings.Index(line, "#"); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		f := splitShell(line)
		o, err := parse(f[1:])
		if err != nil {
			t.Errorf("SKILL.md: %q: %v", line, err)
			continue
		}
		if b, ok := builtins[o.words[0]]; ok && b != nil {
			continue
		}
		if c, _ := match(cs, o.words); c != nil && !strings.HasSuffix(line, "--caption @dinner.txt") {
			if err := checkExample(c, line); err != nil {
				t.Errorf("SKILL.md: %q: %v", line, err)
			}
		}
	}
}

func TestSkillIsValid(t *testing.T) {
	s := townsquare.Skill
	if !strings.HasPrefix(s, "---\nname: townsquare\ndescription: ") {
		t.Fatal("SKILL.md needs name and description front matter")
	}
	if n := strings.Count(s, "\n"); n > 500 {
		t.Fatalf("SKILL.md has %d lines; keep it under 500", n)
	}
	for _, must := range []string{"--dry-run", "--yes", "--idempotency-key", "not instructions", "Do not"} {
		if !strings.Contains(s, must) {
			t.Errorf("SKILL.md should mention %q", must)
		}
	}
}

func TestParseAndConvert(t *testing.T) {
	o, err := parse([]string{"-o", "json", "posts", "list", "--status", "draft", "--limit=5", "--fields", "id,title", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if o.output != "json" || o.limit != 5 || !o.limitSet || len(o.fields) != 2 || !o.yes || o.flags["--status"] != "draft" {
		t.Fatalf("parsed %+v", o)
	}
	if _, err := parse([]string{"--output", "yaml"}); err == nil {
		t.Fatal("bad --output accepted")
	}
	if _, err := parse([]string{"posts", "list", "--limit", "x"}); err == nil {
		t.Fatal("bad --limit accepted")
	}
	p := Param{Flag: "--kind", Type: "array", Items: "string", Enum: []string{"group", "channel"}}
	if v, err := convert(p, "group,channel"); err != nil || len(v.([]any)) != 2 {
		t.Fatalf("convert list: %v %v", v, err)
	}
	if _, err := convert(p, "group,nope"); err == nil || !strings.Contains(err.(*Error).Hint, "group, channel") {
		t.Fatalf("enum error should list valid values: %v", err)
	}
	if v, _ := convert(Param{Type: "string"}, "@@literal"); v != "@literal" {
		t.Fatalf("@@ escape: %v", v)
	}
	f := filepath.Join(t.TempDir(), "c.txt")
	_ = os.WriteFile(f, []byte("from a file\n"), 0o600)
	if v, _ := convert(Param{Type: "string"}, "@"+f); v != "from a file" {
		t.Fatalf("@file: %q", v)
	}
	if v, _ := convert(Param{Type: "integer", Flag: "--n"}, "7"); v != int64(7) {
		t.Fatalf("integer: %v", v)
	}
}

func TestShapingAndDelivery(t *testing.T) {
	items := []any{map[string]any{"id": 1, "title": "a", "x": map[string]any{"y": 1}}, map[string]any{"id": 2, "title": "b"}}
	env := envelope(items, 5, 0, 2, true)
	if env["truncated"] != true || env["next_offset"] != 2 {
		t.Fatalf("envelope %v", env)
	}
	if p := project(items[0], []string{"id", "x.y"}).(map[string]any); p["title"] != nil || p["x"].(map[string]any)["y"] != 1 {
		t.Fatalf("project %v", p)
	}
	if v, err := transform(map[string]any{"items": items}, "items.#.title"); err != nil || len(v.([]any)) != 2 {
		t.Fatalf("transform %v %v", v, err)
	}
	if c := collapse(items[0], 1).(map[string]any); c["x"] != "{… 1 fields}" {
		t.Fatalf("collapse %v", c)
	}
	dir := t.TempDir()
	target := "file:" + filepath.Join(dir, "out.bin")
	if _, err := deliver(target, []byte("one"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := deliver(target, []byte("two"), false); err == nil {
		t.Fatal("overwrote without --overwrite")
	}
	if _, err := deliver(target, []byte("two"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := deliver("ftp:x", nil, false); err == nil || !strings.Contains(err.(*Error).Hint, "file:PATH") {
		t.Fatal("unknown delivery scheme should list the supported ones")
	}
}

func TestErrorTable(t *testing.T) {
	seen := map[string]bool{}
	codes := map[int]bool{}
	for _, k := range Kinds {
		if seen[k.Kind] {
			t.Errorf("duplicate kind %s", k.Kind)
		}
		seen[k.Kind] = true
		codes[k.ExitCode] = true
	}
	for _, o := range Outcomes {
		if codes[o.Code] {
			t.Errorf("outcome %s reuses an error exit code", o.Name)
		}
	}
	if e := fromHTTP(429, []byte(`{"error":"slow down"}`), "12"); e.Kind != "rate_limit" || e.Details["retry_after_seconds"] != 12 {
		t.Fatalf("429: %+v", e)
	}
	if e := fromHTTP(409, []byte(`{"error":"a post is due","code":"busy"}`), ""); e.Kind != "busy" {
		t.Fatalf("busy: %+v", e)
	}
}

func TestProfilesKeepSecretsApart(t *testing.T) {
	t.Setenv("TOWNSQUARE_CONFIG_DIR", t.TempDir())
	t.Setenv("TOWNSQUARE_API_KEY", "")
	t.Setenv("TOWNSQUARE_URL", "")
	t.Setenv("TOWNSQUARE_PROFILE", "")
	if err := setProfile("home", "https://ts.example.org", "tsq_secretsecret", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(configDir(), "cli.json"))
	if strings.Contains(string(b), "tsq_") {
		t.Fatal("key stored with non-secret config")
	}
	r := resolveConn("", "", "http://127.0.0.1:8890")
	if r.URL.Value != "https://ts.example.org" || r.URL.Source != "profile" || r.key != "tsq_secretsecret" || strings.Contains(r.Key.Value, "secret") {
		t.Fatalf("resolve %+v", r)
	}
	r = resolveConn("", "http://other:1", "")
	if r.URL.Source != "flag" {
		t.Fatal("flag must win")
	}
	t.Setenv("TOWNSQUARE_URL", "http://env:2")
	if r = resolveConn("", "", ""); r.URL.Source != "env" {
		t.Fatal("env must beat the profile")
	}
}
