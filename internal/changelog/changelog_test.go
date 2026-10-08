package changelog

import (
	"strings"
	"testing"

	townsquare "github.com/nerveband/townsquare"
)

func TestParseRealChangelog(t *testing.T) {
	es := Parse(townsquare.Changelog)
	if len(es) < 2 || es[0].Version == "" || !strings.HasPrefix(es[0].Version, "v") {
		t.Fatalf("parsed %d entries: %+v", len(es), es)
	}
	for _, e := range es {
		if e.Notes == "" || strings.Contains(e.Notes, "## [") {
			t.Fatalf("bad notes for %s", e.Version)
		}
	}
}

func TestBetween(t *testing.T) {
	es := Parse("# x\n\n## [Unreleased]\n- soon\n\n## [v0.7.0] - 2026-11-01\n- b\n\n## [v0.6.1] - 2026-10-20\n- a\n\n## [v0.6.0] - 2026-10-08\n- z\n")
	if len(es) != 3 || es[0].Date != "2026-11-01" || es[2].Notes != "- z" {
		t.Fatalf("%+v", es)
	}
	newer := func(a, b string) bool { return a > b } // fine for these strings
	got := Between(es, "v0.6.0", "v0.7.0", newer)
	if len(got) != 2 || got[0].Version != "v0.7.0" || got[1].Version != "v0.6.1" {
		t.Fatalf("%+v", got)
	}
}
