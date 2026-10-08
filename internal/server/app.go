package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/autostart"
	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/update"
	"github.com/nerveband/townsquare/internal/wa"
	qrcode "github.com/skip2/go-qrcode"
)

// Setup and upkeep that used to need a terminal: linking WhatsApp, adding the
// Telegram app id, updates, starting at login and quitting.

// requestRestart asks main to shut down cleanly and then either start the
// downloaded update ("update") or stop ("quit").
func (s *Server) requestRestart(why string) {
	if s.Restart == nil {
		return
	}
	select {
	case s.Restart <- why:
	default:
	}
}

// busySoon reports a post due within 15 minutes either side of now (or one
// being sent), when restarting would be a bad idea.
func (s *Server) busySoon(ctx context.Context) string {
	if s.sendingNow() != "" {
		return "a post is being sent right now"
	}
	posts, err := s.DB.Posts(ctx, "scheduled")
	if err != nil {
		return ""
	}
	now := time.Now()
	if due := store.Expand(posts, now.Add(-15*time.Minute), now.Add(15*time.Minute)); len(due) > 0 {
		return "a post is due within 15 minutes"
	}
	return ""
}

func (s *Server) sendingNow() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sending
}

// RunUpdates checks for a new release every 6 hours. With automatic updates on
// (the default) it downloads the release and restarts into it when no post is
// due within 15 minutes. Builds from source only report updates.
func (s *Server) RunUpdates(ctx context.Context) {
	if s.Updater == nil || s.Demo {
		return
	}
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	first := time.After(time.Minute)
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
		case <-t.C:
		}
		auto := s.DB.Setting(ctx, "auto_update") != "0"
		dev := s.Updater.State().Dev
		if time.Since(last) >= 6*time.Hour {
			last = time.Now()
			var err error
			if auto && !dev {
				_, err = s.Updater.Update(ctx)
			} else {
				_, err = s.Updater.Check(ctx)
			}
			if err != nil {
				log.Println("update:", err)
			}
		}
		if !auto || dev {
			continue
		}
		if st, ok := update.ReadStaged(s.DataDir); ok && update.Newer(st.Version, s.Updater.Current) && s.busySoon(ctx) == "" {
			if _, ok := update.StagedPath(s.DataDir, s.Updater.Current); ok {
				s.DB.Log(ctx, "townsquare", "updated itself to "+st.Version)
				log.Println("update: restarting into", st.Version)
				s.requestRestart("update")
				return
			}
		}
	}
}

func (s *Server) updateState(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeJSON(w, map[string]any{"current": "demo", "dev": true, "auto": false})
		return
	}
	st := s.Updater.State()
	writeJSON(w, map[string]any{"current": st.Current, "latest": st.Latest, "notes": st.Notes, "available": st.Available,
		"staged": st.Staged, "checked_at": st.CheckedAt, "error": st.Error, "platform": st.Platform, "dev": st.Dev,
		"auto": s.DB.Setting(r.Context(), "auto_update") != "0"})
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		failCode(w, 409, "unavailable", errors.New("updates are off in demo mode"))
		return
	}
	if _, err := s.Updater.Check(r.Context()); err != nil {
		failCode(w, 502, "unavailable", err)
		return
	}
	s.updateState(w, r)
}

// updateInstall downloads the newest release and restarts into it. Pass
// {"force": true} to restart even when a post is due within 15 minutes.
func (s *Server) updateInstall(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		failCode(w, 409, "unavailable", errors.New("updates are off in demo mode"))
		return
	}
	if s.Updater.State().Dev {
		failCode(w, 409, "built_from_source", errors.New("this copy was built from source; update it with git pull and a rebuild"))
		return
	}
	var in struct{ Force bool }
	_ = readJSON(r, &in)
	if busy := s.busySoon(r.Context()); busy != "" && !in.Force {
		failCode(w, 409, "busy", errors.New(busy+"; try again after it goes out"))
		return
	}
	v, err := s.Updater.Update(r.Context())
	if err != nil {
		failCode(w, 502, "unavailable", err)
		return
	}
	if v == "" {
		if st, ok := update.ReadStaged(s.DataDir); ok && update.Newer(st.Version, s.Updater.Current) {
			v = st.Version
		}
	}
	if v == "" {
		writeJSON(w, map[string]any{"ok": true, "up_to_date": true, "current": s.Updater.Current})
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "updated Townsquare to "+v)
	writeJSON(w, map[string]any{"ok": true, "restarting": true, "version": v})
	go func() { time.Sleep(500 * time.Millisecond); s.requestRestart("update") }()
}

func (s *Server) autostartState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"supported": autostart.Supported() && !s.Demo, "enabled": autostart.Enabled(), "app_mode": s.AppMode})
}

func (s *Server) setAutostart(w http.ResponseWriter, r *http.Request) {
	var in struct{ Enabled *bool }
	if err := readJSON(r, &in); err != nil || in.Enabled == nil {
		failCode(w, 400, "bad_request", errors.New(`send {"enabled": true} or {"enabled": false}`))
		return
	}
	if s.Demo {
		failCode(w, 409, "unavailable", errors.New("not in demo mode"))
		return
	}
	var err error
	if *in.Enabled {
		err = autostart.Enable(s.DataDir, nz(s.Listen, "127.0.0.1:8890"))
	} else {
		err = autostart.Disable()
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), map[bool]string{true: "turned on start at login", false: "turned off start at login"}[*in.Enabled])
	s.autostartState(w, r)
}

func (s *Server) quit(w http.ResponseWriter, r *http.Request) {
	s.DB.Log(r.Context(), actorOf(r), "stopped Townsquare")
	writeJSON(w, map[string]any{"ok": true})
	go func() { time.Sleep(300 * time.Millisecond); s.requestRestart("quit") }()
}

// WhatsApp linking from the browser.

func (s *Server) whatsappState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"linked": false, "connected": s.isConnected()}
	if s.WA != nil && s.WA.Store.ID != nil {
		out["linked"] = true
		out["number"] = s.WA.Store.ID.User
	}
	s.mu.Lock()
	l := s.linker
	s.mu.Unlock()
	if l != nil {
		out["link"] = l.State()
	}
	writeJSON(w, out)
}

func (s *Server) whatsappLink(w http.ResponseWriter, r *http.Request) {
	if s.Demo || s.WA == nil {
		failCode(w, 409, "unavailable", errors.New("not in demo mode"))
		return
	}
	if s.WA.Store.ID != nil {
		failCode(w, 409, "conflict", errors.New("WhatsApp is already linked"))
		return
	}
	var in struct{ Phone string }
	_ = readJSON(r, &in)
	phone := regexp.MustCompile(`\D`).ReplaceAllString(in.Phone, "")
	s.mu.Lock()
	l := s.linker
	s.mu.Unlock()
	running := false
	if l != nil {
		select {
		case <-l.Done():
		default:
			running = true
		}
	}
	if !running {
		base := s.baseCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, 10*time.Minute)
		nl, err := wa.StartLink(ctx, s.WA, phone, true)
		if err != nil {
			cancel()
			failCode(w, 409, "conflict", err)
			return
		}
		go func() { <-nl.Done(); cancel() }()
		s.mu.Lock()
		s.linker = nl
		s.mu.Unlock()
		s.DB.Log(r.Context(), actorOf(r), "started linking WhatsApp")
		time.Sleep(1500 * time.Millisecond) // usually enough for the first QR code
	}
	s.whatsappState(w, r)
}

func (s *Server) whatsappQR(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	l := s.linker
	s.mu.Unlock()
	if l == nil || l.QR() == "" {
		fail(w, 404, errors.New("no QR code right now"))
		return
	}
	png, err := qrcode.Encode(l.QR(), qrcode.Medium, 512)
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) whatsappLogout(w http.ResponseWriter, r *http.Request) {
	if s.Demo || s.WA == nil || s.WA.Store.ID == nil {
		failCode(w, 409, "conflict", errors.New("WhatsApp isn't linked"))
		return
	}
	if err := s.WA.Logout(r.Context()); err != nil {
		fail(w, 500, err)
		return
	}
	s.setConnected(false)
	s.DB.Log(r.Context(), actorOf(r), "unlinked WhatsApp")
	s.whatsappState(w, r)
}

// telegramApp saves the app id and hash from my.telegram.org and starts Telegram.
func (s *Server) telegramApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APIID   string `json:"api_id"`
		APIHash string `json:"api_hash"`
	}
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_request", err)
		return
	}
	in.APIID, in.APIHash = strings.TrimSpace(in.APIID), strings.TrimSpace(in.APIHash)
	if !regexp.MustCompile(`^\d{3,12}$`).MatchString(in.APIID) || !regexp.MustCompile(`^[0-9a-fA-F]{32}$`).MatchString(in.APIHash) {
		failCode(w, 400, "bad_request", errors.New("api_id is a number and api_hash is 32 letters and digits, both from my.telegram.org → API development tools"))
		return
	}
	if s.Demo {
		failCode(w, 409, "unavailable", errors.New("not in demo mode"))
		return
	}
	if s.TG != nil {
		failCode(w, 409, "conflict", errors.New("Telegram is already set up"))
		return
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "telegram.app"), []byte(in.APIID+"\n"+in.APIHash+"\n"), 0o600); err != nil {
		fail(w, 500, err)
		return
	}
	t, err := tg.New(s.DataDir)
	if err != nil || t == nil {
		fail(w, 500, errors.Join(errors.New("couldn't start Telegram"), err))
		return
	}
	s.TG = t
	base := s.baseCtx
	if base == nil {
		base = context.Background()
	}
	go s.RunTelegram(base)
	s.DB.Log(r.Context(), actorOf(r), "set up Telegram")
	time.Sleep(time.Second)
	s.telegramState(w, r)
}
