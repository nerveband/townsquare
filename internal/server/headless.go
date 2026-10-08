package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/appconfig"
	"github.com/nerveband/townsquare/internal/tgbot"
	"go.mau.fi/whatsmeow"
)

// The rest of what used to need a terminal or a file edit, so an agent can set
// up and run Townsquare entirely through the API: server address and tailnet
// name, restarting, the Telegram bot token, sign-in links for people, and
// creating WhatsApp channels.

// serverConfig shows the saved server settings and the ones running now.
func (s *Server) serverConfig(w http.ResponseWriter, r *http.Request) {
	c, err := appconfig.Load(s.DataDir)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{
		"listen": c.ListenAddr(), "tailscale": c.Tailscale,
		"running":        map[string]string{"listen": s.Listen, "tailscale": s.Tailnet},
		"restart_needed": c.ListenAddr() != s.Listen || c.Tailscale != s.Tailnet,
	})
}

// patchServerConfig saves {"listen": "HOST:PORT", "tailscale": "NAME"} (either
// may be left out; "" turns the tailnet off). It applies after a restart.
func (s *Server) patchServerConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Listen    *string `json:"listen"`
		Tailscale *string `json:"tailscale"`
	}
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_request", err)
		return
	}
	c, err := appconfig.Load(s.DataDir)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if in.Listen != nil {
		c.Listen = *in.Listen
	}
	if in.Tailscale != nil {
		c.Tailscale = *in.Tailscale
	}
	if err := appconfig.Save(s.DataDir, c); err != nil {
		failCode(w, 422, "invalid", err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "changed the server address settings")
	s.serverConfig(w, r)
}

// restartServer restarts Townsquare (to apply server settings or a new
// Telegram bot token). Like updates, it waits for a moment with no post due
// within 15 minutes unless {"force": true}.
func (s *Server) restartServer(w http.ResponseWriter, r *http.Request) {
	var in struct{ Force bool }
	_ = readJSON(r, &in)
	if busy := s.busySoon(r.Context()); busy != "" && !in.Force {
		failCode(w, 409, "busy", errors.New(busy+"; try again after it goes out, or pass force"))
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "restarted Townsquare")
	writeJSON(w, map[string]any{"ok": true, "restarting": true})
	go func() { time.Sleep(500 * time.Millisecond); s.requestRestart("reload") }()
}

var botToken = regexp.MustCompile(`^\d{5,15}:[A-Za-z0-9_-]{30,}$`)

// setBotToken saves the Telegram bot token from @BotFather and starts the bot.
func (s *Server) setBotToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if err := readJSON(r, &in); err != nil || !botToken.MatchString(strings.TrimSpace(in.Token)) {
		failCode(w, 400, "bad_request", errors.New(`send {"token": "123456:ABC..."} from @BotFather`))
		return
	}
	if s.Demo {
		failCode(w, 409, "unavailable", errors.New("not in demo mode"))
		return
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "telegram.token"), []byte(strings.TrimSpace(in.Token)+"\n"), 0o600); err != nil {
		fail(w, 500, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "set the Telegram bot token")
	if s.Bot != nil { // the running bot keeps its token until a restart
		writeJSON(w, map[string]any{"ok": true, "restart_needed": true})
		return
	}
	b, err := tgbot.New(s.DataDir, s.BotChat)
	if err != nil || b == nil {
		fail(w, 500, errors.Join(errors.New("couldn't start the bot"), err))
		return
	}
	s.Bot = b
	base := s.baseCtx
	if base == nil {
		base = context.Background()
	}
	go s.RunBot(base)
	writeJSON(w, map[string]any{"ok": true, "restart_needed": false})
}

// removeBotToken deletes the bot token; the bot stops at the next restart.
func (s *Server) removeBotToken(w http.ResponseWriter, r *http.Request) {
	err := os.Remove(filepath.Join(s.DataDir, "telegram.token"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fail(w, 500, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "removed the Telegram bot token")
	writeJSON(w, map[string]any{"ok": true, "restart_needed": s.Bot != nil})
}

// loginLinkHandler makes a one-time sign-in link for a person (works once, 15 minutes).
func (s *Server) loginLinkHandler(w http.ResponseWriter, r *http.Request) {
	var in struct{ Base string }
	_ = readJSON(r, &in)
	base := strings.TrimSuffix(in.Base, "/")
	if base == "" {
		base = baseURL(r)
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		failCode(w, 400, "bad_request", errors.New("base must start with http:// or https://"))
		return
	}
	link, err := LoginLink(r.Context(), s.DB, base)
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "made a sign-in link")
	writeJSON(w, map[string]any{"link": link, "expires_at": time.Now().Add(15 * time.Minute).Unix()})
}

// createChannel creates a WhatsApp channel you own (it starts off the allowlist).
func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		failCode(w, 400, "bad_request", errors.New(`send {"name": "...", "description": "..."}`))
		return
	}
	if s.Demo || !s.isConnected() {
		failCode(w, 503, "unavailable", errors.New("WhatsApp isn't connected"))
		return
	}
	meta, err := s.WA.CreateNewsletter(r.Context(), whatsmeow.CreateNewsletterParams{Name: strings.TrimSpace(in.Name), Description: in.Description})
	if err != nil {
		fail(w, 502, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "created the WhatsApp channel "+meta.ThreadMeta.Name.Text)
	go func() { _ = s.syncTargets(context.Background()) }()
	writeJSON(w, map[string]any{"jid": meta.ID.String(), "name": meta.ThreadMeta.Name.Text})
}
