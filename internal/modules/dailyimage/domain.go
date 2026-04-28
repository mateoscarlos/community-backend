package dailyimage

import (
	"time"

	"github.com/google/uuid"
)

// DailyImage is the domain model for the image of the day.
// It is the only type that crosses layer boundaries within this module.
type DailyImage struct {
	ID         uuid.UUID
	Date       time.Time
	StorageKey string
	Width      int
	Height     int
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
