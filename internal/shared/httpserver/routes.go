package httpserver

import "github.com/go-chi/chi/v5"

// RouteRegistrar is implemented by each module's handler to mount its routes.
// Collected by Fx via the "routes" value group so main.go never needs to change
// when adding new modules.
type RouteRegistrar interface {
	RegisterRoutes(r chi.Router)
}
