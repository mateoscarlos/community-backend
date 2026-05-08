package grid

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/community-app/community-backend/internal/modules/dailyimage"
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"github.com/community-app/community-backend/internal/shared/sse"
	"github.com/community-app/community-backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const presignExpiry = 15 * time.Minute

type Handler struct {
	svc    *Service
	imgRepo dailyimage.Repository
	store   *storage.Storage
	broker  *sse.Broker
	log     zerolog.Logger
}

var _ httpserver.RouteRegistrar = (*Handler)(nil)

func NewHandler(svc *Service, imgRepo dailyimage.Repository, store *storage.Storage, broker *sse.Broker, log zerolog.Logger) *Handler {
	return &Handler{svc: svc, imgRepo: imgRepo, store: store, broker: broker, log: log}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/periods", func(r chi.Router) {
		r.Get("/current", h.getCurrent)
		r.Get("/current/events", sse.Handler(h.broker, h.log))
		r.Get("/archive", h.listArchive)
		r.Get("/{id}", h.getByID)
	})
}

func (h *Handler) getCurrent(w http.ResponseWriter, r *http.Request) {
	gameType, err := parseGameType(r.URL.Query().Get("game_type"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := h.svc.GetCurrent(r.Context(), gameType)
	if err != nil {
		if errors.Is(err, ErrPeriodNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "no active period")
			return
		}
		h.log.Error().Err(err).Msg("get current period")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp, err := h.buildPeriodResponse(r, current)
	if err != nil {
		h.log.Error().Err(err).Msg("build period response")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "invalid period id")
		return
	}

	current, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrPeriodNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "period not found")
			return
		}
		h.log.Error().Err(err).Msg("get period by id")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp, err := h.buildPeriodResponse(r, current)
	if err != nil {
		h.log.Error().Err(err).Msg("build period response")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) listArchive(w http.ResponseWriter, r *http.Request) {
	gameType, err := parseGameType(r.URL.Query().Get("game_type"))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 50 {
		perPage = 20
	}

	periods, err := h.svc.ListArchive(r.Context(), gameType, perPage, (page-1)*perPage)
	if err != nil {
		h.log.Error().Err(err).Msg("list archive")
		httpserver.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	items := make([]archivePeriodResponse, len(periods))
	for i, p := range periods {
		items[i] = archivePeriodResponse{
			ID:        p.ID.String(),
			GameType:  p.GameType,
			Phase:     p.Phase,
			StartedAt: p.StartedAt,
		}
		if p.EndedAt != nil {
			items[i].EndedAt = p.EndedAt
		}
		if p.FinalImageKey != "" {
			// Calendar day cells are tiny — presign the thumb variant to
			// keep payloads light. The thumb is generated alongside the full
			// in ComposeFinalImage; periods composed before thumbs existed
			// won't have one and will 404 on the browser side.
			thumbKey := ThumbnailKey(p.FinalImageKey)
			if u, err := h.store.PresignedGetURL(r.Context(), thumbKey, presignExpiry); err == nil {
				items[i].FinalImageURL = u
			}
		}
	}

	httpserver.WriteJSON(w, http.StatusOK, archiveListResponse{
		Periods: items,
		Page:    page,
		PerPage: perPage,
	})
}

func (h *Handler) buildPeriodResponse(r *http.Request, current *CurrentPeriod) (*periodResponse, error) {
	// Fetch the period's own daily image (not the currently-active one), so the
	// archive detail page shows the original picture for that day. Prompt-game
	// periods don't have an image — only a prompt string.
	var imageResp *imageResponse
	if current.Period.DailyImageID != nil {
		if imgDomain, err := h.imgRepo.GetByID(r.Context(), *current.Period.DailyImageID); err == nil {
			if imgURL, urlErr := h.store.PresignedGetURL(r.Context(), imgDomain.StorageKey, presignExpiry); urlErr == nil {
				imageResp = &imageResponse{
					ID:               imgDomain.ID.String(),
					Date:             imgDomain.Date.Format("2006-01-02"),
					ImageURL:         imgURL,
					Width:            imgDomain.Width,
					Height:           imgDomain.Height,
					ExpiresInSeconds: int(presignExpiry.Seconds()),
				}
			}
		}
	}

	tiles := make([]tileResponse, len(current.Tiles))
	for i, t := range current.Tiles {
		tiles[i] = tileResponse{
			ID:     t.ID.String(),
			Row:    t.RowIndex,
			Col:    t.ColIndex,
			Status: string(t.Status),
		}
		if t.Status == TileDrawn && t.SubmissionKey != "" {
			if imgURL, err := h.store.PresignedGetURL(r.Context(), t.SubmissionKey, presignExpiry); err == nil {
				tiles[i].ImageURL = imgURL
			}
		}
	}

	// Per-phase mosaics — every "draw" of the day, oldest first.
	mosaics, _ := h.svc.ListMosaics(r.Context(), current.Period.ID)
	mosaicResps := make([]phaseMosaicResponse, 0, len(mosaics))
	for _, m := range mosaics {
		url, err := h.store.PresignedGetURL(r.Context(), m.StorageKey, presignExpiry)
		if err != nil {
			continue
		}
		mosaicResps = append(mosaicResps, phaseMosaicResponse{
			Phase:      m.Phase,
			ImageURL:   url,
			ComposedAt: m.ComposedAt,
		})
	}

	return &periodResponse{
		Period: periodInfo{
			ID:           current.Period.ID.String(),
			GameType:     current.Period.GameType,
			Status:       string(current.Period.Status),
			Phase:        current.Period.Phase,
			StartedAt:    current.Period.StartedAt,
			Image:        imageResp,
			Prompt:       current.Period.Prompt,
			PhaseMosaics: mosaicResps,
		},
		Grid: gridResponse{
			Columns:    current.GridConfig.Columns,
			Rows:       current.GridConfig.Rows,
			TotalTiles: len(current.Tiles),
			DrawnCount: int(current.DrawnCount),
			Tiles:      tiles,
		},
	}, nil
}

// Response types

type periodResponse struct {
	Period periodInfo   `json:"period"`
	Grid   gridResponse `json:"grid"`
}

type periodInfo struct {
	ID           string                `json:"id"`
	GameType     string                `json:"game_type"`
	Status       string                `json:"status"`
	Phase        int                   `json:"phase"`
	StartedAt    time.Time             `json:"started_at"`
	Image        *imageResponse        `json:"image,omitempty"`
	Prompt       string                `json:"prompt,omitempty"`
	PhaseMosaics []phaseMosaicResponse `json:"phase_mosaics,omitempty"`
}

// parseGameType validates the ?game_type= query param. Defaults to photo to
// preserve backwards compat for existing clients.
func parseGameType(raw string) (GameType, error) {
	if raw == "" {
		return GamePhoto, nil
	}
	gt := GameType(raw)
	if !gt.Valid() {
		return "", errors.New("invalid game_type")
	}
	return gt, nil
}

type phaseMosaicResponse struct {
	Phase      int       `json:"phase"`
	ImageURL   string    `json:"image_url"`
	ComposedAt time.Time `json:"composed_at"`
}

type imageResponse struct {
	ID               string `json:"id"`
	Date             string `json:"date"`
	ImageURL         string `json:"image_url"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type gridResponse struct {
	Columns    int            `json:"columns"`
	Rows       int            `json:"rows"`
	TotalTiles int            `json:"total_tiles"`
	DrawnCount int            `json:"drawn_count"`
	Tiles      []tileResponse `json:"tiles"`
}

type tileResponse struct {
	ID       string `json:"id"`
	Row      int    `json:"row"`
	Col      int    `json:"col"`
	Status   string `json:"status"`
	ImageURL string `json:"image_url,omitempty"`
}

type archiveListResponse struct {
	Periods []archivePeriodResponse `json:"periods"`
	Page    int                     `json:"page"`
	PerPage int                     `json:"per_page"`
}

type archivePeriodResponse struct {
	ID            string     `json:"id"`
	GameType      string     `json:"game_type"`
	Phase         int        `json:"phase"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	FinalImageURL string     `json:"final_image_url,omitempty"`
}
