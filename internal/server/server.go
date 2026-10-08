// Package server is Townsquare's HTTP API, embedded UI and send loop.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/tgbot"
	"github.com/nerveband/townsquare/internal/update"
	"github.com/nerveband/townsquare/internal/version"
	"github.com/nerveband/townsquare/internal/wa"
)

type Server struct {
	DB      *store.DB
	WA      *whatsmeow.Client
	DataDir string
	UI      fs.FS
	Demo    bool       // sample data; never connects or sends
	TG      *tg.Client // Telegram account; nil when not set up
	Bot     *tgbot.Bot // Telegram bot; nil when not set up
	Updater *update.Updater
	Restart chan string // main restarts ("update") or stops ("quit") on request
	AppMode bool        // started by opening the app; Settings offers Quit
	Listen  string      // address the web app listens on (for start at login)

	mu        sync.Mutex
	connected bool
	sending   string
	linker    *wa.Linker      // WhatsApp linking in progress, from the browser
	baseCtx   context.Context // lives as long as the server
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/state", s.state)
	m.HandleFunc("GET /api/targets", s.targets)
	m.HandleFunc("POST /api/targets/refresh", s.refreshTargets)
	m.HandleFunc("PATCH /api/targets/{jid}", s.patchTarget)
	m.HandleFunc("POST /api/tags", s.saveNamed("tag"))
	m.HandleFunc("PUT /api/tags/{id}", s.saveNamed("tag"))
	m.HandleFunc("DELETE /api/tags/{id}", s.deleteNamed("tag"))
	m.HandleFunc("POST /api/clients", s.saveNamed("client"))
	m.HandleFunc("PUT /api/clients/{id}", s.saveNamed("client"))
	m.HandleFunc("DELETE /api/clients/{id}", s.deleteNamed("client"))
	m.HandleFunc("POST /api/sets", s.saveSet)
	m.HandleFunc("PUT /api/sets/{id}", s.saveSet)
	m.HandleFunc("DELETE /api/sets/{id}", s.deleteNamed("set"))
	m.HandleFunc("PUT /api/settings", s.saveSettings)
	m.HandleFunc("POST /api/media", s.uploadMedia)
	m.HandleFunc("GET /api/media/{id}", s.mediaInfo)
	m.HandleFunc("GET /api/media/{id}/preview", s.mediaFile(true))
	m.HandleFunc("GET /api/media/{id}/file", s.mediaFile(false))
	m.HandleFunc("GET /api/posts", s.v1Posts)
	m.HandleFunc("POST /api/posts/bulk", s.bulkPosts)
	m.HandleFunc("POST /api/posts", s.createPost)
	m.HandleFunc("GET /api/posts/{id}", s.getPost)
	m.HandleFunc("PUT /api/posts/{id}", s.updatePost)
	m.HandleFunc("DELETE /api/posts/{id}", s.deletePost)
	m.HandleFunc("POST /api/posts/{id}/duplicate", s.duplicatePost)
	m.HandleFunc("POST /api/posts/{id}/send-now", s.sendNow)
	m.HandleFunc("GET /api/posts/{id}/next", s.nextRuns)
	m.HandleFunc("POST /api/preview", s.preview)
	m.HandleFunc("POST /api/test", s.testSend)
	m.HandleFunc("GET /api/sends", s.sends)
	m.HandleFunc("GET /api/sends/detail", s.sendDetail)
	m.HandleFunc("POST /api/sends/move", s.moveSend)
	m.HandleFunc("POST /api/sends/copy", s.copySend)
	m.HandleFunc("POST /api/sends/skip", s.skipSend)
	m.HandleFunc("GET /api/changes", s.changes)
	m.HandleFunc("POST /api/undo", s.undo)
	m.HandleFunc("GET /api/auth/status", s.authStatus)
	m.HandleFunc("POST /api/auth/request", s.authRequest)
	m.HandleFunc("POST /api/auth/logout", s.authLogout)
	m.HandleFunc("GET /auth/login", s.authLogin)
	m.HandleFunc("GET /api/sessions", s.listSessions)
	m.HandleFunc("DELETE /api/sessions/{id}", s.revokeSession)
	m.HandleFunc("GET /api/telegram", s.telegramState)
	m.HandleFunc("POST /api/telegram/login", s.telegramLogin)
	m.HandleFunc("GET /api/telegram/qr.png", s.telegramQR)
	m.HandleFunc("POST /api/telegram/password", s.telegramPassword)
	m.HandleFunc("POST /api/telegram/logout", s.telegramLogout)
	m.HandleFunc("POST /api/telegram/refresh", s.telegramRefresh)
	m.HandleFunc("GET /api/keys", s.listKeys)
	m.HandleFunc("POST /api/keys", s.createKey)
	m.HandleFunc("DELETE /api/keys/{id}", s.revokeKey)
	m.Handle("/api/v1/", s.v1())
	m.HandleFunc("POST /api/redo", s.redo)
	m.HandleFunc("GET /api/stats/summary", s.statsSummary)
	m.HandleFunc("GET /api/stats/summary.txt", s.statsText)
	m.HandleFunc("GET /api/stats/posts/{id}", s.statsPost)
	m.HandleFunc("GET /api/stats/badges", s.statsBadges)
	m.HandleFunc("GET /api/stats/export.csv", s.statsExport)
	m.HandleFunc("POST /api/stats/share", s.statsShare)
	m.HandleFunc("GET /api/update", s.updateState)
	m.HandleFunc("POST /api/update/check", s.updateCheck)
	m.HandleFunc("POST /api/update/install", s.updateInstall)
	m.HandleFunc("GET /api/autostart", s.autostartState)
	m.HandleFunc("PUT /api/autostart", s.setAutostart)
	m.HandleFunc("POST /api/quit", s.quit)
	m.HandleFunc("GET /api/whatsapp", s.whatsappState)
	m.HandleFunc("POST /api/whatsapp/link", s.whatsappLink)
	m.HandleFunc("GET /api/whatsapp/qr.png", s.whatsappQR)
	m.HandleFunc("POST /api/whatsapp/logout", s.whatsappLogout)
	m.HandleFunc("POST /api/telegram/app", s.telegramApp)
	if s.UI != nil {
		files := http.FileServer(http.FS(s.UI))
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if _, err := fs.Stat(s.UI, strings.TrimPrefix(r.URL.Path, "/")); err != nil || r.URL.Path == "/" {
				r.URL.Path = "/"
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // hashed file names
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
		})
	}
	return logRequests(s.requireSession(m))
}

// deprecated wraps a handler that is being retired. It keeps working until the
// sunset date and tells clients what replaces it (RFC 9745 / RFC 8594 headers).
// Also mark the operation "deprecated": true in tools/gen_openapi.py.
func init() { _ = mime.AddExtensionType(".webmanifest", "application/manifest+json") }

func deprecated(h http.HandlerFunc, sunset, successor string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		if sunset != "" {
			w.Header().Set("Sunset", sunset)
		}
		if successor != "" {
			w.Header().Set("Link", "<"+successor+`>; rel="successor-version"`)
		}
		h(w, r)
	}
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Townsquare-Version", version.Version)
		if r.Method != http.MethodGet {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

// ---------- WhatsApp connection ----------

// Connect keeps the WhatsApp connection up and refreshes targets once connected.
func (s *Server) Connect(ctx context.Context) error {
	s.baseCtx = ctx
	s.WA.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.Connected:
			s.setConnected(true)
			go func() {
				if err := s.syncTargets(context.Background()); err != nil {
					log.Println("targets:", err)
				}
			}()
		case *events.Disconnected, *events.LoggedOut, *events.StreamReplaced:
			s.setConnected(false)
		case *events.Receipt, *events.Message:
			go s.onWAStat(evt)
		}
	})
	if s.WA.Store.ID == nil {
		return errors.New("not paired: run `townsquare pair` first")
	}
	return s.WA.ConnectContext(ctx)
}

func (s *Server) setConnected(v bool) {
	s.mu.Lock()
	s.connected = v
	s.mu.Unlock()
}

func (s *Server) isConnected() bool {
	if s.Demo {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected && s.WA.IsConnected()
}

func (s *Server) syncTargets(ctx context.Context) error {
	ts, err := wa.ListTargets(ctx, s.WA)
	if err != nil {
		return err
	}
	conv := make([]store.Target, 0, len(ts))
	for _, t := range ts {
		conv = append(conv, store.Target{JID: t.JID, Kind: t.Kind, Name: t.Name, Parent: t.Parent, CanSend: t.CanSend, Members: t.Members})
	}
	if err := s.DB.UpsertTargets(ctx, "whatsapp", conv); err != nil {
		return err
	}
	// One-time import of the CLI allowlist.
	if b, err := os.ReadFile(filepath.Join(s.DataDir, "allow.txt")); err == nil {
		for _, j := range strings.Fields(string(b)) {
			_, _ = s.DB.ExecContext(ctx, `UPDATE targets SET allowed=1 WHERE jid=?`, j)
		}
		_ = os.Rename(filepath.Join(s.DataDir, "allow.txt"), filepath.Join(s.DataDir, "allow.txt.imported"))
	}
	log.Printf("targets: %d synced", len(conv))
	return nil
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

var codeNames = map[int]string{400: "bad_request", 401: "unauthorized", 403: "forbidden", 404: "not_found", 409: "conflict", 422: "unprocessable", 500: "internal", 503: "unavailable"}

func fail(w http.ResponseWriter, code int, err error) { failCode(w, code, codeNames[code], err) }

// failCode writes {"error": "human message", "code": "machine_code"}.
func failCode(w http.ResponseWriter, status int, code string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "code": code})
}

// peekBody reads the request body and puts it back, for middleware checks.
func peekBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	r.Body = io.NopCloser(bytes.NewReader(b))
	return string(b)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// mutated responds with the change id and undo/redo hints.
func (s *Server) mutated(w http.ResponseWriter, r *http.Request, id int64, extra map[string]any) {
	u, re := s.DB.CanUndoRedo(r.Context())
	out := map[string]any{"change": id, "undo": u, "redo": re}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, out)
}

// ---------- state ----------

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, re := s.DB.CanUndoRedo(ctx)
	settings := s.DB.Settings(ctx)
	phone := ""
	if s.WA.Store.ID != nil {
		phone = s.WA.Store.ID.User
	}
	s.mu.Lock()
	sending := s.sending
	s.mu.Unlock()
	drafts, _ := s.DB.Posts(ctx, "draft")
	writeJSON(w, map[string]any{
		"settings": settings, "tags": s.DB.Tags(ctx), "clients": s.DB.Clients(ctx), "sets": s.DB.Sets(ctx),
		"connected": s.isConnected(), "phone": phone, "sending": sending,
		"sent_today": s.DB.SentToday(ctx, settings["timezone"]),
		"undo":       u, "redo": re, "drafts": drafts, "version": version.Version, "demo": s.Demo,
		"telegram": s.TG.State().Status, "telegram_user": s.TG.State().User,
	})
}

// ---------- targets ----------

func (s *Server) targets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.DB.Targets(r.Context()))
}

func (s *Server) refreshTargets(w http.ResponseWriter, r *http.Request) {
	if !s.isConnected() {
		fail(w, http.StatusServiceUnavailable, errors.New("WhatsApp is not connected"))
		return
	}
	if err := s.syncTargets(r.Context()); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, s.DB.Targets(r.Context()))
}

func (s *Server) patchTarget(w http.ResponseWriter, r *http.Request) {
	jid := r.PathValue("jid")
	var in struct {
		ClientID *int64 `json:"client_id"`
		NoClient bool   `json:"no_client"`
		Allowed  *bool  `json:"allowed"`
		Starred  *bool  `json:"starred"`
	}
	raw := peekBody(r)
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if strings.Contains(strings.ReplaceAll(raw, " ", ""), `"client_id":null`) {
		in.NoClient = true // explicit null clears the client
	}
	var name string
	_ = s.DB.QueryRowContext(r.Context(), `SELECT name FROM targets WHERE jid=?`, jid).Scan(&name)
	if name == "" {
		fail(w, 404, store.ErrNotFound)
		return
	}
	var what []string
	id, err := s.DB.Mutate(r.Context(), actorOf(r), "", []store.Key{{Type: "target", ID: jid}}, func(tx *store.Tx) error {
		if in.ClientID != nil || in.NoClient {
			if _, err := tx.Exec(`UPDATE targets SET client_id=? WHERE jid=?`, in.ClientID, jid); err != nil {
				return err
			}
			what = append(what, "changed the client of "+name)
		}
		if in.Allowed != nil {
			if _, err := tx.Exec(`UPDATE targets SET allowed=? WHERE jid=?`, *in.Allowed, jid); err != nil {
				return err
			}
			if *in.Allowed {
				what = append(what, "allowed sending to "+name)
			} else {
				what = append(what, "blocked sending to "+name)
			}
		}
		if in.Starred != nil {
			if _, err := tx.Exec(`UPDATE targets SET starred=? WHERE jid=?`, *in.Starred, jid); err != nil {
				return err
			}
			what = append(what, map[bool]string{true: "starred ", false: "unstarred "}[*in.Starred]+name)
		}
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	_, _ = s.DB.ExecContext(r.Context(), `UPDATE changes SET summary=? WHERE id=?`, strings.Join(what, ", "), id)
	s.mutated(w, r, id, nil)
}

// ---------- tags, clients, sets, settings ----------

func (s *Server) saveNamed(kind string) http.HandlerFunc {
	table := map[string]string{"tag": "tags", "client": "clients"}[kind]
	return func(w http.ResponseWriter, r *http.Request) {
		var in store.Client // tags use only name and color
		if err := readJSON(r, &in); err != nil {
			fail(w, 400, err)
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			fail(w, 400, errors.New("name is required"))
			return
		}
		if in.Color == "" {
			in.Color = "#128C7E"
		}
		if kind == "client" {
			if err := validQuiet(in.QuietStart, in.QuietEnd, in.Timezone); err != nil {
				failCode(w, 422, "invalid", err)
				return
			}
		}
		id := pathID(r)
		var keys []store.Key
		summary := fmt.Sprintf("created %s %s", kind, in.Name)
		if id != 0 {
			keys = []store.Key{{Type: kind, ID: strconv.FormatInt(id, 10)}}
			summary = fmt.Sprintf("edited %s %s", kind, in.Name)
		}
		cid, err := s.DB.Mutate(r.Context(), actorOf(r), summary, keys, func(tx *store.Tx) error {
			if kind == "client" {
				if id != 0 {
					_, err := tx.Exec(`UPDATE clients SET name=?, color=?, quiet_start=?, quiet_end=?, timezone=? WHERE id=?`, in.Name, in.Color, in.QuietStart, in.QuietEnd, in.Timezone, id)
					return err
				}
				res, err := tx.Exec(`INSERT INTO clients(name,color,quiet_start,quiet_end,timezone) VALUES(?,?,?,?,?)`, in.Name, in.Color, in.QuietStart, in.QuietEnd, in.Timezone)
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
				tx.Created(store.Key{Type: kind, ID: strconv.FormatInt(id, 10)})
				return nil
			}
			if id != 0 {
				_, err := tx.Exec(`UPDATE `+table+` SET name=?, color=? WHERE id=?`, in.Name, in.Color, id)
				return err
			}
			res, err := tx.Exec(`INSERT INTO `+table+`(name,color) VALUES(?,?)`, in.Name, in.Color)
			if err != nil {
				return err
			}
			id, _ = res.LastInsertId()
			tx.Created(store.Key{Type: kind, ID: strconv.FormatInt(id, 10)})
			return nil
		})
		if err != nil {
			fail(w, 500, err)
			return
		}
		s.mutated(w, r, cid, map[string]any{"id": id})
	}
}

// validQuiet checks optional per-client quiet hours: both empty (use global) or both HH:MM.
func validQuiet(start, end, tz string) error {
	if (start == "") != (end == "") {
		return errors.New("set both quiet_start and quiet_end, or neither (to use the global quiet hours)")
	}
	for _, v := range []string{start, end} {
		if v == "" {
			continue
		}
		if _, err := time.Parse("15:04", v); err != nil {
			return fmt.Errorf("quiet hours must be HH:MM, got %q", v)
		}
	}
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("unknown time zone %q", tz)
		}
	}
	return nil
}

func (s *Server) deleteNamed(kind string) http.HandlerFunc {
	table := map[string]string{"tag": "tags", "client": "clients", "set": "target_sets"}[kind]
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		var name string
		_ = s.DB.QueryRowContext(r.Context(), `SELECT name FROM `+table+` WHERE id=?`, id).Scan(&name)
		if name == "" {
			fail(w, 404, store.ErrNotFound)
			return
		}
		// Posts and targets that reference it are cleared, and restored on undo.
		keys := []store.Key{{Type: kind, ID: strconv.FormatInt(id, 10)}}
		col := map[string]string{"tag": "tag_id", "client": "client_id"}[kind]
		if col != "" {
			rows, _ := s.DB.QueryContext(r.Context(), `SELECT id FROM posts WHERE `+col+`=?`, id)
			for rows != nil && rows.Next() {
				var pid int64
				_ = rows.Scan(&pid)
				keys = append(keys, store.PostKey(pid))
			}
			if rows != nil {
				rows.Close()
			}
		}
		if kind == "client" {
			rows, _ := s.DB.QueryContext(r.Context(), `SELECT jid FROM targets WHERE client_id=?`, id)
			for rows != nil && rows.Next() {
				var j string
				_ = rows.Scan(&j)
				keys = append(keys, store.Key{Type: "target", ID: j})
			}
			if rows != nil {
				rows.Close()
			}
		}
		cid, err := s.DB.Mutate(r.Context(), actorOf(r), fmt.Sprintf("deleted %s %s", kind, name), keys, func(tx *store.Tx) error {
			if col != "" {
				if _, err := tx.Exec(`UPDATE posts SET `+col+`=NULL WHERE `+col+`=?`, id); err != nil {
					return err
				}
			}
			if kind == "client" {
				if _, err := tx.Exec(`UPDATE targets SET client_id=NULL WHERE client_id=?`, id); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE target_sets SET client_id=NULL WHERE client_id=?`, id); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`DELETE FROM `+table+` WHERE id=?`, id)
			return err
		})
		if err != nil {
			fail(w, 500, err)
			return
		}
		s.mutated(w, r, cid, nil)
	}
}

func (s *Server) saveSet(w http.ResponseWriter, r *http.Request) {
	var in store.Set
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		fail(w, 400, errors.New("name is required"))
		return
	}
	id := pathID(r)
	j, _ := json.Marshal(in.JIDs)
	var keys []store.Key
	summary := "created group set " + in.Name
	if id != 0 {
		keys = []store.Key{{Type: "set", ID: strconv.FormatInt(id, 10)}}
		summary = "edited group set " + in.Name
	}
	cid, err := s.DB.Mutate(r.Context(), actorOf(r), summary, keys, func(tx *store.Tx) error {
		if id != 0 {
			_, err := tx.Exec(`UPDATE target_sets SET name=?,client_id=?,jids=? WHERE id=?`, in.Name, in.ClientID, string(j), id)
			return err
		}
		res, err := tx.Exec(`INSERT INTO target_sets(name,client_id,jids) VALUES(?,?,?)`, in.Name, in.ClientID, string(j))
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		tx.Created(store.Key{Type: "set", ID: strconv.FormatInt(id, 10)})
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.mutated(w, r, cid, map[string]any{"id": id})
}

var editableSettings = map[string]string{
	"tg_queue_hours": "Telegram queue window",
	"auto_update":    "automatic updates",
	"stats_people":   "who read it lists",
	"timezone":       "time zone", "safe_mode": "safe mode", "gap_min": "minimum gap", "gap_max": "maximum gap",
	"daily_cap": "daily limit", "quiet_start": "quiet hours", "quiet_end": "quiet hours", "grace_min": "late-send grace",
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	var keys []store.Key
	var names []string
	for k, v := range in {
		label, ok := editableSettings[k]
		if !ok {
			fail(w, 400, fmt.Errorf("unknown setting %q", k))
			return
		}
		if k == "timezone" {
			if _, err := time.LoadLocation(v); err != nil {
				fail(w, 400, fmt.Errorf("unknown time zone %q", v))
				return
			}
		}
		keys = append(keys, store.Key{Type: "setting", ID: k})
		if k == "safe_mode" {
			label = map[string]string{"1": "turned safe mode on", "0": "turned safe mode off"}[v]
		} else {
			label = "changed " + label
		}
		names = append(names, label)
	}
	cid, err := s.DB.Mutate(r.Context(), actorOf(r), strings.Join(uniq(names), ", "), keys, func(tx *store.Tx) error {
		for k, v := range in {
			if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	if in["stats_people"] == "0" {
		s.DB.StatClearNames(r.Context())
	}
	s.mutated(w, r, cid, nil)
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// ---------- media ----------

func mediaKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "voice"
	}
	return "document"
}

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	var (
		name, mt string
		src      io.Reader
	)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		// {"url": "https://...", "kind": "voice"}: fetch a file by URL (handy for agents).
		var in struct{ URL, Kind, Name string }
		if err := readJSON(r, &in); err != nil || in.URL == "" {
			failCode(w, 400, "bad_request", errors.New(`send multipart "file", or JSON {"url": "..."}`))
			return
		}
		resp, err := http.Get(in.URL)
		if err != nil {
			failCode(w, 422, "fetch_failed", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			failCode(w, 422, "fetch_failed", fmt.Errorf("GET %s: %s", in.URL, resp.Status))
			return
		}
		name = nz(in.Name, filepath.Base(strings.Split(resp.Request.URL.Path, "?")[0]))
		mt = strings.Split(resp.Header.Get("Content-Type"), ";")[0]
		src = io.LimitReader(resp.Body, 200<<20)
		r.Form = map[string][]string{"kind": {in.Kind}}
	} else {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			failCode(w, 400, "bad_request", err)
			return
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			failCode(w, 400, "bad_request", errors.New(`multipart field "file" is required`))
			return
		}
		defer f.Close()
		name, mt, src = filepath.Base(hdr.Filename), hdr.Header.Get("Content-Type"), f
	}
	m, err := s.ingestMedia(r.Context(), name, mt, r.FormValue("kind"), src)
	if err != nil {
		failCode(w, 422, "media_failed", err)
		return
	}
	writeJSON(w, m)
}

// ingestMedia stores a file, converts it for WhatsApp and makes a preview.
func (s *Server) ingestMedia(ctx context.Context, name, mt, kind string, src io.Reader) (*store.Media, error) {
	dir := filepath.Join(s.DataDir, "media")
	_ = os.MkdirAll(filepath.Join(dir, "orig"), 0o700)
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	if mt == "" || mt == "application/octet-stream" {
		mt = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	if kind == "" {
		kind = mediaKind(mt)
	}
	orig := filepath.Join(dir, "orig", fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
	out, err := os.Create(orig)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return nil, err
	}
	out.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	conv, err := wa.Convert(ctx, kind, orig)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert %s: %w", name, err)
	}
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	final := filepath.Join(dir, stamp+filepath.Ext(conv.Path))
	if conv.Path == orig {
		final = orig
	} else if err := os.Rename(conv.Path, final); err != nil {
		return nil, err
	}
	m := &store.Media{Kind: kind, Name: name, Path: final, Mime: conv.Mime, Width: conv.Width, Height: conv.Height, Seconds: conv.Seconds}
	if kind == "image" || kind == "video" {
		prev := filepath.Join(dir, stamp+".preview.jpg")
		if wa.Preview(ctx, kind, final, prev) == nil {
			m.Thumb = prev
		}
	}
	return m, s.DB.AddMedia(ctx, m)
}

func (s *Server) mediaInfo(w http.ResponseWriter, r *http.Request) {
	m, err := s.DB.Media(r.Context(), pathID(r))
	if err != nil {
		fail(w, 404, err)
		return
	}
	writeJSON(w, m)
}

func (s *Server) mediaFile(preview bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := s.DB.Media(r.Context(), pathID(r))
		if err != nil {
			fail(w, 404, err)
			return
		}
		p := m.Path
		if preview {
			if m.Thumb == "" {
				fail(w, 404, errors.New("no preview"))
				return
			}
			p = m.Thumb
		}
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, p)
	}
}
