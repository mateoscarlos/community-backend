package sse

import "encoding/json"

// Event types.
const (
	EventTileLocked    = "tile_locked"
	EventTileFreed     = "tile_freed"
	EventTileDrawn     = "tile_drawn"
	EventPhaseComplete = "phase_complete"
	EventPeriodUpdated = "period_updated"
)

// TileEvent is the payload for tile state change events.
type TileEvent struct {
	TileID   string `json:"tile_id"`
	Status   string `json:"status"`
	ImageURL string `json:"image_url,omitempty"`
}

// PublishTileEvent is a convenience method to publish a tile state change.
func (b *Broker) PublishTileEvent(eventType, tileID, status string) {
	b.PublishTileEventWithImage(eventType, tileID, status, "")
}

// PublishTileEventWithImage publishes a tile event that includes an image URL (for drawn tiles).
func (b *Broker) PublishTileEventWithImage(eventType, tileID, status, imageURL string) {
	data, _ := json.Marshal(TileEvent{
		TileID:   tileID,
		Status:   status,
		ImageURL: imageURL,
	})
	b.Publish(Event{Type: eventType, Data: data})
}

// PhaseCompleteEvent is the payload for phase completion events.
type PhaseCompleteEvent struct {
	Phase     int  `json:"phase"`      // The phase that was just completed.
	NextPhase int  `json:"next_phase"` // 0 if period is fully completed.
	Completed bool `json:"completed"`  // True if all phases are done.
}

// PublishPhaseComplete notifies all clients that a phase has finished.
func (b *Broker) PublishPhaseComplete(completedPhase, nextPhase int, periodCompleted bool) {
	data, _ := json.Marshal(PhaseCompleteEvent{
		Phase:     completedPhase,
		NextPhase: nextPhase,
		Completed: periodCompleted,
	})
	b.Publish(Event{Type: EventPhaseComplete, Data: data})
}

// PeriodUpdatedEvent signals that the active period's metadata changed in
// place and clients should refetch.
type PeriodUpdatedEvent struct{}

// PublishPeriodUpdated tells clients to refetch the current period — used
// when the period changes without a tile/phase transition (e.g. the admin
// flipped outer_tile_display).
func (b *Broker) PublishPeriodUpdated() {
	data, _ := json.Marshal(PeriodUpdatedEvent{})
	b.Publish(Event{Type: EventPeriodUpdated, Data: data})
}
