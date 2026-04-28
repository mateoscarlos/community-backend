package debug

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/community-app/community-backend/internal/modules/dailyimage"
	"github.com/community-app/community-backend/internal/modules/grid"
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/community-app/community-backend/internal/shared/sse"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const uploadURLExpiry = 30 * time.Minute

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
	})
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

// createPeriod creates an active period from the current active daily image.
// Usage: POST /debug/period
//
//	{"game_type": "photo"}  (optional, defaults to "photo")
func (h *Handler) createPeriod(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GameType string `json:"game_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.GameType = "photo"
	}
	if body.GameType == "" {
		body.GameType = "photo"
	}

	img, err := h.repo.GetActive(r.Context())
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "no active daily image — set one first")
		return
	}

	period, err := h.gridSvc.CreatePeriodWithTiles(r.Context(), img.ID, body.GameType)
	if err != nil {
		h.log.Error().Err(err).Msg("create debug period")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusCreated, period)
}

// deletePeriod deletes the active period and all its tiles, claims, and submissions.
// Usage: DELETE /debug/period
func (h *Handler) deletePeriod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var periodID uuid.UUID
	err := h.db.QueryRowContext(ctx, `SELECT id FROM periods WHERE status = 'active' LIMIT 1`).Scan(&periodID)
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
// Usage: POST /debug/period/reset
func (h *Handler) resetPeriod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var periodID uuid.UUID
	var currentPhase int32
	err := h.db.QueryRowContext(ctx,
		`SELECT id, phase FROM periods WHERE status = 'active' LIMIT 1`,
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

	// Notify all SSE clients to refetch
	h.broker.PublishPhaseComplete(0, 1, false)

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
// Also creates dummy submissions so the phase completion logic works.
// Usage: POST /debug/tiles/draw-all
func (h *Handler) drawAllTiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	period, err := h.gridSvc.GetActivePeriod(ctx)
	if err != nil {
		httpserver.WriteError(w, http.StatusNotFound, "no active period")
		return
	}

	// Get all non-drawn tiles for current phase
	rows, err := h.db.QueryContext(ctx,
		`SELECT id FROM tiles WHERE period_id = $1 AND phase = $2 AND status != 'drawn'`,
		period.ID, period.Phase,
	)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer rows.Close()

	var tileIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		tileIDs = append(tileIDs, id)
	}

	if len(tileIDs) == 0 {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"drawn": 0, "message": "all tiles already drawn",
		})
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer tx.Rollback()

	for _, id := range tileIDs {
		// Delete existing claims
		if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE tile_id = $1`, id); err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		// Create dummy submission
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO submissions (tile_id, claim_id, storage_key, crop_x, crop_y, crop_width, crop_height)
			 VALUES ($1, '00000000-0000-0000-0000-000000000000', 'debug/placeholder.jpg', 0, 0, 100, 100)
			 ON CONFLICT (tile_id) DO NOTHING`,
			id,
		); err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		// Mark as drawn
		if _, err := tx.ExecContext(ctx,
			`UPDATE tiles SET status = 'drawn', updated_at = now() WHERE id = $1`, id,
		); err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Publish SSE events for each tile
	for _, id := range tileIDs {
		h.broker.PublishTileEvent(sse.EventTileDrawn, id.String(), "drawn")
	}

	h.log.Info().Int("count", len(tileIDs)).Msg("drew all tiles (debug)")

	// Check phase completion
	result, err := h.gridSvc.CheckPhaseCompletion(ctx, period.ID)
	if err != nil {
		h.log.Error().Err(err).Msg("check phase completion after draw-all")
	} else {
		switch result {
		case grid.PhaseAdvanced:
			h.broker.PublishPhaseComplete(period.Phase, period.Phase+1, false)
		case grid.PhaseAllComplete:
			h.broker.PublishPhaseComplete(period.Phase, 0, true)
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"drawn":   len(tileIDs),
		"message": "all tiles marked as drawn",
	})
}
