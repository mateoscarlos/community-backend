package dailyimage

import (
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"go.uber.org/fx"
)

// Module bundles all Fx providers for the dailyimage module.
// Add fx.Options(dailyimage.Module) in main.go to enable this module.
var Module = fx.Options(
	fx.Provide(
		NewPostgresRepository,
		NewService,
		fx.Annotate(
			NewHandler,
			fx.As(new(httpserver.RouteRegistrar)),
			fx.ResultTags(`group:"routes"`),
		),
	),
)
