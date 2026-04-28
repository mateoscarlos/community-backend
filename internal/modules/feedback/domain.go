package feedback

import (
	"time"

	"github.com/google/uuid"
)

type Feedback struct {
	ID        uuid.UUID
	Message   string
	Contact   string
	CreatedAt time.Time
}
