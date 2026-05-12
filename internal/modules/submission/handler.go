package submission

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	svc *Service
	log zerolog.Logger
}

var _ httpserver.RouteRegistrar = (*Handler)(nil)

func NewHandler(svc *Service, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/uploads/presign", h.presign)
	r.Post("/api/v1/tiles/{id}/submit", h.submit)
	r.Post("/api/v1/uploads/stage-presign", h.stagePresign)
	r.Get("/api/v1/uploads/staged", h.getStaged)
}

func (h *Handler) presign(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TileID      string `json:"tile_id"`
		ContentType string `json:"content_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	tileID, err := uuid.Parse(body.TileID)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile_id")
		return
	}
	if body.ContentType == "" {
		body.ContentType = "image/jpeg"
	}

	uploadURL, storageKey, err := h.svc.Presign(r.Context(), tileID, body.ContentType)
	if err != nil {
		h.log.Error().Err(err).Msg("presign upload")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, presignResponse{
		UploadURL:        uploadURL,
		StorageKey:       storageKey,
		ExpiresInSeconds: int((5 * time.Minute).Seconds()),
	})
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	tileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile id")
		return
	}

	var body struct {
		SessionID  string `json:"session_id"`
		StorageKey string `json:"storage_key"`
		Crop       Crop   `json:"crop"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionID == "" || body.StorageKey == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id and storage_key are required")
		return
	}

	sub, err := h.svc.Submit(r.Context(), tileID, body.SessionID, body.StorageKey, body.Crop)
	if err != nil {
		if errors.Is(err, ErrNotYourClaim) {
			httpserver.WriteError(w, http.StatusForbidden, "not your claim")
			return
		}
		h.log.Error().Err(err).Msg("submit tile")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, submitResponse{
		ID:         sub.ID.String(),
		TileID:     sub.TileID.String(),
		StorageKey: sub.StorageKey,
	})
}

type presignResponse struct {
	UploadURL        string `json:"upload_url"`
	StorageKey       string `json:"storage_key"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type submitResponse struct {
	ID         string `json:"id"`
	TileID     string `json:"tile_id"`
	StorageKey string `json:"storage_key"`
}

type stagedResponse struct {
	DownloadURL string `json:"download_url"`
}

// stagePresign hands the phone a presigned PUT URL it can use to upload a
// raw camera photo. The laptop driving the game later picks it up via
// GET /uploads/staged.
func (h *Handler) stagePresign(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TileID    string `json:"tile_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	tileID, err := uuid.Parse(body.TileID)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile_id")
		return
	}
	if body.SessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	uploadURL, storageKey, err := h.svc.PresignStage(r.Context(), tileID, body.SessionID)
	if err != nil {
		h.log.Error().Err(err).Msg("stage presign")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, presignResponse{
		UploadURL:        uploadURL,
		StorageKey:       storageKey,
		ExpiresInSeconds: int((5 * time.Minute).Seconds()),
	})
}

// getStaged returns a download URL if the phone has uploaded a photo for the
// given (tile, session), or 404 if not yet — the laptop polls this endpoint.
func (h *Handler) getStaged(w http.ResponseWriter, r *http.Request) {
	tileIDStr := r.URL.Query().Get("tile")
	sessionID := r.URL.Query().Get("session")
	if tileIDStr == "" || sessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "tile and session are required")
		return
	}
	tileID, err := uuid.Parse(tileIDStr)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile")
		return
	}

	downloadURL, exists, err := h.svc.GetStaged(r.Context(), tileID, sessionID)
	if err != nil {
		h.log.Error().Err(err).Msg("get staged")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, stagedResponse{DownloadURL: downloadURL})
}
