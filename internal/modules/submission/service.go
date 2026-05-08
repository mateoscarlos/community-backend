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

// CleanupBatchSize caps how many R2 objects we attempt to delete per sweep
// tick. Picked so a worst-case backlog clears within a few ticks without
// blocking other work for too long. Below the 1000-key R2 batch limit so
// each sweep makes at most one round-trip.
const CleanupBatchSize = 500

// CleanupOrphanedTileObjects deletes R2/MinIO objects for tile submissions
// whose phase has already been composed into a mosaic — at that point the
// individual JPGs are no longer needed for display or recompose. The DB rows
// stay for audit; only the backing object is dropped, and the row is
// stamped so we don't re-attempt the delete next tick.
//
// Returns the number of objects pruned this call.
func (s *Service) CleanupOrphanedTileObjects(ctx context.Context) (int, error) {
	candidates, err := s.repo.ListCleanupCandidates(ctx, CleanupBatchSize)
	if err != nil {
		return 0, fmt.Errorf("cleanup: list candidates: %w", err)
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	keys := make([]string, len(candidates))
	keyToID := make(map[string]uuid.UUID, len(candidates))
	for i, c := range candidates {
		keys[i] = c.StorageKey
		keyToID[c.StorageKey] = c.ID
	}

	removedKeys, delErr := s.store.DeleteObjects(ctx, keys)
	if delErr != nil {
		s.log.Warn().Err(delErr).Int("attempted", len(keys)).Msg("cleanup: some deletes failed")
	}

	cleanedIDs := make([]uuid.UUID, 0, len(removedKeys))
	for _, k := range removedKeys {
		if id, ok := keyToID[k]; ok {
			cleanedIDs = append(cleanedIDs, id)
		}
	}
	if err := s.repo.MarkCleaned(ctx, cleanedIDs); err != nil {
		return 0, fmt.Errorf("cleanup: mark cleaned: %w", err)
	}

	s.log.Info().
		Int("pruned", len(cleanedIDs)).
		Int("attempted", len(keys)).
		Msg("storage cleanup")

	return len(cleanedIDs), nil
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
	// the submission itself already succeeded. Look up the period via the
	// tile so we apply phase logic to the correct game (photo vs prompt).
	period, err := s.gridSvc.GetPeriodByTileID(ctx, tileID)
	if err == nil {
		result, err := s.gridSvc.CheckPhaseCompletion(ctx, period.ID)
		if err != nil {
			s.log.Error().Err(err).Msg("check phase completion after submission")
		} else {
			switch result {
			case grid.PhaseAdvanced:
				s.broker.PublishPhaseComplete(period.GameType, period.Phase, period.Phase+1, false)
			case grid.PhaseAllComplete:
				s.broker.PublishPhaseComplete(period.GameType, period.Phase, 0, true)
			}
		}
	}

	return sub, nil
}
