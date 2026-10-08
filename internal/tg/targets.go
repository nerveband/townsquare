package tg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram/query"
	tgapi "github.com/gotd/td/tg"
)

// Target is a Telegram chat Townsquare can post to. JIDs:
//
//	tg:self                  Saved Messages (your own chat, safe for tests)
//	tg:chat:<id>             basic group
//	tg:ch:<id>:<accessHash>  channel or supergroup
//	tg:ch:<id>:<hash>:t<n>   one topic of a forum supergroup
//	tg:story:self            your own story
//	tg:story:ch:<id>:<hash>  a channel's story
type Target struct {
	JID     string
	Kind    string // self, group, channel, topic, story
	Parent  string
	Name    string
	CanSend bool
	Members int
}

// Targets lists Saved Messages plus every group and channel in the account,
// marking where the user is allowed to post.
func (cl *Client) Targets(ctx context.Context) ([]Target, error) {
	api := cl.API()
	if api == nil || !cl.Ready() {
		return nil, errors.New("Telegram is not logged in")
	}
	out := []Target{{JID: "tg:self", Kind: "self", Name: "Saved Messages", CanSend: true}}
	if _, err := api.StoriesCanSendStory(ctx, &tgapi.InputPeerSelf{}); err == nil {
		out = append(out, Target{JID: "tg:story:self", Kind: "story", Name: "My Story", CanSend: true})
	}
	it := query.GetDialogs(api).BatchSize(100).Iter()
	for it.Next(ctx) {
		e := it.Value()
		switch p := e.Peer.(type) {
		case *tgapi.InputPeerChat:
			ch, ok := e.Entities.Chats()[p.ChatID]
			if !ok || ch.Left || ch.Deactivated {
				continue
			}
			can := ch.Creator || ch.AdminRights.Flags != 0 || !ch.DefaultBannedRights.SendMessages
			out = append(out, Target{JID: fmt.Sprintf("tg:chat:%d", ch.ID), Kind: "group", Name: ch.Title, CanSend: can, Members: ch.ParticipantsCount})
		case *tgapi.InputPeerChannel:
			ch, ok := e.Entities.Channels()[p.ChannelID]
			if !ok || ch.Left {
				continue
			}
			t := Target{JID: fmt.Sprintf("tg:ch:%d:%d", ch.ID, ch.AccessHash), Name: ch.Title}
			t.Members, _ = ch.GetParticipantsCount()
			admin, hasAdmin := ch.GetAdminRights()
			if ch.Broadcast {
				t.Kind = "channel"
				t.CanSend = ch.Creator || (hasAdmin && admin.PostMessages)
				if ch.Creator || (hasAdmin && admin.PostStories) {
					out = append(out, Target{JID: fmt.Sprintf("tg:story:ch:%d:%d", ch.ID, ch.AccessHash), Kind: "story",
						Name: "Story: " + ch.Title, Parent: ch.Title, CanSend: true})
				}
			} else {
				t.Kind = "group"
				banned, isBanned := ch.GetBannedRights()
				def, hasDef := ch.GetDefaultBannedRights()
				t.CanSend = ch.Creator || hasAdmin || !((isBanned && banned.SendMessages) || (hasDef && def.SendMessages))
			}
			out = append(out, t)
			if ch.Forum {
				out = append(out, cl.topics(ctx, ch, t)...)
			}
		}
	}
	if err := it.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// topics lists the open topics of a forum supergroup as separate targets.
func (cl *Client) topics(ctx context.Context, ch *tgapi.Channel, group Target) []Target {
	res, err := cl.API().MessagesGetForumTopics(ctx, &tgapi.MessagesGetForumTopicsRequest{
		Peer: &tgapi.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, Limit: 100})
	if err != nil {
		return nil
	}
	var out []Target
	for _, tc := range res.Topics {
		t, ok := tc.(*tgapi.ForumTopic)
		if !ok || t.ID == 1 { // topic 1 is "General", which the group target already covers
			continue
		}
		out = append(out, Target{JID: fmt.Sprintf("%s:t%d", group.JID, t.ID), Kind: "topic", Name: group.Name + " › " + t.Title,
			Parent: group.Name, Members: group.Members, CanSend: group.CanSend && (!t.Closed || ch.Creator || ch.AdminRights.Flags != 0)})
	}
	return out
}

// Address is a parsed Telegram JID.
type Address struct {
	Peer  tgapi.InputPeerClass
	Topic int  // forum topic id, 0 if none
	Story bool // post as a story instead of a message
}

// Parse turns a Townsquare Telegram JID into an address.
func Parse(jid string) (Address, error) {
	var a Address
	if strings.HasPrefix(jid, "tg:story:") {
		a.Story = true
		jid = "tg:" + strings.TrimPrefix(jid, "tg:story:")
	}
	parts := strings.Split(jid, ":")
	if len(parts) == 5 && parts[1] == "ch" && strings.HasPrefix(parts[4], "t") {
		n, err := strconv.Atoi(parts[4][1:])
		if err != nil {
			return a, fmt.Errorf("bad Telegram topic in %q", jid)
		}
		a.Topic = n
		jid = strings.Join(parts[:4], ":")
	}
	p, err := Peer(jid)
	a.Peer = p
	return a, err
}

// Peer turns a Townsquare JID into a Telegram input peer.
func Peer(jid string) (tgapi.InputPeerClass, error) {
	parts := strings.Split(jid, ":")
	switch {
	case jid == "tg:self":
		return &tgapi.InputPeerSelf{}, nil
	case len(parts) == 3 && parts[1] == "chat":
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, err
		}
		return &tgapi.InputPeerChat{ChatID: id}, nil
	case len(parts) == 4 && parts[1] == "ch":
		id, err1 := strconv.ParseInt(parts[2], 10, 64)
		hash, err2 := strconv.ParseInt(parts[3], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad Telegram id %q", jid)
		}
		return &tgapi.InputPeerChannel{ChannelID: id, AccessHash: hash}, nil
	}
	return nil, fmt.Errorf("not a Telegram chat id: %q", jid)
}

// IsTelegram reports whether a JID belongs to Telegram.
func IsTelegram(jid string) bool { return strings.HasPrefix(jid, "tg:") }
