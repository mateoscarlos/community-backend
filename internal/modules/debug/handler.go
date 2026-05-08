package debug

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strconv"
	"time"

	"github.com/community-app/community-backend/internal/modules/dailyimage"
	"github.com/community-app/community-backend/internal/modules/grid"
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/community-app/community-backend/internal/shared/sse"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	uploadURLExpiry          = 30 * time.Minute
	drawAllUploadConcurrency = 16
)

// Handler exposes debug-only endpoints. Never registered in production.
type Handler struct {
	repo    dailyimage.Repository
	gridSvc *grid.Service
	store   *storage.Storage
	broker  *sse.Broker
	db      *sql.DB
	log     zerolog.Logger
}

var _ httpserver.RouteRegistrar = (*Handler)(nil)

func NewHandler(repo dailyimage.Repository, gridSvc *grid.Service, store *storage.Storage, broker *sse.Broker, db *sql.DB, log zerolog.Logger) *Handler {
	return &Handler{repo: repo, gridSvc: gridSvc, store: store, broker: broker, db: db, log: log}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/debug", func(r chi.Router) {
		r.Get("/storage/upload-url", h.getUploadURL)
		r.Post("/daily-image", h.setActiveDailyImage)
		r.Post("/period", h.createPeriod)
		r.Delete("/period", h.deletePeriod)
		r.Post("/period/reset", h.resetPeriod)
		r.Post("/tiles/{id}/reset", h.resetTile)
		r.Post("/tiles/draw-all", h.drawAllTiles)
		r.Post("/recompose-archives", h.recomposeArchives)

		// Daily image schedule.
		r.Get("/schedule", h.listSchedule)
		r.Post("/schedule", h.upsertSchedule)
		r.Delete("/schedule/{date}", h.deleteSchedule)

		// Prompt schedule (parallel prompt-game).
		r.Get("/prompt-schedule", h.listPromptSchedule)
		r.Post("/prompt-schedule", h.upsertPromptSchedule)
		r.Delete("/prompt-schedule/{date}", h.deletePromptSchedule)

		// Sessions / users.
		r.Get("/sessions", h.listSessions)
		r.Get("/sessions/{id}", h.getSession)
	})
}

// recomposeArchives backfills final mosaic images for completed periods that
// don't have one yet (e.g. periods that completed before composition existed).
// Usage: POST /debug/recompose-archives
func (h *Handler) recomposeArchives(w http.ResponseWriter, r *http.Request) {
	count, err := h.gridSvc.RecomposeMissing(r.Context())
	if err != nil {
		h.log.Error().Err(err).Msg("recompose archives")
		httpserver.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]int{"composed": count})
}

// getUploadURL returns a presigned PUT URL so you can upload directly to MinIO.
// Usage: GET /debug/storage/upload-url?key=photos/my-image.jpg
func (h *Handler) getUploadURL(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "missing query param: key")
		return
	}

	url, err := h.store.PresignedPutURL(r.Context(), key, uploadURLExpiry)
	if err != nil {
		h.log.Error().Err(err).Str("key", key).Msg("presign upload url")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]string{
		"upload_url": url,
		"key":        key,
		"method":     "PUT",
		"expires_in": uploadURLExpiry.String(),
	})
}

// setActiveDailyImage inserts or replaces the active daily image.
// Usage: POST /debug/daily-image
//
//	{"storage_key": "photos/my-image.jpg", "width": 1920, "height": 1080}
//	{"storage_key": "photos/my-image.jpg", "width": 1920, "height": 1080, "date": "2026-03-14"}
func (h *Handler) setActiveDailyImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StorageKey string `json:"storage_key"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		Date       string `json:"date"` // optional, defaults to today
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.StorageKey == "" || body.Width == 0 || body.Height == 0 {
		httpserver.WriteError(w, http.StatusBadRequest, "storage_key, width and height are required")
		return
	}

	date := time.Now().UTC().Truncate(24 * time.Hour)
	if body.Date != "" {
		parsed, err := time.Parse("2006-01-02", body.Date)
		if err != nil {
			httpserver.WriteError(w, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
			return
		}
		date = parsed
	}

	img, err := h.repo.SetActive(r.Context(), date, body.StorageKey, body.Width, body.Height)
	if err != nil {
		h.log.Error().Err(err).Msg("set active daily image")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, img)
}

// createPeriod creates an active period for either game.
// Usage: POST /debug/period
//
//	{"game_type": "photo"}                       — uses active daily image
//	{"game_type": "prompt", "prompt": "an elephant riding a bike"}
func (h *Handler) createPeriod(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GameType string `json:"game_type"`
		Prompt   string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.GameType = "photo"
	}
	if body.GameType == "" {
		body.GameType = "photo"
	}

	switch grid.GameType(body.GameType) {
	case grid.GamePhoto:
		img, err := h.repo.GetActive(r.Context())
		if err != nil {
			httpserver.WriteError(w, http.StatusBadRequest, "no active daily image — set one first")
			return
		}
		period, err := h.gridSvc.CreatePhotoPeriod(r.Context(), img.ID)
		if err != nil {
			h.log.Error().Err(err).Msg("create debug photo period")
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, period)
	case grid.GamePrompt:
		if body.Prompt == "" {
			httpserver.WriteError(w, http.StatusBadRequest, "prompt is required for prompt-game periods")
			return
		}
		period, err := h.gridSvc.CreatePromptPeriod(r.Context(), body.Prompt)
		if err != nil {
			h.log.Error().Err(err).Msg("create debug prompt period")
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, period)
	default:
		httpserver.WriteError(w, http.StatusBadRequest, "invalid game_type")
	}
}

// deletePeriod deletes the active period and all its tiles, claims, and submissions.
// Usage: DELETE /debug/period?game_type=photo
func (h *Handler) deletePeriod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	gameType := grid.GameType(r.URL.Query().Get("game_type"))
	if gameType == "" {
		gameType = grid.GamePhoto
	}
	if !gameType.Valid() {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid game_type")
		return
	}

	var periodID uuid.UUID
	err := h.db.QueryRowContext(ctx,
		`SELECT id FROM periods WHERE status = 'active' AND game_type = $1 LIMIT 1`,
		string(gameType),
	).Scan(&periodID)
	if err != nil {
		httpserver.WriteError(w, http.StatusNotFound, "no active period")
		return
	}

	// Delete in order: submissions → claims → tiles → period
	queries := []string{
		`DELETE FROM submissions WHERE tile_id IN (SELECT id FROM tiles WHERE period_id = $1)`,
		`DELETE FROM claims WHERE tile_id IN (SELECT id FROM tiles WHERE period_id = $1)`,
		`DELETE FROM tiles WHERE period_id = $1`,
		`DELETE FROM periods WHERE id = $1`,
	}
	for _, q := range queries {
		if _, err := h.db.ExecContext(ctx, q, periodID); err != nil {
			h.log.Error().Err(err).Msg("delete period")
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	h.log.Info().Str("period_id", periodID.String()).Msg("deleted period (debug)")

	httpserver.WriteJSON(w, http.StatusOK, map[string]string{
		"deleted": periodID.String(),
	})
}

// resetPeriod resets the active period: deletes all submissions and claims, resets all tiles to free, resets phase to 1.
// Usage: POST /debug/period/reset?game_type=photo
func (h *Handler) resetPeriod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	gameType := grid.GameType(r.URL.Query().Get("game_type"))
	if gameType == "" {
		gameType = grid.GamePhoto
	}
	if !gameType.Valid() {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid game_type")
		return
	}

	var periodID uuid.UUID
	var currentPhase int32
	err := h.db.QueryRowContext(ctx,
		`SELECT id, phase FROM periods WHERE status = 'active' AND game_type = $1 LIMIT 1`,
		string(gameType),
	).Scan(&periodID, &currentPhase)
	if err != nil {
		httpserver.WriteError(w, http.StatusNotFound, "no active period")
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer tx.Rollback()

	// Delete submissions and claims for all tiles in this period (all phases)
	queries := []string{
		`DELETE FROM submissions WHERE tile_id IN (SELECT id FROM tiles WHERE period_id = $1)`,
		`DELETE FROM claims WHERE tile_id IN (SELECT id FROM tiles WHERE period_id = $1)`,
	}
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q, periodID); err != nil {
			h.log.Error().Err(err).Msg("reset period: cleanup")
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	// Delete tiles from phases > 1
	if _, err := tx.ExecContext(ctx, `DELETE FROM tiles WHERE period_id = $1 AND phase > 1`, periodID); err != nil {
		h.log.Error().Err(err).Msg("reset period: delete higher phase tiles")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Reset remaining phase 1 tiles to free
	if _, err := tx.ExecContext(ctx,
		`UPDATE tiles SET status = 'free', updated_at = now() WHERE period_id = $1 AND phase = 1`,
		periodID,
	); err != nil {
		h.log.Error().Err(err).Msg("reset period: reset tiles")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Reset period phase to 1
	if _, err := tx.ExecContext(ctx,
		`UPDATE periods SET phase = 1, ended_at = NULL, status = 'active', updated_at = now() WHERE id = $1`,
		periodID,
	); err != nil {
		h.log.Error().Err(err).Msg("reset period: reset phase")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := tx.Commit(); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.log.Info().Str("period_id", periodID.String()).Msg("reset period to phase 1 (debug)")

	// Notify SSE clients of the affected game to refetch.
	h.broker.PublishPhaseComplete(string(gameType), 0, 1, false)

	httpserver.WriteJSON(w, http.StatusOK, map[string]string{
		"reset":   periodID.String(),
		"message": "period reset to phase 1, all tiles free",
	})
}

// resetTile resets a single tile: deletes its submission and claim, sets status to free.
// Usage: POST /debug/tiles/{id}/reset
func (h *Handler) resetTile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tileIDStr := chi.URLParam(r, "id")
	tileID, err := uuid.Parse(tileIDStr)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile ID")
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM submissions WHERE tile_id = $1`, tileID); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE tile_id = $1`, tileID); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	res, err := tx.ExecContext(ctx, `UPDATE tiles SET status = 'free', updated_at = now() WHERE id = $1`, tileID)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		httpserver.WriteError(w, http.StatusNotFound, "tile not found")
		return
	}

	if err := tx.Commit(); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.log.Info().Str("tile_id", tileID.String()).Msg("reset tile (debug)")
	h.broker.PublishTileEvent(sse.EventTileFreed, tileID.String(), "free")

	httpserver.WriteJSON(w, http.StatusOK, map[string]string{
		"reset": tileID.String(),
	})
}

// drawAllTiles marks all free/locked tiles in the active period as drawn (god mode).
// For each tile it: uploads a colored placeholder JPEG, creates a real claim,
// records a submission, and flips the tile to drawn. The colored placeholders
// give the composer real bytes to stitch so the archive mosaic is visible.
// Usage: POST /debug/tiles/draw-all
func (h *Handler) drawAllTiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	gameType := grid.GameType(r.URL.Query().Get("game_type"))
	if gameType == "" {
		gameType = grid.GamePhoto
	}
	if !gameType.Valid() {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid game_type")
		return
	}

	period, err := h.gridSvc.GetActivePeriod(ctx, gameType)
	if err != nil {
		httpserver.WriteError(w, http.StatusNotFound, "no active period")
		return
	}

	type tileRow struct {
		id  uuid.UUID
		row int
		col int
	}

	rows, err := h.db.QueryContext(ctx,
		`SELECT id, row_index, col_index FROM tiles
		 WHERE period_id = $1 AND phase = $2 AND status != 'drawn'`,
		period.ID, period.Phase,
	)
	if err != nil {
		h.log.Error().Err(err).Msg("draw-all: query tiles")
		httpserver.WriteError(w, http.StatusInternalServerError, "query tiles failed")
		return
	}
	defer rows.Close()

	var tiles []tileRow
	for rows.Next() {
		var t tileRow
		if err := rows.Scan(&t.id, &t.row, &t.col); err != nil {
			h.log.Error().Err(err).Msg("draw-all: scan tile")
			httpserver.WriteError(w, http.StatusInternalServerError, "scan tile failed")
			return
		}
		tiles = append(tiles, t)
	}

	if len(tiles) == 0 {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"drawn": 0, "message": "all tiles already drawn",
		})
		return
	}

	// Upload a colored placeholder per tile in parallel. Done before the DB
	// transaction because object storage is not transactional; orphans on
	// tx-rollback are acceptable for a debug endpoint.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(drawAllUploadConcurrency)
	for _, t := range tiles {
		t := t
		g.Go(func() error {
			jpg, err := makePlaceholderJPEG(t.row, t.col)
			if err != nil {
				return fmt.Errorf("encode placeholder: %w", err)
			}
			key := fmt.Sprintf("tiles/%s.jpg", t.id.String())
			if err := h.store.PutObject(gctx, key, jpg, "image/jpeg"); err != nil {
				return fmt.Errorf("upload %s: %w", key, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		h.log.Error().Err(err).Msg("draw-all: parallel upload")
		httpserver.WriteError(w, http.StatusInternalServerError, "upload placeholders failed")
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback()

	for _, t := range tiles {
		if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE tile_id = $1`, t.id); err != nil {
			h.log.Error().Err(err).Msg("draw-all: delete claims")
			httpserver.WriteError(w, http.StatusInternalServerError, "delete claims failed")
			return
		}

		var claimID uuid.UUID
		err := tx.QueryRowContext(ctx,
			`INSERT INTO claims (tile_id, nickname, session_id, expires_at)
			 VALUES ($1, 'debug', 'debug-draw-all', now() + interval '1 hour')
			 RETURNING id`,
			t.id,
		).Scan(&claimID)
		if err != nil {
			h.log.Error().Err(err).Msg("draw-all: insert claim")
			httpserver.WriteError(w, http.StatusInternalServerError, "insert claim failed")
			return
		}

		key := fmt.Sprintf("tiles/%s.jpg", t.id.String())
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO submissions (tile_id, claim_id, storage_key, crop_x, crop_y, crop_width, crop_height)
			 VALUES ($1, $2, $3, 0, 0, 1, 1)
			 ON CONFLICT (tile_id) DO UPDATE SET claim_id = EXCLUDED.claim_id, storage_key = EXCLUDED.storage_key`,
			t.id, claimID, key,
		); err != nil {
			h.log.Error().Err(err).Msg("draw-all: insert submission")
			httpserver.WriteError(w, http.StatusInternalServerError, "insert submission failed")
			return
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE tiles SET status = 'drawn', updated_at = now() WHERE id = $1`, t.id,
		); err != nil {
			h.log.Error().Err(err).Msg("draw-all: update tile")
			httpserver.WriteError(w, http.StatusInternalServerError, "update tile failed")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		h.log.Error().Err(err).Msg("draw-all: commit")
		httpserver.WriteError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	for _, t := range tiles {
		h.broker.PublishTileEvent(sse.EventTileDrawn, t.id.String(), "drawn")
	}

	h.log.Info().Int("count", len(tiles)).Msg("drew all tiles (debug)")

	result, err := h.gridSvc.CheckPhaseCompletion(ctx, period.ID)
	if err != nil {
		h.log.Error().Err(err).Msg("check phase completion after draw-all")
	} else {
		switch result {
		case grid.PhaseAdvanced:
			h.broker.PublishPhaseComplete(period.GameType, period.Phase, period.Phase+1, false)
		case grid.PhaseAllComplete:
			h.broker.PublishPhaseComplete(period.GameType, period.Phase, 0, true)
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"drawn":   len(tiles),
		"message": "all tiles marked as drawn",
	})
}

const scheduleListWindowDays = 60

// listSchedule returns the upcoming N days of scheduled images (today
// inclusive). Each scheduled day includes a presigned URL so the admin UI
// can preview the image without a separate round-trip per cell.
// Usage: GET /debug/schedule
func (h *Handler) listSchedule(w http.ResponseWriter, r *http.Request) {
	loc, err := time.LoadLocation(grid.PeriodTimezone)
	if err != nil {
		h.log.Error().Err(err).Msg("schedule: load tz")
		httpserver.WriteError(w, http.StatusInternalServerError, "load timezone failed")
		return
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, scheduleListWindowDays-1)

	rows, err := h.repo.ListSchedule(r.Context(), from, to)
	if err != nil {
		h.log.Error().Err(err).Msg("schedule: list")
		httpserver.WriteError(w, http.StatusInternalServerError, "list schedule failed")
		return
	}

	type item struct {
		Date       string `json:"date"`
		StorageKey string `json:"storage_key"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		ImageURL   string `json:"image_url,omitempty"`
	}
	out := make([]item, len(rows))
	for i, row := range rows {
		out[i] = item{
			Date:       row.Date.Format("2006-01-02"),
			StorageKey: row.StorageKey,
			Width:      row.Width,
			Height:     row.Height,
		}
		if u, err := h.store.PresignedGetURL(r.Context(), row.StorageKey, 15*time.Minute); err == nil {
			out[i].ImageURL = u
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"from":  from.Format("2006-01-02"),
		"to":    to.Format("2006-01-02"),
		"items": out,
	})
}

// upsertSchedule registers (or replaces) a scheduled image for a given date.
// Usage: POST /debug/schedule
//
//	{"date": "2026-05-10", "storage_key": "schedule/may-10.jpg", "width": 1920, "height": 1080}
func (h *Handler) upsertSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date       string `json:"date"`
		StorageKey string `json:"storage_key"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Date == "" || body.StorageKey == "" || body.Width <= 0 || body.Height <= 0 {
		httpserver.WriteError(w, http.StatusBadRequest, "date, storage_key, width, height are required")
		return
	}
	date, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}

	saved, err := h.repo.UpsertScheduled(r.Context(), date, body.StorageKey, body.Width, body.Height)
	if err != nil {
		h.log.Error().Err(err).Msg("schedule: upsert")
		httpserver.WriteError(w, http.StatusInternalServerError, "upsert schedule failed")
		return
	}

	// If admin scheduled an image for today, kick off rotation immediately so
	// the photo game becomes playable without a manual "Create Period" step.
	// Idempotent: rotateForToday bails if a period for today already exists.
	if isTodayInCph(date) {
		if err := h.gridSvc.RotateForToday(r.Context(), grid.GamePhoto); err != nil {
			h.log.Warn().Err(err).Msg("schedule: rotate today (photo)")
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"date":        saved.Date.Format("2006-01-02"),
		"storage_key": saved.StorageKey,
		"width":       saved.Width,
		"height":      saved.Height,
	})
}

// deleteSchedule removes a scheduled image for a given date.
// Usage: DELETE /debug/schedule/2026-05-10
func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	dateStr := chi.URLParam(r, "date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}

	if err := h.repo.DeleteScheduledByDate(r.Context(), date); err != nil {
		h.log.Error().Err(err).Msg("schedule: delete")
		httpserver.WriteError(w, http.StatusInternalServerError, "delete schedule failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listPromptSchedule returns the upcoming N days of scheduled prompts.
// Usage: GET /debug/prompt-schedule
func (h *Handler) listPromptSchedule(w http.ResponseWriter, r *http.Request) {
	loc, err := time.LoadLocation(grid.PeriodTimezone)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "load timezone failed")
		return
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, scheduleListWindowDays-1)

	rows, err := h.repo.ListPromptSchedule(r.Context(), from, to)
	if err != nil {
		h.log.Error().Err(err).Msg("prompt schedule: list")
		httpserver.WriteError(w, http.StatusInternalServerError, "list prompt schedule failed")
		return
	}

	type item struct {
		Date   string `json:"date"`
		Prompt string `json:"prompt"`
	}
	out := make([]item, len(rows))
	for i, row := range rows {
		out[i] = item{
			Date:   row.Date.Format("2006-01-02"),
			Prompt: row.Prompt,
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"from":  from.Format("2006-01-02"),
		"to":    to.Format("2006-01-02"),
		"items": out,
	})
}

// upsertPromptSchedule registers (or replaces) a scheduled prompt for a date.
// Usage: POST /debug/prompt-schedule
//
//	{"date": "2026-05-10", "prompt": "an elephant riding a bike"}
func (h *Handler) upsertPromptSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date   string `json:"date"`
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Date == "" || body.Prompt == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "date and prompt are required")
		return
	}
	date, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}

	saved, err := h.repo.UpsertScheduledPrompt(r.Context(), date, body.Prompt)
	if err != nil {
		h.log.Error().Err(err).Msg("prompt schedule: upsert")
		httpserver.WriteError(w, http.StatusInternalServerError, "upsert prompt schedule failed")
		return
	}

	if isTodayInCph(date) {
		if err := h.gridSvc.RotateForToday(r.Context(), grid.GamePrompt); err != nil {
			h.log.Warn().Err(err).Msg("prompt schedule: rotate today")
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"date":   saved.Date.Format("2006-01-02"),
		"prompt": saved.Prompt,
	})
}

// isTodayInCph reports whether the given date refers to today's calendar
// date in the period timezone — used to decide if a freshly-scheduled entry
// should auto-rotate immediately rather than wait for the midnight sweeper.
func isTodayInCph(date time.Time) bool {
	loc, err := time.LoadLocation(grid.PeriodTimezone)
	if err != nil {
		return false
	}
	now := time.Now().In(loc)
	d := date.In(loc)
	return now.Year() == d.Year() && now.Month() == d.Month() && now.Day() == d.Day()
}

// deletePromptSchedule removes a scheduled prompt for a given date.
// Usage: DELETE /debug/prompt-schedule/2026-05-10
func (h *Handler) deletePromptSchedule(w http.ResponseWriter, r *http.Request) {
	dateStr := chi.URLParam(r, "date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}

	if err := h.repo.DeleteScheduledPromptByDate(r.Context(), date); err != nil {
		h.log.Error().Err(err).Msg("prompt schedule: delete")
		httpserver.WriteError(w, http.StatusInternalServerError, "delete prompt schedule failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listSessions returns one row per distinct session_id ever to claim a tile,
// with aggregate activity counts. Sorted by last activity desc.
// Usage: GET /debug/sessions?limit=100
func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	const q = `
		SELECT
		    c.session_id,
		    (
		        SELECT c2.nickname
		        FROM claims c2
		        WHERE c2.session_id = c.session_id
		        ORDER BY c2.claimed_at DESC
		        LIMIT 1
		    ) AS latest_nickname,
		    COUNT(*)                                                AS total_claims,
		    COUNT(s.id)                                             AS drawn_count,
		    COUNT(*) FILTER (WHERE c.released_at IS NOT NULL
		                     AND s.id IS NULL)                      AS released_count,
		    MIN(c.claimed_at)                                       AS first_seen,
		    GREATEST(MAX(c.claimed_at), COALESCE(MAX(s.created_at), MAX(c.claimed_at))) AS last_seen
		FROM claims c
		LEFT JOIN submissions s ON s.claim_id = c.id
		GROUP BY c.session_id
		ORDER BY last_seen DESC
		LIMIT $1
	`
	rows, err := h.db.QueryContext(r.Context(), q, limit)
	if err != nil {
		h.log.Error().Err(err).Msg("sessions: list")
		httpserver.WriteError(w, http.StatusInternalServerError, "list sessions failed")
		return
	}
	defer rows.Close()

	type sessionItem struct {
		SessionID      string    `json:"session_id"`
		LatestNickname string    `json:"latest_nickname"`
		TotalClaims    int64     `json:"total_claims"`
		DrawnCount     int64     `json:"drawn_count"`
		ReleasedCount  int64     `json:"released_count"`
		FirstSeen      time.Time `json:"first_seen"`
		LastSeen       time.Time `json:"last_seen"`
	}
	out := make([]sessionItem, 0)
	for rows.Next() {
		var it sessionItem
		var nickname sql.NullString
		if err := rows.Scan(
			&it.SessionID, &nickname,
			&it.TotalClaims, &it.DrawnCount, &it.ReleasedCount,
			&it.FirstSeen, &it.LastSeen,
		); err != nil {
			h.log.Error().Err(err).Msg("sessions: scan")
			httpserver.WriteError(w, http.StatusInternalServerError, "scan sessions failed")
			return
		}
		if nickname.Valid {
			it.LatestNickname = nickname.String
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "iterate sessions failed")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"sessions": out,
		"limit":    limit,
	})
}

// getSession returns the full claim history for one session, with submission
// thumbnails presigned for inline preview.
// Usage: GET /debug/sessions/{id}
func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "missing session id")
		return
	}

	const q = `
		SELECT
		    c.id, c.tile_id, c.nickname,
		    c.claimed_at, c.expires_at, c.released_at,
		    s.id, s.storage_key, s.created_at,
		    t.period_id, t.phase, t.row_index, t.col_index, t.status
		FROM claims c
		LEFT JOIN submissions s ON s.claim_id = c.id
		JOIN tiles t ON t.id = c.tile_id
		WHERE c.session_id = $1
		ORDER BY c.claimed_at DESC
		LIMIT 500
	`
	rows, err := h.db.QueryContext(r.Context(), q, sessionID)
	if err != nil {
		h.log.Error().Err(err).Msg("session: get")
		httpserver.WriteError(w, http.StatusInternalServerError, "get session failed")
		return
	}
	defer rows.Close()

	type claimItem struct {
		ClaimID      string     `json:"claim_id"`
		TileID       string     `json:"tile_id"`
		Nickname     string     `json:"nickname"`
		ClaimedAt    time.Time  `json:"claimed_at"`
		ExpiresAt    time.Time  `json:"expires_at"`
		ReleasedAt   *time.Time `json:"released_at,omitempty"`
		SubmissionID string     `json:"submission_id,omitempty"`
		SubmittedAt  *time.Time `json:"submitted_at,omitempty"`
		ImageURL     string     `json:"image_url,omitempty"`
		PeriodID     string     `json:"period_id"`
		Phase        int        `json:"phase"`
		Row          int        `json:"row"`
		Col          int        `json:"col"`
		TileStatus   string     `json:"tile_status"`
	}

	out := make([]claimItem, 0)
	for rows.Next() {
		var (
			it          claimItem
			released    sql.NullTime
			subID       sql.NullString
			subKey      sql.NullString
			submittedAt sql.NullTime
		)
		if err := rows.Scan(
			&it.ClaimID, &it.TileID, &it.Nickname,
			&it.ClaimedAt, &it.ExpiresAt, &released,
			&subID, &subKey, &submittedAt,
			&it.PeriodID, &it.Phase, &it.Row, &it.Col, &it.TileStatus,
		); err != nil {
			h.log.Error().Err(err).Msg("session: scan claim")
			httpserver.WriteError(w, http.StatusInternalServerError, "scan claim failed")
			return
		}
		if released.Valid {
			it.ReleasedAt = &released.Time
		}
		if subID.Valid {
			it.SubmissionID = subID.String
		}
		if submittedAt.Valid {
			it.SubmittedAt = &submittedAt.Time
		}
		if subKey.Valid && subKey.String != "" {
			if u, err := h.store.PresignedGetURL(r.Context(), subKey.String, 15*time.Minute); err == nil {
				it.ImageURL = u
			}
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "iterate claims failed")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"claims":     out,
	})
}

// makePlaceholderJPEG returns a small solid-color JPEG keyed off the tile's
// (row, col) so the resulting mosaic shows distinct cells.
func makePlaceholderJPEG(row, col int) ([]byte, error) {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := color.RGBA{
		R: uint8(60 + (row*53)%180),
		G: uint8(60 + (col*97)%180),
		B: uint8(60 + ((row+col)*73)%180),
		A: 255,
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
