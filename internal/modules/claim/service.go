package claim

import (
	"context"
	"fmt"
	"time"

	"github.com/community-app/community-backend/internal/shared/sse"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	// DefaultClaimTTL is the initial time the user has to draw their tile.
	DefaultClaimTTL = 10 * time.Minute

	// ExtendClaimDuration is how much time "Need 2 more minutes" buys.
	ExtendClaimDuration = 2 * time.Minute

	// MaxClaimTotalDuration caps the total time a single claim can occupy a
	// tile, even with repeated extensions. Prevents indefinite hogging.
	MaxClaimTotalDuration = 30 * time.Minute
)

type Service struct {
	repo   Repository
	broker *sse.Broker
	log    zerolog.Logger
}

func NewService(repo Repository, broker *sse.Broker, log zerolog.Logger) *Service {
	return &Service{repo: repo, broker: broker, log: log}
}

// ClaimTile atomically locks a free tile for the given session.
// Safe under concurrent access: only one caller wins, the rest get ErrAlreadyClaimed.
// A session may only hold one active claim at a time.
func (s *Service) ClaimTile(ctx context.Context, tileID uuid.UUID, nickname, sessionID string) (*Claim, error) {
	hasClaim, err := s.repo.HasActiveClaimForSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("claim service: check active: %w", err)
	}
	if hasClaim {
		return nil, ErrSessionAlreadyHasClaim
	}

	expiresAt := time.Now().Add(DefaultClaimTTL)

	c, err := s.repo.ClaimTile(ctx, tileID, nickname, sessionID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("claim service: %w", err)
	}

	s.log.Info().
		Str("tile_id", tileID.String()).
		Str("nickname", nickname).
		Time("expires_at", expiresAt).
		Msg("tile claimed")

	s.broker.PublishTileEvent(sse.EventTileLocked, tileID.String(), "locked")

	return c, nil
}

// ReleaseClaim atomically releases a claim and frees the tile.
func (s *Service) ReleaseClaim(ctx context.Context, tileID uuid.UUID, sessionID string) error {
	if err := s.repo.ReleaseClaim(ctx, tileID, sessionID); err != nil {
		return fmt.Errorf("claim service: release: %w", err)
	}
	s.log.Info().Str("tile_id", tileID.String()).Msg("claim released")

	s.broker.PublishTileEvent(sse.EventTileFreed, tileID.String(), "free")

	return nil
}

// Heartbeat refreshes the claim's liveness timestamp. Returns the (unchanged)
// expires_at so the client can keep its countdown in sync. ErrClaimNotFound
// if the claim doesn't exist or doesn't belong to this session.
func (s *Service) Heartbeat(ctx context.Context, tileID uuid.UUID, sessionID string) (time.Time, error) {
	expiresAt, err := s.repo.HeartbeatClaim(ctx, tileID, sessionID)
	if err != nil {
		return time.Time{}, fmt.Errorf("claim service: heartbeat: %w", err)
	}
	return expiresAt, nil
}

// Extend pushes expires_at forward by ExtendClaimDuration, bounded by
// MaxClaimTotalDuration from claimed_at. Returns the new expires_at.
func (s *Service) Extend(ctx context.Context, tileID uuid.UUID, sessionID string) (time.Time, error) {
	expiresAt, err := s.repo.ExtendClaim(ctx, tileID, sessionID,
		int(ExtendClaimDuration.Seconds()),
		int(MaxClaimTotalDuration.Seconds()))
	if err != nil {
		return time.Time{}, fmt.Errorf("claim service: extend: %w", err)
	}
	s.log.Info().
		Str("tile_id", tileID.String()).
		Time("expires_at", expiresAt).
		Msg("claim extended")
	return expiresAt, nil
}

// SweepExpired atomically releases all expired claims and frees their tiles.
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	freedTileIDs, err := s.repo.SweepExpiredClaims(ctx)
	if err != nil {
		return 0, fmt.Errorf("claim service: sweep: %w", err)
	}
	if len(freedTileIDs) > 0 {
		s.log.Info().Int("count", len(freedTileIDs)).Msg("swept expired claims")
		for _, id := range freedTileIDs {
			s.broker.PublishTileEvent(sse.EventTileFreed, id.String(), "free")
		}
	}
	return len(freedTileIDs), nil
}
