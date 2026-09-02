package web

import (
	"net/http"
	"strconv"
	"sync"
)

// historyHub broadcasts "history updated" notifications to open SSE streams
// so the browser can refresh the query-history panel without polling.
// Subscribers are keyed by connection so a query run on one connection only
// refreshes panels watching that connection.
type historyHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan struct{}]struct{}
}

func newHistoryHub() *historyHub {
	return &historyHub{subs: make(map[string]map[chan struct{}]struct{})}
}

// subscribe registers a channel for the given connection and returns it, plus
// a cleanup func to remove it when the client disconnects.
func (h *historyHub) subscribe(conn string) (chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[conn] == nil {
		h.subs[conn] = make(map[chan struct{}]struct{})
	}
	ch := make(chan struct{}, 1)
	h.subs[conn][ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set, ok := h.subs[conn]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, conn)
			}
		}
	}
}

// notify sends a signal to every subscriber of the given connection without
// blocking the caller.
func (h *historyHub) notify(conn string) {
	if conn == "" {
		return
	}
	h.mu.RLock()
	set := h.subs[conn]
	for ch := range set {
		select {
		case ch <- struct{}{}:
		default:
			// subscriber is already pending an update; skip
		}
	}
	h.mu.RUnlock()
}

// handleHistoryStream serves Server-Sent Events for query-history updates on a
// given connection. The client subscribes once and receives a signal whenever
// a new history row is recorded for that connection.
func (s *Server) handleHistoryStream(w http.ResponseWriter, r *http.Request) {
	serverID, _ := strconv.ParseInt(r.URL.Query().Get("server_id"), 10, 64)
	dbName := r.URL.Query().Get("db_name")

	conn := ""
	if c := encodeConnection(serverID, dbName); c.Valid {
		conn = c.String
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsubscribe := s.historyHub.subscribe(conn)
	defer unsubscribe()

	// Send an initial comment to establish the stream.
	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		return
	}
	fl.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if _, err := w.Write([]byte("data: updated\n\n")); err != nil {
				return
			}
			fl.Flush()
		}
	}
}