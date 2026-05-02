package claim

import (
	"encoding/json"
	"errors"
	"net/http"

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
	r.Route("/api/v1/tiles/{id}", func(r chi.Router) {
		r.Post("/claim", h.claimTile)
		r.Delete("/claim", h.releaseClaim)
		r.Post("/heartbeat", h.heartbeat)
		r.Post("/extend", h.extend)
	})
}

func (h *Handler) claimTile(w http.ResponseWriter, r *http.Request) {
	tileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile id")
		return
	}

	var body struct {
		Nickname  string `json:"nickname"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	c, err := h.svc.ClaimTile(r.Context(), tileID, body.Nickname, body.SessionID)
	if err != nil {
		if errors.Is(err, ErrAlreadyClaimed) {
			httpserver.WriteError(w, http.StatusConflict, "tile already claimed")
			return
		}
		if errors.Is(err, ErrSessionAlreadyHasClaim) {
			httpserver.WriteError(w, http.StatusConflict, "session already has an active claim")
			return
		}
		h.log.Error().Err(err).Msg("claim tile")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, claimResponse{
		ClaimID:   c.ID.String(),
		TileID:    c.TileID.String(),
		ExpiresAt: c.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *Handler) releaseClaim(w http.ResponseWriter, r *http.Request) {
	tileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile id")
		return
	}

	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	if err := h.svc.ReleaseClaim(r.Context(), tileID, body.SessionID); err != nil {
		if errors.Is(err, ErrClaimNotFound) || errors.Is(err, ErrNotYourClaim) {
			httpserver.WriteError(w, http.StatusForbidden, "not your claim")
			return
		}
		h.log.Error().Err(err).Msg("release claim")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type claimResponse struct {
	ClaimID   string `json:"claim_id"`
	TileID    string `json:"tile_id"`
	ExpiresAt string `json:"expires_at"`
}

type expiresAtResponse struct {
	TileID    string `json:"tile_id"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	tileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile id")
		return
	}

	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	expiresAt, err := h.svc.Heartbeat(r.Context(), tileID, body.SessionID)
	if err != nil {
		if errors.Is(err, ErrClaimNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "claim not found")
			return
		}
		h.log.Error().Err(err).Msg("heartbeat")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, expiresAtResponse{
		TileID:    tileID.String(),
		ExpiresAt: expiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *Handler) extend(w http.ResponseWriter, r *http.Request) {
	tileID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid tile id")
		return
	}

	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	expiresAt, err := h.svc.Extend(r.Context(), tileID, body.SessionID)
	if err != nil {
		if errors.Is(err, ErrClaimNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "claim not found")
			return
		}
		h.log.Error().Err(err).Msg("extend")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, expiresAtResponse{
		TileID:    tileID.String(),
		ExpiresAt: expiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}
