// Command townsquare runs Townsquare: one calendar to schedule all your community posts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/nerveband/townsquare/internal/server"
	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/tgbot"
	"github.com/nerveband/townsquare/internal/update"
	"github.com/nerveband/townsquare/internal/version"
	"github.com/nerveband/townsquare/internal/wa"
	"github.com/nerveband/townsquare/web"
	"go.mau.fi/whatsmeow"
)

const usage = `townsquare: one calendar to schedule all your community posts

Usage:
  townsquare                  open the app: starts Townsquare and opens it in your browser
  townsquare serve   [--listen 100.x.y.z:8890]     web app, API and send loop
  townsquare pair    [--listen 100.x.y.z:8899] [--phone 15551234567]
  townsquare targets [--json]
  townsquare send    --to NAME|JID|status --kind text|image|video|voice|audio|document [--file PATH] [--text "caption or body"]
  townsquare status
  townsquare version
  townsquare login-link [--base URL]   one-time sign-in link for the web app
  townsquare apikey create NAME [--scope read|write|admin] | list | revoke ID
  townsquare allow [JID...]           show or extend the send allowlist (~/.townsquare/allow.txt)
  townsquare channel-create --name NAME [--desc TEXT]
  townsquare update [--check]          download the newest release (used on the next start)

Global flags (before the command):
  --data DIR   session directory (default ~/.townsquare)
  --log LEVEL  DEBUG, INFO, WARN, ERROR (default WARN)
`

func main() {
	home, _ := os.UserHomeDir()
	global := flag.NewFlagSet("townsquare", flag.ExitOnError)
	dataDir := global.String("data", filepath.Join(home, ".townsquare"), "session directory")
	logLevel := global.String("log", "WARN", "log level")
	global.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	var argv []string
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-psn_") { // macOS Finder adds this to app launches
			argv = append(argv, a)
		}
	}
	_ = global.Parse(argv)
	args := global.Args()

	// A downloaded update, if newer, runs instead of this binary (it never returns then).
	update.Handoff(*dataDir, version.Version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // SIGTERM: launchd/systemd stop
	defer stop()

	appMode := isAppLaunch(args)
	if appMode {
		if running(appAddr) {
			openSignedIn(ctx, *dataDir, appAddr)
			return
		}
		args = []string{"serve", "--listen", appAddr}
		if runtime.GOOS == "windows" {
			fmt.Println("Townsquare is starting. Keep this window open; close it to stop Townsquare.")
		}
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	case "version":
		fmt.Println("townsquare", version.String())
		return
	case "update":
		fs := flag.NewFlagSet("update", flag.ExitOnError)
		only := fs.Bool("check", false, "only check, don't download")
		_ = fs.Parse(args[1:])
		u := update.New(*dataDir, version.Version)
		m, err := u.Check(ctx)
		check(err)
		if !update.Newer(m.Version, version.Version) {
			fmt.Println("Townsquare", version.Version, "is up to date (newest release:", m.Version+")")
			return
		}
		if *only {
			fmt.Println("Townsquare", m.Version, "is available (you have", version.Version+"). Notes:", m.Notes)
			return
		}
		v, err := u.Download(ctx, m)
		check(err)
		fmt.Println("✓ downloaded Townsquare", v+". It's used from the next start; a running Townsquare switches to it on its own when no post is due.")
		return
	}

	cli, err := wa.Open(ctx, *dataDir, *logLevel)
	check(err)
	defer cli.Disconnect()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "pair":
		fs := flag.NewFlagSet("pair", flag.ExitOnError)
		listen := fs.String("listen", "127.0.0.1:8899", "address for the QR web page (empty to disable)")
		phone := fs.String("phone", "", "phone number with country code, digits only, for a pairing code")
		_ = fs.Parse(rest)
		check(wa.Pair(ctx, cli, wa.PairOptions{Listen: *listen, Phone: strings.TrimPrefix(*phone, "+")}))
		fmt.Println("Done. Try: townsquare targets")

	case "status":
		if cli.Store.ID == nil {
			fmt.Println("Not paired.")
			return
		}
		check(wa.ConnectPaired(ctx, cli))
		fmt.Println("Paired as", cli.Store.ID.String(), "· connected:", cli.IsConnected())

	case "targets":
		fs := flag.NewFlagSet("targets", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print JSON")
		_ = fs.Parse(rest)
		check(wa.ConnectPaired(ctx, cli))
		ts, err := wa.ListTargets(ctx, cli)
		check(err)
		saveTargets(*dataDir, ts)
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			check(enc.Encode(ts))
			return
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "KIND\tNAME\tCAN POST\tMEMBERS\tPARENT\tJID")
		for _, t := range ts {
			fmt.Fprintf(tw, "%s\t%s\t%v\t%d\t%s\t%s\n", t.Kind, t.Name, yes(t.CanSend), t.Members, t.Parent, t.JID)
		}
		check(tw.Flush())

	case "send":
		fs := flag.NewFlagSet("send", flag.ExitOnError)
		to := fs.String("to", "", "target name, JID, or status")
		kind := fs.String("kind", "text", "text, image, video, voice, audio, document")
		file := fs.String("file", "", "media file")
		text := fs.String("text", "", "message body or caption")
		_ = fs.Parse(rest)
		if *to == "" || (*kind == "text" && *text == "") || (*kind != "text" && *file == "") {
			fs.Usage()
			os.Exit(2)
		}
		check(wa.ConnectPaired(ctx, cli))
		ts := loadTargets(*dataDir)
		if ts == nil {
			ts, err = wa.ListTargets(ctx, cli)
			check(err)
			saveTargets(*dataDir, ts)
		}
		t, err := wa.Resolve(ts, *to)
		check(err)
		check(allowed(*dataDir, t))
		start := time.Now()
		id, err := wa.Send(ctx, cli, t, wa.Message{Kind: *kind, File: *file, Text: *text})
		check(err)
		fmt.Printf("✓ sent %s to %s (%s) · id %s · %s\n", *kind, t.Name, t.Kind, id, time.Since(start).Round(time.Millisecond))
		// Stay connected briefly so retry receipts from recipients can be answered.
		time.Sleep(3 * time.Second)

	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		listen := fs.String("listen", "127.0.0.1:8890", "address for the web app and API (empty to disable)")
		tsName := fs.String("tailscale", "", "also join the tailnet as this machine name and serve https://NAME.<tailnet>.ts.net")
		demo := fs.Bool("demo", false, "sample data, never connects to WhatsApp, never sends (uses <data>-demo)")
		_ = fs.Parse(rest)
		if *demo {
			*dataDir = strings.TrimSuffix(*dataDir, "/") + "-demo"
			cli.Disconnect()
			cli, err = wa.Open(ctx, *dataDir, *logLevel)
			check(err)
		}
		db, err := store.Open(*dataDir)
		check(err)
		ctx, cancelServe := context.WithCancel(ctx)
		defer cancelServe()
		restart := make(chan string, 1)
		srv := &server.Server{DB: db, WA: cli, DataDir: *dataDir, UI: web.FS(), Demo: *demo, Restart: restart, AppMode: appMode, Listen: *listen}
		if !*demo {
			srv.Updater = update.New(*dataDir, version.Version)
			go srv.RunUpdates(ctx)
		}
		if *demo {
			check(srv.SeedDemo(ctx))
			fmt.Println("Demo mode: sample data in", *dataDir, "· nothing is ever sent")
		} else {
			if err := srv.Connect(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "whatsapp:", err)
			}
			go srv.Run(ctx)
			go srv.RunStats(ctx)
			if t, err := tg.New(*dataDir); err != nil {
				fmt.Fprintln(os.Stderr, "telegram:", err)
			} else if t != nil {
				srv.TG = t
				go srv.RunTelegram(ctx)
			}
			if b, err := tgbot.New(*dataDir, srv.BotChat); err != nil {
				fmt.Fprintln(os.Stderr, "telegram bot:", err)
			} else if b != nil {
				srv.Bot = b
				go srv.RunBot(ctx)
			}
		}
		handler := srv.Handler()
		errs := make(chan error, 2)
		if *tsName != "" {
			go func() { errs <- serveTailnet(ctx, *tsName, filepath.Join(*dataDir, "tsnet"), handler) }()
		}
		if *listen != "" {
			hs := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
			go func() { <-ctx.Done(); _ = hs.Close() }()
			fmt.Printf("Townsquare is running at http://%s\n", *listen)
			go func() { errs <- hs.ListenAndServe() }()
			if appMode {
				go func() {
					for i := 0; i < 50 && !running(*listen); i++ {
						time.Sleep(200 * time.Millisecond)
					}
					openSignedIn(ctx, *dataDir, *listen)
				}()
			}
		}
		select {
		case err := <-errs:
			if err != nil && err != http.ErrServerClosed {
				check(err)
			}
		case <-ctx.Done():
		case why := <-restart:
			// Stop cleanly: listeners, send loops, WhatsApp, then the database.
			cancelServe()
			time.Sleep(time.Second)
			cli.Disconnect()
			_ = db.Close()
			if why == "update" || why == "reload" {
				fmt.Println("Restarting...")
				if err := update.Restart(*dataDir, version.Version); err != nil {
					fmt.Fprintln(os.Stderr, "update:", err)
					os.Exit(1) // a service manager starts it again (and Handoff picks the update)
				}
			}
			fmt.Println("Townsquare stopped.")
		}

	case "login-link":
		// townsquare login-link [--base http://host:port]: a one-time sign-in link for the web app
		fs := flag.NewFlagSet("login-link", flag.ExitOnError)
		base := fs.String("base", "http://127.0.0.1:8890", "address you open Townsquare at")
		_ = fs.Parse(rest)
		db, err := store.Open(*dataDir)
		check(err)
		link, err := server.LoginLink(ctx, db, *base)
		check(err)
		fmt.Println("Open this link within 15 minutes to sign in (works once):")
		fmt.Println(link)

	case "apikey":
		// townsquare apikey create NAME [--scope read|write|admin] | list | revoke ID
		db, err := store.Open(*dataDir)
		check(err)
		if len(rest) == 0 {
			rest = []string{"list"}
		}
		switch rest[0] {
		case "create":
			fs := flag.NewFlagSet("apikey create", flag.ExitOnError)
			scope := fs.String("scope", "write", "read, write or admin")
			if len(rest) < 2 {
				check(fmt.Errorf("usage: townsquare apikey create NAME [--scope write]"))
			}
			_ = fs.Parse(rest[2:])
			k, secret, err := db.CreateAPIKey(ctx, rest[1], *scope)
			check(err)
			db.Log(ctx, "you", fmt.Sprintf("created API key %s (%s)", k.Name, k.Scope))
			fmt.Printf("Created API key %q (%s). Store this secret now, it is not shown again:\n%s\n", k.Name, k.Scope, secret)
		case "revoke":
			if len(rest) < 2 {
				check(fmt.Errorf("usage: townsquare apikey revoke ID"))
			}
			id, _ := strconv.ParseInt(rest[1], 10, 64)
			check(db.RevokeAPIKey(ctx, id))
			fmt.Println("Revoked key", id)
		default:
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tSCOPE\tPREFIX\tLAST USED\tREVOKED")
			for _, k := range db.APIKeys(ctx) {
				used, rev := "never", ""
				if k.LastUsedAt > 0 {
					used = time.Unix(k.LastUsedAt, 0).Format("2006-01-02 15:04")
				}
				if k.RevokedAt > 0 {
					rev = time.Unix(k.RevokedAt, 0).Format("2006-01-02")
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s…\t%s\t%s\n", k.ID, k.Name, k.Scope, k.Prefix, used, rev)
			}
			check(tw.Flush())
		}

	case "allow":
		// townsquare allow JID [JID...]: add targets to the send allowlist
		if len(rest) == 0 {
			b, _ := os.ReadFile(filepath.Join(*dataDir, "allow.txt"))
			fmt.Print(string(b))
			return
		}
		f, err := os.OpenFile(filepath.Join(*dataDir, "allow.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		check(err)
		for _, j := range rest {
			fmt.Fprintln(f, strings.TrimSpace(j))
		}
		check(f.Close())

	case "channel-create":
		fs := flag.NewFlagSet("channel-create", flag.ExitOnError)
		name := fs.String("name", "", "channel name")
		desc := fs.String("desc", "", "description")
		_ = fs.Parse(rest)
		if *name == "" {
			fs.Usage()
			os.Exit(2)
		}
		check(wa.ConnectPaired(ctx, cli))
		meta, err := cli.CreateNewsletter(ctx, whatsmeow.CreateNewsletterParams{Name: *name, Description: *desc})
		check(err)
		fmt.Println("✓ created channel", meta.ThreadMeta.Name.Text, meta.ID.String())

	default:
		global.Usage()
		os.Exit(2)
	}
}

// allowed refuses any target not listed in DATA/allow.txt, so a typo can never reach a live group.
func allowed(dir string, t wa.Target) error {
	b, _ := os.ReadFile(filepath.Join(dir, "allow.txt"))
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == t.JID {
			return nil
		}
	}
	return fmt.Errorf("blocked: %s (%s) is not in %s. Add it with `townsquare allow %s` only if you mean it", t.Name, t.JID, filepath.Join(dir, "allow.txt"), t.JID)
}

func saveTargets(dir string, ts []wa.Target) {
	b, _ := json.MarshalIndent(ts, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "targets.json"), b, 0o600)
}

func loadTargets(dir string) []wa.Target {
	b, err := os.ReadFile(filepath.Join(dir, "targets.json"))
	if err != nil {
		return nil
	}
	var ts []wa.Target
	if json.Unmarshal(b, &ts) != nil {
		return nil
	}
	return ts
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
