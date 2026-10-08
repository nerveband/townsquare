package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Env is what the CLI needs from the program around it.
type Env struct {
	Version  string
	DataDir  string                                      // this computer's Townsquare data folder
	LocalURL string                                      // its web address (from config.json)
	MakeKey  func(name, scope string) (string, error)    // creates an API key in the local database
	Local    map[string]func(args []string) (int, error) // local commands (serve, update, ...)
}

var env Env

// Handles reports whether the CLI (not a local command) runs these arguments.
func Handles(args []string) bool {
	w := firstWord(args)
	if w == "" {
		return true // flags only: --help, --version
	}
	if _, ok := localSpecs[w]; ok {
		return false
	}
	return true
}

func firstWord(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		name, _, hasVal := strings.Cut(a, "=")
		if !hasVal && isGlobal(name) && !boolGlobals[name] {
			i++ // skip the value
		}
	}
	return ""
}

// Run executes a CLI command and returns the exit code.
func Run(e Env, args []string) int {
	env = e
	o, err := parse(args)
	if err != nil {
		Fail(err, os.Getenv("TOWNSQUARE_OUTPUT") == "json" || !isTTY(os.Stdout))
	}
	if err := run(o); err != nil {
		Fail(err, o.jsonErrors())
	}
	return 0
}

func run(o *opts) error {
	if len(o.words) == 0 {
		if o.flags["--version"] != "" {
			fmt.Println("townsquare", env.Version)
			return nil
		}
		return rootHelp(os.Stdout)
	}
	if b, ok := builtins[o.words[0]]; ok {
		return b(o)
	}
	cs, err := Commands()
	if err != nil {
		return err
	}
	c, rest := match(cs, o.words)
	if c == nil {
		if g := groupOf(cs, o.words); len(g) > 0 {
			return groupHelp(os.Stdout, strings.Join(o.words, " "), g)
		}
		return errf("usage", "Run `townsquare --help` for commands, or `townsquare schema` for all of them as JSON.",
			"unknown command %q", strings.Join(o.words, " "))
	}
	if o.help {
		return commandHelp(os.Stdout, c)
	}
	if o.reqSchema || o.respSchema {
		s := c.BodySchema
		if o.respSchema {
			s = c.RespSchema
		}
		if s == nil {
			s = map[string]any{}
		}
		return printDoc(os.Stdout, s, "json", isTTY(os.Stdout))
	}
	return execute(o, c, rest)
}

// match finds the longest command name that the words start with.
func match(cs []*Command, words []string) (*Command, []string) {
	var best *Command
	for _, c := range cs {
		if len(c.Words) <= len(words) && strings.Join(words[:len(c.Words)], " ") == c.Name {
			if best == nil || len(c.Words) > len(best.Words) {
				best = c
			}
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, words[len(best.Words):]
}

func groupOf(cs []*Command, words []string) []*Command {
	var out []*Command
	prefix := strings.Join(words, " ") + " "
	for _, c := range cs {
		if strings.HasPrefix(c.Name+" ", prefix) {
			out = append(out, c)
		}
	}
	return out
}

var (
	jidRE = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,128}$`)
	occRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
)

// request is a built API call.
type request struct {
	method, path string
	query        url.Values
	body         map[string]any
	rawBody      []byte
	contentType  string
	idemKey      string
}

func build(o *opts, c *Command, rest []string) (*request, error) {
	r := &request{method: c.Method, path: c.Path, query: url.Values{}}
	// Positional id.
	if c.Positional != nil {
		if len(rest) == 0 {
			return nil, errf("usage", "Example: "+firstExample(c), "%s needs a %s", c.Name, c.Positional.Name)
		}
		id := rest[0]
		rest = rest[1:]
		if c.Positional.Type == "integer" {
			if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
				return nil, errf("usage", "Use a number from the matching list command.", "%s must be a positive number, not %q", c.Positional.Name, id)
			}
		} else if !jidRE.MatchString(id) || strings.Contains(id, "..") {
			return nil, errf("usage", "Chat ids look like 120363000000000102@g.us or tg:ch:123:456.", "invalid %s %q", c.Positional.Name, id)
		}
		r.path = strings.Replace(r.path, "{"+c.Positional.Name+"}", url.PathEscape(id), 1)
	}
	if len(rest) > 0 {
		return nil, errf("usage", "Example: "+firstExample(c), "unexpected argument %q", rest[0])
	}
	// Known flags for this command.
	valid := map[string]Param{}
	var names []string
	for _, p := range append(append([]Param{}, c.Query...), c.Body...) {
		if p.Name == "limit" || p.Name == "offset" {
			continue // global --limit / --offset
		}
		valid[p.Flag] = p
		names = append(names, p.Flag)
	}
	if c.Multipart {
		names = append(names, "--file")
	}
	for _, f := range o.flagOrder {
		p, ok := valid[f]
		if !ok && !(c.Multipart && f == "--file") {
			return nil, unknownFlag(f, names)
		}
		if ok && o.bare[f] && p.Type != "boolean" {
			return nil, errf("usage", "Example: "+firstExample(c), "%s needs a value", f)
		}
	}
	for _, p := range c.Query {
		if raw, ok := o.flags[p.Flag]; ok {
			v, err := convert(p, raw)
			if err != nil {
				return nil, err
			}
			switch x := v.(type) {
			case []any:
				parts := make([]string, len(x))
				for i, y := range x {
					parts[i] = fmt.Sprint(y)
				}
				r.query.Set(p.Name, strings.Join(parts, ","))
			default:
				r.query.Set(p.Name, fmt.Sprint(x))
			}
		} else if p.Required {
			return nil, errf("usage", "Example: "+firstExample(c), "%s needs %s", c.Name, p.Flag)
		}
		if p.Name == "occ" || p.Name == "to" || p.Name == "from" {
			if v := r.query.Get(p.Name); v != "" && hasCtl(v) {
				return nil, errf("usage", "", "%s contains control characters", p.Flag)
			}
		}
	}
	if hasParam(c.Query, "limit") && o.limitSet {
		r.query.Set("limit", strconv.Itoa(o.limit))
	}
	if hasParam(c.Query, "offset") && o.offsetSet {
		r.query.Set("offset", strconv.Itoa(o.offset))
	}
	if c.Paginated && o.count {
		r.query.Set("limit", "0")
	}
	// Body.
	if c.Multipart {
		if f, ok := o.flags["--file"]; ok {
			return multipartBody(r, o, c, f)
		}
	}
	if c.HasBody {
		r.body = map[string]any{}
		if o.body != "" {
			raw, err := readValue(o.body)
			if err != nil {
				return nil, err
			}
			if err := jsonUnmarshal([]byte(raw), &r.body); err != nil {
				return nil, errf("usage", "Pass a JSON object, @file.json, or - for stdin.", "--body: invalid JSON: %v", err)
			}
		}
		for _, p := range c.Body {
			raw, ok := o.flags[p.Flag]
			if !ok {
				continue
			}
			if p.Secret && !strings.HasPrefix(raw, "@") && !o.quiet {
				fmt.Fprintf(os.Stderr, "Note: %s was given on the command line; pass @file or @- to keep secrets out of shell history.\n", p.Flag)
			}
			v, err := convert(p, raw)
			if err != nil {
				return nil, err
			}
			r.body[p.Name] = v
		}
		for _, p := range c.Body {
			if _, ok := r.body[p.Name]; p.Required && !ok {
				return nil, errf("usage", "Example: "+firstExample(c), "%s needs %s (or --body with %q)", c.Name, p.Flag, p.Name)
			}
		}
		b, _ := json.Marshal(r.body)
		r.rawBody, r.contentType = b, "application/json"
	}
	if c.Effects == "non_idempotent" {
		r.idemKey = o.idemKey
		if r.idemKey == "" {
			r.idemKey = newKey()
		}
	}
	return r, nil
}

func multipartBody(r *request, o *opts, c *Command, path string) (*request, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errf("usage", "", "can't open %s: %v", path, err)
	}
	defer f.Close()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if k, ok := o.flags["--kind"]; ok {
		_ = mw.WriteField("kind", k)
	}
	part, _ := mw.CreateFormFile("file", filepath.Base(path))
	if _, err := io.Copy(part, f); err != nil {
		return nil, err
	}
	_ = mw.Close()
	r.rawBody, r.contentType = buf.Bytes(), mw.FormDataContentType()
	r.idemKey = o.idemKey
	if r.idemKey == "" {
		r.idemKey = newKey()
	}
	return r, nil
}

func hasParam(ps []Param, name string) bool {
	for _, p := range ps {
		if p.Name == name {
			return true
		}
	}
	return false
}

func firstExample(c *Command) string {
	if len(c.Examples) > 0 {
		return c.Examples[0]
	}
	return "townsquare " + c.Name + " --help"
}

func newKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "cli-" + hex.EncodeToString(b)
}

// reversible: changes recorded in history can be undone.
func reversible(c *Command) bool {
	switch c.Tag {
	case "Posts", "Sends", "Tags", "Clients", "Sets", "Settings", "Targets":
		return c.Name != "posts send-now"
	}
	return false
}

func execute(o *opts, c *Command, rest []string) error {
	scopeNote = "remote"
	r, err := build(o, c, rest) // validation happens before anything is sent
	if err != nil {
		return err
	}
	conn := resolveConn(o.profile, o.url, env.LocalURL)
	if o.dryRun && c.Effects != "read_only" {
		return dryRun(o, c, r, conn)
	}
	if c.Confirm && !o.yes {
		if !isTTY(os.Stdin) || !isTTY(os.Stderr) {
			return &Error{Kind: "confirmation_required", Message: c.Name + " changes, sends or removes something and needs confirmation",
				Hint: "Preview with --dry-run, then re-run with --yes."}
		}
		fmt.Fprintf(os.Stderr, "%s %s on %s. Continue? [y/N] ", c.Name, strings.TrimPrefix(r.path, "/"), conn.URL.Value)
		var ans string
		_, _ = fmt.Fscanln(os.Stdin, &ans)
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
			return &Error{Kind: "confirmation_required", Message: "cancelled", Hint: "Pass --yes to skip the question."}
		}
	}
	if conn.key == "" {
		return &Error{Kind: "auth", Message: "no API key for " + conn.URL.Value,
			Hint: "On the Townsquare computer run `townsquare auth local`; elsewhere `townsquare auth login --url URL --profile NAME < keyfile` or set TOWNSQUARE_API_KEY."}
	}
	resp, body, err := call(o, c, r, conn)
	if err != nil {
		return err
	}
	if resp.StatusCode == 404 && c.Method == "DELETE" && c.Effects == "idempotent" {
		return printDoc(os.Stdout, map[string]any{"changed": false, "status": "absent", "note": "nothing to remove (already gone or never existed)"}, o.resolvedOutput(), isTTY(os.Stdout))
	}
	if resp.StatusCode >= 300 {
		return fromHTTP(resp.StatusCode, body, resp.Header.Get("Retry-After"))
	}
	if resp.Header.Get("Idempotent-Replayed") == "true" && !o.quiet {
		fmt.Fprintln(os.Stderr, "Note: replayed the earlier result for this --idempotency-key; nothing was done twice.")
	}
	if c.OutputKind == "opaque" {
		where, err := deliver(o.deliver, body, o.overwrite)
		if err != nil {
			return err
		}
		if where != "stdout" && !o.quiet {
			fmt.Fprintf(os.Stderr, "Saved %d bytes (%s) to %s\n", len(body), c.MediaType, strings.TrimPrefix(where, "file:"))
		}
		return nil
	}
	if o.resolvedOutput() == "raw" {
		_, err := os.Stdout.Write(body)
		return err
	}
	var doc any
	if len(bytes.TrimSpace(body)) > 0 {
		if err := jsonUnmarshal(body, &doc); err != nil {
			return &Error{Kind: "general", Message: "the server sent something that isn't JSON", Hint: "Check --url points at Townsquare."}
		}
	}
	if o.wait {
		if doc, err = waitFor(o, c, conn, doc); err != nil {
			return err
		}
	}
	if m, ok := doc.(map[string]any); ok && c.Effects != "read_only" {
		if _, has := m["changed"]; !has {
			m["changed"] = true // the server made the change (it says "changed": false when there was nothing to do)
		}
	}
	doc, err = shape(o, c, doc, resp)
	if err != nil {
		return err
	}
	return printDoc(os.Stdout, doc, o.resolvedOutput(), isTTY(os.Stdout))
}

// shape applies the list envelope, --count, --id-only, --fields, --max-depth and --transform.
func shape(o *opts, c *Command, doc any, resp *http.Response) (any, error) {
	if arr, ok := doc.([]any); ok {
		total := len(arr)
		if t, err := strconv.Atoi(resp.Header.Get("X-Total-Count")); err == nil {
			total = t
		}
		if o.count {
			out := map[string]any{"count": total}
			if !c.Paginated {
				out["counted"] = "client" // the API has no count; Townsquare fetched the list and counted it
			}
			if o.transform != "" {
				return transform(out, o.transform)
			}
			return out, nil
		}
		if !c.Paginated && o.limitSet && o.limit < len(arr) {
			arr = arr[min(o.offset, len(arr)):min(o.offset+o.limit, len(arr))]
		}
		items := make([]any, len(arr))
		for i, it := range arr {
			switch {
			case o.idOnly:
				items[i] = idOf(it)
			default:
				items[i] = project(it, o.fields)
			}
		}
		lim := 0
		if o.limitSet {
			lim = o.limit
		}
		doc = envelope(items, total, o.offset, lim, c.Paginated || (o.limitSet && !c.Paginated))
	} else if len(o.fields) > 0 {
		doc = project(doc, o.fields)
	}
	if o.count {
		if m, ok := doc.(map[string]any); ok {
			for _, k := range []string{"items", "sends", "chats"} {
				if a, ok := m[k].([]any); ok {
					return map[string]any{"count": len(a), "counted": "client"}, nil
				}
			}
		}
		return nil, errf("usage", "Use --count with a list command.", "%s doesn't return a list", c.Name)
	}
	if o.maxDepth > 0 {
		doc = collapse(doc, o.maxDepth)
	}
	if o.transform != "" {
		return transform(doc, o.transform)
	}
	return doc, nil
}

func dryRun(o *opts, c *Command, r *request, conn Resolved) error {
	out := map[string]any{"dry_run": true, "scope": "remote", "command": c.Name, "action": c.Summary, "effects": c.Effects,
		"method": r.method, "path": "/api/v1" + r.path, "reversible": reversible(c), "validated": "local", "server": conn.URL.Value}
	if len(r.query) > 0 {
		out["query"] = r.query
	}
	if r.body != nil {
		out["body"] = r.body
	}
	if c.Confirm {
		out["needs"] = "--yes"
	}
	// Ask the server where it can check without changing anything.
	if conn.key != "" {
		switch {
		case c.Name == "posts create" || c.Name == "posts update":
			body := r.body
			if c.Name == "posts update" { // preview the post as it would be after the change
				if resp, b, err := call(o, &Command{Method: "GET", Effects: "read_only"}, &request{method: "GET", path: r.path, query: url.Values{}}, conn); err == nil && resp.StatusCode == 200 {
					var cur map[string]any
					if jsonUnmarshal(b, &cur) == nil {
						for k, v := range r.body {
							cur[k] = v
						}
						body = cur
					}
				}
			}
			raw, _ := json.Marshal(body)
			resp, b, err := call(o, &Command{Method: "POST", Effects: "read_only"}, &request{method: "POST", path: "/posts/preview", query: url.Values{}, rawBody: raw, contentType: "application/json"}, conn)
			if err == nil {
				var pv any
				_ = jsonUnmarshal(b, &pv)
				out["validated"] = "server"
				out["preview"] = pv
				if resp.StatusCode >= 300 {
					out["would_fail"] = fromHTTP(resp.StatusCode, b, "")
				}
			}
		case c.Method == "DELETE" || strings.HasSuffix(c.Path, "/send-now") || strings.HasSuffix(c.Path, "/pause") || strings.HasSuffix(c.Path, "/resume") || c.Method == "PATCH":
			if c.Positional != nil {
				resp, b, err := call(o, &Command{Method: "GET", Effects: "read_only"}, &request{method: "GET", path: strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(r.path, "/send-now"), "/pause"), "/resume"), "/"), query: url.Values{}}, conn)
				if err == nil {
					if resp.StatusCode == 200 {
						var t any
						_ = jsonUnmarshal(b, &t)
						out["target"] = collapse(t, 1)
						out["validated"] = "server"
					} else if resp.StatusCode == 404 {
						out["target"] = nil
						out["validated"] = "server"
						out["note"] = "no such record; the real command would change nothing"
					}
				}
			}
		}
	}
	return printDoc(os.Stdout, out, o.resolvedOutput(), isTTY(os.Stdout))
}

// call sends a request with retries where retrying is safe.
func call(o *opts, c *Command, r *request, conn Resolved) (*http.Response, []byte, error) {
	u := conn.URL.Value + "/api/v1" + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	timeout := o.timeout
	if c.Multipart && timeout < 5*time.Minute {
		timeout = 5 * time.Minute
	}
	retrySafe := c.Effects != "non_idempotent" || r.idemKey != ""
	attempts := 1
	if retrySafe {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i*i) * 500 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		req, _ := http.NewRequestWithContext(ctx, r.method, u, bytes.NewReader(r.rawBody))
		if r.contentType != "" {
			req.Header.Set("Content-Type", r.contentType)
		}
		if conn.key != "" {
			req.Header.Set("Authorization", "Bearer "+conn.key)
		}
		if r.idemKey != "" {
			req.Header.Set("Idempotency-Key", r.idemKey)
		}
		req.Header.Set("User-Agent", "townsquare-cli/"+env.Version)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		checkVersion(o, resp.Header.Get("X-Townsquare-Version"))
		if retrySafe && i+1 < attempts && (resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 504 || resp.StatusCode == 429) {
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 30 {
				time.Sleep(time.Duration(s) * time.Second)
			}
			lastErr = fromHTTP(resp.StatusCode, body, resp.Header.Get("Retry-After"))
			if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "" {
				continue
			}
		}
		return resp, body, nil
	}
	return nil, nil, netError(lastErr, c, r, conn)
}

func netError(err error, c *Command, r *request, conn Resolved) error {
	if e, ok := err.(*Error); ok {
		return e
	}
	var ne net.Error
	isTimeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
	if c.Effects == "non_idempotent" && isTimeout {
		return &Error{Kind: "uncertain_outcome", Message: c.Name + " timed out; it may or may not have happened",
			Hint:    "Re-run the same command with --idempotency-key " + r.idemKey + " (safe: it won't happen twice), or check with the matching list command.",
			Details: map[string]any{"idempotency_key": r.idemKey}}
	}
	if isTimeout {
		return &Error{Kind: "timeout", Message: "no answer from " + conn.URL.Value + " in time", Hint: "Retry, or raise --timeout (for example --timeout 2m)."}
	}
	return &Error{Kind: "network", Message: "can't reach Townsquare at " + conn.URL.Value + ": " + shortErr(err),
		Hint: "Is it running? Check --url or the profile (`townsquare context`), or run `townsquare doctor`."}
}

func shortErr(err error) string {
	if err == nil {
		return "unknown error"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 && len(s) > 120 {
		s = s[i+2:]
	}
	return s
}

var versionWarned bool

func checkVersion(o *opts, server string) {
	if versionWarned || server == "" || server == env.Version || o.quiet {
		return
	}
	versionWarned = true
	fmt.Fprintf(os.Stderr, "Note: the server runs Townsquare %s and this CLI is %s. Commands follow this CLI's contract; run `townsquare update` if they disagree.\n", server, env.Version)
}

// waitFor blocks until linking, an update or a restart finishes.
func waitFor(o *opts, c *Command, conn Resolved, doc any) (any, error) {
	deadline := time.Now().Add(o.timeout)
	poll := func(path string) (any, int) {
		resp, b, err := call(&opts{timeout: 5 * time.Second, quiet: true}, &Command{Method: "GET", Effects: "read_only"}, &request{method: "GET", path: path, query: url.Values{}}, conn)
		if err != nil {
			return nil, 0
		}
		var d any
		_ = jsonUnmarshal(b, &d)
		return d, resp.StatusCode
	}
	switch c.Name {
	case "whatsapp link":
		for time.Now().Before(deadline) {
			d, _ := poll("/whatsapp")
			if m, ok := d.(map[string]any); ok {
				if m["linked"] == true && m["connected"] == true {
					return m, nil
				}
				if l, ok := m["link"].(map[string]any); ok && l["state"] == "error" {
					return nil, &Error{Kind: "unavailable", Message: "linking stopped: " + fmt.Sprint(l["message"]), Hint: "Run `townsquare whatsapp link` again."}
				}
				if !o.quiet {
					fmt.Fprintln(os.Stderr, "Waiting for the phone to scan the code (townsquare whatsapp qr --deliver file:qr.png)...")
				}
			}
			time.Sleep(3 * time.Second)
		}
	case "system update install", "system restart":
		want := ""
		if m, ok := doc.(map[string]any); ok {
			want, _ = m["version"].(string)
			if m["up_to_date"] == true {
				return doc, nil
			}
		}
		time.Sleep(2 * time.Second)
		for time.Now().Before(deadline) {
			d, code := poll("/update")
			if m, ok := d.(map[string]any); ok && code == 200 && (want == "" || m["current"] == want) {
				return map[string]any{"ok": true, "current": m["current"], "changed": true}, nil
			}
			time.Sleep(2 * time.Second)
		}
	default:
		return doc, nil
	}
	return doc, &Error{Kind: "timeout", Message: c.Name + " didn't finish within " + o.timeout.String(),
		Hint: "Run the same command again (it's safe), or wait longer with --timeout 10m.", Details: map[string]any{"last": doc}}
}

func sortedKeys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

var _ = sortedKeys

// FirstWord is the command word in args (skipping global flags and their values).
func FirstWord(args []string) string { return firstWord(args) }

// Usage makes a usage error (exit 2) for local commands.
func Usage(msg string) error {
	return &Error{Kind: "usage", Message: msg, Hint: "See `townsquare --help`."}
}
