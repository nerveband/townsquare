package wa

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// Message is one WhatsApp post. Kind is text, image, video, voice, audio or document.
type Message struct {
	Kind    string
	Text    string // body for text, caption for media
	File    string // path to media
	Caption string
}

// Prepared is media converted and uploaded once, reusable for many targets
// of the same class (regular chats vs channels use different uploads).
type Prepared struct {
	Kind     string
	Media    *Media
	upload   *whatsmeow.UploadResponse
	channel  bool
	fileName string
}

// Send converts, uploads and sends msg to target. It returns the WhatsApp message ID.
func Send(ctx context.Context, cli *whatsmeow.Client, t Target, msg Message) (string, error) {
	if !t.CanSend {
		return "", fmt.Errorf("%s: you can't post here (not an admin, or it's a community parent)", t.Name)
	}
	to, err := types.ParseJID(t.JID)
	if err != nil {
		return "", err
	}
	isChannel := to.Server == types.NewsletterServer

	var wm *waE2E.Message
	var extra whatsmeow.SendRequestExtra
	if msg.Kind == "text" {
		wm = &waE2E.Message{Conversation: proto.String(msg.Text)}
	} else {
		p, err := Prepare(ctx, cli, msg, isChannel)
		if err != nil {
			return "", err
		}
		wm = p.build(msg.Text)
		if isChannel {
			extra.MediaHandle = p.upload.Handle
		}
	}
	resp, err := cli.SendMessage(ctx, to, wm, extra)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// Prepare converts the file for WhatsApp and uploads it.
func Prepare(ctx context.Context, cli *whatsmeow.Client, msg Message, channel bool) (*Prepared, error) {
	m, err := Convert(ctx, msg.Kind, msg.File)
	if err != nil {
		return nil, err
	}
	return Upload(ctx, cli, msg.Kind, m, filepath.Base(msg.File), channel)
}

// Upload uploads already-converted media. Channels need their own upload.
func Upload(ctx context.Context, cli *whatsmeow.Client, kind string, m *Media, fileName string, channel bool) (*Prepared, error) {
	msg := Message{Kind: kind, File: fileName}
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return nil, err
	}
	mt := map[string]whatsmeow.MediaType{
		"image": whatsmeow.MediaImage, "video": whatsmeow.MediaVideo,
		"voice": whatsmeow.MediaAudio, "audio": whatsmeow.MediaAudio, "document": whatsmeow.MediaDocument,
	}[msg.Kind]
	if mt == "" {
		return nil, fmt.Errorf("unknown kind %q", msg.Kind)
	}
	var up whatsmeow.UploadResponse
	if channel {
		up, err = cli.UploadNewsletter(ctx, data, mt)
	} else {
		up, err = cli.Upload(ctx, data, mt)
	}
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	return &Prepared{Kind: msg.Kind, Media: m, upload: &up, channel: channel, fileName: filepath.Base(msg.File)}, nil
}

// SendPrepared sends uploaded media (or text when p is nil) to a JID.
func SendPrepared(ctx context.Context, cli *whatsmeow.Client, jid string, p *Prepared, text string) (string, error) {
	r, err := SendPreparedResp(ctx, cli, jid, p, text)
	return r.ID, err
}

// SendPreparedResp is SendPrepared, returning the full response (channel posts
// carry a server id, used to read view and reaction counts later).
func SendPreparedResp(ctx context.Context, cli *whatsmeow.Client, jid string, p *Prepared, text string) (whatsmeow.SendResponse, error) {
	to, err := types.ParseJID(jid)
	if err != nil {
		return whatsmeow.SendResponse{}, err
	}
	var wm *waE2E.Message
	var extra whatsmeow.SendRequestExtra
	if p == nil {
		wm = &waE2E.Message{Conversation: proto.String(text)}
	} else {
		wm = p.build(text)
		if p.channel {
			extra.MediaHandle = p.upload.Handle
		}
	}
	return cli.SendMessage(ctx, to, wm, extra)
}

// IsChannel reports whether jid is a WhatsApp channel.
func IsChannel(jid string) bool { return strings.HasSuffix(jid, "@"+types.NewsletterServer) }

func (p *Prepared) build(caption string) *waE2E.Message {
	u, m := p.upload, p.Media
	var key []byte
	var encSHA []byte
	if !p.channel {
		key, encSHA = u.MediaKey, u.FileEncSHA256
	}
	cap := proto.String(caption)
	if caption == "" {
		cap = nil
	}
	switch p.Kind {
	case "image":
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: proto.String(u.URL), DirectPath: proto.String(u.DirectPath), MediaKey: key,
			Mimetype: proto.String(m.Mime), FileEncSHA256: encSHA, FileSHA256: u.FileSHA256,
			FileLength: proto.Uint64(u.FileLength), Caption: cap, JPEGThumbnail: m.Thumb,
			Width: u32(m.Width), Height: u32(m.Height),
		}}
	case "video":
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: proto.String(u.URL), DirectPath: proto.String(u.DirectPath), MediaKey: key,
			Mimetype: proto.String(m.Mime), FileEncSHA256: encSHA, FileSHA256: u.FileSHA256,
			FileLength: proto.Uint64(u.FileLength), Caption: cap, JPEGThumbnail: m.Thumb,
			Seconds: u32(m.Seconds), Width: u32(m.Width), Height: u32(m.Height),
		}}
	case "voice", "audio":
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL: proto.String(u.URL), DirectPath: proto.String(u.DirectPath), MediaKey: key,
			Mimetype: proto.String(m.Mime), FileEncSHA256: encSHA, FileSHA256: u.FileSHA256,
			FileLength: proto.Uint64(u.FileLength), Seconds: u32(m.Seconds),
			PTT: proto.Bool(p.Kind == "voice"), Waveform: m.Waveform,
		}}
	default:
		mimeType := m.Mime
		if mimeType == "" {
			mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(p.fileName)))
		}
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL: proto.String(u.URL), DirectPath: proto.String(u.DirectPath), MediaKey: key,
			Mimetype: proto.String(mimeType), FileEncSHA256: encSHA, FileSHA256: u.FileSHA256,
			FileLength: proto.Uint64(u.FileLength), FileName: proto.String(p.fileName),
			Title: proto.String(p.fileName), Caption: cap,
		}}
	}
}

func u32(v int) *uint32 {
	if v <= 0 {
		return nil
	}
	return proto.Uint32(uint32(v))
}
