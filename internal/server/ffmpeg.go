package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nerveband/townsquare/internal/wa"
)

// ffmpegCheckEvery is how often the server re-tests ffmpeg in the background, so a
// broken ffmpeg shows up in status and doctor before someone's upload fails.
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

// ffmpegLast is the latest check, or nil before the first one has finished.
func (s *Server) ffmpegLast() *wa.FFmpegStatus {
	s.ff.mu.Lock()
	defer s.ff.mu.Unlock()
	return s.ff.last
}

// runFFmpegChecks tests ffmpeg at start-up and then every ffmpegCheckEvery.
func (s *Server) runFFmpegChecks(ctx context.Context) {
	t := time.NewTicker(ffmpegCheckEvery)
	defer t.Stop()
	for {
		s.checkFFmpeg(ctx)
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
