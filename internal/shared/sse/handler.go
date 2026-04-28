package sse

import (
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
)

// Handler returns an http.HandlerFunc that streams SSE events to the client.
func Handler(broker *Broker, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

		ch := broker.Subscribe()
		defer broker.Unsubscribe(ch)

		// Send an initial comment so the client knows the connection is alive.
		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		log.Debug().Msg("sse client connected")

		for {
			select {
			case <-r.Context().Done():
				log.Debug().Msg("sse client disconnected")
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data)
				flusher.Flush()
			}
		}
	}
}
