package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

func stdout() io.Writer { return os.Stdout }

var groupDesc = map[string]string{
	"posts": "Create, schedule, edit, pause and send posts", "sends": "Single sends on the calendar: list, move, copy, skip",
	"targets": "Groups, channels, Status and chats (and the allowlist)", "tags": "Color labels for posts", "clients": "Group posts and chats by client",
	"sets": "Saved lists of chats", "media": "Upload photos, videos, voice notes and files", "history": "Who changed what; undo and redo",
	"settings": "Time zone, safe mode, pacing, quiet hours", "status": "Connection, safe mode and today's count", "stats": "Reach, reads, reactions and replies",
	"telegram": "Link Telegram, the bot and the app id", "whatsapp": "Link or unlink WhatsApp; create channels", "system": "Updates, restart, start at login, server address",
	"keys": "API keys", "sessions": "Signed-in browsers", "signin-links": "One-time sign-in links for people", "test": "Send a test to yourself",
	"changelog": "Release notes",
}

func rootHelp(w io.Writer) error {
	cs, err := Commands()
	if err != nil {
		return err
	}
	groups := map[string]bool{}
	for _, c := range cs {
		groups[c.Words[0]] = true
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "townsquare %s: one calendar to schedule all your community posts.\n\n", env.Version)
	fmt.Fprintln(w, "Usage: townsquare [global flags] <resource> <verb> [id] [flags]")
	fmt.Fprintln(w, "       townsquare <resource>            (lists its verbs)")
	fmt.Fprint(w, "       townsquare <resource> <verb> -h  (flags and examples)\n\n")
	fmt.Fprintln(w, "Talk to a Townsquare server:")
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, g := range names {
		fmt.Fprintf(tw, "  %s\t%s\n", g, groupDesc[g])
	}
	tw.Flush()
	fmt.Fprintln(w, "\nSet up and check:")
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, b := range builtinOrder {
		fmt.Fprintf(tw, "  %s\t%s\n", b, builtinDesc[b])
	}
	tw.Flush()
	fmt.Fprintln(w, "\nOn this computer (no server or key needed):")
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, n := range []string{"serve", "pair", "config", "autostart", "due", "apikey", "login-link", "update", "version"} {
		fmt.Fprintf(tw, "  %s\t%s\n", n, localSpecs[n].Summary)
	}
	tw.Flush()
	fmt.Fprintln(w, "\nGlobal flags (before or after the command):")
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, g := range Globals {
		n := g.Name
		if g.Short != "" {
			n = g.Short + ", " + n
		}
		fmt.Fprintf(tw, "  %s\t%s\n", n, g.Desc)
	}
	tw.Flush()
	fmt.Fprintln(w, "\nMachine-readable: `townsquare schema` (every command as JSON, no key or network needed).")
	fmt.Fprintln(w, "Agents: `townsquare skills show` prints the agent guide. Start with `townsquare doctor`.")
	fmt.Fprintln(w, "\nExit codes: 0 ok, 1 general, 2 usage/validation, 3 auth/forbidden, 4 not found, 5 conflict,")
	fmt.Fprintln(w, "6 confirmation required (pass --yes), 7 network/unavailable, 8 rate limited, 9 busy (a post is due),")
	fmt.Fprintln(w, "10 timeout, 11 uncertain outcome (retry with the same --idempotency-key). `due` exits 20 when posts are due.")
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  townsquare doctor")
	fmt.Fprintln(w, "  townsquare posts list --status scheduled --limit 10 --fields id,title,next_at")
	fmt.Fprintln(w, "  townsquare posts create --title 'Family dinner' --caption @dinner.txt --targets 'Main Group' --send-at 2026-10-15T18:30 --dry-run")
	return nil
}

func groupHelp(w io.Writer, group string, cs []*Command) error {
	fmt.Fprintf(w, "townsquare %s: %s\n\n", group, groupDesc[strings.Fields(group)[0]])
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, c := range cs {
		fmt.Fprintf(tw, "  %s\t%s\t[%s]\n", c.Name, c.Summary, c.Effects)
	}
	tw.Flush()
	fmt.Fprintf(w, "\nMore: townsquare %s <verb> --help\n", group)
	return nil
}

func flagLine(p Param) string {
	t := p.Type
	if t == "array" {
		t = p.Items + " list"
	}
	s := fmt.Sprintf("  %s %s\t", p.Flag, strings.ToUpper(strings.ReplaceAll(t, " ", "-")))
	if p.Required {
		s += "(required) "
	}
	s += p.Desc
	if len(p.Enum) > 0 {
		s += " One of: " + strings.Join(p.Enum, ", ") + "."
	}
	if p.Secret {
		s += " Secret: pass @file or @- (stdin)."
	}
	return s
}

func commandHelp(w io.Writer, c *Command) error {
	usage := "townsquare " + c.Name
	if c.Positional != nil {
		usage += " " + strings.ToUpper(c.Positional.Name)
	}
	fmt.Fprintf(w, "%s\n\n%s.", usage+" [flags]", strings.TrimSuffix(c.Summary, "."))
	if c.Description != "" {
		fmt.Fprintf(w, " %s", c.Description)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "\nEffects: %s", c.Effects)
	if c.Confirm {
		fmt.Fprint(w, " · needs --yes without a terminal (preview with --dry-run)")
	}
	if c.Effects == "non_idempotent" {
		fmt.Fprint(w, " · retry safely with --idempotency-key")
	}
	if c.OutputKind == "opaque" {
		fmt.Fprintf(w, " · output: %s (use --deliver file:PATH)", c.MediaType)
	} else if c.Cardinality == "unbounded" {
		fmt.Fprint(w, " · paged: --limit, --offset; also --fields, --id-only, --count")
	}
	fmt.Fprintf(w, "\nAPI: %s /api/v1%s\n", c.Method, c.Path)
	var ps []Param
	ps = append(ps, c.Query...)
	ps = append(ps, c.Body...)
	if len(ps) > 0 || c.Multipart {
		fmt.Fprintln(w, "\nFlags:")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, p := range ps {
			if p.Name == "limit" || p.Name == "offset" {
				continue
			}
			fmt.Fprintln(tw, flagLine(p))
		}
		if c.Multipart {
			fmt.Fprintln(tw, "  --file PATH\tUpload this file (multipart)")
		}
		if c.HasBody {
			fmt.Fprintln(tw, "  --body JSON\tThe whole request as JSON, @file.json or - (stdin); see --request-schema")
		}
		tw.Flush()
		fmt.Fprintln(w, "Values can be @file, @- (stdin), @base64:file, or @@text for a literal starting with @.")
	}
	if len(c.Examples) > 0 {
		fmt.Fprintln(w, "\nExamples:")
		for _, e := range c.Examples {
			fmt.Fprintln(w, "  "+e)
		}
	}
	fmt.Fprintln(w, "\nGlobal flags: townsquare --help · Errors and exit codes: townsquare schema "+c.Name)
	return nil
}

func localHelp(w io.Writer, s localSpec) error {
	fmt.Fprintf(w, "townsquare %s [flags]\n\n%s\n\nRuns on this computer: no server or key needed. Effects: %s\n", s.Name, s.Summary, s.Effects)
	if len(s.Args) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for _, p := range s.Args {
			fmt.Fprintln(tw, flagLine(p))
		}
		tw.Flush()
	}
	fmt.Fprintln(w, "\nGlobal: --data DIR (default ~/.townsquare), --log LEVEL")
	fmt.Fprintln(w, "\nExamples:")
	for _, e := range s.Examples {
		fmt.Fprintln(w, "  "+e)
	}
	return nil
}
