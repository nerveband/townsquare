package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nerveband/townsquare/internal/acct"
	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/wa"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// More than one account. Most people link one WhatsApp and one Telegram
// account and never see any of this: those stay "account 0" with their chat ids
// unchanged. Each extra account has its own login in DATA/accounts/<id>/ and
// its chats carry its number (see internal/acct). Picking a chat picks the
// account that posts to it.

type extraAccount struct {
	store.Account
	wa     *whatsmeow.Client // WhatsApp accounts
	tg     *tg.Client        // Telegram accounts
	linker *wa.Linker

	mu        sync.Mutex
	connected bool
	since     time.Time
	down      time.Time
	synced    time.Time
	chats     int
	cancel    context.CancelFunc
}

type accountSet struct {
	mu    sync.Mutex
	byID  map[int64]*extraAccount
	level string // whatsmeow log level
}

func (s *Server) accountDir(id int64) string {
	return filepath.Join(s.DataDir, "accounts", strconv.FormatInt(id, 10))
}

func (s *Server) extra(id int64) *extraAccount {
	s.accts.mu.Lock()
	defer s.accts.mu.Unlock()
	if s.accts.byID == nil {
		return nil
	}
	return s.accts.byID[id]
}

func (s *Server) extras() []*extraAccount {
	s.accts.mu.Lock()
	defer s.accts.mu.Unlock()
	var out []*extraAccount
	for _, a := range s.accts.byID {
		out = append(out, a)
	}
	return out
}

// StartAccounts opens every extra account (call after Connect). logLevel is whatsmeow's.
func (s *Server) StartAccounts(ctx context.Context, logLevel string) {
	s.accts.mu.Lock()
	s.accts.level = logLevel
	s.accts.byID = map[int64]*extraAccount{}
	s.accts.mu.Unlock()
	for _, a := range s.DB.Accounts(ctx) {
		if err := s.openAccount(ctx, a); err != nil {
			log.Printf("account %d (%s): %v", a.ID, a.Platform, err)
		}
	}
}

func (s *Server) openAccount(ctx context.Context, a store.Account) error {
	base := s.baseCtx
	if base == nil {
		base = ctx
	}
	ac, cancel := context.WithCancel(base)
	x := &extraAccount{Account: a, cancel: cancel}
	s.accts.mu.Lock()
	if s.accts.byID == nil {
		s.accts.byID = map[int64]*extraAccount{}
	}
	s.accts.byID[a.ID] = x
	level := s.accts.level
	s.accts.mu.Unlock()
	switch a.Platform {
	case "whatsapp":
		cli, err := wa.Open(ac, s.accountDir(a.ID), nz(level, "WARN"))
		if err != nil {
			return err
		}
		x.wa = cli
		cli.AddEventHandler(func(evt any) {
			switch evt.(type) {
			case *events.Connected:
				x.mu.Lock()
				x.connected, x.since = true, time.Now()
				x.mu.Unlock()
				go func() {
					if err := s.syncAccount(context.Background(), x); err != nil {
						log.Printf("account %d chats: %v", a.ID, err)
					}
				}()
			case *events.Disconnected, *events.LoggedOut, *events.StreamReplaced:
				x.mu.Lock()
				x.connected, x.down = false, time.Now()
				x.mu.Unlock()
			case *events.Receipt, *events.Message:
				go s.onWAStat(evt)
			}
		})
		if cli.Store.ID != nil {
			go func() {
				if err := cli.ConnectContext(ac); err != nil {
					log.Printf("account %d: %v", a.ID, err)
				}
			}()
		}
	case "telegram":
		c, err := tg.NewIn(s.DataDir, s.accountDir(a.ID))
		if err != nil {
			return err
		}
		if c == nil {
			return errors.New("Telegram isn't set up (no app id)")
		}
		x.tg = c
		go c.Run(ac)
		go func() { // load chats once logged in, then every 30 minutes
			var last time.Time
			for {
				select {
				case <-ac.Done():
					return
				case <-time.After(10 * time.Second):
				}
				if c.Ready() && time.Since(last) > 30*time.Minute {
					if err := s.syncAccount(ac, x); err != nil {
						log.Printf("account %d chats: %v", a.ID, err)
						continue
					}
					last = time.Now()
				}
			}
		}()
	default:
		return fmt.Errorf("unknown platform %q", a.Platform)
	}
	return nil
}

// syncAccount loads an extra account's chats, stored under its own ids.
func (s *Server) syncAccount(ctx context.Context, x *extraAccount) error {
	var conv []store.Target
	switch x.Platform {
	case "whatsapp":
		ts, err := wa.ListTargets(ctx, x.wa)
		if err != nil {
			return err
		}
		for _, t := range ts {
			conv = append(conv, store.Target{JID: acct.Join(x.ID, t.JID), Kind: t.Kind, Name: t.Name, Parent: t.Parent, CanSend: t.CanSend, Members: t.Members})
		}
	case "telegram":
		ts, err := x.tg.Targets(ctx)
		if err != nil {
			return err
		}
		for _, t := range ts {
			conv = append(conv, store.Target{JID: acct.Join(x.ID, t.JID), Kind: t.Kind, Name: t.Name, Parent: t.Parent, CanSend: t.CanSend, Members: t.Members})
		}
	}
	if err := s.DB.UpsertAccountTargets(ctx, x.Platform, x.ID, conv); err != nil {
		return err
	}
	x.mu.Lock()
	x.synced, x.chats = time.Now(), len(conv)
	x.mu.Unlock()
	log.Printf("account %d: %d chats synced", x.ID, len(conv))
	return nil
}

// waFor returns the WhatsApp client that posts to jid, and whether it's connected.
func (s *Server) waFor(jid string) (*whatsmeow.Client, bool) {
	id := acct.Account(jid)
	if id == 0 {
		return s.WA, s.isConnected()
	}
	x := s.extra(id)
	if x == nil || x.wa == nil {
		return nil, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.wa, x.connected && x.wa.Store.ID != nil
}

// tgFor returns the Telegram client that posts to jid (nil if none).
func (s *Server) tgFor(jid string) *tg.Client {
	id := acct.Account(jid)
	if id == 0 {
		return s.TG
	}
	if x := s.extra(id); x != nil {
		return x.tg
	}
	return nil
}

// ready reports whether the account behind jid can send right now.
func (s *Server) ready(jid string) bool {
	switch {
	case isBotJID(jid):
		return s.Bot != nil
	case tg.IsTelegram(jid):
		return s.tgFor(jid).Ready()
	}
	_, ok := s.waFor(jid)
	return ok
}

func isBotJID(jid string) bool { return strings.HasPrefix(jid, "tgbot:") }

// ---- HTTP ----

type accountOut struct {
	ID        int64  `json:"id"` // 0 for the first account of each platform
	Platform  string `json:"platform"`
	Label     string `json:"label"`
	Default   bool   `json:"default"`
	Linked    bool   `json:"linked"`
	Connected bool   `json:"connected"`
	Who       string `json:"who,omitempty"` // number or @username
	Chats     int    `json:"chats"`
	Allowed   int    `json:"allowlisted"`
	LastSync  int64  `json:"last_sync"`
	Since     int64  `json:"connected_since"`
	Link      any    `json:"link,omitempty"`   // WhatsApp linking in progress
	Status    string `json:"status,omitempty"` // Telegram: logged_out, qr, password, ready, error
	Error     string `json:"error,omitempty"`
	QRVersion int    `json:"qr_version,omitempty"`
}

func (s *Server) accountCounts(ctx context.Context, platform string, id int64) (int, int) {
	var n, allowed int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(allowed),0) FROM targets WHERE platform=? AND account=? AND gone=0`, platform, id).Scan(&n, &allowed)
	return n, allowed
}

func (s *Server) describeAccount(ctx context.Context, x *extraAccount) accountOut {
	o := accountOut{ID: x.ID, Platform: x.Platform, Label: x.Label}
	o.Chats, o.Allowed = s.accountCounts(ctx, x.Platform, x.ID)
	x.mu.Lock()
	o.LastSync, o.Since = unixOrZero(x.synced), unixOrZero(x.since)
	if x.wa != nil {
		o.Linked, o.Connected = x.wa.Store.ID != nil, x.connected
		if x.wa.Store.ID != nil {
			o.Who = "+" + x.wa.Store.ID.User
		}
		if x.linker != nil {
			o.Link = x.linker.State()
		}
	}
	x.mu.Unlock()
	if x.tg != nil {
		st := x.tg.State()
		o.Status, o.Error, o.QRVersion = st.Status, st.Error, st.Version
		o.Linked, o.Connected = st.Status == "ready", st.Status == "ready"
		if st.Username != "" {
			o.Who = "@" + st.Username
		} else {
			o.Who = st.User
		}
	}
	return o
}

// listAccounts shows every account: the first WhatsApp and Telegram ones, then any extras.
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []accountOut{}
	if s.WA != nil {
		o := accountOut{ID: 0, Platform: "whatsapp", Label: "WhatsApp", Default: true, Linked: s.WA.Store.ID != nil, Connected: s.isConnected()}
		if s.WA.Store.ID != nil {
			o.Who = "+" + s.WA.Store.ID.User
		}
		o.Chats, o.Allowed = s.accountCounts(ctx, "whatsapp", 0)
		s.mu.Lock()
		o.LastSync, o.Since = unixOrZero(s.conn.WASync), unixOrZero(s.conn.WASince)
		s.mu.Unlock()
		out = append(out, o)
	}
	if s.TG != nil {
		st := s.TG.State()
		o := accountOut{ID: 0, Platform: "telegram", Label: "Telegram", Default: true, Linked: st.Status == "ready", Connected: st.Status == "ready", Status: st.Status}
		o.Who = st.User
		if st.Username != "" {
			o.Who = "@" + st.Username
		}
		o.Chats, o.Allowed = s.accountCounts(ctx, "telegram", 0)
		s.mu.Lock()
		o.LastSync = unixOrZero(s.conn.TGSync)
		s.mu.Unlock()
		out = append(out, o)
	}
	for _, a := range s.DB.Accounts(ctx) {
		if x := s.extra(a.ID); x != nil {
			out = append(out, s.describeAccount(ctx, x))
		}
	}
	writeJSON(w, out)
}

var labelRE = regexp.MustCompile(`^[\pL\pN][\pL\pN .,'&()-]{0,39}$`)

// addAccount links another WhatsApp or Telegram account and starts its QR login.
func (s *Server) addAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Platform string `json:"platform"`
		Label    string `json:"label"`
		Phone    string `json:"phone"`
	}
	if err := readJSON(r, &in); err != nil {
		failCode(w, 400, "bad_request", err)
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Platform != "whatsapp" && in.Platform != "telegram" {
		failCode(w, 400, "bad_request", errors.New("platform is whatsapp or telegram"))
		return
	}
	if !labelRE.MatchString(in.Label) {
		failCode(w, 400, "bad_request", errors.New("give the account a short name (up to 40 letters), for example \"ISLA phone\""))
		return
	}
	if s.Demo {
		failCode(w, 409, "unavailable", errors.New("not in demo mode"))
		return
	}
	if in.Platform == "whatsapp" && (s.WA == nil || s.WA.Store.ID == nil) {
		failCode(w, 409, "conflict", errors.New("link your first WhatsApp account first (Settings → Accounts)"))
		return
	}
	if in.Platform == "telegram" && !s.TG.Ready() {
		failCode(w, 409, "conflict", errors.New("log in to your first Telegram account first (Settings → Accounts)"))
		return
	}
	ctx := r.Context()
	a, err := s.DB.AddAccount(ctx, in.Platform, in.Label)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if err := s.openAccount(ctx, a); err != nil {
		_ = s.DB.RemoveAccount(ctx, a.ID)
		fail(w, 500, err)
		return
	}
	x := s.extra(a.ID)
	if err := s.startAccountLogin(x, in.Phone); err != nil {
		failCode(w, 503, "unavailable", err)
		return
	}
	s.DB.Log(ctx, actorOf(r), fmt.Sprintf("added the %s account %q", platformName(a.Platform), a.Label))
	time.Sleep(1500 * time.Millisecond) // usually enough for the first QR code
	writeJSON(w, s.describeAccount(ctx, x))
}

func platformName(p string) string {
	if p == "telegram" {
		return "Telegram"
	}
	return "WhatsApp"
}

func (s *Server) startAccountLogin(x *extraAccount, phone string) error {
	if x.wa != nil {
		if x.wa.Store.ID != nil {
			return nil
		}
		base := s.baseCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, 10*time.Minute)
		l, err := wa.StartLink(ctx, x.wa, regexp.MustCompile(`\D`).ReplaceAllString(phone, ""), true)
		if err != nil {
			cancel()
			return err
		}
		go func() { <-l.Done(); cancel() }()
		x.mu.Lock()
		x.linker = l
		x.mu.Unlock()
		return nil
	}
	if x.tg != nil && !x.tg.Ready() {
		for i := 0; i < 20 && x.tg.State().Status == "starting"; i++ {
			time.Sleep(250 * time.Millisecond)
		}
		return x.tg.StartQR()
	}
	return nil
}

func (s *Server) accountByPath(w http.ResponseWriter, r *http.Request) *extraAccount {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	x := s.extra(id)
	if x == nil {
		failCode(w, 404, "not_found", errors.New("no such extra account (the first WhatsApp and Telegram accounts are managed under /whatsapp and /telegram)"))
	}
	return x
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	if x := s.accountByPath(w, r); x != nil {
		writeJSON(w, s.describeAccount(r.Context(), x))
	}
}

// accountLogin restarts the QR login of an extra account (for example after it expired).
func (s *Server) accountLogin(w http.ResponseWriter, r *http.Request) {
	x := s.accountByPath(w, r)
	if x == nil {
		return
	}
	var in struct{ Phone string }
	_ = readJSON(r, &in)
	if err := s.startAccountLogin(x, in.Phone); err != nil {
		failCode(w, 409, "conflict", err)
		return
	}
	time.Sleep(1200 * time.Millisecond)
	writeJSON(w, s.describeAccount(r.Context(), x))
}

func (s *Server) accountQR(w http.ResponseWriter, r *http.Request) {
	x := s.accountByPath(w, r)
	if x == nil {
		return
	}
	code := ""
	x.mu.Lock()
	if x.linker != nil {
		code = x.linker.QR()
	}
	x.mu.Unlock()
	if x.tg != nil {
		code = x.tg.QRURL()
	}
	if code == "" {
		fail(w, 404, errors.New("no QR code right now"))
		return
	}
	png, err := qrcode.Encode(code, qrcode.Medium, 512)
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) accountPassword(w http.ResponseWriter, r *http.Request) {
	x := s.accountByPath(w, r)
	if x == nil {
		return
	}
	var in struct{ Password string }
	if err := readJSON(r, &in); err != nil || in.Password == "" || x.tg == nil {
		failCode(w, 400, "bad_request", errors.New("password is required (Telegram accounts only)"))
		return
	}
	if err := x.tg.SubmitPassword(in.Password); err != nil {
		failCode(w, 409, "conflict", err)
		return
	}
	time.Sleep(1500 * time.Millisecond)
	writeJSON(w, s.describeAccount(r.Context(), x))
}

func (s *Server) renameAccount(w http.ResponseWriter, r *http.Request) {
	x := s.accountByPath(w, r)
	if x == nil {
		return
	}
	var in struct{ Label string }
	if err := readJSON(r, &in); err != nil || !labelRE.MatchString(strings.TrimSpace(in.Label)) {
		failCode(w, 400, "bad_request", errors.New("give the account a short name (up to 40 letters)"))
		return
	}
	_ = s.DB.RenameAccount(r.Context(), x.ID, strings.TrimSpace(in.Label))
	x.mu.Lock()
	x.Label = strings.TrimSpace(in.Label)
	x.mu.Unlock()
	writeJSON(w, s.describeAccount(r.Context(), x))
}

func (s *Server) refreshAccount(w http.ResponseWriter, r *http.Request) {
	x := s.accountByPath(w, r)
	if x == nil {
		return
	}
	if !x.connectedNow() {
		failCode(w, 503, "unavailable", errors.New("this account isn't connected"))
		return
	}
	if err := s.syncAccount(r.Context(), x); err != nil {
		fail(w, 502, err)
		return
	}
	writeJSON(w, s.describeAccount(r.Context(), x))
}

func (x *extraAccount) connectedNow() bool {
	if x.tg != nil {
		return x.tg.Ready()
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.connected
}

// removeAccount logs the account out and forgets it. Posts that list its chats
// keep them; those sends fail with a clear reason until you change the post.
func (s *Server) removeAccount(w http.ResponseWriter, r *http.Request) {
	x := s.accountByPath(w, r)
	if x == nil {
		return
	}
	ctx := r.Context()
	if x.wa != nil {
		if x.wa.Store.ID != nil {
			_ = x.wa.Logout(ctx)
		}
		x.wa.Disconnect()
	}
	if x.tg != nil && x.tg.Ready() {
		_ = x.tg.Logout(ctx)
	}
	x.cancel()
	if err := s.DB.RemoveAccount(ctx, x.ID); err != nil {
		fail(w, 500, err)
		return
	}
	s.accts.mu.Lock()
	delete(s.accts.byID, x.ID)
	s.accts.mu.Unlock()
	_ = os.RemoveAll(s.accountDir(x.ID))
	s.DB.Log(ctx, actorOf(r), fmt.Sprintf("removed the %s account %q", platformName(x.Platform), x.Label))
	writeJSON(w, map[string]any{"ok": true, "changed": true})
}

// CloseAccounts disconnects every extra account (before a restart or exit).
func (s *Server) CloseAccounts() {
	for _, x := range s.extras() {
		if x.wa != nil {
			x.wa.Disconnect()
		}
		x.cancel()
	}
}
