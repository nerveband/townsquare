package tg

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"

	"github.com/gotd/td/telegram/uploader"
	tgapi "github.com/gotd/td/tg"
)

var reMarks = regexp.MustCompile("[*_~`]")

// sendStory posts the first photo or video as a story (24 hours, visible to everyone
// who can see your stories, or the channel's subscribers). Stories need media.
func (cl *Client) sendStory(ctx context.Context, peer tgapi.InputPeerClass, caption string, media []Media) (int, error) {
	var m *Media
	for i := range media {
		if media[i].Kind == "image" || media[i].Kind == "video" {
			m = &media[i]
			break
		}
	}
	if m == nil {
		return 0, errors.New("a story needs a photo or video")
	}
	api := cl.API()
	f, err := uploader.NewUploader(api).FromPath(ctx, m.Path)
	if err != nil {
		return 0, fmt.Errorf("upload %s: %w", m.Name, err)
	}
	var in tgapi.InputMediaClass = &tgapi.InputMediaUploadedPhoto{File: f}
	if m.Kind == "video" {
		in = &tgapi.InputMediaUploadedDocument{File: f, MimeType: "video/mp4", Attributes: []tgapi.DocumentAttributeClass{
			&tgapi.DocumentAttributeVideo{SupportsStreaming: true, Duration: float64(m.Seconds), W: m.Width, H: m.Height},
			&tgapi.DocumentAttributeFilename{FileName: m.Name},
		}}
	}
	var r [8]byte
	_, _ = rand.Read(r[:])
	req := &tgapi.StoriesSendStoryRequest{
		Peer: peer, Media: in, Caption: reMarks.ReplaceAllString(caption, ""),
		PrivacyRules: []tgapi.InputPrivacyRuleClass{&tgapi.InputPrivacyValueAllowAll{}},
		RandomID:     int64(binary.LittleEndian.Uint64(r[:])), Period: 86400,
	}
	u, err := api.StoriesSendStory(ctx, req)
	if err != nil {
		return 0, err
	}
	if up, ok := u.(*tgapi.Updates); ok {
		for _, x := range up.Updates {
			if s, ok := x.(*tgapi.UpdateStory); ok {
				return s.Story.GetID(), nil
			}
			if s, ok := x.(*tgapi.UpdateStoryID); ok {
				return s.ID, nil
			}
		}
	}
	return 0, nil
}
