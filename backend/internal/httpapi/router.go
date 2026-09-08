package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/scottfridlund/contacts/backend/internal/auth"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

// NewRouter builds the application's HTTP handler.
func NewRouter(logger *slog.Logger, svc *person.Service, ready pinger, allowedOrigins []string, providers ...*auth.Provider) http.Handler {
	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(requestLogger(logger))
	r.Use(recoverer(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type", requestIDHeader},
		ExposedHeaders:   []string{requestIDHeader},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	if len(providers) > 0 && providers[0] != nil {
		r.Use(providers[0].Middleware)
		providers[0].Routes(r)
	}

	r.Get("/healthz", healthHandler)
	r.Get("/readyz", readyHandler(ready))

	h := &personHandler{svc: svc}
	r.Route("/api/v1", func(api chi.Router) {
		api.Route("/persons", func(p chi.Router) {
			p.Post("/", h.create)
			p.Get("/", h.list)
			p.Get("/deleted", h.listDeleted)
			p.Post("/{id}/restore", h.restore)
			p.Delete("/{id}/permanent", h.permanentDelete)
			p.Get("/{id}", h.get)
			p.Patch("/{id}", h.update)
			p.Delete("/{id}", h.delete)
		})
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	})

	return r
}
