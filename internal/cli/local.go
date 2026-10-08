package cli

// Local commands run on this computer and use its data folder directly (no
// server, no key). They are implemented in cmd/townsquare and declared here, so
// `schema` and help cover them with the same metadata as remote commands.

type localSpec struct {
	Name, Summary, Effects, Cardinality, OutputKind string
	Args                                            []Param
	Examples                                        []string
	Errors                                          []string
	Outcomes                                        bool
}

var localSpecs = map[string]localSpec{
	"serve": {Name: "serve", Summary: "Run the Townsquare server: web app, API and send loop.", Effects: "non_idempotent", OutputKind: "stream",
		Args: []Param{{Flag: "--listen", Type: "string", Desc: "Address for the web app and API (default from `townsquare config`)"},
			{Flag: "--tailscale", Type: "string", Desc: "Also join the tailnet as this machine name"},
			{Flag: "--demo", Type: "boolean", Desc: "Sample data in <data>-demo; never connects or sends"}},
		Examples: []string{"townsquare serve", "townsquare serve --demo --listen 127.0.0.1:8891"}, Errors: []string{"usage", "conflict", "general"}},
	"pair": {Name: "pair", Summary: "Link WhatsApp from a terminal (QR code, or a code for your phone number). Stop the server first.", Effects: "non_idempotent", OutputKind: "stream",
		Args:     []Param{{Flag: "--phone", Type: "string", Desc: "Your number with country code, for a typed pairing code"}, {Flag: "--listen", Type: "string", Desc: "Address for a QR web page"}},
		Examples: []string{"townsquare pair", "townsquare pair --phone 15551234567"}, Errors: []string{"usage", "conflict", "network"}},
	"version": {Name: "version", Summary: "Print the version.", Effects: "read_only", Cardinality: "single",
		Examples: []string{"townsquare version", "townsquare version -o json"}},
	"update": {Name: "update", Summary: "Download the newest signed release for this computer (used from the next start). Never runs on its own.", Effects: "idempotent", Cardinality: "single",
		Args:     []Param{{Flag: "--check", Type: "boolean", Desc: "Only check; download nothing"}},
		Examples: []string{"townsquare update --check", "townsquare update"}, Errors: []string{"network", "unavailable"}},
	"config": {Name: "config", Summary: "Show or set this computer's server address and tailnet name (config.json). Restart to apply.", Effects: "idempotent", Cardinality: "single",
		Args:     []Param{{Flag: "set", Type: "string[]", Desc: "KEY=VALUE pairs: listen, tailscale"}},
		Examples: []string{"townsquare config", "townsquare config set listen=127.0.0.1:8890 tailscale=townsquare"}, Errors: []string{"usage"}},
	"autostart": {Name: "autostart", Summary: "Start Townsquare when you log in (LaunchAgent, systemd user service or Run key): on, off, or show.", Effects: "idempotent", Cardinality: "single",
		Examples: []string{"townsquare autostart", "townsquare autostart on"}, Errors: []string{"usage", "general"}},
	"due": {Name: "due", Summary: "List sends due within a window either side of now. Exits 20 (outcome due_soon) when any are due.", Effects: "read_only", Cardinality: "bounded", Outcomes: true,
		Args:     []Param{{Flag: "--within", Type: "duration", Desc: "Window either side of now (default 15m)"}},
		Examples: []string{"townsquare due", "townsquare due --within 30m"}},
	"apikey": {Name: "apikey", Summary: "Create, list or revoke API keys in this computer's database (the first admin key comes from here).", Effects: "non_idempotent", Cardinality: "bounded",
		Args:     []Param{{Flag: "--scope", Type: "string", Enum: []string{"read", "write", "admin"}, Desc: "For create"}},
		Examples: []string{"townsquare apikey create isla-agent --scope write", "townsquare apikey list", "townsquare apikey revoke 5"}, Errors: []string{"usage", "not_found"}},
	"login-link": {Name: "login-link", Summary: "Print a one-time sign-in link for the web app (works once, 15 minutes).", Effects: "non_idempotent", Cardinality: "single",
		Args:     []Param{{Flag: "--base", Type: "url", Desc: "Address people open Townsquare at"}},
		Examples: []string{"townsquare login-link --base https://townsquare.example.ts.net"}},
}

// LocalHelp prints help for a local command; ok is false for unknown names.
func LocalHelp(name string) bool {
	s, ok := localSpecs[name]
	if !ok {
		return false
	}
	_ = localHelp(stdout(), s)
	return true
}
