// Package broadcast provides a generic Server-Sent Events broadcaster.
// Multiple handlers can share a single broadcaster to push real-time updates.
package broadcast

import (
	"fmt"
	"net/http"
	"sync"
)

// SSE is a thread-safe broadcaster that fans out string messages to all registered SSE clients.
type SSE struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

// New creates a new SSE broadcaster.
func New() *SSE {
	return &SSE{clients: make(map[chan string]struct{})}
}

// Send publishes a message to all connected clients (non-blocking, drops slow clients).
func (b *SSE) Send(msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default: // drop if client is too slow
		}
	}
}

func (b *SSE) register(ch chan string) {
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
}

func (b *SSE) deregister(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

// ServeHTTP upgrades the request to an SSE stream.
// initial() is called once to send existing data to the new client before entering the event loop.
func (b *SSE) ServeHTTP(w http.ResponseWriter, r *http.Request, initial func() []string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send historical / initial data
	for _, msg := range initial() {
		fmt.Fprintf(w, "data: %s\n\n", msg)
	}
	flusher.Flush()

	ch := make(chan string, 64)
	b.register(ch)
	defer b.deregister(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
