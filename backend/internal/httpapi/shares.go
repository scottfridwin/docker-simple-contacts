package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/person"
)

type shareHandler struct {
	svc *person.Service
}

// shareResponse is the wire format for a grant of access to a Person.
type shareResponse struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func toShareResponse(s person.Share) shareResponse {
	return shareResponse{
		ID:          s.ID,
		Email:       s.SharedWithEmail,
		DisplayName: s.SharedWithDisplayName,
		CreatedAt:   s.CreatedAt,
	}
}

func (h *shareHandler) list(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	shares, err := h.svc.ListShares(r.Context(), id)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list shares", nil)
		return
	}
	out := make([]shareResponse, 0, len(shares))
	for _, s := range shares {
		out = append(out, toShareResponse(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type createShareRequest struct {
	Email string `json:"email"`
}

func (h *shareHandler) create(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	var req createShareRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be a JSON object", nil)
		return
	}

	share, validationErrs, err := h.svc.CreateShare(r.Context(), id, req.Email)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if validationErrs.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", validationErrs)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create share", nil)
		return
	}
	writeJSON(w, http.StatusCreated, toShareResponse(*share))
}

func (h *shareHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	shareID, err := uuid.Parse(chi.URLParam(r, "shareId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "shareId must be a valid UUID", nil)
		return
	}
	if err := h.svc.DeleteShare(r.Context(), id, shareID); err != nil {
		if errors.Is(err, person.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "share not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete share", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// leave lets the currently authenticated recipient remove their own access
// to a Person shared with them, without needing the owner to act.
func (h *shareHandler) leave(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.svc.LeaveShare(r.Context(), id); err != nil {
		if errors.Is(err, person.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "share not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to leave share", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
