package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nerveband/townsquare/internal/tg"
)

// Unlink alerts: when WhatsApp ends the link (the phone removed the device, or
// WhatsApp did), tell the owner in their own Telegram Saved Messages. Off by
// default (setting alert_unlink): a test feature until there is a channel that
// doesn't depend on either app.

// unlinkAlert is the message for an unlinked account, or "" when alerts are off.
func (s *Server) unlinkAlert(ctx context.Context, account string) string {
	if s.Demo || s.DB.Setting(ctx, "alert_unlink") != "1" {
		return ""
	}
	zone, err := time.LoadLocation(s.DB.Setting(ctx, "timezone"))
	if err != nil {
		zone = time.UTC
	}
	return fmt.Sprintf("Townsquare: %s was unlinked at %s, so WhatsApp posts can't go out.\n\n"+
		"Link it again in Townsquare: Settings → Manage accounts → Link WhatsApp.",
		account, time.Now().In(zone).Format("Mon 3:04 PM MST"))
}

func (s *Server) alertUnlinked(account string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	msg := s.unlinkAlert(ctx, account)
	if msg == "" {
		return
	}
	if !s.TG.Ready() {
		log.Printf("unlink alert: %s unlinked, but Telegram isn't logged in to tell you", account)
		return
	}
	if _, err := s.TG.Send(ctx, "tg:self", msg, nil, tg.Options{NoPreview: true}); err != nil {
		log.Printf("unlink alert: %v", err)
		return
	}
	s.DB.Log(ctx, "townsquare", "told you on Telegram that "+account+" was unlinked")
}
