package submission

import (
	"github.com/community-app/community-backend/internal/shared/httpserver"
	"go.uber.org/fx"
)

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
	fx.Invoke(StartStorageSweeper),
)
