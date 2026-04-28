package feedback

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

type Service struct {
	repo Repository
	log  zerolog.Logger
}

func NewService(repo Repository, log zerolog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

func (s *Service) Submit(ctx context.Context, message, contact string) (*Feedback, error) {
	fb, err := s.repo.Create(ctx, message, contact)
	if err != nil {
		return nil, fmt.Errorf("feedback service: submit: %w", err)
	}
	s.log.Info().Str("id", fb.ID.String()).Msg("feedback submitted")
	return fb, nil
}
