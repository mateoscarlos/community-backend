package grid

import (
	"time"

	"github.com/google/uuid"
)

type TileStatus string

const (
	TileFree   TileStatus = "free"
	TileLocked TileStatus = "locked"
	TileDrawn  TileStatus = "drawn"
)

type PeriodStatus string

const (
	PeriodActive    PeriodStatus = "active"
	PeriodCompleted PeriodStatus = "completed"
	PeriodArchived  PeriodStatus = "archived"
)

// GameType discriminates between the two parallel games:
//   - photo: users copy a daily image, tile by tile (the original game).
//   - prompt: users free-draw on an empty canvas from a text prompt.
type GameType string

const (
	GamePhoto  GameType = "photo"
	GamePrompt GameType = "prompt"
)

func (g GameType) Valid() bool { return g == GamePhoto || g == GamePrompt }

type Period struct {
	ID            uuid.UUID
	DailyImageID  *uuid.UUID // nil for prompt-game periods
	GameType      string
	Status        PeriodStatus
	Phase         int
	StartedAt     time.Time
	EndedAt       *time.Time
	FinalImageKey string // empty if not yet composed
	ComposedAt    *time.Time
	Prompt        string // empty for photo-game periods
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Tile struct {
	ID            uuid.UUID
	PeriodID      uuid.UUID
	Phase         int
	RowIndex      int
	ColIndex      int
	Status        TileStatus
	SubmissionKey string // storage key of the drawn image, empty if not drawn
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type GridConfig struct {
	ID        uuid.UUID
	Phase     int
	Columns   int
	Rows      int
	CreatedAt time.Time
}

type PhaseMosaic struct {
	PeriodID   uuid.UUID
	Phase      int
	StorageKey string
	ComposedAt time.Time
}
