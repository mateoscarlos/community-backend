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

type Period struct {
	ID           uuid.UUID
	DailyImageID uuid.UUID
	GameType     string
	Status       PeriodStatus
	Phase        int
	StartedAt    time.Time
	EndedAt      *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
