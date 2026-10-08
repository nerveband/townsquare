// Package changelog reads CHANGELOG.md ("## [v1.2.3] - 2026-10-08" sections) so
// the app and the update manifest show the same notes as the GitHub release.
package changelog

import (
	"regexp"
	"strings"
)

// Entry is one released version's notes (Markdown, without the heading).
type Entry struct {
	Version string `json:"version"`
	Date    string `json:"date,omitempty"`
	Notes   string `json:"notes"`
}

var head = regexp.MustCompile(`^## \[(v[^\]]+)\](?:\s*-\s*(\S+))?`)

// Parse returns released versions, newest first. "Unreleased" is skipped.
func Parse(md string) []Entry {
	var out []Entry
	var cur *Entry
	var body []string
	flush := func() {
		if cur != nil {
			cur.Notes = strings.TrimSpace(strings.Join(body, "\n"))
			out = append(out, *cur)
		}
		cur, body = nil, nil
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			if m := head.FindStringSubmatch(line); m != nil {
				cur = &Entry{Version: m[1], Date: m[2]}
			}
			continue
		}
		if cur != nil {
			body = append(body, line)
		}
	}
	flush()
	return out
}

// Between returns entries newer than from and no newer than to (newest first).
// newer reports whether version a is newer than b.
func Between(es []Entry, from, to string, newer func(a, b string) bool) []Entry {
	var out []Entry
	for _, e := range es {
		if (from == "" || newer(e.Version, from)) && (to == "" || !newer(e.Version, to)) {
			out = append(out, e)
		}
	}
	return out
}
