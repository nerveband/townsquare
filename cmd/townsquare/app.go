package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/server"
	"github.com/nerveband/townsquare/internal/store"
)

// localAddr turns a listen address into one this computer can open in a
// browser (0.0.0.0 and :8890 become 127.0.0.1).
func localAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// isAppLaunch reports a start with no command: opening Townsquare.app or
// double-clicking townsquare.exe. macOS may add a -psn_ argument.
func isAppLaunch(args []string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-psn_") {
			return false
		}
	}
	return true
}

// running reports whether Townsquare already answers at addr.
func running(addr string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/auth/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.Header.Get("X-Townsquare-Version") != ""
}

// openSignedIn opens the web app in the browser, already signed in (a one-time
// link: whoever can start the app on this computer is its owner).
func openSignedIn(ctx context.Context, dataDir, addr string) {
	url := "http://" + addr
	if db, err := store.Open(dataDir); err == nil {
		if link, err := server.LoginLink(ctx, db, url); err == nil {
			url = link
		}
		_ = db.Close()
	}
	if err := openBrowser(url); err != nil {
		fmt.Println("Open this in your browser:", url)
	}
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return fmt.Errorf("no display")
		}
		return exec.Command("xdg-open", url).Start()
	}
}
