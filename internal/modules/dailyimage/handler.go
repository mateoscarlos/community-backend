package dailyimage

import (
	"errors"
	"net/http"
	"time"

	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

const presignExpiry = 15 * time.Minute

// Handler handles HTTP requests for the dailyimage module.
// Implements httpserver.RouteRegistrar.
type Handler struct {
	svc   *Service
	store *storage.Storage
	log   zerolog.Logger
}

// Compile-time check that Handler implements RouteRegistrar.
var _ httpserver.RouteRegistrar = (*Handler)(nil)

func NewHandler(svc *Service, store *storage.Storage, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, store: store, log: log}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/daily-image", h.getActive)
}

// getActive handles GET /daily-image.
func (h *Handler) getActive(w http.ResponseWriter, r *http.Request) {
	img, err := h.svc.GetActive(r.Context())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "no active daily image")
			return
		}
		h.log.Error().Err(err).Msg("get active daily image")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	url, err := h.store.PresignedGetURL(r.Context(), img.StorageKey, presignExpiry)
	if err != nil {
		h.log.Error().Err(err).Str("key", img.StorageKey).Msg("presign daily image")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.log.Info().Str("key", img.StorageKey).Str("url", url).Msg("serving daily image")

	httpserver.WriteJSON(w, http.StatusOK, getActiveResponse{
		ID:              img.ID.String(),
		Date:            img.Date.Format("2006-01-02"),
		ImageURL:        url,
		Width:           img.Width,
		Height:          img.Height,
		ExpiresInSeconds: int(presignExpiry.Seconds()),
	})
}

type getActiveResponse struct {
	ID               string `json:"id"`
	Date             string `json:"date"`
	ImageURL         string `json:"image_url"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}
