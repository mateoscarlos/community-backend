package dailyimage

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

// Service contains the application logic for daily images.
type Service struct {
	repo Repository
	log  zerolog.Logger
}

func NewService(repo Repository, log zerolog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

// GetActive returns the currently active daily image.
// Returns ErrNotFound if no image is active.
func (s *Service) GetActive(ctx context.Context) (*DailyImage, error) {
	img, err := s.repo.GetActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("dailyimage service: get active: %w", err)
	}
	return img, nil
}
