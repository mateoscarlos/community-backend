package claim

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTileNotFree    = errors.New("tile is not free")
	ErrAlreadyClaimed = errors.New("tile already claimed")
	ErrNotYourClaim   = errors.New("claim does not belong to this session")
	ErrClaimNotFound  = errors.New("claim not found")
)

type Claim struct {
	ID         uuid.UUID
	TileID     uuid.UUID
	Nickname   string
	SessionID  string
	ClaimedAt  time.Time
	ExpiresAt  time.Time
	ReleasedAt *time.Time
	CreatedAt  time.Time
}
