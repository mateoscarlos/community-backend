package sse

import "encoding/json"

// Event types.
const (
	EventTileLocked    = "tile_locked"
	EventTileFreed     = "tile_freed"
	EventTileDrawn     = "tile_drawn"
	EventPhaseComplete = "phase_complete"
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
	GameType  string `json:"game_type"`  // Which parallel game this event belongs to.
	Phase     int    `json:"phase"`      // The phase that was just completed.
	NextPhase int    `json:"next_phase"` // 0 if period is fully completed.
	Completed bool   `json:"completed"`  // True if all phases are done.
}

// PublishPhaseComplete notifies all clients that a phase has finished.
// gameType lets clients filter — without it the prompt-game advancing would
// trigger "phase complete" overlays for photo-game players.
func (b *Broker) PublishPhaseComplete(gameType string, completedPhase, nextPhase int, periodCompleted bool) {
	data, _ := json.Marshal(PhaseCompleteEvent{
		GameType:  gameType,
		Phase:     completedPhase,
		NextPhase: nextPhase,
		Completed: periodCompleted,
	})
	b.Publish(Event{Type: EventPhaseComplete, Data: data})
}
