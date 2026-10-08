// Package tg connects Townsquare to Telegram as the user's own account (gotd/td,
// the official MTProto API), the way internal/wa links WhatsApp. It logs in by QR
// code, lists the groups and channels the user can post in, and sends posts.
package tg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// State is what the login screen shows.
type State struct {
	Configured bool   `json:"configured"`           // an app id is available
	AppSource  string `json:"app_source,omitempty"` // own, shared or built-in
	Status     string `json:"status"`               // off, starting, logged_out, qr, password, ready, error
	QRURL      string `json:"-"`
	QRExpires  int64  `json:"qr_expires,omitempty"`
	User       string `json:"user,omitempty"`
	Username   string `json:"username,omitempty"`
	Error      string `json:"error,omitempty"`
	Version    int    `json:"version"`
}

// Client is one logged-in (or logging-in) Telegram account.
type Client struct {
	dataDir  string
	appID    int
	appHash  string
	source   string // own, shared or built-in
	c        *telegram.Client
	loggedIn qrlogin.LoggedIn

	mu       sync.Mutex
	state    State
	api      *tgapi.Client
	runCtx   context.Context
	pwWait   chan string
	loginRun bool
}

// New returns nil, nil when no Telegram app id is available (see ResolveApp).
func New(dataDir string) (*Client, error) { return NewIn(dataDir, dataDir) }

// NewIn uses the app id from dataDir and keeps the login session in sessionDir
// (an extra account's own folder).
func NewIn(dataDir, sessionDir string) (*Client, error) {
	app, source, err := ResolveApp(dataDir)
	if err != nil {
		return nil, err
	}
	if !app.Valid() {
		return nil, nil
	}
	id := app.ID
	lines := []string{"", app.Hash}
	cl := &Client{dataDir: sessionDir, appID: id, appHash: strings.TrimSpace(lines[1]), source: source, state: State{Configured: true, Status: "starting", AppSource: source}}
	d := tgapi.NewUpdateDispatcher()
	cl.loggedIn = qrlogin.OnLoginToken(d)
	cl.c = telegram.NewClient(id, cl.appHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: filepath.Join(sessionDir, "telegram.session")},
		UpdateHandler:  d,
		Device: telegram.DeviceConfig{
			DeviceModel: "Townsquare", SystemVersion: "server", AppVersion: "1.0",
		},
	})
	return cl, nil
}

func (cl *Client) set(f func(*State)) {
	cl.mu.Lock()
	f(&cl.state)
	cl.state.Version++
	cl.mu.Unlock()
}

// State returns a copy of the login state.
func (cl *Client) State() State {
	if cl == nil {
		return State{Status: "off"}
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.state
}

// QRURL is the tg://login link to render as a QR code.
func (cl *Client) QRURL() string {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.state.QRURL
}

// Ready reports whether the account is logged in and connected.
func (cl *Client) Ready() bool { return cl != nil && cl.State().Status == "ready" }

// Run keeps the Telegram connection up until ctx ends.
func (cl *Client) Run(ctx context.Context) {
	for ctx.Err() == nil {
		err := cl.c.Run(ctx, func(ctx context.Context) error {
			cl.mu.Lock()
			cl.api = cl.c.API()
			cl.runCtx = ctx
			cl.mu.Unlock()
			st, err := cl.c.Auth().Status(ctx)
			if err != nil {
				return err
			}
			if st.Authorized {
				cl.markReady(ctx)
			} else {
				cl.set(func(s *State) { s.Status = "logged_out"; s.User, s.Username = "", "" })
			}
			<-ctx.Done()
			return ctx.Err()
		})
		if ctx.Err() != nil {
			return
		}
		cl.set(func(s *State) { s.Status = "error"; s.Error = fmt.Sprint(err) })
		time.Sleep(10 * time.Second)
	}
}

func (cl *Client) markReady(ctx context.Context) {
	name, user := "", ""
	if me, err := cl.c.Self(ctx); err == nil {
		name = strings.TrimSpace(me.FirstName + " " + me.LastName)
		user = me.Username
	}
	cl.set(func(s *State) { s.Status = "ready"; s.QRURL = ""; s.Error = ""; s.User = name; s.Username = user })
}

// StartQR begins a QR login. Scan it in Telegram: Settings > Devices > Link Desktop Device.
func (cl *Client) StartQR() error {
	cl.mu.Lock()
	ctx := cl.runCtx
	busy := cl.loginRun
	if !busy {
		cl.loginRun = true
	}
	cl.mu.Unlock()
	if ctx == nil {
		return errors.New("Telegram is still connecting; try again in a few seconds")
	}
	if busy {
		return nil
	}
	go func() {
		defer func() { cl.mu.Lock(); cl.loginRun = false; cl.mu.Unlock() }()
		_, err := cl.c.QR().Auth(ctx, cl.loggedIn, func(_ context.Context, t qrlogin.Token) error {
			cl.set(func(s *State) { s.Status = "qr"; s.QRURL = t.URL(); s.QRExpires = t.Expires().Unix(); s.Error = "" })
			return nil
		})
		if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
			cl.set(func(s *State) { s.Status = "password"; s.QRURL = "" })
			pw := make(chan string, 1)
			cl.mu.Lock()
			cl.pwWait = pw
			cl.mu.Unlock()
			for {
				select {
				case <-ctx.Done():
					return
				case p := <-pw:
					if _, err := cl.c.Auth().Password(ctx, p); err != nil {
						cl.set(func(s *State) { s.Status = "password"; s.Error = "That password didn't work. Try again." })
						continue
					}
					cl.markReady(ctx)
					return
				}
			}
		}
		if err != nil {
			cl.set(func(s *State) { s.Status = "logged_out"; s.QRURL = ""; s.Error = err.Error() })
			return
		}
		cl.markReady(ctx)
	}()
	return nil
}

// SubmitPassword finishes a login for accounts with two-step verification.
func (cl *Client) SubmitPassword(p string) error {
	cl.mu.Lock()
	ch := cl.pwWait
	cl.mu.Unlock()
	if ch == nil || cl.State().Status != "password" {
		return errors.New("no login is waiting for a password")
	}
	cl.set(func(s *State) { s.Error = "" })
	ch <- p
	return nil
}

// Logout signs this device out of the Telegram account.
func (cl *Client) Logout(ctx context.Context) error {
	api := cl.API()
	if api == nil {
		return errors.New("not connected")
	}
	_, err := api.AuthLogOut(ctx)
	_ = os.Remove(filepath.Join(cl.dataDir, "telegram.session"))
	cl.set(func(s *State) { s.Status = "logged_out"; s.User, s.Username = "", "" })
	return err
}

// API is the raw Telegram API client while connected.
func (cl *Client) API() *tgapi.Client {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.api
}

// Source is where the app id came from: own, shared or built-in.
func (cl *Client) Source() string {
	if cl == nil {
		return ""
	}
	return cl.source
}

// UsesApp reports whether the client runs with app id a.
func (cl *Client) UsesApp(a App) bool { return cl != nil && cl.appID == a.ID && cl.appHash == a.Hash }
