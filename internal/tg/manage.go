package tg

import (
	"context"
	"errors"

	"github.com/gotd/td/telegram/message"
	tgapi "github.com/gotd/td/tg"
)

// Delete removes messages for everyone (or deletes stories). Telegram has no
// time limit for this in chats you can post in.
func (cl *Client) Delete(ctx context.Context, jid string, ids []int) error {
	api := cl.API()
	if api == nil || !cl.Ready() {
		return errors.New("Telegram is not logged in")
	}
	addr, err := Parse(jid)
	if err != nil {
		return err
	}
	if addr.Story {
		_, err := api.StoriesDeleteStories(ctx, &tgapi.StoriesDeleteStoriesRequest{Peer: addr.Peer, ID: ids})
		return err
	}
	if p, ok := addr.Peer.(*tgapi.InputPeerChannel); ok {
		_, err := api.ChannelsDeleteMessages(ctx, &tgapi.ChannelsDeleteMessagesRequest{
			Channel: &tgapi.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, ID: ids})
		return err
	}
	_, err = api.MessagesDeleteMessages(ctx, &tgapi.MessagesDeleteMessagesRequest{Revoke: true, ID: ids})
	return err
}

// Edit replaces the text (or caption) of a message, with Townsquare's formatting.
func (cl *Client) Edit(ctx context.Context, jid string, id int, caption string) error {
	api := cl.API()
	if api == nil || !cl.Ready() {
		return errors.New("Telegram is not logged in")
	}
	addr, err := Parse(jid)
	if err != nil {
		return err
	}
	if addr.Story {
		return errors.New("stories can't be edited here; delete it and post again")
	}
	_, err = message.NewSender(api).To(addr.Peer).Edit(id).StyledText(ctx, Styled(caption)...)
	return err
}
