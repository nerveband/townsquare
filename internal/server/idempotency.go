package server

import (
	"bytes"
	"errors"
	"net/http"
	"sync"

	"github.com/nerveband/townsquare/internal/store"
)

// idempotent honors an Idempotency-Key header on v1 writes: the first response
// for a key (per API key) is stored for 24 hours and replayed for repeats, with
// "Idempotent-Replayed: true". The same key on a different request is a 409.
// Server errors (5xx) aren't stored, so those can be retried with the same key.
func (s *Server) idempotent(next http.Handler) http.Handler {
	var mu sync.Mutex
	inflight := map[string]*sync.Mutex{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		k := keyOf(r)
		if key == "" || r.Method == http.MethodGet || k == nil {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 200 {
			failCode(w, 400, "bad_request", errors.New("Idempotency-Key is at most 200 characters"))
			return
		}
		// One request per key at a time, so a fast retry waits for the first.
		mu.Lock()
		l, ok := inflight[key]
		if !ok {
			l = &sync.Mutex{}
			inflight[key] = l
		}
		mu.Unlock()
		l.Lock()
		defer l.Unlock()

		ctx := r.Context()
		if rec, err := s.DB.IdemGet(ctx, key, k.ID); err == nil && rec != nil {
			if rec.Method != r.Method || rec.Path != r.URL.Path {
				failCode(w, 409, "conflict", errors.New("this Idempotency-Key was already used for "+rec.Method+" "+rec.Path))
				return
			}
			if rec.ContentType != "" {
				w.Header().Set("Content-Type", rec.ContentType)
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(rec.Status)
			_, _ = w.Write(rec.Body)
			return
		}
		rw := &recorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if rw.status < 500 {
			s.DB.IdemPut(ctx, key, k.ID, store.IdemRecord{Method: r.Method, Path: r.URL.Path, Status: rw.status,
				Body: rw.buf.Bytes(), ContentType: rw.Header().Get("Content-Type")})
		}
	})
}

type recorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (r *recorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }
func (r *recorder) Write(b []byte) (int, error) {
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}
