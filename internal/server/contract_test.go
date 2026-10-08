package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/nerveband/townsquare/internal/contract"
	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/wa"
)

// The contract (internal/contract/openapi.json) is the single source of truth.
// This test calls every read operation on a demo database and checks the JSON
// each handler returns against the response schema the contract declares, so a
// handler can't change shape without the contract (and with it the CLI, the
// docs and the agent guide) changing too.
func TestResponsesMatchContract(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cli, err := wa.Open(ctx, dir, "ERROR")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cli.Disconnect)
	s := &Server{DB: db, WA: cli, DataDir: dir, Demo: true}
	if err := s.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	// A document needs no ffmpeg, so this works on every CI machine.
	doc, err := s.ingestMedia(ctx, "agenda.pdf", "", "document", strings.NewReader(demoPDF))
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := db.CreateAPIKey(ctx, "contract-test", "admin")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(contract.OpenAPI(), &spec); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"{id}": "2", "{jid}": "120363000000000102@g.us"}
	query := map[string]string{"/sends/deliveries": "schedule_id=2&occ=x", "/sends": "from=2026-01-01&to=2030-01-01"}
	skip := map[string]string{"/media/{id}/file": "binary", "/media/{id}/preview": "binary", "/telegram/qr.png": "binary",
		"/whatsapp/qr.png": "binary", "/stats/summary.txt": "text", "/stats/export.csv": "text"}
	m := s.v1Mux()
	checked := 0
	var paths []string
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		raw, ok := spec.Paths[path]["get"]
		if !ok || skip[path] != "" {
			continue
		}
		var op struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		}
		_ = json.Unmarshal(raw, &op)
		sch := op.Responses["200"].Content["application/json"].Schema
		if sch == nil {
			t.Errorf("GET %s declares no JSON response", path)
			continue
		}
		url := "/api/v1" + path
		if strings.HasPrefix(path, "/media/") {
			url = strings.ReplaceAll(url, "{id}", fmt.Sprint(doc.ID))
		}
		for k, v := range ids {
			url = strings.ReplaceAll(url, k, v)
		}
		if q := query[path]; q != "" {
			url += "?" + q
		}
		req := httptest.NewRequest("GET", url, nil).WithContext(context.WithValue(ctx, keyCtx, key))
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("GET %s: status %d: %s", url, rec.Code, rec.Body.String())
			continue
		}
		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("GET %s: not JSON: %v", url, err)
			continue
		}
		v := validator{schemas: spec.Components.Schemas}
		v.check(sch, body, "GET "+path)
		for _, e := range v.errs {
			t.Error(e)
		}
		checked++
	}
	if checked < 25 {
		t.Fatalf("only %d read operations checked", checked)
	}

	// Writes: the shapes agents rely on (ids, change numbers) must match too.
	writes := []struct{ method, path, body string }{
		{"POST", "/tags", `{"name":"Contract","color":"#123456"}`},
		{"PATCH", "/settings", `{"gap_min":"25"}`},
		{"POST", "/posts", `{"title":"Contract","caption":"x","targets":["Main Group"]}`},
		{"PATCH", "/posts/{id}", `{"caption":"y"}`},
		{"POST", "/posts/{id}/pause", ``},
		{"POST", "/posts/{id}/resume", ``},
		{"POST", "/posts/preview", `{"caption":"x","targets":["Main Group"],"send_at":"2030-01-01T09:00"}`},
		{"POST", "/undo", `{}`},
		{"POST", "/posts/{id}/pause", ``},
		{"POST", "/posts/bulk", `{"ids":[2,99999],"action":"resume"}`},
	}
	for _, w := range writes {
		raw := spec.Paths[w.path][strings.ToLower(w.method)]
		var op struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		}
		_ = json.Unmarshal(raw, &op)
		sch := op.Responses["200"].Content["application/json"].Schema
		url := "/api/v1" + strings.ReplaceAll(w.path, "{id}", "2")
		req := httptest.NewRequest(w.method, url, strings.NewReader(w.body)).WithContext(context.WithValue(ctx, keyCtx, key))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("%s %s: status %d: %s", w.method, url, rec.Code, rec.Body.String())
			continue
		}
		var body any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		v := validator{schemas: spec.Components.Schemas}
		v.check(sch, body, w.method+" "+w.path)
		for _, e := range v.errs {
			t.Error(e)
		}
	}
}

// validator is a small JSON Schema checker for the subset the contract uses:
// $ref, type (incl. lists and null), properties, items, oneOf, enum.
type validator struct {
	schemas map[string]any
	errs    []string
}

func (v *validator) check(sch map[string]any, val any, at string) {
	if sch == nil || len(v.errs) > 20 {
		return
	}
	if ref, ok := sch["$ref"].(string); ok {
		next, _ := v.schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		v.check(next, val, at)
		return
	}
	if alts, ok := sch["oneOf"].([]any); ok {
		for _, a := range alts {
			sub := validator{schemas: v.schemas}
			sub.check(a.(map[string]any), val, at)
			if len(sub.errs) == 0 {
				return
			}
		}
		v.errs = append(v.errs, fmt.Sprintf("%s: %s matches none of the oneOf alternatives", at, short(val)))
		return
	}
	if !typeOK(sch["type"], val) {
		v.errs = append(v.errs, fmt.Sprintf("%s: got %s, contract says %v", at, short(val), sch["type"]))
		return
	}
	if enum, ok := sch["enum"].([]any); ok && val != nil {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(val) {
				found = true
			}
		}
		if !found {
			v.errs = append(v.errs, fmt.Sprintf("%s: %v isn't one of %v", at, val, enum))
		}
	}
	switch x := val.(type) {
	case map[string]any:
		props, _ := sch["properties"].(map[string]any)
		for k, pv := range x {
			ps, ok := props[k].(map[string]any)
			if !ok {
				if len(props) > 0 && sch["additionalProperties"] == nil {
					v.errs = append(v.errs, fmt.Sprintf("%s: field %q isn't in the contract", at, k))
				}
				if ap, ok := sch["additionalProperties"].(map[string]any); ok {
					v.check(ap, pv, at+"."+k)
				}
				continue
			}
			v.check(ps, pv, at+"."+k)
		}
		for _, r := range anyList(sch["required"]) {
			if _, ok := x[fmt.Sprint(r)]; !ok {
				v.errs = append(v.errs, fmt.Sprintf("%s: required field %v is missing", at, r))
			}
		}
	case []any:
		items, _ := sch["items"].(map[string]any)
		for i, it := range x {
			if i > 50 {
				break
			}
			v.check(items, it, fmt.Sprintf("%s[%d]", at, i))
		}
	}
}

func anyList(v any) []any { a, _ := v.([]any); return a }

func typeOK(t any, val any) bool {
	switch tt := t.(type) {
	case nil:
		return true
	case string:
		return typeIs(tt, val)
	case []any:
		for _, x := range tt {
			if typeIs(fmt.Sprint(x), val) {
				return true
			}
		}
		return false
	}
	return true
}

func typeIs(t string, val any) bool {
	switch t {
	case "null":
		return val == nil
	case "string":
		_, ok := val.(string)
		return ok
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "integer":
		f, ok := val.(float64)
		return ok && f == float64(int64(f))
	case "number":
		_, ok := val.(float64)
		return ok
	case "array":
		_, ok := val.([]any)
		return ok || val == nil // Go encodes empty slices as null in places; the CLI treats both as empty
	case "object":
		_, ok := val.(map[string]any)
		return ok || val == nil
	}
	return true
}

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 80 {
		return string(b[:77]) + "..."
	}
	return string(b)
}
