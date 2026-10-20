package server

import (
	"context"
	"testing"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestWhatsAppListsFromEvents(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{DB: db}
	g1, g2, g3 := "120363000000000101@g.us", "120363000000000102@g.us", "120363000000000103@g.us"
	_ = db.UpsertTargets(ctx, "whatsapp", []store.Target{{JID: g1, Kind: "group", Name: "A"}, {JID: g2, Kind: "group", Name: "B"}})
	_ = db.UpsertAccountTargets(ctx, "whatsapp", 2, []store.Target{{JID: "wa@2:" + g3, Kind: "group", Name: "C"}})

	label := func(id, name string, deleted bool) *events.LabelEdit {
		return &events.LabelEdit{LabelID: id, Action: &waSyncAction.LabelEditAction{Name: proto.String(name), Type: waSyncAction.LabelEditAction_CUSTOM.Enum(), Deleted: proto.Bool(deleted)}}
	}
	member := func(id, jid string, in bool) *events.LabelAssociationChat {
		j, _ := types.ParseJID(jid)
		return &events.LabelAssociationChat{JID: j, LabelID: id, Action: &waSyncAction.LabelAssociationAction{Labeled: proto.Bool(in)}}
	}
	lists := func() map[string][]string {
		ls, err := db.WALists(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string][]string{}
		for _, l := range ls {
			out[l.Name] = l.JIDs
		}
		return out
	}

	s.onWAList(0, label("1", "Volunteers", false))
	s.onWAList(0, member("1", g1, true))
	s.onWAList(0, member("1", g2, true))
	s.onWAList(0, member("1", "15550000000@s.whatsapp.net", true)) // a person, not a Townsquare chat
	s.onWAList(2, label("1", "Second phone list", false))          // same id, other account
	s.onWAList(2, member("1", g3, true))
	if got := lists(); len(got["Volunteers"]) != 2 || len(got["Second phone list"]) != 1 || got["Second phone list"][0] != "wa@2:"+g3 {
		t.Fatalf("lists: %v", got)
	}

	s.onWAList(0, member("1", g2, false))
	if got := lists()["Volunteers"]; len(got) != 1 || got[0] != g1 {
		t.Fatalf("removing a chat: %v", got)
	}

	// A chat that left Townsquare's chats drops out of the list.
	_ = db.UpsertTargets(ctx, "whatsapp", []store.Target{{JID: g2, Kind: "group", Name: "B"}})
	if got := lists()["Volunteers"]; len(got) != 0 {
		t.Fatalf("gone chat still listed: %v", got)
	}
	_ = db.UpsertTargets(ctx, "whatsapp", []store.Target{{JID: g1, Kind: "group", Name: "A"}, {JID: g2, Kind: "group", Name: "B"}})

	// Favorites come whole; pins mark the chat.
	fav := func(jids ...string) *events.AppState {
		f := &waSyncAction.FavoritesAction{}
		for _, j := range jids {
			f.Favorites = append(f.Favorites, &waSyncAction.FavoritesAction_Favorite{ID: proto.String(j)})
		}
		return &events.AppState{Index: []string{appstate.IndexFavorites}, SyncActionValue: &waSyncAction.SyncActionValue{FavoritesAction: f}}
	}
	s.onWAList(0, fav(g1, g2))
	s.onWAList(0, fav(g2))
	if got := lists()["Favorites"]; len(got) != 1 || got[0] != g2 {
		t.Fatalf("favorites replaced: %v", got)
	}
	// WhatsApp may also send a Favorites-type list: still one Favorites, with both sets of chats.
	s.onWAList(0, &events.LabelEdit{LabelID: "9", Action: &waSyncAction.LabelEditAction{Name: proto.String("Favourites"), Type: waSyncAction.LabelEditAction_FAVORITES.Enum()}})
	s.onWAList(0, member("9", g1, true))
	if ls, _ := db.WALists(ctx); func() int {
		n := 0
		for _, l := range ls {
			if l.Kind == "favorites" {
				n++
				if l.ID != "favorites" || len(l.JIDs) != 2 {
					t.Fatalf("merged favorites: %+v", l)
				}
			}
		}
		return n
	}() != 1 {
		t.Fatalf("favorites shown more than once: %+v", ls)
	}
	s.onWAList(0, label("9", "Favourites", true))
	gj, _ := types.ParseJID(g2)
	s.onWAList(0, &events.Pin{JID: gj, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}})
	for _, tg := range db.Targets(ctx) {
		if tg.Pinned != (tg.JID == g2) {
			t.Fatalf("pinned %s = %v", tg.JID, tg.Pinned)
		}
	}

	// A full resync drops lists WhatsApp no longer sends, only for that account.
	start := time.Now().UnixNano()
	s.onWAList(0, label("1", "Volunteers", false))
	s.onWAList(0, member("1", g1, true))
	if err := db.SweepWALists(ctx, 0, start); err != nil {
		t.Fatal(err)
	}
	got := lists()
	if _, ok := got["Favorites"]; ok {
		t.Fatalf("favorites not sent again, still there: %v", got)
	}
	if len(got["Volunteers"]) != 1 || len(got["Second phone list"]) != 1 {
		t.Fatalf("after resync: %v", got)
	}

	// Deleting a list in WhatsApp removes it.
	s.onWAList(0, label("1", "Volunteers", true))
	if _, ok := lists()["Volunteers"]; ok {
		t.Fatal("deleted list still there")
	}
}
