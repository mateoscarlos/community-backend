package feedback

import (
	"encoding/json"
	"net/http"

	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/go-chi/chi/v5"
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
	r.Post("/api/v1/feedback", h.submit)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
		Contact string `json:"contact"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Message == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "message is required")
		return
	}

	fb, err := h.svc.Submit(r.Context(), body.Message, body.Contact)
	if err != nil {
		h.log.Error().Err(err).Msg("submit feedback")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusCreated, feedbackResponse{
		ID:        fb.ID.String(),
		CreatedAt: fb.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

type feedbackResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}
