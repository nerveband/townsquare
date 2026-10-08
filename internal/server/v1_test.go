package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/update"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Every operation in openapi.json must be routed, so the spec can't drift from the code.
func TestSpecMatchesRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(openapiJSON, &spec); err != nil {
		t.Fatal(err)
	}
	m := (&Server{}).v1Mux()
	param := regexp.MustCompile(`\{[^}]+\}`)
	n := 0
	for path, ops := range spec.Paths {
		for method := range ops {
			url := "/api/v1" + param.ReplaceAllString(path, "1")
			req := httptest.NewRequest(strings.ToUpper(method), url, nil)
			_, pattern := m.Handler(req)
			if pattern == "" || pattern == "/api/v1/" {
				t.Errorf("%s %s is in the spec but not routed", strings.ToUpper(method), path)
			}
			n++
		}
	}
	if n < 40 {
		t.Fatalf("only %d operations checked", n)
	}
}

func TestDeprecatedHeaders(t *testing.T) {
	h := deprecated(func(w http.ResponseWriter, r *http.Request) {}, "Fri, 01 Jan 2027 00:00:00 GMT", "/api/v1/new")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/api/v1/old", nil))
	if rec.Header().Get("Deprecation") != "true" || rec.Header().Get("Sunset") == "" || !strings.Contains(rec.Header().Get("Link"), "successor-version") {
		t.Fatalf("missing deprecation headers: %v", rec.Header())
	}
}

func TestQuietWindowPerClient(t *testing.T) {
	client := int64(8)
	set := map[string]string{"quiet_start": "22:00", "quiet_end": "07:00", "timezone": "America/New_York"}
	clients := map[int64]store.Client{8: {ID: 8, QuietStart: "22:00", QuietEnd: "06:00", Timezone: "America/New_York"}}
	ny, _ := time.LoadLocation("America/New_York")
	six := time.Date(2026, 10, 7, 6, 0, 0, 0, ny)
	if q := quietNow(six, store.Target{ClientID: &client}, nil, clients, set); q != "" {
		t.Fatalf("6:00 ET for the client should send, got quiet %q", q)
	}
	if q := quietNow(six, store.Target{}, nil, clients, set); q == "" {
		t.Fatal("6:00 ET without a client should be quiet (global 22:00-07:00)")
	}
	if q := quietNow(six, store.Target{}, &client, clients, set); q != "" {
		t.Fatal("post's client should apply when the chat has none")
	}
	clients[8] = store.Client{ID: 8, QuietStart: "00:00", QuietEnd: "00:00"}
	if q := quietNow(time.Date(2026, 10, 7, 3, 0, 0, 0, ny), store.Target{ClientID: &client}, nil, clients, set); q != "" {
		t.Fatal("equal start and end means no quiet hours")
	}
	if q := quietNow(time.Date(2026, 10, 7, 23, 30, 0, 0, ny), store.Target{Kind: "self"}, nil, clients, set); q != "" {
		t.Fatalf("your own chat has no quiet hours, got %q", q)
	}
}

func TestStatsSummaryEmptyAndFiltered(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db}
	for _, q := range []string{"", "?days=7&platform=telegram", "?days=90&tag=0&client=1,2&chat=x@g.us"} {
		rec := httptest.NewRecorder()
		s.statsSummary(rec, httptest.NewRequest("GET", "/api/v1/stats/summary"+q, nil))
		var out map[string]any
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["tiles"] == nil {
			t.Fatalf("%s: %d %s", q, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	s.statsText(rec, httptest.NewRequest("GET", "/api/v1/stats/summary.txt?days=7", nil))
	if !strings.Contains(rec.Body.String(), "last 7 days") {
		t.Fatalf("text: %s", rec.Body.String())
	}
}

func TestUpdateEndpoints(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := &Server{DB: db, DataDir: dir, Updater: update.New(dir, "v0.5.0-23-gabc1234")}
	rec := httptest.NewRecorder()
	s.updateState(rec, httptest.NewRequest("GET", "/api/v1/update", nil))
	var st map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &st) != nil || st["dev"] != true || st["auto"] != true {
		t.Fatalf("state: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.updateInstall(rec, httptest.NewRequest("POST", "/api/v1/update/install", strings.NewReader(`{}`)))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "built_from_source") {
		t.Fatalf("dev install: %d %s", rec.Code, rec.Body.String())
	}
	if b := s.busySoon(t.Context()); b != "" {
		t.Fatalf("empty calendar busy: %q", b)
	}
	for _, c := range []struct {
		method, path string
		admin        bool
	}{
		{"GET", "/api/v1/update", false}, {"POST", "/api/v1/update/check", false}, {"POST", "/api/v1/update/install", true},
		{"GET", "/api/v1/autostart", false}, {"PUT", "/api/v1/autostart", true}, {"POST", "/api/v1/quit", true},
		{"GET", "/api/v1/whatsapp", false}, {"POST", "/api/v1/whatsapp/link", true}, {"GET", "/api/v1/whatsapp/qr.png", true},
		{"POST", "/api/v1/telegram/app", true},
	} {
		if got := needsAdmin(httptest.NewRequest(c.method, c.path, nil)); got != c.admin {
			t.Errorf("needsAdmin(%s %s) = %v", c.method, c.path, got)
		}
	}
}

func TestChangelogEndpoint(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.changelogHandler(rec, httptest.NewRequest("GET", "/api/v1/changelog?limit=1", nil))
	var es []map[string]string
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &es) != nil || len(es) != 1 || es[0]["version"] == "" || es[0]["notes"] == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.changelogHandler(rec, httptest.NewRequest("GET", "/api/v1/changelog?from=v99.0.0", nil))
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("future from: %s", rec.Body.String())
	}
}
