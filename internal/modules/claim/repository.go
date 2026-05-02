package claim

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	claimdb "github.com/community-app/community-backend/internal/modules/claim/repository/db"
)

type Repository interface {
	// ClaimTile atomically locks a free tile and creates a claim.
	// Returns ErrTileNotFree if another user got there first.
	ClaimTile(ctx context.Context, tileID uuid.UUID, nickname, sessionID string, expiresAt time.Time) (*Claim, error)

	// ReleaseClaim atomically releases a claim and frees the tile.
	ReleaseClaim(ctx context.Context, tileID uuid.UUID, sessionID string) error

	// SweepExpiredClaims atomically releases all expired claims and frees their tiles.
	// Returns the IDs of tiles that were freed.
	SweepExpiredClaims(ctx context.Context) ([]uuid.UUID, error)

	GetClaimByTileID(ctx context.Context, tileID uuid.UUID) (*Claim, error)
	GetActiveClaimBySessionAndTile(ctx context.Context, tileID uuid.UUID, sessionID string) (*Claim, error)
	HasActiveClaimForSession(ctx context.Context, sessionID string) (bool, error)

	// HeartbeatClaim refreshes last_heartbeat_at and returns expires_at.
	// Returns ErrClaimNotFound if no active claim matches.
	HeartbeatClaim(ctx context.Context, tileID uuid.UUID, sessionID string) (time.Time, error)

	// ExtendClaim pushes expires_at forward by extraSeconds, capped at
	// claimed_at + maxTotalSeconds. Returns the new expires_at.
	ExtendClaim(ctx context.Context, tileID uuid.UUID, sessionID string, extraSeconds, maxTotalSeconds int) (time.Time, error)
}

type postgresRepository struct {
	db      *sql.DB
	queries *claimdb.Queries
}

func NewPostgresRepository(sqlDB *sql.DB) Repository {
	return &postgresRepository{db: sqlDB, queries: claimdb.New(sqlDB)}
}

// ClaimTile calls the claim_tile() PL/pgSQL function which does everything
// atomically: SELECT FOR UPDATE on the tile, check status=free, INSERT claim,
// UPDATE tile to locked. If two users race, the loser blocks on the row lock
// and then sees status=locked → gets tile_not_free.
func (r *postgresRepository) ClaimTile(ctx context.Context, tileID uuid.UUID, nickname, sessionID string, expiresAt time.Time) (*Claim, error) {
	var c Claim
	err := r.db.QueryRowContext(ctx,
		`SELECT claim_id, tile_id, nickname, session_id, claimed_at, expires_at
		 FROM claim_tile($1, $2, $3, $4)`,
		tileID, nickname, sessionID, expiresAt,
	).Scan(&c.ID, &c.TileID, &c.Nickname, &c.SessionID, &c.ClaimedAt, &c.ExpiresAt)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "tile_not_found") {
			return nil, ErrClaimNotFound
		}
		if strings.Contains(msg, "tile_not_free") {
			return nil, ErrAlreadyClaimed
		}
		return nil, fmt.Errorf("claim tile: %w", err)
	}
	return &c, nil
}

// ReleaseClaim calls the release_claim() PL/pgSQL function.
func (r *postgresRepository) ReleaseClaim(ctx context.Context, tileID uuid.UUID, sessionID string) error {
	var released bool
	err := r.db.QueryRowContext(ctx,
		`SELECT release_claim($1, $2)`,
		tileID, sessionID,
	).Scan(&released)
	if err != nil {
		return fmt.Errorf("release claim: %w", err)
	}
	if !released {
		return ErrNotYourClaim
	}
	return nil
}

// SweepExpiredClaims calls the sweep_expired_claims() PL/pgSQL function.
// Returns the IDs of tiles that were freed.
func (r *postgresRepository) SweepExpiredClaims(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT sweep_expired_claims()`)
	if err != nil {
		return nil, fmt.Errorf("sweep expired claims: %w", err)
	}
	defer rows.Close()

	var tileIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("sweep scan tile id: %w", err)
		}
		tileIDs = append(tileIDs, id)
	}
	return tileIDs, rows.Err()
}

func (r *postgresRepository) GetClaimByTileID(ctx context.Context, tileID uuid.UUID) (*Claim, error) {
	row, err := r.queries.GetClaimByTileID(ctx, tileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClaimNotFound
		}
		return nil, fmt.Errorf("get claim by tile: %w", err)
	}
	return toDomain(row), nil
}

func (r *postgresRepository) GetActiveClaimBySessionAndTile(ctx context.Context, tileID uuid.UUID, sessionID string) (*Claim, error) {
	row, err := r.queries.GetActiveClaimBySessionAndTile(ctx, claimdb.GetActiveClaimBySessionAndTileParams{
		TileID:    tileID,
		SessionID: sessionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClaimNotFound
		}
		return nil, fmt.Errorf("get claim by session and tile: %w", err)
	}
	return toDomain(row), nil
}

func (r *postgresRepository) HeartbeatClaim(ctx context.Context, tileID uuid.UUID, sessionID string) (time.Time, error) {
	var claimID uuid.UUID
	var expiresAt time.Time
	err := r.db.QueryRowContext(ctx,
		`SELECT claim_id, expires_at FROM heartbeat_claim($1, $2)`,
		tileID, sessionID,
	).Scan(&claimID, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrClaimNotFound
		}
		return time.Time{}, fmt.Errorf("heartbeat claim: %w", err)
	}
	return expiresAt, nil
}

func (r *postgresRepository) ExtendClaim(ctx context.Context, tileID uuid.UUID, sessionID string, extraSeconds, maxTotalSeconds int) (time.Time, error) {
	var claimID uuid.UUID
	var expiresAt time.Time
	err := r.db.QueryRowContext(ctx,
		`SELECT claim_id, expires_at FROM extend_claim($1, $2, $3, $4)`,
		tileID, sessionID, extraSeconds, maxTotalSeconds,
	).Scan(&claimID, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrClaimNotFound
		}
		return time.Time{}, fmt.Errorf("extend claim: %w", err)
	}
	return expiresAt, nil
}

func (r *postgresRepository) HasActiveClaimForSession(ctx context.Context, sessionID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM claims
			WHERE session_id = $1
			  AND released_at IS NULL
			  AND expires_at > NOW()
		)`,
		sessionID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has active claim for session: %w", err)
	}
	return exists, nil
}

func toDomain(row claimdb.Claim) *Claim {
	c := &Claim{
		ID:        row.ID,
		TileID:    row.TileID,
		Nickname:  row.Nickname,
		SessionID: row.SessionID,
		ClaimedAt: row.ClaimedAt,
		ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt,
	}
	if row.ReleasedAt.Valid {
		c.ReleasedAt = &row.ReleasedAt.Time
	}
	return c
}
