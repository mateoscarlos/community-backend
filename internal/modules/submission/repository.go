package submission

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	submissiondb "github.com/community-app/community-backend/internal/modules/submission/repository/db"
)

type Repository interface {
	// SubmitTile atomically verifies claim ownership, creates submission,
	// marks tile as drawn, and releases the claim.
	SubmitTile(ctx context.Context, tileID uuid.UUID, sessionID, storageKey string, crop Crop) (*Submission, error)

	GetByTileID(ctx context.Context, tileID uuid.UUID) (*Submission, error)
}

type postgresRepository struct {
	db      *sql.DB
	queries *submissiondb.Queries
}

func NewPostgresRepository(sqlDB *sql.DB) Repository {
	return &postgresRepository{db: sqlDB, queries: submissiondb.New(sqlDB)}
}

// SubmitTile calls the submit_tile() PL/pgSQL function which handles
// everything atomically under a row lock.
func (r *postgresRepository) SubmitTile(ctx context.Context, tileID uuid.UUID, sessionID, storageKey string, crop Crop) (*Submission, error) {
	var s Submission
	err := r.db.QueryRowContext(ctx,
		`SELECT submission_id, tile_id, claim_id, storage_key, created_at
		 FROM submit_tile($1, $2, $3, $4, $5, $6, $7)`,
		tileID, sessionID, storageKey, crop.X, crop.Y, crop.Width, crop.Height,
	).Scan(&s.ID, &s.TileID, &s.ClaimID, &s.StorageKey, &s.CreatedAt)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "tile_not_found") || strings.Contains(msg, "tile_not_locked") {
			return nil, ErrSubmissionNotFound
		}
		if strings.Contains(msg, "not_your_claim") {
			return nil, ErrNotYourClaim
		}
		return nil, fmt.Errorf("submit tile: %w", err)
	}
	return &s, nil
}

func (r *postgresRepository) GetByTileID(ctx context.Context, tileID uuid.UUID) (*Submission, error) {
	row, err := r.queries.GetSubmissionByTileID(ctx, tileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubmissionNotFound
		}
		return nil, fmt.Errorf("get submission by tile: %w", err)
	}
	return toDomain(row), nil
}

func toDomain(row submissiondb.Submission) *Submission {
	return &Submission{
		ID:         row.ID,
		TileID:     row.TileID,
		ClaimID:    row.ClaimID,
		StorageKey: row.StorageKey,
		CropX:      row.CropX,
		CropY:      row.CropY,
		CropWidth:  row.CropWidth,
		CropHeight: row.CropHeight,
		CreatedAt:  row.CreatedAt,
	}
}
