package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"tailscale.com/tsnet"
)

// serveTailnet joins the tailnet as its own machine (e.g. "townsquare") so the app gets
// a dedicated https://townsquare.<tailnet>.ts.net address with an automatic certificate,
// without touching the host's own Tailscale Serve ports.
func serveTailnet(ctx context.Context, name, stateDir string, h http.Handler) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	ts := &tsnet.Server{
		Hostname: name,
		Dir:      stateDir,
		AuthKey:  os.Getenv("TS_AUTHKEY"), // optional; otherwise a login URL is printed once
		Logf:     func(string, ...any) {},
		UserLogf: log.Printf,
	}
	defer ts.Close()
	status, err := ts.Up(ctx)
	if err != nil {
		return fmt.Errorf("tailscale: %w", err)
	}
	domains := ts.CertDomains()
	if len(domains) == 0 {
		return fmt.Errorf("tailscale: HTTPS certificates are not enabled for this tailnet")
	}
	_ = status
	lc, err := ts.LocalClient()
	if err != nil {
		return err
	}
	ln, err := ts.Listen("tcp", ":443")
	if err != nil {
		return err
	}
	tlsLn := tls.NewListener(ln, &tls.Config{GetCertificate: lc.GetCertificate})
	// Plain http:// on the tailnet name redirects to https.
	if ln80, err := ts.Listen("tcp", ":80"); err == nil {
		go func() {
			_ = http.Serve(ln80, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "https://"+domains[0]+r.URL.RequestURI(), http.StatusMovedPermanently)
			}))
		}()
	}
	fmt.Printf("Townsquare is on your tailnet at https://%s\n", domains[0])
	hs := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = hs.Close() }()
	return hs.Serve(tlsLn)
}
