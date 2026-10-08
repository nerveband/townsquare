package acct

import "testing"

func TestSplitJoin(t *testing.T) {
	cases := []struct {
		jid string
		id  int64
		raw string
	}{
		{"120363000000000102@g.us", 0, "120363000000000102@g.us"},
		{"tg:ch:1:2", 0, "tg:ch:1:2"},
		{"wa@3:120363000000000102@g.us", 3, "120363000000000102@g.us"},
		{"tg@4:ch:1:2:t5", 4, "tg:ch:1:2:t5"},
		{"tg@4:self", 4, "tg:self"},
		{"wa@x:bad", 0, "wa@x:bad"},
		{"tgbot:55", 0, "tgbot:55"},
		{"status@broadcast", 0, "status@broadcast"},
	}
	for _, c := range cases {
		id, raw := Split(c.jid)
		if id != c.id || raw != c.raw {
			t.Errorf("Split(%q) = %d %q", c.jid, id, raw)
		}
		if got := Join(id, raw); got != c.jid {
			t.Errorf("Join(%d, %q) = %q, want %q", id, raw, got, c.jid)
		}
	}
}
