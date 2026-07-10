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

// stagingKey is the deterministic R2 key used to hand a raw phone-camera
// photo to the laptop that's driving the game. Keyed by (tile, session) so
// the laptop can locate it without an extra round-trip.
func stagingKey(tileID uuid.UUID, sessionID string) string {
	return fmt.Sprintf("staging/%s-%s.jpg", tileID.String(), sessionID)
}

// PresignStage returns a presigned PUT URL the phone uses to drop a raw
// photo at the staging key. The laptop later picks it up via GetStaged.
func (s *Service) PresignStage(ctx context.Context, tileID uuid.UUID, sessionID string) (uploadURL, key string, err error) {
	key = stagingKey(tileID, sessionID)
	uploadURL, err = s.store.PresignedPutURL(ctx, key, 5*time.Minute)
	if err != nil {
		return "", "", fmt.Errorf("submission service: presign stage: %w", err)
	}
	return uploadURL, key, nil
}

// GetStaged returns a presigned download URL for a staged raw photo if one
// has been uploaded for this (tile, session). exists=false means the laptop
// should keep polling.
func (s *Service) GetStaged(ctx context.Context, tileID uuid.UUID, sessionID string) (downloadURL string, exists bool, err error) {
	key := stagingKey(tileID, sessionID)
	exists, _, err = s.store.StatObject(ctx, key)
	if err != nil {
		return "", false, fmt.Errorf("submission service: stat stage: %w", err)
	}
	if !exists {
		return "", false, nil
	}
	downloadURL, err = s.store.PresignedGetURL(ctx, key, 15*time.Minute)
	if err != nil {
		return "", false, fmt.Errorf("submission service: presign get stage: %w", err)
	}
	return downloadURL, true, nil
}

// deleteStaged removes the staging photo for a (tile, session) — called after
// the laptop has handed in its edited submission. Errors are non-fatal.
func (s *Service) deleteStaged(ctx context.Context, tileID uuid.UUID, sessionID string) {
	key := stagingKey(tileID, sessionID)
	if err := s.store.DeleteObject(ctx, key); err != nil {
		s.log.Warn().Err(err).Str("key", key).Msg("delete staged photo")
	}
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

	// Best-effort cleanup of the raw photo the phone uploaded — only the
	// edited submission is kept long-term.
	s.deleteStaged(ctx, tileID, sessionID)

	// Generate a presigned URL for the drawing so SSE clients can render it immediately.
	imgURL, _ := s.store.PresignedGetURL(ctx, sub.StorageKey, 15*time.Minute)
	s.broker.PublishTileEventWithImage(sse.EventTileDrawn, tileID.String(), "drawn", imgURL)

	// Check if this completes the phase. Errors are logged, not propagated —
	// the submission itself already succeeded. Look up the period via the
	// Trigger phase-advance / masterpiece-complete side effects, if any.
	period, err := s.gridSvc.GetPeriodByTileID(ctx, tileID)
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
