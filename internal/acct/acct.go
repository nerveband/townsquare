// Package acct addresses chats across several linked accounts.
//
// Most people link one WhatsApp and one Telegram account; their chat ids stay
// exactly as the apps write them ("120363…@g.us", "tg:ch:…"). Chats of any
// additional account carry the account number in front:
//
//	wa@3:120363…@g.us   a chat of WhatsApp account 3
//	tg@4:ch:123:456     a chat of Telegram account 4 (the default one is tg:ch:123:456)
//
// Split turns either form into (account, the app's own id); Join does the reverse.
// Account 0 means the platform's default (first) account.
package acct

import (
	"strconv"
	"strings"
)

// Split returns the account number (0 = default) and the chat id the app uses.
func Split(jid string) (int64, string) {
	for _, p := range []string{"wa@", "tg@"} {
		if !strings.HasPrefix(jid, p) {
			continue
		}
		rest := jid[len(p):]
		num, raw, ok := strings.Cut(rest, ":")
		id, err := strconv.ParseInt(num, 10, 64)
		if !ok || err != nil || id <= 0 {
			return 0, jid
		}
		if p == "tg@" {
			return id, "tg:" + raw
		}
		return id, raw
	}
	return 0, jid
}

// Join addresses the app's chat id in an account (0 = default: unchanged).
func Join(account int64, raw string) string {
	if account <= 0 {
		return raw
	}
	if strings.HasPrefix(raw, "tg:") {
		return "tg@" + strconv.FormatInt(account, 10) + ":" + strings.TrimPrefix(raw, "tg:")
	}
	return "wa@" + strconv.FormatInt(account, 10) + ":" + raw
}

// Account is the account number of a chat id (0 = default).
func Account(jid string) int64 { id, _ := Split(jid); return id }

// Raw is the app's own chat id, without any account number.
func Raw(jid string) string { _, r := Split(jid); return r }
