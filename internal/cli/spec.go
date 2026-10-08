// Package cli is the agent-friendly `townsquare` command line. Every remote
// command comes from the API contract (internal/contract/openapi.json, the
// x-cli entries written in tools/gen_openapi.py), so the CLI, the REST API, the
// `schema` output and docs/cli.md can't drift apart. Local commands (serve,
// pair, update, ...) are declared in local.go.
package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nerveband/townsquare/internal/contract"
)

// Param is a flag (query parameter or request body field) or the positional id.
type Param struct {
	Name     string   `json:"name"`            // API name, e.g. quiet_start
	Flag     string   `json:"flag"`            // --quiet-start
	Type     string   `json:"type"`            // string, integer, number, boolean, array, object
	Items    string   `json:"items,omitempty"` // element type for arrays
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
	Desc     string   `json:"description,omitempty"`
	In       string   `json:"in"` // path, query, body
	Secret   bool     `json:"secret,omitempty"`
}

// Command is one remote command, built from an API operation.
type Command struct {
	Name        string
	Words       []string
	Method      string
	Path        string
	Summary     string
	Description string
	Tag         string
	Effects     string
	OutputKind  string
	MediaType   string
	Cardinality string
	Confirm     bool
	Paginated   bool
	Examples    []string
	Positional  *Param
	Query       []Param
	Body        []Param
	HasBody     bool
	Multipart   bool
	BodySchema  map[string]any
	RespSchema  map[string]any
	Errors      []string
	// ServerDryRun: the API takes "dry_run", so --dry-run is checked by the server.
	ServerDryRun bool
}

type specDoc struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]any `json:"schemas"`
	} `json:"components"`
}

var (
	loadOnce sync.Once
	cmds     []*Command
	schemas  map[string]any
	loadErr  error
)

// Commands returns every remote command, sorted by name.
func Commands() ([]*Command, error) {
	loadOnce.Do(func() { cmds, loadErr = load(contract.OpenAPI()) })
	return cmds, loadErr
}

func load(raw []byte) ([]*Command, error) {
	var d specDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("openapi.json: %w", err)
	}
	schemas = d.Components.Schemas
	var out []*Command
	for path, ops := range d.Paths {
		for method, rawOp := range ops {
			var op struct {
				Summary     string           `json:"summary"`
				Description string           `json:"description"`
				Tags        []string         `json:"tags"`
				Parameters  []map[string]any `json:"parameters"`
				RequestBody struct {
					Content map[string]struct {
						Schema map[string]any `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
				Responses map[string]struct {
					Content map[string]struct {
						Schema map[string]any `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
				X struct {
					Name        string            `json:"name"`
					Effects     string            `json:"effects"`
					Examples    []string          `json:"examples"`
					OutputKind  string            `json:"output_kind"`
					MediaType   string            `json:"media_type"`
					Cardinality string            `json:"cardinality"`
					Confirm     bool              `json:"confirm"`
					Paginated   bool              `json:"paginated"`
					Flags       map[string]string `json:"flags"`
				} `json:"x-cli"`
			}
			if err := json.Unmarshal(rawOp, &op); err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, path, err)
			}
			if op.X.Name == "" {
				return nil, fmt.Errorf("%s %s has no x-cli entry", strings.ToUpper(method), path)
			}
			c := &Command{Name: op.X.Name, Words: strings.Fields(op.X.Name), Method: strings.ToUpper(method), Path: path,
				Summary: op.Summary, Description: op.Description, Effects: op.X.Effects, OutputKind: op.X.OutputKind,
				MediaType: op.X.MediaType, Cardinality: op.X.Cardinality, Confirm: op.X.Confirm, Paginated: op.X.Paginated,
				Examples: op.X.Examples}
			if len(op.Tags) > 0 {
				c.Tag = op.Tags[0]
			}
			for _, p := range op.Parameters {
				sch, _ := p["schema"].(map[string]any)
				prm := paramFrom(str(p["name"]), sch, str(p["in"]), p["required"] == true)
				prm.Desc = str(p["description"])
				if f, ok := op.X.Flags[prm.Name]; ok {
					prm.Flag = f
				}
				if prm.In == "path" {
					c.Positional = &prm
				} else {
					c.Query = append(c.Query, prm)
				}
			}
			for ct, body := range op.RequestBody.Content {
				c.HasBody = true
				if strings.HasPrefix(ct, "multipart/") {
					c.Multipart = true
					continue
				}
				c.BodySchema = resolve(body.Schema, 0)
				req := map[string]bool{}
				for _, r := range anySlice(c.BodySchema["required"]) {
					req[str(r)] = true
				}
				props, _ := c.BodySchema["properties"].(map[string]any)
				names := make([]string, 0, len(props))
				for n := range props {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					if n == "dry_run" {
						c.ServerDryRun = true // the global --dry-run asks the server
						continue
					}
					ps, _ := props[n].(map[string]any)
					prm := paramFrom(n, ps, "body", req[n])
					prm.Desc = str(ps["description"])
					if f, ok := op.X.Flags[n]; ok {
						prm.Flag = f
					}
					c.Body = append(c.Body, prm)
				}
			}
			if ok, has := op.Responses["200"]; has {
				for _, v := range ok.Content {
					c.RespSchema = resolve(v.Schema, 0)
				}
			}
			for code := range op.Responses {
				if k := kindForStatus(code); k != "" && !contains(c.Errors, k) {
					c.Errors = append(c.Errors, k)
				}
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var secretNames = map[string]bool{"token": true, "password": true, "api_hash": true}

func paramFrom(name string, sch map[string]any, in string, required bool) Param {
	sch = resolve(sch, 0)
	p := Param{Name: name, Flag: "--" + strings.ReplaceAll(name, "_", "-"), In: in, Required: required, Secret: secretNames[name]}
	p.Type = typeOf(sch)
	if p.Type == "array" {
		items, _ := sch["items"].(map[string]any)
		items = resolve(items, 0)
		p.Items = typeOf(items)
		for _, e := range anySlice(items["enum"]) {
			p.Enum = append(p.Enum, fmt.Sprint(e))
		}
	}
	for _, e := range anySlice(sch["enum"]) {
		p.Enum = append(p.Enum, fmt.Sprint(e))
	}
	return p
}

func typeOf(sch map[string]any) string {
	if sch == nil {
		return "string"
	}
	switch t := sch["type"].(type) {
	case string:
		return t
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && s != "null" {
				return s
			}
		}
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		for _, alt := range anySlice(sch[k]) {
			if m, ok := alt.(map[string]any); ok {
				if t := typeOf(resolve(m, 0)); t != "null" && t != "" {
					return t
				}
			}
		}
	}
	if _, ok := sch["properties"]; ok {
		return "object"
	}
	return "string"
}

// resolve inlines $ref (bounded depth, so recursive schemas stay finite).
func resolve(s map[string]any, depth int) map[string]any {
	if s == nil {
		return nil
	}
	if ref, ok := s["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if depth > 6 {
			return map[string]any{"type": "object"}
		}
		m, _ := schemas[name].(map[string]any)
		return resolve(m, depth+1)
	}
	out := map[string]any{}
	for k, v := range s {
		switch vv := v.(type) {
		case map[string]any:
			out[k] = resolve(vv, depth+1)
		case []any:
			arr := make([]any, len(vv))
			for i, x := range vv {
				if m, ok := x.(map[string]any); ok {
					arr[i] = resolve(m, depth+1)
				} else {
					arr[i] = x
				}
			}
			out[k] = arr
		default:
			out[k] = v
		}
	}
	return out
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func anySlice(v any) []any {
	a, _ := v.([]any)
	return a
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
