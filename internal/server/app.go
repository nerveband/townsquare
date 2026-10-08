package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	townsquare "github.com/nerveband/townsquare"
	"github.com/nerveband/townsquare/internal/autostart"
	"github.com/nerveband/townsquare/internal/changelog"
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

// nextFreeWindow is the first time from now with no post due within 15 minutes
// either side: when an automatic update (or a requested restart) will happen.
func (s *Server) nextFreeWindow(ctx context.Context, now time.Time) time.Time {
	posts, err := s.DB.Posts(ctx, "scheduled")
	if err != nil {
		return now
	}
	const pad = 15 * time.Minute
	occ := store.Expand(posts, now.Add(-pad), now.Add(72*time.Hour))
	t := now
	for moved := true; moved; {
		moved = false
		for _, o := range occ {
			if !t.Before(o.At.Add(-pad)) && !t.After(o.At.Add(pad)) {
				t = o.At.Add(pad + time.Minute)
				moved = true
			}
		}
	}
	return t
}

func (s *Server) sendingNow() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sending
}

// RunUpdates checks for a new release every 6 hours. With automatic updates on
// (the default) it downloads the release and restarts into it at the first
// minute with no post due within 15 minutes either side and nothing being sent.
// Before restarting it runs the new binary once to make sure it works; a version
// that fails is skipped. Builds from source only report updates.
func (s *Server) RunUpdates(ctx context.Context) {
	if s.Updater == nil || s.Demo {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	var last time.Time
	for n := 0; ; n++ {
		select {
		case <-ctx.Done():
			return
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
			s.syncTelegramApp()
		}
		if s.busySoon(ctx) != "" {
			continue // never restart around a send
		}
		if s.reloadWanted() {
			log.Println("telegram: shared app id changed; restarting to use it")
			s.DB.Log(ctx, "townsquare", "switched to the new shared Telegram app id")
			s.requestRestart("reload")
			return
		}
		if !auto || dev {
			continue
		}
		if st, ok := update.ReadStaged(s.DataDir); ok && update.Newer(st.Version, s.Updater.Current) {
			if err := update.Probe(s.DataDir, s.Updater.Current); err != nil {
				log.Println("update:", err)
				s.DB.Log(ctx, "townsquare", "skipped update "+st.Version+": it didn't start on this computer")
				continue
			}
			s.DB.Log(ctx, "townsquare", "updated itself to "+st.Version)
			log.Println("update: restarting into", st.Version)
			s.requestRestart("update")
			return
		}
	}
}

func (s *Server) updateState(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeJSON(w, map[string]any{"current": "demo", "dev": true, "auto": false})
		return
	}
	st := s.Updater.State()
	changes := st.Changes
	if changes == nil {
		changes = []changelog.Entry{}
	}
	writeJSON(w, map[string]any{"current": st.Current, "latest": st.Latest, "notes": st.Notes, "available": st.Available,
		"staged": st.Staged, "checked_at": st.CheckedAt, "error": st.Error, "platform": st.Platform, "dev": st.Dev,
		"auto": s.DB.Setting(r.Context(), "auto_update") != "0", "changes": changes,
		"install_at": s.nextFreeWindow(r.Context(), time.Now()).Unix()})
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
	s.syncTelegramApp()
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
	if err := update.Probe(s.DataDir, s.Updater.Current); err != nil {
		failCode(w, 502, "unavailable", err)
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
		err = autostart.Enable(s.DataDir)
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

// syncTelegramApp saves the shared Telegram app id from the newest verified
// manifest. With no Telegram before, it starts Telegram right away; if the id in
// use changed, the server restarts into it at the next quiet moment.
func (s *Server) syncTelegramApp() {
	if s.Updater == nil || s.Demo {
		return
	}
	m := s.Updater.Last()
	if m == nil || m.Telegram == nil {
		return
	}
	a := tg.App{ID: m.Telegram.ID, Hash: m.Telegram.Hash}
	changed, err := tg.SaveSharedApp(s.DataDir, a)
	if err != nil {
		log.Println("telegram:", err)
		return
	}
	if s.TG == nil {
		s.startTelegram()
		return
	}
	if changed && s.TG.Source() != "own" && !s.TG.UsesApp(a) {
		s.mu.Lock()
		s.reload = true
		s.mu.Unlock()
	}
}

func (s *Server) reloadWanted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reload
}

// startTelegram starts Telegram if an app id is available and it isn't running.
func (s *Server) startTelegram() {
	if s.TG != nil {
		return
	}
	t, err := tg.New(s.DataDir)
	if err != nil || t == nil {
		if err != nil {
			log.Println("telegram:", err)
		}
		return
	}
	s.TG = t
	base := s.baseCtx
	if base == nil {
		base = context.Background()
	}
	go s.RunTelegram(base)
}

// telegramApp sets your own Telegram app id (from my.telegram.org) instead of
// Townsquare's shared one. Telegram restarts to use it, so do this before logging in.
func (s *Server) telegramApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		APIID   string `json:"api_id"`
		APIHash string `json:"api_hash"`
	}
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_request", err)
		return
	}
	a, err := tg.ParseApp(strings.TrimSpace(in.APIID) + ":" + strings.TrimSpace(in.APIHash))
	if err != nil || !regexp.MustCompile(`^[0-9a-fA-F]{32}$`).MatchString(a.Hash) {
		failCode(w, 400, "bad_request", errors.New("api_id is a number and api_hash is 32 letters and digits, both from my.telegram.org → API development tools"))
		return
	}
	s.setTelegramApp(w, r, &a)
}

// telegramAppReset goes back to Townsquare's shared Telegram app id.
func (s *Server) telegramAppReset(w http.ResponseWriter, r *http.Request) {
	s.setTelegramApp(w, r, nil)
}

func (s *Server) setTelegramApp(w http.ResponseWriter, r *http.Request, a *tg.App) {
	if s.Demo {
		failCode(w, 409, "unavailable", errors.New("not in demo mode"))
		return
	}
	if s.TG.Ready() {
		failCode(w, 409, "conflict", errors.New("log out of Telegram first; the app id is used when you log in"))
		return
	}
	var err error
	msg := "set a custom Telegram app id"
	if a != nil {
		err = tg.SaveOwnApp(s.DataDir, *a)
	} else {
		err, msg = tg.RemoveOwnApp(s.DataDir), "went back to the shared Telegram app id"
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), msg)
	if s.TG == nil {
		s.startTelegram()
		time.Sleep(time.Second)
		s.telegramState(w, r)
		return
	}
	// A running client keeps its id until restarted.
	writeJSON(w, map[string]any{"ok": true, "restarting": true})
	go func() { time.Sleep(500 * time.Millisecond); s.requestRestart("reload") }()
}

// changelogHandler returns release notes from the CHANGELOG.md built into this
// version: ?from=v0.6.0 (exclusive) and ?to=v0.7.0 (inclusive), newest first.
func (s *Server) changelogHandler(w http.ResponseWriter, r *http.Request) {
	es := changelog.Between(changelog.Parse(townsquare.Changelog), r.URL.Query().Get("from"), r.URL.Query().Get("to"), update.Newer)
	if n := atoi(r.URL.Query().Get("limit"), 0); n > 0 && len(es) > n {
		es = es[:n]
	}
	if es == nil {
		es = []changelog.Entry{}
	}
	writeJSON(w, es)
}
