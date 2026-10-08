package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nerveband/townsquare/internal/store"
)

type ctxKey int

const keyCtx ctxKey = 1

// actorOf names who made a change: "you" in the web app, "api:NAME" for API keys.
func actorOf(r *http.Request) string {
	if k, ok := r.Context().Value(keyCtx).(*store.APIKey); ok {
		return "api:" + k.Name
	}
	return "you"
}

func keyOf(r *http.Request) *store.APIKey {
	k, _ := r.Context().Value(keyCtx).(*store.APIKey)
	return k
}

// requireKey authenticates /api/v1 with "Authorization: Bearer tsq_..." and
// enforces scopes: read for GET, write for changes, admin for safety and keys.
func (s *Server) requireKey(next http.Handler) http.Handler {
	public := map[string]bool{"/api/v1": true, "/api/v1/": true, "/api/v1/openapi.json": true, "/api/v1/guide.md": true, "/api/v1/docs": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if public[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
		if tok == "" {
			tok = r.Header.Get("X-API-Key")
		}
		if tok == "" {
			failCode(w, 401, "unauthorized", errors.New("send Authorization: Bearer <api key>. Create keys in Settings → API keys or with `townsquare apikey create`"))
			return
		}
		k, err := s.DB.LookupAPIKey(r.Context(), tok)
		if err != nil {
			failCode(w, 401, "unauthorized", errors.New("unknown or revoked API key"))
			return
		}
		need := "read"
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			need = "write"
		}
		if needsAdmin(r) {
			need = "admin"
		}
		if store.Scopes[k.Scope] < store.Scopes[need] {
			failCode(w, 403, "forbidden", errors.New("this key has "+k.Scope+" scope; this request needs "+need))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyCtx, k)))
	})
}

// needsAdmin marks requests that can weaken safety: safe mode, the allowlist and API keys.
func needsAdmin(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/keys") || strings.HasPrefix(p, "/api/v1/sessions") {
		return true
	}
	// Updating, stopping, start at login, and linking or unlinking WhatsApp are admin-only.
	if p == "/api/v1/update/install" || p == "/api/v1/quit" || p == "/api/v1/restart" || p == "/api/v1/login-link" ||
		(p == "/api/v1/config" && r.Method != http.MethodGet) || (p == "/api/v1/autostart" && r.Method != http.MethodGet) ||
		strings.HasPrefix(p, "/api/v1/whatsapp/") ||
		(strings.HasPrefix(p, "/api/v1/accounts") && (r.Method != http.MethodGet || strings.HasSuffix(p, "/qr.png"))) {
		return true
	}
	// Logging an account in or out, or seeing its login QR, is admin-only.
	if strings.HasPrefix(p, "/api/v1/telegram/") && p != "/api/v1/telegram/refresh" {
		return true
	}
	if r.Method == http.MethodPatch && (p == "/api/v1/settings" || strings.HasPrefix(p, "/api/v1/targets/")) {
		body := peekBody(r)
		return strings.Contains(body, `"safe_mode"`) || strings.Contains(body, `"allowed"`)
	}
	return false
}
