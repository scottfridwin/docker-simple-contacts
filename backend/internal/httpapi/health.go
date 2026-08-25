package httpapi

import (
	"context"
	"net/http"
	"time"
)

// pinger reports database connectivity for readiness checks.
type pinger interface {
	Ping(ctx context.Context) error
}

// healthHandler always reports process liveness.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyHandler reports readiness, dependent on database connectivity.
func readyHandler(p pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := p.Ping(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "database is not reachable", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
