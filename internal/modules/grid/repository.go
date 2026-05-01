package grid

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	griddb "github.com/community-app/community-backend/internal/modules/grid/repository/db"
)

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

var (
	ErrPeriodNotFound = errors.New("active period not found")
	ErrGridNotFound   = errors.New("grid config not found")
)

type Repository interface {
	GetActivePeriod(ctx context.Context) (*Period, error)
	GetPeriodByID(ctx context.Context, id uuid.UUID) (*Period, error)
	ListCompletedPeriods(ctx context.Context, limit, offset int) ([]Period, error)
	ListCompletedPeriodsMissingFinalImage(ctx context.Context) ([]Period, error)
	CreatePeriod(ctx context.Context, dailyImageID uuid.UUID, gameType string, status PeriodStatus, phase int) (*Period, error)
	UpdatePeriodPhase(ctx context.Context, id uuid.UUID, phase int) error
	CompletePeriod(ctx context.Context, id uuid.UUID) error
	SetPeriodFinalImage(ctx context.Context, id uuid.UUID, finalImageKey string) error
	GetGridConfig(ctx context.Context, phase int) (*GridConfig, error)
	GetTilesByPeriodAndPhase(ctx context.Context, periodID uuid.UUID, phase int) ([]Tile, error)
	CreateTile(ctx context.Context, periodID uuid.UUID, phase, row, col int) (*Tile, error)
	UpdateTileStatus(ctx context.Context, id uuid.UUID, status TileStatus) error
	CountTilesByStatus(ctx context.Context, periodID uuid.UUID, phase int) (drawnCount, totalCount int64, err error)

	UpsertPeriodMosaic(ctx context.Context, periodID uuid.UUID, phase int, storageKey string) error
	ListMosaicsByPeriod(ctx context.Context, periodID uuid.UUID) ([]PhaseMosaic, error)
}

type postgresRepository struct {
	queries *griddb.Queries
}

func NewPostgresRepository(sqlDB *sql.DB) Repository {
	return &postgresRepository{queries: griddb.New(sqlDB)}
}

func (r *postgresRepository) GetActivePeriod(ctx context.Context) (*Period, error) {
	row, err := r.queries.GetActivePeriod(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPeriodNotFound
		}
		return nil, fmt.Errorf("get active period: %w", err)
	}
	return periodToDomain(row), nil
}

func (r *postgresRepository) GetPeriodByID(ctx context.Context, id uuid.UUID) (*Period, error) {
	row, err := r.queries.GetPeriodByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPeriodNotFound
		}
		return nil, fmt.Errorf("get period by id: %w", err)
	}
	return periodToDomain(row), nil
}

func (r *postgresRepository) ListCompletedPeriods(ctx context.Context, limit, offset int) ([]Period, error) {
	rows, err := r.queries.ListCompletedPeriods(ctx, griddb.ListCompletedPeriodsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("list completed periods: %w", err)
	}
	periods := make([]Period, len(rows))
	for i, row := range rows {
		periods[i] = *periodToDomain(row)
	}
	return periods, nil
}

func (r *postgresRepository) ListCompletedPeriodsMissingFinalImage(ctx context.Context) ([]Period, error) {
	rows, err := r.queries.ListCompletedPeriodsMissingFinalImage(ctx)
	if err != nil {
		return nil, fmt.Errorf("list completed periods missing final image: %w", err)
	}
	periods := make([]Period, len(rows))
	for i, row := range rows {
		periods[i] = *periodToDomain(row)
	}
	return periods, nil
}

func (r *postgresRepository) CreatePeriod(ctx context.Context, dailyImageID uuid.UUID, gameType string, status PeriodStatus, phase int) (*Period, error) {
	row, err := r.queries.CreatePeriod(ctx, griddb.CreatePeriodParams{
		DailyImageID: dailyImageID,
		GameType:     gameType,
		Status:       string(status),
		Phase:        int32(phase),
	})
	if err != nil {
		return nil, fmt.Errorf("create period: %w", err)
	}
	return periodToDomain(row), nil
}

func (r *postgresRepository) UpdatePeriodPhase(ctx context.Context, id uuid.UUID, phase int) error {
	return r.queries.UpdatePeriodPhase(ctx, griddb.UpdatePeriodPhaseParams{ID: id, Phase: int32(phase)})
}

func (r *postgresRepository) CompletePeriod(ctx context.Context, id uuid.UUID) error {
	return r.queries.CompletePeriod(ctx, id)
}

func (r *postgresRepository) SetPeriodFinalImage(ctx context.Context, id uuid.UUID, finalImageKey string) error {
	return r.queries.SetPeriodFinalImage(ctx, griddb.SetPeriodFinalImageParams{
		ID:            id,
		FinalImageKey: nullString(finalImageKey),
	})
}

func (r *postgresRepository) UpsertPeriodMosaic(ctx context.Context, periodID uuid.UUID, phase int, storageKey string) error {
	return r.queries.UpsertPeriodMosaic(ctx, griddb.UpsertPeriodMosaicParams{
		PeriodID:   periodID,
		Phase:      int32(phase),
		StorageKey: storageKey,
	})
}

func (r *postgresRepository) ListMosaicsByPeriod(ctx context.Context, periodID uuid.UUID) ([]PhaseMosaic, error) {
	rows, err := r.queries.ListMosaicsByPeriod(ctx, periodID)
	if err != nil {
		return nil, fmt.Errorf("list mosaics: %w", err)
	}
	out := make([]PhaseMosaic, len(rows))
	for i, row := range rows {
		out[i] = PhaseMosaic{
			PeriodID:   row.PeriodID,
			Phase:      int(row.Phase),
			StorageKey: row.StorageKey,
			ComposedAt: row.ComposedAt,
		}
	}
	return out, nil
}

func (r *postgresRepository) GetGridConfig(ctx context.Context, phase int) (*GridConfig, error) {
	row, err := r.queries.GetGridConfig(ctx, int32(phase))
	if err == nil {
		return gridConfigToDomain(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get grid config: %w", err)
	}

	// No row stored for this phase — fall back to a formula so the grid keeps
	// subdividing forever. Triangular: cols(p) = (p+1)(p+2)/2. Matches the
	// seeded values exactly (p=1→3, p=2→6, p=3→10) and grows quadratically:
	// p=4→15, p=5→21, p=6→28, p=10→66, p=20→231.
	if phase < 1 {
		return nil, ErrGridNotFound
	}
	size := (phase + 1) * (phase + 2) / 2
	return &GridConfig{Phase: phase, Columns: size, Rows: size}, nil
}

func (r *postgresRepository) GetTilesByPeriodAndPhase(ctx context.Context, periodID uuid.UUID, phase int) ([]Tile, error) {
	rows, err := r.queries.GetTilesByPeriodAndPhase(ctx, griddb.GetTilesByPeriodAndPhaseParams{
		PeriodID: periodID,
		Phase:    int32(phase),
	})
	if err != nil {
		return nil, fmt.Errorf("get tiles: %w", err)
	}
	tiles := make([]Tile, len(rows))
	for i, row := range rows {
		tiles[i] = Tile{
			ID:            row.ID,
			PeriodID:      row.PeriodID,
			Phase:         int(row.Phase),
			RowIndex:      int(row.RowIndex),
			ColIndex:      int(row.ColIndex),
			Status:        TileStatus(row.Status),
			SubmissionKey: row.SubmissionKey,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
	}
	return tiles, nil
}

func (r *postgresRepository) CreateTile(ctx context.Context, periodID uuid.UUID, phase, row, col int) (*Tile, error) {
	dbRow, err := r.queries.CreateTile(ctx, griddb.CreateTileParams{
		PeriodID: periodID,
		Phase:    int32(phase),
		RowIndex: int32(row),
		ColIndex: int32(col),
	})
	if err != nil {
		return nil, fmt.Errorf("create tile: %w", err)
	}
	return tileToDomain(dbRow), nil
}

func (r *postgresRepository) UpdateTileStatus(ctx context.Context, id uuid.UUID, status TileStatus) error {
	return r.queries.UpdateTileStatus(ctx, griddb.UpdateTileStatusParams{
		ID:     id,
		Status: string(status),
	})
}

func (r *postgresRepository) CountTilesByStatus(ctx context.Context, periodID uuid.UUID, phase int) (int64, int64, error) {
	row, err := r.queries.CountTilesByStatus(ctx, griddb.CountTilesByStatusParams{
		PeriodID: periodID,
		Phase:    int32(phase),
	})
	if err != nil {
		return 0, 0, fmt.Errorf("count tiles by status: %w", err)
	}
	return row.DrawnCount, row.TotalCount, nil
}

func periodToDomain(row griddb.Period) *Period {
	p := &Period{
		ID:           row.ID,
		DailyImageID: row.DailyImageID,
		GameType:     row.GameType,
		Status:       PeriodStatus(row.Status),
		Phase:        int(row.Phase),
		StartedAt:    row.StartedAt,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	if row.EndedAt.Valid {
		p.EndedAt = &row.EndedAt.Time
	}
	if row.FinalImageKey.Valid {
		p.FinalImageKey = row.FinalImageKey.String
	}
	if row.ComposedAt.Valid {
		p.ComposedAt = &row.ComposedAt.Time
	}
	return p
}

func tileToDomain(row griddb.Tile) *Tile {
	return &Tile{
		ID:        row.ID,
		PeriodID:  row.PeriodID,
		Phase:     int(row.Phase),
		RowIndex:  int(row.RowIndex),
		ColIndex:  int(row.ColIndex),
		Status:    TileStatus(row.Status),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func gridConfigToDomain(row griddb.GridConfig) *GridConfig {
	return &GridConfig{
		ID:        row.ID,
		Phase:     int(row.Phase),
		Columns:   int(row.Columns),
		Rows:      int(row.Rows),
		CreatedAt: row.CreatedAt,
	}
}
