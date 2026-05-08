package dailyimage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	dailyimagedb "github.com/community-app/community-backend/internal/modules/dailyimage/repository/db"
)

// ErrNotFound is returned when a requested daily image does not exist.
var ErrNotFound = errors.New("daily image not found")

// Repository defines the data access contract for daily images.
type Repository interface {
	GetActive(ctx context.Context) (*DailyImage, error)
	GetByDate(ctx context.Context, date time.Time) (*DailyImage, error)
	GetByID(ctx context.Context, id uuid.UUID) (*DailyImage, error)
	SetActive(ctx context.Context, date time.Time, storageKey string, width, height int) (*DailyImage, error)

	// --- Image schedule (pre-uploaded images keyed by date) ---
	UpsertScheduled(ctx context.Context, date time.Time, storageKey string, width, height int) (*ScheduledImage, error)
	GetScheduledByDate(ctx context.Context, date time.Time) (*ScheduledImage, error)
	ListSchedule(ctx context.Context, from, to time.Time) ([]ScheduledImage, error)
	DeleteScheduledByDate(ctx context.Context, date time.Time) error

	// --- Prompt schedule (parallel prompt-based game) ---
	UpsertScheduledPrompt(ctx context.Context, date time.Time, prompt string) (*ScheduledPrompt, error)
	GetScheduledPromptByDate(ctx context.Context, date time.Time) (*ScheduledPrompt, error)
	ListPromptSchedule(ctx context.Context, from, to time.Time) ([]ScheduledPrompt, error)
	DeleteScheduledPromptByDate(ctx context.Context, date time.Time) error
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

func (r *postgresRepository) GetByID(ctx context.Context, id uuid.UUID) (*DailyImage, error) {
	row, err := r.queries.GetDailyImageByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get daily image by id: %w", err)
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

func (r *postgresRepository) UpsertScheduled(ctx context.Context, date time.Time, storageKey string, width, height int) (*ScheduledImage, error) {
	row, err := r.queries.UpsertScheduledImage(ctx, dailyimagedb.UpsertScheduledImageParams{
		Date:       date,
		StorageKey: storageKey,
		Width:      int32(width),
		Height:     int32(height),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert scheduled image: %w", err)
	}
	return scheduledRowToDomain(row), nil
}

func (r *postgresRepository) GetScheduledByDate(ctx context.Context, date time.Time) (*ScheduledImage, error) {
	row, err := r.queries.GetScheduledByDate(ctx, date)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get scheduled image: %w", err)
	}
	return scheduledRowToDomain(row), nil
}

func (r *postgresRepository) ListSchedule(ctx context.Context, from, to time.Time) ([]ScheduledImage, error) {
	rows, err := r.queries.ListScheduleRange(ctx, dailyimagedb.ListScheduleRangeParams{
		Date:   from,
		Date_2: to,
	})
	if err != nil {
		return nil, fmt.Errorf("list schedule: %w", err)
	}
	out := make([]ScheduledImage, len(rows))
	for i, row := range rows {
		out[i] = *scheduledRowToDomain(row)
	}
	return out, nil
}

func (r *postgresRepository) DeleteScheduledByDate(ctx context.Context, date time.Time) error {
	if err := r.queries.DeleteScheduledByDate(ctx, date); err != nil {
		return fmt.Errorf("delete scheduled image: %w", err)
	}
	return nil
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

func (r *postgresRepository) UpsertScheduledPrompt(ctx context.Context, date time.Time, prompt string) (*ScheduledPrompt, error) {
	row, err := r.queries.UpsertScheduledPrompt(ctx, dailyimagedb.UpsertScheduledPromptParams{
		Date:   date,
		Prompt: prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert scheduled prompt: %w", err)
	}
	return scheduledPromptRowToDomain(row), nil
}

func (r *postgresRepository) GetScheduledPromptByDate(ctx context.Context, date time.Time) (*ScheduledPrompt, error) {
	row, err := r.queries.GetScheduledPromptByDate(ctx, date)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get scheduled prompt: %w", err)
	}
	return scheduledPromptRowToDomain(row), nil
}

func (r *postgresRepository) ListPromptSchedule(ctx context.Context, from, to time.Time) ([]ScheduledPrompt, error) {
	rows, err := r.queries.ListPromptScheduleRange(ctx, dailyimagedb.ListPromptScheduleRangeParams{
		Date:   from,
		Date_2: to,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt schedule: %w", err)
	}
	out := make([]ScheduledPrompt, len(rows))
	for i, row := range rows {
		out[i] = *scheduledPromptRowToDomain(row)
	}
	return out, nil
}

func (r *postgresRepository) DeleteScheduledPromptByDate(ctx context.Context, date time.Time) error {
	if err := r.queries.DeleteScheduledPromptByDate(ctx, date); err != nil {
		return fmt.Errorf("delete scheduled prompt: %w", err)
	}
	return nil
}

func scheduledPromptRowToDomain(row dailyimagedb.DailyPromptSchedule) *ScheduledPrompt {
	return &ScheduledPrompt{
		Date:      row.Date,
		Prompt:    row.Prompt,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func scheduledRowToDomain(row dailyimagedb.DailyImageSchedule) *ScheduledImage {
	return &ScheduledImage{
		Date:       row.Date,
		StorageKey: row.StorageKey,
		Width:      int(row.Width),
		Height:     int(row.Height),
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}
