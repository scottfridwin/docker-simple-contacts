package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/scottfridlund/contacts/backend/internal/person"
)

// NewRouter builds the application's HTTP handler.
func NewRouter(logger *slog.Logger, svc *person.Service, relSvc *person.RelationshipService, ready pinger, allowedOrigins []string) http.Handler {
	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(requestLogger(logger))
	r.Use(recoverer(logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type", requestIDHeader},
		ExposedHeaders:   []string{requestIDHeader},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", healthHandler)
	r.Get("/readyz", readyHandler(ready))

	h := &personHandler{svc: svc}
	rh := &relationshipHandler{svc: relSvc}
	r.Route("/api/v1", func(api chi.Router) {
		api.Route("/persons", func(p chi.Router) {
			p.Post("/", h.create)
			p.Get("/", h.list)
			p.Get("/{id}", h.get)
			p.Patch("/{id}", h.update)
			p.Delete("/{id}", h.delete)
			p.Route("/{id}/relationships", func(rel chi.Router) {
				rel.Post("/", rh.create)
				rel.Get("/", rh.listForPerson)
			})
		})
		api.Route("/relationships", func(r chi.Router) {
			r.Get("/{id}", rh.get)
			r.Patch("/{id}", rh.update)
			r.Delete("/{id}", rh.delete)
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
