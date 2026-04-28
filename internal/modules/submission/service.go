package submission

import (
	"context"
	"fmt"
	"time"

	"github.com/community-app/community-backend/internal/modules/grid"
	"github.com/community-app/community-backend/internal/shared/sse"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Service struct {
	repo    Repository
	gridSvc *grid.Service
	store   *storage.Storage
	broker  *sse.Broker
	log     zerolog.Logger
}

func NewService(
	repo Repository,
	gridSvc *grid.Service,
	store *storage.Storage,
	broker *sse.Broker,
	log zerolog.Logger,
) *Service {
	return &Service{
		repo:    repo,
		gridSvc: gridSvc,
		store:   store,
		broker:  broker,
		log:     log,
	}
}

// Presign generates a presigned upload URL for a tile submission.
func (s *Service) Presign(ctx context.Context, tileID uuid.UUID, contentType string) (uploadURL, storageKey string, err error) {
	storageKey = fmt.Sprintf("tiles/%s.jpg", tileID.String())
	uploadURL, err = s.store.PresignedPutURL(ctx, storageKey, 5*time.Minute)
	if err != nil {
		return "", "", fmt.Errorf("submission service: presign: %w", err)
	}
	return uploadURL, storageKey, nil
}

// Submit atomically creates a submission, marks the tile as drawn, and releases
// the claim. Then checks if the phase is now complete.
func (s *Service) Submit(ctx context.Context, tileID uuid.UUID, sessionID, storageKey string, crop Crop) (*Submission, error) {
	sub, err := s.repo.SubmitTile(ctx, tileID, sessionID, storageKey, crop)
	if err != nil {
		return nil, fmt.Errorf("submission service: %w", err)
	}

	s.log.Info().
		Str("tile_id", tileID.String()).
		Str("storage_key", storageKey).
		Msg("tile submitted")

	// Generate a presigned URL for the drawing so SSE clients can render it immediately.
	imgURL, _ := s.store.PresignedGetURL(ctx, sub.StorageKey, 15*time.Minute)
	s.broker.PublishTileEventWithImage(sse.EventTileDrawn, tileID.String(), "drawn", imgURL)

	// Check if this completes the phase. Errors are logged, not propagated —
	// the submission itself already succeeded.
	period, err := s.gridSvc.GetActivePeriod(ctx)
	if err == nil {
		result, err := s.gridSvc.CheckPhaseCompletion(ctx, period.ID)
		if err != nil {
			s.log.Error().Err(err).Msg("check phase completion after submission")
		} else {
			switch result {
			case grid.PhaseAdvanced:
				s.broker.PublishPhaseComplete(period.Phase, period.Phase+1, false)
			case grid.PhaseAllComplete:
				s.broker.PublishPhaseComplete(period.Phase, 0, true)
			}
		}
	}

	return sub, nil
}
