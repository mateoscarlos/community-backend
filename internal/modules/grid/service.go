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
	// maxFullSize caps the longest dimension of a composed full mosaic to keep
	// upload sizes reasonable across a wide range of final_grid_size settings.
	maxFullSize = 2048

	// maxFullCellSize bounds the per-cell pixel size so a small window doesn't
	// get stretched to 2048 px wide.
	maxFullCellSize = 256

	// thumbSize is the side of the square thumbnail used by the archive
	// calendar. Day cells render at ~80-160 CSS pixels; 512 covers high-DPI.
	thumbSize = 512

	// PeriodTimezone is the calendar timezone the daily masterpiece runs on.
	// A period started during day D in this zone is over the moment it crosses
	// midnight into D+1 (whichever phase it's currently on) unless a
	// period_duration_hours override is set in app_settings.
	PeriodTimezone = "Europe/Copenhagen"

	// composeFetchConcurrency caps in-flight tile downloads during compose.
	composeFetchConcurrency = 16

	// DefaultFinalGridSize is the fallback for new periods when the app-settings
	// override isn't set. Chosen so the default phase progression [3,5,7,9] hits
	// its last phase exactly. Overridable via appsettings once the M5 admin card
	// lands; for now, periods created from the sweeper use this constant.
	DefaultFinalGridSize = 9
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

// nextDailyCutoff returns the next Copenhagen midnight strictly after `now`.
// Used to build the completed-period countdown target.
func nextDailyCutoff(now time.Time) time.Time {
	loc, err := time.LoadLocation(PeriodTimezone)
	if err != nil {
		return now.Add(24 * time.Hour)
	}
	local := now.In(loc)
	y, m, d := local.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, loc)
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

// isPastCutoff reports whether the period should be closed. If a period
// duration is configured in app_settings it's a fixed span (startedAt +
// duration); otherwise it falls back to the legacy daily (midnight) cutoff.
func (s *Service) isPastCutoff(ctx context.Context, startedAt, now time.Time) bool {
	if dur, ok := appsettings.PeriodDuration(ctx, s.db); ok {
		return !now.Before(startedAt.Add(dur))
	}
	return isPastDailyCutoff(startedAt, now)
}

// nextPeriodCutoff returns the timestamp when the current cycle ends. Same
// logic as isPastCutoff but forward-looking — used to power the completed-
// period countdown card in the UI.
func (s *Service) nextPeriodCutoff(ctx context.Context, startedAt, now time.Time) time.Time {
	if dur, ok := appsettings.PeriodDuration(ctx, s.db); ok {
		return startedAt.Add(dur)
	}
	return nextDailyCutoff(now)
}

// CurrentPeriod bundles everything the handler needs to build a period
// response: the period record, its full tile set (all final_grid_size² tiles,
// including future-locked ones), and the count of drawn tiles in the
// currently-playable window.
type CurrentPeriod struct {
	Period       *Period
	Tiles        []Tile
	DrawnCount   int64
	TotalCount   int64
	PhaseSizes   []int
	OuterDisplay string
	// NextPeriodStartsAt is populated only when Period.Status is completed —
	// the frontend's countdown target.
	NextPeriodStartsAt *time.Time
}

// PhaseWindowSize returns the side length of the unlocked window at the given
// 1-indexed phase. Falls back to phaseSizes[len-1] if phase overshoots.
func PhaseWindowSize(phase int, phaseSizes []int) int {
	if len(phaseSizes) == 0 {
		return 0
	}
	if phase < 1 {
		return phaseSizes[0]
	}
	if phase-1 >= len(phaseSizes) {
		return phaseSizes[len(phaseSizes)-1]
	}
	return phaseSizes[phase-1]
}

// GetActivePeriod returns the sole active period or ErrPeriodNotFound.
func (s *Service) GetActivePeriod(ctx context.Context) (*Period, error) {
	return s.repo.GetActivePeriod(ctx)
}

// GetPeriodByTileID returns the period that owns a given tile.
func (s *Service) GetPeriodByTileID(ctx context.Context, tileID uuid.UUID) (*Period, error) {
	return s.repo.GetPeriodByTileID(ctx, tileID)
}

// GetCurrent returns the active period with its tiles + counts + config
// snapshot. Falls back to the most-recent completed period when no active
// one exists, so the frontend can render the "come back tomorrow" resting
// state (see docs/game-model-rework.md §M4). Only returns ErrPeriodNotFound
// when the DB has literally never held a period.
func (s *Service) GetCurrent(ctx context.Context) (*CurrentPeriod, error) {
	period, err := s.repo.GetActivePeriod(ctx)
	if err != nil {
		if !errors.Is(err, ErrPeriodNotFound) {
			return nil, fmt.Errorf("grid service: get current: %w", err)
		}
		period, err = s.repo.GetLatestCompletedPeriod(ctx)
		if err != nil {
			return nil, fmt.Errorf("grid service: get latest completed: %w", err)
		}
	}
	return s.hydratePeriod(ctx, period)
}

// GetByID returns a period by its ID hydrated the same way as GetCurrent.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*CurrentPeriod, error) {
	period, err := s.repo.GetPeriodByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("grid service: get by id: %w", err)
	}
	return s.hydratePeriod(ctx, period)
}

func (s *Service) hydratePeriod(ctx context.Context, period *Period) (*CurrentPeriod, error) {
	tiles, err := s.repo.GetTilesByPeriod(ctx, period.ID)
	if err != nil {
		return nil, fmt.Errorf("grid service: get tiles: %w", err)
	}
	drawn, total, err := s.repo.CountTilesUpToPhase(ctx, period.ID, period.Phase)
	if err != nil {
		return nil, fmt.Errorf("grid service: count tiles: %w", err)
	}
	cp := &CurrentPeriod{
		Period:       period,
		Tiles:        tiles,
		DrawnCount:   drawn,
		TotalCount:   total,
		PhaseSizes:   appsettings.PhaseGridSizes(ctx, s.db),
		OuterDisplay: appsettings.OuterTileDisplay(ctx, s.db),
	}
	if period.Status == PeriodCompleted {
		t := s.nextPeriodCutoff(ctx, period.StartedAt, time.Now())
		cp.NextPeriodStartsAt = &t
	}
	return cp, nil
}

// ListArchive returns completed periods with pagination.
func (s *Service) ListArchive(ctx context.Context, limit, offset int) ([]Period, error) {
	periods, err := s.repo.ListCompletedPeriods(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("grid service: list archive: %w", err)
	}
	return periods, nil
}

// ComposeFinalImage stitches every drawn tile with phase <= `phase` into a
// mosaic and stores it under the period's archive key. The canvas is sized to
// the phase's window (from phase_grid_sizes) so early-phase mosaics show only
// the drawn center; the final-phase mosaic covers the entire final grid.
//
// Idempotent: re-running overwrites the previous mosaic, so the archive
// always shows the highest-fidelity phase composed so far.
func (s *Service) ComposeFinalImage(ctx context.Context, periodID uuid.UUID, phase int) error {
	period, err := s.repo.GetPeriodByID(ctx, periodID)
	if err != nil {
		return fmt.Errorf("compose: get period: %w", err)
	}

	phaseSizes := appsettings.PhaseGridSizes(ctx, s.db)
	windowSize := PhaseWindowSize(phase, phaseSizes)
	if windowSize <= 0 {
		return fmt.Errorf("compose: no window size for phase %d", phase)
	}
	// Window is centered on the final grid. For a final_grid_size=9 with
	// phase 2 (windowSize=5), tiles at absolute rows 2..6 are translated to
	// composer rows 0..4.
	offset := (period.FinalGridSize - windowSize) / 2

	allTiles, err := s.repo.GetTilesByPeriod(ctx, periodID)
	if err != nil {
		return fmt.Errorf("compose: get tiles: %w", err)
	}

	// Download tile bytes in parallel — biggest perf win for high-phase grids.
	type fetched struct {
		row, col int
		data     []byte
	}
	results := make([]fetched, 0, len(allTiles))
	var resultsMu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(composeFetchConcurrency)
	var failedMu sync.Mutex
	failedKeys := make([]string, 0)
	for _, t := range allTiles {
		if t.Phase > phase || t.SubmissionKey == "" {
			continue
		}
		t := t
		g.Go(func() error {
			data, err := s.store.GetObject(gctx, t.SubmissionKey)
			if err != nil {
				failedMu.Lock()
				failedKeys = append(failedKeys, t.SubmissionKey)
				failedMu.Unlock()
				return nil // missing tile is non-fatal — leaves a black cell
			}
			resultsMu.Lock()
			results = append(results, fetched{row: t.RowIndex - offset, col: t.ColIndex - offset, data: data})
			resultsMu.Unlock()
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
		tileImgs = append(tileImgs, composer.TileImage{
			Row:  r.row,
			Col:  r.col,
			Data: r.data,
		})
	}

	if len(tileImgs) == 0 {
		return fmt.Errorf("compose: no tile images for period %s at phase %d", periodID, phase)
	}

	// Clamp cell size so a large window doesn't blow up the canvas.
	cellSize := maxFullSize / windowSize
	if cellSize < 1 {
		cellSize = 1
	}
	if cellSize > maxFullCellSize {
		cellSize = maxFullCellSize
	}

	mosaic, err := composer.Compose(composer.Options{
		Cols:     windowSize,
		Rows:     windowSize,
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
		Int("window_size", windowSize).
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

// SweepExpiredPeriod closes any active period past its cutoff, composes a
// final mosaic for whatever phase was in flight, then auto-rotates to the
// next scheduled image. Errors are logged but don't cascade.
func (s *Service) SweepExpiredPeriod(ctx context.Context) error {
	p, err := s.repo.GetActivePeriod(ctx)
	if err == nil {
		if !s.isPastCutoff(ctx, p.StartedAt, time.Now()) {
			return nil
		}

		if cErr := s.repo.CompletePeriod(ctx, p.ID); cErr != nil {
			return fmt.Errorf("sweep period: complete: %w", cErr)
		}

		drawn, total, cntErr := s.repo.CountTilesUpToPhase(ctx, p.ID, p.Phase)
		if cntErr != nil {
			s.log.Warn().Err(cntErr).Msg("sweep period: count tiles")
		} else if drawn > 0 {
			if cErr := s.ComposeFinalImage(ctx, p.ID, p.Phase); cErr != nil {
				s.log.Error().Err(cErr).Msg("sweep period: compose")
			}
		}

		s.log.Info().
			Str("period_id", p.ID.String()).
			Int64("drawn", drawn).
			Int64("total", total).
			Int("phase", p.Phase).
			Msg("swept past-cutoff period")
	} else if !errors.Is(err, ErrPeriodNotFound) {
		return fmt.Errorf("sweep period: get active: %w", err)
	}

	// Try to spawn a new period for today from the schedule.
	if err := s.rotateForToday(ctx); err != nil {
		s.log.Warn().Err(err).Msg("sweep period: auto-rotate skipped")
	}
	return nil
}

// RotateForToday is the exported wrapper around rotateForToday so callers
// outside the package (e.g. the debug handler reacting to a schedule upsert)
// can trigger an immediate rotation when an admin uploads today's content
// instead of waiting for the midnight sweeper.
func (s *Service) RotateForToday(ctx context.Context) error {
	return s.rotateForToday(ctx)
}

// rotateForToday promotes today's scheduled image into an active period.
// No-op if a period for today already exists or nothing is scheduled.
func (s *Service) rotateForToday(ctx context.Context) error {
	loc, err := time.LoadLocation(PeriodTimezone)
	if err != nil {
		return fmt.Errorf("load tz: %w", err)
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)

	if existing, err := s.repo.GetActivePeriod(ctx); err == nil {
		startedLocal := existing.StartedAt.In(loc)
		ey, em, ed := startedLocal.Date()
		if ey == y && em == m && ed == d {
			return nil
		}
	}

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

// CreatePhotoPeriod creates a new active period anchored to a daily image and
// seeds all final_grid_size² tiles up front. The center 3×3 (or whatever
// phase_grid_sizes[0] resolves to) starts phase_locked=FALSE; outer rings
// start locked and get flipped ring-by-ring as phases advance.
func (s *Service) CreatePhotoPeriod(ctx context.Context, dailyImageID uuid.UUID) (*Period, error) {
	phaseSizes := appsettings.PhaseGridSizes(ctx, s.db)
	finalSize := phaseSizes[len(phaseSizes)-1]

	period, err := s.repo.CreatePeriod(ctx, dailyImageID, PeriodActive, 1, finalSize)
	if err != nil {
		return nil, fmt.Errorf("grid service: create photo period: %w", err)
	}
	if err := s.seedAllTiles(ctx, period, phaseSizes); err != nil {
		return nil, err
	}
	return period, nil
}

// seedAllTiles inserts finalSize² tile rows for the given period. Each tile's
// phase is the smallest phase whose concentric window covers it. Tiles at
// phase 1 start playable (phase_locked=false); everything else is
// phase_locked=true and gets unlocked when its ring is reached.
func (s *Service) seedAllTiles(ctx context.Context, period *Period, phaseSizes []int) error {
	finalSize := period.FinalGridSize
	center := finalSize / 2 // integer division; finalSize is odd

	for row := 0; row < finalSize; row++ {
		for col := 0; col < finalSize; col++ {
			phase := tilePhase(row, col, center, phaseSizes)
			phaseLocked := phase > 1
			if _, err := s.repo.CreateTile(ctx, period.ID, phase, row, col, phaseLocked); err != nil {
				return fmt.Errorf("grid service: create tile [%d,%d]: %w", row, col, err)
			}
		}
	}

	s.log.Info().
		Str("period_id", period.ID.String()).
		Int("final_grid_size", finalSize).
		Int("total_tiles", finalSize*finalSize).
		Msg("seeded period tiles")
	return nil
}

// tilePhase returns the smallest phase index (1-based) whose concentric
// window covers the given (row, col). Tiles are assigned to the first phase
// that includes them so the phase field is stable for the tile's lifetime.
func tilePhase(row, col, center int, phaseSizes []int) int {
	dr := row - center
	if dr < 0 {
		dr = -dr
	}
	dc := col - center
	if dc < 0 {
		dc = -dc
	}
	dist := dr
	if dc > dist {
		dist = dc
	}
	for i, size := range phaseSizes {
		if dist <= size/2 {
			return i + 1
		}
	}
	// Should never happen if the last phase covers the full grid, but fall
	// back to the last phase index so the tile is at least assignable.
	return len(phaseSizes)
}

// PhaseResult describes what happened after checking phase completion.
type PhaseResult int

const (
	PhaseIncomplete  PhaseResult = iota // Not all tiles drawn yet.
	PhaseAdvanced                       // Next ring unlocked; period continues.
	PhaseAllComplete                    // All rings drawn; masterpiece complete.
)

// CheckPhaseCompletion looks at the currently-playable window; if every tile
// in it is drawn it advances to the next ring (or, if this was the last ring,
// completes the period and composes the masterpiece).
func (s *Service) CheckPhaseCompletion(ctx context.Context, periodID uuid.UUID) (PhaseResult, error) {
	period, err := s.repo.GetPeriodByID(ctx, periodID)
	if err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: check completion: %w", err)
	}

	drawnCount, totalCount, err := s.repo.CountTilesUpToPhase(ctx, period.ID, period.Phase)
	if err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: count tiles: %w", err)
	}
	if drawnCount < totalCount {
		return PhaseIncomplete, nil
	}

	phaseSizes := appsettings.PhaseGridSizes(ctx, s.db)
	completedPhase := period.Phase
	isLastPhase := completedPhase >= len(phaseSizes)

	if isLastPhase {
		s.log.Info().
			Str("period_id", periodID.String()).
			Int("completed_phase", completedPhase).
			Msg("masterpiece complete — closing period")
		if err := s.repo.CompletePeriod(ctx, periodID); err != nil {
			return PhaseIncomplete, err
		}
		if composeErr := s.ComposeFinalImage(ctx, periodID, completedPhase); composeErr != nil {
			s.log.Error().Err(composeErr).Str("period_id", periodID.String()).Msg("compose final image")
		}
		return PhaseAllComplete, nil
	}

	nextPhase := completedPhase + 1
	if err := s.repo.UpdatePeriodPhase(ctx, periodID, nextPhase); err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: update phase: %w", err)
	}
	if err := s.repo.UnlockPhaseRing(ctx, periodID, nextPhase); err != nil {
		return PhaseIncomplete, fmt.Errorf("grid service: unlock ring: %w", err)
	}

	s.log.Info().
		Str("period_id", periodID.String()).
		Int("phase", nextPhase).
		Int("window_size", PhaseWindowSize(nextPhase, phaseSizes)).
		Msg("advanced to next phase")

	// Compose interim mosaic for the ring we just finished.
	if composeErr := s.ComposeFinalImage(ctx, periodID, completedPhase); composeErr != nil {
		s.log.Error().Err(composeErr).Str("period_id", periodID.String()).Int("phase", completedPhase).Msg("compose mosaic")
	}

	return PhaseAdvanced, nil
}
