package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrorKind is one of the fixed failure kinds, each with one exit code.
type ErrorKind struct {
	Kind        string `json:"kind"`
	ExitCode    int    `json:"exit_code"`
	Retryable   bool   `json:"retryable"`
	Description string `json:"description"`
}

// Kinds is the complete, declared error table (also in `townsquare schema`).
var Kinds = []ErrorKind{
	{"general", 1, false, "Unexpected failure inside Townsquare"},
	{"usage", 2, false, "Bad command, flag or value (nothing was sent)"},
	{"validation", 2, false, "The server rejected the input; the message says what to fix"},
	{"auth", 3, false, "No API key, or the key is unknown or revoked"},
	{"forbidden", 3, false, "The key's scope is too low for this command"},
	{"not_found", 4, false, "No such post, tag, chat or other record"},
	{"conflict", 5, false, "The request conflicts with the current state (for example nothing to undo)"},
	{"confirmation_required", 6, false, "A command that changes or sends things needs --yes without a terminal"},
	{"network", 7, true, "Townsquare can't be reached (wrong --url, or it isn't running)"},
	{"unavailable", 7, true, "Townsquare is up but WhatsApp or Telegram isn't connected, or the update server can't be reached"},
	{"rate_limit", 8, true, "Too many requests; wait for the given time"},
	{"busy", 9, true, "A post is due within 15 minutes, so a restart or update waits"},
	{"timeout", 10, true, "No answer in time (--timeout)"},
	{"uncertain_outcome", 11, false, "A request that creates or sends something timed out; it may or may not have happened"},
}

// Outcomes are non-error exits that report a data state.
var Outcomes = []struct {
	Code        int    `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}{
	{20, "due_soon", "`townsquare due`: a post is due within the window (the list is on stdout). Not an error."},
	{21, "unhealthy", "`townsquare doctor`: at least one check failed (the report is on stdout). Not an error."},
}

func kindByName(k string) ErrorKind {
	for _, e := range Kinds {
		if e.Kind == k {
			return e
		}
	}
	return Kinds[0]
}

// Error is a CLI failure with a kind.
type Error struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Hint    string         `json:"hint,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func errf(kind, hint, format string, a ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, a...), Hint: hint}
}

func kindForStatus(code string) string {
	switch code {
	case "400", "422":
		return "validation"
	case "401":
		return "auth"
	case "403":
		return "forbidden"
	case "404":
		return "not_found"
	case "409":
		return "conflict"
	case "429":
		return "rate_limit"
	case "502", "503":
		return "unavailable"
	}
	return ""
}

// fromHTTP turns an API error response into an Error.
func fromHTTP(status int, body []byte, retryAfter string) *Error {
	var env struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(body, &env)
	msg := env.Error
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	kind := kindForStatus(strconv.Itoa(status))
	if kind == "" {
		kind = "general"
	}
	e := &Error{Kind: kind, Message: msg, Details: map[string]any{"http_status": status}}
	if env.Code != "" {
		e.Details["api_code"] = env.Code
	}
	switch {
	case env.Code == "busy":
		e.Kind, e.Hint = "busy", "Wait until no post is due (see `townsquare system update get` → install_at), or pass --force-restart if the command offers it."
	case kind == "auth":
		e.Hint = "Run `townsquare auth status`. Save a key with `townsquare auth login --url URL < keyfile`, or `townsquare auth local` on the Townsquare computer."
	case kind == "forbidden":
		e.Hint = "Use a key with a higher scope (keys create --scope write|admin)."
	case kind == "not_found":
		e.Hint = "Check the id with the matching `list` command."
	case kind == "rate_limit":
		e.Hint = "Wait, then retry."
		if s, err := strconv.Atoi(retryAfter); err == nil {
			e.Details["retry_after_seconds"] = s
			e.Hint = fmt.Sprintf("Retry after %d seconds.", s)
		}
	case kind == "unavailable":
		e.Hint = "Check `townsquare status get` (connected) or `townsquare doctor`."
	case status >= 500:
		e.Hint = "Check the server log (~/.townsquare/serve.log) or run `townsquare doctor`."
	}
	return e
}

// asError converts any error into an *Error.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: "general", Message: err.Error()}
}

// Fail writes the error (JSON envelope as the last stderr line when the format
// is structured, a human message otherwise) and exits with its kind's code.
func Fail(err error, jsonErrors bool) {
	e := asError(err)
	k := kindByName(e.Kind)
	if jsonErrors {
		env := map[string]any{"kind": e.Kind, "message": e.Message, "retryable": k.Retryable, "exit_code": k.ExitCode}
		if e.Hint != "" {
			env["hint"] = e.Hint
		}
		if len(e.Details) > 0 {
			env["details"] = e.Details
		}
		b, _ := json.Marshal(map[string]any{"error": env})
		fmt.Fprintln(os.Stderr, string(b))
	} else {
		fmt.Fprintln(os.Stderr, "Error:", e.Message)
		if e.Hint != "" {
			fmt.Fprintln(os.Stderr, e.Hint)
		}
	}
	os.Exit(k.ExitCode)
}
