package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nerveband/townsquare/internal/wa"
)

// ffmpegCheckEvery is how often the server re-tests ffmpeg in the background, so a
// broken ffmpeg shows up in status and doctor before someone's upload fails. It is also
// when a failed ffmpeg download is tried again.
const ffmpegCheckEvery = 10 * time.Minute

// ffmpegState holds the latest ffmpeg check. runMu keeps two checks from running at once.
type ffmpegState struct {
	runMu sync.Mutex
	mu    sync.Mutex
	last  *wa.FFmpegStatus
}

// checkFFmpeg runs the real conversion check now and remembers the result.
func (s *Server) checkFFmpeg(ctx context.Context) wa.FFmpegStatus {
	s.ff.runMu.Lock()
	defer s.ff.runMu.Unlock()
	st := wa.CheckFFmpeg(ctx)
	s.ff.mu.Lock()
	prev := s.ff.last
	s.ff.last = &st
	s.ff.mu.Unlock()
	if !st.OK && (prev == nil || prev.OK || prev.Error != st.Error) {
		log.Println("ffmpeg check failed:", st.Error)
	}
	return st
}

// ffmpegLast is the latest check (with the download state as it is now), or nil before
// the first check has finished.
func (s *Server) ffmpegLast() *wa.FFmpegStatus {
	s.ff.mu.Lock()
	defer s.ff.mu.Unlock()
	if s.ff.last == nil {
		return nil
	}
	st := *s.ff.last
	st.Install = wa.CurrentInstall()
	return &st
}

// runFFmpegChecks tests ffmpeg at start-up and then every ffmpegCheckEvery. When
// Townsquare's own copy isn't downloaded yet it downloads it between two checks, so
// uploads keep working with the system ffmpeg meanwhile. Demo mode never downloads.
func (s *Server) runFFmpegChecks(ctx context.Context) {
	t := time.NewTicker(ffmpegCheckEvery)
	defer t.Stop()
	for {
		s.checkFFmpeg(ctx)
		if !s.Demo && wa.NeedsFFmpegInstall() {
			if err := wa.InstallFFmpeg(ctx); err != nil {
				log.Println("ffmpeg download failed:", err)
			} else {
				log.Println("ffmpeg downloaded to", wa.ManagedFFmpegDir)
			}
			s.checkFFmpeg(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ffmpegNow answers GET /ffmpeg: it runs the check right now (about a second).
func (s *Server) ffmpegNow(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.checkFFmpeg(r.Context()))
}

// ffmpegInstall answers POST /ffmpeg/install: download Townsquare's own ffmpeg now, then
// test it. A download that failed for good (a bad checksum, a build that can't run here)
// is tried again too.
func (s *Server) ffmpegInstall(w http.ResponseWriter, r *http.Request) {
	if s.Demo {
		failCode(w, http.StatusUnprocessableEntity, "unavailable", errors.New("ffmpeg downloads are off in demo mode"))
		return
	}
	if err := wa.InstallFFmpeg(r.Context()); err != nil {
		code := "fetch_failed"
		if errors.Is(err, wa.ErrNoFFmpegDownload) {
			code = "unavailable"
		}
		failCode(w, http.StatusUnprocessableEntity, code, err)
		return
	}
	writeJSON(w, s.checkFFmpeg(r.Context()))
}
