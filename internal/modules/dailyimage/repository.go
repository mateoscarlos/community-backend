package dailyimage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dailyimagedb "github.com/community-app/community-backend/internal/modules/dailyimage/repository/db"
)

// ErrNotFound is returned when a requested daily image does not exist.
var ErrNotFound = errors.New("daily image not found")

// Repository defines the data access contract for daily images.
type Repository interface {
	GetActive(ctx context.Context) (*DailyImage, error)
	GetByDate(ctx context.Context, date time.Time) (*DailyImage, error)
	SetActive(ctx context.Context, date time.Time, storageKey string, width, height int) (*DailyImage, error)
}

type postgresRepository struct {
	queries *dailyimagedb.Queries
}

// NewPostgresRepository returns a Repository backed by PostgreSQL via SQLC.
func NewPostgresRepository(sqlDB *sql.DB) Repository {
	return &postgresRepository{queries: dailyimagedb.New(sqlDB)}
}

func (r *postgresRepository) GetActive(ctx context.Context) (*DailyImage, error) {
	row, err := r.queries.GetActiveDailyImage(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get active daily image: %w", err)
	}
	return toDomain(row), nil
}

func (r *postgresRepository) GetByDate(ctx context.Context, date time.Time) (*DailyImage, error) {
	row, err := r.queries.GetDailyImageByDate(ctx, date)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get daily image by date: %w", err)
	}
	return toDomain(row), nil
}

func (r *postgresRepository) SetActive(ctx context.Context, date time.Time, storageKey string, width, height int) (*DailyImage, error) {
	if err := r.queries.DeactivateAllDailyImages(ctx); err != nil {
		return nil, fmt.Errorf("deactivate daily images: %w", err)
	}
	row, err := r.queries.UpsertDailyImage(ctx, dailyimagedb.UpsertDailyImageParams{
		Date:       date,
		StorageKey: storageKey,
		Width:      int32(width),
		Height:     int32(height),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert daily image: %w", err)
	}
	return toDomain(row), nil
}

// toDomain maps the SQLC-generated type to the domain model.
// This is the only place this translation occurs.
func toDomain(row dailyimagedb.DailyImage) *DailyImage {
	return &DailyImage{
		ID:         row.ID,
		Date:       row.Date,
		StorageKey: row.StorageKey,
		Width:      int(row.Width),
		Height:     int(row.Height),
		IsActive:   row.IsActive,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}
