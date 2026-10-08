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

	"github.com/nerveband/townsquare/internal/appconfig"
	"github.com/nerveband/townsquare/internal/autostart"
	"github.com/nerveband/townsquare/internal/cli"
	"github.com/nerveband/townsquare/internal/server"
	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/tgbot"
	"github.com/nerveband/townsquare/internal/update"
	"github.com/nerveband/townsquare/internal/version"
	"github.com/nerveband/townsquare/internal/wa"
	"github.com/nerveband/townsquare/web"
	"golang.org/x/term"
)

func main() {
	home, _ := os.UserHomeDir()
	dataDir := new(string)
	logLevel := new(string)
	*dataDir, *logLevel = filepath.Join(home, ".townsquare"), "WARN"
	// --data and --log (local settings) may come anywhere before the command; every
	// other flag belongs to the CLI or the command.
	var args []string
	raw := os.Args[1:]
	for i := 0; i < len(raw); i++ {
		a := raw[i]
		if strings.HasPrefix(a, "-psn_") { // macOS Finder adds this to app launches
			continue
		}
		name, val, hasVal := strings.Cut(a, "=")
		if name == "--data" || name == "-data" || name == "--log" || name == "-log" {
			if !hasVal && i+1 < len(raw) {
				i++
				val = raw[i]
			}
			if strings.HasSuffix(name, "data") {
				*dataDir = val
			} else {
				*logLevel = val
			}
			continue
		}
		args = append(args, a)
	}

	appMode := isAppLaunch(args)
	word := cli.FirstWord(args)

	// A downloaded update, if newer, runs instead of this binary (it never returns then).
	update.Handoff(*dataDir, version.Version, appMode || word == "serve")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // SIGTERM: launchd/systemd stop
	defer stop()

	cfg, err := appconfig.Load(*dataDir)
	check(err)
	if !appMode && cli.Handles(args) {
		os.Exit(cli.Run(cli.Env{Version: version.Version, DataDir: *dataDir, LocalURL: "http://" + localAddr(cfg.ListenAddr()),
			MakeKey: func(name, scope string) (string, error) {
				db, err := store.Open(*dataDir)
				if err != nil {
					return "", err
				}
				defer db.Close()
				k, secret, err := db.CreateAPIKey(ctx, name, scope)
				if err == nil {
					db.Log(ctx, "you", fmt.Sprintf("created API key %s (%s) for the command line", k.Name, k.Scope))
				}
				return secret, err
			}}, args))
	}
	if appMode {
		if addr := localAddr(cfg.ListenAddr()); running(addr) {
			openSignedIn(ctx, *dataDir, addr)
			return
		}
		args = []string{"serve"}
		if runtime.GOOS == "windows" {
			fmt.Println("Townsquare is starting. Keep this window open; close it to stop Townsquare.")
		}
	}
	// Local commands: the word first, then its own flags.
	for i, a := range args {
		if a == word {
			args = args[i:]
			break
		}
	}
	for _, a := range args[1:] {
		if a == "-h" || a == "--help" {
			cli.LocalHelp(args[0])
			return
		}
	}
	jsonOut := !isTerminal(os.Stdout) || hasArg(args, "--json") || hasPair(args, "-o", "json") || hasPair(args, "--output", "json")
	if hasPair(args, "-o", "text") || hasPair(args, "--output", "text") {
		jsonOut = false
	}
	args = stripOutputFlags(args)
	dryRun := hasArg(args, "--dry-run")
	if dryRun {
		var keep []string
		for _, a := range args {
			if a != "--dry-run" {
				keep = append(keep, a)
			}
		}
		args = keep
	}
	preview := func(v map[string]any) {
		v["dry_run"], v["validated"], v["scope"] = true, "local", "local"
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}
	switch args[0] {
	case "version":
		if jsonOut {
			b, _ := json.Marshal(map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date})
			fmt.Println(string(b))
			return
		}
		fmt.Println("townsquare", version.String())
		return
	case "config":
		// townsquare config [get] | set KEY=VALUE...   (listen, tailscale; restart to apply)
		if len(args) >= 2 && args[1] == "set" {
			for _, kv := range args[2:] {
				k, v, ok := strings.Cut(kv, "=")
				switch {
				case !ok:
					check(usageErr("use KEY=VALUE, for example listen=0.0.0.0:8890"))
				case k == "listen":
					cfg.Listen = v
				case k == "tailscale":
					cfg.Tailscale = v
				default:
					check(usageErr(fmt.Sprintf("unknown key %q (listen, tailscale)", k)))
				}
			}
			check(appconfig.Save(*dataDir, cfg))
		}
		b, _ := json.MarshalIndent(map[string]string{"listen": cfg.ListenAddr(), "tailscale": cfg.Tailscale}, "", "  ")
		fmt.Println(string(b))
		return
	case "autostart":
		// townsquare autostart [on|off]: start Townsquare (serve) when you log in
		if len(args) >= 2 {
			switch args[1] {
			case "on":
				check(autostart.Enable(*dataDir))
			case "off":
				check(autostart.Disable())
			default:
				check(usageErr("use: townsquare autostart [on|off]"))
			}
		}
		if jsonOut {
			b, _ := json.Marshal(map[string]any{"enabled": autostart.Enabled(), "supported": autostart.Supported(), "program": autostart.Installed()})
			fmt.Println(string(b))
			return
		}
		fmt.Printf("start at login: %v (program: %s)\n", autostart.Enabled(), autostart.Installed())
		return
	case "due":
		// Used by scripts/deploy.sh: never restart Townsquare right around a send.
		fs := flag.NewFlagSet("due", flag.ExitOnError)
		within := fs.Duration("within", 15*time.Minute, "window either side of now")
		_ = fs.Parse(args[1:])
		db, err := store.Open(*dataDir)
		check(err)
		posts, err := db.Posts(ctx, "scheduled")
		check(err)
		now := time.Now()
		due := store.Expand(posts, now.Add(-*within), now.Add(*within))
		if jsonOut {
			items := []map[string]any{}
			for _, o := range due {
				items = append(items, map[string]any{"post_id": o.PostID, "at": o.At, "chats": len(o.Targets), "schedule_id": o.ScheduleID, "occ": o.Occ})
			}
			b, _ := json.Marshal(map[string]any{"items": items, "total": len(items), "within": within.String()})
			fmt.Println(string(b))
		} else {
			for _, o := range due {
				fmt.Printf("post %d due %s (%d chats)\n", o.PostID, o.At.Local().Format("15:04"), len(o.Targets))
			}
			if len(due) == 0 {
				fmt.Println("nothing due within", *within)
			}
		}
		if len(due) > 0 {
			os.Exit(20) // outcome due_soon (not an error)
		}
		return
	case "update":
		fs := flag.NewFlagSet("update", flag.ExitOnError)
		only := fs.Bool("check", false, "only check, don't download")
		_ = fs.Parse(args[1:])
		u := update.New(*dataDir, version.Version)
		m, err := u.Check(ctx)
		check(err)
		res := map[string]any{"current": version.Version, "latest": m.Version, "available": update.Newer(m.Version, version.Version), "notes": m.Notes, "changed": false}
		if res["available"] == true && !*only {
			v, err := u.Download(ctx, m)
			check(err)
			res["downloaded"], res["changed"] = v, true
		}
		if jsonOut {
			b, _ := json.Marshal(res)
			fmt.Println(string(b))
			return
		}
		switch {
		case res["available"] != true:
			fmt.Println("Townsquare", version.Version, "is up to date (newest release:", m.Version+")")
		case *only:
			fmt.Println("Townsquare", m.Version, "is available (you have", version.Version+"). Notes:", m.Notes)
		default:
			fmt.Println("✓ downloaded Townsquare", m.Version+". It's used from the next start; a running Townsquare switches to it on its own when no post is due.")
		}
		return
	}

	waCli, err := wa.Open(ctx, *dataDir, *logLevel)
	check(err)
	defer waCli.Disconnect()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "pair":
		fs := flag.NewFlagSet("pair", flag.ExitOnError)
		listen := fs.String("listen", "127.0.0.1:8899", "address for the QR web page (empty to disable)")
		phone := fs.String("phone", "", "phone number with country code, digits only, for a pairing code")
		_ = fs.Parse(rest)
		if dryRun {
			preview(map[string]any{"action": "link WhatsApp to this computer", "already_linked": waCli.Store.ID != nil, "qr_page": *listen, "phone_code": *phone != ""})
			return
		}
		check(wa.Pair(ctx, waCli, wa.PairOptions{Listen: *listen, Phone: strings.TrimPrefix(*phone, "+")}))
		fmt.Println("Done. Start Townsquare (townsquare serve), then: townsquare targets list")

	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		listen := fs.String("listen", cfg.ListenAddr(), "address for the web app and API (empty to disable; default from `townsquare config`)")
		tsName := fs.String("tailscale", cfg.Tailscale, "also join the tailnet as this machine name and serve https://NAME.<tailnet>.ts.net")
		demo := fs.Bool("demo", false, "sample data, never connects to WhatsApp, never sends (uses <data>-demo)")
		_ = fs.Parse(rest)
		if dryRun {
			ok, _ := appconfig.Lock(*dataDir)
			appconfig.Unlock()
			preview(map[string]any{"action": "run the Townsquare server", "listen": *listen, "tailscale": *tsName, "demo": *demo,
				"data_dir": *dataDir, "another_server_running": !ok, "whatsapp_linked": waCli.Store.ID != nil})
			return
		}
		if *demo {
			*dataDir = strings.TrimSuffix(*dataDir, "/") + "-demo"
			waCli.Disconnect()
			waCli, err = wa.Open(ctx, *dataDir, *logLevel)
			check(err)
		}
		// One server per data folder: two would both send every post.
		if ok, err := appconfig.Lock(*dataDir); err != nil || !ok {
			if appMode {
				openSignedIn(ctx, *dataDir, localAddr(*listen))
				return
			}
			check(fmt.Errorf("another Townsquare is already running with %s (stop it first)", *dataDir))
		}
		db, err := store.Open(*dataDir)
		check(err)
		ctx, cancelServe := context.WithCancel(ctx)
		defer cancelServe()
		restart := make(chan string, 1)
		srv := &server.Server{DB: db, WA: waCli, DataDir: *dataDir, UI: web.FS(), Demo: *demo, Restart: restart, AppMode: appMode, Listen: *listen, Tailnet: *tsName}
		if !*demo {
			srv.Updater = update.New(*dataDir, version.Version)
			go srv.RunUpdates(ctx)
			go func() { // a version that serves for 2 minutes is good; keep using it
				select {
				case <-ctx.Done():
				case <-time.After(2 * time.Minute):
					update.MarkHealthy(*dataDir, version.Version)
				}
			}()
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
					addr := localAddr(*listen)
					for i := 0; i < 50 && !running(addr); i++ {
						time.Sleep(200 * time.Millisecond)
					}
					openSignedIn(ctx, *dataDir, addr)
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
			waCli.Disconnect()
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
		if dryRun {
			preview(map[string]any{"action": "make a one-time sign-in link", "base": *base})
			return
		}
		db, err := store.Open(*dataDir)
		check(err)
		link, err := server.LoginLink(ctx, db, *base)
		check(err)
		if jsonOut {
			b, _ := json.Marshal(map[string]any{"link": link, "expires_at": time.Now().Add(15 * time.Minute).Unix()})
			fmt.Println(string(b))
			return
		}
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
				check(usageErr("usage: townsquare apikey create NAME [--scope write]"))
			}
			_ = fs.Parse(rest[2:])
			if dryRun {
				preview(map[string]any{"action": "create API key", "name": rest[1], "key_scope": *scope})
				return
			}
			k, secret, err := db.CreateAPIKey(ctx, rest[1], *scope)
			check(err)
			db.Log(ctx, "you", fmt.Sprintf("created API key %s (%s)", k.Name, k.Scope))
			if jsonOut {
				b, _ := json.Marshal(map[string]any{"id": k.ID, "name": k.Name, "scope": k.Scope, "secret": secret, "note": "shown once; store it now"})
				fmt.Println(string(b))
				return
			}
			fmt.Printf("Created API key %q (%s). Store this secret now, it is not shown again:\n%s\n", k.Name, k.Scope, secret)
		case "revoke":
			if len(rest) < 2 {
				check(usageErr("usage: townsquare apikey revoke ID"))
			}
			id, err := strconv.ParseInt(rest[1], 10, 64)
			if err != nil {
				check(usageErr("the key id is a number (townsquare apikey list)"))
			}
			if dryRun {
				preview(map[string]any{"action": "revoke API key", "id": id, "reversible": false})
				return
			}
			check(db.RevokeAPIKey(ctx, id))
			if jsonOut {
				fmt.Printf("{\"revoked\":%d,\"changed\":true}\n", id)
				return
			}
			fmt.Println("Revoked key", id)
		default:
			if jsonOut {
				b, _ := json.Marshal(map[string]any{"items": db.APIKeys(ctx), "total": len(db.APIKeys(ctx))})
				fmt.Println(string(b))
				return
			}
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

	default:
		check(usageErr("unknown command " + args[0]))
	}
}

func check(err error) {
	if err != nil {
		cli.Fail(err, !isTerminal(os.Stdout))
	}
}

func usageErr(msg string) error { return cli.Usage(msg) }

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func stripOutputFlags(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json", strings.HasPrefix(a, "--output="), strings.HasPrefix(a, "-o="):
			continue
		case (a == "-o" || a == "--output") && i+1 < len(args):
			i++
			continue
		}
		out = append(out, a)
	}
	return out
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

func hasPair(args []string, k, v string) bool {
	for i, x := range args {
		if (x == k && i+1 < len(args) && args[i+1] == v) || x == k+"="+v {
			return true
		}
	}
	return false
}
