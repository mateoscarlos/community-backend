package debug

import "github.com/go-chi/chi/v5"

// nopRegistrar registers no routes. Used in production.
type nopRegistrar struct{}

func (nopRegistrar) RegisterRoutes(chi.Router) {}
