package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nerveband/townsquare/internal/acct"
	"github.com/nerveband/townsquare/internal/wa"
	"go.mau.fi/whatsmeow"
)

// WhatsApp lists (Business labels are lists now), Favorites and pins, read
// from WhatsApp so posts can go to a list you already keep there. Read-only:
// Townsquare never changes them in WhatsApp.

// listsSynced remembers which accounts had their full list resync since start.
var listsSynced sync.Map // account id -> struct{}

// onWAList stores one list, favorites or pin change from a WhatsApp event.
func (s *Server) onWAList(account int64, evt any) {
	c, ok := wa.ParseListEvent(evt)
	if !ok {
		return
	}
	ctx, gen := context.Background(), time.Now().UnixNano()
	var err error
	switch c.Op {
	case "list":
		err = s.DB.PutWAList(ctx, account, c.ListID, c.Name, c.Kind, c.Color, c.Order, c.Deleted, gen)
	case "member":
		err = s.DB.SetWAListChat(ctx, account, c.ListID, acct.Join(account, c.JID), c.In, gen)
	case "favorites":
		jids := make([]string, len(c.JIDs))
		for i, j := range c.JIDs {
			jids[i] = acct.Join(account, j)
		}
		err = s.DB.ReplaceWAList(ctx, account, c.ListID, c.Name, c.Kind, c.Order, jids, gen)
	case "pin":
		err = s.DB.SetPinned(ctx, acct.Join(account, c.JID), c.In)
	}
	if err != nil {
		log.Printf("whatsapp lists: %v", err)
	}
}

// syncLists reads every list again from WhatsApp: once per account after
// start (lists made before this version, or while it was off), and whenever
// chats are refreshed by hand (force).
func (s *Server) syncLists(ctx context.Context, cli *whatsmeow.Client, account int64, force bool) error {
	if s.Demo || cli == nil || cli.Store.ID == nil {
		return nil
	}
	if _, done := listsSynced.Load(account); done && !force {
		return nil
	}
	start := time.Now().UnixNano()
	if err := wa.ResyncLists(ctx, cli); err != nil {
		return err
	}
	listsSynced.Store(account, struct{}{})
	return s.DB.SweepWALists(ctx, account, start)
}

func (s *Server) whatsappLists(w http.ResponseWriter, r *http.Request) {
	ls, err := s.DB.WALists(r.Context())
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, ls)
}
