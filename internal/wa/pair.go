package wa

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

type pairState struct {
	mu       sync.Mutex
	State    string `json:"state"` // waiting, code, linked, error
	QR       string `json:"-"`
	PairCode string `json:"pair_code,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
	Message  string `json:"message,omitempty"`
	Version  int    `json:"version"`
}

func (p *pairState) set(f func(*pairState)) {
	p.mu.Lock()
	f(p)
	p.Version++
	p.mu.Unlock()
}

// PairOptions controls how pairing is offered.
type PairOptions struct {
	Listen string // address for the pairing web page, e.g. 100.x.y.z:8899. Empty disables it.
	Phone  string // optional phone number (digits, with country code) for a pairing code instead of QR
}

// LinkState is what a pairing screen shows.
type LinkState struct {
	State    string `json:"state"` // idle, waiting, code, linked, error
	PairCode string `json:"pair_code,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
	Message  string `json:"message,omitempty"`
	Version  int    `json:"version"`
}

// Linker links this device to a phone: it keeps fresh QR codes coming (and an
// optional phone-number pairing code) until the phone links or ctx ends.
type Linker struct {
	st     *pairState
	linked chan struct{}
	done   chan struct{}
}

// StartLink begins linking. Quiet suppresses terminal output (the web app uses it).
func StartLink(ctx context.Context, cli *whatsmeow.Client, phone string, quiet bool) (*Linker, error) {
	if cli.Store.ID != nil {
		return nil, fmt.Errorf("already linked as %s", cli.Store.ID.User)
	}
	l := &Linker{st: &pairState{State: "waiting"}, linked: make(chan struct{}, 1), done: make(chan struct{})}
	st := l.st
	say := func(a ...any) {
		if !quiet {
			fmt.Println(a...)
		}
	}
	hid := cli.AddEventHandler(func(evt any) {
		switch e := evt.(type) {
		case *events.PairSuccess:
			st.set(func(p *pairState) { p.State = "linked"; p.Message = "Linked as +" + e.ID.User })
			say("✓ Linked as", e.ID.String())
		case *events.Connected:
			if cli.Store.ID != nil {
				select {
				case l.linked <- struct{}{}:
				default:
				}
			}
		case *events.PairError:
			st.set(func(p *pairState) { p.State = "error"; p.Message = e.Error.Error() })
		}
	})

	// WhatsApp hands out about six QR codes per connection. Keep reconnecting
	// for fresh codes until the phone links or the context ends.
	go func() {
		defer close(l.done)
		defer cli.RemoveEventHandler(hid)
		for round := 0; ctx.Err() == nil && cli.Store.ID == nil; round++ {
			qrChan, err := cli.GetQRChannel(ctx)
			if err != nil {
				st.set(func(p *pairState) { p.State = "error"; p.Message = err.Error() })
				return
			}
			if err := cli.ConnectContext(ctx); err != nil {
				st.set(func(p *pairState) { p.State = "error"; p.Message = err.Error() })
				return
			}
			if phone != "" && round == 0 {
				code, err := cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Townsquare)")
				if err != nil {
					st.set(func(p *pairState) { p.Message = "pairing code: " + err.Error() })
					if !quiet {
						fmt.Fprintln(os.Stderr, "pairing code:", err)
					}
				} else {
					st.set(func(p *pairState) { p.PairCode = code })
					say("Pairing code:", code, "(WhatsApp > Linked devices > Link with phone number)")
				}
			}
			for item := range qrChan {
				switch item.Event {
				case whatsmeow.QRChannelEventCode:
					code, exp := item.Code, time.Now().Add(item.Timeout).Unix()
					st.set(func(p *pairState) { p.State = "code"; p.QR = code; p.Expires = exp })
					if round == 0 && !quiet {
						qrterminal.GenerateHalfBlock(code, qrterminal.L, os.Stdout)
					}
					say(fmt.Sprintf("QR code ready (refreshes in %s)", item.Timeout))
				case "success":
					return
				case "timeout":
					say("QR codes expired, getting new ones...")
				default:
					msg := item.Event
					if item.Error != nil {
						msg = item.Error.Error()
					}
					st.set(func(p *pairState) {
						if p.State != "linked" {
							p.State = "error"
							p.Message = msg
						}
					})
					return
				}
			}
			if cli.Store.ID != nil {
				return
			}
			cli.Disconnect()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return l, nil
}

// State returns the current pairing state.
func (l *Linker) State() LinkState {
	l.st.mu.Lock()
	defer l.st.mu.Unlock()
	return LinkState{State: l.st.State, PairCode: l.st.PairCode, Expires: l.st.Expires, Message: l.st.Message, Version: l.st.Version}
}

// QR returns the current QR code text ("" when none).
func (l *Linker) QR() string {
	l.st.mu.Lock()
	defer l.st.mu.Unlock()
	return l.st.QR
}

// Linked is closed-or-signalled once the phone has linked and the connection is up.
func (l *Linker) Linked() <-chan struct{} { return l.linked }

// Done is closed when the QR loop stops (linked, failed or cancelled).
func (l *Linker) Done() <-chan struct{} { return l.done }

// Pair links this device to a phone from the command line. It serves a live QR
// page, prints the QR to the terminal, and optionally requests an 8-character
// pairing code for phone-number linking.
func Pair(ctx context.Context, cli *whatsmeow.Client, opt PairOptions) error {
	if cli.Store.ID != nil {
		return fmt.Errorf("already paired as %s; delete the data dir to re-pair", cli.Store.ID)
	}
	l, err := StartLink(ctx, cli, opt.Phone, false)
	if err != nil {
		return err
	}
	if opt.Listen != "" {
		token := randHex(8)
		srv := &http.Server{Addr: opt.Listen, Handler: pairMux(l.st, token)}
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintln(os.Stderr, "pair page:", err)
			}
		}()
		defer srv.Close()
		fmt.Printf("\nOpen this page to scan the QR code:\n  http://%s/pair/%s\n\n", opt.Listen, token)
	}
	select {
	case <-l.Linked():
		// Give the phone a moment to deliver app-state keys (contacts, needed for Status).
		fmt.Println("Connected. Finishing initial setup (about 20s)...")
		time.Sleep(20 * time.Second)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func pairMux(st *pairState, token string) http.Handler {
	m := http.NewServeMux()
	base := "/pair/" + token
	m.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, pairPage)
	})
	m.HandleFunc(base+"/state", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(st)
	})
	m.HandleFunc(base+"/qr.png", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		code := st.QR
		st.mu.Unlock()
		if code == "" {
			http.Error(w, "no code yet", http.StatusNotFound)
			return
		}
		png, err := qrcode.Encode(code, qrcode.Medium, 512)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})
	return m
}

const pairPage = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Link Townsquare</title>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=Sofia+Sans+Extra+Condensed:wght@600&display=swap" rel="stylesheet">
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#EFEAE2;font:15px/1.5 Inter,system-ui,sans-serif;color:#111B21}
.c{background:#fff;border-radius:16px;padding:28px;width:min(92vw,420px);box-shadow:0 24px 60px -30px rgba(6,48,43,.4);text-align:center}
h1{font:600 40px/1 "Sofia Sans Extra Condensed",sans-serif;text-transform:uppercase;color:#075E54;margin:0 0 6px}
ol{text-align:left;color:#3B4A54;font-size:14px;padding-left:20px}
img{width:100%;max-width:320px;image-rendering:pixelated;border-radius:8px}
.bar{height:4px;background:#E4E0D8;border-radius:2px;overflow:hidden;margin:10px auto 0;max-width:320px}.bar i{display:block;height:100%;background:#25D366;transition:width 1s linear}
.code{font:600 34px/1 ui-monospace,monospace;letter-spacing:.12em;color:#075E54;margin:12px 0}
.ok{font-size:64px;color:#25D366}
small{color:#667781}
</style></head><body><div class="c">
<h1>Link Townsquare</h1>
<div id="v"><p>Starting…</p></div>
</div>
<script>
let ver=-1;
async function tick(){
 try{const s=await (await fetch(location.pathname+'/state',{cache:'no-store'})).json();
  if(s.version!==ver){ver=s.version;render(s)}
  if(s.state==='code'){const left=Math.max(0,s.expires-Date.now()/1000);const b=document.querySelector('.bar i');if(b)b.style.width=Math.min(100,left/20*100)+'%'}
 }catch(e){document.getElementById('v').innerHTML='<p>Pairing finished or stopped.</p>'}
 setTimeout(tick,1000)}
function render(s){const v=document.getElementById('v');
 if(s.state==='linked'){v.innerHTML='<div class="ok">✓</div><p><b>Linked.</b> You can close this page.</p><small>'+(s.message||'')+'</small>';return}
 if(s.state==='error'){v.innerHTML='<p><b>Pairing stopped.</b></p><small>'+s.message+'</small>';return}
 let h='<ol><li>Open WhatsApp on your phone</li><li>Settings → Linked devices → Link a device</li><li>Scan this code</li></ol>';
 if(s.state==='code')h+='<img alt="WhatsApp link QR code" src="'+location.pathname+'/qr.png?v='+s.version+'"><div class="bar"><i style="width:100%"></i></div><small>The code refreshes on its own.</small>';
 else h+='<p>Waiting for a code…</p>';
 if(s.pair_code)h+='<p style="margin-top:18px">Or choose <b>Link with phone number</b> and enter:</p><div class="code">'+s.pair_code+'</div>';
 v.innerHTML=h}
tick();
</script></body></html>`
