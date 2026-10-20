package wa

import (
	"context"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types/events"
)

// FavoritesList is the id Townsquare gives WhatsApp's Favorites list.
const FavoritesList = "favorites"

// ListChange is one change to the WhatsApp lists (Business labels are lists too).
// Townsquare only reads them; it never edits lists in WhatsApp.
type ListChange struct {
	// Op is "list" (a list was created, renamed or deleted), "member" (a chat was
	// added to or removed from a list), "favorites" (the whole Favorites list) or
	// "pin" (a chat was pinned or unpinned).
	Op      string
	ListID  string
	Name    string
	Kind    string // favorites, custom or label
	Color   int
	Order   int
	Deleted bool     // list: deleted (or a built-in list we don't show)
	JID     string   // member, pin: the chat
	In      bool     // member: added (true) or removed; pin: pinned
	JIDs    []string // favorites: every chat in it
}

// listKind maps WhatsApp's list types to what Townsquare shows. Lists that
// WhatsApp fills on its own (Unread, Groups, Channels and so on) have no
// members to read, so they are left out.
func listKind(t waSyncAction.LabelEditAction_ListType) string {
	switch t {
	case waSyncAction.LabelEditAction_FAVORITES:
		return "favorites"
	case waSyncAction.LabelEditAction_CUSTOM:
		return "custom"
	case waSyncAction.LabelEditAction_NONE, waSyncAction.LabelEditAction_PREDEFINED,
		waSyncAction.LabelEditAction_SERVER_ASSIGNED, waSyncAction.LabelEditAction_LEAD:
		return "label" // WhatsApp Business labels
	}
	return ""
}

// ParseListEvent turns a whatsmeow event into a list change (ok=false for other events).
func ParseListEvent(evt any) (ListChange, bool) {
	switch e := evt.(type) {
	case *events.LabelEdit:
		a := e.Action
		kind := listKind(a.GetType())
		return ListChange{Op: "list", ListID: e.LabelID, Name: a.GetName(), Kind: kind, Color: int(a.GetColor()),
			Order: int(a.GetOrderIndex()), Deleted: a.GetDeleted() || kind == "" || a.GetName() == ""}, true
	case *events.LabelAssociationChat:
		return ListChange{Op: "member", ListID: e.LabelID, JID: e.JID.ToNonAD().String(), In: e.Action.GetLabeled()}, true
	case *events.Pin:
		return ListChange{Op: "pin", JID: e.JID.ToNonAD().String(), In: e.Action.GetPinned()}, true
	case *events.AppState:
		if len(e.Index) == 0 || e.Index[0] != appstate.IndexFavorites {
			return ListChange{}, false
		}
		ch := ListChange{Op: "favorites", ListID: FavoritesList, Name: "Favorites", Kind: "favorites", Order: -1, JIDs: []string{}}
		for _, f := range e.SyncActionValue.GetFavoritesAction().GetFavorites() {
			if id := f.GetID(); id != "" {
				ch.JIDs = append(ch.JIDs, id)
			}
		}
		return ch, true
	}
	return ListChange{}, false
}

// ResyncLists asks WhatsApp for the full account state again, so every list,
// label, favorite and pin arrives as events (see ParseListEvent). Events are
// handled before it returns.
func ResyncLists(ctx context.Context, cli *whatsmeow.Client) error {
	for _, name := range []appstate.WAPatchName{appstate.WAPatchRegular, appstate.WAPatchRegularLow, appstate.WAPatchRegularHigh} {
		if err := cli.FetchAppState(ctx, name, true, false); err != nil {
			return err
		}
	}
	return nil
}
