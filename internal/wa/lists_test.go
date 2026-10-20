package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestParseListEvent(t *testing.T) {
	group := types.NewJID("120363000000000102", types.GroupServer)
	edit := func(name string, typ waSyncAction.LabelEditAction_ListType, deleted bool) *events.LabelEdit {
		return &events.LabelEdit{LabelID: "7", Action: &waSyncAction.LabelEditAction{Name: proto.String(name), Type: typ.Enum(), Deleted: proto.Bool(deleted), OrderIndex: proto.Int32(3)}}
	}
	cases := []struct {
		name string
		evt  any
		want ListChange
		ok   bool
	}{
		{"custom list", edit("Volunteers", waSyncAction.LabelEditAction_CUSTOM, false),
			ListChange{Op: "list", ListID: "7", Name: "Volunteers", Kind: "custom", Order: 3}, true},
		{"business label", edit("New customer", waSyncAction.LabelEditAction_NONE, false),
			ListChange{Op: "list", ListID: "7", Name: "New customer", Kind: "label", Order: 3}, true},
		// WhatsApp fills these itself: nothing to read, so they are dropped.
		{"built-in Unread", edit("Unread", waSyncAction.LabelEditAction_UNREAD, false),
			ListChange{Op: "list", ListID: "7", Name: "Unread", Order: 3, Deleted: true}, true},
		{"deleted list", edit("Old", waSyncAction.LabelEditAction_CUSTOM, true),
			ListChange{Op: "list", ListID: "7", Name: "Old", Kind: "custom", Order: 3, Deleted: true}, true},
		{"chat added", &events.LabelAssociationChat{JID: group, LabelID: "7", Action: &waSyncAction.LabelAssociationAction{Labeled: proto.Bool(true)}},
			ListChange{Op: "member", ListID: "7", JID: group.String(), In: true}, true},
		{"chat removed", &events.LabelAssociationChat{JID: group, LabelID: "7", Action: &waSyncAction.LabelAssociationAction{Labeled: proto.Bool(false)}},
			ListChange{Op: "member", ListID: "7", JID: group.String()}, true},
		{"pinned", &events.Pin{JID: group, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}},
			ListChange{Op: "pin", JID: group.String(), In: true}, true},
		{"other app state", &events.AppState{Index: []string{appstate.IndexArchive}}, ListChange{}, false},
		{"unrelated event", &events.Connected{}, ListChange{}, false},
	}
	for _, c := range cases {
		got, ok := ParseListEvent(c.evt)
		if ok != c.ok || got.Op != c.want.Op || got.ListID != c.want.ListID || got.Name != c.want.Name || got.Kind != c.want.Kind ||
			got.Order != c.want.Order || got.Deleted != c.want.Deleted || got.JID != c.want.JID || got.In != c.want.In {
			t.Errorf("%s: got %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}

	fav := &events.AppState{Index: []string{appstate.IndexFavorites}, SyncActionValue: &waSyncAction.SyncActionValue{
		FavoritesAction: &waSyncAction.FavoritesAction{Favorites: []*waSyncAction.FavoritesAction_Favorite{{ID: proto.String(group.String())}, {ID: proto.String("")}}}}}
	got, ok := ParseListEvent(fav)
	if !ok || got.Op != "favorites" || got.ListID != FavoritesList || len(got.JIDs) != 1 || got.JIDs[0] != group.String() {
		t.Fatalf("favorites: %+v %v", got, ok)
	}
	// An emptied Favorites list must clear it, not be ignored.
	empty := &events.AppState{Index: []string{appstate.IndexFavorites}, SyncActionValue: &waSyncAction.SyncActionValue{FavoritesAction: &waSyncAction.FavoritesAction{}}}
	if got, ok := ParseListEvent(empty); !ok || got.JIDs == nil || len(got.JIDs) != 0 {
		t.Fatalf("empty favorites: %+v %v", got, ok)
	}
}
