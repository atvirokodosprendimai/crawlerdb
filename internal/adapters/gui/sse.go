package gui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

// SSEBroker manages Server-Sent Events connections.
type SSEBroker struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	logger  *slog.Logger
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker(logger *slog.Logger) *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan []byte]struct{}),
		logger:  logger,
	}
}

// ServeHTTP handles SSE connections.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}()

	// Send initial keepalive.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// Broadcast sends an event to all connected clients.
func (b *SSEBroker) Broadcast(eventType string, payload any) {
	data, err := json.Marshal(map[string]any{
		"type":    eventType,
		"payload": payload,
	})
	if err != nil {
		b.logger.Error("marshal SSE event", "err", err)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.clients {
		select {
		case ch <- data:
		default:
			// Client slow, drop event.
		}
	}
}

// ClientCount returns number of connected clients.
func (b *SSEBroker) ClientCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}
