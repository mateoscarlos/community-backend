package debug

import (
	"github.com/community-app/community-backend/internal/config"
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"go.uber.org/fx"
)

// Module registers the admin/debug endpoints only when ADMIN_SECRET is set.
// This works in every environment (local, dev, prod) — the routes simply
// don't exist unless a secret is configured, and when they do exist every
// request must present a matching X-Admin-Secret header (see handler.go).
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

// newRegistrarIfEnabled returns the debug Handler when an admin secret is
// configured, or a no-op registrar otherwise so the routes are never exposed
// without protection.
func newRegistrarIfEnabled(cfg *config.Config, h *Handler) httpserver.RouteRegistrar {
	if cfg.AdminSecret == "" {
		return nopRegistrar{}
	}
	return h
}
