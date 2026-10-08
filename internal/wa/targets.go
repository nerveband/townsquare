package wa

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// Target is anything Townsquare can post to.
type Target struct {
	JID     string `json:"jid"`
	Kind    string `json:"kind"` // group, community, announce, channel, status
	Name    string `json:"name"`
	Parent  string `json:"parent,omitempty"`
	CanSend bool   `json:"can_send"`
	Members int    `json:"members,omitempty"`
}

// ListTargets fetches groups, communities and channels live from WhatsApp.
func ListTargets(ctx context.Context, cli *whatsmeow.Client) ([]Target, error) {
	own := cli.Store.GetJID().ToNonAD()
	ownLID := cli.Store.GetLID().ToNonAD()
	out := []Target{
		{JID: own.String(), Kind: "self", Name: "Message yourself", CanSend: true},
		{JID: types.StatusBroadcastJID.String(), Kind: "status", Name: "My Status", CanSend: true},
	}

	groups, err := cli.GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("groups: %w", err)
	}
	names := map[string]string{}
	for _, g := range groups {
		names[g.JID.String()] = g.Name
	}
	for _, g := range groups {
		t := Target{JID: g.JID.String(), Name: g.Name, Kind: "group", Members: len(g.Participants)}
		if strings.TrimSpace(t.Name) == "" {
			t.Name = fmt.Sprintf("Unnamed group (%d members)", t.Members)
		}
		isAdmin := false
		for _, p := range g.Participants {
			pj := p.JID.ToNonAD()
			if (pj == own || pj == ownLID || p.LID.ToNonAD() == ownLID) && (p.IsAdmin || p.IsSuperAdmin) {
				isAdmin = true
			}
		}
		switch {
		case g.IsParent:
			t.Kind = "community"
		case g.IsDefaultSubGroup:
			t.Kind = "announce"
		}
		if !g.LinkedParentJID.IsEmpty() {
			t.Parent = names[g.LinkedParentJID.String()]
			if t.Parent == "" {
				t.Parent = g.LinkedParentJID.String()
			}
		}
		t.CanSend = !g.IsAnnounce || isAdmin
		if g.IsParent {
			t.CanSend = false // post to the announcement group instead
		}
		out = append(out, t)
	}

	chans, err := cli.GetSubscribedNewsletters(ctx)
	if err != nil {
		return nil, fmt.Errorf("channels: %w", err)
	}
	for _, n := range chans {
		role := types.NewsletterRole("")
		if n.ViewerMeta != nil {
			role = n.ViewerMeta.Role
		}
		out = append(out, Target{
			JID: n.ID.String(), Kind: "channel", Name: n.ThreadMeta.Name.Text,
			CanSend: role == types.NewsletterRoleAdmin || role == types.NewsletterRoleOwner,
			Members: n.ThreadMeta.SubscriberCount,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindOrder(out[i].Kind) < kindOrder(out[j].Kind)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func kindOrder(k string) int {
	return map[string]int{"self": 0, "status": 1, "announce": 2, "community": 3, "group": 4, "channel": 5}[k]
}

// Resolve finds one target by JID, "status", or a case-insensitive name match.
func Resolve(targets []Target, q string) (Target, error) {
	lq := strings.ToLower(strings.TrimSpace(q))
	for _, t := range targets {
		if (lq == "status" && t.Kind == "status") || ((lq == "me" || lq == "self") && t.Kind == "self") {
			return t, nil
		}
	}
	var hits []Target
	for _, t := range targets {
		if t.JID == q {
			return t, nil
		}
		if strings.ToLower(t.Name) == lq {
			return t, nil
		}
		if strings.Contains(strings.ToLower(t.Name), lq) {
			hits = append(hits, t)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) == 0 {
		return Target{}, fmt.Errorf("no target matches %q", q)
	}
	var names []string
	for _, h := range hits {
		names = append(names, fmt.Sprintf("%s (%s, %s)", h.Name, h.Kind, h.JID))
	}
	return Target{}, fmt.Errorf("%q matches %d targets: %s", q, len(hits), strings.Join(names, "; "))
}
