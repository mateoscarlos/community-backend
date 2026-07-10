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

// GameTypePhoto is the sole game type. The prompt-based parallel game was
// removed as part of the concentric-ring rework (see
// docs/game-model-rework.md). The DB column stays for archive compatibility
// but is pinned to 'photo' — never write anything else.
const GameTypePhoto = "photo"

type Period struct {
	ID            uuid.UUID
	DailyImageID  uuid.UUID // always set — every period anchors to a daily image
	GameType      string
	Status        PeriodStatus
	Phase         int
	FinalGridSize int
	StartedAt     time.Time
	EndedAt       *time.Time
	FinalImageKey string // empty if not yet composed
	ComposedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Tile struct {
	ID            uuid.UUID
	PeriodID      uuid.UUID
	// Phase is the phase at which this tile unlocks — the smallest phase whose
	// concentric window covers it. Set once at period creation and never
	// changed. Tiles with Phase > period.Phase are future-locked.
	Phase         int
	RowIndex      int
	ColIndex      int
	Status        TileStatus
	// PhaseLocked is TRUE while the tile's ring hasn't unlocked yet. Flipped
	// to FALSE on phase advance. Frontends render `PhaseLocked` differently
	// from regular `locked` status (which means "another player is currently
	// claiming this").
	PhaseLocked   bool
	SubmissionKey string // storage key of the drawn image, empty if not drawn
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PhaseMosaic struct {
	PeriodID   uuid.UUID
	Phase      int
	StorageKey string
	ComposedAt time.Time
}
