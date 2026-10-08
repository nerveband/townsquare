package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/tg"
	"github.com/nerveband/townsquare/internal/tgbot"
	"github.com/nerveband/townsquare/internal/wa"
)

// Telegram: log in as your own account (QR), list your groups and channels,
// and send through internal/tg. Same safety rules as WhatsApp.

// RunBot starts the optional Telegram bot and records chats as it is added to them.
func (s *Server) RunBot(ctx context.Context) {
	if s.Bot == nil {
		return
	}
	s.Bot.Start(ctx)
}

// BotChat stores a chat the bot was added to (or removed from). New chats start
// off the allowlist, like every other chat.
func (s *Server) BotChat(c tgbot.Chat) {
	ctx := context.Background()
	if err := s.DB.UpsertTarget(ctx, "telegram_bot", store.Target{JID: c.JID, Kind: c.Kind, Name: c.Name, CanSend: c.CanSend, Gone: c.Gone}); err != nil {
		log.Println("telegram bot chat:", err)
		return
	}
	log.Printf("telegram bot: %s %q (can post: %v, gone: %v)", c.Kind, c.Name, c.CanSend, c.Gone)
}

// RunTelegram keeps Telegram connected and syncs chats once it is logged in.
func (s *Server) RunTelegram(ctx context.Context) {
	if s.TG == nil {
		return
	}
	go s.TG.Run(ctx)
	synced := false
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	last := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !s.TG.Ready() {
			synced = false
			continue
		}
		if !synced || time.Since(last) > 30*time.Minute {
			if err := s.syncTelegram(ctx); err != nil {
				log.Println("telegram chats:", err)
				continue
			}
			synced, last = true, time.Now()
		}
		s.reconcileTelegramQueue(ctx)
	}
}

func (s *Server) syncTelegram(ctx context.Context) error {
	ts, err := s.TG.Targets(ctx)
	if err != nil {
		return err
	}
	conv := make([]store.Target, 0, len(ts))
	for _, t := range ts {
		conv = append(conv, store.Target{JID: t.JID, Kind: t.Kind, Name: t.Name, Parent: t.Parent, CanSend: t.CanSend, Members: t.Members})
	}
	var had int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM targets WHERE jid='tg:self'`).Scan(&had)
	if err := s.DB.UpsertTargets(ctx, "telegram", conv); err != nil {
		return err
	}
	// Saved Messages is the user's own chat: allow it once so tests work in safe mode.
	// Every other Telegram chat starts off the allowlist.
	if had == 0 {
		_, _ = s.DB.ExecContext(ctx, `UPDATE targets SET allowed=1 WHERE jid='tg:self'`)
	}
	log.Printf("telegram chats: %d synced", len(conv))
	return nil
}

// deliverTelegram sends one occurrence to one Telegram chat.
func (s *Server) deliverTelegram(ctx context.Context, o store.Occurrence, jid string, msgs *[]store.StatMsg) (string, error) {
	if !s.TG.Ready() {
		return "", errors.New("Telegram is not logged in")
	}
	media, err := s.telegramMedia(ctx, o)
	if err != nil {
		return "", err
	}
	ids, err := s.TG.SendIDs(ctx, jid, o.Caption, media, tg.Options{})
	if msgs != nil {
		kind := "text"
		if len(media) > 0 {
			kind = media[0].Kind
		}
		for _, id := range ids {
			*msgs = append(*msgs, store.StatMsg{Platform: "telegram", ID: strconv.Itoa(id), Kind: kind})
		}
	}
	if len(ids) == 0 {
		return "", err
	}
	return strconv.Itoa(ids[0]), err
}

func (s *Server) telegramMedia(ctx context.Context, o store.Occurrence) ([]tg.Media, error) {
	var media []tg.Media
	for _, id := range o.Media {
		m, err := s.DB.Media(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("media %d: %w", id, err)
		}
		tm := tg.Media{Kind: m.Kind, Path: m.Path, Name: filepath.Base(m.Name), Mime: m.Mime, Seconds: m.Seconds, Width: m.Width, Height: m.Height}
		if m.Kind == "voice" {
			if d, err := wa.Describe(ctx, m.Kind, m.Path, m.Mime); err == nil {
				tm.Waveform = tgWaveform(d.Waveform)
			}
		}
		media = append(media, tm)
	}
	return media, nil
}

// tgWaveform packs 0..100 samples into Telegram's 5-bit waveform format.
func tgWaveform(samples []byte) []byte {
	if len(samples) == 0 {
		return nil
	}
	bits := len(samples) * 5
	out := make([]byte, (bits+7)/8)
	for i, v := range samples {
		val := uint(v) * 31 / 100
		for b := 0; b < 5; b++ {
			if val&(1<<b) != 0 {
				pos := i*5 + b
				out[pos/8] |= 1 << (pos % 8)
			}
		}
	}
	return out
}

// ---------- HTTP ----------

func (s *Server) telegramState(w http.ResponseWriter, r *http.Request) {
	st := s.TG.State()
	var chats, allowed int
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(allowed),0) FROM targets WHERE platform='telegram' AND gone=0`).Scan(&chats, &allowed)
	out := map[string]any{"configured": st.Configured, "app_source": st.AppSource, "status": st.Status, "user": st.User, "username": st.Username,
		"error": st.Error, "qr_expires": st.QRExpires, "version": st.Version, "chats": chats, "allowlisted": allowed,
		"queue_hours": atoi(s.DB.Setting(r.Context(), "tg_queue_hours"), 0)}
	if s.Bot != nil {
		var bc int
		_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM targets WHERE platform='telegram_bot' AND gone=0`).Scan(&bc)
		name, user := s.Bot.Info()
		out["bot"] = map[string]any{"name": name, "username": user, "chats": bc}
	}
	writeJSON(w, out)
}

func (s *Server) telegramLogin(w http.ResponseWriter, r *http.Request) {
	if s.TG == nil {
		failCode(w, 409, "not_configured", errors.New("Telegram isn't set up on this server: add telegram.app (api_id and api_hash from my.telegram.org) to the data folder and restart"))
		return
	}
	if s.TG.Ready() {
		failCode(w, 409, "conflict", errors.New("already logged in"))
		return
	}
	if err := s.TG.StartQR(); err != nil {
		failCode(w, 503, "unavailable", err)
		return
	}
	s.DB.Log(r.Context(), actorOf(r), "started a Telegram login")
	time.Sleep(700 * time.Millisecond) // usually enough for the first QR code
	s.telegramState(w, r)
}

func (s *Server) telegramQR(w http.ResponseWriter, r *http.Request) {
	if s.TG == nil || s.TG.QRURL() == "" {
		fail(w, 404, errors.New("no QR code right now"))
		return
	}
	png, err := qrcode.Encode(s.TG.QRURL(), qrcode.Medium, 512)
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) telegramPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if err := readJSON(r, &in); err != nil || in.Password == "" {
		failCode(w, 400, "bad_request", errors.New("password is required"))
		return
	}
	if s.TG == nil {
		fail(w, 409, errors.New("Telegram isn't set up"))
		return
	}
	if err := s.TG.SubmitPassword(in.Password); err != nil {
		failCode(w, 409, "conflict", err)
		return
	}
	time.Sleep(1500 * time.Millisecond)
	s.telegramState(w, r)
}

func (s *Server) telegramLogout(w http.ResponseWriter, r *http.Request) {
	if s.TG == nil {
		fail(w, 409, errors.New("Telegram isn't set up"))
		return
	}
	if err := s.TG.Logout(r.Context()); err != nil {
		log.Println("telegram logout:", err)
	}
	s.DB.Log(r.Context(), actorOf(r), "logged out of Telegram")
	s.telegramState(w, r)
}

func (s *Server) telegramRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.TG.Ready() {
		failCode(w, 503, "unavailable", errors.New("Telegram is not logged in"))
		return
	}
	if err := s.syncTelegram(r.Context()); err != nil {
		fail(w, 500, err)
		return
	}
	s.telegramState(w, r)
}
