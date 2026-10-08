package server

import (
	"net/http"
	"strconv"
)

// pageBounds applies ?limit and ?offset to a list of n items on the server and
// reports the full count in X-Total-Count, so clients page instead of fetching
// everything. limit=0 (or a missing limit) means no limit; ?limit=0&count=1
// style callers read the header from an empty page.
func pageBounds(w http.ResponseWriter, r *http.Request, n int) (lo, hi int) {
	q := r.URL.Query()
	w.Header().Set("X-Total-Count", strconv.Itoa(n))
	lo, hi = 0, n
	if off, err := strconv.Atoi(q.Get("offset")); err == nil && off > 0 {
		lo = min(off, n)
	}
	if lim, err := strconv.Atoi(q.Get("limit")); err == nil && lim >= 0 && q.Get("limit") != "" {
		hi = min(lo+lim, n)
	}
	return lo, hi
}
