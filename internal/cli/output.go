package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

func jsonUnmarshal(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

func isTTY(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// resolvedOutput turns auto into json (piped) or text (terminal).
func (o *opts) resolvedOutput() string {
	if o.output == "auto" || o.output == "" {
		if isTTY(os.Stdout) {
			return "text"
		}
		return "json"
	}
	return o.output
}

func (o *opts) jsonErrors() bool {
	switch o.formatError {
	case "json":
		return true
	case "text":
		return false
	}
	out := o.resolvedOutput()
	return out == "json" || out == "jsonl" || out == "raw"
}

// envelope wraps a list in {"items": [...]} with paging metadata.
func envelope(items []any, total, offset, limit int, paged bool) map[string]any {
	env := map[string]any{"items": items, "total": total}
	if scopeNote != "" {
		env["scope"] = scopeNote
	}
	if paged {
		env["offset"] = offset
		if limit > 0 {
			env["limit"] = limit
		}
		next := offset + len(items)
		if next < total {
			env["next_offset"] = next
			env["truncated"] = true
		} else {
			env["next_offset"] = nil
			env["truncated"] = false
		}
	} else {
		env["truncated"] = false
	}
	return env
}

// textCols are the --fields, used as table columns in text output.
var textCols []string

// scopeNote is repeated in list output: "remote" for server data, "local" otherwise.
var scopeNote = ""

// project keeps only the given (dotted) fields.
func project(v any, fields []string) any {
	m, ok := v.(map[string]any)
	if !ok || len(fields) == 0 {
		return v
	}
	out := map[string]any{}
	for _, f := range fields {
		if x, ok := getPath(m, f); ok {
			setPath(out, f, x)
		}
	}
	return out
}

func getPath(v any, path string) (any, bool) {
	cur := v
	for _, part := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			x, ok := c[part]
			if !ok {
				return nil, false
			}
			cur = x
		case []any:
			if part == "#" {
				return len(c), true
			}
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		n, ok := m[p].(map[string]any)
		if !ok {
			n = map[string]any{}
			m[p] = n
		}
		m = n
	}
	m[parts[len(parts)-1]] = v
}

// transform evaluates a path; "items.#.title" maps over a list.
func transform(v any, path string) (any, error) {
	if before, after, ok := strings.Cut(path, ".#."); ok {
		list, found := getPath(v, before)
		arr, isArr := list.([]any)
		if !found || !isArr {
			return nil, errf("usage", "Check the path with -o json first.", "--transform %q: %s is not a list", path, before)
		}
		out := make([]any, 0, len(arr))
		for _, x := range arr {
			if y, ok := getPath(x, after); ok {
				out = append(out, y)
			}
		}
		return out, nil
	}
	x, ok := getPath(v, path)
	if !ok {
		return nil, errf("usage", "Check the path with -o json first.", "--transform %q matched nothing", path)
	}
	return x, nil
}

// collapse replaces objects and lists below depth with a short note.
func collapse(v any, depth int) any {
	switch x := v.(type) {
	case map[string]any:
		if depth <= 0 {
			return fmt.Sprintf("{… %d fields}", len(x))
		}
		out := map[string]any{}
		for k, y := range x {
			out[k] = collapse(y, depth-1)
		}
		return out
	case []any:
		if depth <= 0 {
			return fmt.Sprintf("[… %d items]", len(x))
		}
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = collapse(y, depth-1)
		}
		return out
	}
	return v
}

func idOf(v any) any {
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"id", "jid", "version", "name"} {
			if x, ok := m[k]; ok {
				return x
			}
		}
	}
	return v
}

// print writes a data document in the chosen format.
func printDoc(w io.Writer, v any, format string, tty bool) error {
	switch format {
	case "jsonl":
		if m, ok := v.(map[string]any); ok {
			if items, ok := m["items"].([]any); ok {
				for _, it := range items {
					b, _ := json.Marshal(it)
					fmt.Fprintln(w, string(b))
				}
				return nil
			}
		}
		b, _ := json.Marshal(v)
		fmt.Fprintln(w, string(b))
	case "text":
		printText(w, v)
	default:
		var b []byte
		if tty {
			b, _ = json.MarshalIndent(v, "", "  ")
		} else {
			b, _ = json.Marshal(v)
		}
		fmt.Fprintln(w, string(b))
	}
	return nil
}

// clean strips terminal control sequences from remote text in human output.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0x1b || (r < 0x20 && r != '\t') || r == 0x7f {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return clean(x)
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		s := clean(string(b))
		if len(s) > 60 {
			s = s[:57] + "..."
		}
		return s
	default:
		return clean(fmt.Sprint(x))
	}
}

func printText(w io.Writer, v any) {
	m, isMap := v.(map[string]any)
	if isMap {
		if items, ok := m["items"].([]any); ok {
			printTable(w, items)
			if t, ok := m["truncated"].(bool); ok && t {
				fmt.Fprintf(w, "(%d of %v; next: --offset %v)\n", len(items), m["total"], m["next_offset"])
			}
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, k := range keys {
			fmt.Fprintf(tw, "%s\t%s\n", k, scalar(m[k]))
		}
		tw.Flush()
		return
	}
	if arr, ok := v.([]any); ok {
		printTable(w, arr)
		return
	}
	fmt.Fprintln(w, scalar(v))
}

func printTable(w io.Writer, items []any) {
	if len(items) == 0 {
		fmt.Fprintln(w, "(none)")
		return
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		for _, it := range items {
			fmt.Fprintln(w, scalar(it))
		}
		return
	}
	var cols []string
	for _, f := range textCols { // --fields picks the columns, in order
		if _, ok := first[f]; ok {
			cols = append(cols, f)
		}
	}
	for _, k := range []string{"id", "jid", "at", "name", "title", "kind", "status", "version", "summary", "actor", "next_at", "members", "allowed"} {
		if len(textCols) > 0 {
			break
		}
		if _, ok := first[k]; ok {
			cols = append(cols, k)
		}
	}
	if len(cols) == 0 {
		for k, v := range first {
			switch v.(type) {
			case map[string]any, []any:
				continue
			}
			cols = append(cols, k)
		}
		sort.Strings(cols)
	}
	if len(cols) > 7 {
		cols = cols[:7]
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(cols, "\t")))
	for _, it := range items {
		m, _ := it.(map[string]any)
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = scalar(m[c])
			if len(row[i]) > 48 {
				row[i] = row[i][:45] + "..."
			}
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
}

// deliver writes an artifact to stdout or file:PATH (atomically; no overwrite
// unless --overwrite).
func deliver(target string, body []byte, overwrite bool) (string, error) {
	switch {
	case target == "" || target == "stdout":
		_, err := os.Stdout.Write(body)
		return "stdout", err
	case strings.HasPrefix(target, "file:"):
		path := strings.TrimPrefix(target, "file:")
		if path == "" {
			return "", errf("usage", "For example --deliver file:./flyer.jpg", "--deliver file: needs a path")
		}
		if _, err := os.Stat(path); err == nil && !overwrite {
			return "", errf("conflict", "Pass --overwrite to replace it, or pick another path.", "%s already exists", path)
		}
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if err := writeFileAtomic(path, body, 0o644); err != nil {
			return "", err
		}
		return "file:" + path, nil
	}
	return "", errf("usage", "Supported: stdout, file:PATH.", "unknown --deliver target %q", target)
}
