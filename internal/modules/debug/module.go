package debug

import (
	"github.com/community-app/community-backend/internal/config"
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"go.uber.org/fx"
)

// Module registers debug endpoints only when ENV != "prod".
var Module = fx.Options(
	fx.Provide(
		NewHandler,
		fx.Annotate(
			newRegistrarIfEnabled,
			fx.As(new(httpserver.RouteRegistrar)),
			fx.ResultTags(`group:"routes"`),
		),
	),
)

// newRegistrarIfEnabled returns the debug Handler in non-prod environments,
// or a no-op registrar in production so no routes are ever exposed.
func newRegistrarIfEnabled(cfg *config.Config, h *Handler) httpserver.RouteRegistrar {
	if cfg.Env == "prod" {
		return nopRegistrar{}
	}
	return h
}
