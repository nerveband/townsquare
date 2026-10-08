// Package tgbot is the optional Telegram bot transport (official Bot API via
// go-telegram/bot). The bot only sees chats it was added to, and posts as itself
// in groups and as the channel in channels.
package tgbot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/nerveband/townsquare/internal/tg"
)

// Chat is a chat the bot can see. JIDs are "tgbot:<chat id>".
type Chat struct {
	JID     string
	Kind    string // self, group, channel
	Name    string
	CanSend bool
	Gone    bool
}

// Bot wraps a running bot.
type Bot struct {
	mu       sync.Mutex
	b        *bot.Bot
	Name     string
	Username string
	onChat   func(Chat)
}

// New returns nil, nil when there is no telegram.token in the data folder.
func New(dataDir string, onChat func(Chat)) (*Bot, error) {
	tok, err := os.ReadFile(filepath.Join(dataDir, "telegram.token"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w := &Bot{onChat: onChat}
	// Skip the startup getMe check so a network blip at boot doesn't disable the bot;
	// Start fetches the bot's name, and polling retries on its own.
	b, err := bot.New(strings.TrimSpace(string(tok)), bot.WithSkipGetMe(), bot.WithDefaultHandler(w.handle),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message", "channel_post", "my_chat_member"}))
	if err != nil {
		return nil, fmt.Errorf("telegram bot: %w", err)
	}
	w.b = b
	return w, nil
}

// Start reads updates (long polling, no public address needed) until ctx ends.
func (w *Bot) Start(ctx context.Context) {
	go func() {
		for {
			if me, err := w.b.GetMe(ctx); err == nil {
				w.mu.Lock()
				w.Name, w.Username = me.FirstName, me.Username
				w.mu.Unlock()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
	}()
	w.b.Start(ctx)
}

// Info returns the bot's display name and @username (empty until Telegram answers).
func (w *Bot) Info() (name, username string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Name, w.Username
}

func kindOf(c models.Chat) string {
	switch c.Type {
	case models.ChatTypeChannel:
		return "channel"
	case models.ChatTypePrivate:
		return "self"
	}
	return "group"
}

func title(c models.Chat) string {
	if c.Type == models.ChatTypePrivate {
		return "Bot chat with " + strings.TrimSpace(c.FirstName+" "+c.LastName)
	}
	return c.Title
}

func (w *Bot) handle(ctx context.Context, _ *bot.Bot, u *models.Update) {
	switch {
	case u.MyChatMember != nil:
		m := u.MyChatMember
		ch := Chat{JID: fmt.Sprintf("tgbot:%d", m.Chat.ID), Kind: kindOf(m.Chat), Name: title(m.Chat)}
		switch m.NewChatMember.Type {
		case models.ChatMemberTypeLeft, models.ChatMemberTypeBanned:
			ch.Gone = true
		case models.ChatMemberTypeAdministrator:
			a := m.NewChatMember.Administrator
			ch.CanSend = ch.Kind != "channel" || (a != nil && a.CanPostMessages)
		case models.ChatMemberTypeMember:
			ch.CanSend = ch.Kind != "channel"
		case models.ChatMemberTypeRestricted:
			ch.CanSend = false
		}
		w.onChat(ch)
	case u.Message != nil && u.Message.Chat.Type == models.ChatTypePrivate:
		// /start from the owner: remember the private chat as a safe test target.
		w.onChat(Chat{JID: fmt.Sprintf("tgbot:%d", u.Message.Chat.ID), Kind: "self", Name: title(u.Message.Chat), CanSend: true})
	}
}

// Send posts text and media to a chat with Telegram formatting.
func (w *Bot) Send(ctx context.Context, jid, caption string, media []tg.Media) (string, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(jid, "tgbot:"), 10, 64)
	if err != nil {
		return "", fmt.Errorf("bad bot chat id %q", jid)
	}
	html := tg.ToHTML(caption)
	if len(media) == 0 {
		m, err := w.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: id, Text: html, ParseMode: models.ParseModeHTML})
		if err != nil {
			return "", err
		}
		return strconv.Itoa(m.ID), nil
	}
	first := ""
	used := false
	note := func(m *models.Message, err error) error {
		if err == nil && first == "" && m != nil {
			first = strconv.Itoa(m.ID)
		}
		return err
	}
	var visual []tg.Media
	for _, m := range media {
		if m.Kind == "image" || m.Kind == "video" {
			visual = append(visual, m)
		}
	}
	if len(visual) > 1 {
		var group []models.InputMedia
		var files []*os.File
		defer func() {
			for _, f := range files {
				f.Close()
			}
		}()
		for i, m := range visual[:min(len(visual), 10)] {
			f, err := os.Open(m.Path)
			if err != nil {
				return first, err
			}
			files = append(files, f)
			cap, pm := "", models.ParseMode("")
			if i == 0 {
				cap, pm = html, models.ParseModeHTML
			}
			att := fmt.Sprintf("attach://f%d", i)
			if m.Kind == "image" {
				group = append(group, &models.InputMediaPhoto{Media: att, Caption: cap, ParseMode: pm, MediaAttachment: f})
			} else {
				group = append(group, &models.InputMediaVideo{Media: att, Caption: cap, ParseMode: pm, MediaAttachment: f, SupportsStreaming: true})
			}
		}
		msgs, err := w.b.SendMediaGroup(ctx, &bot.SendMediaGroupParams{ChatID: id, Media: group})
		if err != nil {
			return first, err
		}
		if len(msgs) > 0 {
			first = strconv.Itoa(msgs[0].ID)
		}
		used = true
		visual = nil
	}
	for _, m := range media {
		if (m.Kind == "image" || m.Kind == "video") && visual == nil {
			continue // already sent as an album
		}
		f, err := os.Open(m.Path)
		if err != nil {
			return first, err
		}
		in := &models.InputFileUpload{Filename: m.Name, Data: f}
		cap, pm := "", models.ParseMode("")
		if !used && m.Kind != "voice" {
			cap, pm, used = html, models.ParseModeHTML, true
		}
		switch m.Kind {
		case "image":
			err = note(w.b.SendPhoto(ctx, &bot.SendPhotoParams{ChatID: id, Photo: in, Caption: cap, ParseMode: pm}))
		case "video":
			err = note(w.b.SendVideo(ctx, &bot.SendVideoParams{ChatID: id, Video: in, Caption: cap, ParseMode: pm, SupportsStreaming: true}))
		case "voice":
			err = note(w.b.SendVoice(ctx, &bot.SendVoiceParams{ChatID: id, Voice: in, Duration: m.Seconds}))
		default:
			err = note(w.b.SendDocument(ctx, &bot.SendDocumentParams{ChatID: id, Document: in, Caption: cap, ParseMode: pm}))
		}
		f.Close()
		if err != nil {
			return first, err
		}
	}
	if !used && strings.TrimSpace(caption) != "" {
		if err := note(w.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: id, Text: html, ParseMode: models.ParseModeHTML})); err != nil {
			return first, err
		}
	}
	return first, nil
}

// IsBot reports whether a JID belongs to the bot transport.
func IsBot(jid string) bool { return strings.HasPrefix(jid, "tgbot:") }

// Delete removes the bot's messages from a chat for everyone (Telegram allows
// this for 48 hours in groups).
func (w *Bot) Delete(ctx context.Context, jid string, ids []int) error {
	id, err := strconv.ParseInt(strings.TrimPrefix(jid, "tgbot:"), 10, 64)
	if err != nil {
		return fmt.Errorf("bad bot chat id %q", jid)
	}
	_, err = w.b.DeleteMessages(ctx, &bot.DeleteMessagesParams{ChatID: id, MessageIDs: ids})
	return err
}

// Edit changes the text (or, for a photo or file, the caption) of a bot message.
func (w *Bot) Edit(ctx context.Context, jid string, msgID int, caption string, isMedia bool) error {
	id, err := strconv.ParseInt(strings.TrimPrefix(jid, "tgbot:"), 10, 64)
	if err != nil {
		return fmt.Errorf("bad bot chat id %q", jid)
	}
	html := tg.ToHTML(caption)
	if isMedia {
		_, err = w.b.EditMessageCaption(ctx, &bot.EditMessageCaptionParams{ChatID: id, MessageID: msgID, Caption: html, ParseMode: models.ParseModeHTML})
	} else {
		_, err = w.b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: id, MessageID: msgID, Text: html, ParseMode: models.ParseModeHTML})
	}
	return err
}
