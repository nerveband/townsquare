package server

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/wa"
)

// Web app sign-in. The browser UI uses /api/... with a session cookie; agents use
// /api/v1 with API keys. A sign-in link is sent to the owner's own WhatsApp chat
// ("Message yourself"), or printed by `townsquare login-link` on the server.

const cookieName = "townsquare_session"

const sessionCtx ctxKey = 2

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		open := s.Demo || !strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/api/v1/") || p == "/api/v1" || strings.HasPrefix(p, "/api/auth/")
		if open {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(cookieName)
		if err == nil {
			if id, err := s.DB.LookupSession(r.Context(), c.Value); err == nil {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionCtx, id)))
				return
			}
		}
		failCode(w, 401, "signin_required", errors.New("sign in to use the web app (agents: use /api/v1 with an API key)"))
	})
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	signed := s.Demo
	if c, err := r.Cookie(cookieName); err == nil {
		if _, err := s.DB.LookupSession(r.Context(), c.Value); err == nil {
			signed = true
		}
	}
	writeJSON(w, map[string]any{"signed_in": signed, "demo": s.Demo, "can_send_link": s.isConnected() || s.TG.Ready(),
		"whatsapp": s.isConnected(), "telegram": s.TG.Ready()})
}

// authRequest sends a one-time sign-in link to the owner's own chat: WhatsApp
// "Message yourself" or Telegram Saved Messages ({"via": "whatsapp"|"telegram"}).
func (s *Server) authRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct{ Via string }
	_ = readJSON(r, &in)
	if in.Via == "" {
		in.Via = "whatsapp"
		if !s.isConnected() && s.TG.Ready() {
			in.Via = "telegram"
		}
	}
	if wait := time.Until(s.DB.LastLoginToken(ctx).Add(30 * time.Second)); wait > 0 {
		failCode(w, 429, "too_soon", fmt.Errorf("a link was just sent; try again in %d seconds", int(wait.Seconds())+1))
		return
	}
	if in.Via == "telegram" && !s.TG.Ready() {
		failCode(w, 503, "unavailable", errors.New("Telegram isn't logged in, so a link can't be sent there. Try WhatsApp, or on the computer running Townsquare run: townsquare login-link"))
		return
	}
	if in.Via != "telegram" && (!s.isConnected() || s.WA.Store.ID == nil) {
		failCode(w, 503, "unavailable", errors.New("WhatsApp isn't connected, so a link can't be sent. On the computer running Townsquare, run: townsquare login-link"))
		return
	}
	tok, err := s.DB.NewLoginToken(ctx, 15*time.Minute)
	if err != nil {
		fail(w, 500, err)
		return
	}
	link := baseURL(r) + "/auth/login?t=" + tok
	msg := "Sign in to Townsquare:\n" + link + "\n\nThis link works once and expires in 15 minutes. If you didn't ask for it, ignore this message."
	where := "Message yourself"
	if in.Via == "telegram" {
		where = "Telegram Saved Messages"
		if _, err := s.TG.Send(ctx, "tg:self", msg, nil, tg.Options{NoPreview: true}); err != nil {
			fail(w, 500, err)
			return
		}
	} else if _, err := wa.SendPrepared(ctx, s.WA, s.WA.Store.ID.ToNonAD().String(), nil, msg); err != nil {
		fail(w, 500, err)
		return
	}
	s.DB.Log(ctx, "you", "requested a sign-in link (sent to "+where+")")
	writeJSON(w, map[string]any{"ok": true, "sent_to": where})
}

// authLogin redeems a link and sets the session cookie.
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	sec, err := s.DB.UseLoginToken(r.Context(), r.URL.Query().Get("t"), r.UserAgent())
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(400)
		fmt.Fprintf(w, `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><body style="font:16px system-ui;padding:40px;max-width:420px;margin:auto"><h2>Couldn't sign in</h2><p>%s</p><p><a href="/">Back to Townsquare</a></p>`, html.EscapeString(err.Error()))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sec, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil, MaxAge: 400 * 24 * 3600})
	s.DB.Log(r.Context(), "you", "signed in on a new device")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if id, err := s.DB.LookupSession(r.Context(), c.Value); err == nil {
			_ = s.DB.RevokeSession(r.Context(), id)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	cur, _ := r.Context().Value(sessionCtx).(int64)
	out := s.DB.Sessions(r.Context())
	for i := range out {
		out[i].Current = out[i].ID == cur
	}
	writeJSON(w, out)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := s.DB.RevokeSession(r.Context(), id); err != nil {
		fail(w, 404, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), fmt.Sprintf("signed out device #%d", id))
	writeJSON(w, map[string]bool{"ok": true})
}

// LoginLink makes a sign-in link for the CLI (`townsquare login-link`).
func LoginLink(ctx context.Context, db *store.DB, base string) (string, error) {
	tok, err := db.NewLoginToken(ctx, 15*time.Minute)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(base, "/") + "/auth/login?t=" + tok, nil
}
