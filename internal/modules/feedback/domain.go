package feedback

import (
	"time"

	"github.com/google/uuid"
)

type Feedback struct {
	ID        uuid.UUID
	Message   string
	Contact   string
	Rating    *int // 1 = negative, 2 = neutral, 3 = positive; nil for written notes
	Context   string
	CreatedAt time.Time
}
