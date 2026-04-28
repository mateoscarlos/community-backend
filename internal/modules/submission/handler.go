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
