package submission

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotYourClaim      = errors.New("claim does not belong to this session")
	ErrSubmissionExists  = errors.New("tile already has a submission")
	ErrSubmissionNotFound = errors.New("submission not found")
)

type Submission struct {
	ID         uuid.UUID
	TileID     uuid.UUID
	ClaimID    uuid.UUID
	StorageKey string
	CropX      float64
	CropY      float64
	CropWidth  float64
	CropHeight float64
	CreatedAt  time.Time
}

type Crop struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
