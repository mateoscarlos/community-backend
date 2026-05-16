package grid

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/community-app/community-backend/internal/modules/dailyimage"
	"github.com/community-app/community-backend/internal/shared/appsettings"
	"github.com/community-app/community-backend/internal/shared/composer"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	// maxFullSize caps the longest dimension of a composed full mosaic. Without
	// this cap the canvas grows quadratically with phase (a 100x100 grid would
	// be 25,600 px square at the old 256px cell size). 2048 keeps full mosaics
	// under ~1MB at quality 85 while still preserving per-tile detail through
	// phase ~6.
	maxFullSize = 2048

	// maxFullCellSize bounds the per-cell pixel size for low-phase grids so a
	// 3x3 phase doesn't get stretched to 2048 px wide.
	maxFullCellSize = 256

	// thumbSize is the side of the square thumbnail used by the archive
	// calendar. Day cells render at ~80-160 CSS pixels; 512 covers high-DPI.
	thumbSize = 512

	// MaxPhasesPerPeriod is a safety guard against runaway phase advancement.
	// In practice the period closes long before this from the daily cutoff;
	// effectively unbounded so the grid keeps subdividing.
	MaxPhasesPerPeriod = 1_000_000

	// PeriodTimezone is the calendar timezone the daily masterpiece runs on.
	// A period started during day D in this zone is over the moment it crosses
	// midnight into D+1 (whichever phase it's currently on).
	PeriodTimezone = "Europe/Copenhagen"

	// composeFetchConcurrency caps in-flight tile downloads during compose.
	// Object storage round-trips (R2/MinIO) dominate compose latency at high
	// phase counts; pulling tiles in parallel turns hundreds of sequential
	// 50ms RTTs into batches.
	composeFetchConcurrency = 16
)

// ThumbnailKey returns the storage key for the thumbnail variant of a full
// archive mosaic key. Convention: insert "-thumb" before ".jpg".
func ThumbnailKey(fullKey string) string {
	if !strings.HasSuffix(fullKey, ".jpg") {
		return fullKey + "-thumb"
	}
	return strings.TrimSuffix(fullKey, ".jpg") + "-thumb.jpg"
}

// isPastDailyCutoff reports whether `now` is past midnight (in PeriodTimezone)
// after period.StartedAt. Falls back to "not past" if the timezone DB is missing.
func isPastDailyCutoff(startedAt, now time.Time) bool {
	loc, err := time.LoadLocation(PeriodTimezone)
	if err != nil {
		return false
	}
	startedLocal := startedAt.In(loc)
	y, m, d := startedLocal.Date()
	cutoff := time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	return !now.Before(cutoff)
}

type Service struct {
	repo    Repository
	imgRepo dailyimage.Repository
	store   *storage.Storage
	db      *sql.DB
	log     zerolog.Logger
}

func NewService(repo Repository, imgRepo dailyimage.Repository, store *storage.Storage, db *sql.DB, log zerolog.Logger) *Service {
	return &Service{repo: repo, imgRepo: imgRepo, store: store, db: db, log: log}
}

// isPastCutoff reports whether the period that started at startedAt should now
// be closed. If a period duration is configured in app_settings it's a fixed
// span (startedAt + duration); otherwise it falls back to the legacy daily
// cutoff (midnight in PeriodTimezone). Changing the setting affects the
// running period at the next sweep — that's the intended "global knob".
func (s *Service) isPastCutoff(ctx context.Context, startedAt, now time.Time) bool {
	if dur, ok := appsettings.PeriodDuration(ctx, s.db); ok {
		return !now.Before(startedAt.Add(dur))
	}
	return isPastDailyCutoff(startedAt, now)
}

type CurrentPeriod struct {
	Period     *Period
	GridConfig *GridConfig
	Tiles      []Tile
	DrawnCount int64
}

// GetActivePeriod returns the active period domain object for a given game.
func (s *Service) GetActivePeriod(ctx context.Context, gameType GameType) (*Period, error) {
	return s.repo.GetActivePeriod(ctx, gameType)
}

// GetPeriodByTileID returns the period that owns a given tile. Used by code
// paths (like submission) that operate on a single tile and need to derive
// which game they're acting on.
func (s *Service) GetPeriodByTileID(ctx context.Context, tileID uuid.UUID) (*Period, error) {
	return s.repo.GetPeriodByTileID(ctx, tileID)
}

// GetCurrent returns the active period for the given game with its grid
// config and tiles.
func (s *Service) GetCurrent(ctx context.Context, gameType GameType) (*CurrentPeriod, error) {
	period, err := s.repo.GetActivePeriod(ctx, gameType)
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

// ListArchive returns completed periods with pagination, scoped to a single
// game so photo and prompt archives stay independent.
func (s *Service) ListArchive(ctx context.Context, gameType GameType, limit, offset int) ([]Period, error) {
	periods, err := s.repo.ListCompletedPeriods(ctx, gameType, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("grid service: list archive: %w", err)
	}
	return periods, nil
}

// ComposeFinalImage downloads all tile submissions for the given phase,
// stitches them into a JPEG mosaic, uploads it, and saves the storage key on
// the period. Idempotent: re-running overwrites the previous mosaic, so the
// archive always shows the highest-fidelity phase that has been completed.
func (s *Service) ComposeFinalImage(ctx context.Context, periodID uuid.UUID, phase int) error {
	gridCfg, err := s.repo.GetGridConfig(ctx, phase)
	if err != nil {
		return fmt.Errorf("compose: get grid config for phase %d: %w", phase, err)
	}

	tiles, err := s.repo.GetTilesByPeriodAndPhase(ctx, periodID, phase)
	if err != nil {
		return fmt.Errorf("compose: get tiles: %w", err)
	}

	// Download tile bytes in parallel — biggest perf win for high-phase grids.
	type fetched struct {
		row, col int
		data     []byte
	}
	results := make([]fetched, len(tiles))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(composeFetchConcurrency)
	var failedMu sync.Mutex
	failedKeys := make([]string, 0)
	for i, t := range tiles {
		if t.SubmissionKey == "" {
			continue
		}
		i, t := i, t
		g.Go(func() error {
			data, err := s.store.GetObject(gctx, t.SubmissionKey)
			if err != nil {
				failedMu.Lock()
				failedKeys = append(failedKeys, t.SubmissionKey)
				failedMu.Unlock()
				return nil // missing tile is non-fatal — leaves a black cell
			}
			results[i] = fetched{row: t.RowIndex, col: t.ColIndex, data: data}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("compose: parallel fetch: %w", err)
	}
	if len(failedKeys) > 0 {
		s.log.Warn().Int("count", len(failedKeys)).Msg("compose: some tiles failed to fetch")
	}

	tileImgs := make([]composer.TileImage, 0, len(results))
	for _, r := range results {
		if r.data == nil {
			continue
		}
		tileImgs = append(tileImgs, composer.TileImage{
			Row:  r.row,
			Col:  r.col,
			Data: r.data,
		})
	}

	if len(tileImgs) == 0 {
		return fmt.Errorf("compose: no tile images for period %s", periodID)
	}

	// Clamp cell size so a high-phase grid doesn't blow up the canvas. At
	// phase 20 (231x231) this gives ~9px per cell — individual cells lose
	// detail but the mosaic stays a sane file size.
	maxDim := gridCfg.Columns
	if gridCfg.Rows > maxDim {
		maxDim = gridCfg.Rows
	}
	cellSize := maxFullSize / maxDim
	if cellSize < 1 {
		cellSize = 1
	}
	if cellSize > maxFullCellSize {
		cellSize = maxFullCellSize
	}

	mosaic, err := composer.Compose(composer.Options{
		Cols:     gridCfg.Columns,
		Rows:     gridCfg.Rows,
		CellSize: cellSize,
	}, tileImgs)
	if err != nil {
		return fmt.Errorf("compose: stitch: %w", err)
	}

	// Per-phase key so we keep the history; final_image_key on the period
	// always points at the latest one for the calendar preview.
	fullKey := fmt.Sprintf("archive/%s/phase-%d.jpg", periodID.String(), phase)
	if err := s.store.PutObject(ctx, fullKey, mosaic, "image/jpeg"); err != nil {
		return fmt.Errorf("compose: upload full: %w", err)
	}

	// Cheap thumbnail derived from the full so the calendar list stays light.
	thumb, err := composer.Thumbnail(mosaic, thumbSize)
	if err != nil {
		// Thumb failure is non-fatal — we can fall back to the full URL.
		s.log.Warn().Err(err).Str("period_id", periodID.String()).Msg("compose: thumbnail")
	} else {
		if err := s.store.PutObject(ctx, ThumbnailKey(fullKey), thumb, "image/jpeg"); err != nil {
			s.log.Warn().Err(err).Str("period_id", periodID.String()).Msg("compose: upload thumb")
		}
	}

	if err := s.repo.UpsertPeriodMosaic(ctx, periodID, phase, fullKey); err != nil {
		return fmt.Errorf("compose: save phase mosaic: %w", err)
	}

	if err := s.repo.SetPeriodFinalImage(ctx, periodID, fullKey); err != nil {
		return fmt.Errorf("compose: save latest key: %w", err)
	}

	s.log.Info().
		Str("period_id", periodID.String()).
		Int("phase", phase).
		Str("key", fullKey).
		Int("cell_size", cellSize).
		Int("tiles", len(tileImgs)).
		Int("full_bytes", len(mosaic)).
		Int("thumb_bytes", len(thumb)).
		Msg("composed mosaic")

	return nil
}

// ListMosaics returns all per-phase mosaics for a period, ordered phase asc.
func (s *Service) ListMosaics(ctx context.Context, periodID uuid.UUID) ([]PhaseMosaic, error) {
	return s.repo.ListMosaicsByPeriod(ctx, periodID)
}

// SweepExpiredPeriod closes any active periods past their daily cutoff
// (midnight in PeriodTimezone) for both games, composes a final mosaic for
// whatever phase was in flight, then auto-rotates each game from its own
// schedule. Each step's failure is logged but doesn't block the others.
func (s *Service) SweepExpiredPeriod(ctx context.Context) error {
	for _, gt := range []GameType{GamePhoto, GamePrompt} {
		if err := s.sweepGame(ctx, gt); err != nil {
			s.log.Warn().Err(err).Str("game_type", string(gt)).Msg("sweep game")
		}
	}
	return nil
}

func (s *Service) sweepGame(ctx context.Context, gameType GameType) error {
	p, err := s.repo.GetActivePeriod(ctx, gameType)
	if err == nil {
		if !s.isPastCutoff(ctx, p.StartedAt, time.Now()) {
			return nil
		}

		if cErr := s.repo.CompletePeriod(ctx, p.ID); cErr != nil {
			return fmt.Errorf("sweep period: complete: %w", cErr)
		}

		drawn, total, cntErr := s.repo.CountTilesByStatus(ctx, p.ID, p.Phase)
		if cntErr != nil {
			s.log.Warn().Err(cntErr).Msg("sweep period: count tiles")
		} else if drawn > 0 {
			if cErr := s.ComposeFinalImage(ctx, p.ID, p.Phase); cErr != nil {
				s.log.Error().Err(cErr).Msg("sweep period: compose")
			}
		}

		s.log.Info().
			Str("period_id", p.ID.String()).
			Str("game_type", string(gameType)).
			Int64("drawn", drawn).
			Int64("total", total).
			Int("phase", p.Phase).
			Msg("swept past-cutoff period")
	} else if !errors.Is(err, ErrPeriodNotFound) {
		return fmt.Errorf("sweep period: get active: %w", err)
	}

	// Try to spawn a new period for today using the per-game schedule.
	if err := s.rotateForToday(ctx, gameType); err != nil {
		s.log.Warn().Err(err).Str("game_type", string(gameType)).Msg("sweep period: auto-rotate skipped")
	}
	return nil
}

// RotateForToday is the exported wrapper around rotateForToday so callers
// outside the package (e.g. the debug handler reacting to a schedule upsert)
// can trigger an immediate rotation when an admin uploads today's content
// instead of waiting for the midnight sweeper.
func (s *Service) RotateForToday(ctx context.Context, gameType GameType) error {
	return s.rotateForToday(ctx, gameType)
}

// rotateForToday promotes today's schedule entry for the given game and
// creates an active period from it. No-op if a period for today already exists
// or nothing is scheduled.
func (s *Service) rotateForToday(ctx context.Context, gameType GameType) error {
	loc, err := time.LoadLocation(PeriodTimezone)
	if err != nil {
		return fmt.Errorf("load tz: %w", err)
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)

	if existing, err := s.repo.GetActivePeriod(ctx, gameType); err == nil {
		startedLocal := existing.StartedAt.In(loc)
		ey, em, ed := startedLocal.Date()
		if ey == y && em == m && ed == d {
			return nil
		}
	}

	switch gameType {
	case GamePhoto:
		return s.rotatePhotoForToday(ctx, today)
	case GamePrompt:
		return s.rotatePromptForToday(ctx, today)
	default:
		return fmt.Errorf("rotate: unknown game type %q", gameType)
	}
}

func (s *Service) rotatePhotoForToday(ctx context.Context, today time.Time) error {
	scheduled, err := s.imgRepo.GetScheduledByDate(ctx, today)
	if err != nil {
		if errors.Is(err, dailyimage.ErrNotFound) {
			return fmt.Errorf("no scheduled image for %s", today.Format("2006-01-02"))
		}
		return fmt.Errorf("get scheduled: %w", err)
	}

	img, err := s.imgRepo.SetActive(ctx, today, scheduled.StorageKey, scheduled.Width, scheduled.Height)
	if err != nil {
		return fmt.Errorf("promote scheduled: %w", err)
	}

	period, err := s.CreatePhotoPeriod(ctx, img.ID)
	if err != nil {
		return fmt.Errorf("create period: %w", err)
	}

	s.log.Info().
		Str("period_id", period.ID.String()).
		Str("date", today.Format("2006-01-02")).
		Msg("auto-rotated to scheduled image")
	return nil
}

func (s *Service) rotatePromptForToday(ctx context.Context, today time.Time) error {
	scheduled, err := s.imgRepo.GetScheduledPromptByDate(ctx, today)
	if err != nil {
		if errors.Is(err, dailyimage.ErrNotFound) {
			return fmt.Errorf("no scheduled prompt for %s", today.Format("2006-01-02"))
		}
		return fmt.Errorf("get scheduled prompt: %w", err)
	}

	period, err := s.CreatePromptPeriod(ctx, scheduled.Prompt)
	if err != nil {
		return fmt.Errorf("create period: %w", err)
	}

	s.log.Info().
		Str("period_id", period.ID.String()).
		Str("date", today.Format("2006-01-02")).
		Msg("auto-rotated to scheduled prompt")
	return nil
}

// RecomposeMissing iterates all completed periods missing a final image and
// composes each one using its final phase. Returns the count successfully composed.
func (s *Service) RecomposeMissing(ctx context.Context) (int, error) {
	periods, err := s.repo.ListCompletedPeriodsMissingFinalImage(ctx)
	if err != nil {
		return 0, fmt.Errorf("recompose: list: %w", err)
	}
	composed := 0
	for _, p := range periods {
		if err := s.ComposeFinalImage(ctx, p.ID, p.Phase); err != nil {
			s.log.Error().Err(err).Str("period_id", p.ID.String()).Msg("recompose period")
			continue
		}
		composed++
	}
	return composed, nil
}

// CreatePhotoPeriod creates a new active photo-game period anchored to a
// daily image and generates all tiles for phase 1.
func (s *Service) CreatePhotoPeriod(ctx context.Context, dailyImageID uuid.UUID) (*Period, error) {
	period, err := s.repo.CreatePeriod(ctx, &dailyImageID, GamePhoto, PeriodActive, 1, "")
	if err != nil {
		return nil, fmt.Errorf("grid service: create photo period: %w", err)
	}
	if err := s.populatePhase1Tiles(ctx, period); err != nil {
		return nil, err
	}
	return period, nil
}

// CreatePromptPeriod creates a new active prompt-game period anchored to a
// text prompt and generates all tiles for phase 1.
func (s *Service) CreatePromptPeriod(ctx context.Context, prompt string) (*Period, error) {
	if prompt == "" {
		return nil, fmt.Errorf("grid service: prompt period requires non-empty prompt")
	}
	period, err := s.repo.CreatePeriod(ctx, nil, GamePrompt, PeriodActive, 1, prompt)
	if err != nil {
		return nil, fmt.Errorf("grid service: create prompt period: %w", err)
	}
	if err := s.populatePhase1Tiles(ctx, period); err != nil {
		return nil, err
	}
	return period, nil
}

func (s *Service) populatePhase1Tiles(ctx context.Context, period *Period) error {
	gridCfg, err := s.repo.GetGridConfig(ctx, 1)
	if err != nil {
		return fmt.Errorf("grid service: get phase 1 config: %w", err)
	}

	for row := range gridCfg.Rows {
		for col := range gridCfg.Columns {
			if _, err := s.repo.CreateTile(ctx, period.ID, 1, row, col); err != nil {
				return fmt.Errorf("grid service: create tile [%d,%d]: %w", row, col, err)
			}
		}
	}

	s.log.Info().
		Str("period_id", period.ID.String()).
		Str("game_type", period.GameType).
		Int("rows", gridCfg.Rows).
		Int("cols", gridCfg.Columns).
		Msg("created period with tiles")
	return nil
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

	completedPhase := period.Phase

	// Configured duration elapsed (or legacy midnight) or phase-cap reached
	// → close the period.
	timeUp := s.isPastCutoff(ctx, period.StartedAt, time.Now())
	phasesUp := completedPhase >= MaxPhasesPerPeriod

	nextPhase := completedPhase + 1
	nextCfg, err := s.repo.GetGridConfig(ctx, nextPhase)
	noNextConfig := err != nil

	if timeUp || phasesUp || noNextConfig {
		s.log.Info().
			Str("period_id", periodID.String()).
			Int("completed_phase", completedPhase).
			Bool("time_up", timeUp).
			Bool("phases_up", phasesUp).
			Bool("no_next_config", noNextConfig).
			Msg("closing period")
		if err := s.repo.CompletePeriod(ctx, periodID); err != nil {
			return PhaseIncomplete, err
		}
		if composeErr := s.ComposeFinalImage(ctx, periodID, completedPhase); composeErr != nil {
			s.log.Error().Err(composeErr).Str("period_id", periodID.String()).Msg("compose final image")
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

	// Compose mosaic for the phase we just finished — replaces any previous
	// mosaic for this period so the archive always shows the latest fidelity.
	if composeErr := s.ComposeFinalImage(ctx, periodID, completedPhase); composeErr != nil {
		s.log.Error().Err(composeErr).Str("period_id", periodID.String()).Int("phase", completedPhase).Msg("compose mosaic")
	}

	return PhaseAdvanced, nil
}
