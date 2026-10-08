package tg

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	tghtml "github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	tgapi "github.com/gotd/td/tg"
)

// Media is one already-converted file to send (see internal/wa.Convert).
type Media struct {
	Kind     string // image, video, voice, audio, document
	Path     string
	Name     string
	Mime     string
	Seconds  int
	Width    int
	Height   int
	Waveform []byte
}

// Options are Telegram-specific send settings.
type Options struct {
	Silent    bool      // deliver without a notification sound
	NoPreview bool      // don't show link previews
	Schedule  time.Time // if set, hand the message to Telegram's own scheduled queue
}

// Send posts a caption and media to one chat. Photos and videos go as one album
// (up to 10) with the caption on the first item; other files go one by one.
// It returns the id of the first message.
func (cl *Client) Send(ctx context.Context, jid, caption string, media []Media, o Options) (string, error) {
	ids, err := cl.SendIDs(ctx, jid, caption, media, o)
	if len(ids) == 0 {
		return "", err
	}
	return fmt.Sprint(ids[0]), err
}

// SendIDs is Send, returning every message id created (for managing scheduled messages).
func (cl *Client) SendIDs(ctx context.Context, jid, caption string, media []Media, o Options) ([]int, error) {
	api := cl.API()
	if api == nil || !cl.Ready() {
		return nil, errors.New("Telegram is not logged in")
	}
	addr, err := Parse(jid)
	if err != nil {
		return nil, err
	}
	if addr.Story {
		id, err := cl.sendStory(ctx, addr.Peer, caption, media)
		if err != nil {
			return nil, err
		}
		return []int{id}, nil
	}
	var all []int
	collect := func(u tgapi.UpdatesClass, err error) (string, error) {
		ids := updateIDs(u)
		all = append(all, ids...)
		if err != nil {
			return "", err
		}
		return "ok", nil
	}
	b := message.NewSender(api).To(addr.Peer).CloneBuilder()
	if addr.Topic != 0 {
		b = b.Reply(addr.Topic)
	}
	if o.Silent {
		b = b.Silent()
	}
	if o.NoPreview {
		b = b.NoWebpage()
	}
	if !o.Schedule.IsZero() {
		b = b.Schedule(o.Schedule)
	}
	text := Styled(caption)
	up := uploader.NewUploader(api)

	if len(media) == 0 {
		if strings.TrimSpace(caption) == "" {
			return nil, errors.New("nothing to send")
		}
		_, err := collect(b.StyledText(ctx, text...))
		return all, err
	}

	var visual []Media
	var others []Media
	for _, m := range media {
		if m.Kind == "image" || m.Kind == "video" {
			visual = append(visual, m)
		} else {
			others = append(others, m)
		}
	}
	captionUsed := false
	if len(visual) > 0 {
		var opts []message.MultiMediaOption
		for i, m := range visual {
			f, err := up.FromPath(ctx, m.Path)
			if err != nil {
				return all, fmt.Errorf("upload %s: %w", m.Name, err)
			}
			var cap []message.StyledTextOption
			if i == 0 {
				cap = text
			}
			if m.Kind == "image" {
				opts = append(opts, message.UploadedPhoto(f, cap...))
			} else {
				opts = append(opts, message.UploadedDocument(f, cap...).MIME("video/mp4").Filename(m.Name).
					Attributes(&tgapi.DocumentAttributeVideo{SupportsStreaming: true, Duration: float64(m.Seconds), W: m.Width, H: m.Height}))
			}
		}
		var id string
		if len(opts) == 1 {
			id, err = collect(b.Media(ctx, opts[0]))
		} else {
			// Telegram albums hold at most 10 items.
			for start := 0; start < len(opts); start += 10 {
				end := min(start+10, len(opts))
				var gid string
				gid, err = collect(b.Album(ctx, opts[start], opts[start+1:end]...))
				if id == "" {
					id = gid
				}
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			return all, err
		}
		_ = id
		captionUsed = true
	}
	for _, m := range others {
		f, err := up.FromPath(ctx, m.Path)
		if err != nil {
			return all, fmt.Errorf("upload %s: %w", m.Name, err)
		}
		var cap []message.StyledTextOption
		if !captionUsed && m.Kind != "voice" {
			cap, captionUsed = text, true
		}
		doc := message.UploadedDocument(f, cap...).MIME(m.Mime).Filename(m.Name)
		switch m.Kind {
		case "voice":
			doc = doc.MIME("audio/ogg").Attributes(&tgapi.DocumentAttributeAudio{Voice: true, Duration: m.Seconds, Waveform: m.Waveform})
		case "audio":
			doc = doc.Attributes(&tgapi.DocumentAttributeAudio{Duration: m.Seconds, Title: m.Name})
		}
		if _, err := collect(b.Media(ctx, doc)); err != nil {
			return all, err
		}
	}
	if !captionUsed && strings.TrimSpace(caption) != "" {
		if _, err := collect(b.StyledText(ctx, text...)); err != nil {
			return all, err
		}
	}
	return all, nil
}

// updateIDs pulls the new message ids out of a send response.
func updateIDs(u tgapi.UpdatesClass) []int {
	var ids []int
	add := func(m tgapi.MessageClass) {
		if mm, ok := m.(*tgapi.Message); ok {
			ids = append(ids, mm.ID)
		}
	}
	switch v := u.(type) {
	case *tgapi.Updates:
		for _, up := range v.Updates {
			switch x := up.(type) {
			case *tgapi.UpdateNewMessage:
				add(x.Message)
			case *tgapi.UpdateNewChannelMessage:
				add(x.Message)
			case *tgapi.UpdateNewScheduledMessage:
				add(x.Message)
			}
		}
		if len(ids) == 0 {
			for _, up := range v.Updates {
				if x, ok := up.(*tgapi.UpdateMessageID); ok {
					ids = append(ids, x.ID)
				}
			}
		}
	case *tgapi.UpdateShortSentMessage:
		ids = append(ids, v.ID)
	}
	return ids
}

// Styled converts WhatsApp-style formatting (*bold* _italic_ ~strike~ `code`
// ```block``` and "> quote" lines) to Telegram formatting.
func Styled(text string) []message.StyledTextOption {
	if strings.TrimSpace(text) == "" {
		return []message.StyledTextOption{styling.Plain("")}
	}
	return []message.StyledTextOption{tghtml.String(nil, ToHTML(text))}
}

var (
	reBold   = regexp.MustCompile(`(^|[\s(\["'“‘])\*([^\s*](?:[^*\n]*?[^\s*])?)\*`)
	reItalic = regexp.MustCompile(`(^|[\s(\["'“‘])_([^\s_](?:[^_\n]*?[^\s_])?)_`)
	reStrike = regexp.MustCompile(`(^|[\s(\["'“‘])~([^\s~](?:[^~\n]*?[^\s~])?)~`)
	reCode   = regexp.MustCompile("`([^`\n]+)`")
	reURL    = regexp.MustCompile(`https?://[^\s<]+`)
)

// ToHTML turns WhatsApp markup into the HTML subset Telegram understands.
func ToHTML(text string) string {
	parts := strings.Split(text, "```")
	var out strings.Builder
	for i, chunk := range parts {
		if i%2 == 1 {
			out.WriteString("<pre>" + html.EscapeString(strings.TrimPrefix(chunk, "\n")) + "</pre>")
			continue
		}
		lines := strings.Split(chunk, "\n")
		for j, line := range lines {
			quote := strings.HasPrefix(line, "> ")
			if quote {
				line = strings.TrimPrefix(line, "> ")
			}
			// Protect URLs and inline code from the other markers.
			var keep []string
			hold := func(s string) string { keep = append(keep, s); return fmt.Sprintf("\x00%d\x00", len(keep)-1) }
			line = reCode.ReplaceAllStringFunc(line, func(m string) string {
				return hold("<code>" + html.EscapeString(m[1:len(m)-1]) + "</code>")
			})
			line = reURL.ReplaceAllStringFunc(line, func(m string) string { return hold(html.EscapeString(m)) })
			line = html.EscapeString(line)
			line = reBold.ReplaceAllString(line, "$1<b>$2</b>")
			line = reItalic.ReplaceAllString(line, "$1<i>$2</i>")
			line = reStrike.ReplaceAllString(line, "$1<s>$2</s>")
			for k, v := range keep {
				line = strings.ReplaceAll(line, fmt.Sprintf("\x00%d\x00", k), v)
			}
			if quote {
				line = "<blockquote>" + line + "</blockquote>"
			}
			out.WriteString(line)
			if j < len(lines)-1 {
				out.WriteString("\n")
			}
		}
	}
	return out.String()
}
