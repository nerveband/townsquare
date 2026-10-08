package cli

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GlobalArg is a flag every command accepts (declared in `schema` global_args).
type GlobalArg struct {
	Name    string   `json:"name"`
	Short   string   `json:"short,omitempty"`
	Type    string   `json:"type"`
	Enum    []string `json:"enum,omitempty"`
	Default any      `json:"default,omitempty"`
	Desc    string   `json:"description"`
	Aliases []string `json:"x-aliases,omitempty"`
}

// Globals is the one vocabulary of cross-cutting flags.
var Globals = []GlobalArg{
	{Name: "--output", Short: "-o", Type: "string", Enum: []string{"auto", "json", "jsonl", "text", "raw"}, Default: "auto",
		Desc: "Output format. auto = text on a terminal, json when piped. raw = the exact API response.", Aliases: []string{"--json (= -o json)"}},
	{Name: "--format-error", Type: "string", Enum: []string{"auto", "json", "text"}, Default: "auto", Desc: "Error format on stderr; auto follows --output"},
	{Name: "--profile", Type: "string", Desc: "Saved server and key to use (townsquare profiles list)"},
	{Name: "--url", Type: "url", Desc: "Townsquare address, overriding the profile (for example https://townsquare.example.ts.net)"},
	{Name: "--fields", Type: "string[]", Desc: "Only these fields (comma list; dotted paths allowed), for records and list items"},
	{Name: "--transform", Type: "string", Desc: "Print one value by path, for example items.0.id or items.#.title"},
	{Name: "--id-only", Type: "boolean", Desc: "Lists: print only ids"},
	{Name: "--count", Type: "boolean", Desc: "Lists: print only the number of matches (server-counted for paged lists)"},
	{Name: "--limit", Type: "integer", Desc: "Lists: at most this many (paged on the server)"},
	{Name: "--offset", Type: "integer", Desc: "Lists: skip this many"},
	{Name: "--max-depth", Type: "integer", Desc: "Collapse nested objects deeper than this"},
	{Name: "--timeout", Type: "duration", Default: "30s", Desc: "Per-request timeout (and the limit for --wait)"},
	{Name: "--quiet", Short: "-q", Type: "boolean", Desc: "No notes on stderr; stdout carries only data"},
	{Name: "--dry-run", Type: "boolean", Desc: "Show what a change would do; send nothing"},
	{Name: "--yes", Short: "-y", Type: "boolean", Desc: "Confirm a command that deletes, sends or restarts (required without a terminal)"},
	{Name: "--idempotency-key", Type: "string", Desc: "Make a create or send safe to retry: a repeat with the same key returns the first result"},
	{Name: "--body", Type: "json", Desc: "The full request body as JSON, @file.json, or - for stdin; flags override its fields"},
	{Name: "--deliver", Type: "string", Default: "stdout", Desc: "Files (media, CSV, QR codes): stdout or file:PATH"},
	{Name: "--overwrite", Type: "boolean", Desc: "Let --deliver file:PATH replace an existing file"},
	{Name: "--wait", Type: "boolean", Desc: "Block until linking, an update or a restart finishes (bounded by --timeout)"},
	{Name: "--request-schema", Type: "boolean", Desc: "Print the command's request JSON Schema and exit (no network)"},
	{Name: "--response-schema", Type: "boolean", Desc: "Print the command's response JSON Schema and exit (no network)"},
	{Name: "--help", Short: "-h", Type: "boolean", Desc: "Help for any command or group"},
}

type opts struct {
	output, formatError, profile, url, transform, idemKey, body, deliver string
	fields                                                               []string
	idOnly, count, quiet, dryRun, yes, overwrite, wait, reqSchema        bool
	respSchema, help, outputSet                                          bool
	limit, offset, maxDepth                                              int
	limitSet, offsetSet                                                  bool
	timeout                                                              time.Duration
	words                                                                []string          // non-flag tokens
	flags                                                                map[string]string // command flags: --name -> raw value
	flagOrder                                                            []string
	bare                                                                 map[string]bool // flags given without a value
}

var boolGlobals = map[string]bool{"--json": true, "--id-only": true, "--count": true, "--quiet": true, "-q": true, "--dry-run": true,
	"--yes": true, "-y": true, "--overwrite": true, "--wait": true, "--request-schema": true, "--response-schema": true, "--help": true, "-h": true}

func isGlobal(name string) bool {
	if boolGlobals[name] {
		return true
	}
	for _, g := range Globals {
		if g.Name == name || g.Short == name {
			return true
		}
	}
	return false
}

// parse splits tokens into global options, command flags and words.
func parse(args []string) (*opts, error) {
	o := &opts{output: "auto", formatError: "auto", deliver: "stdout", timeout: 30 * time.Second, flags: map[string]string{}, bare: map[string]bool{}}
	if v := os.Getenv("TOWNSQUARE_OUTPUT"); v != "" {
		o.output = v
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			o.words = append(o.words, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" || isNumber(a) {
			o.words = append(o.words, a)
			continue
		}
		name, val, hasVal := strings.Cut(a, "=")
		next := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", errf("usage", "Give a value, for example "+name+" VALUE.", "%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		if boolGlobals[name] {
			b := true
			if hasVal {
				pb, err := strconv.ParseBool(val)
				if err != nil {
					return nil, errf("usage", "Use "+name+" or "+name+"=false.", "%s takes true or false, not %q", name, val)
				}
				b = pb
			}
			switch name {
			case "--json":
				if b {
					o.output, o.outputSet = "json", true
				}
			case "--id-only":
				o.idOnly = b
			case "--count":
				o.count = b
			case "--quiet", "-q":
				o.quiet = b
			case "--dry-run":
				o.dryRun = b
			case "--yes", "-y":
				o.yes = b
			case "--overwrite":
				o.overwrite = b
			case "--wait":
				o.wait = b
			case "--request-schema":
				o.reqSchema = b
			case "--response-schema":
				o.respSchema = b
			case "--help", "-h":
				o.help = b
			}
			continue
		}
		if isGlobal(name) {
			v, err := next()
			if err != nil {
				return nil, err
			}
			if err := o.setGlobal(name, v); err != nil {
				return nil, err
			}
			continue
		}
		// A command flag: takes a value, except a bare boolean-style flag at the end
		// or before another flag, which means true.
		if !hasVal && (i+1 >= len(args) || (strings.HasPrefix(args[i+1], "--") && !isNumber(args[i+1]))) {
			val, hasVal = "true", true
			o.bare[name] = true
		}
		v, err := next()
		if err != nil {
			return nil, err
		}
		if old, ok := o.flags[name]; ok {
			v = old + "," + v // repeated flag = list
		} else {
			o.flagOrder = append(o.flagOrder, name)
		}
		o.flags[name] = v
	}
	return o, nil
}

func (o *opts) setGlobal(name, v string) error {
	enumCheck := func(valid []string) error {
		for _, x := range valid {
			if x == v {
				return nil
			}
		}
		return errf("usage", "Valid values: "+strings.Join(valid, ", ")+".", "invalid %s %q", name, v)
	}
	intVal := func() (int, error) {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, errf("usage", "Use a whole number, for example "+name+" 20.", "%s must be a whole number, not %q", name, v)
		}
		return n, nil
	}
	var err error
	switch name {
	case "--output", "-o":
		o.output, o.outputSet = v, true
		return enumCheck(Globals[0].Enum)
	case "--format-error":
		o.formatError = v
		return enumCheck(Globals[1].Enum)
	case "--profile":
		o.profile = v
	case "--url":
		if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
			return errf("usage", "For example --url https://townsquare.example.ts.net", "--url must start with http:// or https://")
		}
		o.url = v
	case "--fields":
		for _, f := range strings.Split(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				o.fields = append(o.fields, f)
			}
		}
	case "--transform":
		o.transform = v
	case "--limit":
		o.limit, err = intVal()
		o.limitSet = true
	case "--offset":
		o.offset, err = intVal()
		o.offsetSet = true
	case "--max-depth":
		o.maxDepth, err = intVal()
		if err == nil && o.maxDepth == 0 {
			err = errf("usage", "Use 1 or more.", "--max-depth must be at least 1")
		}
	case "--timeout":
		d, perr := time.ParseDuration(v)
		if perr != nil {
			if n, nerr := strconv.Atoi(v); nerr == nil {
				d, perr = time.Duration(n)*time.Second, nil
			}
		}
		if perr != nil || d <= 0 {
			return errf("usage", "For example --timeout 45s or --timeout 2m.", "invalid --timeout %q", v)
		}
		o.timeout = d
	case "--idempotency-key":
		if len(v) == 0 || len(v) > 200 {
			return errf("usage", "", "--idempotency-key must be 1 to 200 characters")
		}
		o.idemKey = v
	case "--body":
		o.body = v
	case "--deliver":
		o.deliver = v
	}
	return err
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// readValue expands @file, @- (stdin), @base64:file and @@literal.
func readValue(v string) (string, error) {
	switch {
	case strings.HasPrefix(v, "@@"):
		return v[1:], nil
	case v == "@-" || v == "-":
		b, err := io.ReadAll(os.Stdin)
		return strings.TrimSuffix(string(b), "\n"), err
	case strings.HasPrefix(v, "@base64:"):
		b, err := os.ReadFile(strings.TrimPrefix(v, "@base64:"))
		if err != nil {
			return "", errf("usage", "", "can't read %s: %v", strings.TrimPrefix(v, "@base64:"), err)
		}
		return base64.StdEncoding.EncodeToString(b), nil
	case strings.HasPrefix(v, "@"):
		b, err := os.ReadFile(v[1:])
		if err != nil {
			return "", errf("usage", "Write @@text for a value that starts with @.", "can't read %s: %v", v[1:], err)
		}
		return strings.TrimSuffix(string(b), "\n"), nil
	}
	return v, nil
}

// convert parses a flag value into the parameter's JSON type.
func convert(p Param, raw string) (any, error) {
	v, err := readValue(raw)
	if err != nil {
		return nil, err
	}
	if hasCtl(v) && p.Type != "string" {
		return nil, errf("usage", "", "%s contains control characters", p.Flag)
	}
	check := func(x string) error {
		if len(p.Enum) == 0 {
			return nil
		}
		for _, e := range p.Enum {
			if e == x {
				return nil
			}
		}
		return errf("usage", "Valid values: "+strings.Join(p.Enum, ", ")+".", "invalid %s %q", p.Flag, x)
	}
	switch p.Type {
	case "integer":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, errf("usage", "Use a whole number.", "%s must be a whole number, not %q", p.Flag, v)
		}
		return n, check(v)
	case "number":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, errf("usage", "", "%s must be a number, not %q", p.Flag, v)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, errf("usage", "Use true or false.", "%s must be true or false, not %q", p.Flag, v)
		}
		return b, nil
	case "array":
		if strings.HasPrefix(strings.TrimSpace(v), "[") {
			var a []any
			if err := jsonUnmarshal([]byte(v), &a); err != nil {
				return nil, errf("usage", "", "%s: invalid JSON list: %v", p.Flag, err)
			}
			return a, nil
		}
		var out []any
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if p.Items == "integer" {
				n, err := strconv.ParseInt(part, 10, 64)
				if err != nil {
					return nil, errf("usage", "", "%s takes whole numbers, not %q", p.Flag, part)
				}
				out = append(out, n)
				continue
			}
			if err := check(part); err != nil {
				return nil, err
			}
			out = append(out, part)
		}
		return out, nil
	case "object":
		var m any
		if err := jsonUnmarshal([]byte(v), &m); err != nil {
			return nil, errf("usage", "Pass a JSON object, for example '{\"key\": 1}'.", "%s: invalid JSON: %v", p.Flag, err)
		}
		return m, nil
	}
	return v, check(v)
}

func hasCtl(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' && r != '\r' {
			return true
		}
	}
	return false
}

// unknownFlag builds a usage error that lists what the command accepts.
func unknownFlag(name string, valid []string) error {
	sort.Strings(valid)
	hint := "This command takes: " + strings.Join(valid, ", ") + " (plus the global flags in `townsquare --help`)."
	if len(valid) == 0 {
		hint = "This command takes only the global flags (`townsquare --help`)."
	}
	if s := closest(name, valid); s != "" {
		hint = "Did you mean " + s + "? " + hint
	}
	return errf("usage", hint, "unknown flag %s", name)
}

func closest(name string, valid []string) string {
	best, bestD := "", 4
	for _, v := range valid {
		if d := lev(name, v); d < bestD {
			best, bestD = v, d
		}
	}
	return best
}

func lev(a, b string) int {
	d := make([]int, len(b)+1)
	for j := range d {
		d[j] = j
	}
	for i := 1; i <= len(a); i++ {
		prev := d[0]
		d[0] = i
		for j := 1; j <= len(b); j++ {
			cur := d[j]
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			d[j] = min(d[j]+1, d[j-1]+1, prev+c)
			prev = cur
		}
	}
	return d[len(b)]
}

var _ = fmt.Sprint
