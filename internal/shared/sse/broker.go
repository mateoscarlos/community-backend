package sse

import (
	"sync"

	"github.com/rs/zerolog"
)

// Event represents a server-sent event.
type Event struct {
	Type string `json:"type"` // e.g. "tile_locked", "tile_freed", "tile_drawn"
	Data []byte `json:"data"` // JSON payload
}

// Broker manages SSE client connections and broadcasts events.
// Clients subscribe via a channel and unsubscribe when done.
// Thread-safe for concurrent publish/subscribe/unsubscribe.
type Broker struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
	log     zerolog.Logger
}

func NewBroker(log zerolog.Logger) *Broker {
	return &Broker{
		clients: make(map[chan Event]struct{}),
		log:     log.With().Str("component", "sse").Logger(),
	}
}

// Subscribe registers a new client and returns its event channel.
// The channel is buffered to avoid blocking the publisher if a client
// is slow. If the buffer fills, that client's event is dropped.
func (b *Broker) Subscribe() chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	count := len(b.clients)
	b.mu.Unlock()
	b.log.Debug().Int("clients", count).Msg("client subscribed")
	return ch
}

// Unsubscribe removes a client and closes its channel.
func (b *Broker) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.clients, ch)
	count := len(b.clients)
	b.mu.Unlock()
	close(ch)
	b.log.Debug().Int("clients", count).Msg("client unsubscribed")
}

// Publish sends an event to all connected clients.
// Non-blocking: if a client's buffer is full, the event is dropped for that client.
func (b *Broker) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- e:
		default:
			// Client too slow, drop event. They'll catch up on next poll or event.
		}
	}
}

// ClientCount returns the number of connected clients.
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}
