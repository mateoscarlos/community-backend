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
)

type Repository interface {
	GetActivePeriod(ctx context.Context) (*Period, error)
	GetLatestCompletedPeriod(ctx context.Context) (*Period, error)
	GetPeriodByID(ctx context.Context, id uuid.UUID) (*Period, error)
	GetPeriodByTileID(ctx context.Context, tileID uuid.UUID) (*Period, error)
	ListCompletedPeriods(ctx context.Context, limit, offset int) ([]Period, error)
	ListCompletedPeriodsMissingFinalImage(ctx context.Context) ([]Period, error)
	CreatePeriod(ctx context.Context, dailyImageID uuid.UUID, status PeriodStatus, phase, finalGridSize int) (*Period, error)
	UpdatePeriodPhase(ctx context.Context, id uuid.UUID, phase int) error
	CompletePeriod(ctx context.Context, id uuid.UUID) error
	SetPeriodFinalImage(ctx context.Context, id uuid.UUID, finalImageKey string) error
	GetTilesByPeriod(ctx context.Context, periodID uuid.UUID) ([]Tile, error)
	CreateTile(ctx context.Context, periodID uuid.UUID, phase, row, col int, phaseLocked bool) (*Tile, error)
	UpdateTileStatus(ctx context.Context, id uuid.UUID, status TileStatus) error
	// UnlockPhaseRing flips phase_locked=FALSE for every tile at the given
	// phase. Called on phase advance to make the newly-unlocked ring playable
	// while leaving earlier-phase drawings untouched.
	UnlockPhaseRing(ctx context.Context, periodID uuid.UUID, phase int) error
	// CountTilesUpToPhase counts tiles in the currently-playable window
	// (phase <= upToPhase and phase_locked=false).
	CountTilesUpToPhase(ctx context.Context, periodID uuid.UUID, upToPhase int) (drawnCount, totalCount int64, err error)

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

func (r *postgresRepository) GetLatestCompletedPeriod(ctx context.Context) (*Period, error) {
	row, err := r.queries.GetLatestCompletedPeriod(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPeriodNotFound
		}
		return nil, fmt.Errorf("get latest completed period: %w", err)
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

func (r *postgresRepository) GetPeriodByTileID(ctx context.Context, tileID uuid.UUID) (*Period, error) {
	row, err := r.queries.GetPeriodByTileID(ctx, tileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPeriodNotFound
		}
		return nil, fmt.Errorf("get period by tile id: %w", err)
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

func (r *postgresRepository) CreatePeriod(ctx context.Context, dailyImageID uuid.UUID, status PeriodStatus, phase, finalGridSize int) (*Period, error) {
	row, err := r.queries.CreatePeriod(ctx, griddb.CreatePeriodParams{
		DailyImageID:  uuid.NullUUID{UUID: dailyImageID, Valid: true},
		Status:        string(status),
		Phase:         int32(phase),
		FinalGridSize: int32(finalGridSize),
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

func (r *postgresRepository) GetTilesByPeriod(ctx context.Context, periodID uuid.UUID) ([]Tile, error) {
	rows, err := r.queries.GetTilesByPeriod(ctx, periodID)
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
			PhaseLocked:   row.PhaseLocked,
			SubmissionKey: row.SubmissionKey,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
	}
	return tiles, nil
}

func (r *postgresRepository) CreateTile(ctx context.Context, periodID uuid.UUID, phase, row, col int, phaseLocked bool) (*Tile, error) {
	dbRow, err := r.queries.CreateTile(ctx, griddb.CreateTileParams{
		PeriodID:    periodID,
		Phase:       int32(phase),
		RowIndex:    int32(row),
		ColIndex:    int32(col),
		PhaseLocked: phaseLocked,
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

func (r *postgresRepository) UnlockPhaseRing(ctx context.Context, periodID uuid.UUID, phase int) error {
	return r.queries.UnlockPhaseRing(ctx, griddb.UnlockPhaseRingParams{
		PeriodID: periodID,
		Phase:    int32(phase),
	})
}

func (r *postgresRepository) CountTilesUpToPhase(ctx context.Context, periodID uuid.UUID, upToPhase int) (int64, int64, error) {
	row, err := r.queries.CountTilesUpToPhase(ctx, griddb.CountTilesUpToPhaseParams{
		PeriodID: periodID,
		Phase:    int32(upToPhase),
	})
	if err != nil {
		return 0, 0, fmt.Errorf("count tiles up to phase: %w", err)
	}
	return row.DrawnCount, row.TotalCount, nil
}

func periodToDomain(row griddb.Period) *Period {
	p := &Period{
		ID:            row.ID,
		GameType:      row.GameType,
		Status:        PeriodStatus(row.Status),
		Phase:         int(row.Phase),
		FinalGridSize: int(row.FinalGridSize),
		StartedAt:     row.StartedAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	// The DB column is still nullable for backwards compatibility with legacy
	// schema, but we now insert only non-null image IDs (see M1.5 in
	// docs/game-model-rework.md).
	if row.DailyImageID.Valid {
		p.DailyImageID = row.DailyImageID.UUID
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
		ID:          row.ID,
		PeriodID:    row.PeriodID,
		Phase:       int(row.Phase),
		RowIndex:    int(row.RowIndex),
		ColIndex:    int(row.ColIndex),
		Status:      TileStatus(row.Status),
		PhaseLocked: row.PhaseLocked,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
