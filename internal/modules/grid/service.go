package grid

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Service struct {
	repo Repository
	log  zerolog.Logger
}

func NewService(repo Repository, log zerolog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

type CurrentPeriod struct {
	Period     *Period
	GridConfig *GridConfig
	Tiles      []Tile
	DrawnCount int64
}

// GetActivePeriod returns the active period domain object.
func (s *Service) GetActivePeriod(ctx context.Context) (*Period, error) {
	return s.repo.GetActivePeriod(ctx)
}

// GetCurrent returns the active period with its grid config and tiles.
func (s *Service) GetCurrent(ctx context.Context) (*CurrentPeriod, error) {
	period, err := s.repo.GetActivePeriod(ctx)
	if err != nil {
		return nil, fmt.Errorf("grid service: get current: %w", err)
	}

	gridCfg, err := s.repo.GetGridConfig(ctx, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: get grid config for phase %d: %w", period.Phase, err)
	}

	tiles, err := s.repo.GetTilesByPeriodAndPhase(ctx, period.ID, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: get tiles: %w", err)
	}

	drawnCount, _, err := s.repo.CountTilesByStatus(ctx, period.ID, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: count tiles: %w", err)
	}

	return &CurrentPeriod{
		Period:     period,
		GridConfig: gridCfg,
		Tiles:      tiles,
		DrawnCount: drawnCount,
	}, nil
}

// GetByID returns a period by its ID with grid config and tiles.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*CurrentPeriod, error) {
	period, err := s.repo.GetPeriodByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("grid service: get by id: %w", err)
	}

	gridCfg, err := s.repo.GetGridConfig(ctx, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: get grid config: %w", err)
	}

	tiles, err := s.repo.GetTilesByPeriodAndPhase(ctx, period.ID, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: get tiles: %w", err)
	}

	drawnCount, _, err := s.repo.CountTilesByStatus(ctx, period.ID, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: count tiles: %w", err)
	}

	return &CurrentPeriod{
		Period:     period,
		GridConfig: gridCfg,
		Tiles:      tiles,
		DrawnCount: drawnCount,
	}, nil
}

// ListArchive returns completed periods with pagination.
func (s *Service) ListArchive(ctx context.Context, limit, offset int) ([]Period, error) {
	periods, err := s.repo.ListCompletedPeriods(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("grid service: list archive: %w", err)
	}
	return periods, nil
}

// CreatePeriodWithTiles creates a new active period and generates all tiles for phase 1.
func (s *Service) CreatePeriodWithTiles(ctx context.Context, dailyImageID uuid.UUID, gameType string) (*Period, error) {
	gridCfg, err := s.repo.GetGridConfig(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("grid service: get phase 1 config: %w", err)
	}

	period, err := s.repo.CreatePeriod(ctx, dailyImageID, gameType, PeriodActive, 1)
	if err != nil {
		return nil, fmt.Errorf("grid service: create period: %w", err)
	}

	for row := range gridCfg.Rows {
		for col := range gridCfg.Columns {
			if _, err := s.repo.CreateTile(ctx, period.ID, 1, row, col); err != nil {
				return nil, fmt.Errorf("grid service: create tile [%d,%d]: %w", row, col, err)
			}
		}
	}

	s.log.Info().
		Str("period_id", period.ID.String()).
		Int("rows", gridCfg.Rows).
		Int("cols", gridCfg.Columns).
		Msg("created period with tiles")

	return period, nil
}

// PhaseResult describes what happened after checking phase completion.
type PhaseResult int

const (
	PhaseIncomplete  PhaseResult = iota // Not all tiles drawn yet.
	PhaseAdvanced                       // Advanced to next phase with new tiles.
	PhaseAllComplete                    // All phases done, period closed.
)

// CheckPhaseCompletion checks if all tiles are drawn and advances to the next phase or completes the period.
func (s *Service) CheckPhaseCompletion(ctx context.Context, periodID uuid.UUID) (PhaseResult, error) {
	period, err := s.repo.GetPeriodByID(ctx, periodID)
	if err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: check completion: %w", err)
	}

	drawnCount, totalCount, err := s.repo.CountTilesByStatus(ctx, period.ID, period.Phase)
	if err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: count tiles: %w", err)
	}

	if drawnCount < totalCount {
		return PhaseIncomplete, nil
	}

	nextPhase := period.Phase + 1
	nextCfg, err := s.repo.GetGridConfig(ctx, nextPhase)
	if err != nil {
		s.log.Info().Str("period_id", periodID.String()).Msg("all phases complete, closing period")
		if err := s.repo.CompletePeriod(ctx, periodID); err != nil {
			return PhaseIncomplete, err
		}
		return PhaseAllComplete, nil
	}

	if err := s.repo.UpdatePeriodPhase(ctx, periodID, nextPhase); err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: update phase: %w", err)
	}

	for row := range nextCfg.Rows {
		for col := range nextCfg.Columns {
			if _, err := s.repo.CreateTile(ctx, periodID, nextPhase, row, col); err != nil {
				return PhaseIncomplete, fmt.Errorf("grid service: create tile [%d,%d]: %w", row, col, err)
			}
		}
	}

	s.log.Info().
		Str("period_id", periodID.String()).
		Int("phase", nextPhase).
		Int("rows", nextCfg.Rows).
		Int("cols", nextCfg.Columns).
		Msg("advanced to next phase")

	return PhaseAdvanced, nil
}
